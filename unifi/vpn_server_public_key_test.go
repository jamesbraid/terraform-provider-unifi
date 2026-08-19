package unifi

import (
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// TestAccVPNServerWireguardPublicKeyIsPopulated is #209's assertion: the
// attribute the schema promises must hold a value.
//
// IT ASSERTS A SHAPE RATHER THAN A CONSTANT, deliberately. The expected key is
// a function of the private key in the config, and writing the answer in would
// make this a test of my own arithmetic in two places. The known-answer check
// against RFC 7748 lives in wireguard_key_test.go, where it can use a published
// vector; here the question is whether the value reaches state at all, which is
// what was broken -- null on create, null after update, null forever.
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
				// The second apply is the half that was never reached: the
				// controller returns no public key on a read either, so a
				// value that survives an update is one the provider derived
				// both times rather than one it happened to hold.
				Config: config,
				Check: resource.ComposeTestCheckFunc(
					resource.TestMatchResourceAttr(
						"unifi_vpn_server.pubkey", "wireguard.public_key", base64Key),
				),
			},
		},
	})
}
