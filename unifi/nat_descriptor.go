package unifi

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
	listresource_nat "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/listresource_nat"
	resource_nat "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_nat"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/resourcekit"
)

type natKitModel struct {
	ID                    types.String   `tfsdk:"id"`
	Site                  types.String   `tfsdk:"site"`
	Description           types.String   `tfsdk:"description"`
	Type                  types.String   `tfsdk:"type"`
	IPVersion             types.String   `tfsdk:"ip_version"`
	Protocol              types.String   `tfsdk:"protocol"`
	Enabled               types.Bool     `tfsdk:"enabled"`
	Logging               types.Bool     `tfsdk:"logging"`
	OutInterface          types.String   `tfsdk:"out_interface"`
	InInterface           types.String   `tfsdk:"in_interface"`
	IPAddress             types.String   `tfsdk:"ip_address"`
	Port                  types.Int64    `tfsdk:"port"`
	SettingPreference     types.String   `tfsdk:"setting_preference"`
	Exclude               types.Bool     `tfsdk:"exclude"`
	IsPredefined          types.Bool     `tfsdk:"is_predefined"`
	PppoeUseBaseInterface types.Bool     `tfsdk:"pppoe_use_base_interface"`
	RuleIndex             types.Int64    `tfsdk:"rule_index"`
	SourceFilter          types.Object   `tfsdk:"source_filter"`
	DestinationFilter     types.Object   `tfsdk:"destination_filter"`
	Timeouts              timeouts.Value `tfsdk:"timeouts"`
}

// natFilterModel is the shape of both source_filter and destination_filter,
// which the SDK carries as two identical-but-distinct structs.
type natFilterModel struct {
	Address          types.String `tfsdk:"address"`
	FilterType       types.String `tfsdk:"filter_type"`
	FirewallGroupIDs types.List   `tfsdk:"firewall_group_ids"`
	InvertAddress    types.Bool   `tfsdk:"invert_address"`
	InvertPort       types.Bool   `tfsdk:"invert_port"`
	NetworkConfID    types.String `tfsdk:"network_conf_id"`
	Port             types.Int64  `tfsdk:"port"`
}

// natFilterValues is the member set both SDK filter types share, converted
// separately since Go can't reach a struct field generically.
type natFilterValues struct {
	Address          string
	FilterType       string
	FirewallGroupIDs []string
	InvertAddress    bool
	InvertPort       bool
	NetworkConfID    string
	Port             *int64
}

func natFilterAttrTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"address":            types.StringType,
		"filter_type":        types.StringType,
		"firewall_group_ids": types.ListType{ElemType: types.StringType},
		"invert_address":     types.BoolType,
		"invert_port":        types.BoolType,
		"network_conf_id":    types.StringType,
		"port":               types.Int64Type,
	}
}

// natFilterValuesFrom reads a filter object. firewall_group_ids is guarded
// against null/unknown, which is what a create leaves it as before the
// controller answers.
func natFilterValuesFrom(ctx context.Context, object types.Object) (natFilterValues, diag.Diagnostics) {
	var diags diag.Diagnostics
	var m natFilterModel
	diags.Append(object.As(ctx, &m, basetypes.ObjectAsOptions{})...)
	if diags.HasError() {
		return natFilterValues{}, diags
	}
	values := natFilterValues{
		Address:       m.Address.ValueString(),
		FilterType:    m.FilterType.ValueString(),
		InvertAddress: m.InvertAddress.ValueBool(),
		InvertPort:    m.InvertPort.ValueBool(),
		NetworkConfID: m.NetworkConfID.ValueString(),
		Port:          m.Port.ValueInt64Pointer(),
	}
	if !m.FirewallGroupIDs.IsNull() && !m.FirewallGroupIDs.IsUnknown() {
		diags.Append(m.FirewallGroupIDs.ElementsAs(ctx, &values.FirewallGroupIDs, false)...)
	}
	return values, diags
}

func natFilterObjectFrom(ctx context.Context, values natFilterValues) (types.Object, diag.Diagnostics) {
	var diags diag.Diagnostics
	groups, d := types.ListValueFrom(ctx, types.StringType, values.FirewallGroupIDs)
	diags.Append(d...)
	object, d := types.ObjectValueFrom(ctx, natFilterAttrTypes(), natFilterModel{
		Address:          types.StringValue(values.Address),
		FilterType:       types.StringValue(values.FilterType),
		FirewallGroupIDs: groups,
		InvertAddress:    types.BoolValue(values.InvertAddress),
		InvertPort:       types.BoolValue(values.InvertPort),
		NetworkConfID:    types.StringValue(values.NetworkConfID),
		Port:             types.Int64PointerValue(values.Port),
	})
	diags.Append(d...)
	return object, diags
}

func natKitSpec() resourcekit.Spec[natKitModel, ui.Nat] {
	return resourcekit.Spec[natKitModel, ui.Nat]{
		TypeName: "nat",
		Subject:  "NAT Rule",
		IDWire:   "_id",
		New:      func() *ui.Nat { return &ui.Nat{} },
		ID:       func(m *natKitModel) *types.String { return &m.ID },
		Site:     func(m *natKitModel) *types.String { return &m.Site },
		Timeouts: func(m *natKitModel) *timeouts.Value { return &m.Timeouts },
		Fields: []resourcekit.Field[natKitModel, ui.Nat]{
			resourcekit.StringField[natKitModel, ui.Nat]{
				Wire:  "description",
				Model: func(m *natKitModel) *types.String { return &m.Description },
				SDK:   func(s *ui.Nat) *string { return &s.Description },
				Elide: resourcekit.KeepZero,
			},
			resourcekit.StringField[natKitModel, ui.Nat]{
				Wire:  "type",
				Model: func(m *natKitModel) *types.String { return &m.Type },
				SDK:   func(s *ui.Nat) *string { return &s.Type },
				Elide: resourcekit.KeepZero,
			},
			// ip_version's zero ("") is not the default ("IPV4") and the derived
			// OneOf rejects it, so an absent value is null, not empty.
			resourcekit.StringField[natKitModel, ui.Nat]{
				Wire:  "ip_version",
				Model: func(m *natKitModel) *types.String { return &m.IPVersion },
				SDK:   func(s *ui.Nat) *string { return &s.Version },
				Elide: resourcekit.NullZero,
			},
			// protocol, source_filter and destination_filter are the three the
			// controller requires on every write yet the SDK tags omitempty: an
			// unset one vanishes from the JSON and the controller answers a 500
			// (a NullPointerException). protocol is modelled here with a default
			// so it always serializes; the two filters below are Required nested
			// objects for the same reason. Same elide reasoning as ip_version.
			resourcekit.StringField[natKitModel, ui.Nat]{
				Wire:  "protocol",
				Model: func(m *natKitModel) *types.String { return &m.Protocol },
				SDK:   func(s *ui.Nat) *string { return &s.Protocol },
				Elide: resourcekit.NullZero,
			},
			resourcekit.BoolField[natKitModel, ui.Nat]{
				Wire:  "enabled",
				Model: func(m *natKitModel) *types.Bool { return &m.Enabled },
				SDK:   func(s *ui.Nat) *bool { return &s.Enabled },
			},
			resourcekit.BoolField[natKitModel, ui.Nat]{
				Wire:  "logging",
				Model: func(m *natKitModel) *types.Bool { return &m.Logging },
				SDK:   func(s *ui.Nat) *bool { return &s.Logging },
			},
			resourcekit.StringField[natKitModel, ui.Nat]{
				Wire:  "out_interface",
				Model: func(m *natKitModel) *types.String { return &m.OutInterface },
				SDK:   func(s *ui.Nat) *string { return &s.OutInterface },
				Elide: resourcekit.KeepZero,
			},
			// Like ip_address below: a create omits in_interface (the SDK's
			// omitempty drops the ""), but the masked update names it and
			// maskedBody then sends "" explicitly, which the controller
			// rejects on the PUT with NatRuleInvalidNetworkConf -- an empty
			// interface is not a valid network reference. A source NAT rule
			// carries no inbound interface, so suppress the write when empty
			// and match what a create sends.
			resourcekit.StringField[natKitModel, ui.Nat]{
				Wire:      "in_interface",
				Model:     func(m *natKitModel) *types.String { return &m.InInterface },
				SDK:       func(s *ui.Nat) *string { return &s.InInterface },
				Elide:     resourcekit.KeepZero,
				WriteWhen: func(m *natKitModel) bool { return !m.InInterface.IsNull() && m.InInterface.ValueString() != "" },
			},
			// The controller rejects an empty ip_address on the update PUT
			// ("Invalid IP Address") though it tolerates its absence on
			// create; a MASQUERADE rule has none, and a read echoes it back
			// as a known "". Suppress the write when empty so the masked
			// update never re-sends "".
			resourcekit.StringField[natKitModel, ui.Nat]{
				Wire:      "ip_address",
				Model:     func(m *natKitModel) *types.String { return &m.IPAddress },
				SDK:       func(s *ui.Nat) *string { return &s.IPAddress },
				Elide:     resourcekit.KeepZero,
				WriteWhen: func(m *natKitModel) bool { return !m.IPAddress.IsNull() && m.IPAddress.ValueString() != "" },
			},
			// The controller's own pattern ([1-9][0-9]{0,4}) rejects a zero, so
			// an unset port is omitted rather than sent as 0.
			resourcekit.Int64PtrField[natKitModel, ui.Nat]{
				Wire:     "port",
				Model:    func(m *natKitModel) *types.Int64 { return &m.Port },
				SDK:      func(s *ui.Nat) **int64 { return &s.Port },
				Elide:    resourcekit.NullZero,
				OmitZero: true,
			},
			resourcekit.StringField[natKitModel, ui.Nat]{
				Wire:  "setting_preference",
				Model: func(m *natKitModel) *types.String { return &m.SettingPreference },
				SDK:   func(s *ui.Nat) *string { return &s.SettingPreference },
				Elide: resourcekit.NullZero,
			},
			// exclude, is_predefined and pppoe_use_base_interface are controller
			// internals: the create adds them as false and a read carries them
			// back. Read-only, so they never join the write mask; the create's
			// full-struct write still sends them as false, which is the value the
			// controller assigns anyway.
			resourcekit.ReadOnly[natKitModel, ui.Nat](
				resourcekit.BoolField[natKitModel, ui.Nat]{
					Wire:  "exclude",
					Model: func(m *natKitModel) *types.Bool { return &m.Exclude },
					SDK:   func(s *ui.Nat) *bool { return &s.Exclude },
				},
			),
			resourcekit.ReadOnly[natKitModel, ui.Nat](
				resourcekit.BoolField[natKitModel, ui.Nat]{
					Wire:  "is_predefined",
					Model: func(m *natKitModel) *types.Bool { return &m.IsPredefined },
					SDK:   func(s *ui.Nat) *bool { return &s.IsPredefined },
				},
			),
			resourcekit.ReadOnly[natKitModel, ui.Nat](
				resourcekit.BoolField[natKitModel, ui.Nat]{
					Wire:  "pppoe_use_base_interface",
					Model: func(m *natKitModel) *types.Bool { return &m.PppoeUseBaseInterface },
					SDK:   func(s *ui.Nat) *bool { return &s.PppoeUseBaseInterface },
				},
			),
			// rule_index is controller-assigned; UniFi ignores a client value.
			resourcekit.ReadOnly[natKitModel, ui.Nat](
				resourcekit.Int64PtrField[natKitModel, ui.Nat]{
					Wire:  "rule_index",
					Model: func(m *natKitModel) *types.Int64 { return &m.RuleIndex },
					SDK:   func(s *ui.Nat) **int64 { return &s.RuleIndex },
				},
			),
			resourcekit.ObjectField[natKitModel, ui.Nat, ui.NatSourceFilter]{
				Wire:      "source_filter",
				Model:     func(m *natKitModel) *types.Object { return &m.SourceFilter },
				SDK:       func(s *ui.Nat) **ui.NatSourceFilter { return &s.SourceFilter },
				AttrTypes: natFilterAttrTypes(),
				Encode: func(ctx context.Context, object types.Object) (*ui.NatSourceFilter, diag.Diagnostics) {
					v, diags := natFilterValuesFrom(ctx, object)
					if diags.HasError() {
						return nil, diags
					}
					return &ui.NatSourceFilter{
						Address:          v.Address,
						FilterType:       v.FilterType,
						FirewallGroupIDs: v.FirewallGroupIDs,
						InvertAddress:    v.InvertAddress,
						InvertPort:       v.InvertPort,
						NetworkConfID:    v.NetworkConfID,
						Port:             v.Port,
					}, diags
				},
				Decode: func(ctx context.Context, sdk *ui.NatSourceFilter) (types.Object, diag.Diagnostics) {
					return natFilterObjectFrom(ctx, natFilterValues{
						Address:          sdk.Address,
						FilterType:       sdk.FilterType,
						FirewallGroupIDs: sdk.FirewallGroupIDs,
						InvertAddress:    sdk.InvertAddress,
						InvertPort:       sdk.InvertPort,
						NetworkConfID:    sdk.NetworkConfID,
						Port:             sdk.Port,
					})
				},
				Elide: resourcekit.KeepZero,
			},
			resourcekit.ObjectField[natKitModel, ui.Nat, ui.NatDestinationFilter]{
				Wire:      "destination_filter",
				Model:     func(m *natKitModel) *types.Object { return &m.DestinationFilter },
				SDK:       func(s *ui.Nat) **ui.NatDestinationFilter { return &s.DestinationFilter },
				AttrTypes: natFilterAttrTypes(),
				Encode: func(ctx context.Context, object types.Object) (*ui.NatDestinationFilter, diag.Diagnostics) {
					v, diags := natFilterValuesFrom(ctx, object)
					if diags.HasError() {
						return nil, diags
					}
					return &ui.NatDestinationFilter{
						Address:          v.Address,
						FilterType:       v.FilterType,
						FirewallGroupIDs: v.FirewallGroupIDs,
						InvertAddress:    v.InvertAddress,
						InvertPort:       v.InvertPort,
						NetworkConfID:    v.NetworkConfID,
						Port:             v.Port,
					}, diags
				},
				Decode: func(ctx context.Context, sdk *ui.NatDestinationFilter) (types.Object, diag.Diagnostics) {
					return natFilterObjectFrom(ctx, natFilterValues{
						Address:          sdk.Address,
						FilterType:       sdk.FilterType,
						FirewallGroupIDs: sdk.FirewallGroupIDs,
						InvertAddress:    sdk.InvertAddress,
						InvertPort:       sdk.InvertPort,
						NetworkConfID:    sdk.NetworkConfID,
						Port:             sdk.Port,
					})
				},
				Elide: resourcekit.KeepZero,
			},
		},
		// Seeded here as well as in natKitBackend, because Configure binds the
		// real Backend and a unit test calling ToModel on an unconfigured spec
		// would otherwise dereference nil.
		Backend: resourcekit.Backend[ui.Nat]{
			GetID: func(s *ui.Nat) string { return s.ID },
			SetID: func(s *ui.Nat, id string) { s.ID = id },
		},
	}
}

func natKitSchema() resourcekit.SchemaSpec {
	return resourcekit.SchemaSpec{
		Resource: resource_nat.NatResourceSchema,
		Timeouts: timeouts.Opts{Create: true, Read: true, Update: true, Delete: true},
	}
}

func natKitList() resourcekit.ListSpec[ui.Nat] {
	return resourcekit.ListSpec[ui.Nat]{
		ConfigSchema: listresource_nat.NatListResourceSchema,
		DisplayName: func(s *ui.Nat) string {
			if s.Description != "" {
				return s.Description
			}
			return s.ID
		},
		Filters: map[string]func(*ui.Nat) string{
			"type":          func(s *ui.Nat) string { return s.Type },
			"out_interface": func(s *ui.Nat) string { return s.OutInterface },
		},
	}
}

func natKitBackend(client *ui.ApiClient) resourcekit.Backend[ui.Nat] {
	return resourcekit.Backend[ui.Nat]{
		Create: func(ctx context.Context, site string, in *ui.Nat) (*ui.Nat, error) {
			return client.CreateNat(ctx, site, in)
		},
		Read: func(ctx context.Context, site, id string) (*ui.Nat, error) {
			return client.GetNat(ctx, site, id)
		},
		UpdateFields: func(
			ctx context.Context, site string, in *ui.Nat, fields ...string,
		) (*ui.Nat, error) {
			return client.UpdateNatFields(ctx, site, in, fields...)
		},
		Delete: func(ctx context.Context, site, id string) error {
			return client.DeleteNat(ctx, site, id)
		},
		List: func(ctx context.Context, site string) ([]ui.Nat, error) {
			return client.ListNat(ctx, site)
		},
		GetID: func(s *ui.Nat) string { return s.ID },
		SetID: func(s *ui.Nat, id string) { s.ID = id },
	}
}
