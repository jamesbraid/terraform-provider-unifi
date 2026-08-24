package unifi

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/list"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
	resource_port_forward "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_port_forward"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/resourcekit"
)

// The four nested models below stay in this package: they are the schema's
// shape, not the kit's — the descriptor's Encode/Decode and tests read them directly.

// portForwardWanModel describes the WAN configuration for a port forwarding rule.
type portForwardWanModel struct {
	Interface types.String `tfsdk:"interface"`
	IPAddress types.String `tfsdk:"ip_address"`
	Port      types.String `tfsdk:"port"`
}

func (m portForwardWanModel) AttributeTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"interface":  types.StringType,
		"ip_address": types.StringType,
		"port":       types.StringType,
	}
}

// portForwardForwardModel describes the forward destination for a port forwarding rule.
type portForwardForwardModel struct {
	IP   types.String `tfsdk:"ip"`
	Port types.String `tfsdk:"port"`
}

func (m portForwardForwardModel) AttributeTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"ip":   types.StringType,
		"port": types.StringType,
	}
}

// portForwardSourceLimitingModel describes the source limiting configuration.
type portForwardSourceLimitingModel struct {
	IP              types.String `tfsdk:"ip"`
	FirewallGroupID types.String `tfsdk:"firewall_group_id"`
	Enabled         types.Bool   `tfsdk:"enabled"`
	Type            types.String `tfsdk:"type"`
}

func (m portForwardSourceLimitingModel) AttributeTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"ip":                types.StringType,
		"firewall_group_id": types.StringType,
		"enabled":           types.BoolType,
		"type":              types.StringType,
	}
}

// portForwardDestinationIPModel describes an additional destination IP/interface
// pair for a port forwarding rule (used for multi-WAN setups).
type portForwardDestinationIPModel struct {
	DestinationIP types.String `tfsdk:"destination_ip"`
	Interface     types.String `tfsdk:"interface"`
}

func (m portForwardDestinationIPModel) AttributeTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"destination_ip": types.StringType,
		"interface":      types.StringType,
	}
}

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
