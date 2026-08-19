package unifi

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// TestEndpointMappersCarryATrueMatchFlag is the fast-loop half of 96fa0dba's
// guard, and it exists because the acceptance half only runs in a campaign.
//
// The four flags in FirewallPolicySource and FirewallPolicyDestination carry no
// omitempty, so a mapper that rebuilds the struct from a model without them
// sends false on every apply -- and match_opposite_ips true against a specific
// IP list means "match everything EXCEPT this list", so the reset turned "block
// everything but these" into "block only these" while the policy stayed present
// and enforcing.
//
// EVERY EXISTING UNIT TEST OF THESE MAPPERS ASSERTS FALSE. That is what let the
// fix land unguarded: a table asserting BoolValue(false) is satisfied by the
// broken code, which produced false unconditionally, and by the fixed code fed
// a false model. Removing the four assignments left the whole repo suite green
// -- 53 packages, 0 failures.
//
// The direction that discriminates is TRUE IN, TRUE OUT. It is the only shape
// the pre-fix mapper could not produce.
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
	// BOTH MAPPERS, because they are separate functions with the same defect.
	// A guard on one would have passed while the other reset four more flags.
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
