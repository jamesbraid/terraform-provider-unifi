package upgradeplan

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestEveryCombinationOfTheThreeNumbers walks the whole cross-product of what
// the control and the subject can report against both fixture polarities.
//
// The shell expressed this as a five-branch if/elif chain that nothing could
// call, in a script that needs a live controller to reach -- so which branch
// fired for which pair of exit codes had never been checked by anything.
func TestEveryCombinationOfTheThreeNumbers(t *testing.T) {
	for _, c := range []struct {
		name     string
		expected Expectation
		control  int
		subject  int
		want     string
		exit     int
		says     string
	}{
		// An upgrade fixture: the released provider settles it, so a red
		// subject is the candidate regressing.
		{"upgrade fixture, both clean", 0, 0, 0, "pass", 0, "plans clean under"},
		{"upgrade fixture, candidate errors", 0, 0, 1, "fail", 1, "could not plan state written by"},
		{"upgrade fixture, candidate wants changes", 0, 0, 2, "fail", 1, "upgrade regression"},
		{"upgrade fixture that does not settle", 0, 2, 0, "void", 2, "fix the fixture, do not report a regression"},
		{"upgrade fixture whose control errors", 0, 1, 0, "void", 2, "could not plan its own state"},

		// A regression fixture: the released provider must NOT settle it, so a
		// clean subject is the fix working.
		{"regression fixture, fix holds", 2, 2, 0, "pass", 0, "the fix it was written for still holds"},
		{"regression fixture, candidate still broken", 2, 2, 2, "fail", 1, "upgrade regression"},
		{"regression fixture that stopped demonstrating", 2, 0, 0, "void", 2, "did not (control 0, expected 2)"},
		{"regression fixture whose control errors", 2, 1, 0, "void", 2, "could not plan its own state"},
	} {
		t.Run(c.name, func(t *testing.T) {
			got := Decide("v0.101.2", c.expected, c.control, c.subject)
			if got.Result != c.want {
				t.Fatalf("result = %q, want %q (verdict: %s)", got.Result, c.want, got.Verdict)
			}
			if got.ExitCode != c.exit {
				t.Fatalf("exit code = %d, want %d", got.ExitCode, c.exit)
			}
			if !strings.Contains(got.Verdict, c.says) {
				t.Fatalf("verdict %q does not say %q", got.Verdict, c.says)
			}
		})
	}
}

// TestTheTwoRedsAreDistinguishable. void says the measurement is meaningless;
// fail says the candidate broke something. They have opposite remedies -- fix
// the fixture, or fix the provider -- and one message for both would hand the
// reader a coin flip.
func TestTheTwoRedsAreDistinguishable(t *testing.T) {
	void := Decide("v0.101.2", 0, 2, 0)
	fail := Decide("v0.101.2", 0, 0, 2)

	if void.Result == fail.Result {
		t.Fatal("a void measurement and a candidate regression share a result")
	}
	if void.ExitCode == fail.ExitCode {
		t.Fatalf("both reds exit %d, so a caller cannot tell them apart without parsing prose",
			void.ExitCode)
	}
	if void.Verdict == fail.Verdict {
		t.Fatal("both reds carry the same sentence")
	}
	// The void message must point at the FIXTURE and the fail message at the
	// CANDIDATE, or the reader goes to the wrong place.
	if !strings.Contains(void.Verdict, "fixture") {
		t.Fatalf("the void verdict %q does not name the fixture", void.Verdict)
	}
	if !strings.Contains(fail.Verdict, "candidate") {
		t.Fatalf("the fail verdict %q does not name the candidate", fail.Verdict)
	}
}

// TestThePassVerdictSaysWhichQuestionWasAnswered is a deliberate divergence.
// The shell printed one sentence for both polarities, so a green log could not
// say whether it had shown an upgrade safe or a fix holding -- and those are
// the two reasons anyone runs this.
func TestThePassVerdictSaysWhichQuestionWasAnswered(t *testing.T) {
	upgrade := Decide("v0.101.2", 0, 0, 0)
	regression := Decide("v0.101.2", 2, 2, 0)
	if upgrade.Verdict == regression.Verdict {
		t.Fatal("both fixture kinds pass with the same sentence, so a green run cannot say " +
			"which question it answered")
	}
}

// TestBothCommittedFixturesDeclareTheirPolarity. An undeclared fixture reports
// a fix as proven by a configuration that never showed the defect.
func TestBothCommittedFixturesDeclareTheirPolarity(t *testing.T) {
	seen := map[Expectation]string{}
	for _, name := range []string{"upgrade", "upgrade-regression"} {
		directory := filepath.Join("..", "..", ".woodpecker", "fixtures", name)
		fixture, err := LoadFixture(directory)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if fixture.Files == 0 {
			t.Fatalf("%s declares no .tf files", name)
		}
		seen[fixture.Expected] = name
	}
	// The two fixtures must have OPPOSITE polarities, or one of them is not
	// asking the question its name says it asks.
	if len(seen) != 2 {
		t.Fatalf("both committed fixtures declare the same expectation (%v); one of them is not "+
			"asking the question its name implies", seen)
	}
}

func TestLoadFixtureRefusesWhatCannotJudge(t *testing.T) {
	for _, c := range []struct {
		name    string
		wants   string
		prepare func(t *testing.T, directory string)
	}{
		{"no EXPECT_OLD_PLAN", "does not declare EXPECT_OLD_PLAN", func(t *testing.T, d string) {
			write(t, filepath.Join(d, "main.tf"), "resource \"x\" \"y\" {}\n")
		}},
		{"no .tf files", "contains no .tf files", func(t *testing.T, d string) {
			write(t, filepath.Join(d, "EXPECT_OLD_PLAN"), "0\n")
		}},
		{"an expectation that is neither 0 nor 2", "must be 0 or 2", func(t *testing.T, d string) {
			write(t, filepath.Join(d, "main.tf"), "resource \"x\" \"y\" {}\n")
			write(t, filepath.Join(d, "EXPECT_OLD_PLAN"), "1\n")
		}},
		{"an expectation that is not a number", "must be 0 or 2", func(t *testing.T, d string) {
			write(t, filepath.Join(d, "main.tf"), "resource \"x\" \"y\" {}\n")
			write(t, filepath.Join(d, "EXPECT_OLD_PLAN"), "maybe\n")
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			directory := t.TempDir()
			c.prepare(t, directory)
			_, err := LoadFixture(directory)
			if err == nil {
				t.Fatalf("accepted; expected a refusal mentioning %q", c.wants)
			}
			if !strings.Contains(err.Error(), c.wants) {
				t.Fatalf("refused with %q, which does not mention %q", err, c.wants)
			}
		})
	}

	// The control: a well-formed fixture is accepted, and its declaration is
	// read rather than defaulted.
	directory := t.TempDir()
	write(t, filepath.Join(directory, "main.tf"), "resource \"x\" \"y\" {}\n")
	write(t, filepath.Join(directory, "other.tf"), "# also counted\n")
	write(t, filepath.Join(directory, "EXPECT_OLD_PLAN"), " 2 \n")
	fixture, err := LoadFixture(directory)
	if err != nil {
		t.Fatalf("a well-formed fixture was refused: %v", err)
	}
	if fixture.Expected != PlanChanges {
		t.Fatalf("expectation = %d, want 2", fixture.Expected)
	}
	if fixture.Files != 2 {
		t.Fatalf("counted %d .tf file(s), want 2", fixture.Files)
	}
}

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}
