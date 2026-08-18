package catalogparity

import (
	"strings"

	"github.com/ubiquiti-community/terraform-provider-unifi/internal/receiptcheck"
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
//
// IT IS NOW AN ALIAS rather than a second implementation. receiptcheck.Failure
// carries the same two fields and the same message format, so err.(*GateFailure)
// still succeeds and .Mismatch still counts -- an alias rather than a rename
// because the assertion is the contract, and a distinct type would have broken
// it silently at the one call site that reads the error rather than its text.
type GateFailure = receiptcheck.Failure

// CheckBuildSchemaReceipt replaces
//
//	jq -e '.result == "pass" and .promotion_blockers == [] and .build_network == "none"'
//
// build_network is the one that carries information rather than restating the
// script's own success: it records whether the build reached the network, which
// the script cannot decide for itself after the fact.
func CheckBuildSchemaReceipt(receipt BuildSchemaReceipt) error {
	return receiptcheck.Run("catalog-build-schema",
		receiptcheck.Equals("result", receipt.Result, "pass"),
		receiptcheck.Equals("build_network", receipt.BuildNetwork, "none"),
		receiptcheck.Custom(len(receipt.PromotionBlockers) == 0,
			"promotion_blockers has %d entr(ies): %s",
			len(receipt.PromotionBlockers), strings.Join(receipt.PromotionBlockers, ", ")),
	)
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
	rules := []receiptcheck.Rule{
		receiptcheck.Equals("result", receipt.Result, "pass"),
		receiptcheck.Equals("network", receipt.Network, "none"),
		receiptcheck.Equals("released.result", receipt.Released.Result, "pass"),
		receiptcheck.Equals("candidate.result", receipt.Candidate.Result, "pass"),
		receiptcheck.Custom(len(receipt.PromotionBlockers) == 0,
			"promotion_blockers has %d entr(ies): %s",
			len(receipt.PromotionBlockers), strings.Join(receipt.PromotionBlockers, ", ")),

		// NOT IN THE YAML VERSION, AND THE REASON IT IS HERE. A suite that ran
		// nothing reports result "pass" with zero tests, so the four assertions
		// above all hold on a run that tested nothing at all. The shell receipt
		// already carries the counts; nothing was looking at them.
		receiptcheck.Custom(receipt.Released.PassedTestCount != 0,
			"released.passed_test_count is 0, so the released suite passed by running nothing"),
		receiptcheck.Custom(receipt.Candidate.PassedTestCount != 0,
			"candidate.passed_test_count is 0, so the candidate suite passed by running nothing"),
	}
	return receiptcheck.Run("catalog-unit-differential", rules...)
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
//
// IT ALSO ABSORBS THE .result CONJUNCT THE WORKFLOW USED TO CARRY, which was the
// last of task 160's doors: the YAML required the literal "blocked_evidence", so
// the gate would have turned red on the run the campaign is working towards.
// Asked of ControllerResultForGaps rather than written out again here -- that
// literal had six homes, and a seventh would be the defect rather than the fix.
func CheckControllerDifferentialReceipt(receipt ControllerDifferentialReceipt) error {
	agreement := RequireControllerResultAgreesWithGaps(receipt)

	// THE ENVELOPE RULES COME FIRST AND THEY ARE SHARED. format_version and gate
	// were never checked by this gate; format_version alone is compared against
	// the bare literal 1 in 43 places across the tree, agreeing by accident
	// because nothing defines it. receiptcheck.FormatVersion now does.
	//
	// Result is deliberately absent from the expectation: it must FOLLOW from
	// the gap count rather than equal a literal, which is what
	// RequireControllerResultAgreesWithGaps decides below.
	rules := receiptcheck.EnvelopeRules(receipt.EnvelopeView(), receiptcheck.EnvelopeExpectation{
		Gate: "catalog controller differential",
	})

	rules = append(rules,
		receiptcheck.Custom(agreement == nil, "%s", errorText(agreement)),
		receiptcheck.Custom(receipt.Plan.EvidenceGapCount <= ControllerGapCeiling,
			"plan.evidence_gap_count is %d, want at most %d",
			receipt.Plan.EvidenceGapCount, ControllerGapCeiling),
		// An absent count and a genuine zero are the same value in Go. They are
		// told apart by the surfaces the plan lists: a plan with surfaces and no
		// gaps is a real state, a receipt with neither has not been filled in.
		receiptcheck.Custom(receipt.Plan.EvidenceGapCount != 0 || len(receipt.Plan.Surfaces) != 0,
			"plan.evidence_gap_count is 0 and plan lists no surfaces, so the plan is absent rather than clean"),
	)
	return receiptcheck.Run("catalog-controller-differential", rules...)
}

// errorText renders an error for a Custom rule that only fires when it is
// non-nil. Guarding here rather than at each call site keeps the rule list flat.
func errorText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
