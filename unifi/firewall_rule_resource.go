package unifi

import (
	"context"

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

// Metadata is here, not promoted from an embedded type: descriptor_policy_test.go's
// kitServedSurfaces resolves each surface's TypeName by parsing this method.
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
	client, ok := resourceClient(req.ProviderData, &resp.Diagnostics)
	if !ok {
		return
	}
	r.Spec.Backend = firewallRuleKitBackend(client.ApiClient)
	r.DefaultSite = client.Site
}
