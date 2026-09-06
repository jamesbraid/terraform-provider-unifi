package unifi

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/list"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/listresource_site"
	resource_site "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_site"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/resourcekit"
)

type siteKitResource struct {
	resourcekit.Resource[siteKitModel, ui.Site]
}

var (
	_ resource.Resource                = &siteKitResource{}
	_ resource.ResourceWithImportState = &siteKitResource{}
	_ resource.ResourceWithIdentity    = &siteKitResource{}
	_ list.ListResource                = &siteKitResource{}
	_ list.ListResourceWithConfigure   = &siteKitResource{}
)

func newSiteKitResource() *siteKitResource {
	r := &siteKitResource{}
	r.Spec = siteKitSpec()
	r.SchemaSpec = siteKitSchema()
	// ONLY ConfigSchema. Sites are global, so their list config has no site
	// attribute and the kit's List (which decodes one) cannot serve it; the
	// List method below supplies its own, and ConfigSchema is the one member
	// the kit still consults, from ListResourceConfigSchema.
	r.ListSurface = resourcekit.ListSpec[ui.Site]{
		ConfigSchema: listresource_site.SiteListResourceSchema,
	}
	return r
}

func (r *siteKitResource) Schema(
	ctx context.Context,
	_ resource.SchemaRequest,
	resp *resource.SchemaResponse,
) {
	resp.Schema = resource_site.SiteResourceSchema(ctx)
	resp.Schema.Attributes["timeouts"] = timeouts.Attributes(
		ctx, timeouts.Opts{Create: true, Read: true, Update: true, Delete: true})
}

// Metadata is here, not promoted from an embedded type: descriptor_policy_test.go's
// kitServedSurfaces resolves each surface's TypeName by parsing this method.
func (r *siteKitResource) Metadata(
	_ context.Context,
	req resource.MetadataRequest,
	resp *resource.MetadataResponse,
) {
	resp.TypeName = req.ProviderTypeName + "_site"
}

func NewSiteFrameworkResource() resource.Resource { return newSiteKitResource() }

func NewSiteListResource() list.ListResource { return newSiteKitResource() }

func (r *siteKitResource) Configure(
	_ context.Context,
	req resource.ConfigureRequest,
	resp *resource.ConfigureResponse,
) {
	client, ok := resourceClient(req.ProviderData, &resp.Diagnostics)
	if !ok {
		return
	}
	r.Spec.Backend = siteKitBackend(client.ApiClient)
	r.DefaultSite = client.Site
}

// siteListConfigModel is the list configuration. Sites are global, so there
// is no site attribute -- the one shape in the provider the kit's shared
// ListConfig cannot decode, which is why List below is this surface's own.
type siteListConfigModel struct {
	Filter types.List `tfsdk:"filter"`
}

// List streams every site that matches the filters.
func (r *siteKitResource) List(
	ctx context.Context,
	req list.ListRequest,
	stream *list.ListResultsStream,
) {
	var config siteListConfigModel
	if diags := req.Config.Get(ctx, &config); diags.HasError() {
		stream.Results = list.ListResultsStreamDiagnostics(diags)
		return
	}

	wanted := map[string]string{}
	if !config.Filter.IsNull() && !config.Filter.IsUnknown() {
		var filters []resourcekit.ListFilter
		if diags := config.Filter.ElementsAs(ctx, &filters, false); diags.HasError() {
			stream.Results = list.ListResultsStreamDiagnostics(diags)
			return
		}
		for _, f := range filters {
			wanted[f.Name.ValueString()] = f.Value.ValueString()
		}
	}

	// The same refusal the kit's List makes: a filter naming no field would
	// match everything, which reads as "nothing matched" rather than as the
	// practitioner's mistake.
	fields := map[string]func(*ui.Site) string{
		"name":        func(s *ui.Site) string { return s.Name },
		"description": func(s *ui.Site) string { return s.Description },
	}
	var unknown diag.Diagnostics
	for name := range wanted {
		if _, ok := fields[name]; !ok {
			unknown.AddError("Unknown filter",
				"This resource has no filterable field named "+name+
					". A filter that names nothing would match everything.")
		}
	}
	if unknown.HasError() {
		stream.Results = list.ListResultsStreamDiagnostics(unknown)
		return
	}

	sites, err := r.Spec.Backend.List(ctx, "")
	if err != nil {
		var d diag.Diagnostics
		d.AddError("Error Listing Sites",
			"Could not list sites: "+resourcekit.DiagErrorText(err))
		stream.Results = list.ListResultsStreamDiagnostics(d)
		return
	}

	stream.Results = func(push func(list.ListResult) bool) {
		for i := range sites {
			site := &sites[i]
			matched := true
			for name, value := range wanted {
				if fields[name](site) != value {
					matched = false
					break
				}
			}
			if !matched {
				continue
			}

			result := req.NewListResult(ctx)

			// Display name: prefer description, fall back to name then ID.
			switch {
			case site.Description != "":
				result.DisplayName = site.Description
			case site.Name != "":
				result.DisplayName = site.Name
			default:
				result.DisplayName = site.ID
			}

			result.Diagnostics.Append(result.Identity.SetAttribute(
				ctx, path.Root("id"), types.StringValue(site.ID))...)

			var model siteKitModel
			result.Diagnostics.Append(r.Spec.ToModel(ctx, site, &model, "")...)
			model.Timeouts = timeoutsNullValue()
			result.Diagnostics.Append(result.Resource.Set(ctx, model)...)

			if !push(result) {
				return
			}
		}
	}
}
