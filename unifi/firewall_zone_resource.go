package unifi

import (
	"context"
	"fmt"

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

// Schema STAYS IN THIS PACKAGE, HAND-WRITTEN, because internal/schemabehaviour
// derives what the provider applies by parsing unifi/*.go for a receiver whose
// Metadata names the surface and whose Schema it can follow, and promotion from
// an embedded type is invisible to a parser. Moving it into the kit would not
// fail; it would go quiet.
//
// NO plainDescriptions HERE, unlike firewall_group: this surface's released
// schema is described by the generated text, and adding the call would change
// description_kind in the other direction. The presence or absence of that line
// is per-surface and TestBuiltSchemaMatchesReleasedBaseline is what says which.
func (r *firewallZoneKitResource) Schema(
	ctx context.Context,
	_ resource.SchemaRequest,
	resp *resource.SchemaResponse,
) {
	resp.Schema = resource_firewall_zone.FirewallZoneResourceSchema(ctx)
	resp.Schema.Attributes["timeouts"] = timeouts.Attributes(
		ctx, timeouts.Opts{Create: true, Read: true, Update: true, Delete: true})
}

// Metadata is here for the same reason: the deriver reads the surface name off
// this method, in this package.
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
	if req.ProviderData == nil {
		return
	}
	client, ok := req.ProviderData.(*Client)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Resource Configure Type",
			fmt.Sprintf("Expected *Client, got: %T. Please report this issue to the provider developers.",
				req.ProviderData),
		)
		return
	}
	r.Spec.Backend = firewallZoneKitBackend(client.ApiClient)
	r.DefaultSite = client.Site
}
