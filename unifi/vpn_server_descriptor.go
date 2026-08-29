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

// vpnServerKitModel describes the resource data model.
//
// The wire format is the purpose alias (marshalUserVPN for PurposeUserVPN),
// not the struct: it omits fields the struct's own json tags don't mark
// omitempty, so reading the struct's tags gives a different, wrong answer.
// The three VPN types are mutually exclusive, so a wireguard server leaving
// every openvpn wire unassigned on every apply is the normal path, not a bug.
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

// local_port is spelled out at both declaration sites below rather than
// shared through a constant: the mapping checker parses Wires entries as
// string literals, and a name it cannot read is a wire it cannot account for.

func vpnServerObjectAs[T any](ctx context.Context, object types.Object, into *T) bool {
	return !object.As(ctx, into, basetypes.ObjectAsOptions{}).HasError()
}

// knownNonEmptyIn reports whether a member of an object is set to something the
// controller should be told about: the encoders below skip assigning a wire
// when its value is null, unknown or empty, so the wire must not be masked in
// those cases either, or the mask sends "" over the controller's own value.
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

// vpnServerDNSServersTouched reports whether this apply gives dns.servers a
// value at all -- distinct from giving it a SHORTER one. Omitting servers
// from an otherwise-set dns block (toggling just enabled, say) must leave
// existing DNS servers alone; an explicit list, even an empty one, is the
// practitioner saying what the servers now are, and is what gates the
// clearing read in vpnServerBeforeSend.
func vpnServerDNSServersTouched(object types.Object) bool {
	if object.IsNull() || object.IsUnknown() {
		return false
	}
	servers, ok := object.Attributes()["servers"].(types.List)
	return ok && !servers.IsNull() && !servers.IsUnknown()
}

// encodeVPNServerWAN is a no-op, deliberately: it runs during ToSDK's Fields
// pass, before vpnServerBeforeSend (the only place that sets sdk.VPNType) has
// run, so the switch in vpnServerWANIPToNetwork/vpnServerWANInterfaceToNetwork
// would match no case and silently write nothing. vpnServerBeforeSend does
// the actual write instead, once VPNType exists. This field stays declared
// (Wires, AttrTypes, Decode) for the real wire names and read; only the write
// half moved.
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
	// Encode must be deterministic: ConditionalWireProblems checks whether a
	// wire was written by encoding onto two differently-seeded structs and
	// comparing, so generating a random key here would make an always-written
	// wire look written-sometimes. Generation moved to BeforeSend; Encode only
	// copies what the practitioner gave.
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

// A block belonging to another VPN type reads back null, not zeros: the
// three are mutually exclusive, so presenting an empty block would be a
// permanent diff against a config that never mentioned it.
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
	// Assign only what is set, rather than nil for what is not: nil reads the
	// same as unset on a fresh object but clears a value on one opened from
	// the controller, and skipping is the direction that cannot destroy
	// anything.
	set := func(target **string, v types.String) {
		if knownNonEmptyIn(v) {
			*target = v.ValueStringPointer()
		}
	}
	set(&sdk.OpenVPNMode, ovpn.Mode)
	set(&sdk.OpenVPNEncryptionCipher, ovpn.EncryptionCipher)
	// The controller issues this material: on create these are unknown so
	// nothing is asserted, and on update they're echoed back from state. Each
	// is masked only when actually written -- eight separate predicates,
	// since masking an unwritten one sends "" over a certificate.
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

// A block belonging to another VPN type reads back null, not zeros: the
// three are mutually exclusive, so presenting an empty block would be a
// permanent diff against a config that never mentioned it.
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
// This is safe unlike dropping a name because a field sits at its zero (which
// can't tell "omitted at zero" from "never emitted", and breaks maskedBody's
// ability to clear a value): this drops openvpn_* on a wireguard server by
// family name, never by value, so an empty wireguard_interface on a
// wireguard server stays on the mask and can still be cleared.
//
// It exists because ConditionalWires can't express this: that predicate only
// gets the object, but the family lives in the SDK object, which only a hook
// after BeforeSend can read.
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

	// The configured family's own pair is also unwritable when its slot is
	// empty -- this half is value-based, unlike the family check above. The
	// UserVPN alias nils an empty string before applying omitempty, so an
	// empty slot is a value the encoding cannot carry at all, and go-unifi's
	// masked write refuses a mask naming it. No cannot-clear risk here: the
	// alias can't send an empty value for these six wires either way.
	for name, value := range map[string]*string{
		"wireguard_local_wan_ip": sdk.WireguardLocalWANIP,
		"wireguard_interface":    sdk.WireguardInterface,
		"l2tp_local_wan_ip":      sdk.L2TpLocalWANIP,
		"l2tp_interface":         sdk.L2TpInterface,
		"openvpn_local_wan_ip":   sdk.OpenVPNLocalWANIP,
		"openvpn_interface":      sdk.OpenVPNInterface,
		// AlwaysWire puts this key on every mask so BeforeSend's generated
		// value travels; on an l2tp or openvpn server nothing generates one,
		// the slot stays empty, and the alias can't carry it, so it must be
		// dropped too.
		"x_wireguard_private_key": sdk.WireguardPrivateKey,
	} {
		if value == nil || *value == "" {
			unwritable = append(unwritable, name)
		}
	}

	// The four DNS slots are unconditionally in the mask (see the DNS
	// ScatteredObjectField), so a slot neither Encode nor
	// vpnServerClearDroppedDNS wrote has to be dropped here or go-unifi
	// sends JSON null for it -- a write, not the "leave it alone" this is
	// for. Unlike the pairs above, "" is NOT unwritable here: it is
	// vpnServerClearDroppedDNS's own, deliberate clear, and must stay on
	// the mask for the write to say anything.
	for name, value := range map[string]*string{
		"dhcpd_dns_1": sdk.DHCPDDNS1,
		"dhcpd_dns_2": sdk.DHCPDDNS2,
		"dhcpd_dns_3": sdk.DHCPDDNS3,
		"dhcpd_dns_4": sdk.DHCPDDNS4,
	} {
		if value == nil {
			unwritable = append(unwritable, name)
		}
	}
	return unwritable
}

// vpnServerBeforeSend runs the purpose/vpn_type/WAN setup all three VPN
// types share (vpnServerBeforeSendBody), then clears any DNS server prior
// held that this apply's list no longer reaches (vpnServerClearDroppedDNS).
func vpnServerBeforeSend(
	ctx context.Context,
	_, effective *vpnServerKitModel,
	prior vpnServerKitModel,
	sdk *ui.Network,
	_ any,
) diag.Diagnostics {
	diags := vpnServerBeforeSendBody(ctx, effective, sdk)
	if diags.HasError() {
		return diags
	}
	diags.Append(vpnServerClearDroppedDNS(ctx, &prior, effective, sdk)...)
	return diags
}

// vpnServerClearDroppedDNS clears a DNS slot prior fills that this apply's
// server list no longer reaches, writing directly onto sdk -- the SAME
// object the kit's one masked write sends, not a second one.
//
// This can't be Encode's job: by the time ToSDK runs, the kit's Update has
// already merged the plan onto state, so the object Encode sees holds only
// the NEW list. prior is what makes the OLD one available here -- see
// resourcekit.Spec.BeforeSend's own doc. The DNS ScatteredObjectField
// declares all four dhcpd_dns_N wires unconditionally for exactly this: the
// mask is computed from the plan before this hook runs, so a wire this
// function decides to clear has to already be eligible, and
// vpnServerUnwritableWires drops whichever of the four this apply leaves nil
// -- neither Encode nor prior ever had anything to say about it.
//
// Skipped whenever dns.servers isn't part of this apply at all (an update
// touching some other attribute, or toggling dns.enabled alone, must leave
// existing DNS servers untouched).
func vpnServerClearDroppedDNS(
	ctx context.Context,
	prior, effective *vpnServerKitModel,
	sdk *ui.Network,
) diag.Diagnostics {
	var diags diag.Diagnostics
	if !vpnServerDNSServersTouched(effective.DNS) {
		return diags
	}
	newServers, d := vpnServerDNSServerList(ctx, effective.DNS)
	diags.Append(d...)
	priorServers, d := vpnServerDNSServerList(ctx, prior.DNS)
	diags.Append(d...)
	if diags.HasError() {
		return diags
	}
	priorNetwork := &ui.Network{}
	vpnServerDNSServersToNetwork(priorServers, priorNetwork)
	for _, wire := range vpnServerDNSServersClearDropped(newServers, priorNetwork) {
		switch wire {
		case "dhcpd_dns_1":
			sdk.DHCPDDNS1 = util.Ptr("")
		case "dhcpd_dns_2":
			sdk.DHCPDDNS2 = util.Ptr("")
		case "dhcpd_dns_3":
			sdk.DHCPDDNS3 = util.Ptr("")
		case "dhcpd_dns_4":
			sdk.DHCPDDNS4 = util.Ptr("")
		}
	}
	return diags
}

// vpnServerDNSServerList reads dns.servers as a plain slice, empty (not an
// error) when the block or the list itself is null or unknown.
func vpnServerDNSServerList(ctx context.Context, object types.Object) ([]string, diag.Diagnostics) {
	var diags diag.Diagnostics
	if object.IsNull() || object.IsUnknown() {
		return nil, diags
	}
	servers, ok := object.Attributes()["servers"].(types.List)
	if !ok || servers.IsNull() || servers.IsUnknown() {
		return nil, diags
	}
	var out []string
	diags.Append(servers.ElementsAs(ctx, &out, false)...)
	return out, diags
}

func vpnServerBeforeSendBody(ctx context.Context, effective *vpnServerKitModel, sdk *ui.Network) diag.Diagnostics {
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

	// Now that VPNType is set (above), these helpers route correctly;
	// encodeVPNServerWAN runs too early in ToSDK to do this itself.
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

// vpnServerAfterReceive restores the two secrets the controller does not
// echo, then derives the wire the controller never sends. Reads prior, not
// model: ToModel already overwrote these via their Field's Decode. Restoring
// only when the fresh read is empty matters -- a controller that does return
// the key must win, or a key rotated outside Terraform would be masked by
// state forever.
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
// not sent one, deriving it from private_key; it runs after carry() so it
// sees any state-preserved key too. The controller never returns public_key
// at all -- decodeVPNServerWireguard reads it straight off the SDK object,
// which is always empty -- see wireguard_key.go for why deriving it here
// isn't the provider inventing a value.
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
		// Reported, not swallowed: falling back to null here would silently
		// restore the defect this replaces, leaving the practitioner unable
		// to learn the key was malformed.
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
			// dhcpd_dns_enabled has no omitempty in the UserVPN alias and
			// always travels. The four slots share ONE predicate --
			// vpnServerDNSServersTouched, "is servers part of this apply at
			// all" -- rather than one per slot keyed on how many servers the
			// PLAN gives: a per-slot count would exclude, say, dhcpd_dns_2
			// from the mask whenever the plan has only one server, and
			// vpnServerClearDroppedDNS needs that slot IN the mask to clear
			// it when prior had a second one. The shared predicate only ever
			// widens or closes the whole gate; vpnServerUnwritableWires
			// narrows it back down afterwards, once Encode and
			// vpnServerClearDroppedDNS have both had their say about which
			// of the four this particular apply actually writes.
			resourcekit.ScatteredObjectField[vpnServerKitModel, ui.Network]{
				Wires: []string{
					"dhcpd_dns_enabled", "dhcpd_dns_1", "dhcpd_dns_2", "dhcpd_dns_3", "dhcpd_dns_4",
				},
				Model:     func(m *vpnServerKitModel) *types.Object { return &m.DNS },
				AttrTypes: vpnServerDNSModel{}.AttributeTypes(),
				Encode:    encodeVPNServerDNS,
				Decode:    decodeVPNServerDNS,
				ConditionalWires: map[string]func(types.Object) bool{
					"dhcpd_dns_1": vpnServerDNSServersTouched,
					"dhcpd_dns_2": vpnServerDNSServersTouched,
					"dhcpd_dns_3": vpnServerDNSServersTouched,
					"dhcpd_dns_4": vpnServerDNSServersTouched,
				},
			},
			// All six wan wires are declared; vpnServerUnwritableWires drops the
			// four belonging to the families that are not configured, and the
			// configured family's pair whenever its slot is empty.
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
					// The controller accepts no wireguard_public_key:
					// marshalUserVPN emits no such wire, so masking it would
					// make maskedBody refuse the whole update. It stays
					// declared (not omitted) so WireNameProblems still checks
					// the name against the SDK's real tag; ReadOnlyWires keeps
					// it off the mask.
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
			// Eight separate predicates for eight certificates: a practitioner
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
		// Seeded here as well as in vpnServerKitBackend, because Configure binds
		// the real Backend and a unit test calling ToModel on an unconfigured
		// spec would otherwise dereference nil.
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
