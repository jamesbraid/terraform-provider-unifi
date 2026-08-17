package catalogparity

import "fmt"

// ControllerResultForGaps is what a controller differential's top-level result
// must be, given how many evidence gaps its plan found.
//
// ONE HOME FOR A RULE THAT WAS WRITTEN OUT AT SIX SITES AS A LITERAL. The
// producer derives the result from the gap count -- pass when the catalog is
// complete, blocked_evidence when it is not -- and six independent consumers
// required the string "blocked_evidence" outright, with no alternative at any
// of them:
//
//	.woodpecker/catalog-controller-differential.yml   a silent jq -e gate
//	.woodpecker/scripts/catalog-controller-followup.sh
//	cmd/catalog-pragmatic-evidence
//	catalogparity.validateControllerDifferential
//	releasequalification.BuildHardwareDispositionReceipt
//	releasequalification.validateMigrationController
//
// THE CONSEQUENCE IS THAT THE RELEASE PATH ASSUMES THE CAMPAIGN NEVER
// COMPLETES. The moment the gaps close the producer emits "pass", and admission,
// hardware disposition and migration recovery all reject it -- so the success
// state is unreachable through six separate doors, and fixing one moves the wall
// rather than removing it. That is #113's shape: a check that manufactures a
// failure at the moment the thing it guards starts working.
//
// The invariant the literal was reaching for is the AGREEMENT. It is satisfied
// today, with six gaps outstanding, and stays satisfied when there are none.
func ControllerResultForGaps(evidenceGapCount int) string {
	if evidenceGapCount == 0 {
		return "pass"
	}
	return "blocked_evidence"
}

// RequireControllerResultAgreesWithGaps reports why a receipt's result does not
// follow from its own plan, or nil when it does.
//
// It reads the gap count from the RECEIPT rather than from a policy, because
// the question here is whether the receipt is internally consistent. Whether
// that gap count is the one the campaign declared is a separate comparison, and
// merging the two would let a receipt with the wrong number of gaps pass by
// agreeing with itself.
func RequireControllerResultAgreesWithGaps(receipt ControllerDifferentialReceipt) error {
	want := ControllerResultForGaps(receipt.Plan.EvidenceGapCount)
	if receipt.Result == want {
		return nil
	}
	return fmt.Errorf("controller differential result is %q with %d evidence gap(s); a plan with "+
		"%s gaps must report %q", receipt.Result, receipt.Plan.EvidenceGapCount,
		map[bool]string{true: "no", false: "outstanding"}[receipt.Plan.EvidenceGapCount == 0], want)
}
