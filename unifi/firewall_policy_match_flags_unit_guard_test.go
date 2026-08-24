package unifi

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// TestEndpointMappersCarryATrueMatchFlag is the fast-loop half of the
// match-flags fix's guard; the acceptance half only runs against a live
// controller.
//
// The four flags in FirewallPolicySource and FirewallPolicyDestination
// carry no omitempty, so a mapper that rebuilds the struct from a model
// without them sends false on every apply -- and for the inversion flags
// (e.g. match_opposite_ips), that flips which traffic the rule matches.
//
// Every existing unit test of these mappers asserts false, so they can't
// tell the fixed mapper apart from the broken one. True in, true out is
// the one shape the pre-fix mapper couldn't produce.
func TestEndpointMappersCarryATrueMatchFlag(t *testing.T) {
	ctx := context.Background()
	model := firewallPolicyEndpointModel{
		ZoneID:                types.StringValue("zone"),
		MatchingTarget:        types.StringValue("IP"),
		MatchingTargetType:    types.StringValue("SPECIFIC"),
		PortMatchingType:      types.StringValue("ANY"),
		IPs:                   types.ListNull(types.StringType),
		NetworkIDs:            types.ListNull(types.StringType),
		ClientMACs:            types.ListNull(types.StringType),
		WebDomains:            types.ListNull(types.StringType),
		MatchMAC:              types.BoolValue(true),
		MatchOppositeIPs:      types.BoolValue(true),
		MatchOppositeNetworks: types.BoolValue(true),
		MatchOppositePorts:    types.BoolValue(true),
	}

	var sourceDiags diag.Diagnostics
	source := endpointModelToSource(ctx, model, &sourceDiags)
	if sourceDiags.HasError() {
		t.Fatalf("endpointModelToSource: %v", sourceDiags.Errors())
	}
	for name, got := range map[string]bool{
		"match_mac":               source.MatchMAC,
		"match_opposite_ips":      source.MatchOppositeIPs,
		"match_opposite_networks": source.MatchOppositeNetworks,
		"match_opposite_ports":    source.MatchOppositePorts,
	} {
		if !got {
			t.Errorf("source.%s: the model carried true and the mapper sent false. "+
				"An apply would clear the flag on the controller, and for the inversion "+
				"flags that reverses which traffic the rule matches.", name)
		}
	}

	var destinationDiags diag.Diagnostics
	destination := endpointModelToDestination(ctx, model, &destinationDiags)
	if destinationDiags.HasError() {
		t.Fatalf("endpointModelToDestination: %v", destinationDiags.Errors())
	}
	// Both mappers: they are separate functions with the same defect, so a
	// guard on one alone would have passed while the other reset four more
	// flags.
	for name, got := range map[string]bool{
		"match_mac":               destination.MatchMAC,
		"match_opposite_ips":      destination.MatchOppositeIPs,
		"match_opposite_networks": destination.MatchOppositeNetworks,
		"match_opposite_ports":    destination.MatchOppositePorts,
	} {
		if !got {
			t.Errorf("destination.%s: the model carried true and the mapper sent false", name)
		}
	}
}
