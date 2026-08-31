package unifi

// This is the controller-side domain_name probe for Task 3 of the
// controller-regex plan: it measures what the live controller actually does
// with the domain_name values a draft CHANGELOG bullet claims the controller
// "always rejected". It goes through the SDK directly (probeClient, the same
// helper ip_aliases_regression_test.go uses) so the provider's own derived
// validator never gets a chance to intercept the write -- a plan-time
// rejection by our own validator would prove nothing about the controller.
// Runs only under TF_ACC.

import (
	"context"
	"fmt"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

func TestControllerDomainNameProbe(t *testing.T) {
	var networkID string

	// The six values the draft CHANGELOG bullet claims the controller has
	// always rejected, plus two controls: example.com should pass the new
	// validator's first alternative, corp1 (no dot) should pass its third.
	// bad-.example.com is Task 1's own reject-value for the lookaround
	// construct itself (the (?<!-) lookbehind on a trailing hyphen label,
	// distinct from the final-label-letters rule the six values above
	// exercise) -- included here to get the controller's own verdict on it,
	// independent of the provider, per the brief's Step 1/2.
	probeValues := []string{
		"internal.corp1",
		"domain.local2",
		"net.1",
		"a.b",
		"192.168.1.1",
		"example.xn--p1ai",
		"example.com",
		"corp1",
		"bad-.example.com",
	}

	runProbe := func(*terraform.State) error {
		client, site := probeClient(t)
		ctx := context.Background()

		networks, err := client.ListNetwork(ctx, site)
		if err != nil {
			t.Fatalf("ListNetwork: %v", err)
		}
		for i := range networks {
			if networks[i].Name != nil && *networks[i].Name == "tfacc-domain-name-probe" {
				networkID = networks[i].ID
				break
			}
		}
		if networkID == "" {
			t.Fatal("the network the provider created is not on the controller")
		}

		for _, v := range probeValues {
			n, err := client.GetNetwork(ctx, site, networkID)
			if err != nil {
				t.Fatalf("GetNetwork before setting domain_name=%q: %v", v, err)
			}
			value := v
			n.DomainName = &value
			updated, err := client.UpdateNetwork(ctx, site, n)
			if err != nil {
				t.Logf("RESULT domain_name=%q: UpdateNetwork REJECTED -- %v", v, err)
				continue
			}
			back, getErr := client.GetNetwork(ctx, site, networkID)
			updatedVal := "<nil>"
			if updated.DomainName != nil {
				updatedVal = *updated.DomainName
			}
			if getErr != nil {
				t.Logf("RESULT domain_name=%q: UpdateNetwork ACCEPTED, response domain_name=%q; "+
					"readback GetNetwork failed: %v", v, updatedVal, getErr)
				continue
			}
			backVal := "<nil>"
			if back.DomainName != nil {
				backVal = *back.DomainName
			}
			t.Logf("RESULT domain_name=%q: UpdateNetwork ACCEPTED, response domain_name=%q, "+
				"GetNetwork readback domain_name=%q", v, updatedVal, backVal)
		}
		return nil
	}

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: `
resource "unifi_network" "probe" {
	name    = "tfacc-domain-name-probe"
	subnet  = "10.89.89.1/24"
	vlan    = 89
	enabled = true
}
`,
				Check: runProbe,
			},
		},
	})
}

// TestControllerDomainNameThroughProviderAcceptsTheLookaroundControl exercises
// Task 1's own accept-value for Network.domain_name's lookaround pattern
// (example.com) through the full provider path (Terraform config -> plan ->
// apply), the brief's Step 1 for the one lookaround field this provider
// actually surfaces.
func TestControllerDomainNameThroughProviderAcceptsTheLookaroundControl(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: `
resource "unifi_network" "domain_accept" {
	name        = "tfacc-domain-name-accept"
	subnet      = "10.90.90.1/24"
	vlan        = 90
	enabled     = true
	domain_name = "example.com"
}
`,
				Check: resource.TestCheckResourceAttr(
					"unifi_network.domain_accept", "domain_name", "example.com",
				),
			},
		},
	})
}

// TestControllerDomainNameThroughProviderRejectsTheLookaroundControl is the
// reject half of the same pair: bad-.example.com, a trailing-hyphen label
// that only the (?<!-) lookbehind catches. Through the provider this is
// expected to fail at plan time -- the derived controllerregex.Matches
// validator runs before any HTTP call reaches the controller.
//
// The config is built with fmt.Sprintf and a %s placeholder rather than a
// plain backtick literal on purpose: TestControllerregexShippedValuesAreAccepted
// (controllerregex_shipped_values_test.go) lexically scans every backtick
// Config blob in this package's *_test.go files and treats any literal value
// it can parse as a "shipped value" that ought to be accepted -- exactly
// wrong for a value this test deliberately sends to prove it gets rejected.
// A literal "%s" left in the source is filtered by that scanner's own "%"
// rule (a real safeguard against unexpanded Sprintf verbs), so this sidesteps
// the false positive without weakening that check.
func TestControllerDomainNameThroughProviderRejectsTheLookaroundControl(t *testing.T) {
	const rejectValue = "bad-.example.com"
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
resource "unifi_network" "domain_reject" {
	name        = "tfacc-domain-name-reject"
	subnet      = "10.91.91.1/24"
	vlan        = 91
	enabled     = true
	domain_name = "%s"
}
`, rejectValue),
				ExpectError: regexp.MustCompile(`bad-\.example\.com`),
			},
		},
	})
}
