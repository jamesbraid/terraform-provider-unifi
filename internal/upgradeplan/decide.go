// Package upgradeplan answers the one question the acceptance suite cannot
// express: does a configuration APPLIED BY THE PREVIOUS RELEASE still plan
// clean under this one?
//
// The acceptance harness compiles the provider into the test binary through
// ProtoV6ProviderFactories, so a run holds exactly one provider and can only
// ask "does this build create and then re-plan cleanly". That is a strictly
// easier question and it is not the one an operator faces on upgrade.
//
// THREE PLANS, AND THE MIDDLE ONE IS WHY THE RESULT MEANS ANYTHING:
//
//	apply with OLD                   the state must be written by the old binary
//	plan with OLD on that state      CONTROL   must match the fixture's declaration
//	plan with NEW on that same state SUBJECT   must report no changes
//
// Without the control a red subject is unreadable: a fixture that never settles
// under ANY build looks exactly like a regression the new build introduced.
// That is not theoretical -- plain networks settle by themselves, which is why
// the fixture that reproduces is one attached to a firewall zone, chosen
// because it does not settle easily. A green control is what turns a red
// subject into a finding.
package upgradeplan

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Plan exit codes, as `-detailed-exitcode` defines them. They are read straight
// from the process, NEVER through a pipe: with -detailed-exitcode the exit
// status IS the result, so piping to tee or head reports tee's status and a
// harness reads 0 from a plan that never ran.
const (
	PlanNoChanges = 0
	PlanError     = 1
	PlanChanges   = 2
)

// Expectation is what the fixture declares the RELEASED provider should do with
// it, and it has no default.
//
// Two different questions run through the same machinery and the control's
// polarity is what tells them apart:
//
//	0  is the upgrade safe -- the released provider settles the fixture, so a
//	   red subject is the candidate regressing
//	2  does a fix work -- the released provider demonstrably does NOT settle it,
//	   so a green subject is the fix working
//
// A fixture whose expectation is 0 when it should be 2 reports a fix as proven
// by a fixture that never showed the defect. Defaulting either way rebuilds the
// check that cannot fail one layer up, so an undeclared fixture is a hard error.
type Expectation int

// Verdict is the run's outcome and the sentence that explains it.
type Verdict struct {
	Result  string
	Verdict string
	// ExitCode lets the caller distinguish the two reds. void is not fail: one
	// says the measurement is meaningless, the other says the candidate broke
	// something, and they are acted on by different people.
	ExitCode int
}

// Decide reads the three numbers the run produced.
//
// THE TWO REDS CARRY DIFFERENT MESSAGES ON PURPOSE. One message for both would
// hand the reader a coin flip between "the fixture is wrong" and "the candidate
// regressed", and those have opposite remedies.
func Decide(releasedRef string, expected Expectation, controlExit, subjectExit int) Verdict {
	switch {
	case controlExit == PlanError:
		// More specific than the mismatch below, which would also catch this:
		// a control that ERRORED did not merely disagree, it never produced a
		// comparable answer at all.
		return Verdict{"void",
			"the released provider could not plan its own state (exit 1); the subject result is meaningless",
			2}

	case controlExit != int(expected) && expected == 0:
		return Verdict{"void",
			"the fixture does not settle under the released provider, so it cannot judge the " +
				"candidate; fix the fixture, do not report a regression",
			2}

	case controlExit != int(expected):
		return Verdict{"void",
			fmt.Sprintf("the fixture was declared to demonstrate a defect under the released "+
				"provider and did not (control %d, expected %d); a clean subject would prove nothing",
				controlExit, expected),
			2}

	case subjectExit == PlanError:
		return Verdict{"fail",
			fmt.Sprintf("the candidate provider could not plan state written by %s (exit 1)", releasedRef),
			1}

	case subjectExit != PlanNoChanges:
		return Verdict{"fail",
			fmt.Sprintf("the candidate provider does not settle on state written by %s; "+
				"this is an upgrade regression", releasedRef),
			1}
	}

	// The pass verdict NAMES WHICH QUESTION WAS ANSWERED. The shell printed one
	// sentence for both, so a green log could not say whether it had shown an
	// upgrade safe or a fix holding -- and those are the two reasons anyone
	// runs this.
	if expected == PlanChanges {
		return Verdict{"pass",
			fmt.Sprintf("the released provider does not settle this fixture and the candidate "+
				"does, so the fix it was written for still holds under %s state", releasedRef),
			0}
	}
	return Verdict{"pass",
		fmt.Sprintf("a configuration applied by %s plans clean under the candidate", releasedRef),
		0}
}

// Fixture is the configuration this run measures.
type Fixture struct {
	Directory string
	Files     int
	Expected  Expectation
}

// LoadFixture reads a fixture directory and refuses one that cannot judge
// anything.
func LoadFixture(directory string) (Fixture, error) {
	fixture := Fixture{Directory: directory}
	info, err := os.Stat(directory)
	if err != nil {
		return fixture, fmt.Errorf("fixture directory: %w", err)
	}
	if !info.IsDir() {
		return fixture, fmt.Errorf("fixture %s is not a directory", directory)
	}

	entries, err := os.ReadDir(directory)
	if err != nil {
		return fixture, err
	}
	for _, entry := range entries {
		if !entry.IsDir() && filepath.Ext(entry.Name()) == ".tf" {
			fixture.Files++
		}
	}
	if fixture.Files == 0 {
		return fixture, fmt.Errorf("fixture directory %s contains no .tf files, so the apply "+
			"below would measure nothing", directory)
	}

	path := filepath.Join(directory, "EXPECT_OLD_PLAN")
	raw, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return fixture, fmt.Errorf("%s does not declare EXPECT_OLD_PLAN. A fixture that does not "+
			"say whether the released provider should settle it cannot judge anything; write 0 "+
			"(upgrade fixture) or 2 (regression fixture)", directory)
	}
	declared, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil {
		return fixture, fmt.Errorf("%s says %q; it must be 0 or 2", path, strings.TrimSpace(string(raw)))
	}
	if declared != PlanNoChanges && declared != PlanChanges {
		return fixture, fmt.Errorf("%s says %d; it must be 0 or 2", path, declared)
	}
	fixture.Expected = Expectation(declared)
	return fixture, nil
}
