package controllerdifferential

import (
	"fmt"
	"sort"
	"strings"

	"github.com/ubiquiti-community/terraform-provider-unifi/internal/catalogparity"
)

// CampaignCounts are the campaign policy's declared totals, which a full run's
// plan must match exactly.
//
// They are what stops a run from quietly covering less than the campaign says
// it covers: a plan built from a drifted inventory would still be internally
// consistent, and only a comparison against the committed numbers can see it.
type CampaignCounts struct {
	SurfaceCount     int `json:"surface_count"`
	EvidenceGapCount int `json:"evidence_gap_count"`
	TestNameCount    int `json:"test_name_count"`
}

// Followup reports what a controller receipt was: a complete campaign run, or a
// deliberately narrowed diagnostic one.
//
// It replaces catalog-controller-followup.sh, whose single caller runs it TWICE
// -- once grepping for diagnostic_complete and again for full -- because a
// shell script that prints a word is the only way a workflow can ask a question
// of it. One call answers both here.
func Followup(receipt catalogparity.ControllerDifferentialReceipt, policy CampaignPolicy, counts CampaignCounts) (string, error) {
	var problems []string
	add := func(format string, args ...any) { problems = append(problems, fmt.Sprintf(format, args...)) }

	// The overall result must AGREE WITH THE EVIDENCE GAP COUNT rather than be
	// a fixed string.
	//
	// The shell required result == "blocked_evidence" outright. That is true
	// today, because the campaign has six declared gaps -- and it means the
	// gate FAILS ON SUCCESS: close the gaps, set the policy's count to zero,
	// and the run produces "pass" while this check still demands
	// "blocked_evidence". A check that breaks at the moment the thing it
	// guards starts working is one somebody deletes rather than reads. The
	// invariant it was reaching for is the agreement, so that is what is
	// checked.
	//
	// DELEGATED RATHER THAN REPEATED. This function used to map the gap count to
	// the wanted result itself, which made it the SIXTH site holding that rule
	// and the only one not asking catalogparity for it -- admission.go:660,
	// hardware.go:27, migration.go:295 and cmd/catalog-pragmatic-evidence all go
	// through the helper, and ControllerResultForGaps is its single home. A rule
	// with two implementations is a rule that can disagree with itself, which is
	// the shape this lane exists to remove and would have been reintroduced by
	// the fix for it.
	//
	// The error is COLLECTED as a problem rather than returned, so a receipt
	// with several faults still reports all of them in one pass. A gate that
	// stops at the first fault makes the operator re-run to find the second.
	if err := catalogparity.RequireControllerResultAgreesWithGaps(receipt); err != nil {
		add("%v", err)
	}

	problems = append(problems, releasedShortfallProblems(receipt)...)
	problems = append(problems, cleanSuiteProblems("candidate", receipt.Candidate)...)

	if receipt.Plan.SurfaceCount != counts.SurfaceCount {
		add("the plan covers %d surface(s), the campaign declares %d",
			receipt.Plan.SurfaceCount, counts.SurfaceCount)
	}
	if receipt.Plan.EvidenceGapCount != counts.EvidenceGapCount {
		add("the plan reports %d evidence gap(s), the campaign declares %d",
			receipt.Plan.EvidenceGapCount, counts.EvidenceGapCount)
	}

	if receipt.Plan.DiagnosticSelection {
		if receipt.Plan.CatalogTestCount != counts.TestNameCount {
			add("a diagnostic run narrowed a catalog of %d test(s); the campaign declares %d",
				receipt.Plan.CatalogTestCount, counts.TestNameCount)
		}
		// A narrowed run must be a PROPER subset. Zero tests is a run that did
		// nothing; the whole catalog is a full run wearing a diagnostic label,
		// and calling that complete would let a narrowing that narrowed nothing
		// stand in for the campaign.
		if len(receipt.Plan.TestNames) == 0 {
			add("a diagnostic run selected no tests at all")
		}
		if len(receipt.Plan.TestNames) >= receipt.Plan.CatalogTestCount {
			add("a diagnostic run selected %d of %d tests, which is not a narrowing",
				len(receipt.Plan.TestNames), receipt.Plan.CatalogTestCount)
		}
		if len(problems) > 0 {
			return "", report(problems)
		}
		return "diagnostic_complete", nil
	}

	if len(receipt.Plan.TestNames) != counts.TestNameCount {
		add("a full run covers %d test(s), the campaign declares %d",
			len(receipt.Plan.TestNames), counts.TestNameCount)
	}
	// The dispositions must be the campaign's, entire. A run that dropped one
	// would be a narrower campaign presented as the whole one -- and comparing
	// as SETS rather than as arrays, because the shell compared jq arrays and
	// would have failed on a reordering of the policy file.
	for _, disposition := range []struct {
		name       string
		plan, want []string
	}{
		{"allowed_skips", receipt.Plan.AllowedSkips, policy.AllowedSkips},
		{"released_allowed_failures", receipt.Plan.ReleasedAllowedFailures, policy.ReleasedAllowedFailures},
		{"released_allowed_missing", receipt.Plan.ReleasedAllowedMissing, policy.ReleasedAllowedMissing},
	} {
		if !sameSet(disposition.plan, disposition.want) {
			add("%s in the plan is %v, the campaign declares %v",
				disposition.name, sorted(disposition.plan), sorted(disposition.want))
		}
	}
	if len(problems) > 0 {
		return "", report(problems)
	}
	return "full", nil
}

// releasedShortfallProblems allows the released side exactly what the plan
// declared in advance, and nothing else.
func releasedShortfallProblems(receipt catalogparity.ControllerDifferentialReceipt) []string {
	released := receipt.Released
	switch released.Result {
	case "pass":
		return cleanSuiteProblems("released", released)
	case "accepted_limitation":
		var problems []string
		if !sameSet(released.AcceptedFailures, released.Failed) {
			problems = append(problems, fmt.Sprintf(
				"the released suite failed %v but accepted %v; an unaccepted failure is not a limitation",
				sorted(released.Failed), sorted(released.AcceptedFailures)))
		}
		if extra := without(released.Failed, receipt.Plan.ReleasedAllowedFailures); len(extra) > 0 {
			problems = append(problems, fmt.Sprintf(
				"the released suite failed %v, which the plan did not allow", sorted(extra)))
		}
		if len(released.UnexpectedFailures) > 0 {
			problems = append(problems, fmt.Sprintf(
				"the released suite reports unexpected failures %v", sorted(released.UnexpectedFailures)))
		}
		// EXACT, WHERE THE FAILURES ABOVE ARE A SUBSET, and the asymmetry is the
		// point rather than an oversight.
		//
		// released_allowed_failures is a LICENCE: the released provider may
		// fail these. Failing fewer than licensed uses less of the permission,
		// and no claim is invalidated -- so a subset is right.
		//
		// released_allowed_missing is a CLAIM ABOUT THE RELEASED TREE: these
		// tests are not in it. If one of them runs, the tree contains a test
		// the plan said it lacked, and the plan is describing something other
		// than the tree it is about. Everything downstream reads that plan, so
		// the right response is to fail rather than to treat a shrinking
		// Missing as an improvement.
		//
		// The two ways it can shrink are both plan defects, not campaign
		// progress: a scenario file lent to the released tree brings its tests
		// with it, or the policy names a test that is no longer new. BuildPlan
		// now refuses the first case at the source, so a failure here is the
		// second.
		if !sameSet(released.Missing, receipt.Plan.ReleasedAllowedMissing) {
			problems = append(problems, fmt.Sprintf(
				"the released suite is missing %v, the plan allows %v",
				sorted(released.Missing), sorted(receipt.Plan.ReleasedAllowedMissing)))
		}
		return problems
	default:
		return []string{fmt.Sprintf("the released suite result is %q", released.Result)}
	}
}

// cleanSuiteProblems requires a suite to have nothing outstanding at all.
func cleanSuiteProblems(label string, suite catalogparity.ControllerSuiteReceipt) []string {
	var problems []string
	if suite.Result != "pass" {
		problems = append(problems, fmt.Sprintf("the %s suite result is %q", label, suite.Result))
	}
	for _, outstanding := range []struct {
		what   string
		values []string
	}{
		{"accepted failures", suite.AcceptedFailures},
		{"unexpected failures", suite.UnexpectedFailures},
		{"failures", suite.Failed},
		{"missing tests", suite.Missing},
	} {
		if len(outstanding.values) > 0 {
			problems = append(problems, fmt.Sprintf("the %s suite reports %s %v",
				label, outstanding.what, sorted(outstanding.values)))
		}
	}
	return problems
}

// report returns every problem rather than the first. The shell was a single jq
// -e expression: it printed nothing at all and exited 1, so an operator saw a
// failed step and had to re-derive which of its fourteen conjuncts was false.
func report(problems []string) error {
	sort.Strings(problems)
	return fmt.Errorf("the controller receipt does not describe an acceptable run:\n  %s",
		strings.Join(problems, "\n  "))
}

func sorted(values []string) []string {
	out := append([]string(nil), values...)
	sort.Strings(out)
	return out
}
