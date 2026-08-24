package unifi

import (
	"context"
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

// The regression test for the match-flags fix, which landed without one:
// that commit declared the eight nested match flags and made the endpoint
// mappers carry the model's value instead of rebuilding a Go zero, fixing a
// defect where an apply that changed only the description inverted a
// firewall rule -- "block everything except these addresses" silently
// became "block only these".
//
// Nothing on main would have caught its loss: the unit tests assert the
// mapper's output for a model carrying false, which is what the broken code
// produced too, so they hold either way. Only an apply against a controller
// separates the two, which is what this test is.
//
// The symptom also changed as part of the fix: with the flags declared
// Optional+Computed+UseStateForUnknown, a mapper that resets one to false
// now trips the framework's own consistency check
// (".source.match_opposite_ips: was cty.True, but now cty.False") instead of
// silently reverting it -- the schema half guards the runtime half.

// TestAnUnrelatedApplyInvertsAFirewallRule closes the chain: the four match
// flags are force-emitted, unmodelled, and held by the controller, so the
// only remaining question is whether an apply resets them.
//
// The config sets everything the flag depends on (matching_target,
// matching_target_type, ips), so the out-of-band change is the flag alone
// -- otherwise the apply would legitimately rewrite the target and the
// reset would prove nothing.
//
// A mask can't reach it: a field mask names top-level keys, so naming
// source sends the whole nested object, with no way to send
// source.zone_id but not source.match_opposite_ips.
func TestAnUnrelatedApplyInvertsAFirewallRule(t *testing.T) {
	var policyID string

	setFlagOutOfBand := func() {
		client, site := probeClient(t)
		ctx := context.Background()
		policies, err := client.ListFirewallPolicy(ctx, site)
		if err != nil {
			t.Fatal(err)
		}
		for i := range policies {
			if policies[i].Name == "tfacc-nested-flag-victim" {
				policyID = policies[i].ID
				break
			}
		}
		if policyID == "" {
			t.Fatal("the policy the provider created is not on the controller")
		}
		target, err := client.GetFirewallPolicy(ctx, site, policyID)
		if err != nil {
			t.Fatal(err)
		}
		target.Source.MatchOppositeIPs = true
		if _, err := client.UpdateFirewallPolicy(ctx, site, target); err != nil {
			t.Skipf("the controller refused the flag (%v)", err)
		}
		back, err := client.GetFirewallPolicy(ctx, site, policyID)
		if err != nil {
			t.Fatal(err)
		}
		if !back.Source.MatchOppositeIPs {
			t.Skip("the controller did not store source.match_opposite_ips, so there is " +
				"nothing for the apply to destroy")
		}
		t.Log("POSITIVE CONTROL: controller holds source.match_opposite_ips=true")
	}

	check := func(*terraform.State) error {
		client, site := probeClient(t)
		back, err := client.GetFirewallPolicy(context.Background(), site, policyID)
		if err != nil {
			return err
		}
		t.Logf("after the apply: source.match_opposite_ips=%v (matching_target=%q ips=%v)",
			back.Source.MatchOppositeIPs, back.Source.MatchingTarget, back.Source.IPs)
		if !back.Source.MatchOppositeIPs {
			return fmt.Errorf(
				"THIS FIREWALL RULE NOW MATCHES THE OPPOSITE TRAFFIC.\n"+
					"    Before the apply it matched everything EXCEPT %v.\n"+
					"    It now matches EXACTLY %v and nothing else.\n"+
					"    The rule is still present, still enabled, and still names the same\n"+
					"    addresses -- only the inversion is gone, so it reads as correct and\n"+
					"    enforces the complement of what it was written for.\n\n"+
					"    The only change in the config was the description. The provider was\n"+
					"    never asked to touch source.match_opposite_ips: it is unmodelled and\n"+
					"    force-emitted, so every apply sends false.\n\n"+
					"    THIS IS A REGRESSION OF THE MATCH-FLAGS FIX. The four assignments in\n"+
					"    endpointModelToSource are what keep the model's value on the wire;\n"+
					"    without them the mapper rebuilds a Go zero and the flag is lost.",
				back.Source.IPs, back.Source.IPs)
		}
		return nil
	}

	config := func(description string) string {
		return fmt.Sprintf(`
resource "unifi_firewall_zone" "z" {
	name = "tfacc-nested-flag-zone"
}

resource "unifi_firewall_policy" "victim" {
	name        = "tfacc-nested-flag-victim"
	action      = "ALLOW"
	description = %q
	source = {
		zone_id              = unifi_firewall_zone.z.id
		matching_target      = "IP"
		ips                  = ["10.60.60.0/24"]
		port_matching_type   = "ANY"
	}
	destination = {
		zone_id            = unifi_firewall_zone.z.id
		matching_target    = "ANY"
		port_matching_type = "ANY"
	}
}
`, description)
	}

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{Config: config("before")},
			{PreConfig: setFlagOutOfBand, Config: config("after"), Check: check},
		},
	})
}
