package unifi

import (
	"context"
	"fmt"

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

// Schema STAYS IN THIS PACKAGE, HAND-WRITTEN, because internal/schemabehaviour
// derives what the provider applies by parsing unifi/*.go for a receiver whose
// Metadata names the surface and whose Schema it can follow, and promotion from
// an embedded type is invisible to a parser. Moving it into the kit would not
// fail; it would go quiet.
func (r *firewallGroupKitResource) Schema(
	ctx context.Context,
	_ resource.SchemaRequest,
	resp *resource.SchemaResponse,
) {
	resp.Schema = resource_firewall_group.FirewallGroupResourceSchema(ctx)
	// THE RELEASED SCHEMA DESCRIBES THIS SURFACE IN PLAIN TEXT, which a
	// generated schema cannot express. Dropping this call in the cutover
	// changed description_kind on six attributes and the block, and
	// TestBuiltSchemaMatchesReleasedBaseline caught it. ap_group and
	// port_profile call it too, so their cutovers need the same line.
	plainDescriptions(&resp.Schema)
	resp.Schema.Attributes["timeouts"] = timeouts.Attributes(
		ctx, timeouts.Opts{Create: true, Read: true, Update: true, Delete: true})
}

// Metadata is here for the same reason: the deriver reads the surface name off
// this method, in this package.
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
	r.Spec.Backend = firewallGroupKitBackend(client.ApiClient)
	r.DefaultSite = client.Site
}
