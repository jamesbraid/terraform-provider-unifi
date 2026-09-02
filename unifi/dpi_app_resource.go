package unifi

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/list"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
	resource_dpi_app "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_dpi_app"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/resourcekit"
)

type dpiAppKitResource struct {
	resourcekit.Resource[dpiAppKitModel, ui.DpiApp]
}

var (
	_ resource.Resource                = &dpiAppKitResource{}
	_ resource.ResourceWithImportState = &dpiAppKitResource{}
	_ resource.ResourceWithIdentity    = &dpiAppKitResource{}
	_ list.ListResource                = &dpiAppKitResource{}
	_ list.ListResourceWithConfigure   = &dpiAppKitResource{}
)

func newDpiAppKitResource() *dpiAppKitResource {
	r := &dpiAppKitResource{}
	r.Spec = dpiAppKitSpec()
	r.SchemaSpec = dpiAppKitSchema()
	r.ListSurface = dpiAppKitList()
	return r
}

func (r *dpiAppKitResource) Schema(
	ctx context.Context,
	_ resource.SchemaRequest,
	resp *resource.SchemaResponse,
) {
	resp.Schema = resource_dpi_app.DpiAppResourceSchema(ctx)
	resp.Schema.Attributes["timeouts"] = timeouts.Attributes(
		ctx, timeouts.Opts{Create: true, Read: true, Update: true, Delete: true})
}

// Metadata is here, not promoted from an embedded type: descriptor_policy_test.go's
// kitServedSurfaces resolves each surface's TypeName by parsing this method.
func (r *dpiAppKitResource) Metadata(
	_ context.Context,
	req resource.MetadataRequest,
	resp *resource.MetadataResponse,
) {
	resp.TypeName = req.ProviderTypeName + "_dpi_app"
}

func NewDpiAppResource() resource.Resource { return newDpiAppKitResource() }

func NewDpiAppListResource() list.ListResource { return newDpiAppKitResource() }

func (r *dpiAppKitResource) Configure(
	_ context.Context,
	req resource.ConfigureRequest,
	resp *resource.ConfigureResponse,
) {
	client, ok := resourceClient(req.ProviderData, &resp.Diagnostics)
	if !ok {
		return
	}
	r.Spec.Backend = dpiAppKitBackend(client.ApiClient)
	r.DefaultSite = client.Site
}
