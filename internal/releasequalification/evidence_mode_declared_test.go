package releasequalification

import (
	"strings"
	"testing"

	"github.com/ubiquiti-community/terraform-provider-unifi/internal/catalogparity"
)

// The declared-missing excuse exists because a test the released provider
// cannot run was vetoing the tests beside it that ran on both. These fix what
// it may and may not do, because the failure mode of an excuse is that it
// quietly excuses more than it was written for.
//
// It did exactly that on the first attempt. Stated as "a declared test does not
// veto" and censused across all sixty-seven surfaces, it moved sixteen rather
// than one: fifteen surfaces GAINED differential_scenario, the strongest mode,
// including firewall_policy, firewall_zone, site_to_site_vpn, device and
// power_supervisor. Each of the fifteen plans exactly ONE acceptance test and
// that test is already in released_allowed_missing, so excusing it left the
// per-test loops iterating over nothing and holds:true fell out of an empty
// iteration -- a check that cannot fail, handed to the surfaces with the least
// evidence. Only unifi_network had anything left: 11 planned, 2 declared, 9
// still compared against the released tree.
//
// NONE is the correct answer for those fifteen, and that is a checked claim
// rather than an assumption. One planned acceptance test, declared missing from
// the released provider, means zero scenarios ran on both sides, which is
// precisely what differential_scenario asserts. There is no comparison to
// weigh, so there is no differential evidence to record.
//
// A census cannot keep this honest: it is a one-off over the committed
// inventory, it grants every planned test a pass on both providers so a FAILING
// test never occurs in it, and it is not run by `go test ./...` against a
// hypothetical rule change. These are.
func TestDeclaredMissingExcusesOnlyTheReleasedSide(t *testing.T) {
	key := catalogparity.SurfaceKey{Kind: catalogparity.ManagedResource, Name: "unifi_example"}

	// The surface holds one scenario shared with the released tree and one the
	// candidate introduced, which is the shape the excuse was written for.
	surface := catalogparity.SurfaceEvidenceInventory{
		SurfaceKey: key,
		Runtime:    catalogparity.FileComparison{Status: catalogparity.FileChanged},
		Tests:      catalogparity.FileComparison{Status: catalogparity.FileChanged},
		Scenarios: []catalogparity.ScenarioComparison{
			{Name: "TestAccShared", Status: catalogparity.ScenarioIdentical},
			{Name: "TestAccBrandNew", Status: catalogparity.ScenarioAdded},
		},
	}

	for name, tc := range map[string]struct {
		declared          []string
		planned           []string
		candidatePassed   []string
		wantHolds         bool
		wantBecauseSubstr string
	}{
		// The case the excuse exists for: a candidate-only test must not strike
		// down the scenario beside it that both providers ran.
		"declared candidate-only test does not veto its neighbour": {
			declared:        []string{"TestAccBrandNew"},
			planned:         []string{"TestAccShared", "TestAccBrandNew"},
			candidatePassed: []string{"TestAccShared", "TestAccBrandNew"},
			wantHolds:       true,
		},
		// An escape hatch that opens by accident is not a declaration.
		"undeclared candidate-only test still vetoes": {
			declared:        nil,
			planned:         []string{"TestAccShared", "TestAccBrandNew"},
			candidatePassed: []string{"TestAccShared", "TestAccBrandNew"},
			wantHolds:       false,
			// "added" rather than "no per-scenario comparison": the inventory
			// DOES measure a candidate-only scenario, and records it as added
			// precisely so a reader can tell "the released tree never had this"
			// from "nobody looked".
			wantBecauseSubstr: "is added against the released tree",
		},
		// The declaration says "released never had this test". It does not say
		// "do not judge this test", so declaring one must not launder its
		// failure on the candidate.
		"declared test failing on the candidate still vetoes": {
			declared:          []string{"TestAccBrandNew"},
			planned:           []string{"TestAccShared", "TestAccBrandNew"},
			candidatePassed:   []string{"TestAccShared"},
			wantHolds:         false,
			wantBecauseSubstr: "did not pass on the candidate provider",
		},
		// THE VACUITY CASE. Delete the non-vacuity clause and this case and
		// the one below it go red, while the other three still pass --
		// verified by removing the clause and running, rather than assumed.
		// That split is the point: a surface with surviving comparisons is
		// unaffected by the clause, so only a case with none can guard it.
		// Without these two the fifteen surfaces above come back silently.
		"every planned test declared holds nothing vacuously": {
			declared:          []string{"TestAccShared", "TestAccBrandNew"},
			planned:           []string{"TestAccShared", "TestAccBrandNew"},
			candidatePassed:   []string{"TestAccShared", "TestAccBrandNew"},
			wantHolds:         false,
			wantBecauseSubstr: "no scenario compares the two",
		},
		// The same shape as the fifteen: a single planned test, declared. This
		// is the real-world arrangement rather than the two-test abstraction.
		"a surface whose only planned test is declared": {
			declared:          []string{"TestAccShared"},
			planned:           []string{"TestAccShared"},
			candidatePassed:   []string{"TestAccShared"},
			wantHolds:         false,
			wantBecauseSubstr: "no scenario compares the two",
		},
	} {
		t.Run(name, func(t *testing.T) {
			view := evidenceView{
				inventory: map[catalogparity.SurfaceKey]catalogparity.SurfaceEvidenceInventory{key: surface},
				admission: map[catalogparity.SurfaceKey]catalogparity.SurfaceAdmission{key: {SurfaceKey: key}},
				planned:   map[catalogparity.SurfaceKey][]string{key: tc.planned},
				// The released provider ran the shared scenario and nothing else.
				released:        map[string]struct{}{"TestAccShared": {}},
				candidate:       map[string]struct{}{},
				declaredMissing: map[string]struct{}{},
			}
			for _, n := range tc.candidatePassed {
				view.candidate[n] = struct{}{}
			}
			for _, n := range tc.declared {
				view.declaredMissing[n] = struct{}{}
			}

			got := view.differentialScenario(key, surface)
			if got.holds != tc.wantHolds {
				t.Fatalf("differential_scenario holds = %t, want %t (because: %s)",
					got.holds, tc.wantHolds, got.because)
			}
			if tc.wantBecauseSubstr == "" {
				return
			}
			if !strings.Contains(got.because, tc.wantBecauseSubstr) {
				t.Fatalf("refused for %q, want a reason containing %q",
					got.because, tc.wantBecauseSubstr)
			}
		})
	}
}
