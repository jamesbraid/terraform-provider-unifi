package unifi

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/resourcekit"
	"github.com/ubiquiti-community/terraform-provider-unifi/unifi/util"
)

// vpnClientWireguardWires is every attribute of unifi.Network that the
// `wireguard` object writes. ALL of them reach the mask; a name missing here is
// a value the practitioner sets and the apply never sends.
//
// TEN, NOT THREE, and the two that are not wireguard-named are the point.
// dhcpd_dns_1 and dhcpd_dns_2 are written by wireguardDNSServersToNetwork from
// the block's dns_servers list, so an author enumerating this list by grepping
// the SDK for "Wireguard" produces eight names, the mask omits two, and
// dns_servers becomes an attribute the practitioner can set and nothing writes.
// That is the silent write-drop this kind exists to prevent, and it is reachable
// on the first real surface.
//
// x_wireguard_private_key IS THE OTHER TRAP. The Go field is
// WireguardPrivateKey, so a name transcribed from the struct is
// wireguard_private_key -- an attribute unifi.Network does not have. A mask
// naming it is accepted and changes nothing, which is dns_record's `name` ->
// `key` again. WireNameProblems catches it; nothing else does.
//
// THREE OF THE TEN ARE FORCE-EMITTED: wireguard_client_preshared_key_enabled,
// dhcpd_dns_1 and dhcpd_dns_2 carry no omitempty on the struct. The last two are
// #211's cannot-clear pair -- the VPNClient encoder adds omitempty that the
// struct does not -- so a practitioner can set them and cannot empty them. That
// is an open defect this field neither causes nor fixes, recorded here because
// this is where someone will next look at these two names.
func vpnClientWireguardWires() []string {
	return []string{
		"x_wireguard_private_key",
		"wireguard_interface",
		"wireguard_client_preshared_key_enabled",
		"wireguard_client_preshared_key",
		"wireguard_client_mode",
		"wireguard_client_peer_public_key",
		"wireguard_client_peer_ip",
		"wireguard_client_peer_port",
		"dhcpd_dns_1",
		"dhcpd_dns_2",
	}
}

// vpnClientWireguardField binds the wireguard object to those ten fields.
//
// Encode is the existing mapper's wireguard branch, moved rather than rewritten:
// the configuration-file path parses and derives, the peer path writes manual
// mode, and the preshared key is optional to both. Decode is its counterpart
// from the read side. What the kind adds is the mask half -- that every name
// above travels together, and that each is a real attribute of the SDK type.
func vpnClientWireguardField() resourcekit.ScatteredObjectField[vpnClientResourceModel, ui.Network] {
	return resourcekit.ScatteredObjectField[vpnClientResourceModel, ui.Network]{
		Wires:     vpnClientWireguardWires(),
		Model:     func(m *vpnClientResourceModel) *types.Object { return &m.Wireguard },
		AttrTypes: wireguardModel{}.AttributeTypes(),
		// TWO OF THE TEN TRAVEL ONLY WITH dns_servers, and declaring all ten
		// unconditionally is a destructive write rather than an untidy one.
		//
		// wireguardDNSServersToNetwork assigns DHCPDDNS1 and DHCPDDNS2 only when
		// the practitioner supplied servers, and go-unifi sends a masked field's
		// ZERO when the object carries no value. So a mask naming them on an
		// apply that set a wireguard block without dns_servers writes two empty
		// strings and blanks the controller's DNS. The hand-written mask this
		// field replaces omits exactly these two and unifi/wire_field_masks_test.go
		// records why, under conditionallyAssigned.
		//
		// Measured on the field as it stood before ConditionalWires existed: both
		// names were in the mask with the SDK object carrying "". Nothing caught
		// it -- the descriptor compiles, ElideProblems passes, and WireNameProblems
		// passes because both ARE real json tags on ui.Network.
		//
		// SEVEN OF THE TEN ARE CONDITIONAL, WHICH IS THE MAJORITY. Only
		// x_wireguard_private_key, wireguard_interface and
		// wireguard_client_preshared_key_enabled are assigned on every path
		// through Encode; everything else sits behind the dns_servers guard,
		// the configuration-or-peer switch, or the preshared-key-enabled test.
		// A first pass declared two and the other five were left masked with
		// nothing behind them.
		ConditionalWires: map[string]func(types.Object) bool{
			"dhcpd_dns_1":                      vpnClientWireguardWritesDNS,
			"dhcpd_dns_2":                      vpnClientWireguardWritesDNS,
			"wireguard_client_mode":            vpnClientWireguardWritesPeer,
			"wireguard_client_peer_public_key": vpnClientWireguardWritesPeer,
			"wireguard_client_peer_ip":         vpnClientWireguardWritesPeer,
			"wireguard_client_peer_port":       vpnClientWireguardWritesPeer,
			"wireguard_client_preshared_key":   vpnClientWireguardWritesPresharedKey,
		},
		Encode: encodeVPNClientWireguard,
		Decode: decodeVPNClientWireguard,
	}
}

// vpnClientWireguardWritesDNS reports whether Encode will write the two DNS
// wires for this object.
//
// IT ASKS THE SAME QUESTION Encode ASKS, and the two must not drift: Encode
// writes them when dns_servers is set, and ALSO when a configuration file
// supplies DNS and dns_servers is null. The second case cannot be judged
// without parsing the file, so this answers true whenever a configuration is
// present -- masking a wire Encode might write is safe, and failing to mask one
// it did write is the silent drop.
func vpnClientWireguardWritesDNS(object types.Object) bool {
	attributes := object.Attributes()
	if servers, ok := attributes["dns_servers"].(types.List); ok &&
		!servers.IsNull() && !servers.IsUnknown() {
		return true
	}
	configuration, ok := attributes["configuration"].(types.Object)
	return ok && !configuration.IsNull() && !configuration.IsUnknown()
}

// vpnClientWireguardWritesPeer reports whether Encode will write the four wires
// of the manual-mode switch. Both arms write all four: the configuration arm
// derives them from the parsed file, the peer arm copies them from the block.
// Neither arm runs when the practitioner supplied neither.
func vpnClientWireguardWritesPeer(object types.Object) bool {
	attributes := object.Attributes()
	if configuration, ok := attributes["configuration"].(types.Object); ok &&
		!configuration.IsNull() && !configuration.IsUnknown() {
		return true
	}
	peer, ok := attributes["peer"].(types.Object)
	return ok && !peer.IsNull() && !peer.IsUnknown()
}

// vpnClientWireguardWritesPresharedKey reports whether Encode will write the
// key itself. It writes it when preshared_key_enabled is true, and ALSO from a
// configuration file that carries one -- which cannot be judged without parsing,
// so a configuration present answers true. Over-masking a wire Encode might
// write is safe; failing to mask one it did write is the silent drop.
func vpnClientWireguardWritesPresharedKey(object types.Object) bool {
	attributes := object.Attributes()
	if enabled, ok := attributes["preshared_key_enabled"].(types.Bool); ok &&
		enabled.ValueBool() {
		return true
	}
	configuration, ok := attributes["configuration"].(types.Object)
	return ok && !configuration.IsNull() && !configuration.IsUnknown()
}

func encodeVPNClientWireguard(
	ctx context.Context,
	object types.Object,
	network *ui.Network,
) diag.Diagnostics {
	var diags diag.Diagnostics
	var wireguard wireguardModel
	diags.Append(object.As(ctx, &wireguard, basetypes.ObjectAsOptions{})...)
	if diags.HasError() {
		return diags
	}

	network.WireguardPrivateKey = wireguard.PrivateKey.ValueStringPointer()
	network.WireguardClientPresharedKeyEnabled = wireguard.PresharedKeyEnabled.ValueBool()
	network.WireguardInterface = wireguard.Interface.ValueStringPointer()

	if !wireguard.DnsServers.IsNull() && !wireguard.DnsServers.IsUnknown() {
		var dnsServers []string
		diags.Append(wireguard.DnsServers.ElementsAs(ctx, &dnsServers, false)...)
		if diags.HasError() {
			return diags
		}
		wireguardDNSServersToNetwork(dnsServers, network)
	}

	switch {
	case !wireguard.Configuration.IsNull() && !wireguard.Configuration.IsUnknown():
		var config wireguardConfigurationModel
		diags.Append(wireguard.Configuration.As(ctx, &config, basetypes.ObjectAsOptions{})...)
		if diags.HasError() {
			return diags
		}
		parsed, err := parseWireGuardBase64Config(config.Content.ValueString())
		if err != nil {
			diags.AddError("Invalid WireGuard Configuration File",
				"Failed to parse WireGuard configuration: "+err.Error())
			return diags
		}
		network.WireguardClientMode = util.Ptr("manual")
		network.WireguardClientPeerPublicKey = util.Ptr(parsed.PublicKey)
		network.WireguardClientPeerIP = util.Ptr(parsed.EndpointIP)
		network.WireguardClientPeerPort = util.Ptr(parsed.EndpointPort)
		if parsed.PrivateKey != "" &&
			(wireguard.PrivateKey.IsNull() || wireguard.PrivateKey.IsUnknown()) {
			network.WireguardPrivateKey = util.Ptr(parsed.PrivateKey)
		}
		if parsed.PresharedKey != "" {
			network.WireguardClientPresharedKeyEnabled = true
			network.WireguardClientPresharedKey = util.Ptr(parsed.PresharedKey)
		}
		if len(parsed.DNS) > 0 && wireguard.DnsServers.IsNull() {
			wireguardDNSServersToNetwork(parsed.DNS, network)
		}
	case !wireguard.Peer.IsNull() && !wireguard.Peer.IsUnknown():
		var peer wireguardPeerModel
		diags.Append(wireguard.Peer.As(ctx, &peer, basetypes.ObjectAsOptions{})...)
		if diags.HasError() {
			return diags
		}
		network.WireguardClientMode = util.Ptr("manual")
		wireguardPeerToNetwork(peer, network)
	}

	if wireguard.PresharedKeyEnabled.ValueBool() {
		network.WireguardClientPresharedKey = wireguard.PresharedKey.ValueStringPointer()
	}
	return diags
}

// decodeVPNClientWireguard builds the object from WHAT THE CONTROLLER RETURNS,
// which is not all of it.
//
// x_wireguard_private_key and wireguard_client_preshared_key are write-only: the
// controller never sends them back, and the hand-written read path carries them
// forward from PRIOR STATE.
//
// AN EARLIER VERSION OF THIS COMMENT SAID AfterReceive IS WHERE A SURFACE
// EXPRESSES THAT, AND THAT IS WRONG. Read runs Spec.ToModel and THEN afterReceive
// -- resource.go:450-451 -- so by the time the hook sees the model, every
// attribute a Field decodes has already been overwritten. The rule that does hold
// is narrower: an attribute NO FIELD TOUCHES keeps its prior value, which is how
// device's port_override survives. An attribute a Field decodes does not.
//
// THE PRIOR VALUE IS STILL REACHABLE, ONE LEVEL IN. Spec.ToModel passes the model
// that was loaded from state, so at the moment a field's ToModel runs,
// *f.Model(model) is still the prior object. The kind cannot use it because
// Decode's signature is (ctx, *S) and never sees the model.
//
// SO THE SEAM IS ScatteredObjectField.Decode, NOT A HOOK, and closing it means
// giving that one kind's Decode the prior object rather than widening anything
// shared. Until then these decode null, which is visible and wrong in the safe
// direction -- a refresh blanks two secrets in state rather than inventing values
// for them -- and vpn_client cannot be cut over on this field alone. Recorded
// here because this file is the worked example for the kind.
func decodeVPNClientWireguard(
	ctx context.Context,
	network *ui.Network,
) (types.Object, diag.Diagnostics) {
	var diags diag.Diagnostics
	value := wireguardModel{
		PrivateKey:          types.StringNull(),
		Configuration:       types.ObjectNull(wireguardConfigurationModel{}.AttributeTypes()),
		Peer:                types.ObjectNull(wireguardPeerModel{}.AttributeTypes()),
		PresharedKeyEnabled: types.BoolValue(network.WireguardClientPresharedKeyEnabled),
		PresharedKey:        types.StringNull(),
		Interface:           types.StringPointerValue(network.WireguardInterface),
		DnsServers:          wireguardDNSServersFromNetwork(ctx, &diags, network),
	}
	object, d := types.ObjectValueFrom(ctx, value.AttributeTypes(), value)
	diags.Append(d...)
	return object, diags
}
