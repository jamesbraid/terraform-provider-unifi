package unifi

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
	resource_nat "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_nat"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/resourcekit"
)

// The unifi_nat descriptor is derived from the controller's Nat definition.
// Everything mechanical -- the model, and the plain string/bool/int fields --
// is emitted into nat_descriptor_gen.go. The judgment kept by hand here is:
//
//   - source_filter and destination_filter are one shared shape (natFilterModel)
//     over two distinct SDK structs, so they carry hand encode/decode.
//   - exclude, is_predefined, pppoe_use_base_interface and rule_index are
//     controller-managed (schema-computed); ReadOnly keeps them off the write
//     mask, though a create's full-struct write still carries them.
//   - in_interface and ip_address suppress an empty write: the behaviour
//     artifact records both as EMPTY-REJECTED (the controller refuses a "" on
//     the update PUT) but OMIT-CLEARS, so a rule that carries neither must omit
//     rather than send "".
//
// protocol, source_filter and destination_filter are required on create; that
// requiredness is not declared here but forced by the compiler from the
// behaviour artifact's writes.Nat.required_on_create, cross-checked against the
// SDK struct.

// natFilterValues is the member set both SDK filter types share, converted
// separately since Go cannot reach a struct field generically.
type natFilterValues struct {
	Address          string
	FilterType       string
	FirewallGroupIDs []string
	InvertAddress    bool
	InvertPort       bool
	NetworkConfID    string
	Port             *int64
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
	object, d := types.ObjectValueFrom(ctx, natFilterModel{}.AttributeTypes(), natFilterModel{
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
		Fields: resourcekit.Override(natGenFields(), []resourcekit.Field[natKitModel, ui.Nat]{
			// The controller rejects an empty in_interface on the update PUT
			// (EMPTY-REJECTED) though it tolerates its absence; a source NAT
			// rule carries no inbound interface. Suppress the write when empty
			// so the masked update never re-sends "".
			resourcekit.StringField[natKitModel, ui.Nat]{
				Wire:      "in_interface",
				Model:     func(m *natKitModel) *types.String { return &m.InInterface },
				SDK:       func(s *ui.Nat) *string { return &s.InInterface },
				Elide:     resourcekit.KeepZero,
				WriteWhen: func(m *natKitModel) bool { return !m.InInterface.IsNull() && m.InInterface.ValueString() != "" },
			},
			// Same as in_interface: an empty ip_address is EMPTY-REJECTED on
			// the update PUT, and a MASQUERADE rule has none. Suppress the
			// write when empty.
			resourcekit.StringField[natKitModel, ui.Nat]{
				Wire:      "ip_address",
				Model:     func(m *natKitModel) *types.String { return &m.IPAddress },
				SDK:       func(s *ui.Nat) *string { return &s.IPAddress },
				Elide:     resourcekit.KeepZero,
				WriteWhen: func(m *natKitModel) bool { return !m.IPAddress.IsNull() && m.IPAddress.ValueString() != "" },
			},
			// exclude, is_predefined and pppoe_use_base_interface are
			// controller internals: read-only, so they never join the write
			// mask; a create's full-struct write still sends them as false,
			// which is the value the controller assigns anyway.
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
				AttrTypes: natFilterModel{}.AttributeTypes(),
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
				AttrTypes: natFilterModel{}.AttributeTypes(),
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
		}),
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
