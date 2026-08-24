package unifi

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/list"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
	resource_firewall_group "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_firewall_group"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/resourcekit"
)

type firewallGroupKitResource struct {
	resourcekit.Resource[firewallGroupKitModel, ui.FirewallGroup]
}

var (
	_ resource.Resource                = &firewallGroupKitResource{}
	_ resource.ResourceWithImportState = &firewallGroupKitResource{}
	_ resource.ResourceWithIdentity    = &firewallGroupKitResource{}
	_ list.ListResource                = &firewallGroupKitResource{}
	_ list.ListResourceWithConfigure   = &firewallGroupKitResource{}
)

func newFirewallGroupKitResource() *firewallGroupKitResource {
	r := &firewallGroupKitResource{}
	r.Spec = firewallGroupKitSpec()
	r.SchemaSpec = firewallGroupKitSchema()
	r.ListSurface = firewallGroupKitList()
	return r
}

func (r *firewallGroupKitResource) Schema(
	ctx context.Context,
	_ resource.SchemaRequest,
	resp *resource.SchemaResponse,
) {
	resp.Schema = resource_firewall_group.FirewallGroupResourceSchema(ctx)
	// The released schema is plain text, not markdown, so strip the MarkdownDescription
	// a generated schema always carries (ap_group and port_profile call it too).
	plainDescriptions(&resp.Schema)
	resp.Schema.Attributes["timeouts"] = timeouts.Attributes(
		ctx, timeouts.Opts{Create: true, Read: true, Update: true, Delete: true})
}

// Metadata is here, not promoted from an embedded type: descriptor_policy_test.go's
// kitServedSurfaces resolves each surface's TypeName by parsing this method.
func (r *firewallGroupKitResource) Metadata(
	_ context.Context,
	req resource.MetadataRequest,
	resp *resource.MetadataResponse,
) {
	resp.TypeName = req.ProviderTypeName + "_firewall_group"
}

func NewFirewallGroupFrameworkResource() resource.Resource { return newFirewallGroupKitResource() }

func NewFirewallGroupListResource() list.ListResource { return newFirewallGroupKitResource() }

func (r *firewallGroupKitResource) Configure(
	_ context.Context,
	req resource.ConfigureRequest,
	resp *resource.ConfigureResponse,
) {
	client, ok := resourceClient(req.ProviderData, &resp.Diagnostics)
	if !ok {
		return
	}
	r.Spec.Backend = firewallGroupKitBackend(client.ApiClient)
	r.DefaultSite = client.Site
}
