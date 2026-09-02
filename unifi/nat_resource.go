package unifi

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/list"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
	resource_nat "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_nat"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/resourcekit"
)

type natKitResource struct {
	resourcekit.Resource[natKitModel, ui.Nat]
}

var (
	_ resource.Resource                = &natKitResource{}
	_ resource.ResourceWithImportState = &natKitResource{}
	_ resource.ResourceWithIdentity    = &natKitResource{}
	_ list.ListResource                = &natKitResource{}
	_ list.ListResourceWithConfigure   = &natKitResource{}
)

func newNATKitResource() *natKitResource {
	r := &natKitResource{}
	r.Spec = natKitSpec()
	r.SchemaSpec = natKitSchema()
	r.ListSurface = natKitList()
	return r
}

func (r *natKitResource) Schema(
	ctx context.Context,
	_ resource.SchemaRequest,
	resp *resource.SchemaResponse,
) {
	resp.Schema = resource_nat.NatResourceSchema(ctx)
	resp.Schema.Attributes["timeouts"] = timeouts.Attributes(
		ctx, timeouts.Opts{Create: true, Read: true, Update: true, Delete: true})
}

// Metadata is here, not promoted from an embedded type: descriptor_policy_test.go's
// kitServedSurfaces resolves each surface's TypeName by parsing this method.
func (r *natKitResource) Metadata(
	_ context.Context,
	req resource.MetadataRequest,
	resp *resource.MetadataResponse,
) {
	resp.TypeName = req.ProviderTypeName + "_nat"
}

func NewNATResource() resource.Resource { return newNATKitResource() }

func NewNATListResource() list.ListResource { return newNATKitResource() }

func (r *natKitResource) Configure(
	_ context.Context,
	req resource.ConfigureRequest,
	resp *resource.ConfigureResponse,
) {
	client, ok := resourceClient(req.ProviderData, &resp.Diagnostics)
	if !ok {
		return
	}
	r.Spec.Backend = natKitBackend(client.ApiClient)
	r.DefaultSite = client.Site
}
