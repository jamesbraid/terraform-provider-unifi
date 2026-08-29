package unifi

import (
	"context"
	"fmt"
	"sort"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
)

// TestEachDHCPFlagWithItsOperand turns the dhcp_server floor established
// elsewhere into a total. The earlier sweep set each boolean alone and kept
// whatever the controller accepted; three were refused, each naming what
// was missing:
//
//	dhcpd_boot_enabled     BootFilenameInvalid
//	dhcpd_ntp_enabled      NtpAddressInvalid
//	dhcpd_gateway_enabled  (absent from both lists)
//
// Those were recorded as unmeasurable, but a flag that switches a feature
// on needs the feature configured -- the same shape as ip_aliases needing a
// CIDR, or match_opposite_ips needing a target list. So every flag here is
// set together with its operand, making the at-risk set and the apply's
// loss count real rather than a lower bound from a fixture that couldn't
// turn the features on.
func TestEachDHCPFlagWithItsOperand(t *testing.T) {
	var networkID string
	establishedBefore := map[string]bool{}

	// Each entry sets the flag and everything that flag operates on. A pointer
	// operand is set through a local so the address is stable.
	bootFile, bootServer := "pxelinux.0", "10.76.76.2"
	ntp1, gateway := "10.76.76.3", "10.76.76.4"
	wins1 := "10.76.76.5"
	offset := int64(3600)
	dns1 := "10.76.76.6"

	flags := []struct {
		name    string
		set     func(*ui.Network)
		read    func(*ui.Network) bool
		operand string
	}{
		{"dhcpd_dns_enabled", func(n *ui.Network) {
			n.DHCPDDNSEnabled = true
			n.DHCPDDNS1 = &dns1
		}, func(n *ui.Network) bool { return n.DHCPDDNSEnabled }, "dhcpd_dns_1"},

		{"dhcpd_conflict_checking", func(n *ui.Network) {
			n.DHCPDConflictChecking = true
		}, func(n *ui.Network) bool { return n.DHCPDConflictChecking }, "none"},

		{"dhcpd_boot_enabled", func(n *ui.Network) {
			n.DHCPDBootEnabled = true
			n.DHCPDBootFilename = &bootFile
			n.DHCPDBootServer = bootServer
		}, func(n *ui.Network) bool { return n.DHCPDBootEnabled }, "dhcpd_boot_filename + dhcpd_boot_server"},

		{"dhcpd_ntp_enabled", func(n *ui.Network) {
			n.DHCPDNtpEnabled = true
			n.DHCPDNtp1 = &ntp1
		}, func(n *ui.Network) bool { return n.DHCPDNtpEnabled }, "dhcpd_ntp_1"},

		{"dhcpd_gateway_enabled", func(n *ui.Network) {
			n.DHCPDGatewayEnabled = true
			n.DHCPDGateway = &gateway
		}, func(n *ui.Network) bool { return n.DHCPDGatewayEnabled }, "dhcpd_gateway"},

		{"dhcpd_time_offset_enabled", func(n *ui.Network) {
			n.DHCPDTimeOffsetEnabled = true
			n.DHCPDTimeOffset = &offset
		}, func(n *ui.Network) bool { return n.DHCPDTimeOffsetEnabled }, "dhcpd_time_offset"},

		{"dhcpd_wins_enabled", func(n *ui.Network) {
			n.DHCPDWinsEnabled = true
			n.DHCPDWins1 = &wins1
		}, func(n *ui.Network) bool { return n.DHCPDWinsEnabled }, "dhcpd_wins_1"},
	}

	setDHCPOutOfBand := func() {
		client, site := probeClient(t)
		ctx := context.Background()
		networks, err := client.ListNetwork(ctx, site)
		if err != nil {
			t.Fatal(err)
		}
		for i := range networks {
			if networks[i].Name != nil && *networks[i].Name == "tfacc-dhcp-operand-victim" {
				networkID = networks[i].ID
				break
			}
		}
		if networkID == "" {
			t.Fatal("the network the provider created is not on the controller")
		}

		base, err := client.GetNetwork(ctx, site, networkID)
		if err != nil {
			t.Fatal(err)
		}
		base.DHCPDEnabled = true
		base.DHCPDDNS1 = &dns1
		if _, err := client.UpdateNetwork(ctx, site, base); err != nil {
			t.Fatalf("the controller refused dhcpd_enabled with a DNS server: %v", err)
		}

		// ONE AT A TIME STILL, so a refusal names one flag rather than the set.
		for _, flag := range flags {
			current, err := client.GetNetwork(ctx, site, networkID)
			if err != nil {
				t.Fatal(err)
			}
			flag.set(current)
			if _, err := client.UpdateNetwork(ctx, site, current); err != nil {
				t.Logf("  REFUSED  %s even with %s: %v", flag.name, flag.operand, err)
				continue
			}
		}

		back, err := client.GetNetwork(ctx, site, networkID)
		if err != nil {
			t.Fatal(err)
		}
		if !back.DHCPDEnabled {
			t.Fatal("the controller did not store dhcpd_enabled, so there is nothing for " +
				"the apply to destroy and this would pass vacuously")
		}
		var established []string
		for _, flag := range flags {
			if flag.read(back) {
				establishedBefore[flag.name] = true
				established = append(established, flag.name)
			}
		}
		sort.Strings(established)
		t.Logf("POSITIVE CONTROL: dhcpd_enabled=true and %d of %d flags established: %v",
			len(established), len(flags), established)

		// The point of the exercise. If the three that the earlier sweep could
		// not turn on are still off, the operand theory is wrong and the floor
		// stands.
		for _, name := range []string{"dhcpd_boot_enabled", "dhcpd_ntp_enabled", "dhcpd_gateway_enabled"} {
			if !establishedBefore[name] {
				t.Logf("  STILL UNESTABLISHED: %s -- its operand did not make it settable, "+
					"so it stays out of the total rather than counted as safe", name)
			}
		}
	}

	checkAfterApply := func(*terraform.State) error {
		client, site := probeClient(t)
		back, err := client.GetNetwork(context.Background(), site, networkID)
		if err != nil {
			return err
		}
		var lost, kept []string
		for _, flag := range flags {
			if !establishedBefore[flag.name] {
				continue
			}
			if flag.read(back) {
				kept = append(kept, flag.name)
			} else {
				lost = append(lost, flag.name)
			}
		}
		sort.Strings(lost)
		sort.Strings(kept)
		t.Logf("after an apply that changed only the vlan:")
		t.Logf("  kept %d %v", len(kept), kept)
		t.Logf("  lost %d %v", len(lost), lost)
		t.Logf("  operands: boot_filename=%v boot_server=%q ntp_1=%v gateway=%v wins_1=%v time_offset=%v",
			back.DHCPDBootFilename, back.DHCPDBootServer, back.DHCPDNtp1,
			back.DHCPDGateway, back.DHCPDWins1, back.DHCPDTimeOffset)

		if len(kept)+len(lost) == 0 {
			return fmt.Errorf("no flag was established, so this apply measured nothing")
		}
		if len(lost) > 0 {
			return fmt.Errorf(
				"an apply whose only change was the vlan reset %d of %d established DHCP "+
					"flag(s): %v.\n"+
					"    The provider was never asked to touch dhcp_server.\n"+
					"    EXPECTED TO FAIL UNTIL THE dhcp_server GUARD DEFECT IS FIXED.",
				len(lost), len(kept)+len(lost), lost)
		}
		return nil
	}

	config := func(vlan int) string {
		return fmt.Sprintf(`
resource "unifi_network" "dhcp" {
	name    = "tfacc-dhcp-operand-victim"
	subnet  = "10.76.76.1/24"
	vlan    = %d
	enabled = true
}
`, vlan)
	}

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{Config: config(76)},
			{PreConfig: setDHCPOutOfBand, Config: config(81), Check: checkAfterApply},
		},
	})
}
