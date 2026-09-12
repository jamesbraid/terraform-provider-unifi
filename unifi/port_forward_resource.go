package unifi

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/list"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
	resource_port_forward "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_port_forward"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/resourcekit"
)

// The four nested models below stay in this package: they are the schema's
// shape, not the kit's — the descriptor's Encode/Decode and tests read them directly.

type portForwardResource struct {
	resourcekit.Resource[portForwardKitModel, ui.PortForward]
}

var (
	_ resource.Resource                = &portForwardResource{}
	_ resource.ResourceWithImportState = &portForwardResource{}
	_ resource.ResourceWithIdentity    = &portForwardResource{}
	_ list.ListResource                = &portForwardResource{}
	_ list.ListResourceWithConfigure   = &portForwardResource{}
)

func newPortForwardKitResource() *portForwardResource {
	r := &portForwardResource{}
	r.Spec = portForwardKitSpec()
	r.SchemaSpec = portForwardKitSchema()
	r.ListSurface = portForwardKitList()
	return r
}

func NewPortForwardResource() resource.Resource { return newPortForwardKitResource() }

func NewPortForwardListResource() list.ListResource { return newPortForwardKitResource() }

func (r *portForwardResource) Schema(
	ctx context.Context,
	_ resource.SchemaRequest,
	resp *resource.SchemaResponse,
) {
	resp.Schema = resource_port_forward.PortForwardResourceSchema(ctx)
	resp.Schema.Attributes["timeouts"] = timeouts.Attributes(
		ctx,
		timeouts.Opts{Create: true, Read: true, Update: true, Delete: true},
	)
}

// Metadata is here, not promoted from an embedded type: descriptor_policy_test.go's
// kitServedSurfaces resolves each surface's TypeName by parsing this method.
func (r *portForwardResource) Metadata(
	_ context.Context,
	req resource.MetadataRequest,
	resp *resource.MetadataResponse,
) {
	resp.TypeName = req.ProviderTypeName + "_port_forward"
}

func (r *portForwardResource) Configure(
	_ context.Context,
	req resource.ConfigureRequest,
	resp *resource.ConfigureResponse,
) {
	client, ok := resourceClient(req.ProviderData, &resp.Diagnostics)
	if !ok {
		return
	}
	r.Spec.Backend = portForwardKitBackend(client.ApiClient)
	r.DefaultSite = client.Site
}
