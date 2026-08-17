package controllerdifferential

import (
	"bytes"
	"encoding/json"
	"sort"

	"github.com/ubiquiti-community/terraform-provider-unifi/internal/catalogparity"
)

// maxPreTestDiagnostics caps the lines kept when a suite produced no test
// outcome at all. The shell used 40; the number is arbitrary and the cap is
// not -- an unbounded copy of a failed container's output into a receipt makes
// the receipt unreadable and unstorable.
const maxPreTestDiagnostics = 40

// SummariseSuite reduces one controller suite's log to its receipt.
//
// It replaces .woodpecker/scripts/catalog-controller-summary.jq. That file was
// the entire meaning of the gate -- what passed, what may fail, what counts as
// an accepted limitation -- and nothing could call it, so the branch that
// decides whether the RELEASED side is allowed to fall short had never been
// executed by a test.
//
// ONLY PLANNED TESTS COUNT. An event whose Test is not in the plan is ignored,
// which is how subtests stay out of the counts: go test reports TestAccFoo/case
// as its own event, and counting those would make one planned test look like
// five passes.
func SummariseSuite(raw []byte, plan catalogparity.ControllerPlanReceipt, label string, exitCode int) catalogparity.ControllerSuiteReceipt {
	planned := plan.TestNames
	allowedSkips := plan.AllowedSkips
	// The released side alone may fall short, because it is the OLD provider
	// being judged partly by the candidate's tests. The candidate has no such
	// licence: a test the candidate cannot pass is a regression.
	var allowedFailures, allowedMissing []string
	if label == "released" {
		allowedFailures = plan.ReleasedAllowedFailures
		allowedMissing = plan.ReleasedAllowedMissing
	}

	inPlan := map[string]bool{}
	for _, name := range planned {
		inPlan[name] = true
	}
	passedSet, skippedSet, failedSet := map[string]bool{}, map[string]bool{}, map[string]bool{}
	var packageOutput []string
	for _, line := range bytes.Split(raw, []byte("\n")) {
		if len(line) == 0 {
			continue
		}
		var event struct {
			Action string  `json:"Action"`
			Test   *string `json:"Test"`
			Output string  `json:"Output"`
		}
		if err := json.Unmarshal(line, &event); err != nil {
			continue
		}
		if event.Test == nil {
			if event.Action == "output" {
				packageOutput = append(packageOutput, event.Output)
			}
			continue
		}
		if !inPlan[*event.Test] {
			continue
		}
		switch event.Action {
		case "pass":
			passedSet[*event.Test] = true
		case "skip":
			skippedSet[*event.Test] = true
		case "fail":
			failedSet[*event.Test] = true
		}
	}

	receipt := catalogparity.ControllerSuiteReceipt{
		ExitCode:           exitCode,
		Passed:             keys(passedSet),
		Skipped:            keys(skippedSet),
		Failed:             keys(failedSet),
		PreTestDiagnostics: []string{},
	}
	receipt.AcceptedFailures = intersect(receipt.Failed, allowedFailures)
	receipt.UnexpectedFailures = without(receipt.Failed, allowedFailures)
	receipt.Missing = without(without(without(planned, receipt.Passed), receipt.Skipped), receipt.Failed)

	// A suite that produced no test outcome at all did not run. Keeping the
	// package-level output is the only way to say WHY -- a controller that
	// never came up, an image that is not there, a compile failure -- and
	// without it the receipt records a suite with nothing in it, which reads
	// the same as a suite with nothing to do.
	if len(receipt.Passed) == 0 && len(receipt.Skipped) == 0 && len(receipt.Failed) == 0 {
		diagnostics := uniqueSorted(packageOutput)
		if len(diagnostics) > maxPreTestDiagnostics {
			diagnostics = diagnostics[:maxPreTestDiagnostics]
		}
		receipt.PreTestDiagnostics = diagnostics
	}

	receipt.Result = suiteResult(receipt, planned, allowedSkips, allowedFailures, allowedMissing, label, exitCode)
	return receipt
}

// suiteResult is the gate's actual verdict, and its three outcomes answer
// different questions.
//
// pass: everything planned ran, everything that was meant to be skipped was
// skipped, nothing failed.
//
// accepted_limitation: the RELEASED side only, and only where the plan declared
// in advance what it may fail or lack. It also requires the exit code to AGREE
// with the failures -- a non-zero exit with nothing failed, or a zero exit with
// something failed, means the log and the process disagree about what happened,
// and neither of them can then be trusted to describe the run.
//
// fail: anything else, including any shortfall on the candidate side.
//
// THE COMPARISONS ARE SETS. The shell compared jq arrays, which is order
// sensitive: $skipped came out of `unique` (sorted) while $allowed_skips came
// out of the policy file in whatever order it was written. That worked because
// the policy happens to be sorted. Reordering three lines in a JSON file would
// have failed this gate with a message about skips.
func suiteResult(receipt catalogparity.ControllerSuiteReceipt,
	planned, allowedSkips, allowedFailures, allowedMissing []string, label string, exitCode int,
) string {
	if exitCode == 0 && sameSet(receipt.Skipped, allowedSkips) &&
		len(receipt.Failed) == 0 && sameSet(receipt.Passed, without(planned, allowedSkips)) {
		return "pass"
	}

	// ONE GATE, NOT TWO. The licence to fall short is granted in exactly one
	// place -- the caller only fills allowedFailures and allowedMissing for the
	// released side -- so a `label != "released"` test here would be a check
	// that cannot fail, with the emptiness guard always firing first. That is
	// the shape this package exists to remove, and it was in this function
	// until a mutation run found it: deleting the label test changed no test's
	// verdict.
	if len(allowedFailures) == 0 && len(allowedMissing) == 0 {
		return "fail"
	}
	exitAgreesWithFailures := (len(receipt.Failed) > 0 && exitCode != 0) ||
		(len(receipt.Failed) == 0 && exitCode == 0)
	if !exitAgreesWithFailures {
		return "fail"
	}
	if !sameSet(receipt.Skipped, allowedSkips) || len(receipt.UnexpectedFailures) > 0 {
		return "fail"
	}
	expectedPasses := without(without(without(planned, allowedSkips), receipt.Failed), allowedMissing)
	if !sameSet(receipt.Passed, expectedPasses) || !sameSet(receipt.Missing, allowedMissing) {
		return "fail"
	}
	return "accepted_limitation"
}

func keys(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for value := range set {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func without(values, removed []string) []string {
	drop := map[string]bool{}
	for _, name := range removed {
		drop[name] = true
	}
	out := []string{}
	for _, value := range values {
		if !drop[value] {
			out = append(out, value)
		}
	}
	return out
}

func sameSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	present := map[string]bool{}
	for _, value := range a {
		present[value] = true
	}
	for _, value := range b {
		if !present[value] {
			return false
		}
	}
	return true
}
