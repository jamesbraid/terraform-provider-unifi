package unifi

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/list"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
	resource_hotspot_op "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_hotspot_op"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/resourcekit"
)

type hotspotOpKitResource struct {
	resourcekit.Resource[hotspotOpKitModel, ui.HotspotOp]
}

var (
	_ resource.Resource                = &hotspotOpKitResource{}
	_ resource.ResourceWithImportState = &hotspotOpKitResource{}
	_ resource.ResourceWithIdentity    = &hotspotOpKitResource{}
	_ list.ListResource                = &hotspotOpKitResource{}
	_ list.ListResourceWithConfigure   = &hotspotOpKitResource{}
)

func newHotspotOpKitResource() *hotspotOpKitResource {
	r := &hotspotOpKitResource{}
	r.Spec = hotspotOpKitSpec()
	r.SchemaSpec = hotspotOpKitSchema()
	r.ListSurface = hotspotOpKitList()
	return r
}

func (r *hotspotOpKitResource) Schema(
	ctx context.Context,
	_ resource.SchemaRequest,
	resp *resource.SchemaResponse,
) {
	resp.Schema = resource_hotspot_op.HotspotOpResourceSchema(ctx)
	resp.Schema.Attributes["timeouts"] = timeouts.Attributes(
		ctx, timeouts.Opts{Create: true, Read: true, Update: true, Delete: true})
}

// Metadata is here, not promoted from an embedded type: descriptor_policy_test.go's
// kitServedSurfaces resolves each surface's TypeName by parsing this method.
func (r *hotspotOpKitResource) Metadata(
	_ context.Context,
	req resource.MetadataRequest,
	resp *resource.MetadataResponse,
) {
	resp.TypeName = req.ProviderTypeName + "_hotspot_op"
}

func NewHotspotOpResource() resource.Resource { return newHotspotOpKitResource() }

func NewHotspotOpListResource() list.ListResource { return newHotspotOpKitResource() }

func (r *hotspotOpKitResource) Configure(
	_ context.Context,
	req resource.ConfigureRequest,
	resp *resource.ConfigureResponse,
) {
	client, ok := resourceClient(req.ProviderData, &resp.Diagnostics)
	if !ok {
		return
	}
	r.Spec.Backend = hotspotOpKitBackend(client.ApiClient)
	r.DefaultSite = client.Site
}
