package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// managementcontract.SurfaceContract.AttemptResult is checked by two gates that
// cannot fail, because its only non-test writer is the literal paritydiff.Pass.
// The field carries a long comment saying so and referring to task 112, and
// that comment is right: the gate is correct and its INPUT is the defect.
//
// Wiring a real producer is a subsystem, not a call -- an adapter harness that
// runs both providers per scenario and captures observations -- so the decision
// is not this test's to make. What this test does is stop the premise decaying
// silently in either direction.
//
// The prose says "vacuous today". Today is the part that expires. If somebody
// wires a producer, the gates stop being vacuous and the comment becomes wrong
// in the direction that matters -- it would go on telling readers not to trust
// a check that had started working. If somebody adds a SECOND literal writer,
// the vacuity spreads while the comment still describes one site. Either way
// the file should say so, and prose cannot.
//
// WHAT THIS IS NOT: an assertion that the situation is acceptable. It fails
// when the situation CHANGES, which is the only service available until the
// decision is made.

var (
	attemptResultAssignment = regexp.MustCompile(`AttemptResult:\s*([A-Za-z0-9_.]+)`)
	parityCompareCall       = regexp.MustCompile(`\bparitydiff\.Compare\(`)
)

func TestAttemptResultPremiseStillHolds(t *testing.T) {
	type site struct {
		file  string
		line  int
		value string
	}
	var writers []site
	compareCalls := 0
	scanned := 0

	err := filepath.Walk(".", func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			// paritydiff owns the type and tests it thoroughly; its own
			// constructions are not production writers of SurfaceContract.
			if info.Name() == ".git" || path == filepath.Join("internal", "paritydiff") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		scanned++
		for number, line := range strings.Split(string(data), "\n") {
			if trimmed := strings.TrimSpace(line); strings.HasPrefix(trimmed, "//") {
				continue
			}
			if match := attemptResultAssignment.FindStringSubmatch(line); match != nil {
				writers = append(writers, site{path, number + 1, match[1]})
			}
			if parityCompareCall.MatchString(line) {
				compareCalls++
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	// Floor: if the walk stops reaching Go files this test would report a
	// premise it never examined.
	if scanned < 50 {
		t.Fatalf("scanned only %d non-test Go file(s); the walk is not reaching the tree", scanned)
	}

	if len(writers) != 1 {
		t.Fatalf("expected exactly 1 non-test writer of AttemptResult, found %d: %+v\n"+
			"\tTask 112 rests on there being one, and on it being a literal. More than one spreads "+
			"the vacuity; none means the field lost its writer. Either way the comment on "+
			"managementcontract.SurfaceContract.AttemptResult now describes something that is not "+
			"the case.", len(writers), writers)
	}
	if got := writers[0].value; got != "paritydiff.Pass" {
		t.Errorf("%s:%d assigns AttemptResult = %s, not the literal paritydiff.Pass.\n"+
			"\tIf this is now a MEASURED verdict, that is the outcome task 112 asked for -- and the "+
			"two gates in internal/managementcontract stop being vacuous, so the comment telling "+
			"readers they compare a constant to itself has to go, and attempt_result should be "+
			"persisted so the gate has something to read.",
			writers[0].file, writers[0].line, got)
	}
	if compareCalls != 0 {
		t.Errorf("paritydiff.Compare now has %d non-test caller(s); it had none, which is why "+
			"AttemptResult had nothing to be filled from. Revisit task 112: the producing half may "+
			"exist now.", compareCalls)
	}

	t.Logf("premise intact: 1 writer (%s:%d = %s), 0 non-test paritydiff.Compare callers, "+
		"across %d non-test Go files",
		writers[0].file, writers[0].line, writers[0].value, scanned)
}
