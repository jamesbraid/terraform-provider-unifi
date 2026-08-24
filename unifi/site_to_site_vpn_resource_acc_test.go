package unifi

// Acceptance tests only. Nothing in this file reaches into provider internals:
// every test drives the provider through HCL, so the file is safe to graft onto
// the released tree as a shared scenario owner. That is why it is separate from
// site_to_site_vpn_resource_test.go, which is full of unit tests bound to
// converted internals -- grafting THAT file would drag them onto a provider
// that does not have the types they reference.

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// TestAccSiteToSiteVPNFramework_basic exercises create, read, update, import and
// delete for the managed surface, which has no acceptance test of its own --
// only the list resource's empty-collection query, which creates nothing.
//
// The config is the minimal documented one from
// examples/resources/unifi_site_to_site_vpn: name, peer_ip, pre_shared_key and
// remote_subnets. There is no controller-side dependency to seed, which is what
// makes this surface testable where power_supervisor is not: that one needs an
// adopted device, and the test controller adopts none.
func TestAccSiteToSiteVPNFramework_basic(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccSiteToSiteVPNAccConfig("tf-acc-s2s", "203.0.113.10", "192.168.20.0/24"),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet("unifi_site_to_site_vpn.test", "id"),
					resource.TestCheckResourceAttr(
						"unifi_site_to_site_vpn.test", "name", "tf-acc-s2s"),
					resource.TestCheckResourceAttr(
						"unifi_site_to_site_vpn.test", "peer_ip", "203.0.113.10"),
					resource.TestCheckResourceAttr(
						"unifi_site_to_site_vpn.test", "remote_subnets.#", "1"),
				),
			},
			// Update the name and the remote subnet in place.
			{
				Config: testAccSiteToSiteVPNAccConfig("tf-acc-s2s-2", "203.0.113.10", "192.168.30.0/24"),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"unifi_site_to_site_vpn.test", "name", "tf-acc-s2s-2"),
					resource.TestCheckResourceAttr(
						"unifi_site_to_site_vpn.test", "remote_subnets.0", "192.168.30.0/24"),
				),
			},
			// The pre-shared key is not recoverable on import -- the documented
			// behaviour -- so it is ignored rather than verified.
			{
				ResourceName:            "unifi_site_to_site_vpn.test",
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"pre_shared_key"},
			},
		},
	})
}

func testAccSiteToSiteVPNAccConfig(name, peerIP, remoteSubnet string) string {
	return fmt.Sprintf(`
resource "unifi_site_to_site_vpn" "test" {
  name           = %q
  peer_ip        = %q
  pre_shared_key = "tf-acc-psk-not-a-real-secret"
  remote_subnets = [%q]
}
`, name, peerIP, remoteSubnet)
}
