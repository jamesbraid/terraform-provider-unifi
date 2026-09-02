package unifi

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
	resource_ospf_router "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_ospf_router"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/resourcekit"
)

type ospfRouterKitResource struct {
	resourcekit.Resource[ospfRouterKitModel, ui.OSPFRouter]
}

var (
	_ resource.Resource                = &ospfRouterKitResource{}
	_ resource.ResourceWithImportState = &ospfRouterKitResource{}
	_ resource.ResourceWithIdentity    = &ospfRouterKitResource{}
)

func newOSPFRouterKitResource() *ospfRouterKitResource {
	r := &ospfRouterKitResource{}
	r.Spec = ospfRouterKitSpec()
	r.SchemaSpec = ospfRouterKitSchema()
	return r
}

func NewOSPFRouterResource() resource.Resource { return newOSPFRouterKitResource() }

func (r *ospfRouterKitResource) Schema(
	ctx context.Context,
	_ resource.SchemaRequest,
	resp *resource.SchemaResponse,
) {
	resp.Schema = resource_ospf_router.OspfRouterResourceSchema(ctx)
	// Grafted rather than generated, as everywhere else: timeouts.Attributes
	// is a call, not a literal, so the code specification cannot carry it.
	resp.Schema.Attributes["timeouts"] = timeouts.Attributes(
		ctx,
		timeouts.Opts{Create: true, Read: true, Update: true, Delete: true},
	)
}

// Metadata is here, not promoted from an embedded type: descriptor_policy_test.go's
// kitServedSurfaces resolves each surface's TypeName by parsing this method.
func (r *ospfRouterKitResource) Metadata(
	_ context.Context,
	req resource.MetadataRequest,
	resp *resource.MetadataResponse,
) {
	resp.TypeName = req.ProviderTypeName + "_ospf_router"
}

func (r *ospfRouterKitResource) Configure(
	_ context.Context,
	req resource.ConfigureRequest,
	resp *resource.ConfigureResponse,
) {
	client, ok := resourceClient(req.ProviderData, &resp.Diagnostics)
	if !ok {
		return
	}
	r.Spec.Backend = ospfRouterKitBackend(client.ApiClient)
	r.DefaultSite = client.Site
}
