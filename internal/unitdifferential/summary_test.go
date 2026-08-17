package unitdifferential

import (
	"strings"
	"testing"
)

// go test -json events, as the suite actually emits them. Package-level results
// carry no Test field at all; test-level ones do.
const (
	packagePass = `{"Time":"2026-01-01T00:00:00Z","Action":"pass","Package":"example.com/a","Elapsed":0.1}`
	packageFail = `{"Time":"2026-01-01T00:00:00Z","Action":"fail","Package":"example.com/a","Elapsed":0.1}`
	testPass    = `{"Time":"2026-01-01T00:00:00Z","Action":"pass","Package":"example.com/a","Test":"TestOne","Elapsed":0.1}`
	testSkip    = `{"Time":"2026-01-01T00:00:00Z","Action":"skip","Package":"example.com/a","Test":"TestTwo","Elapsed":0}`
	testFail    = `{"Time":"2026-01-01T00:00:00Z","Action":"fail","Package":"example.com/a","Test":"TestThree","Elapsed":0.1}`
	testRun     = `{"Time":"2026-01-01T00:00:00Z","Action":"run","Package":"example.com/a","Test":"TestOne"}`
	testOutput  = `{"Time":"2026-01-01T00:00:00Z","Action":"output","Package":"example.com/a","Test":"TestOne","Output":"--- PASS\n"}`
)

func log(lines ...string) []byte { return []byte(strings.Join(lines, "\n") + "\n") }

func TestACleanRunPasses(t *testing.T) {
	got := Summarise(log(testRun, testOutput, testPass, testSkip, packagePass), 0)
	if got.Receipt.Result != "pass" {
		t.Fatalf("result = %q, want pass", got.Receipt.Result)
	}
	if got.Receipt.PackagePassCount != 1 || got.Receipt.PassedTestCount != 1 ||
		got.Receipt.SkippedTestCount != 1 {
		t.Fatalf("counts = %+v, want one package pass, one test pass, one skip", got.Receipt)
	}
	if got.Receipt.FailedTestCount != 0 || got.Receipt.PackageFailCount != 0 ||
		got.Receipt.UnparsedLineCount != 0 {
		t.Fatalf("counts = %+v, want no failures and no unparsed lines", got.Receipt)
	}
}

// TestEachFailureConditionFiresOnItsOwn is the point of the port. The shell had
// three conditions in one boolean expression and no way to exercise any of them
// separately, so nothing established that each could fail alone.
func TestEachFailureConditionFiresOnItsOwn(t *testing.T) {
	t.Run("a failing test, with a zero exit code", func(t *testing.T) {
		got := Summarise(log(testFail, packagePass), 0)
		if got.Receipt.Result != "fail" {
			t.Fatal("a reported test failure did not fail the suite. The exit code is not the " +
				"only evidence a run went wrong")
		}
		if got.Receipt.FailedTestCount != 1 {
			t.Fatalf("failed test count = %d, want 1", got.Receipt.FailedTestCount)
		}
	})

	t.Run("a non-zero exit code, with nothing reported failing", func(t *testing.T) {
		got := Summarise(log(testPass, packagePass), 2)
		if got.Receipt.Result != "fail" {
			t.Fatal("a non-zero exit was ignored because every reported outcome passed. That is " +
				"exactly the run where the harness died after the last event")
		}
		if got.Receipt.FailedTestCount != 0 {
			t.Fatal("a failure was invented; the log reported none")
		}
	})

	t.Run("an unparsed line, with a clean exit and no failures", func(t *testing.T) {
		// The suite's stderr goes into the same log, so a build error arrives
		// as text. It produces no failing event: without the unparsed-line
		// condition this run reads as a clean pass over the packages that did
		// build.
		got := Summarise(log("# example.com/a", "./broken.go:3:2: undefined: nope",
			testPass, packagePass), 0)
		if got.Receipt.UnparsedLineCount != 2 {
			t.Fatalf("unparsed line count = %d, want 2", got.Receipt.UnparsedLineCount)
		}
		if got.Receipt.FailedTestCount != 0 || got.Receipt.PackageFailCount != 0 {
			t.Fatal("a build error was counted as a test failure, which it is not")
		}
		if got.Receipt.Result != "fail" {
			t.Fatal("a log carrying a build error passed, because nothing in it FAILED. " +
				"No failures and nothing read are different states")
		}
	})
}

func TestPackageResultsAreNotCountedAsTests(t *testing.T) {
	got := Summarise(log(packageFail, testFail), 1)
	if got.Receipt.PackageFailCount != 1 || got.Receipt.FailedTestCount != 1 {
		t.Fatalf("counts = %+v, want one package failure and one test failure counted "+
			"separately. A package result has no test name and merging them would double "+
			"every failure", got.Receipt)
	}
}

func TestRepeatedEventsAreCountedOnce(t *testing.T) {
	got := Summarise(log(testPass, testPass, packagePass, packagePass), 0)
	if got.Receipt.PassedTestCount != 1 || got.Receipt.PackagePassCount != 1 {
		t.Fatalf("counts = %+v, want each outcome once. A retried or re-emitted event would "+
			"otherwise inflate the pass counts a reader compares between two runs", got.Receipt)
	}
	if len(got.Events) != 2 {
		t.Fatalf("events = %d, want 2", len(got.Events))
	}
}

func TestNonOutcomeActionsAreIgnored(t *testing.T) {
	got := Summarise(log(testRun, testOutput), 0)
	if len(got.Events) != 0 {
		t.Fatalf("run and output events became outcomes: %+v", got.Events)
	}
	if got.Receipt.UnparsedLineCount != 0 {
		t.Fatal("well-formed events were counted as unparsed")
	}
}

// TestEventsAreOrderedPackageResultFirst pins the ordering the digest is taken
// over. jq sorts null before every string, so a package's own result precedes
// its tests; a port that ordered them the other way would produce a different
// digest for an identical run.
func TestEventsAreOrderedPackageResultFirst(t *testing.T) {
	got := Summarise(log(testPass, packagePass, testSkip), 0)
	if len(got.Events) != 3 {
		t.Fatalf("events = %+v, want 3", got.Events)
	}
	if got.Events[0].Test != nil {
		t.Fatalf("first event is %+v, want the package result", got.Events[0])
	}
	if *got.Events[1].Test != "TestOne" || *got.Events[2].Test != "TestTwo" {
		t.Fatalf("tests are out of order: %+v", got.Events)
	}
}

func TestTheDigestMovesWithTheEvents(t *testing.T) {
	first, err := RenderEvents(Summarise(log(testPass, packagePass), 0).Events)
	if err != nil {
		t.Fatal(err)
	}
	second, err := RenderEvents(Summarise(log(testSkip, packagePass), 0).Events)
	if err != nil {
		t.Fatal(err)
	}
	if Digest(first) == Digest(second) {
		t.Fatal("two different outcome sets digest the same, so the recorded digest says " +
			"nothing about what the suite did")
	}
	again, err := RenderEvents(Summarise(log(packagePass, testPass), 0).Events)
	if err != nil {
		t.Fatal(err)
	}
	if Digest(first) != Digest(again) {
		t.Fatal("the same outcome set in a different log order digested differently. Receipts " +
			"are compared between runs, and go test does not promise an event order")
	}
}

func TestAnEmptyLogIsNotAPass(t *testing.T) {
	got := Summarise(nil, 0)
	if got.Receipt.PackagePassCount != 0 || got.Receipt.PassedTestCount != 0 {
		t.Fatalf("an empty log produced counts: %+v", got.Receipt)
	}
	// It IS a pass by the three conditions -- zero exit, no failures, nothing
	// unparsed -- and that is worth pinning rather than leaving as a surprise:
	// the gate's protection against a suite that ran nothing is the counts in
	// the receipt, not this result field.
	if got.Receipt.Result != "pass" {
		t.Fatalf("result = %q; an empty log meets all three conditions, and the check that a "+
			"run happened is the count fields", got.Receipt.Result)
	}
	rendered, err := RenderEvents(got.Events)
	if err != nil {
		t.Fatal(err)
	}
	if string(rendered) != "[]\n" {
		t.Fatalf("empty events rendered as %q, want an empty array. A null here would decode "+
			"to a nil slice and read as absent rather than as empty", rendered)
	}
}
