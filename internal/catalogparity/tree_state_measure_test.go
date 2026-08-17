package catalogparity

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestMeasureTreeState replaces tree-state_test.sh and tree-state-guard-fires_test.sh
// -- 257 lines of shell testing shell -- with table tests against real
// repositories built in t.TempDir().
//
// THE SHELL VERSION COULD NOT DO THIS. It had to exercise the guard by invoking
// the seven production scripts and watching them refuse, because the guard read
// its repository root from BASH_SOURCE and there was no way to hand it a
// different tree. That is why the coverage test grew to 396 lines: it was
// reasoning about call sites by grep because it could not call the thing.
//
// Here the root is a parameter, so each case is a two-line repository.
func TestMeasureTreeState(t *testing.T) {
	t.Run("clean tree records the commit", func(t *testing.T) {
		dir := newRepo(t)
		state, err := MeasureTreeState(dir, "the test artifact", false)
		if err != nil {
			t.Fatalf("MeasureTreeState() error = %v, want a clean reading", err)
		}
		if state.Status != "clean" {
			t.Errorf("status = %q, want clean", state.Status)
		}
		if len(state.Commit) != 40 {
			t.Errorf("commit = %q, want a 40-character SHA", state.Commit)
		}
		if len(state.DirtyPaths) != 0 {
			t.Errorf("dirty_paths = %v, want empty", state.DirtyPaths)
		}
	})

	// THE CASE THE ARTIFACT FAILURE WAS. Untracked, not modified: the inventory
	// digested evidence files that had not been added yet. A guard that only
	// noticed modifications would have watched that happen.
	t.Run("an untracked file is dirty", func(t *testing.T) {
		dir := newRepo(t)
		writeFile(t, dir, "untracked.json", "{}")
		if _, err := MeasureTreeState(dir, "the test artifact", false); err == nil {
			t.Fatal("MeasureTreeState() succeeded on an untracked file; the inventory failure was exactly this")
		} else if !strings.Contains(err.Error(), "untracked.json") {
			t.Errorf("error does not name the file:\n%v", err)
		}
	})

	t.Run("a modified file is dirty", func(t *testing.T) {
		dir := newRepo(t)
		writeFile(t, dir, "seed.txt", "changed")
		if _, err := MeasureTreeState(dir, "the test artifact", false); err == nil {
			t.Fatal("MeasureTreeState() succeeded on a modified file")
		}
	})

	// REFUSES RATHER THAN WARNS. This is the property the rewrite had to
	// preserve: the guard returns an error, so a caller that ignores it has to
	// ignore it deliberately rather than by not reading stderr.
	t.Run("refusal names the artifact and the remedy", func(t *testing.T) {
		dir := newRepo(t)
		writeFile(t, dir, "untracked.json", "{}")
		_, err := MeasureTreeState(dir, "the M1 compiler receipt", false)
		if err == nil {
			t.Fatal("MeasureTreeState() did not refuse")
		}
		for _, want := range []string{
			"the M1 compiler receipt",
			"refusing to generate evidence from a dirty tree",
			"EVIDENCE_ALLOW_DIRTY_TREE",
			"in no commit",
		} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("refusal omits %q:\n%v", want, err)
			}
		}
	})

	// THE THIRD OUTCOME, and the reason the guard is not simply a block.
	t.Run("acknowledged dirt is recorded, not hidden", func(t *testing.T) {
		dir := newRepo(t)
		writeFile(t, dir, "untracked.json", "{}")
		state, err := MeasureTreeState(dir, "the test artifact", true)
		if err != nil {
			t.Fatalf("MeasureTreeState() error = %v, want a dirty reading", err)
		}
		if state.Status != "dirty" {
			t.Errorf("status = %q, want dirty", state.Status)
		}
		if len(state.DirtyPaths) != 1 || !strings.Contains(state.DirtyPaths[0], "untracked.json") {
			t.Errorf("dirty_paths = %v, want the untracked file", state.DirtyPaths)
		}
		if state.Commit == "" {
			t.Error("an acknowledged dirty run still has to name the commit it diverged from")
		}
	})

	t.Run("a non-repository is refused", func(t *testing.T) {
		if _, err := MeasureTreeState(t.TempDir(), "the test artifact", false); err == nil {
			t.Fatal("MeasureTreeState() succeeded outside a repository")
		}
	})

	// The shell version took the artifact name as ${1:?...}; losing that would
	// produce refusals that do not say what they are guarding.
	t.Run("the artifact name is required", func(t *testing.T) {
		if _, err := MeasureTreeState(newRepo(t), "", false); err == nil {
			t.Fatal("MeasureTreeState() accepted an empty artifact name")
		}
	})

	// The receipt is digested and compared, so two readings of one tree must be
	// byte-identical. Ordering is the only thing that could vary.
	t.Run("two readings of one tree agree", func(t *testing.T) {
		dir := newRepo(t)
		for _, name := range []string{"c.json", "a.json", "b.json"} {
			writeFile(t, dir, name, "{}")
		}
		first, err := MeasureTreeState(dir, "the test artifact", true)
		if err != nil {
			t.Fatal(err)
		}
		second, err := MeasureTreeState(dir, "the test artifact", true)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Join(first.DirtyPaths, "\n") != strings.Join(second.DirtyPaths, "\n") {
			t.Errorf("two readings disagree:\n%v\n%v", first.DirtyPaths, second.DirtyPaths)
		}
	})
}

// TestMeasuredTreeStateRoundTripsThroughItsParser closes the seam between the
// producer and the consumer, which in shell could not be tested at all: one side
// was jq and the other was Go, and nothing held both.
func TestMeasuredTreeStateRoundTripsThroughItsParser(t *testing.T) {
	dir := newRepo(t)
	writeFile(t, dir, "untracked.json", "{}")
	state, err := MeasureTreeState(dir, "the test artifact", true)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := state.JSON()
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := ParseTreeState(encoded)
	if err != nil {
		t.Fatalf("ParseTreeState(MeasureTreeState(...)) error = %v\ninput: %s", err, encoded)
	}
	if parsed.Status != state.Status || parsed.Commit != state.Commit ||
		len(parsed.DirtyPaths) != len(state.DirtyPaths) {
		t.Errorf("round trip changed the state:\n produced %+v\n parsed   %+v", state, parsed)
	}
}

func newRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init", "--quiet"},
		{"config", "user.email", "test@example.invalid"},
		{"config", "user.name", "test"},
		{"config", "commit.gpgsign", "false"},
	} {
		run(t, dir, args...)
	}
	writeFile(t, dir, "seed.txt", "seed")
	run(t, dir, "add", "seed.txt")
	run(t, dir, "commit", "--quiet", "-m", "seed")
	return dir
}

func writeFile(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func run(t *testing.T, dir string, args ...string) {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", dir}, args...)...)
	if out, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}
