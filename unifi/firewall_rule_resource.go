package unifi

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/list"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
	resource_firewall_rule "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_firewall_rule"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/resourcekit"
)

type firewallRuleKitResource struct {
	resourcekit.Resource[firewallRuleKitModel, ui.FirewallRule]
}

var (
	_ resource.Resource                = &firewallRuleKitResource{}
	_ resource.ResourceWithImportState = &firewallRuleKitResource{}
	_ resource.ResourceWithIdentity    = &firewallRuleKitResource{}
	_ list.ListResource                = &firewallRuleKitResource{}
	_ list.ListResourceWithConfigure   = &firewallRuleKitResource{}
)

func newFirewallRuleKitResource() *firewallRuleKitResource {
	r := &firewallRuleKitResource{}
	r.Spec = firewallRuleKitSpec()
	r.SchemaSpec = firewallRuleKitSchema()
	r.ListSurface = firewallRuleKitList()
	return r
}

// Schema STAYS IN THIS PACKAGE, HAND-WRITTEN, because internal/schemabehaviour
// derives what the provider applies by parsing unifi/*.go for a receiver whose
// Metadata names the surface and whose Schema it can follow, and promotion from
// an embedded type is invisible to a parser. Moving it into the kit would not
// fail; it would go quiet.
func (r *firewallRuleKitResource) Schema(
	ctx context.Context,
	_ resource.SchemaRequest,
	resp *resource.SchemaResponse,
) {
	resp.Schema = resource_firewall_rule.FirewallRuleResourceSchema(ctx)
	resp.Schema.Attributes["timeouts"] = timeouts.Attributes(
		ctx,
		timeouts.Opts{Create: true, Read: true, Update: true, Delete: true},
	)
}

// Metadata is here for the same reason: the deriver reads the surface name off
// this method, in this package.
func (r *firewallRuleKitResource) Metadata(
	_ context.Context,
	req resource.MetadataRequest,
	resp *resource.MetadataResponse,
) {
	resp.TypeName = req.ProviderTypeName + "_firewall_rule"
}

func NewFirewallRuleResource() resource.Resource { return newFirewallRuleKitResource() }

func NewFirewallRuleListResource() list.ListResource { return newFirewallRuleKitResource() }

func (r *firewallRuleKitResource) Configure(
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
			fmt.Sprintf(
				"Expected *Client, got: %T. Please report this issue to the provider developers.",
				req.ProviderData,
			),
		)
		return
	}
	r.Spec.Backend = firewallRuleKitBackend(client.ApiClient)
	r.DefaultSite = client.Site
}
