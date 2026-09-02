package unifi

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/list"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
	resource_dpi_group "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_dpi_group"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/resourcekit"
)

type dpiGroupKitResource struct {
	resourcekit.Resource[dpiGroupKitModel, ui.DpiGroup]
}

var (
	_ resource.Resource                = &dpiGroupKitResource{}
	_ resource.ResourceWithImportState = &dpiGroupKitResource{}
	_ resource.ResourceWithIdentity    = &dpiGroupKitResource{}
	_ list.ListResource                = &dpiGroupKitResource{}
	_ list.ListResourceWithConfigure   = &dpiGroupKitResource{}
)

func newDpiGroupKitResource() *dpiGroupKitResource {
	r := &dpiGroupKitResource{}
	r.Spec = dpiGroupKitSpec()
	r.SchemaSpec = dpiGroupKitSchema()
	r.ListSurface = dpiGroupKitList()
	return r
}

func (r *dpiGroupKitResource) Schema(
	ctx context.Context,
	_ resource.SchemaRequest,
	resp *resource.SchemaResponse,
) {
	resp.Schema = resource_dpi_group.DpiGroupResourceSchema(ctx)
	resp.Schema.Attributes["timeouts"] = timeouts.Attributes(
		ctx, timeouts.Opts{Create: true, Read: true, Update: true, Delete: true})
}

// Metadata is here, not promoted from an embedded type: descriptor_policy_test.go's
// kitServedSurfaces resolves each surface's TypeName by parsing this method.
func (r *dpiGroupKitResource) Metadata(
	_ context.Context,
	req resource.MetadataRequest,
	resp *resource.MetadataResponse,
) {
	resp.TypeName = req.ProviderTypeName + "_dpi_group"
}

func NewDpiGroupResource() resource.Resource { return newDpiGroupKitResource() }

func NewDpiGroupListResource() list.ListResource { return newDpiGroupKitResource() }

func (r *dpiGroupKitResource) Configure(
	_ context.Context,
	req resource.ConfigureRequest,
	resp *resource.ConfigureResponse,
) {
	client, ok := resourceClient(req.ProviderData, &resp.Diagnostics)
	if !ok {
		return
	}
	r.Spec.Backend = dpiGroupKitBackend(client.ApiClient)
	r.DefaultSite = client.Site
}
