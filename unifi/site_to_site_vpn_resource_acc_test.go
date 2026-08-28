package unifi

// Acceptance tests only: every test drives the provider through HCL rather
// than reaching into internals, which is what makes this file safe to graft
// onto the released tree as a shared scenario owner --
// site_to_site_vpn_resource_test.go is full of unit tests bound to
// converted internals and would drag them onto a provider that doesn't have
// the types they reference.

import (
	"fmt"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// TestAccSiteToSiteVPNFramework_basic exercises create, read, update, import
// and delete for the managed surface, which otherwise has no acceptance
// test of its own. Uses the minimal documented config
// (examples/resources/unifi_site_to_site_vpn): no controller-side
// dependency to seed, unlike power_supervisor, which needs an adopted
// device the test controller never has.
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

// TestAccSiteToSiteVPNFramework_pfsDisable is the live counterpart to
// TestPFSAndDynamicRoutingAreEmittedUnconditionally (in
// site_to_site_vpn_resource_test.go): proof that a PFS/dynamic-routing
// disable reaches the controller, not just the descriptor's wire mask.
//
// State alone can't tell a real disable from one the controller silently
// ignored, since Update overlays a known plan value onto state
// unconditionally. The final PlanOnly step re-reads the controller
// independently of that overlay and would produce a nonempty plan if the
// disable hadn't actually stuck.
func TestAccSiteToSiteVPNFramework_pfsDisable(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccSiteToSiteVPNAccConfig_pfs("tf-acc-s2s-pfs", true, true),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"unifi_site_to_site_vpn.pfs", "pfs", "true"),
					resource.TestCheckResourceAttr(
						"unifi_site_to_site_vpn.pfs", "dynamic_routing", "true"),
				),
			},
			{
				Config: testAccSiteToSiteVPNAccConfig_pfs("tf-acc-s2s-pfs", false, false),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"unifi_site_to_site_vpn.pfs", "pfs", "false"),
					resource.TestCheckResourceAttr(
						"unifi_site_to_site_vpn.pfs", "dynamic_routing", "false"),
				),
			},
			{
				// The independent re-read: see the function comment.
				Config:   testAccSiteToSiteVPNAccConfig_pfs("tf-acc-s2s-pfs", false, false),
				PlanOnly: true,
			},
		},
	})
}

func testAccSiteToSiteVPNAccConfig_pfs(name string, pfs, dynamicRouting bool) string {
	return fmt.Sprintf(`
resource "unifi_site_to_site_vpn" "pfs" {
  name             = %q
  peer_ip          = "203.0.113.30"
  pre_shared_key   = "tf-acc-psk-not-a-real-secret"
  remote_subnets   = ["192.168.40.0/24"]
  pfs              = %t
  dynamic_routing  = %t
}
`, name, pfs, dynamicRouting)
}

// TestAccSiteToSiteVPNFramework_dynamicRoutingEmptySubnets is the live
// counterpart to Test_siteToSiteVPNRemoteSubnetsConfigValidator_ValidateResource
// -- but it records a live-measured correction to the #433 claim it set out
// to adopt, not the claim itself.
//
// The ConfigValidator this task added is schema-level only: it lets
// dynamic_routing=true, remote_subnets=[] past Terraform's own validation,
// matching upstream's #433 fix and this provider's triage row. It does NOT
// apply, though -- a live probe (unifi/zz_s2s_remote_subnets_probe_test.go,
// deleted after use) isolated why: go-unifi's marshalSiteVPN encoder tags
// RemoteVPNSubnets `json:"remote_vpn_subnets,omitempty"`
// (network_encode.go), and Go's omitempty drops a zero-length slice exactly
// like nil, so this provider can never put an explicit `[]` on the wire for
// this field -- only "entirely absent" or "one or more values". The probe
// confirmed the controller rejects a create with the key entirely absent
// with api.err.Invalid REGARDLESS of dynamic_routing (both
// dynamic_routing=true and dynamic_routing=false variants with nil
// RemoteVPNSubnets failed identically; both variants with one subnet
// succeeded), so this is not a dynamic-routing-specific rule the controller
// enforces -- it is a wire-level defect in how this SDK version encodes an
// empty list for this specific field, independent of the schema fix. Fixing
// it needs a go-unifi change (drop omitempty, matching how ip_aliases and
// ipv6_aliases already avoid it in marshalCorporate) and is out of scope
// here (no SDK bump in this task).
func TestAccSiteToSiteVPNFramework_dynamicRoutingEmptySubnets(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:      testAccSiteToSiteVPNAccConfig_dynamicRoutingNoSubnets("tf-acc-s2s-dynamic"),
				ExpectError: regexp.MustCompile(`api\.err\.Invalid`),
			},
		},
	})
}

func testAccSiteToSiteVPNAccConfig_dynamicRoutingNoSubnets(name string) string {
	return fmt.Sprintf(`
resource "unifi_site_to_site_vpn" "dynamic" {
  name             = %q
  peer_ip          = "203.0.113.50"
  pre_shared_key   = "tf-acc-psk-not-a-real-secret"
  remote_subnets   = []
  dynamic_routing  = true
}
`, name)
}
