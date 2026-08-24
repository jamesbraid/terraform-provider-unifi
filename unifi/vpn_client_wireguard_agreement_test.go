package unifi

import (
	"context"
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
func TestWireguardConditionalWiresAgreeWithEncode(t *testing.T) {
	objects := []types.Object{
		wireguardObjectWithDNS(t, []string{"1.1.1.1", "8.8.8.8"}), // both written
		wireguardObjectWithDNS(t, []string{"1.1.1.1"}),            // only the first
		wireguardObjectWithDNS(t, nil),                            // neither
		wireguardObjectWithPeer(t),                                // the four peer wires
		wireguardObjectWithPresharedKey(t),                        // the key itself
	}
	problems := resourcekit.ConditionalWireProblems(
		vpnClientWireguardField(), objects,
		// The encoder dispatches on Purpose and a zero Network cannot marshal
		// at all, so the discriminator is supplied the way maskedBody requires.
		func(n *ui.Network) { n.Purpose = ui.PurposeVPNClient },
	)
	for _, problem := range problems {
		t.Errorf("%s", problem)
	}
}
