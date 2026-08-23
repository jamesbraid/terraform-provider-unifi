package unifi

// The port_forward descriptor.
//
// THREE SCATTERED OBJECTS AND ONE REAL ONE, which is the shape the SDK forces.
// `wan`, `forward` and `source_limiting` are groupings the schema invented:
// each spans several flat fields of ui.PortForward and none has a struct behind
// it. `destination_ips` is the exception -- the SDK owns
// PortForwardDestinationIPs -- so it is an ObjectListField and the others are
// ScatteredObjectFields.
//
// FOURTEEN WIRES, ENUMERATED FROM WHAT THE MAPPER WROTE. The names do not line
// up with the attributes and one pair actively misleads: wan.ip_address is
// destination_ip, SINGULAR, while the separate destination_ips list is the
// multi-WAN one. wan.port is dst_port and wan.interface is pfwd_interface.
// Taking this list from the SDK's field names pairs the two destination fields
// and gets both wrong, and every name a scattered object omits is an attribute
// the practitioner sets and the apply silently drops.

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/listresource_port_forward"
	resource_port_forward "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_port_forward"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/resourcekit"
)

type portForwardKitModel struct {
	ID             types.String   `tfsdk:"id"`
	Site           types.String   `tfsdk:"site"`
	Name           types.String   `tfsdk:"name"`
	Wan            types.Object   `tfsdk:"wan"`
	Forward        types.Object   `tfsdk:"forward"`
	SourceLimiting types.Object   `tfsdk:"source_limiting"`
	DestinationIPs types.List     `tfsdk:"destination_ips"`
	Protocol       types.String   `tfsdk:"protocol"`
	Logging        types.Bool     `tfsdk:"logging"`
	Enabled        types.Bool     `tfsdk:"enabled"`
	Timeouts       timeouts.Value `tfsdk:"timeouts"`
}

// portForwardWanField binds `wan` to the three flat fields it spans.
func encodePortForwardWan(ctx context.Context, object types.Object, sdk *ui.PortForward) diag.Diagnostics {
	var diags diag.Diagnostics
	var wan portForwardWanModel
	diags.Append(object.As(ctx, &wan, basetypes.ObjectAsOptions{})...)
	if diags.HasError() {
		return diags
	}
	// EACH MEMBER IS GUARDED SEPARATELY, as the hand-written mapper
	// guarded it. All three carry omitempty, so a member the
	// practitioner left out stays "" and never reaches the wire.
	if !wan.Interface.IsNull() {
		sdk.PfwdInterface = wan.Interface.ValueString()
	}
	if !wan.IPAddress.IsNull() {
		sdk.DestinationIP = wan.IPAddress.ValueString()
	}
	if !wan.Port.IsNull() {
		sdk.DstPort = wan.Port.ValueString()
	}
	return diags
}

func decodePortForwardWan(ctx context.Context, sdk *ui.PortForward, _ types.Object) (types.Object, diag.Diagnostics) {
	attrTypes := portForwardWanModel{}.AttributeTypes()
	if sdk.PfwdInterface == "" && sdk.DestinationIP == "" && sdk.DstPort == "" {
		return types.ObjectNull(attrTypes), nil
	}
	value := portForwardWanModel{
		Interface: portForwardStringOrNullValue(sdk.PfwdInterface),
		IPAddress: portForwardStringOrNullValue(sdk.DestinationIP),
		Port:      portForwardStringOrNullValue(sdk.DstPort),
	}
	return types.ObjectValueFrom(ctx, attrTypes, value)
}

func encodePortForwardForward(ctx context.Context, object types.Object, sdk *ui.PortForward) diag.Diagnostics {
	var diags diag.Diagnostics
	var forward portForwardForwardModel
	diags.Append(object.As(ctx, &forward, basetypes.ObjectAsOptions{})...)
	if diags.HasError() {
		return diags
	}
	if !forward.IP.IsNull() {
		sdk.Fwd = forward.IP.ValueString()
	}
	if !forward.Port.IsNull() {
		sdk.FwdPort = forward.Port.ValueString()
	}
	return diags
}

func decodePortForwardForward(ctx context.Context, sdk *ui.PortForward, _ types.Object) (types.Object, diag.Diagnostics) {
	attrTypes := portForwardForwardModel{}.AttributeTypes()
	if sdk.Fwd == "" && sdk.FwdPort == "" {
		return types.ObjectNull(attrTypes), nil
	}
	value := portForwardForwardModel{
		IP:   portForwardStringOrNullValue(sdk.Fwd),
		Port: portForwardStringOrNullValue(sdk.FwdPort),
	}
	return types.ObjectValueFrom(ctx, attrTypes, value)
}

// portForwardSourceLimitingConfigured decides whether a read means the
// controller holds source limiting or is reporting its own default.
//
// THE PLAIN ZERO CHECK IS WRONG HERE and it is the one thing on this surface
// that no field kind could have guessed. The controller answers src "any" with
// limiting disabled on EVERY rule, so treating a non-empty src as configuration
// makes an omitted block plan as null and apply as an object -- "provider
// produced inconsistent result after apply". The predicate is the hand-written
// read's, moved rather than rewritten.
func portForwardSourceLimitingConfigured(sdk *ui.PortForward) bool {
	return sdk.SrcLimitingEnabled ||
		sdk.SrcFirewallGroupID != "" ||
		(sdk.Src != "" && sdk.Src != "any")
}

func encodePortForwardSourceLimiting(ctx context.Context, object types.Object, sdk *ui.PortForward) diag.Diagnostics {
	var diags diag.Diagnostics
	var source portForwardSourceLimitingModel
	diags.Append(object.As(ctx, &source, basetypes.ObjectAsOptions{})...)
	if diags.HasError() {
		return diags
	}
	// GUARDED, SO THE PREDICATES ABOVE HAVE SOMETHING TO KEY ON. Assigning
	// unconditionally writes the zero for an unset member, and an assigned zero
	// is indistinguishable from a real write -- no predicate can take it off
	// the mask, and the mapper is the only place to fix it.
	if !source.IP.IsNull() && !source.IP.IsUnknown() {
		sdk.Src = source.IP.ValueString()
	}
	if !source.Enabled.IsNull() && !source.Enabled.IsUnknown() {
		sdk.SrcLimitingEnabled = source.Enabled.ValueBool()
	}
	if !source.FirewallGroupID.IsNull() {
		sdk.SrcFirewallGroupID = source.FirewallGroupID.ValueString()
	}
	// THE TYPE IS INFERRED WHEN THE PRACTITIONER LEAVES IT OUT, and the
	// inference is the behaviour rather than the assignment: an explicit
	// value wins, a firewall group means firewall_group, anything else
	// means ip.
	switch {
	case !source.Type.IsNull() && !source.Type.IsUnknown():
		sdk.SrcLimitingType = source.Type.ValueString()
	case !source.FirewallGroupID.IsNull():
		sdk.SrcLimitingType = "firewall_group"
	default:
		sdk.SrcLimitingType = "ip"
	}
	return diags
}

func decodePortForwardSourceLimiting(ctx context.Context, sdk *ui.PortForward, _ types.Object) (types.Object, diag.Diagnostics) {
	attrTypes := portForwardSourceLimitingModel{}.AttributeTypes()
	if !portForwardSourceLimitingConfigured(sdk) {
		return types.ObjectNull(attrTypes), nil
	}
	value := portForwardSourceLimitingModel{
		IP:              portForwardStringOrNullValue(sdk.Src),
		FirewallGroupID: portForwardStringOrNullValue(sdk.SrcFirewallGroupID),
		Enabled:         types.BoolValue(sdk.SrcLimitingEnabled),
		Type:            portForwardStringOrNullValue(sdk.SrcLimitingType),
	}
	return types.ObjectValueFrom(ctx, attrTypes, value)
}

// portForwardMemberSet builds the predicate for a wire written only when one
// member of its block carries a value.
//
// A PARTLY FILLED BLOCK IS THE CASE, and it is the one every whole-object check
// misses. `wan { port = "8080" }` sets the block, so SetInPlan is true and all
// three of its wires join the mask -- including the two whose members are null
// and whose Encode therefore left alone. go-unifi sends those zeros over
// whatever the controller holds. The hand-written whole-object write did not:
// pfwd_interface and destination_ip carry omitempty, so an empty one simply was
// not there. The masked update is what put them on the wire, and these
// predicates are what take them off again.
func portForwardMemberSet(member string) func(types.Object) bool {
	return func(object types.Object) bool {
		value, present := object.Attributes()[member]
		return present && !value.IsNull() && !value.IsUnknown()
	}
}

func portForwardStringOrNullValue(s string) types.String {
	if s == "" {
		return types.StringNull()
	}
	return types.StringValue(s)
}

func portForwardKitSpec() resourcekit.Spec[portForwardKitModel, ui.PortForward] {
	return resourcekit.Spec[portForwardKitModel, ui.PortForward]{
		TypeName: "port_forward",
		Subject:  "Port Forward",
		New:      func() *ui.PortForward { return &ui.PortForward{} },
		ID:       func(m *portForwardKitModel) *types.String { return &m.ID },
		Site:     func(m *portForwardKitModel) *types.String { return &m.Site },
		Timeouts: func(m *portForwardKitModel) *timeouts.Value { return &m.Timeouts },
		Fields: []resourcekit.Field[portForwardKitModel, ui.PortForward]{
			resourcekit.StringField[portForwardKitModel, ui.PortForward]{
				Wire:  "name",
				Model: func(m *portForwardKitModel) *types.String { return &m.Name },
				SDK:   func(s *ui.PortForward) *string { return &s.Name },
				Elide: resourcekit.NullZero,
			},
			resourcekit.StringField[portForwardKitModel, ui.PortForward]{
				Wire:  "proto",
				Model: func(m *portForwardKitModel) *types.String { return &m.Protocol },
				SDK:   func(s *ui.PortForward) *string { return &s.Proto },
				Elide: resourcekit.NullZero,
			},
			resourcekit.BoolField[portForwardKitModel, ui.PortForward]{
				Wire:  "log",
				Model: func(m *portForwardKitModel) *types.Bool { return &m.Logging },
				SDK:   func(s *ui.PortForward) *bool { return &s.Log },
			},
			resourcekit.BoolField[portForwardKitModel, ui.PortForward]{
				Wire:  "enabled",
				Model: func(m *portForwardKitModel) *types.Bool { return &m.Enabled },
				SDK:   func(s *ui.PortForward) *bool { return &s.Enabled },
			},
			// THE THREE SCATTERED OBJECTS ARE DECLARED INLINE, and their
			// Encode and Decode are named functions above rather than
			// literals here: the mapping reader parses a Fields entry as a
			// composite literal, so a helper returning one would hide every
			// name it declares -- and every hidden name is an attribute the
			// practitioner sets and the apply silently drops.
			resourcekit.ScatteredObjectField[portForwardKitModel, ui.PortForward]{
				Wires:     []string{"pfwd_interface", "destination_ip", "dst_port"},
				Model:     func(m *portForwardKitModel) *types.Object { return &m.Wan },
				AttrTypes: portForwardWanModel{}.AttributeTypes(),
				Elide:     resourcekit.NullZero,
				ConditionalWires: map[string]func(types.Object) bool{
					"pfwd_interface": portForwardMemberSet("interface"),
					"destination_ip": portForwardMemberSet("ip_address"),
					"dst_port":       portForwardMemberSet("port"),
				},
				Encode: encodePortForwardWan,
				Decode: decodePortForwardWan,
			},
			resourcekit.ScatteredObjectField[portForwardKitModel, ui.PortForward]{
				Wires:     []string{"fwd", "fwd_port"},
				Model:     func(m *portForwardKitModel) *types.Object { return &m.Forward },
				AttrTypes: portForwardForwardModel{}.AttributeTypes(),
				Elide:     resourcekit.NullZero,
				ConditionalWires: map[string]func(types.Object) bool{
					"fwd":      portForwardMemberSet("ip"),
					"fwd_port": portForwardMemberSet("port"),
				},
				Encode: encodePortForwardForward,
				Decode: decodePortForwardForward,
			},
			resourcekit.ScatteredObjectField[portForwardKitModel, ui.PortForward]{
				Wires: []string{
					"src",
					"src_limiting_enabled",
					"src_firewall_group_id",
					"src_limiting_type",
				},
				Model:     func(m *portForwardKitModel) *types.Object { return &m.SourceLimiting },
				AttrTypes: portForwardSourceLimitingModel{}.AttributeTypes(),
				Elide:     resourcekit.NullZero,
				ConditionalWires: map[string]func(types.Object) bool{
					"src":                   portForwardMemberSet("ip"),
					"src_firewall_group_id": portForwardMemberSet("firewall_group_id"),
					"src_limiting_enabled":  portForwardMemberSet("enabled"),
				},
				Encode: encodePortForwardSourceLimiting,
				Decode: decodePortForwardSourceLimiting,
			},
			resourcekit.ObjectListField[portForwardKitModel, ui.PortForward, ui.PortForwardDestinationIPs]{
				Wire:      "destination_ips",
				Model:     func(m *portForwardKitModel) *types.List { return &m.DestinationIPs },
				SDK:       func(s *ui.PortForward) *[]ui.PortForwardDestinationIPs { return &s.DestinationIPs },
				AttrTypes: portForwardDestinationIPModel{}.AttributeTypes(),
				Elide:     resourcekit.NullZero,
				Encode: func(ctx context.Context, object types.Object) (ui.PortForwardDestinationIPs, diag.Diagnostics) {
					var diags diag.Diagnostics
					var element portForwardDestinationIPModel
					diags.Append(object.As(ctx, &element, basetypes.ObjectAsOptions{})...)
					return ui.PortForwardDestinationIPs{
						DestinationIP: element.DestinationIP.ValueString(),
						Interface:     element.Interface.ValueString(),
					}, diags
				},
				Decode: func(ctx context.Context, element ui.PortForwardDestinationIPs) (types.Object, diag.Diagnostics) {
					value := portForwardDestinationIPModel{
						DestinationIP: portForwardStringOrNullValue(element.DestinationIP),
						Interface:     portForwardStringOrNullValue(element.Interface),
					}
					return types.ObjectValueFrom(ctx, portForwardDestinationIPModel{}.AttributeTypes(), value)
				},
			},
		},
		Backend: resourcekit.Backend[ui.PortForward]{
			GetID: func(s *ui.PortForward) string { return s.ID },
			SetID: func(s *ui.PortForward, id string) { s.ID = id },
		},
	}
}

func portForwardKitSchema() resourcekit.SchemaSpec {
	return resourcekit.SchemaSpec{
		Resource: resource_port_forward.PortForwardResourceSchema,
		Timeouts: timeouts.Opts{Create: true, Read: true, Update: true, Delete: true},
	}
}

func portForwardKitList() resourcekit.ListSpec[ui.PortForward] {
	return resourcekit.ListSpec[ui.PortForward]{
		ConfigSchema: listresource_port_forward.PortForwardListResourceSchema,
		DisplayName: func(s *ui.PortForward) string {
			if s.Name != "" {
				return s.Name
			}
			return s.ID
		},
		Filters: map[string]func(*ui.PortForward) string{
			"name": func(s *ui.PortForward) string { return s.Name },
			"enabled": func(s *ui.PortForward) string {
				if s.Enabled {
					return "true"
				}
				return "false"
			},
		},
	}
}

func portForwardKitBackend(client *ui.ApiClient) resourcekit.Backend[ui.PortForward] {
	return resourcekit.Backend[ui.PortForward]{
		Create: func(ctx context.Context, site string, in *ui.PortForward) (*ui.PortForward, error) {
			return client.CreatePortForward(ctx, site, in)
		},
		Read: func(ctx context.Context, site, id string) (*ui.PortForward, error) {
			return client.GetPortForward(ctx, site, id)
		},
		UpdateFields: func(ctx context.Context, site string, in *ui.PortForward, fields ...string) (*ui.PortForward, error) {
			return client.UpdatePortForwardFields(ctx, site, in, fields...)
		},
		Delete: func(ctx context.Context, site, id string) error {
			return client.DeletePortForward(ctx, site, id)
		},
		List: func(ctx context.Context, site string) ([]ui.PortForward, error) {
			return client.ListPortForward(ctx, site)
		},
		GetID: func(s *ui.PortForward) string { return s.ID },
		SetID: func(s *ui.PortForward, id string) { s.ID = id },
	}
}
