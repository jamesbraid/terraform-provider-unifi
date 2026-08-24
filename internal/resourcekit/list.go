package resourcekit

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/list"
	listschema "github.com/hashicorp/terraform-plugin-framework/list/schema"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// ListConfig is the configuration every list resource takes: one type for
// all of them, since every one declares the same site and repeated
// name/value filter shape. That's the first thing to break if a surface ever
// grows a third attribute, which is why the decode is strict rather than
// silently ignoring it.
type ListConfig struct {
	Site   types.String `tfsdk:"site"`
	Filter types.List   `tfsdk:"filter"`
}

// ListFilter is one name/value pair from the configuration.
type ListFilter struct {
	Name  types.String `tfsdk:"name"`
	Value types.String `tfsdk:"value"`
}

// ListSpec is the part of a list surface that varies.
type ListSpec[S any] struct {
	// ConfigSchema is the generated list schema, passed rather than derived --
	// the tfplugingen output is a function per surface and the kit cannot name
	// it generically.
	ConfigSchema func(context.Context) listschema.Schema

	// DisplayName is what the practitioner sees for one result. dns_record
	// prefers the record key and falls back to the id, which is a per-resource
	// choice about which of its fields is recognisable.
	DisplayName func(*S) string

	// Filters renders the fields a `filter` block may name, keyed by the name
	// a practitioner writes. Strings on both sides is the framework's shape,
	// not a simplification: a filter block carries name and value as
	// strings, so a boolean field compares as "true".
	Filters map[string]func(*S) string
}

// ListResourceConfigSchema implements [list.ListResource].
func (r *Resource[M, S]) ListResourceConfigSchema(
	ctx context.Context,
	_ list.ListResourceSchemaRequest,
	resp *list.ListResourceSchemaResponse,
) {
	resp.Schema = r.ListSurface.ConfigSchema(ctx)
}

// List streams every object on the site that matches the filters. The
// controller's list endpoints take no query, so every object comes back and
// the provider narrows it here -- one request and a scan, not one request
// per match.
func (r *Resource[M, S]) List(
	ctx context.Context,
	req list.ListRequest,
	stream *list.ListResultsStream,
) {
	var config ListConfig
	if diags := req.Config.Get(ctx, &config); diags.HasError() {
		stream.Results = list.ListResultsStreamDiagnostics(diags)
		return
	}

	site := config.Site.ValueString()
	if site == "" {
		site = r.DefaultSite
	}

	wanted := map[string]string{}
	if !config.Filter.IsNull() && !config.Filter.IsUnknown() {
		var filters []ListFilter
		if diags := config.Filter.ElementsAs(ctx, &filters, false); diags.HasError() {
			stream.Results = list.ListResultsStreamDiagnostics(diags)
			return
		}
		for _, f := range filters {
			wanted[f.Name.ValueString()] = f.Value.ValueString()
		}
	}

	// A filter naming no field is refused rather than ignored: silently
	// skipping it would return every object, which reads as "nothing
	// matched" rather than as the practitioner's mistake.
	var unknown diag.Diagnostics
	for name := range wanted {
		if _, ok := r.ListSurface.Filters[name]; !ok {
			unknown.AddError("Unknown filter",
				"This resource has no filterable field named "+name+
					". A filter that names nothing would match everything.")
		}
	}
	if unknown.HasError() {
		stream.Results = list.ListResultsStreamDiagnostics(unknown)
		return
	}

	objects, err := r.Spec.Backend.List(ctx, site)
	if err != nil {
		var diags diag.Diagnostics
		diags.AddError("Error Listing "+r.Spec.Subject, err.Error())
		stream.Results = list.ListResultsStreamDiagnostics(diags)
		return
	}

	// Prefetch runs here once, and AfterReceive runs per object below -- the
	// same order Read uses, so a listed object and a read one go through the
	// same steps. ReadDefault (see field.go) only solves this for a
	// transform needing nothing but the object; a surface like port_profile
	// needs the site's whole network inventory, which no Field accessor can
	// reach, so the hooks have to run here too.
	//
	// Prefetch runs once, not per object: it's scoped to the site, and
	// calling it inside the loop would turn one request into one per result.
	prefetched, prefetchDiags := r.prefetch(ctx, site)
	if prefetchDiags.HasError() {
		stream.Results = list.ListResultsStreamDiagnostics(prefetchDiags)
		return
	}

	stream.Results = func(push func(list.ListResult) bool) {
		for i := range objects {
			object := &objects[i]
			if !r.matches(object, wanted) {
				continue
			}
			result := req.NewListResult(ctx)
			// A warning from Prefetch is not dropped: only an error ends the
			// stream, so anything short of one would otherwise vanish here
			// while Read reports it. It rides on the first result, once, not
			// on every result.
			if len(prefetchDiags) > 0 {
				result.Diagnostics.Append(prefetchDiags...)
				prefetchDiags = nil
			}
			result.DisplayName = r.ListSurface.DisplayName(object)
			result.Diagnostics.Append(result.Identity.SetAttribute(
				ctx, path.Root("id"), types.StringValue(r.Spec.Backend.GetID(object)))...)

			// A list has no prior and says so with a zero model: nothing was
			// recorded for these objects, so a hook that carries a value
			// forward must see that rather than see the previous element's.
			var model M
			var prior M
			result.Diagnostics.Append(r.Spec.ToModel(ctx, object, &model, site)...)
			result.Diagnostics.Append(r.afterReceive(ctx, object, &model, prior, prefetched)...)
			*r.Spec.Timeouts(&model) = nullTimeouts()
			result.Diagnostics.Append(result.Resource.Set(ctx, model)...)

			if !push(result) {
				return
			}
		}
	}
}

// matches is safe on its own, deliberately redundant with the refusal in
// List: an earlier version indexed Filters and called the result directly,
// which panicked on a nil func for an unknown name instead of failing safely.
func (r *Resource[M, S]) matches(object *S, wanted map[string]string) bool {
	for name, value := range wanted {
		render, known := r.ListSurface.Filters[name]
		if !known {
			// Unreachable while the refusal in List stands; not matching is
			// still the safe direction if it ever isn't, since matching
			// everything is the wrong answer this surface exists to avoid.
			return false
		}
		if render(object) != value {
			return false
		}
	}
	return true
}
