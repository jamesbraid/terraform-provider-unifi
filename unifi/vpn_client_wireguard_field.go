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

// vpnClientWireguardWires and vpnClientWireguardField read the shipped
// descriptor rather than declaring a second copy: a hand-enumerated list
// would miss dhcpd_dns_1/dhcpd_dns_2 (written from dns_servers, not
// wireguard-named) and could mistranscribe x_wireguard_private_key (the Go
// field is WireguardPrivateKey) -- a wrong wire name is accepted and silently
// no-ops rather than erroring.
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

// decodeVPNClientPeer builds the peer block from what the controller reports,
// and returns null when it reports no manual mode.
//
// The controller always reports manual mode, even for a config file
// converted to it on the way out, so this alone can't tell a peer config from
// a file config -- only prior state (vpnClientAfterReceive) can, and it
// overrides this answer for the file case.
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
// the SDK response.
//
// Both are Optional (private_key_wo can also supply the first), so echoing
// the controller's response unconditionally would leak a write-only secret
// through its sibling. Both default to null and are only adopted from the
// echo when prior is non-null AND the prior attribute itself is non-null --
// not just "the last apply used the write-only path" but also "there is a
// prior state at all". terraform import leaves prior null outright, on the
// object's first Read, which the old before.PrivateKey.IsNull() check never
// saw because it lived inside the `!prior.IsNull()` branch.
//
// private_key_wo_version has no wire -- it's an invented rotation counter so
// an unchanged write-only key still triggers an update -- so prior is the
// only place a bare refresh can get its value from.
func decodeVPNClientWireguard(
	ctx context.Context,
	network *ui.Network,
	prior types.Object,
) (types.Object, diag.Diagnostics) {
	var diags diag.Diagnostics

	privateKey := types.StringNull()
	presharedKey := types.StringNull()
	version := types.Int64Null()
	if !prior.IsNull() && !prior.IsUnknown() {
		var before wireguardModel
		diags.Append(prior.As(ctx, &before, basetypes.ObjectAsOptions{})...)
		if diags.HasError() {
			return types.ObjectNull(wireguardModel{}.AttributeTypes()), diags
		}
		if !before.PrivateKey.IsNull() {
			privateKey = types.StringPointerValue(network.WireguardPrivateKey)
		}
		if !before.PresharedKey.IsNull() {
			presharedKey = types.StringPointerValue(network.WireguardClientPresharedKey)
		}
		version = before.PrivateKeyWOVersion
	}

	value := wireguardModel{
		PrivateKey: privateKey,
		// Never read back, whatever the controller echoes: write-only means
		// write-only.
		PrivateKeyWO:        types.StringNull(),
		PrivateKeyWOVersion: version,
		Configuration:       types.ObjectNull(wireguardConfigurationModel{}.AttributeTypes()),
		Peer:                decodeVPNClientPeer(ctx, &diags, network),
		PresharedKeyEnabled: types.BoolValue(network.WireguardClientPresharedKeyEnabled),
		PresharedKey:        presharedKey,
		Interface:           types.StringPointerValue(network.WireguardInterface),
		DnsServers:          wireguardDNSServersFromNetwork(ctx, &diags, network),
	}
	object, d := types.ObjectValueFrom(ctx, value.AttributeTypes(), value)
	diags.Append(d...)
	return object, diags
}
