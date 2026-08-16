package releasequalification

import (
	"testing"

	"github.com/ubiquiti-community/terraform-provider-unifi/internal/catalogparity"
)

// The declared-missing excuse exists because a test the released provider
// cannot run was vetoing the tests beside it that ran on both. These tests fix
// what it may and may not do, because the failure mode of an excuse is that it
// quietly excuses more than it was written for -- which it did on the first
// attempt, handing fifteen surfaces the strongest evidence mode on the strength
// of no comparison at all.
//
// A census over the committed inventory cannot show most of this: it grants
// every planned test a pass on both providers, so a test that FAILS never
// occurs there. These construct the view directly for that reason.

func declaredMissingKey() catalogparity.SurfaceKey {
	return catalogparity.SurfaceKey{Kind: catalogparity.ManagedResource, Name: "unifi_example"}
}

// declaredMissingView builds a surface with one shared scenario and one
// candidate-only scenario, which is the shape the excuse was written for.
func declaredMissingView(
	sharedPassesOnCandidate bool,
	declared map[string]struct{},
) evidenceView {
	key := declaredMissingKey()
	surface := catalogparity.SurfaceEvidenceInventory{
		SurfaceKey: key,
		Runtime:    catalogparity.FileComparison{Status: catalogparity.FileChanged},
		Tests:      catalogparity.FileComparison{Status: catalogparity.FileChanged},
		Scenarios: []catalogparity.ScenarioComparison{
			{Name: "TestAccShared", Status: catalogparity.ScenarioIdentical},
			{Name: "TestAccBrandNew", Status: catalogparity.ScenarioAdded},
		},
	}
	view := evidenceView{
		inventory: map[catalogparity.SurfaceKey]catalogparity.SurfaceEvidenceInventory{key: surface},
		admission: map[catalogparity.SurfaceKey]catalogparity.SurfaceAdmission{
			key: {SurfaceKey: key},
		},
		planned: map[catalogparity.SurfaceKey][]string{
			key: {"TestAccShared", "TestAccBrandNew"},
		},
		released:        map[string]struct{}{"TestAccShared": {}},
		candidate:       map[string]struct{}{},
		declaredMissing: declared,
	}
	if sharedPassesOnCandidate {
		view.candidate["TestAccShared"] = struct{}{}
	}
	view.candidate["TestAccBrandNew"] = struct{}{}
	return view
}

// A candidate-only test, declared, must not veto the scenario beside it that
// ran on both providers.
func TestDeclaredMissingDoesNotVetoASharedScenario(t *testing.T) {
	view := declaredMissingView(true, map[string]struct{}{"TestAccBrandNew": {}})
	got := view.differentialScenario(declaredMissingKey(), view.inventory[declaredMissingKey()])
	if !got.holds {
		t.Fatalf("differential_scenario denied with a declared candidate-only test: %s", got.because)
	}
}

// Without the declaration the same test vetoes. An escape hatch that opens by
// accident is not a declaration.
func TestUndeclaredCandidateOnlyTestStillVetoes(t *testing.T) {
	view := declaredMissingView(true, map[string]struct{}{})
	got := view.differentialScenario(declaredMissingKey(), view.inventory[declaredMissingKey()])
	if got.holds {
		t.Fatal("differential_scenario held with an undeclared candidate-only test")
	}
}

// The declaration says "released never had this test". It does not say "do not
// judge this test", so a declared test that fails on the CANDIDATE must still
// deny the mode. Otherwise declaring a test would launder its failure.
func TestDeclaredMissingStillVetoesACandidateFailure(t *testing.T) {
	key := declaredMissingKey()
	view := declaredMissingView(true, map[string]struct{}{"TestAccBrandNew": {}})
	delete(view.candidate, "TestAccBrandNew")
	got := view.differentialScenario(key, view.inventory[key])
	if got.holds {
		t.Fatal("differential_scenario held while a declared test failed on the candidate")
	}
}

// A surface whose EVERY planned test is declared has nothing comparing the two
// providers, so the mode must not hold vacuously.
//
// Measured on this tree, fifteen surfaces are in exactly this position --
// firewall_policy, firewall_zone, site_to_site_vpn, device, power_supervisor
// and ten more each plan one acceptance test that is already declared. Without
// this the excuse handed all fifteen the strongest mode at once.
func TestEveryPlannedTestDeclaredDoesNotHoldVacuously(t *testing.T) {
	key := declaredMissingKey()
	view := declaredMissingView(true, map[string]struct{}{
		"TestAccShared": {}, "TestAccBrandNew": {},
	})
	got := view.differentialScenario(key, view.inventory[key])
	if got.holds {
		t.Fatal("differential_scenario held with every planned test declared missing")
	}
}
