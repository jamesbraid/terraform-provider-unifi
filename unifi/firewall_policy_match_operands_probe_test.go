package unifi

import (
	"context"
	"fmt"
	"testing"

	ui "github.com/ubiquiti-community/go-unifi/unifi"
)

// TestDoesTheControllerHoldEachSourceMatchFlag completes task 198's field set.
//
// Four flags live in FirewallPolicySource, all force-emitting, none modelled,
// and "source" is one wire key inside the masked update -- so every apply sends
// all four as false together. match_opposite_ips was confirmed end to end: an
// apply that changed only the description inverted a rule.
//
// The other three were recorded as UNMEASURED, and the reason is worth keeping,
// because it was a fixture fault dressed as a controller limit. Setting all
// four on one policy fails with MissingFirewallPolicySourceIpMac. A source has
// ONE matching_target, and these flags each invert a different one: ips needs
// IP, networks needs NETWORK, mac needs a client MAC list. Asking for all four
// at once asks for a source that cannot exist.
//
// So one policy per flag, each with the target its own flag operates on. That
// is the prerequisite rule -- before setting a flag, ask what it operates on --
// applied before being bitten rather than after.
//
// LAYER 2 IS THE WHOLE QUESTION HERE. Layers 1 and 3 are not separate facts for
// these three: they are fields of the same struct, written by the same
// assignment, inside the same masked key as the flag already confirmed. If the
// controller holds them, they are lost by exactly the mechanism already proved
// for match_opposite_ips, and the field set is complete rather than
// representative.
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

	// SPECIFIC, NOT OBJECT, and the control caught me getting this wrong. The
	// first run of this test used OBJECT and the controller refused the IP case
	// with MissingFirewallSourceIpGroupId: OBJECT means the endpoint names a
	// stored IP-group object, so it demands an ip_group_id and a literal list is
	// not one. firewallPolicyMatchingTargetType, the provider's own derivation,
	// says the same thing -- OBJECT only when an ip_group_id is present, and
	// SPECIFIC for any other non-ANY target.
	//
	// match_opposite_networks HELD under OBJECT anyway, which is exactly why the
	// classifier control is on the known member rather than on the count: three
	// of four verdicts looked fine.
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
		// TWO SHAPES FOR match_mac, because the operand rule does not say which
		// target it operates on and the SDK lists both CLIENT and MAC. A single
		// shape that stores false cannot tell "the controller ignores this flag"
		// from "the probe paired it with the wrong target" -- which is the
		// mistake the ip case above already caught once.
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
	// THE CLASSIFIER CONTROL. match_opposite_ips is already proved held and
	// already proved destroyed end to end, so it must come out of this sweep in
	// the held class. If it does not, the fixture is wrong and the other three
	// verdicts are worth nothing.
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
