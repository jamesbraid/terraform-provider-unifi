package unifi

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-nettypes/cidrtypes"
	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
	listresource_vpn_server "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/listresource_vpn_server"
	resource_vpn_server "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_vpn_server"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/resourcekit"
	"github.com/ubiquiti-community/terraform-provider-unifi/unifi/util"
)

// THE WIRE FORMAT IS THE PURPOSE ALIAS, NOT THE STRUCT, and every conditional
// declaration below was measured against it.
//
// unifi.Network marshals through marshalUserVPN for PurposeUserVPN. That alias
// emits 48 wires, six of them without omitempty -- purpose, enabled,
// dhcpd_dns_enabled, l2tp_allow_weak_ciphers, require_mschapv2 and
// vpn_client_configuration_remote_ip_override_enabled. Every other wire this
// surface writes carries omitempty THERE even where the generated struct does
// not, so reading the struct's tags gives a different and wrong answer. That
// mistake was made twice on vpn_client before it was caught.
//
// 22 of the 29 wires this surface writes are written only sometimes, and TEN of
// those carry certificates or private keys. The three VPN types are mutually
// exclusive, so a wireguard server leaves every openvpn wire unassigned on
// EVERY apply -- the blanking is the normal path here, not an edge case.

// vpnServerKitModel describes the resource data model.
type vpnServerKitModel struct {
	ID              types.String         `tfsdk:"id"`
	Site            types.String         `tfsdk:"site"`
	Name            types.String         `tfsdk:"name"`
	Enabled         types.Bool           `tfsdk:"enabled"`
	Subnet          cidrtypes.IPv4Prefix `tfsdk:"subnet"`
	DNS             types.Object         `tfsdk:"dns"`
	WAN             types.Object         `tfsdk:"wan"`
	RADIUSProfileID types.String         `tfsdk:"radiusprofile_id"`
	Wireguard       types.Object         `tfsdk:"wireguard"`
	L2TP            types.Object         `tfsdk:"l2tp"`
	OpenVPN         types.Object         `tfsdk:"openvpn"`
	Timeouts        timeouts.Value       `tfsdk:"timeouts"`
}

// local_port is written by whichever of the wireguard and openvpn blocks is
// configured, from that block's own port attribute. It is spelled out at both
// declaration sites rather than shared through a constant: the mapping checker
// parses Wires entries as string literals and refuses an identifier, because a
// name it cannot read is a wire it cannot account for.

func vpnServerObjectAs[T any](ctx context.Context, object types.Object, into *T) bool {
	return !object.As(ctx, into, basetypes.ObjectAsOptions{}).HasError()
}

// knownNonEmptyIn reports whether a member of an object is set to something the
// controller should be told about. It is the predicate half of knownNonEmpty:
// the mapper sends nil for null, unknown or empty, so the wire must not be
// masked in those cases or the mask sends "" over the controller's own value.
func knownNonEmptyIn(v types.String) bool {
	return !v.IsNull() && !v.IsUnknown() && v.ValueString() != ""
}

func encodeVPNServerDNS(ctx context.Context, object types.Object, sdk *ui.Network) diag.Diagnostics {
	var diags diag.Diagnostics
	var dns vpnServerDNSModel
	if !vpnServerObjectAs(ctx, object, &dns) {
		diags.AddError("Invalid DNS block", "could not read the dns block")
		return diags
	}
	if !dns.Enabled.IsNull() && !dns.Enabled.IsUnknown() {
		sdk.DHCPDDNSEnabled = dns.Enabled.ValueBool()
	}
	if !dns.Servers.IsNull() && !dns.Servers.IsUnknown() {
		var servers []string
		diags.Append(dns.Servers.ElementsAs(ctx, &servers, false)...)
		if diags.HasError() {
			return diags
		}
		vpnServerDNSServersToNetwork(servers, sdk)
		if len(servers) > 0 && (dns.Enabled.IsNull() || dns.Enabled.IsUnknown()) {
			sdk.DHCPDDNSEnabled = true
		}
	}
	return diags
}

func decodeVPNServerDNS(ctx context.Context, sdk *ui.Network, _ types.Object) (types.Object, diag.Diagnostics) {
	var diags diag.Diagnostics
	return types.ObjectValue(vpnServerDNSModel{}.AttributeTypes(), map[string]attr.Value{
		"enabled": types.BoolValue(sdk.DHCPDDNSEnabled),
		"servers": vpnServerDNSServersFromNetwork(ctx, &diags, sdk),
	})
}

// vpnServerDNSServerCount is the predicate half of the positional distribution.
//
// ONE PREDICATE PER WIRE, NOT ONE PER BLOCK. vpnServerDNSServersToNetwork writes
// slot one at len > 0 and slot two at len > 1, so a shared test would mask
// dhcpd_dns_2 for a practitioner who supplied a single server -- and a masked
// but unwritten field goes out as its zero, blanking the controller's second
// DNS. That is the defect vpn_client shipped before it was caught.
//
// It reads Attributes() rather than decoding the object, because a
// ConditionalWires predicate is handed the object alone with no context.
func vpnServerDNSServerCount(object types.Object) int {
	servers, ok := object.Attributes()["servers"].(types.List)
	if !ok || servers.IsNull() || servers.IsUnknown() {
		return 0
	}
	return len(servers.Elements())
}

// encodeVPNServerWAN is a NO-OP, deliberately, and the reason is ordering.
//
// wan.ip and wan.interface belong to the pair matching the configured VPN
// type, and vpnServerWANIPToNetwork / vpnServerWANInterfaceToNetwork read that
// type off sdk.VPNType to choose it. But this runs during ToSDK's Fields
// pass, and vpnServerBeforeSend -- the only place that sets sdk.VPNType --
// runs AFTER every field has been encoded. At this point sdk.VPNType is
// always nil, so the switch in both helpers matches no case and the wan pair
// was silently never written: measured live, a wireguard server configured
// with wan.ip="any", wan.interface="wan2" sent neither to the controller, and
// the first apply's check only ever passed because ApplyPlanToState overlays
// the plan's known values over Decode's null -- a refresh has no plan to fall
// back on and the block emptied on the very next Read.
//
// vpnServerBeforeSend does the actual write instead, once its own switch has
// set sdk.VPNType and the discriminator this needs finally exists. This field
// stays declared (Wires, AttrTypes, Decode) because those still describe real
// wire names and a real read; only the write half moved.
func encodeVPNServerWAN(context.Context, types.Object, *ui.Network) diag.Diagnostics {
	return nil
}

func decodeVPNServerWAN(_ context.Context, sdk *ui.Network, _ types.Object) (types.Object, diag.Diagnostics) {
	return types.ObjectValue(vpnServerWANModel{}.AttributeTypes(), map[string]attr.Value{
		"ip":        vpnServerWANIPFromNetwork(sdk),
		"interface": vpnServerWANInterfaceFromNetwork(sdk),
	})
}

func encodeVPNServerWireguard(ctx context.Context, object types.Object, sdk *ui.Network) diag.Diagnostics {
	var diags diag.Diagnostics
	var wg vpnServerWireguardModel
	if !vpnServerObjectAs(ctx, object, &wg) {
		diags.AddError("Invalid wireguard block", "could not read the wireguard block")
		return diags
	}
	// ENCODE MUST BE DETERMINISTIC, and generating the key here made it not.
	//
	// ConditionalWireProblems decides whether a wire was written by encoding
	// onto two differently-seeded structs and asking whether they converge. A
	// fresh random key on each call never converges, so a wire that is ALWAYS
	// written read as written-sometimes. Generation moved to BeforeSend, where
	// derived values belong; Encode now only copies what the practitioner gave.
	if knownNonEmptyIn(wg.PrivateKey) {
		sdk.WireguardPrivateKey = wg.PrivateKey.ValueStringPointer()
	}
	if !wg.Port.IsNull() && !wg.Port.IsUnknown() {
		vpnServerLocalPortToNetwork(wg.Port, sdk)
	}
	return diags
}

func decodeVPNServerWireguard(_ context.Context, sdk *ui.Network, _ types.Object) (types.Object, diag.Diagnostics) {
	if vpnServerType(sdk) != "wireguard-server" {
		return types.ObjectNull(vpnServerWireguardModel{}.AttributeTypes()), nil
	}
	return types.ObjectValue(vpnServerWireguardModel{}.AttributeTypes(), map[string]attr.Value{
		"private_key": types.StringPointerValue(sdk.WireguardPrivateKey),
		"public_key":  types.StringPointerValue(sdk.WireguardPublicKey),
		"port":        vpnServerLocalPortFromNetwork(sdk),
	})
}

func encodeVPNServerL2TP(ctx context.Context, object types.Object, sdk *ui.Network) diag.Diagnostics {
	var diags diag.Diagnostics
	var l2tp vpnServerL2TPModel
	if !vpnServerObjectAs(ctx, object, &l2tp) {
		diags.AddError("Invalid l2tp block", "could not read the l2tp block")
		return diags
	}
	sdk.L2TpAllowWeakCiphers = l2tp.AllowWeakCiphers.ValueBool()
	if knownNonEmptyIn(l2tp.PreSharedKey) {
		sdk.IPSecPreSharedKey = l2tp.PreSharedKey.ValueStringPointer()
	}
	return diags
}

// A BLOCK BELONGING TO ANOTHER VPN TYPE READS BACK NULL, not an object of
// zeros. The three are mutually exclusive, so a wireguard server must not
// present an empty l2tp block -- that would be a permanent diff against a
// configuration that never mentioned it.
func decodeVPNServerL2TP(_ context.Context, sdk *ui.Network, _ types.Object) (types.Object, diag.Diagnostics) {
	if vpnServerType(sdk) != "l2tp-server" {
		return types.ObjectNull(vpnServerL2TPModel{}.AttributeTypes()), nil
	}
	return types.ObjectValue(vpnServerL2TPModel{}.AttributeTypes(), map[string]attr.Value{
		"allow_weak_ciphers": types.BoolValue(sdk.L2TpAllowWeakCiphers),
		"pre_shared_key":     types.StringPointerValue(sdk.IPSecPreSharedKey),
	})
}

func encodeVPNServerOpenVPN(ctx context.Context, object types.Object, sdk *ui.Network) diag.Diagnostics {
	var diags diag.Diagnostics
	var ovpn vpnServerOpenVPNModel
	if !vpnServerObjectAs(ctx, object, &ovpn) {
		diags.AddError("Invalid openvpn block", "could not read the openvpn block")
		return diags
	}
	// Guarded for the same reason as every assignment below: writing nil for an
	// unset port makes the wire read as unconditionally written, and a masked
	// local_port with no value sends null over the controller's own port.
	if !ovpn.Port.IsNull() && !ovpn.Port.IsUnknown() {
		vpnServerLocalPortToNetwork(ovpn.Port, sdk)
	}
	// ASSIGN ONLY WHAT IS SET, rather than assigning nil for what is not.
	//
	// The hand-written mapper wrote nil through knownNonEmpty, which reads the
	// same on a freshly built object and NOT the same on one opened from the
	// controller: nil over a value clears it. ConditionalWireProblems caught
	// the disagreement -- Encode touched a wire its predicate said it would not
	// -- and skipping is the direction that cannot destroy anything.
	set := func(target **string, v types.String) {
		if knownNonEmptyIn(v) {
			*target = v.ValueStringPointer()
		}
	}
	set(&sdk.OpenVPNMode, ovpn.Mode)
	set(&sdk.OpenVPNEncryptionCipher, ovpn.EncryptionCipher)
	// The controller ISSUES this material. On create these are unknown, and
	// knownNonEmpty yields nil so nothing is asserted; on update the values
	// come back from state and are echoed. Each is masked only when it is
	// actually written -- eight separate predicates, because a practitioner may
	// supply any subset and masking an unwritten one sends "" over a
	// certificate.
	set(&sdk.ServerCrt, ovpn.ServerCrt)
	set(&sdk.ServerKey, ovpn.ServerKey)
	set(&sdk.DhKey, ovpn.DhKey)
	set(&sdk.SharedClientKey, ovpn.SharedClientKey)
	set(&sdk.SharedClientCrt, ovpn.SharedClientCrt)
	set(&sdk.AuthKey, ovpn.AuthKey)
	set(&sdk.CaCrt, ovpn.CaCrt)
	set(&sdk.CaKey, ovpn.CaKey)
	return diags
}

// A BLOCK BELONGING TO ANOTHER VPN TYPE READS BACK NULL, not an object of
// zeros. The three are mutually exclusive, so a wireguard server must not
// present an empty l2tp block -- that would be a permanent diff against a
// configuration that never mentioned it.
func decodeVPNServerOpenVPN(_ context.Context, sdk *ui.Network, _ types.Object) (types.Object, diag.Diagnostics) {
	if vpnServerType(sdk) != "openvpn-server" {
		return types.ObjectNull(vpnServerOpenVPNModel{}.AttributeTypes()), nil
	}
	return types.ObjectValue(vpnServerOpenVPNModel{}.AttributeTypes(), map[string]attr.Value{
		"port":              vpnServerLocalPortFromNetwork(sdk),
		"mode":              types.StringPointerValue(sdk.OpenVPNMode),
		"encryption_cipher": types.StringPointerValue(sdk.OpenVPNEncryptionCipher),
		"server_crt":        types.StringPointerValue(sdk.ServerCrt),
		"server_key":        types.StringPointerValue(sdk.ServerKey),
		"dh_key":            types.StringPointerValue(sdk.DhKey),
		"shared_client_key": types.StringPointerValue(sdk.SharedClientKey),
		"shared_client_crt": types.StringPointerValue(sdk.SharedClientCrt),
		"auth_key":          types.StringPointerValue(sdk.AuthKey),
		"ca_crt":            types.StringPointerValue(sdk.CaCrt),
		"ca_key":            types.StringPointerValue(sdk.CaKey),
	})
}

// openVPNMemberSet builds the per-wire predicate for one certificate member,
// keyed by its attribute name. Eight of these rather than one shared test,
// because a practitioner may supply any subset and each wire is written only
// when its own member is set.
func openVPNMemberSet(attribute string) func(types.Object) bool {
	return func(object types.Object) bool {
		value, ok := object.Attributes()[attribute].(types.String)
		return ok && knownNonEmptyIn(value)
	}
}

// vpnServerUnwritableWires names the wan wires belonging to the two VPN
// families that are NOT configured, so the kit drops them from the mask.
//
// IT IS NOT THE did-emit NARROWING AND DOES NOT REINTRODUCE THE CANNOT-CLEAR.
// The hazard recorded against this hook comes from dropping a name BECAUSE THE
// FIELD SITS AT ITS ZERO, which cannot tell "omitted at zero" from "never
// emitted" and so throws away maskedBody's ability to clear a value. This one
// never looks at a value: it drops openvpn_* on a wireguard server BY NAME,
// keyed on the family. An empty wireguard_interface on a wireguard server stays
// on the mask and can still be cleared. Different question, different failure
// mode, no overlap with the ban.
//
// It exists because ConditionalWires cannot express this. That predicate is
// handed the object alone; the values live in the wan block and the family
// lives in the SDK object, which only a hook running after BeforeSend can read.
// A null wan block needs nothing from here -- ScatteredObjectField's SetInPlan
// already keeps all six names off the mask when the object is absent.
func vpnServerUnwritableWires(sdk *ui.Network) []string {
	families := map[string][]string{
		"wireguard-server": {"wireguard_local_wan_ip", "wireguard_interface"},
		"l2tp-server":      {"l2tp_local_wan_ip", "l2tp_interface"},
		"openvpn-server":   {"openvpn_local_wan_ip", "openvpn_interface"},
	}
	configured := vpnServerType(sdk)
	var unwritable []string
	for _, family := range []string{"wireguard-server", "l2tp-server", "openvpn-server"} {
		if family != configured {
			unwritable = append(unwritable, families[family]...)
		}
	}

	// THE CONFIGURED FAMILY'S OWN PAIR IS ALSO UNWRITABLE WHEN ITS SLOT IS
	// EMPTY, and this half is value-based on purpose. The UserVPN alias nils
	// an empty string before applying omitempty, so an empty slot is a wire
	// the encoding cannot carry at any value the model can express -- and
	// go-unifi's masked write refuses a mask naming it. Measured live: every
	// wireguard update failed with "this type does not write:
	// wireguard_interface, wireguard_local_wan_ip" once the read path put a
	// decoded, empty-membered wan object into state. The cannot-clear caution
	// recorded above does not apply here, because the alias cannot send an
	// empty value for these six wires at all -- there is nothing to lose.
	for name, value := range map[string]*string{
		"wireguard_local_wan_ip": sdk.WireguardLocalWANIP,
		"wireguard_interface":    sdk.WireguardInterface,
		"l2tp_local_wan_ip":      sdk.L2TpLocalWANIP,
		"l2tp_interface":         sdk.L2TpInterface,
		"openvpn_local_wan_ip":   sdk.OpenVPNLocalWANIP,
		"openvpn_interface":      sdk.OpenVPNInterface,
		// AlwaysWire puts the key on every mask so BeforeSend's generated
		// value travels; on an l2tp or openvpn server nothing generates one,
		// the slot stays empty, and the alias cannot carry it -- measured
		// live, every l2tp and openvpn update was refused over it.
		"x_wireguard_private_key": sdk.WireguardPrivateKey,
	} {
		if value == nil || *value == "" {
			unwritable = append(unwritable, name)
		}
	}
	return unwritable
}

func vpnServerBeforeSend(ctx context.Context, _, effective *vpnServerKitModel, sdk *ui.Network, _ any) diag.Diagnostics {
	var diags diag.Diagnostics
	sdk.Purpose = ui.PurposeUserVPN
	sdk.SettingPreference = util.Ptr("manual")

	// The WireGuard key the practitioner did not supply. Here rather than in
	// Encode so Encode stays deterministic. x_wireguard_private_key is in
	// AlwaysWire because this guarantees a value whenever the block is set.
	if !effective.Wireguard.IsNull() && !effective.Wireguard.IsUnknown() &&
		(sdk.WireguardPrivateKey == nil || *sdk.WireguardPrivateKey == "") {
		key, err := generateWireGuardPrivateKey()
		if err != nil {
			diags.AddError("Unable to generate WireGuard private key", err.Error())
			return diags
		}
		sdk.WireguardPrivateKey = &key
	}
	switch {
	case !effective.Wireguard.IsNull() && !effective.Wireguard.IsUnknown():
		sdk.VPNType = util.Ptr("wireguard-server")
	case !effective.L2TP.IsNull() && !effective.L2TP.IsUnknown():
		sdk.VPNType = util.Ptr("l2tp-server")
	case !effective.OpenVPN.IsNull() && !effective.OpenVPN.IsUnknown():
		sdk.VPNType = util.Ptr("openvpn-server")
	default:
		diags.AddError(
			"Missing VPN Type Configuration",
			"Exactly one of `wireguard`, `l2tp`, or `openvpn` must be specified.",
		)
	}
	if diags.HasError() {
		return diags
	}

	// THE WAN PAIR, NOW THAT THE DISCRIMINATOR EXISTS. encodeVPNServerWAN
	// cannot do this: it runs during ToSDK, before sdk.VPNType above is set,
	// so vpnServerWANIPToNetwork's switch on it would match nothing. Here it
	// just set VPNType two lines up, so the same helpers now route correctly.
	if !effective.WAN.IsNull() && !effective.WAN.IsUnknown() {
		var wan vpnServerWANModel
		if !vpnServerObjectAs(ctx, effective.WAN, &wan) {
			diags.AddError("Invalid WAN block", "could not read the wan block")
			return diags
		}
		vpnServerWANIPToNetwork(wan.IP, sdk)
		vpnServerWANInterfaceToNetwork(wan.Interface, sdk)
	}
	return diags
}

// vpnServerAfterReceive restores the two secrets the controller does not echo,
// then derives the one wire the controller never sends at all.
//
// THIS IS WHAT AfterReceive's prior PARAMETER EXISTS FOR. Both members belong to
// objects a Field decodes, so by the time this runs ToModel has already
// overwritten them with whatever came back -- which for these two is nothing.
// Reading the model would find the loss; reading prior finds the value.
//
// Restoring only when the fresh read is empty matters: a controller that DOES
// return the key must win, or a key rotated outside Terraform would be masked
// by state forever.
func vpnServerAfterReceive(ctx context.Context, _ *ui.Network, model *vpnServerKitModel, prior vpnServerKitModel, _ any) diag.Diagnostics {
	var diags diag.Diagnostics
	carry := func(current *types.Object, priorObject types.Object, member string) {
		if current.IsNull() || current.IsUnknown() || priorObject.IsNull() || priorObject.IsUnknown() {
			return
		}
		fresh, ok := current.Attributes()[member].(types.String)
		if !ok || knownNonEmptyIn(fresh) {
			return
		}
		kept, ok := priorObject.Attributes()[member].(types.String)
		if !ok || !knownNonEmptyIn(kept) {
			return
		}
		attributes := current.Attributes()
		attributes[member] = kept
		rebuilt, d := types.ObjectValue(current.AttributeTypes(ctx), attributes)
		diags.Append(d...)
		if !d.HasError() {
			*current = rebuilt
		}
	}
	carry(&model.Wireguard, prior.Wireguard, "private_key")
	carry(&model.L2TP, prior.L2TP, "pre_shared_key")
	diags.Append(vpnServerDerivePublicKey(ctx, &model.Wireguard)...)
	return diags
}

// vpnServerDerivePublicKey fills wireguard.public_key when the controller has
// not sent one, deriving it from private_key. It runs after carry() above so
// it sees the private key once any state-preserved value has been restored --
// the same key this attribute is computed from either way.
//
// THE CONTROLLER NEVER RETURNS ONE. Measured on 10.4.57: wireguard_public_key
// is absent on create, absent after every update, absent on every read, while
// x_wireguard_private_key comes back at full length each time. decodeVPNServerWireguard
// reads wireguard_public_key straight off the SDK object, which is why it was
// null from the first apply and stayed null once the kit cutover dropped this
// step -- see wireguard_key.go for why deriving it here is not the provider
// inventing a value.
func vpnServerDerivePublicKey(ctx context.Context, current *types.Object) diag.Diagnostics {
	var diags diag.Diagnostics
	if current.IsNull() || current.IsUnknown() {
		return diags
	}
	attributes := current.Attributes()
	privateKey, ok := attributes["private_key"].(types.String)
	if !ok || !knownNonEmptyIn(privateKey) {
		return diags
	}
	if publicKey, ok := attributes["public_key"].(types.String); ok && knownNonEmptyIn(publicKey) {
		return diags
	}
	derived, err := wireguardPublicKey(privateKey.ValueString())
	if err != nil {
		// REPORTED, NOT SWALLOWED. Falling back to null here would restore the
		// defect this replaces, and silently: the practitioner would see the
		// same empty string and have no way to learn the key was malformed.
		diags.AddError(
			"Cannot derive the WireGuard public key",
			"The controller does not return wireguard_public_key, so the provider "+
				"derives it from the private key. That failed: "+err.Error(),
		)
		return diags
	}
	attributes["public_key"] = types.StringValue(derived)
	rebuilt, d := types.ObjectValue(current.AttributeTypes(ctx), attributes)
	diags.Append(d...)
	if !d.HasError() {
		*current = rebuilt
	}
	return diags
}

func vpnServerKitSpec() resourcekit.Spec[vpnServerKitModel, ui.Network] {
	return resourcekit.Spec[vpnServerKitModel, ui.Network]{
		TypeName: "vpn_server",
		Subject:  "VPN Server",
		New:      func() *ui.Network { return &ui.Network{} },
		ID:       func(m *vpnServerKitModel) *types.String { return &m.ID },
		Site:     func(m *vpnServerKitModel) *types.String { return &m.Site },
		Timeouts: func(m *vpnServerKitModel) *timeouts.Value { return &m.Timeouts },
		IDWire:   "_id",
		// purpose, setting_preference and vpn_type are set by BeforeSend and
		// held by no attribute, so nothing else would put them on the mask.
		AlwaysWire: []string{
			"purpose", "setting_preference", "vpn_type",
			// BeforeSend guarantees this whenever the block is set.
			"x_wireguard_private_key",
		},
		BeforeSend:      vpnServerBeforeSend,
		AfterReceive:    vpnServerAfterReceive,
		UnwritableWires: vpnServerUnwritableWires,
		Fields: []resourcekit.Field[vpnServerKitModel, ui.Network]{
			resourcekit.StringLikePtrField[vpnServerKitModel, ui.Network, types.String]{
				Wire:  "name",
				Model: func(m *vpnServerKitModel) *types.String { return &m.Name },
				SDK:   func(s *ui.Network) **string { return &s.Name },
				New:   func(v basetypes.StringValue) types.String { return v },
			},
			resourcekit.StringLikePtrField[vpnServerKitModel, ui.Network, cidrtypes.IPv4Prefix]{
				Wire:  "ip_subnet",
				Model: func(m *vpnServerKitModel) *cidrtypes.IPv4Prefix { return &m.Subnet },
				SDK:   func(s *ui.Network) **string { return &s.IPSubnet },
				New: func(v basetypes.StringValue) cidrtypes.IPv4Prefix {
					return cidrtypes.IPv4Prefix{StringValue: v}
				},
			},
			resourcekit.BoolField[vpnServerKitModel, ui.Network]{
				Wire:  "enabled",
				Model: func(m *vpnServerKitModel) *types.Bool { return &m.Enabled },
				SDK:   func(s *ui.Network) *bool { return &s.Enabled },
			},
			resourcekit.StringLikePtrField[vpnServerKitModel, ui.Network, types.String]{
				Wire:  "radiusprofile_id",
				Model: func(m *vpnServerKitModel) *types.String { return &m.RADIUSProfileID },
				SDK:   func(s *ui.Network) **string { return &s.RADIUSProfileID },
				New:   func(v basetypes.StringValue) types.String { return v },
			},
			// dhcpd_dns_enabled has NO omitempty in the UserVPN alias, so it
			// travels whenever the block does and needs no predicate. The two
			// slots do, and they differ -- see vpnServerDNSServerCount.
			resourcekit.ScatteredObjectField[vpnServerKitModel, ui.Network]{
				Wires:     []string{"dhcpd_dns_enabled", "dhcpd_dns_1", "dhcpd_dns_2"},
				Model:     func(m *vpnServerKitModel) *types.Object { return &m.DNS },
				AttrTypes: vpnServerDNSModel{}.AttributeTypes(),
				Encode:    encodeVPNServerDNS,
				Decode:    decodeVPNServerDNS,
				ConditionalWires: map[string]func(types.Object) bool{
					"dhcpd_dns_1": func(o types.Object) bool { return vpnServerDNSServerCount(o) > 0 },
					"dhcpd_dns_2": func(o types.Object) bool { return vpnServerDNSServerCount(o) > 1 },
				},
			},
			// All six wan wires are declared; vpnServerUnwritableWires drops the
			// four belonging to the families that are not configured, and the
			// configured family's pair whenever its slot is empty -- see the
			// measured reason on that function.
			resourcekit.ScatteredObjectField[vpnServerKitModel, ui.Network]{
				Wires: []string{
					"wireguard_local_wan_ip", "wireguard_interface",
					"l2tp_local_wan_ip", "l2tp_interface",
					"openvpn_local_wan_ip", "openvpn_interface",
				},
				Model:     func(m *vpnServerKitModel) *types.Object { return &m.WAN },
				AttrTypes: vpnServerWANModel{}.AttributeTypes(),
				Encode:    encodeVPNServerWAN,
				Decode:    decodeVPNServerWAN,
			},
			// x_wireguard_private_key is NOT conditional: when the block is
			// present the key is either supplied or generated, so the wire is
			// always written. local_port likewise comes from the block's port.
			resourcekit.ScatteredObjectField[vpnServerKitModel, ui.Network]{
				Wires: []string{
					"x_wireguard_private_key",
					"local_port",
					// The controller issues wireguard_public_key and accepts
					// none: marshalUserVPN emits no such wire, so masking it
					// names a field the encoder cannot write and maskedBody
					// refuses the whole update. Decode reads it; there is no
					// write half.
					//
					// It is DECLARED rather than omitted so WireNameProblems
					// still checks the name against the SDK's own tags, where
					// wireguard_public_key is real. ReadOnlyWires keeps it off
					// the mask.
					"wireguard_public_key",
				},
				Model:         func(m *vpnServerKitModel) *types.Object { return &m.Wireguard },
				AttrTypes:     vpnServerWireguardModel{}.AttributeTypes(),
				Elide:         resourcekit.NullZero,
				Encode:        encodeVPNServerWireguard,
				Decode:        decodeVPNServerWireguard,
				ReadOnlyWires: []string{"wireguard_public_key"},
				ConditionalWires: map[string]func(types.Object) bool{
					"x_wireguard_private_key": openVPNMemberSet("private_key"),
					"local_port":              portSet,
				},
			},
			resourcekit.ScatteredObjectField[vpnServerKitModel, ui.Network]{
				Wires:     []string{"l2tp_allow_weak_ciphers", "x_ipsec_pre_shared_key"},
				Model:     func(m *vpnServerKitModel) *types.Object { return &m.L2TP },
				AttrTypes: vpnServerL2TPModel{}.AttributeTypes(),
				Elide:     resourcekit.NullZero,
				Encode:    encodeVPNServerL2TP,
				Decode:    decodeVPNServerL2TP,
				ConditionalWires: map[string]func(types.Object) bool{
					"x_ipsec_pre_shared_key": openVPNMemberSet("pre_shared_key"),
				},
			},
			// EIGHT SEPARATE PREDICATES FOR EIGHT CERTIFICATES. A practitioner
			// may supply any subset, the controller issues the rest, and a
			// masked-but-unwritten wire goes out as "" over key material.
			resourcekit.ScatteredObjectField[vpnServerKitModel, ui.Network]{
				Wires: []string{
					"local_port", "openvpn_mode", "openvpn_encryption_cipher",
					"x_server_crt", "x_server_key", "x_dh_key",
					"x_shared_client_key", "x_shared_client_crt",
					"x_auth_key", "x_ca_crt", "x_ca_key",
				},
				Model:     func(m *vpnServerKitModel) *types.Object { return &m.OpenVPN },
				AttrTypes: vpnServerOpenVPNModel{}.AttributeTypes(),
				Elide:     resourcekit.NullZero,
				Encode:    encodeVPNServerOpenVPN,
				Decode:    decodeVPNServerOpenVPN,
				ConditionalWires: map[string]func(types.Object) bool{
					"openvpn_mode":              openVPNMemberSet("mode"),
					"openvpn_encryption_cipher": openVPNMemberSet("encryption_cipher"),
					"x_server_crt":              openVPNMemberSet("server_crt"),
					"x_server_key":              openVPNMemberSet("server_key"),
					"x_dh_key":                  openVPNMemberSet("dh_key"),
					"x_shared_client_key":       openVPNMemberSet("shared_client_key"),
					"x_shared_client_crt":       openVPNMemberSet("shared_client_crt"),
					"x_auth_key":                openVPNMemberSet("auth_key"),
					"x_ca_crt":                  openVPNMemberSet("ca_crt"),
					"x_ca_key":                  openVPNMemberSet("ca_key"),
					"local_port":                portSet,
				},
			},
		},
		Backend: resourcekit.Backend[ui.Network]{
			GetID: func(s *ui.Network) string { return s.ID },
			SetID: func(s *ui.Network, id string) { s.ID = id },
		},
	}
}

func vpnServerKitSchema() resourcekit.SchemaSpec {
	return resourcekit.SchemaSpec{
		Resource: resource_vpn_server.VpnServerResourceSchema,
		Timeouts: timeouts.Opts{Create: true, Read: true, Update: true, Delete: true},
	}
}

func vpnServerKitList() resourcekit.ListSpec[ui.Network] {
	return resourcekit.ListSpec[ui.Network]{
		ConfigSchema: listresource_vpn_server.VpnServerListResourceSchema,
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

func vpnServerKitBackend(client *ui.ApiClient) resourcekit.Backend[ui.Network] {
	return resourcekit.Backend[ui.Network]{
		Create: func(ctx context.Context, site string, in *ui.Network) (*ui.Network, error) {
			return client.CreateNetwork(ctx, site, in)
		},
		Read: func(ctx context.Context, site, id string) (*ui.Network, error) {
			return client.GetNetwork(ctx, site, id)
		},
		UpdateFields: func(ctx context.Context, site string, in *ui.Network, fields ...string) (*ui.Network, error) {
			return client.UpdateNetworkFields(ctx, site, in, fields...)
		},
		Delete: func(ctx context.Context, site, id string) error {
			existing, err := client.GetNetwork(ctx, site, id)
			if err != nil {
				return err
			}
			name := ""
			if existing.Name != nil {
				name = *existing.Name
			}
			return client.DeleteNetwork(ctx, site, id, name)
		},
		List: func(ctx context.Context, site string) ([]ui.Network, error) {
			return client.ListNetwork(ctx, site)
		},
		GetID: func(s *ui.Network) string { return s.ID },
		SetID: func(s *ui.Network, id string) { s.ID = id },
	}
}

// portSet reports whether a block's port member carries a value, for the two
// blocks that write local_port from their own port attribute.
func portSet(object types.Object) bool {
	port, ok := object.Attributes()["port"].(types.Int64)
	return ok && !port.IsNull() && !port.IsUnknown()
}
