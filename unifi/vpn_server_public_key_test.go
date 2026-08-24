package unifi

import (
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// TestAccVPNServerWireguardPublicKeyIsPopulated asserts the shape of the
// derived public key rather than a constant: computing the expected value
// here would just test the same arithmetic twice. The known-answer check
// against RFC 7748 lives in wireguard_key_test.go; this only checks the
// value reaches state at all.
func TestAccVPNServerWireguardPublicKeyIsPopulated(t *testing.T) {
	base64Key := regexp.MustCompile(`^[A-Za-z0-9+/]{42}[A-Za-z0-9+/=]{2}$`)

	config := `
resource "unifi_vpn_server" "pubkey" {
  name   = "tfacc-wg-pubkey"
  subnet = "10.183.0.1/24"

  wireguard = {
    private_key = "WPiBa/Ak1W+8Sp8L5yvbyhHeRO2o5kJvihq2VtJ+kFg="
    port        = 51840
  }
}
`
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: config,
				Check: resource.ComposeTestCheckFunc(
					resource.TestMatchResourceAttr(
						"unifi_vpn_server.pubkey", "wireguard.public_key", base64Key),
				),
			},
			{
				// Checks the value survives an update: the controller
				// returns no public key on read, so it must be re-derived.
				Config: config,
				Check: resource.ComposeTestCheckFunc(
					resource.TestMatchResourceAttr(
						"unifi_vpn_server.pubkey", "wireguard.public_key", base64Key),
				),
			},
		},
	})
}
