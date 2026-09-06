package unifi

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
	resource_bgp "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_bgp"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/resourcekit"
)

type bgpKitResource struct {
	resourcekit.Resource[bgpKitModel, ui.BGPConfig]
}

var (
	_ resource.Resource                = &bgpKitResource{}
	_ resource.ResourceWithImportState = &bgpKitResource{}
	_ resource.ResourceWithIdentity    = &bgpKitResource{}
)

func newBGPKitResource() *bgpKitResource {
	r := &bgpKitResource{}
	r.Spec = bgpKitSpec()
	r.SchemaSpec = bgpKitSchema()
	return r
}

func (r *bgpKitResource) Schema(
	ctx context.Context,
	_ resource.SchemaRequest,
	resp *resource.SchemaResponse,
) {
	resp.Schema = resource_bgp.BgpResourceSchema(ctx)
	resp.Schema.Attributes["timeouts"] = timeouts.Attributes(
		ctx, timeouts.Opts{Create: true, Read: true, Update: true, Delete: true})
}

// Metadata is here, not promoted from an embedded type: descriptor_policy_test.go's
// kitServedSurfaces resolves each surface's TypeName by parsing this method.
func (r *bgpKitResource) Metadata(
	_ context.Context,
	req resource.MetadataRequest,
	resp *resource.MetadataResponse,
) {
	resp.TypeName = req.ProviderTypeName + "_bgp"
}

func NewBGPResource() resource.Resource { return newBGPKitResource() }

func (r *bgpKitResource) Configure(
	_ context.Context,
	req resource.ConfigureRequest,
	resp *resource.ConfigureResponse,
) {
	client, ok := resourceClient(req.ProviderData, &resp.Diagnostics)
	if !ok {
		return
	}
	r.Spec.Backend = bgpKitBackend(client.ApiClient)
	r.DefaultSite = client.Site
}
