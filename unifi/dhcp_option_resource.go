package unifi

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/list"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
	resource_dhcp_option "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_dhcp_option"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/resourcekit"
)

type dhcpOptionKitResource struct {
	resourcekit.Resource[dhcpOptionKitModel, ui.DHCPOption]
}

var (
	_ resource.Resource                = &dhcpOptionKitResource{}
	_ resource.ResourceWithImportState = &dhcpOptionKitResource{}
	_ resource.ResourceWithIdentity    = &dhcpOptionKitResource{}
	_ list.ListResource                = &dhcpOptionKitResource{}
	_ list.ListResourceWithConfigure   = &dhcpOptionKitResource{}
)

func newDHCPOptionKitResource() *dhcpOptionKitResource {
	r := &dhcpOptionKitResource{}
	r.Spec = dhcpOptionKitSpec()
	r.SchemaSpec = dhcpOptionKitSchema()
	r.ListSurface = dhcpOptionKitList()
	return r
}

func (r *dhcpOptionKitResource) Schema(
	ctx context.Context,
	_ resource.SchemaRequest,
	resp *resource.SchemaResponse,
) {
	resp.Schema = resource_dhcp_option.DhcpOptionResourceSchema(ctx)
	resp.Schema.Attributes["timeouts"] = timeouts.Attributes(
		ctx, timeouts.Opts{Create: true, Read: true, Update: true, Delete: true})
}

// Metadata is here, not promoted from an embedded type: descriptor_policy_test.go's
// kitServedSurfaces resolves each surface's TypeName by parsing this method.
func (r *dhcpOptionKitResource) Metadata(
	_ context.Context,
	req resource.MetadataRequest,
	resp *resource.MetadataResponse,
) {
	resp.TypeName = req.ProviderTypeName + "_dhcp_option"
}

func NewDHCPOptionResource() resource.Resource { return newDHCPOptionKitResource() }

func NewDHCPOptionListResource() list.ListResource { return newDHCPOptionKitResource() }

func (r *dhcpOptionKitResource) Configure(
	_ context.Context,
	req resource.ConfigureRequest,
	resp *resource.ConfigureResponse,
) {
	client, ok := resourceClient(req.ProviderData, &resp.Diagnostics)
	if !ok {
		return
	}
	r.Spec.Backend = dhcpOptionKitBackend(client.ApiClient)
	r.DefaultSite = client.Site
}
