package unifi

import (
	"context"
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

// The regression test for 96fa0dba, which landed without one.
//
// That commit declared the eight nested match flags and made the endpoint
// mappers carry the model's value instead of rebuilding a Go zero. It is the
// fix for a defect measured on a controller: an apply that changed only the
// description inverted a firewall rule, so "block everything except these
// addresses" silently became "block only these".
//
// NOTHING ON MAIN WOULD HAVE CAUGHT ITS LOSS. Reverting the four assignments in
// endpointModelToSource and endpointModelToDestination leaves the whole repo
// suite green -- 53 packages, 0 failures. The unit tests the fix updated assert
// the mapper's output for a model carrying false, which is what the broken code
// produced too, so they hold either way. Only an apply against a controller
// separates them, and this is that apply: with the assignments removed it fails,
// with them present it passes.
//
// THE SYMPTOM CHANGES, WHICH IS ITSELF PART OF THE FIX. Before 96fa0dba the
// flags were not in the schema, so the reset was silent. With them declared
// Optional+Computed+UseStateForUnknown, the prior true is carried into the plan,
// and a mapper that sends false now trips the framework's own consistency check:
//
//	.source.match_opposite_ips: was cty.True, but now cty.False
//
// So the schema half guards the runtime half. That is worth knowing before
// anyone treats the declarations as cosmetic and the assignments as the fix.

// TestAnUnrelatedApplyInvertsAFirewallRule closes task 198's chain: the
// four match flags are force-emitted, unmodelled, and held by the controller.
// The only remaining question is whether an apply resets them.
//
// THE CONFIG SETS EVERYTHING THE FLAG DEPENDS ON, so the out-of-band change is
// the flag ALONE. matching_target, matching_target_type and ips are all
// modelled, so a config that omits them would have the apply legitimately
// rewriting the target and the reset would prove nothing about the flag.
//
// A MASK CANNOT REACH IT, which is why the fix declares the flags instead. A
// field mask names top-level keys, so naming source sends the whole nested
// object -- there is no way to express "send source.zone_id but not
// source.match_opposite_ips".
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
					"    THIS IS A REGRESSION OF 96fa0dba. The four assignments in\n"+
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
