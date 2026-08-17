package controllerdifferential

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ubiquiti-community/terraform-provider-unifi/internal/catalogparity"
)

const repositoryRoot = "../.."

// TestBuildPlanReproducesTheFrozenPlan is the acceptance test for this port,
// and unlike the receipt comparisons elsewhere it is NOT close to a tautology.
//
// Both inputs are committed -- the evidence inventory and the campaign policy --
// and the frozen receipt records what the shell's jq expression made of them on
// pipeline 232. So this reads the same two files, runs the Go, and requires the
// same plan out the other end: 67 surfaces, 156 test names, 6 evidence gaps,
// three shared scenario owners and three dispositions, every one of them
// derived rather than copied from the receipt.
func TestBuildPlanReproducesTheFrozenPlan(t *testing.T) {
	frozen := frozenPlan(t)
	built := buildFromCommittedFiles(t, frozen.Waves)

	want, err := catalogparity.MarshalReceipt(frozen)
	if err != nil {
		t.Fatal(err)
	}
	got, err := catalogparity.MarshalReceipt(built)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(want, got) {
		t.Fatalf("the plan does not match what the shell built from the same two files.\n"+
			"frozen surfaces=%d tests=%d gaps=%d owners=%d\n"+
			"built  surfaces=%d tests=%d gaps=%d owners=%d",
			frozen.SurfaceCount, len(frozen.TestNames), frozen.EvidenceGapCount, len(frozen.SharedScenarioOwners),
			built.SurfaceCount, len(built.TestNames), built.EvidenceGapCount, len(built.SharedScenarioOwners))
	}
}

// TestTheFrozenPlanIsNotTrivial states what the comparison above is worth. A
// plan of zero surfaces would match a builder that returns nothing.
func TestTheFrozenPlanIsNotTrivial(t *testing.T) {
	frozen := frozenPlan(t)
	if frozen.SurfaceCount < 2 || len(frozen.TestNames) < 2 || len(frozen.Waves) < 2 {
		t.Fatalf("the frozen plan has %d surface(s), %d test(s), %d wave(s); the comparison "+
			"against it proves little", frozen.SurfaceCount, len(frozen.TestNames), len(frozen.Waves))
	}
	if len(frozen.SharedScenarioOwners) == 0 {
		t.Fatal("the frozen plan lends no scenario owners, so the exception logic is unexercised " +
			"by the comparison")
	}
	if len(frozen.ReleasedAllowedMissing) == 0 {
		t.Fatal("the frozen plan carries no dispositions, so the intersection with the selected " +
			"tests is unexercised")
	}
}

// TestSelectingFewerWavesSelectsFewerSurfaces is the control for the wave
// filter. Without it, a builder that ignored the wave list entirely would still
// reproduce the frozen plan, because that run selected every wave there is.
//
// IT IS ALSO THE ONLY TEST THAT SEES THE DISPOSITION INTERSECTION, found by
// mutation rather than by reading. Removing that intersection leaves the frozen
// comparison GREEN, because on the full wave selection every policy entry names
// a test the plan runs -- so the intersection is a no-op for exactly the run the
// baseline records. The frozen example agrees with the mistake, which is the
// same blind spot clean_builds has in the schema receipt. Narrowing to wave 1
// is what makes it disagree.
func TestSelectingFewerWavesSelectsFewerSurfaces(t *testing.T) {
	all := buildFromCommittedFiles(t, frozenPlan(t).Waves)
	one := buildFromCommittedFiles(t, []int{1})
	if one.SurfaceCount >= all.SurfaceCount {
		t.Fatalf("wave 1 alone selects %d of %d surfaces, so the wave filter is not filtering",
			one.SurfaceCount, all.SurfaceCount)
	}
	if one.SurfaceCount == 0 {
		t.Fatal("wave 1 selects nothing, so this control cannot tell filtering from an empty read")
	}
	// The dispositions must narrow with the selection. A policy entry naming a
	// test outside the chosen waves licenses nothing, and carrying it would
	// make the plan look as though it had permission it never used.
	for _, name := range one.ReleasedAllowedMissing {
		if !contains(one.TestNames, name) {
			t.Fatalf("%q is allowed-missing in a plan that does not run it", name)
		}
	}
}

// TestListTestsBelongToTheListSurfaceOnly. Merging the two kinds would run
// every list test twice -- once as its own surface and once as the managed
// resource's -- and a duplicated pass reads as broader coverage than exists.
func TestListTestsBelongToTheListSurfaceOnly(t *testing.T) {
	functions := []string{"TestAccThingList_basic", "TestAccThing_basic", "TestUnitThing", "TestAccThingList_empty"}
	managed := AcceptanceTests("managed_resource", functions)
	list := AcceptanceTests("list_resource", functions)

	if len(managed) != 1 || managed[0] != "TestAccThing_basic" {
		t.Fatalf("managed resource picked %v", managed)
	}
	if len(list) != 2 {
		t.Fatalf("list resource picked %v", list)
	}
	for _, name := range list {
		if contains(managed, name) {
			t.Fatalf("%q belongs to both kinds, so it would run twice", name)
		}
	}
	// A kind that is neither takes every acceptance test, which is how actions
	// and data sources are covered.
	if other := AcceptanceTests("action", functions); len(other) != 3 {
		t.Fatalf("action picked %v, want all three acceptance tests", other)
	}
}

// TestOnlyIdenticalOrLicensedRuntimesLendTheirScenarios. Grafting a changed
// runtime's scenario onto the released tree has the released provider judged by
// the candidate's tests.
func TestOnlyIdenticalOrLicensedRuntimesLendTheirScenarios(t *testing.T) {
	inventory := Inventory{Surfaces: []InventorySurface{
		surface("managed_resource", "unifi_same", 1, "identical", "same_test.go", "TestAccSame_basic"),
		surface("managed_resource", "unifi_changed", 1, "changed", "changed_test.go", "TestAccChanged_basic"),
		surface("action", "unifi_port", 1, "changed", "port_test.go", "TestAccPort_basic"),
	}}
	policy := CampaignPolicy{SharedScenarioExceptions: []ScenarioException{
		{Kind: "action", Name: "unifi_port", Reason: "declared"},
	}}
	plan, err := BuildPlan(inventory, policy, []int{1})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"port_test.go", "same_test.go"}
	if strings.Join(plan.SharedScenarioOwners, ",") != strings.Join(want, ",") {
		t.Fatalf("lent %v, want %v. An identical runtime lends; a changed one lends only with a "+
			"declared exception", plan.SharedScenarioOwners, want)
	}
}

// TestAPlanThatWouldRunNothingIsRefused. An empty plan produces a receipt in
// which every suite passed, which is indistinguishable from a clean run.
func TestAPlanThatWouldRunNothingIsRefused(t *testing.T) {
	empty := Inventory{Surfaces: []InventorySurface{
		surface("managed_resource", "unifi_thing", 9, "identical", "thing_test.go", "TestUnitOnly"),
	}}
	if _, err := BuildPlan(empty, CampaignPolicy{}, []int{9}); err == nil {
		t.Fatal("a plan with a surface but no acceptance tests was accepted")
	}
	if _, err := BuildPlan(empty, CampaignPolicy{}, []int{1}); err == nil {
		t.Fatal("a plan selecting no surfaces at all was accepted")
	}
	if _, err := BuildPlan(empty, CampaignPolicy{}, nil); err == nil {
		t.Fatal("a plan with no waves was accepted")
	}
}

func TestNarrowRefusesATestThePlanDoesNotRun(t *testing.T) {
	plan := buildFromCommittedFiles(t, frozenPlan(t).Waves)
	if _, err := Narrow(plan, []string{"TestAccNotARealTest"}); err == nil {
		t.Fatal("a diagnostic run was narrowed to a test the plan does not contain. That run " +
			"would execute nothing and record that all zero of its tests passed")
	}
	narrowed, err := Narrow(plan, []string{plan.TestNames[0]})
	if err != nil {
		t.Fatal(err)
	}
	if !narrowed.DiagnosticSelection || narrowed.CatalogTestCount != len(plan.TestNames) {
		t.Fatalf("a narrowed plan does not record that it was narrowed: %+v", narrowed)
	}
	if len(narrowed.TestNames) != 1 {
		t.Fatalf("narrowed to %v", narrowed.TestNames)
	}
	for _, disposition := range [][]string{narrowed.AllowedSkips, narrowed.ReleasedAllowedFailures, narrowed.ReleasedAllowedMissing} {
		for _, name := range disposition {
			if name != narrowed.TestNames[0] {
				t.Fatalf("%q is still dispositioned in a run that does not reach it, so a "+
					"diagnostic run could accept a failure it had no permission for", name)
			}
		}
	}
}

// TestTheRunPatternIsAnchored. Without the anchors a plan naming TestAccFoo
// also runs TestAccFooBar, so the run is wider than the receipt says.
func TestTheRunPatternIsAnchored(t *testing.T) {
	pattern := TestRegex(catalogparity.ControllerPlanReceipt{TestNames: []string{"TestAccFoo", "TestAccBar"}})
	if !strings.HasPrefix(pattern, "^(") || !strings.HasSuffix(pattern, ")(/.*)?$") {
		t.Fatalf("pattern %q is not anchored, so it would match names the plan does not list", pattern)
	}
	if !strings.Contains(pattern, "TestAccFoo|TestAccBar") {
		t.Fatalf("pattern %q does not alternate the plan's tests", pattern)
	}
}

func surface(kind catalogparity.SurfaceKind, name string, wave int, status, owner string, functions ...string) InventorySurface {
	s := InventorySurface{
		Kind: kind, Name: name, Wave: wave,
		ScenarioOwners: []string{owner},
		TestFunctions:  functions,
	}
	s.Runtime.Status = status
	return s
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func frozenPlan(t *testing.T) catalogparity.ControllerPlanReceipt {
	t.Helper()
	path := filepath.Join(repositoryRoot, "build", "migration-baseline", "catalog-controller-differential.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var receipt catalogparity.ControllerDifferentialReceipt
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&receipt); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
	return receipt.Plan
}

func buildFromCommittedFiles(t *testing.T, waves []int) catalogparity.ControllerPlanReceipt {
	t.Helper()
	var inventory Inventory
	readJSON(t, filepath.Join(repositoryRoot, "build", "release-ready", "catalog-evidence-inventory.json"), &inventory)
	var policy CampaignPolicy
	readJSON(t, filepath.Join(repositoryRoot, "provider-codegen", "policy", "catalog-campaign.json"), &policy)

	plan, err := BuildPlan(inventory, policy, waves)
	if err != nil {
		t.Fatalf("build the plan from the committed inventory and policy: %v", err)
	}
	return plan
}

func readJSON(t *testing.T, path string, into any) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if err := json.Unmarshal(raw, into); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
}

// TestAPlanCannotBothLendATestAndDeclareItAbsent.
//
// released_allowed_missing says the released provider does not have these
// tests; a lent scenario file copies the candidate's tests onto the released
// tree, so anything it carries IS there. A name in both is a plan that
// contradicts itself, and the contradiction used to surface much later as the
// released suite reporting fewer missing tests than declared -- a message about
// the suite for a defect in the plan.
func TestAPlanCannotBothLendATestAndDeclareItAbsent(t *testing.T) {
	inventory := Inventory{Surfaces: []InventorySurface{
		surface("managed_resource", "unifi_lent", 1, "identical", "lent_test.go",
			"TestAccLent_basic", "TestAccLent_extra"),
	}}
	policy := CampaignPolicy{ReleasedAllowedMissing: []string{"TestAccLent_extra"}}

	_, err := BuildPlan(inventory, policy, []int{1})
	if err == nil {
		t.Fatal("a plan that lends the file defining TestAccLent_extra while declaring that test " +
			"missing from the released tree was accepted")
	}
	if !strings.Contains(err.Error(), "TestAccLent_extra") {
		t.Fatalf("refused with %q, which does not name the contradicted test", err)
	}
}

// TestTheCommittedPlanHasNoSuchContradiction is the control, and it is the
// measurement the assertion was written from: 17 allowed-missing tests against
// 49 carried by the three lent files, zero overlap. Without it the refusal
// above is satisfied by a rule that refuses every real plan too.
func TestTheCommittedPlanHasNoSuchContradiction(t *testing.T) {
	plan := buildFromCommittedFiles(t, frozenPlan(t).Waves)
	if len(plan.ReleasedAllowedMissing) == 0 || len(plan.SharedScenarioOwners) == 0 {
		t.Fatalf("the committed plan declares %d missing and lends %d owner(s); with either at "+
			"zero the disjointness holds trivially and this control checks nothing",
			len(plan.ReleasedAllowedMissing), len(plan.SharedScenarioOwners))
	}
}
