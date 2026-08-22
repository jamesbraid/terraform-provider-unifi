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

// ConditionalWires is a second list that has to agree with a decision already
// made inside Encode, and nothing checked the two until this did.
//
// THE CARDINALITY IS THE CASE THAT WAS WRONG. One shared predicate served both
// DNS wires and answered "is dns_servers set", while
// wireguardDNSServersToNetwork writes dhcpd_dns_1 when the list is non-empty and
// dhcpd_dns_2 only when it has a second entry. A practitioner supplying ONE
// server therefore had dhcpd_dns_2 masked, unwritten, and sent as "" -- the
// destruction ConditionalWires exists to prevent, surviving inside the remedy at
// a cardinality nobody exercised.
//
// THREE OBJECTS RATHER THAN TWO, so both wires are exercised in both directions:
// a false-only run passes for a predicate that always returns false, which masks
// nothing and silently drops every write.
func TestWireguardConditionalWiresAgreeWithEncode(t *testing.T) {
	objects := []types.Object{
		wireguardObjectWithDNS(t, []string{"1.1.1.1", "8.8.8.8"}), // both written
		wireguardObjectWithDNS(t, []string{"1.1.1.1"}),            // only the first
		wireguardObjectWithDNS(t, nil),                            // neither
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
