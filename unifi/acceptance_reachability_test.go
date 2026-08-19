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

	// THE ROUTING TABLE IS THE POLICY FILE, not a list here. It used to be a
	// map in this test naming what nothing ran; the campaign now has a second
	// selector that consumes the same names, so the declaration moved to where
	// it is acted on. Two homes for one fact is the defect this repository
	// keeps finding, and a list that only a test reads cannot make anything run.
	declared := declaredRegressionGuards(t)

	gated, unreachable := 0, map[string]bool{}
	for name := range index.functions {
		if !strings.HasPrefix(name, "Test") || !index.needsAController(name) {
			continue
		}
		gated++
		if strings.HasPrefix(name, "TestAcc") {
			continue // the catalog selector can name it
		}
		if declared[name] {
			continue // the regression selector can name it
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
		t.Errorf("%s needs a controller and no selector can name it: TF_ACC is unset on a "+
			"push, the catalog selector takes only TestAcc names "+
			"(controllerdifferential/plan.go), and it is not in regression_tests. Either "+
			"rename it, or add it to provider-codegen/policy/catalog-campaign.json so the "+
			"campaign actually runs it.", name)
	}
	// THE OTHER DIRECTION, and it is the one a typo needs. A name in the policy
	// that is not a test function makes the -run pattern match nothing for that
	// entry, and a suite that ran fewer guards than it lists still reports a
	// clean pass. Narrow() refuses an unknown diagnostic name for the same
	// reason; this is that rule applied to the guards.
	for name := range declared {
		if _, exists := index.functions[name]; !exists {
			t.Errorf("regression_tests names %q, which is not a test function in this "+
				"package. The -run pattern would match nothing for it and the suite would "+
				"pass having run one fewer guard than it claims.", name)
			continue
		}
		if !index.needsAController(name) {
			t.Errorf("regression_tests names %q, which does not need a controller. A guard "+
				"that runs in fast-loop belongs there, not in a suite that only a campaign "+
				"starts.", name)
		}
	}

	names := make([]string, 0, len(unreachable))
	for name := range unreachable {
		names = append(names, name)
	}
	sort.Strings(names)
	t.Logf("%d controller-gated test function(s); %d declared regression guard(s); "+
		"%d run in no automated path: %v", gated, len(declared), len(unreachable), names)
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

// declaredRegressionGuards reads the campaign's second selector.
//
// The policy file is the authority because it is what the runner consumes:
// controllerdifferential builds a -run pattern from these names. A copy here
// would be a second home for the fact, and the copy that is not read by the
// thing doing the work is the one that goes stale.
func declaredRegressionGuards(t *testing.T) map[string]bool {
	body, err := os.ReadFile(filepath.Join("..", "provider-codegen", "policy",
		"catalog-campaign.json"))
	if err != nil {
		t.Fatalf("reading the campaign policy: %v", err)
	}
	var policy struct {
		RegressionTests []string `json:"regression_tests"`
	}
	if err := json.Unmarshal(body, &policy); err != nil {
		t.Fatalf("the campaign policy is not JSON: %v", err)
	}
	if len(policy.RegressionTests) == 0 {
		t.Fatal("the campaign policy names no regression tests, so the campaign would run a " +
			"pattern matching nothing and report a suite that guarded nothing")
	}
	declared := make(map[string]bool, len(policy.RegressionTests))
	for _, name := range policy.RegressionTests {
		declared[name] = true
	}
	return declared
}
