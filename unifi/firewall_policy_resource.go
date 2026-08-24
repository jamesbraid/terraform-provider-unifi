package unifi

import (
	"context"
	"fmt"
	"strconv"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/list"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
	resource_firewall_policy "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_firewall_policy"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/resourcekit"
)

type firewallPolicyKitResource struct {
	resourcekit.Resource[firewallPolicyKitModel, ui.FirewallPolicy]
}

var (
	_ resource.Resource                 = &firewallPolicyKitResource{}
	_ resource.ResourceWithImportState  = &firewallPolicyKitResource{}
	_ resource.ResourceWithIdentity     = &firewallPolicyKitResource{}
	_ resource.ResourceWithUpgradeState = &firewallPolicyKitResource{}
	_ list.ListResource                 = &firewallPolicyKitResource{}
	_ list.ListResourceWithConfigure    = &firewallPolicyKitResource{}
)

func newFirewallPolicyKitResource() *firewallPolicyKitResource {
	r := &firewallPolicyKitResource{}
	r.Spec = firewallPolicyKitSpec()
	r.SchemaSpec = firewallPolicyKitSchema()
	r.ListSurface = firewallPolicyKitList()
	return r
}

func NewFirewallPolicyResource() resource.Resource { return newFirewallPolicyKitResource() }

func NewFirewallPolicyListResource() list.ListResource { return newFirewallPolicyKitResource() }

// firewallPolicyEndpointModel is the nested source/destination block model.
type firewallPolicyEndpointModel struct {
	ZoneID           types.String `tfsdk:"zone_id"`
	MatchingTarget   types.String `tfsdk:"matching_target"`
	NetworkIDs       types.List   `tfsdk:"network_ids"`
	ClientMACs       types.List   `tfsdk:"client_macs"`
	IPs              types.List   `tfsdk:"ips"`
	WebDomains       types.List   `tfsdk:"web_domains"`
	Port             types.String `tfsdk:"port"`
	PortGroupID      types.String `tfsdk:"port_group_id"`
	IPGroupID        types.String `tfsdk:"ip_group_id"`
	PortMatchingType types.String `tfsdk:"port_matching_type"`
	// Firmware-managed; round-tripped so updates keep it (a PUT that omits
	// source/destination matching_target_type is rejected with HTTP 400).
	MatchingTargetType types.String `tfsdk:"matching_target_type"`
	// Declared because omitting them reversed rules rather than losing settings:
	// the SDK emits all four without omitempty, so a missing value forces false --
	// and match_opposite_ips=true flips "block all but this list" into "block only this list".
	MatchMAC              types.Bool `tfsdk:"match_mac"`
	MatchOppositeIPs      types.Bool `tfsdk:"match_opposite_ips"`
	MatchOppositeNetworks types.Bool `tfsdk:"match_opposite_networks"`
	MatchOppositePorts    types.Bool `tfsdk:"match_opposite_ports"`
}

func (m firewallPolicyEndpointModel) AttributeTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"zone_id":              types.StringType,
		"matching_target":      types.StringType,
		"network_ids":          types.ListType{ElemType: types.StringType},
		"client_macs":          types.ListType{ElemType: types.StringType},
		"ips":                  types.ListType{ElemType: types.StringType},
		"web_domains":          types.ListType{ElemType: types.StringType},
		"port":                 types.StringType,
		"port_group_id":        types.StringType,
		"ip_group_id":          types.StringType,
		"port_matching_type":   types.StringType,
		"matching_target_type": types.StringType,

		"match_mac":               types.BoolType,
		"match_opposite_ips":      types.BoolType,
		"match_opposite_networks": types.BoolType,
		"match_opposite_ports":    types.BoolType,
	}
}

func (r *firewallPolicyKitResource) Metadata(
	ctx context.Context,
	req resource.MetadataRequest,
	resp *resource.MetadataResponse,
) {
	resp.TypeName = req.ProviderTypeName + "_firewall_policy"
}

func (r *firewallPolicyKitResource) Schema(
	ctx context.Context,
	req resource.SchemaRequest,
	resp *resource.SchemaResponse,
) {
	resp.Schema = resource_firewall_policy.FirewallPolicyResourceSchema(ctx)
	// v1: source and destination `port` changed from Int64 to String.
	resp.Schema.Version = 1
	resp.Schema.Attributes["timeouts"] = timeouts.Attributes(
		ctx,
		timeouts.Opts{Create: true, Read: true, Update: true, Delete: true},
	)
}

func (r *firewallPolicyKitResource) Configure(
	ctx context.Context,
	req resource.ConfigureRequest,
	resp *resource.ConfigureResponse,
) {
	client, ok := resourceClient(req.ProviderData, &resp.Diagnostics)
	if !ok {
		return
	}

	r.Spec.Backend = firewallPolicyKitBackend(client.ApiClient)
	// The schedule hook is wired here, next to Backend, because it needs the same
	// controller to read from -- through Backend.Read, so there's one way this resource fetches a policy.
	r.Spec.BeforeSend = firewallPolicyCarrySchedule(r.Spec.Backend.Read, client.Site)
	r.DefaultSite = client.Site
}

// firewallPolicyEndpointModelV0 mirrors firewallPolicyEndpointModel but with the
// pre-v1 integer `port`. It exists only to decode prior state during upgrade.
type firewallPolicyEndpointModelV0 struct {
	ZoneID             types.String `tfsdk:"zone_id"`
	MatchingTarget     types.String `tfsdk:"matching_target"`
	NetworkIDs         types.List   `tfsdk:"network_ids"`
	ClientMACs         types.List   `tfsdk:"client_macs"`
	IPs                types.List   `tfsdk:"ips"`
	WebDomains         types.List   `tfsdk:"web_domains"`
	Port               types.Int64  `tfsdk:"port"`
	PortGroupID        types.String `tfsdk:"port_group_id"`
	IPGroupID          types.String `tfsdk:"ip_group_id"`
	PortMatchingType   types.String `tfsdk:"port_matching_type"`
	MatchingTargetType types.String `tfsdk:"matching_target_type"`
}

func (r *firewallPolicyKitResource) UpgradeState(
	ctx context.Context,
) map[int64]resource.StateUpgrader {
	// Derives the v0 schema from the live one, swapping port back to an integer --
	// the only structural difference -- so the upgrader stays correct as the rest of the schema evolves.
	var schemaResp resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &schemaResp)
	priorSchema := schemaResp.Schema
	priorSchema.Version = 0
	for _, key := range []string{"source", "destination"} {
		nested, ok := priorSchema.Attributes[key].(schema.SingleNestedAttribute)
		if !ok {
			// Silently leaving port as a string would decode v0 state against the
			// wrong type at upgrade time, on a path no test exercises; panicking surfaces it at provider start instead.
			panic(fmt.Sprintf("firewall policy %q is not a single nested attribute, so the v0 schema cannot be derived", key))
		}
		// The generated schema's custom object type is what GetType reports, so
		// replacing the attribute alone wouldn't change it -- dropping CustomType makes the framework derive the object from these attributes instead.
		nested.CustomType = nil
		attrs := make(map[string]schema.Attribute, len(nested.Attributes))
		for k, v := range nested.Attributes {
			attrs[k] = v
		}
		attrs["port"] = schema.Int64Attribute{Optional: true, Computed: true}
		nested.Attributes = attrs
		priorSchema.Attributes[key] = nested
	}

	return map[int64]resource.StateUpgrader{
		// v0 modeled port as an integer, which dropped multi-port values (#286) and
		// serialized portless endpoints as invalid "0" (#288); v1 is a string, so 0/null here means "no port".
		0: {
			PriorSchema: &priorSchema,
			StateUpgrader: func(
				ctx context.Context,
				req resource.UpgradeStateRequest,
				resp *resource.UpgradeStateResponse,
			) {
				var state firewallPolicyKitModel
				resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
				if resp.Diagnostics.HasError() {
					return
				}

				state.Source = upgradeFirewallPolicyEndpointV0(
					ctx, state.Source, &resp.Diagnostics,
				)
				state.Destination = upgradeFirewallPolicyEndpointV0(
					ctx, state.Destination, &resp.Diagnostics,
				)
				if resp.Diagnostics.HasError() {
					return
				}

				resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
			},
		},
	}
}

func upgradeFirewallPolicyEndpointV0(
	ctx context.Context,
	obj types.Object,
	diags *diag.Diagnostics,
) types.Object {
	newTypes := firewallPolicyEndpointModel{}.AttributeTypes()
	if obj.IsNull() {
		return types.ObjectNull(newTypes)
	}
	if obj.IsUnknown() {
		return types.ObjectUnknown(newTypes)
	}

	var v0 firewallPolicyEndpointModelV0
	diags.Append(obj.As(ctx, &v0, basetypes.ObjectAsOptions{})...)
	if diags.HasError() {
		return obj
	}

	port := types.StringNull()
	if !v0.Port.IsNull() && !v0.Port.IsUnknown() && v0.Port.ValueInt64() != 0 {
		port = types.StringValue(strconv.FormatInt(v0.Port.ValueInt64(), 10))
	}

	upgraded := firewallPolicyEndpointModel{
		ZoneID:             v0.ZoneID,
		MatchingTarget:     v0.MatchingTarget,
		NetworkIDs:         v0.NetworkIDs,
		ClientMACs:         v0.ClientMACs,
		IPs:                v0.IPs,
		WebDomains:         v0.WebDomains,
		Port:               port,
		PortGroupID:        v0.PortGroupID,
		IPGroupID:          v0.IPGroupID,
		PortMatchingType:   v0.PortMatchingType,
		MatchingTargetType: v0.MatchingTargetType,
	}

	newObj, d := types.ObjectValueFrom(ctx, newTypes, upgraded)
	diags.Append(d...)
	return newObj
}

// portToStringValue maps the API port string to a Terraform value: "" (portless)
// and historically "0" (older provider versions, #288) both map to null so plans stay clean.
func portToStringValue(p string) types.String {
	if p == "" || p == "0" {
		return types.StringNull()
	}
	return types.StringValue(p)
}

// firewallPolicyMatchingTargetType ensures a concrete matching_target_type for a
// specific (non-ANY) match: the controller rejects an empty type on an IP/NETWORK
// match (#293) when a source moves off ANY, and rejects "SPECIFIC" for a group
// reference via ip_group_id (#316), which always derives "OBJECT" instead. A controller-assigned "OBJECT"/"LIST" is preserved.
func firewallPolicyMatchingTargetType(matchingTarget, currentType, ipGroupID string) string {
	if ipGroupID != "" && currentType != "OBJECT" && currentType != "LIST" {
		return "OBJECT"
	}
	if matchingTarget != "" && matchingTarget != "ANY" &&
		(currentType == "" || currentType == "ANY") {
		return "SPECIFIC"
	}
	return currentType
}
