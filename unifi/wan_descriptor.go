package unifi

import (
	"context"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/listresource_wan"
	resource_wan "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_wan"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/resourcekit"
	"github.com/ubiquiti-community/terraform-provider-unifi/unifi/util"
)

func wanPtr(
	wire string,
	model func(*wanKitModel) *types.String,
	sdk func(*ui.Network) **string,
) resourcekit.StringLikePtrField[wanKitModel, ui.Network, types.String] {
	return resourcekit.StringLikePtrField[wanKitModel, ui.Network, types.String]{
		Wire: wire, Model: model, SDK: sdk,
		New: func(v basetypes.StringValue) types.String { return v },
	}
}

func wanBool(
	wire string,
	model func(*wanKitModel) *types.Bool,
	sdk func(*ui.Network) *bool,
) resourcekit.BoolField[wanKitModel, ui.Network] {
	return resourcekit.BoolField[wanKitModel, ui.Network]{Wire: wire, Model: model, SDK: sdk}
}

// wanMemberSet keys a conditional wire on exactly the member the hand mapper
// guarded it with: a wire whose member is null or unknown is not written by
// Encode, and a mask naming it anyway would send its zero and blank whatever
// the controller holds.
func wanMemberSet(member string) func(types.Object) bool {
	return func(object types.Object) bool {
		value, ok := object.Attributes()[member]
		return ok && !value.IsNull() && !value.IsUnknown()
	}
}

// wanPriorKnown reports whether a nested object held a value before this
// read. An unknown prior is a create in flight, which the hand resource's
// fresh-model read treated the same as no state at all.
func wanPriorKnown(prior types.Object) bool {
	return !prior.IsNull() && !prior.IsUnknown()
}

// wanPriorMembers seeds target from prior when prior is known, mirroring the
// hand networkToModel's merge: a member the controller omits keeps what state
// held rather than resetting to null. Unknown members are resolved to null
// first -- see wanResolveUnknowns.
func wanPriorMembers(
	ctx context.Context,
	diags *diag.Diagnostics,
	prior types.Object,
	target any,
) {
	if !wanPriorKnown(prior) {
		return
	}
	diags.Append(wanResolveUnknowns(ctx, diags, prior).As(ctx, target, basetypes.ObjectAsOptions{})...)
}

// wanResolveUnknowns resolves prior's unknown members to null before a
// Decode merges from it. On create, prior is the PLAN's object, and an
// Optional+Computed member the configuration omits arrives unknown; if the
// controller's response then omits the wire too, the member would leave
// Decode still unknown, and Terraform refuses an unknown after apply. The
// hand networkToModel never saw the case because its create-path seed was a
// fresh, all-null model.
func wanResolveUnknowns(
	ctx context.Context,
	diags *diag.Diagnostics,
	prior types.Object,
) types.Object {
	attrTypes := prior.AttributeTypes(ctx)
	values := prior.Attributes()
	resolved := make(map[string]attr.Value, len(values))
	changed := false
	for name, value := range values {
		if !value.IsUnknown() {
			resolved[name] = value
			continue
		}
		null, ok := wanNullOf(attrTypes[name])
		if !ok {
			// Loud rather than passed through: an unresolved unknown fails
			// the apply anyway, but with a message blaming Terraform.
			diags.AddError("Decoding a WAN object",
				"member "+name+" has a type wanNullOf does not cover, so its unknown "+
					"value cannot be resolved to null")
			return prior
		}
		resolved[name] = null
		changed = true
	}
	if !changed {
		return prior
	}
	object, d := types.ObjectValue(attrTypes, resolved)
	diags.Append(d...)
	if d.HasError() {
		return prior
	}
	return object
}

// wanNullOf builds the null value of one member type. The switch covers
// every member type wan's objects declare; a new kind landing past it is
// reported by wanResolveUnknowns rather than silently left unknown.
func wanNullOf(typ attr.Type) (attr.Value, bool) {
	switch concrete := typ.(type) {
	case basetypes.BoolType:
		return types.BoolNull(), true
	case basetypes.Int64Type:
		return types.Int64Null(), true
	case basetypes.StringType:
		return types.StringNull(), true
	case types.ListType:
		return types.ListNull(concrete.ElemType), true
	}
	return nil, false
}

// wanKitBeforeSend derives the two wires no Field carries. The interface's
// WAN network group (WAN, WAN2, ...) rides every write: hard-coding "WAN"
// collides with a secondary uplink's write and the controller rejects it
// (#334), and HiddenID mirrors the group the way the hand mapper always did.
func wanKitBeforeSend(
	_ context.Context,
	_, effective *wanKitModel,
	_ wanKitModel,
	sdk *ui.Network,
	_ any,
) diag.Diagnostics {
	group := "WAN"
	if v := effective.Networkgroup; !v.IsNull() && !v.IsUnknown() && v.ValueString() != "" {
		group = v.ValueString()
	}
	sdk.WANNetworkGroup = util.Ptr(group)
	sdk.HiddenID = group
	return nil
}

// wanKitAfterReceive defaults the group the way the hand networkToModel did
// when the controller omits it, so updates keep targeting the right
// interface (#334).
func wanKitAfterReceive(
	_ context.Context,
	_ *ui.Network,
	model *wanKitModel,
	_ wanKitModel,
	_ any,
) diag.Diagnostics {
	if model.Networkgroup.IsNull() {
		model.Networkgroup = types.StringValue("WAN")
	}
	return nil
}

func wanKitSpec() resourcekit.Spec[wanKitModel, ui.Network] {
	return resourcekit.Spec[wanKitModel, ui.Network]{
		TypeName: "wan",
		Subject:  "WAN Network",
		New:      func() *ui.Network { return &ui.Network{Purpose: ui.PurposeWAN} },
		ID:       func(m *wanKitModel) *types.String { return &m.ID },
		Site:     func(m *wanKitModel) *types.String { return &m.Site },
		Timeouts: func(m *wanKitModel) *timeouts.Value { return &m.Timeouts },
		// The hand resource imported by name ("name=WAN" or any non-24-hex
		// handle); Spec.Name and Backend.ReadByName keep both forms.
		Name: func(m *wanKitModel) *types.String { return &m.Name },

		BeforeSend:   wanKitBeforeSend,
		AfterReceive: wanKitAfterReceive,

		// purpose is seeded by New, and the group pair is derived by
		// BeforeSend, so nothing in the plan can put them in the mask. The
		// hand wanWireFields masked all three on every write.
		AlwaysWire: []string{"purpose", "wan_networkgroup", "attr_hidden_id"},

		Fields: resourcekit.Override(wanGenFields(), []resourcekit.Field[wanKitModel, ui.Network]{
			wanPtr("name", func(m *wanKitModel) *types.String { return &m.Name },
				func(s *ui.Network) **string { return &s.Name }),
			wanPtr("wan_type", func(m *wanKitModel) *types.String { return &m.Type },
				func(s *ui.Network) **string { return &s.WANType }),
			wanPtr("wan_type_v6", func(m *wanKitModel) *types.String { return &m.TypeV6 },
				func(s *ui.Network) **string { return &s.WANTypeV6 }),
			wanPtr("wan_networkgroup", func(m *wanKitModel) *types.String { return &m.Networkgroup },
				func(s *ui.Network) **string { return &s.WANNetworkGroup }),
			wanPtr("setting_preference",
				func(m *wanKitModel) *types.String { return &m.SettingPreference },
				func(s *ui.Network) **string { return &s.SettingPreference }),
			wanPtr("ipv6_setting_preference",
				func(m *wanKitModel) *types.String { return &m.IPv6SettingPreference },
				func(s *ui.Network) **string { return &s.IPV6SettingPreference }),
			// ReadOnly, and not because the controller owns them: the WAN
			// purpose's encoder drops both wires, so the hand resource's
			// masked writes never carried them either (networkMaskFor
			// filtered them out of every mask), and a mask naming them now
			// would be refused outright by go-unifi. ValidateConfig still
			// warns when a configuration sets one.
			resourcekit.ReadOnly(wanPtr("single_network_lan",
				func(m *wanKitModel) *types.String { return &m.SingleNetworkLAN },
				func(s *ui.Network) **string { return &s.SingleNetworkLan })),
			resourcekit.ReadOnly(wanBool("mac_override_enabled",
				func(m *wanKitModel) *types.Bool { return &m.MACOverrideEnabled },
				func(s *ui.Network) *bool { return &s.MACOverrideEnabled })),
			wanPtr("wan_dslite_remote_host",
				func(m *wanKitModel) *types.String { return &m.WANDsliteRemoteHost },
				func(s *ui.Network) **string { return &s.WANDsliteRemoteHost }),
			resourcekit.ScatteredObjectField[wanKitModel, ui.Network]{
				Wires:     []string{"wan_vlan_enabled", "wan_vlan"},
				Model:     func(m *wanKitModel) *types.Object { return &m.VLAN },
				AttrTypes: vlanModel{}.AttributeTypes(),
				ConditionalWires: map[string]func(types.Object) bool{
					"wan_vlan_enabled": wanMemberSet("enabled"),
					"wan_vlan":         wanMemberSet("id"),
				},
				Encode: encodeWANVLAN,
				Decode: decodeWANVLAN,
				Elide:  resourcekit.KeepZero,
			},
			resourcekit.ScatteredObjectField[wanKitModel, ui.Network]{
				Wires:     []string{"wan_egress_qos_enabled", "wan_egress_qos"},
				Model:     func(m *wanKitModel) *types.Object { return &m.EgressQoS },
				AttrTypes: egressQosModel{}.AttributeTypes(),
				ConditionalWires: map[string]func(types.Object) bool{
					"wan_egress_qos_enabled": wanMemberSet("enabled"),
					"wan_egress_qos":         wanMemberSet("priority"),
				},
				Encode: encodeWANEgressQOS,
				Decode: decodeWANEgressQOS,
				Elide:  resourcekit.KeepZero,
			},
			resourcekit.ScatteredObjectField[wanKitModel, ui.Network]{
				Wires: []string{
					"wan_dns1", "wan_dns2", "wan_ipv6_dns1", "wan_ipv6_dns2",
					"wan_dns_preference", "wan_ipv6_dns_preference",
				},
				Model:     func(m *wanKitModel) *types.Object { return &m.DNS },
				AttrTypes: dnsModel{}.AttributeTypes(),
				ConditionalWires: map[string]func(types.Object) bool{
					"wan_dns1":                wanMemberSet("primary"),
					"wan_dns2":                wanMemberSet("secondary"),
					"wan_ipv6_dns1":           wanMemberSet("ipv6_primary"),
					"wan_ipv6_dns2":           wanMemberSet("ipv6_secondary"),
					"wan_dns_preference":      wanMemberSet("preference"),
					"wan_ipv6_dns_preference": wanMemberSet("ipv6_preference"),
				},
				Encode: encodeWANDNS,
				Decode: decodeWANDNS,
				Elide:  resourcekit.KeepZero,
			},
			resourcekit.ScatteredObjectField[wanKitModel, ui.Network]{
				Wires:     []string{"wan_dhcp_cos", "wan_dhcp_options"},
				Model:     func(m *wanKitModel) *types.Object { return &m.DHCP },
				AttrTypes: dhcpWanModel{}.AttributeTypes(),
				ConditionalWires: map[string]func(types.Object) bool{
					"wan_dhcp_cos":     wanMemberSet("cos"),
					"wan_dhcp_options": wanMemberSet("options"),
				},
				Encode: encodeWANDHCP,
				Decode: decodeWANDHCP,
				Elide:  resourcekit.KeepZero,
			},
			resourcekit.ScatteredObjectField[wanKitModel, ui.Network]{
				Wires: []string{
					"wan_dhcpv6_cos", "wan_dhcpv6_pd_size", "wan_dhcpv6_pd_size_auto",
					"wan_dhcpv6_options", "ipv6_wan_delegation_type",
				},
				Model:     func(m *wanKitModel) *types.Object { return &m.DHCPv6 },
				AttrTypes: dhcpv6WanModel{}.AttributeTypes(),
				ConditionalWires: map[string]func(types.Object) bool{
					"wan_dhcpv6_cos":           wanMemberSet("cos"),
					"wan_dhcpv6_pd_size":       wanMemberSet("pd_size"),
					"wan_dhcpv6_pd_size_auto":  wanMemberSet("pd_size_auto"),
					"wan_dhcpv6_options":       wanMemberSet("options"),
					"ipv6_wan_delegation_type": wanMemberSet("wan_delegation_type"),
				},
				Encode: encodeWANDHCPv6,
				Decode: decodeWANDHCPv6,
				Elide:  resourcekit.KeepZero,
			},
			resourcekit.ScatteredObjectField[wanKitModel, ui.Network]{
				Wires:     []string{"wan_smartq_enabled", "wan_smartq_up_rate", "wan_smartq_down_rate"},
				Model:     func(m *wanKitModel) *types.Object { return &m.Smartq },
				AttrTypes: smartqModel{}.AttributeTypes(),
				ConditionalWires: map[string]func(types.Object) bool{
					"wan_smartq_enabled":   wanMemberSet("enabled"),
					"wan_smartq_up_rate":   wanMemberSet("up_rate"),
					"wan_smartq_down_rate": wanMemberSet("down_rate"),
				},
				Encode: encodeWANSmartQ,
				Decode: decodeWANSmartQ,
				Elide:  resourcekit.KeepZero,
			},
			resourcekit.ScatteredObjectField[wanKitModel, ui.Network]{
				Wires: []string{
					"upnp_enabled", "upnp_wan_interface", "upnp_nat_pmp_enabled", "upnp_secure_mode",
				},
				Model:     func(m *wanKitModel) *types.Object { return &m.UPnP },
				AttrTypes: upnpModel{}.AttributeTypes(),
				ConditionalWires: map[string]func(types.Object) bool{
					"upnp_enabled":         wanMemberSet("enabled"),
					"upnp_wan_interface":   wanMemberSet("wan_interface"),
					"upnp_nat_pmp_enabled": wanMemberSet("nat_pmp_enabled"),
					"upnp_secure_mode":     wanMemberSet("secure_mode"),
				},
				Encode: encodeWANUPnP,
				Decode: decodeWANUPnP,
				Elide:  resourcekit.KeepZero,
			},
			resourcekit.ScatteredObjectField[wanKitModel, ui.Network]{
				Wires: []string{
					"wan_load_balance_type", "wan_load_balance_weight", "wan_failover_priority",
				},
				Model:     func(m *wanKitModel) *types.Object { return &m.LoadBalance },
				AttrTypes: loadBalanceModel{}.AttributeTypes(),
				ConditionalWires: map[string]func(types.Object) bool{
					"wan_load_balance_type":   wanMemberSet("type"),
					"wan_load_balance_weight": wanMemberSet("weight"),
					"wan_failover_priority":   wanMemberSet("failover_priority"),
				},
				Encode: encodeWANLoadBalance,
				Decode: decodeWANLoadBalance,
				Elide:  resourcekit.KeepZero,
			},
			resourcekit.ScatteredObjectField[wanKitModel, ui.Network]{
				Wires:     []string{"igmp_proxy_for", "igmp_proxy_upstream"},
				Model:     func(m *wanKitModel) *types.Object { return &m.IGMPProxy },
				AttrTypes: igmpProxyModel{}.AttributeTypes(),
				ConditionalWires: map[string]func(types.Object) bool{
					"igmp_proxy_for":      wanMemberSet("downstream"),
					"igmp_proxy_upstream": wanMemberSet("upstream"),
				},
				Encode: encodeWANIGMPProxy,
				Decode: decodeWANIGMPProxy,
				Elide:  resourcekit.KeepZero,
			},
			// One wire over one real nested struct: ObjectField could carry it,
			// but only a scattered Decode sees prior, and the hand resource
			// preserved state when the API reported nothing.
			resourcekit.ScatteredObjectField[wanKitModel, ui.Network]{
				Wires:     []string{"wan_provider_capabilities"},
				Model:     func(m *wanKitModel) *types.Object { return &m.ProviderCapabilities },
				AttrTypes: providerCapabilitiesModel{}.AttributeTypes(),
				Encode:    encodeWANProviderCapabilities,
				Decode:    decodeWANProviderCapabilities,
				Elide:     resourcekit.KeepZero,
			},
		}),

		// Seeded so ToModel doesn't nil-dereference: Configure replaces the
		// whole Backend, so a test binary that never calls it would panic
		// here otherwise.
		Backend: resourcekit.Backend[ui.Network]{
			GetID: func(s *ui.Network) string { return s.ID },
			SetID: func(s *ui.Network, id string) { s.ID = id },
		},
	}
}

func wanKitSchema() resourcekit.SchemaSpec {
	return resourcekit.SchemaSpec{
		Resource: resource_wan.WanResourceSchema,
		Timeouts: timeouts.Opts{Create: true, Read: true, Update: true, Delete: true},
	}
}

func wanKitList() resourcekit.ListSpec[ui.Network] {
	return resourcekit.ListSpec[ui.Network]{
		ConfigSchema: listresource_wan.WanListResourceSchema,
		DisplayName: func(s *ui.Network) string {
			if s.Name != nil && *s.Name != "" {
				return *s.Name
			}
			return s.ID
		},
		Filters: map[string]func(*ui.Network) string{
			"name": func(s *ui.Network) string {
				if s.Name == nil {
					return ""
				}
				return *s.Name
			},
		},
	}
}

func wanKitBackend(client *ui.ApiClient) resourcekit.Backend[ui.Network] {
	return resourcekit.Backend[ui.Network]{
		// Create adopts on conflict, which is what makes it the masked
		// CreateFields: the POST still sends the whole object, and the mask
		// only matters on the adopt path, where it keeps the overlay from
		// asserting values the plan never set over an existing
		// controller-configured WAN (the hand adoptExistingWAN masked its
		// overlay for the same reason).
		CreateFields: func(
			ctx context.Context, site string, in *ui.Network, fields ...string,
		) (*ui.Network, error) {
			created, err := client.CreateNetwork(ctx, site, in)
			if err == nil {
				return created, nil
			}
			if !strings.Contains(err.Error(), "WanConfigurationForNetworkGroupAlreadyExists") {
				return nil, err
			}
			return adoptExistingWAN(ctx, client, site, in, fields)
		},
		Read: func(ctx context.Context, site, id string) (*ui.Network, error) {
			return client.GetNetwork(ctx, site, id)
		},
		ReadByName: func(ctx context.Context, site, name string) (*ui.Network, error) {
			return client.GetNetworkByName(ctx, site, name)
		},
		UpdateFields: func(
			ctx context.Context, site string, in *ui.Network, fields ...string,
		) (*ui.Network, error) {
			return client.UpdateNetworkFields(ctx, site, in, fields...)
		},
		// Delete's body carries the name, which the kit's Delete signature
		// doesn't pass, so this reads the object for it. A controller refuses
		// to delete a WAN outright ("NoDelete"); the hand resource dropped it
		// from state and left the controller alone, and so does this.
		Delete: func(ctx context.Context, site, id string) error {
			existing, err := client.GetNetwork(ctx, site, id)
			if err != nil {
				return err
			}
			name := ""
			if existing.Name != nil {
				name = *existing.Name
			}
			err = client.DeleteNetwork(ctx, site, id, name)
			if err != nil && strings.Contains(err.Error(), "NoDelete") {
				return nil
			}
			return err
		},
		// One networkconf endpoint serves all seven purposes; without the
		// filter a WAN list would return corporate LANs as well. The group
		// check matches the hand List, which skipped a WAN-purposed document
		// with no interface behind it.
		List: func(ctx context.Context, site string) ([]ui.Network, error) {
			all, err := client.ListNetwork(ctx, site)
			if err != nil {
				return nil, err
			}
			out := make([]ui.Network, 0, len(all))
			for _, n := range all {
				if n.Purpose == ui.PurposeWAN && n.WANNetworkGroup != nil {
					out = append(out, n)
				}
			}
			return out, nil
		},
		GetID: func(s *ui.Network) string { return s.ID },
		SetID: func(s *ui.Network, id string) { s.ID = id },
	}
}

// adoptExistingWAN finds the WAN owning the same network group we tried to
// create (WAN, WAN2, ...) and overlays the planned configuration onto it with
// a masked write -- not just the primary "WAN", or a WAN2 conflict adopts the
// wrong interface (#334). Transcribed from the hand resource's
// adoptExistingWAN; the mask is the plan's own rather than the hand code's
// everything-the-object-encodes list, so an attribute the plan never set
// leaves the adopted WAN's live value alone.
func adoptExistingWAN(
	ctx context.Context,
	client *ui.ApiClient,
	site string,
	network *ui.Network,
	fields []string,
) (*ui.Network, error) {
	networks, err := client.ListNetwork(ctx, site)
	if err != nil {
		return nil, err
	}
	wantGroup := "WAN"
	if network.WANNetworkGroup != nil && *network.WANNetworkGroup != "" {
		wantGroup = *network.WANNetworkGroup
	}
	var existing *ui.Network
	for _, n := range networks {
		if n.Purpose == ui.PurposeWAN && n.WANNetworkGroup != nil &&
			*n.WANNetworkGroup == wantGroup {
			existing = &n
			break
		}
	}
	if existing == nil {
		return nil, errNoAdoptableWAN(wantGroup)
	}
	network.ID = existing.ID
	return client.UpdateNetworkFields(ctx, site, network, fields...)
}

// errNoAdoptableWAN keeps the hand resource's message: a creation conflict
// with nothing to adopt is a controller inconsistency worth naming.
func errNoAdoptableWAN(group string) error {
	return &noAdoptableWANError{group: group}
}

type noAdoptableWANError struct{ group string }

func (e *noAdoptableWANError) Error() string {
	return "existing WAN network (group " + e.group + ") not found despite creation conflict"
}

// dnsAddrValue maps a controller WAN DNS address pointer to a Terraform
// value, treating a nil pointer and an empty string alike as null -- the
// controller returns "" for this unconfigured Optional field, which would
// otherwise conflict with a planned null (#333).
func dnsAddrValue(p *string) types.String {
	if p == nil || *p == "" {
		return types.StringNull()
	}
	return types.StringValue(*p)
}

func encodeWANVLAN(ctx context.Context, object types.Object, sdk *ui.Network) diag.Diagnostics {
	var vlan vlanModel
	diags := object.As(ctx, &vlan, basetypes.ObjectAsOptions{})
	if diags.HasError() {
		return diags
	}
	if !vlan.Enabled.IsNull() && !vlan.Enabled.IsUnknown() {
		sdk.WANVLANEnabled = vlan.Enabled.ValueBool()
	}
	if !vlan.ID.IsNull() && !vlan.ID.IsUnknown() {
		sdk.WANVLAN = vlan.ID.ValueInt64Pointer()
	}
	return diags
}

// decodeWANVLAN always builds the object: the controller omits the VLAN id
// when unset, and mapping that to the schema default (0) rather than null is
// what lets an imported WAN plan clean without an apply (#262).
func decodeWANVLAN(
	ctx context.Context,
	sdk *ui.Network,
	_ types.Object,
) (types.Object, diag.Diagnostics) {
	var diags diag.Diagnostics
	id := int64(0)
	if sdk.WANVLAN != nil {
		id = *sdk.WANVLAN
	}
	value := vlanModel{
		Enabled: types.BoolValue(sdk.WANVLANEnabled),
		ID:      types.Int64Value(id),
	}
	object, d := types.ObjectValueFrom(ctx, value.AttributeTypes(), value)
	diags.Append(d...)
	return object, diags
}

func encodeWANEgressQOS(ctx context.Context, object types.Object, sdk *ui.Network) diag.Diagnostics {
	var egress egressQosModel
	diags := object.As(ctx, &egress, basetypes.ObjectAsOptions{})
	if diags.HasError() {
		return diags
	}
	if !egress.Enabled.IsNull() && !egress.Enabled.IsUnknown() {
		sdk.WANEgressQOSEnabled = egress.Enabled.ValueBoolPointer()
	}
	if !egress.Priority.IsNull() && !egress.Priority.IsUnknown() {
		sdk.WANEgressQOS = egress.Priority.ValueInt64Pointer()
	}
	return diags
}

func decodeWANEgressQOS(
	ctx context.Context,
	sdk *ui.Network,
	prior types.Object,
) (types.Object, diag.Diagnostics) {
	var diags diag.Diagnostics
	hasData := sdk.WANEgressQOSEnabled != nil || sdk.WANEgressQOS != nil
	if !wanPriorKnown(prior) && !hasData {
		return types.ObjectNull(egressQosModel{}.AttributeTypes()), diags
	}
	var current egressQosModel
	wanPriorMembers(ctx, &diags, prior, &current)
	if sdk.WANEgressQOSEnabled != nil {
		current.Enabled = types.BoolValue(*sdk.WANEgressQOSEnabled)
	}
	if sdk.WANEgressQOS != nil {
		current.Priority = types.Int64Value(*sdk.WANEgressQOS)
	}
	object, d := types.ObjectValueFrom(ctx, current.AttributeTypes(), current)
	diags.Append(d...)
	return object, diags
}

func encodeWANDNS(ctx context.Context, object types.Object, sdk *ui.Network) diag.Diagnostics {
	var dns dnsModel
	diags := object.As(ctx, &dns, basetypes.ObjectAsOptions{})
	if diags.HasError() {
		return diags
	}
	if !dns.Primary.IsNull() && !dns.Primary.IsUnknown() {
		sdk.WANDNS1 = dns.Primary.ValueStringPointer()
	}
	if !dns.Secondary.IsNull() && !dns.Secondary.IsUnknown() {
		sdk.WANDNS2 = dns.Secondary.ValueStringPointer()
	}
	if !dns.IPv6Primary.IsNull() && !dns.IPv6Primary.IsUnknown() {
		sdk.WANIPV6DNS1 = dns.IPv6Primary.ValueStringPointer()
	}
	if !dns.IPv6Secondary.IsNull() && !dns.IPv6Secondary.IsUnknown() {
		sdk.WANIPV6DNS2 = dns.IPv6Secondary.ValueStringPointer()
	}
	if !dns.Preference.IsNull() && !dns.Preference.IsUnknown() {
		sdk.WANDNSPreference = dns.Preference.ValueStringPointer()
	}
	if !dns.IPv6Preference.IsNull() && !dns.IPv6Preference.IsUnknown() {
		sdk.WANIPV6DNSPreference = dns.IPv6Preference.ValueStringPointer()
	}
	return diags
}

func decodeWANDNS(
	ctx context.Context,
	sdk *ui.Network,
	prior types.Object,
) (types.Object, diag.Diagnostics) {
	var diags diag.Diagnostics
	hasData := sdk.WANDNS1 != nil || sdk.WANDNS2 != nil ||
		sdk.WANIPV6DNS1 != nil || sdk.WANIPV6DNS2 != nil ||
		sdk.WANDNSPreference != nil || sdk.WANIPV6DNSPreference != nil
	if !wanPriorKnown(prior) && !hasData {
		return types.ObjectNull(dnsModel{}.AttributeTypes()), diags
	}
	var current dnsModel
	wanPriorMembers(ctx, &diags, prior, &current)
	if sdk.WANDNS1 != nil {
		current.Primary = dnsAddrValue(sdk.WANDNS1)
	}
	if sdk.WANDNS2 != nil {
		current.Secondary = dnsAddrValue(sdk.WANDNS2)
	}
	if sdk.WANIPV6DNS1 != nil {
		current.IPv6Primary = dnsAddrValue(sdk.WANIPV6DNS1)
	}
	if sdk.WANIPV6DNS2 != nil {
		current.IPv6Secondary = dnsAddrValue(sdk.WANIPV6DNS2)
	}
	if sdk.WANDNSPreference != nil {
		current.Preference = types.StringValue(*sdk.WANDNSPreference)
	}
	if sdk.WANIPV6DNSPreference != nil {
		current.IPv6Preference = types.StringValue(*sdk.WANIPV6DNSPreference)
	}
	object, d := types.ObjectValueFrom(ctx, current.AttributeTypes(), current)
	diags.Append(d...)
	return object, diags
}

func encodeWANDHCP(ctx context.Context, object types.Object, sdk *ui.Network) diag.Diagnostics {
	var dhcp dhcpWanModel
	diags := object.As(ctx, &dhcp, basetypes.ObjectAsOptions{})
	if diags.HasError() {
		return diags
	}
	if !dhcp.CoS.IsNull() && !dhcp.CoS.IsUnknown() {
		sdk.WANDHCPCos = dhcp.CoS.ValueInt64Pointer()
	}
	if !dhcp.Options.IsNull() && !dhcp.Options.IsUnknown() {
		var options []dhcpOptionModel
		diags.Append(dhcp.Options.ElementsAs(ctx, &options, false)...)
		if diags.HasError() {
			return diags
		}
		sdk.WANDHCPOptions = make([]ui.NetworkWANDHCPOptions, len(options))
		for i, opt := range options {
			sdk.WANDHCPOptions[i] = ui.NetworkWANDHCPOptions{
				OptionNumber: opt.OptionNumber.ValueInt64Pointer(),
				Value:        opt.Value.ValueStringPointer(),
			}
		}
	}
	return diags
}

func decodeWANDHCP(
	ctx context.Context,
	sdk *ui.Network,
	prior types.Object,
) (types.Object, diag.Diagnostics) {
	var diags diag.Diagnostics
	hasData := sdk.WANDHCPCos != nil || len(sdk.WANDHCPOptions) > 0
	if !wanPriorKnown(prior) && !hasData {
		return types.ObjectNull(dhcpWanModel{}.AttributeTypes()), diags
	}
	var current dhcpWanModel
	wanPriorMembers(ctx, &diags, prior, &current)
	if sdk.WANDHCPCos != nil {
		current.CoS = types.Int64Value(*sdk.WANDHCPCos)
	}
	optionType := types.ObjectType{AttrTypes: dhcpOptionModel{}.AttributeTypes()}
	if len(sdk.WANDHCPOptions) > 0 {
		values := make([]attr.Value, len(sdk.WANDHCPOptions))
		for i, opt := range sdk.WANDHCPOptions {
			value, d := types.ObjectValue(dhcpOptionModel{}.AttributeTypes(), map[string]attr.Value{
				"option_number": types.Int64PointerValue(opt.OptionNumber),
				"value":         types.StringPointerValue(opt.Value),
			})
			diags.Append(d...)
			values[i] = value
		}
		list, d := types.ListValue(optionType, values)
		diags.Append(d...)
		current.Options = list
	} else if current.Options.IsNull() || current.Options.IsUnknown() {
		current.Options = types.ListNull(optionType)
	}
	object, d := types.ObjectValueFrom(ctx, current.AttributeTypes(), current)
	diags.Append(d...)
	return object, diags
}

func encodeWANDHCPv6(ctx context.Context, object types.Object, sdk *ui.Network) diag.Diagnostics {
	var dhcpv6 dhcpv6WanModel
	diags := object.As(ctx, &dhcpv6, basetypes.ObjectAsOptions{})
	if diags.HasError() {
		return diags
	}
	if !dhcpv6.CoS.IsNull() && !dhcpv6.CoS.IsUnknown() {
		sdk.WANDHCPv6Cos = dhcpv6.CoS.ValueInt64Pointer()
	}
	if !dhcpv6.PDSize.IsNull() && !dhcpv6.PDSize.IsUnknown() {
		sdk.WANDHCPv6PDSize = dhcpv6.PDSize.ValueInt64Pointer()
	}
	if !dhcpv6.PDSizeAuto.IsNull() && !dhcpv6.PDSizeAuto.IsUnknown() {
		sdk.WANDHCPv6PDSizeAuto = dhcpv6.PDSizeAuto.ValueBool()
	}
	if !dhcpv6.DelegationType.IsNull() && !dhcpv6.DelegationType.IsUnknown() {
		sdk.IPV6WANDelegationType = dhcpv6.DelegationType.ValueStringPointer()
	}
	if !dhcpv6.Options.IsNull() && !dhcpv6.Options.IsUnknown() {
		var options []dhcpOptionModel
		diags.Append(dhcpv6.Options.ElementsAs(ctx, &options, false)...)
		if diags.HasError() {
			return diags
		}
		sdk.WANDHCPv6Options = make([]ui.NetworkWANDHCPv6Options, len(options))
		for i, opt := range options {
			sdk.WANDHCPv6Options[i] = ui.NetworkWANDHCPv6Options{
				OptionNumber: opt.OptionNumber.ValueInt64Pointer(),
				Value:        opt.Value.ValueStringPointer(),
			}
		}
	}
	return diags
}

func decodeWANDHCPv6(
	ctx context.Context,
	sdk *ui.Network,
	prior types.Object,
) (types.Object, diag.Diagnostics) {
	var diags diag.Diagnostics
	hasData := sdk.WANDHCPv6Cos != nil || sdk.WANDHCPv6PDSize != nil ||
		sdk.IPV6WANDelegationType != nil || len(sdk.WANDHCPv6Options) > 0
	if !wanPriorKnown(prior) && !hasData {
		return types.ObjectNull(dhcpv6WanModel{}.AttributeTypes()), diags
	}
	var current dhcpv6WanModel
	wanPriorMembers(ctx, &diags, prior, &current)
	if sdk.WANDHCPv6Cos != nil {
		current.CoS = types.Int64Value(*sdk.WANDHCPv6Cos)
	}
	if sdk.WANDHCPv6PDSize != nil {
		current.PDSize = types.Int64Value(*sdk.WANDHCPv6PDSize)
	}
	current.PDSizeAuto = types.BoolValue(sdk.WANDHCPv6PDSizeAuto)
	if sdk.IPV6WANDelegationType != nil {
		current.DelegationType = types.StringValue(*sdk.IPV6WANDelegationType)
	}
	optionType := types.ObjectType{AttrTypes: dhcpOptionModel{}.AttributeTypes()}
	if len(sdk.WANDHCPv6Options) > 0 {
		values := make([]attr.Value, len(sdk.WANDHCPv6Options))
		for i, opt := range sdk.WANDHCPv6Options {
			value, d := types.ObjectValue(dhcpOptionModel{}.AttributeTypes(), map[string]attr.Value{
				"option_number": types.Int64PointerValue(opt.OptionNumber),
				"value":         types.StringPointerValue(opt.Value),
			})
			diags.Append(d...)
			values[i] = value
		}
		list, d := types.ListValue(optionType, values)
		diags.Append(d...)
		current.Options = list
	} else if current.Options.IsNull() || current.Options.IsUnknown() {
		current.Options = types.ListNull(optionType)
	}
	object, d := types.ObjectValueFrom(ctx, current.AttributeTypes(), current)
	diags.Append(d...)
	return object, diags
}

func encodeWANSmartQ(ctx context.Context, object types.Object, sdk *ui.Network) diag.Diagnostics {
	var smartq smartqModel
	diags := object.As(ctx, &smartq, basetypes.ObjectAsOptions{})
	if diags.HasError() {
		return diags
	}
	if !smartq.Enabled.IsNull() && !smartq.Enabled.IsUnknown() {
		sdk.WANSmartQEnabled = smartq.Enabled.ValueBool()
	}
	if !smartq.UpRate.IsNull() && !smartq.UpRate.IsUnknown() {
		sdk.WANSmartQUpRate = smartq.UpRate.ValueInt64Pointer()
	}
	if !smartq.DownRate.IsNull() && !smartq.DownRate.IsUnknown() {
		sdk.WANSmartQDownRate = smartq.DownRate.ValueInt64Pointer()
	}
	return diags
}

func decodeWANSmartQ(
	ctx context.Context,
	sdk *ui.Network,
	prior types.Object,
) (types.Object, diag.Diagnostics) {
	var diags diag.Diagnostics
	hasData := sdk.WANSmartQEnabled || sdk.WANSmartQUpRate != nil || sdk.WANSmartQDownRate != nil
	if !wanPriorKnown(prior) && !hasData {
		return types.ObjectNull(smartqModel{}.AttributeTypes()), diags
	}
	var current smartqModel
	wanPriorMembers(ctx, &diags, prior, &current)
	if hasData {
		current.Enabled = types.BoolValue(sdk.WANSmartQEnabled)
		if sdk.WANSmartQUpRate != nil {
			current.UpRate = types.Int64Value(*sdk.WANSmartQUpRate)
		}
		if sdk.WANSmartQDownRate != nil {
			current.DownRate = types.Int64Value(*sdk.WANSmartQDownRate)
		}
	}
	object, d := types.ObjectValueFrom(ctx, current.AttributeTypes(), current)
	diags.Append(d...)
	return object, diags
}

func encodeWANUPnP(ctx context.Context, object types.Object, sdk *ui.Network) diag.Diagnostics {
	var upnp upnpModel
	diags := object.As(ctx, &upnp, basetypes.ObjectAsOptions{})
	if diags.HasError() {
		return diags
	}
	if !upnp.Enabled.IsNull() && !upnp.Enabled.IsUnknown() {
		sdk.UPnPEnabled = upnp.Enabled.ValueBoolPointer()
	}
	if !upnp.WANInterface.IsNull() && !upnp.WANInterface.IsUnknown() {
		sdk.UPnPWANInterface = upnp.WANInterface.ValueStringPointer()
	}
	if !upnp.NatPMPEnabled.IsNull() && !upnp.NatPMPEnabled.IsUnknown() {
		sdk.UPnPNatPMPEnabled = upnp.NatPMPEnabled.ValueBoolPointer()
	}
	if !upnp.SecureMode.IsNull() && !upnp.SecureMode.IsUnknown() {
		sdk.UPnPSecureMode = upnp.SecureMode.ValueBoolPointer()
	}
	return diags
}

func decodeWANUPnP(
	ctx context.Context,
	sdk *ui.Network,
	prior types.Object,
) (types.Object, diag.Diagnostics) {
	var diags diag.Diagnostics
	hasData := sdk.UPnPEnabled != nil || sdk.UPnPWANInterface != nil ||
		sdk.UPnPNatPMPEnabled != nil || sdk.UPnPSecureMode != nil
	if !wanPriorKnown(prior) && !hasData {
		return types.ObjectNull(upnpModel{}.AttributeTypes()), diags
	}
	var current upnpModel
	wanPriorMembers(ctx, &diags, prior, &current)
	if sdk.UPnPEnabled != nil {
		current.Enabled = types.BoolValue(*sdk.UPnPEnabled)
	}
	if sdk.UPnPWANInterface != nil {
		current.WANInterface = types.StringValue(*sdk.UPnPWANInterface)
	}
	if sdk.UPnPNatPMPEnabled != nil {
		current.NatPMPEnabled = types.BoolValue(*sdk.UPnPNatPMPEnabled)
	}
	if sdk.UPnPSecureMode != nil {
		current.SecureMode = types.BoolValue(*sdk.UPnPSecureMode)
	}
	object, d := types.ObjectValueFrom(ctx, current.AttributeTypes(), current)
	diags.Append(d...)
	return object, diags
}

func encodeWANLoadBalance(ctx context.Context, object types.Object, sdk *ui.Network) diag.Diagnostics {
	var balance loadBalanceModel
	diags := object.As(ctx, &balance, basetypes.ObjectAsOptions{})
	if diags.HasError() {
		return diags
	}
	if !balance.Type.IsNull() && !balance.Type.IsUnknown() {
		sdk.WANLoadBalanceType = balance.Type.ValueStringPointer()
	}
	if !balance.Weight.IsNull() && !balance.Weight.IsUnknown() {
		sdk.WANLoadBalanceWeight = balance.Weight.ValueInt64Pointer()
	}
	if !balance.FailoverPriority.IsNull() && !balance.FailoverPriority.IsUnknown() {
		sdk.WANFailoverPriority = balance.FailoverPriority.ValueInt64Pointer()
	}
	return diags
}

func decodeWANLoadBalance(
	ctx context.Context,
	sdk *ui.Network,
	prior types.Object,
) (types.Object, diag.Diagnostics) {
	var diags diag.Diagnostics
	hasData := sdk.WANLoadBalanceType != nil || sdk.WANLoadBalanceWeight != nil ||
		sdk.WANFailoverPriority != nil
	if !wanPriorKnown(prior) && !hasData {
		return types.ObjectNull(loadBalanceModel{}.AttributeTypes()), diags
	}
	var current loadBalanceModel
	wanPriorMembers(ctx, &diags, prior, &current)
	if sdk.WANLoadBalanceType != nil {
		current.Type = types.StringValue(*sdk.WANLoadBalanceType)
	}
	if sdk.WANLoadBalanceWeight != nil {
		current.Weight = types.Int64Value(*sdk.WANLoadBalanceWeight)
	}
	if sdk.WANFailoverPriority != nil {
		current.FailoverPriority = types.Int64Value(*sdk.WANFailoverPriority)
	}
	object, d := types.ObjectValueFrom(ctx, current.AttributeTypes(), current)
	diags.Append(d...)
	return object, diags
}

func encodeWANIGMPProxy(ctx context.Context, object types.Object, sdk *ui.Network) diag.Diagnostics {
	var igmp igmpProxyModel
	diags := object.As(ctx, &igmp, basetypes.ObjectAsOptions{})
	if diags.HasError() {
		return diags
	}
	if !igmp.Downstream.IsNull() && !igmp.Downstream.IsUnknown() {
		sdk.IGMPProxyFor = igmp.Downstream.ValueStringPointer()
	}
	if !igmp.Upstream.IsNull() && !igmp.Upstream.IsUnknown() {
		sdk.IGMPProxyUpstream = igmp.Upstream.ValueBool()
	}
	return diags
}

func decodeWANIGMPProxy(
	ctx context.Context,
	sdk *ui.Network,
	prior types.Object,
) (types.Object, diag.Diagnostics) {
	var diags diag.Diagnostics
	hasData := sdk.IGMPProxyFor != nil || sdk.IGMPProxyUpstream
	if !wanPriorKnown(prior) && !hasData {
		return types.ObjectNull(igmpProxyModel{}.AttributeTypes()), diags
	}
	var current igmpProxyModel
	wanPriorMembers(ctx, &diags, prior, &current)
	if sdk.IGMPProxyFor != nil {
		current.Downstream = types.StringValue(*sdk.IGMPProxyFor)
	}
	current.Upstream = types.BoolValue(sdk.IGMPProxyUpstream)
	object, d := types.ObjectValueFrom(ctx, current.AttributeTypes(), current)
	diags.Append(d...)
	return object, diags
}

func encodeWANProviderCapabilities(
	ctx context.Context,
	object types.Object,
	sdk *ui.Network,
) diag.Diagnostics {
	var caps providerCapabilitiesModel
	diags := object.As(ctx, &caps, basetypes.ObjectAsOptions{})
	if diags.HasError() {
		return diags
	}
	sdk.WANProviderCapabilities = &ui.NetworkWANProviderCapabilities{
		DownloadKilobitsPerSecond: caps.DownloadKbps.ValueInt64Pointer(),
		UploadKilobitsPerSecond:   caps.UploadKbps.ValueInt64Pointer(),
	}
	return diags
}

// decodeWANProviderCapabilities only takes the controller's answer when it
// carries data; a nil or empty response preserves what state held, matching
// the hand networkToModel.
func decodeWANProviderCapabilities(
	ctx context.Context,
	sdk *ui.Network,
	prior types.Object,
) (types.Object, diag.Diagnostics) {
	var diags diag.Diagnostics
	if caps := sdk.WANProviderCapabilities; caps != nil &&
		(caps.DownloadKilobitsPerSecond != nil || caps.UploadKilobitsPerSecond != nil) {
		object, d := types.ObjectValue(providerCapabilitiesModel{}.AttributeTypes(),
			map[string]attr.Value{
				"download_kilobits_per_second": types.Int64PointerValue(
					caps.DownloadKilobitsPerSecond),
				"upload_kilobits_per_second": types.Int64PointerValue(
					caps.UploadKilobitsPerSecond),
			})
		diags.Append(d...)
		return object, diags
	}
	if wanPriorKnown(prior) {
		// The same unknown-member resolution as the merge path: on create
		// this prior is the plan's object, and returning it unresolved would
		// hand Terraform an unknown after apply.
		return wanResolveUnknowns(ctx, &diags, prior), diags
	}
	return types.ObjectNull(providerCapabilitiesModel{}.AttributeTypes()), diags
}
