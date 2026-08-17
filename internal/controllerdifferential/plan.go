// Package controllerdifferential decides which acceptance tests the controller
// differential runs, which scenario files the released tree may borrow, and
// what each suite's outcome means.
//
// It replaces the judgement in .woodpecker/scripts/catalog-controller-differential.sh,
// which carried it in three jq programs totalling ninety lines. None of them
// could be called, so the most expensive gate in the pipeline -- an hour of
// controllers -- decided what to run using code no test had ever exercised.
package controllerdifferential

import (
	"fmt"
	"sort"
	"strings"

	"github.com/ubiquiti-community/terraform-provider-unifi/internal/catalogparity"
)

// Inventory is the part of the evidence inventory the plan is built from.
type Inventory struct {
	Surfaces []InventorySurface `json:"surfaces"`
}

// InventorySurface is one surface as the inventory records it.
type InventorySurface struct {
	Kind           catalogparity.SurfaceKind `json:"kind"`
	Name           string                    `json:"name"`
	Wave           int                       `json:"wave"`
	MissingSignals []string                  `json:"missing_signals"`
	ScenarioOwners []string                  `json:"scenario_owners"`
	TestFunctions  []string                  `json:"test_functions"`
	Runtime        struct {
		Status string `json:"status"`
	} `json:"runtime"`
}

// CampaignPolicy is the committed set of dispositions.
//
// The dispositions come from here rather than from the plan builder so that the
// builder, the followup gate and Go admission cannot drift apart. The shared
// scenario exception used to be hardcoded in the plan expression AS WELL AS
// declared in this file -- one fact with two homes, which is the shape of drift
// this campaign has been bitten by repeatedly.
type CampaignPolicy struct {
	AllowedSkips             []string            `json:"allowed_skips"`
	ReleasedAllowedFailures  []string            `json:"released_allowed_failures"`
	ReleasedAllowedMissing   []string            `json:"released_allowed_missing"`
	SharedScenarioExceptions []ScenarioException `json:"shared_scenario_exceptions"`
}

// ScenarioException licenses one surface to lend its scenario file to the
// released tree even though its runtime changed.
type ScenarioException struct {
	Kind   catalogparity.SurfaceKind `json:"kind"`
	Name   string                    `json:"name"`
	Reason string                    `json:"reason"`
}

// AcceptanceTests picks the acceptance tests a surface of this kind owns.
//
// THE THREE KINDS SELECT DIFFERENTLY and the difference is not cosmetic. A
// managed resource's acceptance tests are the TestAcc functions that are NOT
// list tests; a list resource's are the ones that ARE. Merging them would run
// every list test twice -- once as its own surface and once as the managed
// resource's -- and a duplicated pass reads as broader coverage than exists.
func AcceptanceTests(kind catalogparity.SurfaceKind, functions []string) []string {
	var picked []string
	for _, name := range functions {
		if !strings.HasPrefix(name, "TestAcc") {
			continue
		}
		isList := strings.Contains(name, "List")
		switch kind {
		case "managed_resource":
			if isList {
				continue
			}
		case "list_resource":
			if !isList {
				continue
			}
		}
		picked = append(picked, name)
	}
	return picked
}

// BuildPlan selects the surfaces in the requested waves and everything that
// follows from them.
func BuildPlan(inventory Inventory, policy CampaignPolicy, waves []int) (catalogparity.ControllerPlanReceipt, error) {
	if len(waves) == 0 {
		return catalogparity.ControllerPlanReceipt{}, fmt.Errorf("no waves selected, so the plan would run nothing")
	}
	selected := map[int]bool{}
	for _, wave := range waves {
		selected[wave] = true
	}
	licensed := map[catalogparity.SurfaceKey]bool{}
	for _, exception := range policy.SharedScenarioExceptions {
		licensed[catalogparity.SurfaceKey{Kind: exception.Kind, Name: exception.Name}] = true
	}

	plan := catalogparity.ControllerPlanReceipt{
		FormatVersion: 1,
		Gate:          "catalog controller differential",
		Waves:         append([]int(nil), waves...),
		Surfaces:      []catalogparity.ControllerPlanSurface{},
	}
	var testNames, owners []string
	for _, surface := range inventory.Surfaces {
		if !selected[surface.Wave] {
			continue
		}
		tests := AcceptanceTests(surface.Kind, surface.TestFunctions)
		if tests == nil {
			tests = []string{}
		}
		missing := surface.MissingSignals
		if missing == nil {
			missing = []string{}
		}
		plan.Surfaces = append(plan.Surfaces, catalogparity.ControllerPlanSurface{
			SurfaceKey:     catalogparity.SurfaceKey{Kind: surface.Kind, Name: surface.Name},
			Wave:           surface.Wave,
			MissingSignals: missing,
			TestNames:      tests,
		})
		testNames = append(testNames, tests...)
		plan.EvidenceGapCount += len(missing)

		// A surface may lend its scenario file to the released tree when its
		// runtime path is IDENTICAL, or when the policy declares an exception
		// and says why. Anything else keeps its released test owner and needs
		// separate migration evidence: grafting a changed runtime's scenario
		// would have the released provider judged by the candidate's tests.
		if surface.Runtime.Status == "identical" ||
			licensed[catalogparity.SurfaceKey{Kind: surface.Kind, Name: surface.Name}] {
			owners = append(owners, surface.ScenarioOwners...)
		}
	}

	plan.SurfaceCount = len(plan.Surfaces)
	plan.TestNames = uniqueSorted(testNames)
	plan.SharedScenarioOwners = uniqueSorted(owners)

	// The dispositions are INTERSECTED with the tests this plan actually runs.
	// A policy entry naming a test outside the selected waves licenses nothing
	// here, and carrying it into the receipt would make the plan look as though
	// it had permission it never used.
	plan.AllowedSkips = intersect(policy.AllowedSkips, plan.TestNames)
	plan.ReleasedAllowedFailures = intersect(policy.ReleasedAllowedFailures, plan.TestNames)
	plan.ReleasedAllowedMissing = intersect(policy.ReleasedAllowedMissing, plan.TestNames)

	// A TEST CANNOT BE BOTH DECLARED ABSENT FROM THE RELEASED TREE AND PUT
	// THERE BY THE GRAFT.
	//
	// released_allowed_missing says the released provider does not have these
	// tests. A lent scenario file copies the candidate's tests onto the
	// released tree, so anything it carries IS there. A name in both is a plan
	// that contradicts itself, and the contradiction surfaces much later as the
	// released suite reporting fewer missing tests than declared -- a message
	// about the suite for a defect in the plan.
	//
	// Measured before asserting: on the committed inventory and policy the two
	// sets are disjoint, 17 allowed-missing against 49 tests in the three lent
	// files, zero overlap. This keeps them that way.
	if lent := lentTests(inventory, plan.SharedScenarioOwners); len(lent) > 0 {
		var contradicted []string
		for _, name := range plan.ReleasedAllowedMissing {
			if lent[name] {
				contradicted = append(contradicted, name)
			}
		}
		if len(contradicted) > 0 {
			sort.Strings(contradicted)
			return plan, fmt.Errorf("the plan declares %d test(s) missing from the released tree "+
				"and lends the scenario file that defines them: %s. A test cannot be both absent "+
				"and grafted", len(contradicted), strings.Join(contradicted, ", "))
		}
	}

	if plan.SurfaceCount == 0 || len(plan.TestNames) == 0 {
		return plan, fmt.Errorf("the plan selects %d surface(s) and %d test(s); a run with nothing "+
			"to do would produce a receipt that looks like a clean pass",
			plan.SurfaceCount, len(plan.TestNames))
	}
	return plan, nil
}

// Narrow restricts a plan to an operator's chosen tests, for a diagnostic run.
//
// Every requested name must already be in the plan. A typo would otherwise
// silently narrow the run to nothing and produce a receipt recording that all
// zero of its tests passed.
func Narrow(plan catalogparity.ControllerPlanReceipt, requested []string) (catalogparity.ControllerPlanReceipt, error) {
	available := map[string]bool{}
	for _, name := range plan.TestNames {
		available[name] = true
	}
	var unknown []string
	for _, name := range requested {
		if !available[name] {
			unknown = append(unknown, name)
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		return plan, fmt.Errorf("requested diagnostic test(s) are not in the selected catalog: %s",
			strings.Join(unknown, ", "))
	}
	narrowed := uniqueSorted(requested)
	if len(narrowed) == 0 {
		return plan, fmt.Errorf("no diagnostic tests were requested")
	}

	plan.DiagnosticSelection = true
	plan.CatalogTestCount = len(plan.TestNames)
	plan.TestNames = narrowed
	// The dispositions narrow with the selection. Keeping the full set would
	// let a diagnostic run accept a failure it never had permission to reach.
	plan.AllowedSkips = intersect(plan.AllowedSkips, narrowed)
	plan.ReleasedAllowedFailures = intersect(plan.ReleasedAllowedFailures, narrowed)
	plan.ReleasedAllowedMissing = intersect(plan.ReleasedAllowedMissing, narrowed)
	return plan, nil
}

// TestRegex is the -run pattern for a plan.
//
// Anchored, with an optional subtest suffix, so a plan naming TestAccFoo does
// not also run TestAccFooBar. The shell built the same pattern; it is here so
// something can assert the anchoring, which is the part that silently widens a
// run when it is wrong.
func TestRegex(plan catalogparity.ControllerPlanReceipt) string {
	return "^(" + strings.Join(plan.TestNames, "|") + ")(/.*)?$"
}

func uniqueSorted(values []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, value := range values {
		if seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func intersect(values, allowed []string) []string {
	permitted := map[string]bool{}
	for _, name := range allowed {
		permitted[name] = true
	}
	out := []string{}
	for _, value := range values {
		if permitted[value] {
			out = append(out, value)
		}
	}
	return out
}

// lentTests names every test function carried by a lent scenario file.
//
// It reads the inventory rather than the plan because the plan records which
// tests a surface OWNS for the run, and the question here is what a FILE
// contains once it is copied -- a scenario file brings all of its functions,
// not only the ones this wave selected.
func lentTests(inventory Inventory, owners []string) map[string]bool {
	lent := map[string]bool{}
	if len(owners) == 0 {
		return lent
	}
	wanted := map[string]bool{}
	for _, owner := range owners {
		wanted[owner] = true
	}
	for _, surface := range inventory.Surfaces {
		for _, owner := range surface.ScenarioOwners {
			if !wanted[owner] {
				continue
			}
			for _, name := range surface.TestFunctions {
				lent[name] = true
			}
		}
	}
	return lent
}
