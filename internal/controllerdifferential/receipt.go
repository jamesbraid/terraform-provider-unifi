package controllerdifferential

import (
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/catalogparity"
)

// Environment is what the run was executed against, all of it measured.
//
// The images are recorded by ID as well as by tag because a tag is a name
// somebody can move: two runs of "the controller image" are the same run only
// if the IDs agree, and the tag alone cannot say so.
type Environment struct {
	ControllerImage       string
	ControllerImageID     string
	SyntheticImage        string
	SyntheticImageID      string
	RyukImage             string
	RyukImageID           string
	HerderSHA256          string
	TerraformBinarySHA256 string
}

// Run is everything one controller differential observed.
type Run struct {
	Plan            catalogparity.ControllerPlanReceipt
	PlanSHA256      string
	Released        catalogparity.ControllerSuiteReceipt
	Candidate       catalogparity.ControllerSuiteReceipt
	ReleasedCommit  string
	CandidateCommit string
	Environment     Environment
	TreeState       *catalogparity.TreeState
}

// BuildReceipt assembles the controller differential receipt.
//
// RESULT IS DERIVED FROM THE TWO SUITES AND THE EVIDENCE GAP COUNT, and the
// three outcomes are three different statements:
//
//	fail             a suite fell short of what the plan allowed it
//	blocked_evidence both suites did what they were meant to, and the catalog
//	                 still has surfaces with no acceptance signal at all
//	pass             both suites, and nothing outstanding anywhere
//
// blocked_evidence is the one that matters, because it is the campaign's
// current state: the differential SUCCEEDED and the release is still blocked.
// Collapsing it into either neighbour would lose the distinction between "the
// comparison went wrong" and "the comparison went right and the catalog is
// incomplete", which are acted on by different people.
func BuildReceipt(run Run) catalogparity.ControllerDifferentialReceipt {
	result := "fail"
	releasedAcceptable := run.Released.Result == "pass" || run.Released.Result == "accepted_limitation"
	if releasedAcceptable && run.Candidate.Result == "pass" {
		result = "blocked_evidence"
		if run.Plan.EvidenceGapCount == 0 {
			result = "pass"
		}
	}

	return catalogparity.ControllerDifferentialReceipt{
		FormatVersion:   1,
		Gate:            "catalog controller differential",
		TreeState:       run.TreeState,
		Result:          result,
		PlanSHA256:      run.PlanSHA256,
		ReleasedCommit:  run.ReleasedCommit,
		CandidateCommit: run.CandidateCommit,
		Target: catalogparity.ControllerImageReceipt{
			Image:   run.Environment.ControllerImage,
			ImageID: run.Environment.ControllerImageID,
			// The images are never fetched by this gate: it refuses when one is
			// not already present locally. Recording the policy is what makes
			// that refusal auditable afterwards rather than only at the time.
			PullPolicy: "never",
		},
		Fleet: catalogparity.ControllerFleetReceipt{
			Image:        run.Environment.SyntheticImage,
			ImageID:      run.Environment.SyntheticImageID,
			HerderSHA256: run.Environment.HerderSHA256,
		},
		Testcontainers: catalogparity.ControllerRyukReceipt{
			RyukImage:   run.Environment.RyukImage,
			RyukImageID: run.Environment.RyukImageID,
		},
		TerraformBinarySHA256: run.Environment.TerraformBinarySHA256,
		Plan:                  run.Plan,
		Released:              run.Released,
		Candidate:             run.Candidate,
	}
}
