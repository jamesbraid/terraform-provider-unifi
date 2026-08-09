package catalogparity

import (
	"encoding/json"
	"os"
	"reflect"
	"slices"
	"sort"
	"strings"
	"testing"
)

// TestCampaignPolicyMatchesCommittedInventory recomputes the campaign shape
// from the committed evidence inventory using the same rules as the jq plan
// builder in .woodpecker/scripts/catalog-controller-differential.sh.
//
// Without this, the policy is only checked against a plan that the tests
// themselves synthesised from that same policy, which is true by construction.
// A count that has drifted away from the real catalog would then survive every
// local check and only surface an hour into a controller campaign.
func TestCampaignPolicyMatchesCommittedInventory(t *testing.T) {
	policy := testCampaignPolicy(t)

	data, err := os.ReadFile("../../build/release-ready/catalog-evidence-inventory.json")
	if err != nil {
		t.Fatal(err)
	}
	var inventory EvidenceInventory
	if err := json.Unmarshal(data, &inventory); err != nil {
		t.Fatal(err)
	}

	waves := []int{1, 2, 3, 4, 5}
	surfaceCount := 0
	evidenceGapCount := 0
	testNames := make([]string, 0)
	sharedScenarioOwners := make([]string, 0)
	runtimeChanged := make([]SurfaceKey, 0)
	for _, surface := range inventory.Surfaces {
		if !slices.Contains(waves, surface.Wave) {
			continue
		}
		if surface.Runtime.Status == FileChanged {
			runtimeChanged = append(runtimeChanged, surface.SurfaceKey)
		}
		surfaceCount++
		evidenceGapCount += len(surface.MissingSignals)
		for _, name := range surface.TestFunctions {
			if !strings.HasPrefix(name, "TestAcc") {
				continue
			}
			isList := strings.Contains(name, "List")
			switch surface.Kind {
			case ManagedResource:
				if isList {
					continue
				}
			case ListResource:
				if !isList {
					continue
				}
			}
			if !slices.Contains(testNames, name) {
				testNames = append(testNames, name)
			}
		}
		// The port action shares its scenario with the released suite even
		// though its runtime differs; that sharing is what makes the hardware
		// disposition satisfiable.
		shared := surface.Runtime.Status == "identical" ||
			(surface.Kind == Action && surface.Name == "unifi_port")
		if shared && !slices.Contains(sharedScenarioOwners, surface.ScenarioOwner) {
			sharedScenarioOwners = append(sharedScenarioOwners, surface.ScenarioOwner)
		}
	}

	for _, check := range []struct {
		field string
		got   int
		want  int
	}{
		{"surface_count", surfaceCount, policy.SurfaceCount},
		{"evidence_gap_count", evidenceGapCount, policy.EvidenceGapCount},
		{"test_name_count", len(testNames), policy.TestNameCount},
		{"shared_scenario_owner_count", len(sharedScenarioOwners), policy.SharedScenarioOwnerCount},
	} {
		if check.got != check.want {
			t.Errorf("inventory yields %s = %d, policy says %d", check.field, check.got, check.want)
		}
	}

	// Migration recovery compares this exact set, sorted by kind then name, an
	// hour into a campaign. Catching a stale entry here costs a second.
	sort.Slice(runtimeChanged, func(a, b int) bool {
		if runtimeChanged[a].Kind != runtimeChanged[b].Kind {
			return runtimeChanged[a].Kind < runtimeChanged[b].Kind
		}
		return runtimeChanged[a].Name < runtimeChanged[b].Name
	})
	if !reflect.DeepEqual(runtimeChanged, policy.RuntimeChangeSet) {
		t.Errorf("inventory runtime change set is %v, policy declares %v",
			runtimeChanged, policy.RuntimeChangeSet)
	}

	// Every disposition has to name a test the campaign actually plans to run,
	// or the released suite can never satisfy it.
	for _, names := range [][]string{
		policy.AllowedSkips,
		policy.ReleasedAllowedFailures,
		policy.ReleasedAllowedMissing,
	} {
		for _, name := range names {
			if !slices.Contains(testNames, name) {
				t.Errorf("campaign policy names %q, which the catalog does not plan", name)
			}
		}
	}
}
