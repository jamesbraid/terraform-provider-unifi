package catalogparity

import (
	"fmt"
	"strings"
)

// Workflow gates are the assertions that used to live as `jq -e` one-liners in
// .woodpecker/*.yml, moved here so they can be given a failing input.
//
// THE REASON IS NOT THAT SHELL IS BAD. An assertion inside a `commands:` entry
// has no way to be handed a receipt that should fail it: its input is a /tmp
// path that exists only while a pipeline is running, and a gate is silent on
// every run that passes. Thirteen of the fourteen gates in this repository are
// in exactly that position, which is why four separate findings -- a gate
// asserting evidence modes the producer had deleted, one comparing a constant
// to itself, and two on receipts nothing could decode -- all went unseen. Not
// diligence. Structure.
//
// The same assertion in Go can be run against a receipt built in a test, so
// every clause can be watched failing. That is the deliverable; the deleted
// YAML line is a side effect.
//
// THE GATES TAKE A DECODED RECEIPT, NOT BYTES, deliberately. Decoding is
// already strict and already tested, and a gate that re-parsed would be a
// second opinion about the shape -- which is how a receipt comes to be accepted
// by one consumer and rejected by another.

// GateFailure lists what a gate wanted and did not get. It is a slice rather
// than a first error because a receipt that fails three ways should say so
// once: an operator who fixes the first and re-runs a controller campaign to
// find the second has paid an hour for information we already had.
type GateFailure struct {
	Gate     string
	Mismatch []string
}

func (g *GateFailure) Error() string {
	return fmt.Sprintf("%s: %d assertion(s) failed:\n    %s",
		g.Gate, len(g.Mismatch), strings.Join(g.Mismatch, "\n    "))
}

func (g *GateFailure) want(what string, want, got any) {
	if want != got {
		g.Mismatch = append(g.Mismatch, fmt.Sprintf("%s is %v, want %v", what, got, want))
	}
}

func (g *GateFailure) resolve() error {
	if len(g.Mismatch) == 0 {
		return nil
	}
	return g
}

// CheckBuildSchemaReceipt replaces
//
//	jq -e '.result == "pass" and .promotion_blockers == [] and .build_network == "none"'
//
// build_network is the one that carries information rather than restating the
// script's own success: it records whether the build reached the network, which
// the script cannot decide for itself after the fact.
func CheckBuildSchemaReceipt(receipt BuildSchemaReceipt) error {
	failure := &GateFailure{Gate: "catalog-build-schema"}
	failure.want("result", "pass", receipt.Result)
	failure.want("build_network", "none", receipt.BuildNetwork)
	if len(receipt.PromotionBlockers) != 0 {
		failure.Mismatch = append(failure.Mismatch,
			fmt.Sprintf("promotion_blockers has %d entr(ies): %s",
				len(receipt.PromotionBlockers), strings.Join(receipt.PromotionBlockers, ", ")))
	}
	return failure.resolve()
}

// CheckUnitDifferentialReceipt replaces
//
//	jq -e '.result == "pass" and .network == "none" and .promotion_blockers == []
//	       and .released.result == "pass" and .candidate.result == "pass"'
//
// The two suite results are the part that is not a restatement: the top-level
// result is written by the script after its own checks, while released and
// candidate come from two separate go test event streams.
func CheckUnitDifferentialReceipt(receipt UnitDifferentialReceipt) error {
	failure := &GateFailure{Gate: "catalog-unit-differential"}
	failure.want("result", "pass", receipt.Result)
	failure.want("network", "none", receipt.Network)
	failure.want("released.result", "pass", receipt.Released.Result)
	failure.want("candidate.result", "pass", receipt.Candidate.Result)
	if len(receipt.PromotionBlockers) != 0 {
		failure.Mismatch = append(failure.Mismatch,
			fmt.Sprintf("promotion_blockers has %d entr(ies): %s",
				len(receipt.PromotionBlockers), strings.Join(receipt.PromotionBlockers, ", ")))
	}

	// NOT IN THE YAML VERSION, AND THE REASON IT IS HERE. A suite that ran
	// nothing reports result "pass" with zero tests, so the four assertions
	// above all hold on a run that tested nothing at all. The shell receipt
	// already carries the counts; nothing was looking at them.
	if receipt.Released.PassedTestCount == 0 {
		failure.Mismatch = append(failure.Mismatch,
			"released.passed_test_count is 0, so the released suite passed by running nothing")
	}
	if receipt.Candidate.PassedTestCount == 0 {
		failure.Mismatch = append(failure.Mismatch,
			"candidate.passed_test_count is 0, so the candidate suite passed by running nothing")
	}
	return failure.resolve()
}

// ControllerGapCeiling is the most evidence gaps the campaign will admit.
//
// The frozen receipt from pipeline 232 reports exactly this many, so the gate
// currently sits on its boundary: one more gap fires it. That is recorded here
// rather than in a comment beside the number, because a ceiling equal to the
// observed value is worth noticing when it next moves in either direction.
const ControllerGapCeiling = 6

// CheckControllerDifferentialReceipt replaces
//
//	jq -e '(.plan.evidence_gap_count | type) == "number" and .plan.evidence_gap_count <= 6'
//
// THE TYPE TEST WAS LOAD-BEARING IN jq AND IS FREE HERE. `null <= 6` is true in
// jq, so the obvious filter passed when the field was absent -- green exactly
// when the receipt stopped carrying the number the gate exists to bound. In Go
// an absent field decodes to zero, which passes the ceiling for a different
// wrong reason, so the count is checked against the plan it came from instead.
func CheckControllerDifferentialReceipt(receipt ControllerDifferentialReceipt) error {
	failure := &GateFailure{Gate: "catalog-controller-differential"}
	if receipt.Plan.EvidenceGapCount > ControllerGapCeiling {
		failure.Mismatch = append(failure.Mismatch,
			fmt.Sprintf("plan.evidence_gap_count is %d, want at most %d",
				receipt.Plan.EvidenceGapCount, ControllerGapCeiling))
	}
	// An absent count and a genuine zero are the same value in Go. They are
	// told apart by the surfaces the plan lists: a plan with surfaces and no
	// gaps is a real state, a receipt with neither has not been filled in.
	if receipt.Plan.EvidenceGapCount == 0 && len(receipt.Plan.Surfaces) == 0 {
		failure.Mismatch = append(failure.Mismatch,
			"plan.evidence_gap_count is 0 and plan lists no surfaces, so the plan is absent rather than clean")
	}
	return failure.resolve()
}
