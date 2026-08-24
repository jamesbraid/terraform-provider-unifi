package unifi

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/list"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
	resource_firewall_zone "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_firewall_zone"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/resourcekit"
)

type firewallZoneKitResource struct {
	resourcekit.Resource[firewallZoneKitModel, ui.FirewallZone]
}

var (
	_ resource.Resource                = &firewallZoneKitResource{}
	_ resource.ResourceWithImportState = &firewallZoneKitResource{}
	_ resource.ResourceWithIdentity    = &firewallZoneKitResource{}
	_ list.ListResource                = &firewallZoneKitResource{}
	_ list.ListResourceWithConfigure   = &firewallZoneKitResource{}
)

func newFirewallZoneKitResource() *firewallZoneKitResource {
	r := &firewallZoneKitResource{}
	r.Spec = firewallZoneKitSpec()
	r.SchemaSpec = firewallZoneKitSchema()
	r.ListSurface = firewallZoneKitList()
	return r
}

// No plainDescriptions call here (unlike firewall_group): this surface's released
// schema already matches the generated markdown text, so adding the call would flip description_kind the wrong way.
func (r *firewallZoneKitResource) Schema(
	ctx context.Context,
	_ resource.SchemaRequest,
	resp *resource.SchemaResponse,
) {
	resp.Schema = resource_firewall_zone.FirewallZoneResourceSchema(ctx)
	resp.Schema.Attributes["timeouts"] = timeouts.Attributes(
		ctx, timeouts.Opts{Create: true, Read: true, Update: true, Delete: true})
}

// Metadata is here, not promoted from an embedded type: descriptor_policy_test.go's
// kitServedSurfaces resolves each surface's TypeName by parsing this method.
func (r *firewallZoneKitResource) Metadata(
	_ context.Context,
	req resource.MetadataRequest,
	resp *resource.MetadataResponse,
) {
	resp.TypeName = req.ProviderTypeName + "_firewall_zone"
}

func NewFirewallZoneResource() resource.Resource { return newFirewallZoneKitResource() }

func NewFirewallZoneListResource() list.ListResource { return newFirewallZoneKitResource() }

func (r *firewallZoneKitResource) Configure(
	_ context.Context,
	req resource.ConfigureRequest,
	resp *resource.ConfigureResponse,
) {
	client, ok := resourceClient(req.ProviderData, &resp.Diagnostics)
	if !ok {
		return
	}
	r.Spec.Backend = firewallZoneKitBackend(client.ApiClient)
	r.DefaultSite = client.Site
}
