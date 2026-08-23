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

// vpnClientWireguardWires and vpnClientWireguardField READ THE SHIPPED
// DESCRIPTOR rather than declaring a second copy.
//
// TEN WIRES, NOT THREE, AND THE TWO THAT ARE NOT WIREGUARD-NAMED ARE THE POINT.
// dhcpd_dns_1 and dhcpd_dns_2 are written by wireguardDNSServersToNetwork from
// the block's dns_servers list, so an author enumerating by grepping the SDK for
// "Wireguard" produces eight names, the mask omits two, and dns_servers becomes
// an attribute the practitioner can set and nothing writes.
//
// x_wireguard_private_key IS THE OTHER TRAP. The Go field is
// WireguardPrivateKey, so a name transcribed from the struct is
// wireguard_private_key -- an attribute unifi.Network does not have. A mask
// naming it is accepted and changes nothing, which is dns_record's `name` ->
// `key` again. WireNameProblems catches it; nothing else does.
//
// THE NAMES LIVE IN THE Spec LITERAL because the mapping reader parses a Fields
// entry as a composite literal and a helper returning one hides every name it
// declares. Reading them back from there is what keeps this from becoming the
// second list that has to agree with the first.
func vpnClientWireguardField() resourcekit.ScatteredObjectField[vpnClientResourceModel, ui.Network] {
	for _, field := range vpnClientKitSpec().Fields {
		if scattered, ok := field.(resourcekit.ScatteredObjectField[vpnClientResourceModel, ui.Network]); ok {
			return scattered
		}
	}
	panic("the vpn_client descriptor declares no scattered object field")
}

func vpnClientWireguardWires() []string { return vpnClientWireguardField().Wires }

func vpnClientWireguardWritesDNS(nth int) func(types.Object) bool {
	return func(object types.Object) bool {
		attributes := object.Attributes()
		if servers, ok := attributes["dns_servers"].(types.List); ok &&
			!servers.IsNull() && !servers.IsUnknown() {
			return len(servers.Elements()) >= nth
		}
		configuration, ok := attributes["configuration"].(types.Object)
		if !ok || configuration.IsNull() || configuration.IsUnknown() {
			return false
		}
		content, ok := configuration.Attributes()["content"].(types.String)
		if !ok || content.IsNull() || content.IsUnknown() {
			return false
		}
		parsed, err := parseWireGuardBase64Config(content.ValueString())
		if err != nil {
			// Encode surfaces the error and writes nothing, so nothing is
			// written here either.
			return false
		}
		return len(parsed.DNS) >= nth
	}
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
		// The mode goes with the peer: wireguardPeerToNetwork writes it.
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
// decodeVPNClientPeer builds the peer block from what the controller reports,
// and returns null when it reports no manual mode.
//
// THE CONTROLLER ALWAYS SAYS MANUAL. The provider converts a configuration file
// to manual mode on the way out, so this cannot tell a practitioner who wrote a
// peer block from one who supplied a file -- and answering "peer" for the second
// replaces their configuration on every refresh. Only prior state can tell them
// apart, which is what the descriptor's AfterReceive is for. This produces the
// answer for the peer case and AfterReceive overrides it for the other.
func decodeVPNClientPeer(
	ctx context.Context,
	diags *diag.Diagnostics,
	network *ui.Network,
) types.Object {
	if network.WireguardClientMode == nil || *network.WireguardClientMode != "manual" {
		return types.ObjectNull(wireguardPeerModel{}.AttributeTypes())
	}
	return wireguardPeerFromNetwork(ctx, diags, network)
}

// decodeVPNClientWireguard reads private_key and preshared_key straight off
// the SDK response. Measured live on 10.4.57: x_wireguard_private_key and
// wireguard_client_preshared_key both come back at full length for a
// manual-mode (peer) client, nil rather than "" when no preshared key is set
// -- the controller echoes both, the same as it does for vpn_server.
//
// HARDCODING NULL HERE WAS THE DEFECT this replaces. private_key is Required
// in the schema -- always a known value in the config -- so a Decode that
// always returned null for it disagreed with the config on every refresh:
// "+ private_key" forever. See TestWireguardFieldRoundTripsWhatTheControllerReturns
// in vpn_client_wireguard_field_test.go, which asserted the null and had to
// be corrected alongside this.
//
// vpnClientAfterReceive's file-mode carry-forward is unrelated and still
// applies afterward, for the case this cannot: a practitioner who configured
// a file rather than a peer, where the controller's stored representation of
// the parsed key need not match the file byte for byte.
func decodeVPNClientWireguard(
	ctx context.Context,
	network *ui.Network,
	_ types.Object,
) (types.Object, diag.Diagnostics) {
	var diags diag.Diagnostics
	value := wireguardModel{
		PrivateKey:          types.StringPointerValue(network.WireguardPrivateKey),
		Configuration:       types.ObjectNull(wireguardConfigurationModel{}.AttributeTypes()),
		Peer:                decodeVPNClientPeer(ctx, &diags, network),
		PresharedKeyEnabled: types.BoolValue(network.WireguardClientPresharedKeyEnabled),
		PresharedKey:        types.StringPointerValue(network.WireguardClientPresharedKey),
		Interface:           types.StringPointerValue(network.WireguardInterface),
		DnsServers:          wireguardDNSServersFromNetwork(ctx, &diags, network),
	}
	object, d := types.ObjectValueFrom(ctx, value.AttributeTypes(), value)
	diags.Append(d...)
	return object, diags
}
