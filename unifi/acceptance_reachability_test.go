package unifi

import (
	"encoding/json"
	"go/ast"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// TestEveryControllerTestIsReachableBySomething is the check that would have
// caught a regression test nothing ever runs.
//
// A test needing a controller skips without TF_ACC, and nothing in .woodpecker
// sets it. The only thing that does is cmd/catalog-controller-differential,
// whose runSuite exports TF_ACC=1 -- and it does not run the package. It runs a
// -run regex built by controllerdifferential.TestRegex from a plan, and that
// plan takes only names beginning "TestAcc":
//
//	if !strings.HasPrefix(name, "TestAcc") { continue }   plan.go:69
//
// SO A CONTROLLER TEST NAMED ANYTHING ELSE RUNS IN NO AUTOMATED PATH AT ALL.
// Not on a push, because TF_ACC is unset. Not in the campaign, because the
// regex never names it. Only a human typing -run reaches it, which is how
// every one of them was exercised.
//
// That is not hypothetical. The regression tests for the zero-value defect
// family -- the class this whole campaign exists to find -- are all named
// TestAnUnrelatedApply..., and all three are in that state today. They are
// declared below rather than renamed, because renaming them to TestAcc has
// consequences this test cannot decide: the campaign compares a released tree
// with a candidate one, so a candidate-only TestAcc needs a
// released_allowed_missing entry, and a TestAcc function is what the scenario
// owner rules in unifi/acceptance_coverage_test.go treat as surface coverage.
//
// What the declaration buys is that the NEXT one fails here instead of joining
// them silently.
func TestEveryControllerTestIsReachableBySomething(t *testing.T) {
	index := indexPackage(t)

	// Declared unreachable: a controller test the campaign's selector cannot
	// name. Compared as a SET, both directions, so one that gets fixed must be
	// removed here and a new one must be added deliberately.
	declaredUnreachable := map[string]string{
		"TestAnUnrelatedApplyDestroysControllerSideIPAliases": "guards 923bc482 (#193)",
		"TestAnUnrelatedApplyInvertsAFirewallRule":            "guards 96fa0dba (#198)",
		"TestAnUnrelatedApplyResetsAFirewallPolicySchedule":   "guards d097f36f (#200)",
		"TestDoesTheControllerHoldAFirewallPolicySchedule":    "measures #200's layer 2",
		"TestCanAFirewallPolicyBeCreatedWithoutASchedule":     "measures #200's fix shape",
		"TestDoesTheControllerHoldEachSourceMatchFlag":        "measures #198's field set",
		"TestEachDHCPFlagWithItsOperand":                      "red until #196's dhcp_server half lands",
	}

	gated, unreachable := 0, map[string]bool{}
	for name := range index.functions {
		if !strings.HasPrefix(name, "Test") || !index.needsAController(name) {
			continue
		}
		gated++
		if strings.HasPrefix(name, "TestAcc") {
			continue // the campaign's selector can name it
		}
		unreachable[name] = true
	}

	// Without this the check passes by classifying nothing, which is also what
	// a renamed helper would look like.
	if gated == 0 {
		t.Fatal("no controller-gated test was recognised at all; the walk is not reaching " +
			"the corpus, so an unreachable test would go unreported")
	}
	// THE CONTROL, on a known member of each class. A sweep that put everything
	// in one bucket is the failure this test exists to make visible.
	if !index.needsAController("TestAccFirewallPolicyFramework_basic") {
		t.Error("TestAccFirewallPolicyFramework_basic was not recognised as needing a " +
			"controller; the gated set is undercounted and the verdicts below are not evidence")
	}
	if index.needsAController("TestEndpointMappersCarryATrueMatchFlag") {
		t.Error("a pure mapper test was classified as needing a controller; the gated set " +
			"is overcounted")
	}

	for name := range unreachable {
		if _, declared := declaredUnreachable[name]; !declared {
			t.Errorf("%s needs a controller and is not named TestAcc, so no automated path "+
				"runs it: TF_ACC is unset on a push, and the campaign's plan selects only "+
				"TestAcc names (controllerdifferential/plan.go:69). Either rename it, or "+
				"declare it here with what it guards.", name)
		}
	}
	for name, why := range declaredUnreachable {
		if !unreachable[name] {
			t.Errorf("%s is declared unreachable (%s) and is not; remove the entry so the "+
				"list keeps describing the tree", name, why)
		}
	}

	names := make([]string, 0, len(unreachable))
	for name := range unreachable {
		names = append(names, name)
	}
	sort.Strings(names)
	t.Logf("%d controller-gated test function(s); %d of them run in no automated path: %v",
		gated, len(unreachable), names)
	reportInventoryReach(t, index)
}

// reportInventoryReach is the SECOND condition, reported rather than asserted.
//
// Being named TestAcc only makes a test ELIGIBLE. The plan is built from
// build/release-ready/catalog-evidence-inventory.json, a tracked artifact, so a
// TestAcc function absent from it is still not run -- until the inventory is
// regenerated, which the release flow does. Asserting on it here would fail
// every legitimately new test between writing it and regenerating, so this
// reports and the reader decides.
func reportInventoryReach(t *testing.T, index *coverageIndex) {
	body, err := os.ReadFile(filepath.Join("..", "build", "release-ready",
		"catalog-evidence-inventory.json"))
	if err != nil {
		t.Logf("inventory not readable (%v); campaign membership not reported", err)
		return
	}
	var inventory struct {
		Surfaces []struct {
			TestFunctions []string `json:"test_functions"`
		} `json:"surfaces"`
	}
	if err := json.Unmarshal(body, &inventory); err != nil {
		t.Fatalf("the inventory is not JSON: %v", err)
	}
	listed := map[string]bool{}
	for _, surface := range inventory.Surfaces {
		for _, name := range surface.TestFunctions {
			listed[name] = true
		}
	}
	if len(listed) == 0 {
		t.Fatal("the inventory listed no test functions, so the report below would be " +
			"vacuously alarming")
	}
	var absent []string
	for name := range index.functions {
		if strings.HasPrefix(name, "TestAcc") && !listed[name] {
			absent = append(absent, name)
		}
	}
	sort.Strings(absent)
	t.Logf("%d TestAcc function(s) are not in the tracked inventory, so the campaign plan "+
		"does not name them until it is regenerated: %v", len(absent), absent)
}

// needsAController reports whether a test skips without TF_ACC, following calls
// into the package so a gate inside a helper counts.
//
// preCheck and probeClient are the two helpers that carry the skip, and
// resource.Test is the framework's own: it skips the whole case when TF_ACC is
// absent. Reading only the test body would miss every one that gates through a
// helper.
func (c *coverageIndex) needsAController(entry string) bool {
	visited := map[string]bool{}
	found := false

	var walk func(string)
	walk = func(name string) {
		if found || visited[name] {
			return
		}
		visited[name] = true
		decl, ok := c.functions[name]
		if !ok || decl.Body == nil {
			return
		}
		ast.Inspect(decl.Body, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			switch calleeName(call.Fun) {
			case "Test", "ParallelTest":
				// resource.Test / resource.ParallelTest, not testing.T.Run.
				if selector, ok := call.Fun.(*ast.SelectorExpr); ok {
					if pkg, ok := selector.X.(*ast.Ident); ok && pkg.Name == "resource" {
						found = true
						return false
					}
				}
			case "probeClient", "rawSession", "preCheck":
				found = true
				return false
			}
			walk(calleeName(call.Fun))
			return true
		})
	}
	walk(entry)
	return found
}
