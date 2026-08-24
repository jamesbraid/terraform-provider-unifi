package unifi

import (
	"context"
	"fmt"
	"testing"

	ui "github.com/ubiquiti-community/go-unifi/unifi"
)

// TestDoesTheControllerHoldEachSourceMatchFlag completes the match flags'
// field set: four flags live in FirewallPolicySource, all force-emitting,
// none modelled, and "source" is one wire key in the masked update, so
// every apply sends all four as false together. match_opposite_ips was
// already confirmed end to end (an apply that changed only the description
// inverted a rule); the other three are checked here for whether the
// controller holds them at all -- they're fields of the same struct written
// by the same assignment inside the same masked key, so if held, they're
// lost by the same mechanism already proved.
//
// Each flag needs its own policy: a source has one matching_target, and
// each flag inverts a different one (ips needs IP, networks needs NETWORK,
// mac needs a MAC list), so asking for all four on one policy asks for a
// source that can't exist.
func TestDoesTheControllerHoldEachSourceMatchFlag(t *testing.T) {
	client, site := probeClient(t)
	ctx := context.Background()

	zones, err := client.ListFirewallZone(ctx, site)
	if err != nil {
		t.Fatal(err)
	}
	if len(zones) == 0 {
		t.Skip("no firewall zone to hang a policy from")
	}
	zone := zones[0].ID

	networks, err := client.ListNetwork(ctx, site)
	if err != nil {
		t.Fatal(err)
	}
	if len(networks) == 0 {
		t.Skip("no network to point a NETWORK match at")
	}
	network := networks[0].ID

	// SPECIFIC, not OBJECT: OBJECT means the endpoint names a stored
	// IP-group object and demands an ip_group_id, which a literal list
	// doesn't have. firewallPolicyMatchingTargetType agrees -- OBJECT only
	// when an ip_group_id is present, SPECIFIC for any other non-ANY target.
	//
	// The classifier control below checks the known member
	// (match_opposite_ips), not just a count: match_opposite_networks held
	// under OBJECT too, so three of four verdicts alone would have looked
	// fine regardless.
	cases := []struct {
		flag string
		// source is built with the target the flag inverts, and nothing else.
		source func() *ui.FirewallPolicySource
		read   func(*ui.FirewallPolicySource) bool
	}{
		{
			flag: "match_opposite_ips",
			source: func() *ui.FirewallPolicySource {
				return &ui.FirewallPolicySource{
					ZoneID: zone, MatchingTarget: "IP", MatchingTargetType: "SPECIFIC",
					IPs: []string{"10.70.70.0/24"}, PortMatchingType: "ANY",
					MatchOppositeIPs: true,
				}
			},
			read: func(s *ui.FirewallPolicySource) bool { return s.MatchOppositeIPs },
		},
		{
			flag: "match_opposite_networks",
			source: func() *ui.FirewallPolicySource {
				return &ui.FirewallPolicySource{
					ZoneID: zone, MatchingTarget: "NETWORK", MatchingTargetType: "SPECIFIC",
					NetworkIDs: []string{network}, PortMatchingType: "ANY",
					MatchOppositeNetworks: true,
				}
			},
			read: func(s *ui.FirewallPolicySource) bool { return s.MatchOppositeNetworks },
		},
		// Two shapes for match_mac: the operand rule doesn't say which target
		// it operates on, and the SDK lists both CLIENT and MAC. A single
		// shape that stores false can't tell "the controller ignores this
		// flag" from "the probe paired it with the wrong target".
		{
			flag: "match_mac (CLIENT target)",
			source: func() *ui.FirewallPolicySource {
				return &ui.FirewallPolicySource{
					ZoneID: zone, MatchingTarget: "CLIENT", MatchingTargetType: "SPECIFIC",
					ClientMACs: []string{"00:11:22:33:44:55"}, PortMatchingType: "ANY",
					MatchMAC: true,
				}
			},
			read: func(s *ui.FirewallPolicySource) bool { return s.MatchMAC },
		},
		{
			flag: "match_mac (MAC target)",
			source: func() *ui.FirewallPolicySource {
				return &ui.FirewallPolicySource{
					ZoneID: zone, MatchingTarget: "MAC", MatchingTargetType: "SPECIFIC",
					ClientMACs: []string{"00:11:22:33:44:55"}, PortMatchingType: "ANY",
					MatchMAC: true,
				}
			},
			read: func(s *ui.FirewallPolicySource) bool { return s.MatchMAC },
		},
		{
			flag: "match_opposite_ports",
			source: func() *ui.FirewallPolicySource {
				return &ui.FirewallPolicySource{
					ZoneID: zone, MatchingTarget: "ANY",
					PortMatchingType: "SPECIFIC", Port: "8080",
					MatchOppositePorts: true,
				}
			},
			read: func(s *ui.FirewallPolicySource) bool { return s.MatchOppositePorts },
		},
	}

	held, refused, dropped := []string{}, []string{}, []string{}
	for index, testCase := range cases {
		policy := &ui.FirewallPolicy{
			Name:             fmt.Sprintf("tfacc-match-operand-%d", index),
			Action:           "ALLOW",
			Enabled:          true,
			Protocol:         "all",
			Version:          "IPV4",
			ConnectionStates: []string{},
			Source:           testCase.source(),
			Destination: &ui.FirewallPolicyDestination{
				ZoneID: zone, MatchingTarget: "ANY", PortMatchingType: "ANY",
			},
			// Required on every write; see firewall_policy_wire_fields.go.
			Schedule: &ui.FirewallPolicySchedule{Mode: "ALWAYS"},
		}
		created, err := client.CreateFirewallPolicy(ctx, site, policy)
		if err != nil {
			refused = append(refused, testCase.flag)
			t.Logf("  REFUSED  %s with its operand: %v", testCase.flag, err)
			continue
		}
		defer func(id string) {
			if err := client.DeleteFirewallPolicy(ctx, site, id); err != nil {
				t.Logf("cleaning up %s: %v", id, err)
			}
		}(created.ID)

		// A fresh GET, not the create echo: an echo can repeat the request.
		back, err := client.GetFirewallPolicy(ctx, site, created.ID)
		if err != nil {
			t.Fatal(err)
		}
		if back.Source == nil {
			t.Fatalf("%s: the controller returned a policy with no source", testCase.flag)
		}
		if testCase.read(back.Source) {
			held = append(held, testCase.flag)
			t.Logf("  HELD     %s (matching_target=%q)", testCase.flag, back.Source.MatchingTarget)
			continue
		}
		dropped = append(dropped, testCase.flag)
		t.Logf("  STORED FALSE  %s: the controller accepted the write and did not keep the flag "+
			"(matching_target=%q)", testCase.flag, back.Source.MatchingTarget)
	}

	// Without this the test reports "nothing held" when nothing ran.
	if len(held)+len(dropped)+len(refused) != len(cases) {
		t.Fatalf("only %d of %d cases produced a verdict; the sweep did not run",
			len(held)+len(dropped)+len(refused), len(cases))
	}
	if !contains(held, "match_opposite_ips") {
		t.Errorf("match_opposite_ips did not come out held, and it is known to be: "+
			"the fixture is wrong and the other verdicts prove nothing.\n"+
			"    held: %v\n    stored false: %v\n    refused: %v", held, dropped, refused)
	}

	t.Logf("HELD %d/%d: %v", len(held), len(cases), held)
	if len(dropped) > 0 {
		t.Logf("STORED FALSE %d: %v -- inert on this controller, so an apply sending false "+
			"destroys nothing", len(dropped), dropped)
	}
	if len(refused) > 0 {
		t.Logf("REFUSED %d: %v -- still unmeasured, and the refusal names what is missing",
			len(refused), refused)
	}
}
