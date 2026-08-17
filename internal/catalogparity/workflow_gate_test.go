package catalogparity

import (
	"strings"
	"testing"
)

// These tests exist because the assertions they cover could not previously be
// tested at all. As `jq -e` lines in a workflow they took a /tmp path that only
// exists mid-pipeline, and they were silent on every run that passed -- so no
// clause in any of them had ever been observed failing.
//
// EVERY CLAUSE IS WATCHED FAILING HERE, one mutation at a time from a receipt
// that passes. A gate tested only against a passing input tests nothing, which
// is the whole finding: on the recorded run, a deleted clause and a live one
// produce the same verdict.

func passingBuildSchema() BuildSchemaReceipt {
	return BuildSchemaReceipt{
		Result:            "pass",
		BuildNetwork:      "none",
		PromotionBlockers: []string{},
	}
}

func TestCheckBuildSchemaReceipt(t *testing.T) {
	if err := CheckBuildSchemaReceipt(passingBuildSchema()); err != nil {
		t.Fatalf("a passing receipt was rejected: %v", err)
	}

	for _, c := range []struct {
		name    string
		mutate  func(*BuildSchemaReceipt)
		mention string
	}{
		{"result", func(r *BuildSchemaReceipt) { r.Result = "fail" }, "result"},
		{"result absent", func(r *BuildSchemaReceipt) { r.Result = "" }, "result"},
		{"build network", func(r *BuildSchemaReceipt) { r.BuildNetwork = "internet" }, "build_network"},
		{"build network absent", func(r *BuildSchemaReceipt) { r.BuildNetwork = "" }, "build_network"},
		{"a promotion blocker", func(r *BuildSchemaReceipt) { r.PromotionBlockers = []string{"schema drift"} }, "promotion_blockers"},
	} {
		t.Run(c.name, func(t *testing.T) {
			receipt := passingBuildSchema()
			c.mutate(&receipt)
			requireGateFailure(t, CheckBuildSchemaReceipt(receipt), c.mention)
		})
	}
}

func passingUnitDifferential() UnitDifferentialReceipt {
	suite := UnitSuiteReceipt{Result: "pass", PassedTestCount: 152}
	return UnitDifferentialReceipt{
		Result:            "pass",
		Network:           "none",
		PromotionBlockers: []string{},
		Released:          suite,
		Candidate:         suite,
	}
}

func TestCheckUnitDifferentialReceipt(t *testing.T) {
	if err := CheckUnitDifferentialReceipt(passingUnitDifferential()); err != nil {
		t.Fatalf("a passing receipt was rejected: %v", err)
	}

	for _, c := range []struct {
		name    string
		mutate  func(*UnitDifferentialReceipt)
		mention string
	}{
		{"result", func(r *UnitDifferentialReceipt) { r.Result = "fail" }, "result"},
		{"network", func(r *UnitDifferentialReceipt) { r.Network = "internet" }, "network"},
		{"released suite", func(r *UnitDifferentialReceipt) { r.Released.Result = "fail" }, "released.result"},
		{"candidate suite", func(r *UnitDifferentialReceipt) { r.Candidate.Result = "fail" }, "candidate.result"},
		{"a promotion blocker", func(r *UnitDifferentialReceipt) { r.PromotionBlockers = []string{"x"} }, "promotion_blockers"},

		// THE CASE THE YAML GATE COULD NOT SEE. Both suites report pass and
		// every clause of the original assertion holds, on a run that executed
		// no tests. The count was already in the receipt.
		{"released ran nothing", func(r *UnitDifferentialReceipt) { r.Released.PassedTestCount = 0 }, "running nothing"},
		{"candidate ran nothing", func(r *UnitDifferentialReceipt) { r.Candidate.PassedTestCount = 0 }, "running nothing"},
	} {
		t.Run(c.name, func(t *testing.T) {
			receipt := passingUnitDifferential()
			c.mutate(&receipt)
			requireGateFailure(t, CheckUnitDifferentialReceipt(receipt), c.mention)
		})
	}
}

func passingControllerDifferential() ControllerDifferentialReceipt {
	return ControllerDifferentialReceipt{
		// The result is set rather than left at the zero value because the gate
		// now requires it to follow from the gap count. An empty string used to
		// pass here, which is how a receipt with no result at all would have
		// reached the release path.
		Result: "blocked_evidence",
		Plan: ControllerPlanReceipt{
			EvidenceGapCount: ControllerGapCeiling,
			Surfaces:         []ControllerPlanSurface{{Wave: 1}},
		},
	}
}

func TestCheckControllerDifferentialReceipt(t *testing.T) {
	// The frozen receipt from pipeline 232 reports exactly the ceiling, so the
	// passing case here is the boundary rather than a comfortable middle.
	if err := CheckControllerDifferentialReceipt(passingControllerDifferential()); err != nil {
		t.Fatalf("a receipt at the ceiling was rejected: %v", err)
	}

	t.Run("one gap over the ceiling", func(t *testing.T) {
		receipt := passingControllerDifferential()
		receipt.Plan.EvidenceGapCount = ControllerGapCeiling + 1
		requireGateFailure(t, CheckControllerDifferentialReceipt(receipt), "evidence_gap_count")
	})

	// jq's `null <= 6` was true, so the original passed when the field was
	// absent. Go decodes absent to zero, which passes for a different wrong
	// reason -- so absence is distinguished by the plan being empty as well.
	t.Run("an unfilled plan", func(t *testing.T) {
		requireGateFailure(t, CheckControllerDifferentialReceipt(ControllerDifferentialReceipt{}),
			"absent rather than clean")
	})

	t.Run("a genuinely clean plan is accepted", func(t *testing.T) {
		receipt := passingControllerDifferential()
		receipt.Plan.EvidenceGapCount = 0
		receipt.Result = "pass"
		if err := CheckControllerDifferentialReceipt(receipt); err != nil {
			t.Errorf("zero gaps with surfaces present should pass this gate: %v", err)
		}
	})

	// THE LAST OF TASK 160'S DOORS, AND IT WAS IN THE WORKFLOW RATHER THAN IN
	// GO. catalog-controller-differential.yml carried
	// `jq -e '.result == "blocked_evidence" ...'`, which is unreachable for a
	// completed campaign; the conjunct moved here, where it is the AGREEMENT
	// rather than a literal.
	//
	// Both directions, because either alone is satisfied by deleting the call.
	// The accepting direction is the subtest above -- a completed campaign,
	// which the workflow line rejected.
	t.Run("a result that does not follow from the gap count", func(t *testing.T) {
		for _, c := range []struct {
			name   string
			result string
			gaps   int
		}{
			{"claims pass with gaps outstanding", "pass", ControllerGapCeiling},
			{"claims blocked with no gaps", "blocked_evidence", 0},
		} {
			t.Run(c.name, func(t *testing.T) {
				receipt := passingControllerDifferential()
				receipt.Result = c.result
				receipt.Plan.EvidenceGapCount = c.gaps
				requireGateFailure(t, CheckControllerDifferentialReceipt(receipt), "evidence gap")
			})
		}
	})
}

// TestGateFailuresReportEveryMismatch covers the reason these return a list.
// A receipt wrong in three ways, reported one way at a time, costs a controller
// campaign per fix.
func TestGateFailuresReportEveryMismatch(t *testing.T) {
	receipt := passingBuildSchema()
	receipt.Result = "fail"
	receipt.BuildNetwork = "internet"
	receipt.PromotionBlockers = []string{"drift"}

	err := CheckBuildSchemaReceipt(receipt)
	if err == nil {
		t.Fatal("a receipt wrong three ways was accepted")
	}
	failure, ok := err.(*GateFailure)
	if !ok {
		t.Fatalf("error is %T, want *GateFailure", err)
	}
	if len(failure.Mismatch) != 3 {
		t.Errorf("reported %d mismatch(es), want all 3:\n%v", len(failure.Mismatch), err)
	}
}

func requireGateFailure(t *testing.T, err error, mention string) {
	t.Helper()
	if err == nil {
		t.Fatalf("the gate accepted a receipt it should have refused; expected it to mention %q", mention)
	}
	if !strings.Contains(err.Error(), mention) {
		t.Errorf("the gate refused, but not for the reason under test.\n want mention of: %s\n got: %v", mention, err)
	}
}
