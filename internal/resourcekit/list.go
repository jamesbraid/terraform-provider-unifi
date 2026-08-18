package resourcekit

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/list"
	listschema "github.com/hashicorp/terraform-plugin-framework/list/schema"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// ListConfig is the configuration every list resource in this provider takes.
//
// ONE TYPE FOR ALL OF THEM because all of them declare the same two attributes:
// a site and a repeated name/value filter. That is not an assumption -- 25 of
// the 27 managed resources register a list surface and the generated schemas
// are identical in shape -- but it IS the thing that breaks first if a surface
// ever grows a third, and the decode is strict enough to say so rather than
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

	// Filters renders the fields a `filter` block may name, keyed by the name a
	// practitioner writes.
	//
	// STRINGS ON BOTH SIDES, and that is the framework's shape rather than a
	// simplification: a filter block carries name and value as strings, so a
	// boolean field is compared as "true". Rendering here rather than comparing
	// here keeps the comparison in one place and the rendering with the field
	// that knows its own type.
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

// ListResource streams every object on the site that matches the filters.
//
// THE FILTERS ARE APPLIED HERE AND NOT SENT. The controller's list endpoints
// take no query, so every object comes back and the provider narrows it. That
// is worth knowing when a site is large: the cost is one request and a scan,
// not one request per match.
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

	// A FILTER NAMING NO FIELD IS REFUSED RATHER THAN IGNORED. The hand-written
	// resources skip an unknown key silently, which returns every object and
	// reads as "nothing matched that value" -- the practitioner sees a wrong
	// answer rather than a mistake. Every name a surface accepts is in Filters.
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

	stream.Results = func(push func(list.ListResult) bool) {
		for i := range objects {
			object := &objects[i]
			if !r.matches(object, wanted) {
				continue
			}
			result := req.NewListResult(ctx)
			result.DisplayName = r.ListSurface.DisplayName(object)
			result.Diagnostics.Append(result.Identity.SetAttribute(
				ctx, path.Root("id"), types.StringValue(r.Spec.Backend.GetID(object)))...)

			var model M
			result.Diagnostics.Append(r.Spec.ToModel(ctx, object, &model, site)...)
			*r.Spec.Timeouts(&model) = nullTimeouts()
			result.Diagnostics.Append(result.Resource.Set(ctx, model)...)

			if !push(result) {
				return
			}
		}
	}
}

// matches is SAFE ON ITS OWN, and that is a correction rather than caution.
//
// The first version indexed Filters and called the result. Removing the
// unknown-filter refusal above -- as a mutation, to prove the refusal was
// tested -- made this PANIC on a nil function rather than fail: a missing key
// in a map of funcs yields nil, and calling it segfaults the provider.
//
// So the refusal was load-bearing against a crash, which is a fragile pairing:
// two things a hundred lines apart, one silently holding the other up. The
// refusal is now the message a practitioner gets and this is the reason a typo
// cannot take the provider down, and each stands without the other.
func (r *Resource[M, S]) matches(object *S, wanted map[string]string) bool {
	for name, value := range wanted {
		render, known := r.ListSurface.Filters[name]
		if !known {
			// Unreachable while the refusal above stands. Not matching is the
			// safe direction if it ever does not: an unfilterable name that
			// matched everything is the wrong answer this surface exists to
			// avoid.
			return false
		}
		if render(object) != value {
			return false
		}
	}
	return true
}
