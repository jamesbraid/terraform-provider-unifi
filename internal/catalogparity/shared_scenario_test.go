package catalogparity

import (
	"encoding/json"
	"os"
	"sort"
	"testing"
)

// TestSharedScenarioOwnerGuardDetectsBothSides mutation-tests
// validateSharedScenarioOwners against the committed inventory.
//
// It exists because of a defect shape worth naming. The guard compares the
// controller plan's shared scenario owners against a set derived from the
// inventory and the policy's declared exceptions. The plan builder derives its
// list from those same two inputs. A test that BUILDS the plan from the policy
// and then checks it against the guard therefore agrees by construction: empty
// the exceptions and both sides move together, so the mutation passes and the
// exception handling is never exercised. TestBuildAdmission's fixture builds
// its plan exactly that way, which is correct for what that test is for and
// blind for this.
//
// So the mutations here hold one side fixed while moving the other, which is
// the only arrangement that can detect anything.
func TestSharedScenarioOwnerGuardDetectsBothSides(t *testing.T) {
	data, err := os.ReadFile("../../build/release-ready/catalog-evidence-inventory.json")
	if err != nil {
		t.Fatal(err)
	}
	var inventory EvidenceInventory
	if err := json.Unmarshal(data, &inventory); err != nil {
		t.Fatal(err)
	}
	policy := testCampaignPolicy(t)
	plan := sharedScenarioOwnersFromPolicy(inventory, policy)
	if len(plan) == 0 {
		t.Fatal("the committed catalog yields no shared scenario owners, so every mutation below would be vacuous")
	}

	// The control. Six failing mutations against an already-broken fixture
	// would prove nothing.
	if err := validateSharedScenarioOwners(plan, inventory, policy); err != nil {
		t.Fatalf("the unmutated plan was rejected: %v", err)
	}

	t.Run("policy exception withdrawn while the plan keeps its owner", func(t *testing.T) {
		mutated := policy
		mutated.SharedScenarioExceptions = nil
		if err := validateSharedScenarioOwners(plan, inventory, mutated); err == nil {
			t.Fatal("withdrawing every declared exception was not detected")
		}
	})

	t.Run("plan drops an owner the inventory yields", func(t *testing.T) {
		if err := validateSharedScenarioOwners(plan[1:], inventory, policy); err == nil {
			t.Fatal("a plan missing a shared scenario owner was not detected")
		}
	})

	t.Run("plan carries an owner the inventory does not yield", func(t *testing.T) {
		extra := append(append([]string{}, plan...), "unifi/not_a_scenario_owner_test.go")
		sort.Strings(extra)
		if err := validateSharedScenarioOwners(extra, inventory, policy); err == nil {
			t.Fatal("a plan carrying an extra owner was not detected")
		}
	})

	// The old check compared LENGTHS. This is the case it could not see: the
	// right number of owners, one of them wrong.
	t.Run("plan substitutes one owner for another", func(t *testing.T) {
		substituted := append([]string{}, plan...)
		substituted[0] = "unifi/not_a_scenario_owner_test.go"
		sort.Strings(substituted)
		if err := validateSharedScenarioOwners(substituted, inventory, policy); err == nil {
			t.Fatal("a plan of the right length with a substituted owner was not detected")
		}
	})
}

// sharedScenarioOwnersFromPolicy applies the jq plan builder's rule from
// .woodpecker/scripts/catalog-controller-differential.sh, written out here so
// the fixture is a plausible plan rather than a call to the code under test.
func sharedScenarioOwnersFromPolicy(inventory EvidenceInventory, policy CampaignPolicy) []string {
	excepted := make(map[SurfaceKey]bool, len(policy.SharedScenarioExceptions))
	for _, exception := range policy.SharedScenarioExceptions {
		excepted[exception.SurfaceKey] = true
	}
	owners := map[string]struct{}{}
	for _, surface := range inventory.Surfaces {
		if surface.Runtime.Status == FileIdentical || excepted[surface.SurfaceKey] {
			owners[surface.ScenarioOwner] = struct{}{}
		}
	}
	result := make([]string, 0, len(owners))
	for owner := range owners {
		result = append(result, owner)
	}
	sort.Strings(result)
	return result
}
