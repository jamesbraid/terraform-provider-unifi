package controllerdifferential

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ubiquiti-community/terraform-provider-unifi/internal/catalogparity"
)

// synthesiseLog builds a go test -json log that reports the given outcomes, so
// a summary can be exercised without a controller.
func synthesiseLog(passed, skipped, failed, packageOutput []string) []byte {
	var lines []string
	add := func(action string, tests []string) {
		for _, name := range tests {
			line, _ := json.Marshal(map[string]any{"Action": action, "Package": "unifi", "Test": name})
			lines = append(lines, string(line))
			// A run event and a subtest, neither of which may reach the counts:
			// the run event is not an outcome, and the subtest is not a planned
			// name. Both appear in every real log.
			run, _ := json.Marshal(map[string]any{"Action": "run", "Package": "unifi", "Test": name})
			sub, _ := json.Marshal(map[string]any{"Action": action, "Package": "unifi", "Test": name + "/case"})
			lines = append(lines, string(run), string(sub))
		}
	}
	add("pass", passed)
	add("skip", skipped)
	add("fail", failed)
	for _, text := range packageOutput {
		line, _ := json.Marshal(map[string]any{"Action": "output", "Package": "unifi", "Output": text})
		lines = append(lines, string(line))
	}
	if len(lines) == 0 {
		return nil
	}
	return []byte(strings.Join(lines, "\n") + "\n")
}

// TestTheFrozenSuitesAreReproducedFromTheirOwnOutcomes replays each frozen
// suite: a log is synthesised that reports exactly what that suite reported,
// and the summariser must reach the same verdict on the same plan.
//
// The released side is the one worth having. It is the ONLY suite in the
// campaign that reaches accepted_limitation -- seventeen tests the released
// provider does not have, declared in advance -- and that branch had never been
// executed by any test, because the jq file it lived in could not be called.
func TestTheFrozenSuitesAreReproducedFromTheirOwnOutcomes(t *testing.T) {
	receipt := frozenReceipt(t)
	for _, c := range []struct {
		label string
		suite catalogparity.ControllerSuiteReceipt
	}{
		{"released", receipt.Released},
		{"candidate", receipt.Candidate},
	} {
		t.Run(c.label, func(t *testing.T) {
			log := synthesiseLog(c.suite.Passed, c.suite.Skipped, c.suite.Failed, nil)
			got := SummariseSuite(log, receipt.Plan, c.label, c.suite.ExitCode)

			if got.Result != c.suite.Result {
				t.Fatalf("result %q, want %q", got.Result, c.suite.Result)
			}
			for _, field := range []struct {
				name        string
				got, frozen []string
			}{
				{"passed", got.Passed, c.suite.Passed},
				{"skipped", got.Skipped, c.suite.Skipped},
				{"failed", got.Failed, c.suite.Failed},
				{"accepted_failures", got.AcceptedFailures, c.suite.AcceptedFailures},
				{"unexpected_failures", got.UnexpectedFailures, c.suite.UnexpectedFailures},
				{"missing", got.Missing, c.suite.Missing},
			} {
				if !sameSet(field.got, field.frozen) {
					t.Fatalf("%s: got %v, frozen %v", field.name, field.got, field.frozen)
				}
			}
		})
	}
}

// TestTheReleasedSuiteActuallyExercisesTheAcceptedBranch states what the test
// above is worth. If the frozen released suite were a plain pass, replaying it
// would exercise nothing that the candidate replay does not.
func TestTheReleasedSuiteActuallyExercisesTheAcceptedBranch(t *testing.T) {
	receipt := frozenReceipt(t)
	if receipt.Released.Result != "accepted_limitation" {
		t.Fatalf("the frozen released suite is %q, so the accepted_limitation branch is not "+
			"exercised by replaying it", receipt.Released.Result)
	}
	if len(receipt.Released.Missing) == 0 {
		t.Fatal("the frozen released suite is missing nothing, so the allowance is unexercised")
	}
}

// TestOnlyTheReleasedSideMayFallShort. The candidate is the new provider: a
// test it cannot pass is a regression, and no policy entry excuses one.
func TestOnlyTheReleasedSideMayFallShort(t *testing.T) {
	plan := catalogparity.ControllerPlanReceipt{
		TestNames:              []string{"TestAccOne", "TestAccTwo"},
		AllowedSkips:           []string{},
		ReleasedAllowedMissing: []string{"TestAccTwo"},
	}
	log := synthesiseLog([]string{"TestAccOne"}, nil, nil, nil)

	if got := SummariseSuite(log, plan, "released", 0); got.Result != "accepted_limitation" {
		t.Fatalf("released side with a declared missing test is %q", got.Result)
	}
	if got := SummariseSuite(log, plan, "candidate", 0); got.Result != "fail" {
		t.Fatalf("the candidate reached %q with a test it never ran. released_allowed_missing "+
			"is a licence for the OLD provider, not for the new one", got.Result)
	}
}

// TestALogAndAnExitCodeThatDisagreeIsAFailure. A non-zero exit with nothing
// failed, or a zero exit with something failed, means the process and the log
// describe different runs -- and neither can then be trusted.
func TestALogAndAnExitCodeThatDisagreeIsAFailure(t *testing.T) {
	plan := catalogparity.ControllerPlanReceipt{
		TestNames:               []string{"TestAccOne", "TestAccTwo"},
		AllowedSkips:            []string{},
		ReleasedAllowedFailures: []string{"TestAccTwo"},
	}
	agreeing := synthesiseLog([]string{"TestAccOne"}, nil, []string{"TestAccTwo"}, nil)
	if got := SummariseSuite(agreeing, plan, "released", 1); got.Result != "accepted_limitation" {
		t.Fatalf("a declared failure with a non-zero exit is %q", got.Result)
	}
	if got := SummariseSuite(agreeing, plan, "released", 0); got.Result != "fail" {
		t.Fatalf("a failing test with a ZERO exit reached %q. The log says something failed and "+
			"the process says nothing did", got.Result)
	}
	clean := synthesiseLog([]string{"TestAccOne", "TestAccTwo"}, nil, nil, nil)
	if got := SummariseSuite(clean, plan, "released", 3); got.Result != "fail" {
		t.Fatalf("a clean log with a non-zero exit reached %q. Something killed the suite after "+
			"the last event", got.Result)
	}
}

// TestASuiteThatNeverRanKeepsItsPackageOutput. Without it the receipt records a
// suite with nothing in it, which reads exactly like a suite with nothing to do.
func TestASuiteThatNeverRanKeepsItsPackageOutput(t *testing.T) {
	plan := catalogparity.ControllerPlanReceipt{TestNames: []string{"TestAccOne"}, AllowedSkips: []string{}}
	log := synthesiseLog(nil, nil, nil, []string{"cannot connect to the controller\n", "exit status 1\n"})
	got := SummariseSuite(log, plan, "candidate", 1)

	if got.Result != "fail" {
		t.Fatalf("a suite that ran nothing is %q", got.Result)
	}
	if len(got.PreTestDiagnostics) == 0 {
		t.Fatal("a suite that produced no test outcome kept no package output, so the receipt " +
			"cannot say why it did not run")
	}
	// And a suite that DID run keeps none, so the diagnostics are not noise on
	// every receipt.
	ran := synthesiseLog([]string{"TestAccOne"}, nil, nil, []string{"ok  unifi\n"})
	if diagnostics := SummariseSuite(ran, plan, "candidate", 0).PreTestDiagnostics; len(diagnostics) != 0 {
		t.Fatalf("a suite that ran kept %v as pre-test diagnostics", diagnostics)
	}
}

// TestSubtestsAndUnplannedTestsDoNotCount. go test reports TestAccFoo/case as
// its own event; counting those would make one planned test look like five.
func TestSubtestsAndUnplannedTestsDoNotCount(t *testing.T) {
	plan := catalogparity.ControllerPlanReceipt{TestNames: []string{"TestAccOne"}, AllowedSkips: []string{}}
	log := synthesiseLog([]string{"TestAccOne", "TestAccSomethingElse"}, nil, nil, nil)
	got := SummariseSuite(log, plan, "candidate", 0)
	if len(got.Passed) != 1 || got.Passed[0] != "TestAccOne" {
		t.Fatalf("counted %v; only planned tests count, and a subtest is not a planned name", got.Passed)
	}
}

// TestTheSkipComparisonIsASetAndTheShellsWasNot is a deliberate divergence,
// recorded rather than hidden.
//
// The jq compared $skipped -- which came out of `unique`, so sorted -- against
// $allowed_skips, which came out of the policy file in whatever order it was
// written. That works only because the policy happens to be sorted. Reordering
// three lines in a JSON file would have failed the gate with a message about
// skips.
func TestTheSkipComparisonIsASetAndTheShellsWasNot(t *testing.T) {
	plan := catalogparity.ControllerPlanReceipt{
		TestNames:    []string{"TestAccAlpha", "TestAccZulu"},
		AllowedSkips: []string{"TestAccZulu", "TestAccAlpha"}, // deliberately unsorted
	}
	log := synthesiseLog(nil, []string{"TestAccAlpha", "TestAccZulu"}, nil, nil)
	if got := SummariseSuite(log, plan, "candidate", 0); got.Result != "pass" {
		t.Fatalf("an unsorted allowed-skip list produced %q. The comparison must be about the "+
			"set of skipped tests, not about the order somebody wrote them in", got.Result)
	}
}

func frozenReceipt(t *testing.T) catalogparity.ControllerDifferentialReceipt {
	t.Helper()
	path := filepath.Join(repositoryRoot, "build", "migration-baseline", "catalog-controller-differential.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var receipt catalogparity.ControllerDifferentialReceipt
	if err := json.Unmarshal(raw, &receipt); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
	return receipt
}
