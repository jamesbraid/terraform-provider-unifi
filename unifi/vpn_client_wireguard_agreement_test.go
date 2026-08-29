package unifi

import (
	"context"
	"maps"
	"slices"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/resourcekit"
)

func wireguardObjectWithDNS(t *testing.T, servers []string) types.Object {
	t.Helper()
	ctx := context.Background()
	list := types.ListNull(types.StringType)
	if servers != nil {
		var diags interface{ HasError() bool }
		value, d := types.ListValueFrom(ctx, types.StringType, servers)
		diags = d
		if diags.HasError() {
			t.Fatalf("building the dns list: %v", d)
		}
		list = value
	}
	value := wireguardModel{
		PrivateKey:          types.StringValue("privkey"),
		Configuration:       types.ObjectNull(wireguardConfigurationModel{}.AttributeTypes()),
		Peer:                types.ObjectNull(wireguardPeerModel{}.AttributeTypes()),
		PresharedKeyEnabled: types.BoolValue(false),
		PresharedKey:        types.StringNull(),
		Interface:           types.StringValue("wan"),
		DnsServers:          list,
	}
	object, d := types.ObjectValueFrom(ctx, value.AttributeTypes(), value)
	if d.HasError() {
		t.Fatalf("building the wireguard object: %v", d)
	}
	return object
}

// wireguardObjectWithPeer supplies the manual-mode branch, which is the only
// path that writes the four peer and mode wires.
func wireguardObjectWithPeer(t *testing.T) types.Object {
	t.Helper()
	ctx := context.Background()
	peer, d := types.ObjectValueFrom(ctx, wireguardPeerModel{}.AttributeTypes(), wireguardPeerModel{
		IP:        types.StringValue("203.0.113.1"),
		Port:      types.Int64Value(51820),
		PublicKey: types.StringValue("pubkey"),
	})
	if d.HasError() {
		t.Fatalf("building the peer object: %v", d)
	}
	value := wireguardModel{
		PrivateKey:          types.StringValue("privkey"),
		Configuration:       types.ObjectNull(wireguardConfigurationModel{}.AttributeTypes()),
		Peer:                peer,
		PresharedKeyEnabled: types.BoolValue(false),
		PresharedKey:        types.StringNull(),
		Interface:           types.StringValue("wan"),
		DnsServers:          types.ListNull(types.StringType),
	}
	object, d := types.ObjectValueFrom(ctx, value.AttributeTypes(), value)
	if d.HasError() {
		t.Fatalf("building the wireguard object: %v", d)
	}
	return object
}

// wireguardObjectWithPresharedKey turns the flag on, which is the branch that
// writes the key itself rather than only the flag.
func wireguardObjectWithPresharedKey(t *testing.T) types.Object {
	t.Helper()
	ctx := context.Background()
	value := wireguardModel{
		PrivateKey:          types.StringValue("privkey"),
		Configuration:       types.ObjectNull(wireguardConfigurationModel{}.AttributeTypes()),
		Peer:                types.ObjectNull(wireguardPeerModel{}.AttributeTypes()),
		PresharedKeyEnabled: types.BoolValue(true),
		PresharedKey:        types.StringValue("psk"),
		Interface:           types.StringValue("wan"),
		DnsServers:          types.ListNull(types.StringType),
	}
	object, d := types.ObjectValueFrom(ctx, value.AttributeTypes(), value)
	if d.HasError() {
		t.Fatalf("building the wireguard object: %v", d)
	}
	return object
}

// ConditionalWires is a second list that has to agree with a decision
// already made inside Encode; a wire no object here exercises is reported
// as unchecked rather than passing.
//
// dhcpd_dns_1 and dhcpd_dns_2 are excluded: both now share ONE predicate
// (vpnClientWireguardWritesDNS(1), "does something supply at least one DNS
// server"), on purpose -- vpnClientClearDroppedDNS needs dhcpd_dns_2 in the
// mask even for a one-server apply, to clear a second server prior held, so
// its mask eligibility can no longer track Encode's own per-slot write
// decision the way every other wire here still does. Their masking is now a
// joint decision between Encode, vpnClientClearDroppedDNS and
// vpnClientUnwritableWires, which this Encode-only check has no way to
// model. TestWireguardDNSServersFillTwoSlotsInOrder and
// TestWireguardDNSServersClearTheSlotDropped cover that pair directly.
func TestWireguardConditionalWiresAgreeWithEncode(t *testing.T) {
	objects := []types.Object{
		wireguardObjectWithDNS(t, []string{"1.1.1.1", "8.8.8.8"}), // both written
		wireguardObjectWithDNS(t, []string{"1.1.1.1"}),            // only the first
		wireguardObjectWithDNS(t, nil),                            // neither
		wireguardObjectWithPeer(t),                                // the four peer wires
		wireguardObjectWithPresharedKey(t),                        // the key itself
	}
	field := vpnClientWireguardField()
	field.Wires = slices.DeleteFunc(slices.Clone(field.Wires), func(name string) bool {
		return name == "dhcpd_dns_1" || name == "dhcpd_dns_2"
	})
	field.ConditionalWires = maps.Clone(field.ConditionalWires)
	delete(field.ConditionalWires, "dhcpd_dns_1")
	delete(field.ConditionalWires, "dhcpd_dns_2")
	problems := resourcekit.ConditionalWireProblems(
		field, objects,
		// The encoder dispatches on Purpose and a zero Network cannot marshal
		// at all, so the discriminator is supplied the way maskedBody requires.
		func(n *ui.Network) { n.Purpose = ui.PurposeVPNClient },
	)
	for _, problem := range problems {
		t.Errorf("%s", problem)
	}
}
