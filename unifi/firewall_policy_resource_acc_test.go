package unifi

// Acceptance tests only -- see the note in site_to_site_vpn_resource_acc_test.go.
// This file exists so the scenario can be grafted onto the released tree
// without dragging firewall_policy_resource_test.go's unit tests with it.

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// TestAccFirewallPolicyFramework_basic exercises create, read, update,
// import and delete for the managed surface, which otherwise has no
// acceptance test of its own -- its only other TestAcc function is the
// list resource's empty-or-seeded query, which creates nothing.
//
// A policy needs a zone, and a zone needs a network, so the config seeds
// both. The subnet and VLAN are deliberately unusual to avoid colliding
// with anything a seeded controller already defines.
//
// This test's fixture depends on the network surface working against a
// live controller: if network is broken, this fails on its own fixture
// setup and proves nothing about firewall_policy.
func TestAccFirewallPolicyFramework_basic(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccFirewallPolicyAccConfig("tf-acc-fwpolicy", "BLOCK"),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet("unifi_firewall_policy.test", "id"),
					resource.TestCheckResourceAttr(
						"unifi_firewall_policy.test", "name", "tf-acc-fwpolicy"),
					resource.TestCheckResourceAttr(
						"unifi_firewall_policy.test", "action", "BLOCK"),
					resource.TestCheckResourceAttrPair(
						"unifi_firewall_policy.test", "source.zone_id",
						"unifi_firewall_zone.test", "id"),
				),
			},
			{
				Config: testAccFirewallPolicyAccConfig("tf-acc-fwpolicy-2", "REJECT"),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"unifi_firewall_policy.test", "name", "tf-acc-fwpolicy-2"),
					resource.TestCheckResourceAttr(
						"unifi_firewall_policy.test", "action", "REJECT"),
				),
			},
			{
				ResourceName:      "unifi_firewall_policy.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

func testAccFirewallPolicyAccConfig(name, action string) string {
	return fmt.Sprintf(`
resource "unifi_network" "test" {
  name   = "tf-acc-fwpolicy-net"
  subnet = "10.181.0.1/24"
  vlan   = 181
}

resource "unifi_firewall_zone" "test" {
  name        = "tf-acc-fwpolicy-zone"
  network_ids = [unifi_network.test.id]
}

resource "unifi_firewall_policy" "test" {
  name     = %q
  action   = %q
  protocol = "all"

  source = {
    zone_id         = unifi_firewall_zone.test.id
    matching_target = "ANY"
  }

  destination = {
    zone_id         = unifi_firewall_zone.test.id
    matching_target = "ANY"
  }
}
`, name, action)
}
