package testaudit

import (
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
)

func repositoryRoot(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate this source file, so the repository root cannot be resolved")
	}
	return filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
}

// Test_noUnfailableTests is the gate: zero tolerance, no ledger of standing
// exceptions. A test that cannot fail gets fixed or deleted in the same
// change that would otherwise have added it here.
func Test_noUnfailableTests(t *testing.T) {
	root := repositoryRoot(t)
	findings, err := Scan(root)
	if err != nil {
		t.Fatalf("scanning %s: %v", root, err)
	}
	if len(findings) == 0 {
		return
	}
	lines := make([]string, len(findings))
	for i, f := range findings {
		lines[i] = f.String()
	}
	sort.Strings(lines)
	t.Errorf("%d test(s) that cannot fail:\n    %s\n\n"+
		"    Each of these will pass whatever the code under test does. A test\n"+
		"    named after a behaviour reads as coverage of it -- to the reviewer,\n"+
		"    to the release checklist, and to whoever next decides where the risk\n"+
		"    is. Make it assert something, or delete it: a missing test is an\n"+
		"    absence a reader can see; a passing one that checks nothing is not.",
		len(findings), strings.Join(lines, "\n    "))
}

// ---------------------------------------------------------------------------
// The analyzer's own proof that it can fail, and that it does not fire
// wrongly. A survey tool that over-reports is worse than none: a single
// false positive is enough for a reader to stop trusting the list, and
// then the real entries go unread too. So the fixtures assert both
// directions, and the ones that must not be reported outnumber the ones
// that must.

func scanFixture(t *testing.T, name string) map[string]Kind {
	t.Helper()
	dir := filepath.Join("testdata", name)
	findings, err := Scan(dir)
	if err != nil {
		t.Fatalf("scanning %s: %v", dir, err)
	}
	byName := map[string]Kind{}
	for _, f := range findings {
		byName[f.Name] = f.Kind
	}
	return byName
}

func Test_scanReportsTheThreeShapes(t *testing.T) {
	got := scanFixture(t, "unfailable")

	want := map[string]Kind{
		"TestNothingHappens":        NoAssertion,
		"TestEmptyTable":            EmptyTable,
		"TestPopulatedButMute":      NoAssertion,
		"TestUnconditionalSkip":     SkipStub,
		"TestEmptyMapNeverWritten":  EmptyTable,
		"TestVarDeclaredEmptyTable": EmptyTable,
	}
	for name, kind := range want {
		if got[name] != kind {
			t.Errorf("%s: reported %q, want %q", name, got[name], kind)
		}
	}
}

func Test_scanDoesNotReportATestThatCanFail(t *testing.T) {
	got := scanFixture(t, "failable")

	// Every one of these can fail. Any of them appearing is a false positive,
	// and the helper cases are the ones that matter: a test whose only
	// assertion is one call away is the common shape in this repository.
	for _, name := range []string{
		"TestDirectAssertion",
		"TestViaHelper",
		"TestViaHelperChain",
		"TestViaRequire",
		"TestSubtestAsserts",
		"TestConditionalSkipStillRuns",
		"TestTableWithCases",
		// The three below are about which way the default falls when the
		// analyzer cannot resolve what is being ranged over. An accumulator is
		// declared empty and filled; a directory listing cannot be resolved at
		// all. "I could not tell" and "this cannot fail" are different
		// statements, and reporting the second when only the first is true is
		// the disease this package exists to find.
		"TestAccumulatorIsNotAnEmptyTable",
		"TestAppendAccumulatorIsNotAnEmptyTable",
		"TestRangeOverRuntimeValueIsNotAnEmptyTable",
		"TestVarDeclaredTableFilledLater",
		"TestVarDeclaredTableFilledByPointerArgument",
	} {
		if kind, reported := got[name]; reported {
			t.Errorf("%s can fail but was reported as %q", name, kind)
		}
	}
}

// Test_scanIsNotVacuous guards the two tests above. Both would pass if Scan
// returned nothing at all -- the first only checks named entries, the second
// only checks absence -- so the fixture must be shown to produce findings.
func Test_scanIsNotVacuous(t *testing.T) {
	if n := len(scanFixture(t, "unfailable")); n == 0 {
		t.Fatal("the unfailable fixture produced no findings, so the shape assertions above prove nothing")
	}
	if n := len(scanFixture(t, "failable")); n != 0 {
		t.Fatalf("the failable fixture produced %d finding(s); it exists to have none", n)
	}
}
