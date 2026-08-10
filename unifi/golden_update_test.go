package unifi

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The two environment variables that drive a golden rewrite.
//
// They are deliberately asymmetric. See writeGolden.
const (
	updateGoldenEnv       = "UPDATE_GOLDEN"
	allowGoldenRemovalEnv = "UPDATE_GOLDEN_ALLOW_REMOVAL"
)

// writeGolden rewrites a golden inventory, refusing to drop an entry unless the
// caller says so in a second variable.
//
// The golden-update path is the most dangerous thing in this package, because
// it is the one that turns a real failure into a commit. A test goes red, the
// documented fix is to regenerate, the suite goes green, and the regression
// ships with a clean run behind it. Nothing downstream can tell that apart from
// a legitimate update: the golden IS the record of what the provider used to do,
// so once it is rewritten the evidence of the regression is gone.
//
// The guard is an asymmetry rather than a lock, because the two directions are
// not the same risk:
//
//   - ADDING lines is the ordinary case. Every migrated surface adds behaviour
//     lines, and there are dozens of surfaces left. That must stay a one-variable
//     operation or the guard becomes something people work around.
//
//   - REMOVING lines is the regression direction. A dropped validator, plan
//     modifier or default is exactly what a migration loses and exactly what a
//     regenerate-until-green produces. firewall_policy lost nine validators that
//     way with the whole suite green.
//
// So a removal has to say its name. It is sometimes legitimate -- a deliberate
// behaviour change -- and then naming it costs one word and leaves the reason in
// the shell history of the command that produced the commit, which is more than
// a bare regeneration leaves.
//
// The refusal prints the entries it would have dropped, because "run it again
// with another variable" is not useful on its own: the entries are the thing the
// reader has to decide about.
// goldenTB is the slice of testing.TB writeGolden needs. Taking an interface
// rather than *testing.T is what lets the guard be tested at all: t.Fatal ends
// the goroutine, so a refusal driven through a real *testing.T fails the test
// observing it no matter how the observation is arranged.
type goldenTB interface {
	Helper()
	Fatal(args ...any)
	Logf(format string, args ...any)
}

func writeGolden(t goldenTB, path, header string, got []string) {
	t.Helper()

	// A missing golden is the first run, not a removal of everything.
	var removed []string
	if existing, err := os.ReadFile(path); err == nil {
		_, removed = diffSorted(splitNonEmpty(string(existing)), got)
	} else if !os.IsNotExist(err) {
		t.Fatal(fmt.Sprintf("reading %s: %v", path, err))
	}

	if len(removed) > 0 && os.Getenv(allowGoldenRemovalEnv) == "" {
		t.Fatal(goldenRemovalRefusal(path, removed))
		// Fatal on a real *testing.T ends the goroutine, so this return is
		// unreachable there. It is here because the guard must not depend on
		// that: refusing and then writing anyway would erase the evidence
		// regardless, which is the entire thing being prevented.
		return
	}

	body := header + strings.Join(got, "\n") + "\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(fmt.Sprintf("writing %s: %v", path, err))
	}
	t.Logf("wrote %d entries to %s", len(got), path)
}

// goldenRemovalRefusal is the message, separated from the refusal so a test can
// read it without provoking a failure.
func goldenRemovalRefusal(path string, removed []string) string {
	return fmt.Sprintf(
		"refusing to rewrite %s: it would drop %d entr%s the provider used to apply:\n    %s\n\n"+
			"    Each of these ran before this change and would not run after it, and\n"+
			"    rewriting the golden is what would erase the evidence of that.\n"+
			"    If the removal is intended, it is a behaviour change: say so with\n"+
			"    %s=1 alongside %s=1, and land it on its own rather than inside a\n"+
			"    migration.\n"+
			"    If it is not intended, the golden is right and the provider is wrong.",
		path, len(removed), plural(len(removed)), strings.Join(removed, "\n    "),
		allowGoldenRemovalEnv, updateGoldenEnv,
	)
}

func plural(n int) string {
	if n == 1 {
		return "y"
	}
	return "ies"
}

// Test_goldenUpdateAllowsAnAddition is the ordinary case and has to stay
// ordinary. Every migrated surface adds behaviour lines, and there are dozens
// left; if a normal landing needed the second variable, people would set both
// out of habit and the guard would protect nothing.
func Test_goldenUpdateAllowsAnAddition(t *testing.T) {
	path := filepath.Join(t.TempDir(), "golden.txt")
	if err := os.WriteFile(path, []byte("# head\nalpha\n"), 0o644); err != nil {
		t.Fatalf("seeding golden: %v", err)
	}

	writeGolden(t, path, "# head\n", []string{"alpha", "beta"})

	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading golden: %v", err)
	}
	if !strings.Contains(string(body), "beta") {
		t.Errorf("the addition was not written:\n%s", body)
	}
}

// recordingTB captures a refusal instead of ending the test that provoked it.
type recordingTB struct {
	fatal string
	fired bool
}

func (r *recordingTB) Helper()             {}
func (r *recordingTB) Logf(string, ...any) {}
func (r *recordingTB) Fatal(args ...any)   { r.fired = true; r.fatal = fmt.Sprint(args...) }

// Test_goldenUpdateRefusesARemoval is the guard itself: a rewrite that would
// drop an entry must not happen silently.
func Test_goldenUpdateRefusesARemoval(t *testing.T) {
	path := filepath.Join(t.TempDir(), "golden.txt")
	if err := os.WriteFile(path, []byte("# head\nalpha\nbeta\n"), 0o644); err != nil {
		t.Fatalf("seeding golden: %v", err)
	}

	rec := &recordingTB{}
	writeGolden(rec, path, "# head\n", []string{"alpha"})

	if !rec.fired {
		t.Fatal("a rewrite that drops an entry was accepted; the golden-update path " +
			"can still turn a behaviour regression into a green commit")
	}

	// Refusing but writing anyway would erase the evidence regardless, which is
	// the whole thing being prevented.
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading golden: %v", err)
	}
	if !strings.Contains(string(body), "beta") {
		t.Errorf("the entry was dropped despite the refusal:\n%s", body)
	}
}

// Test_goldenUpdateRemovalNamesTheEntries keeps the refusal useful. "Set another
// variable" on its own tells the reader nothing about what they are agreeing to;
// the entries are the thing that has to be decided about, so they belong in the
// message rather than in a file the reader is left to diff.
func Test_goldenUpdateRemovalNamesTheEntries(t *testing.T) {
	message := goldenRemovalRefusal("testdata/x.txt", []string{"unifi_thing.x validator"})

	for _, want := range []string{
		"unifi_thing.x validator", // the entry itself
		"testdata/x.txt",          // which golden
		allowGoldenRemovalEnv,     // how to proceed deliberately
	} {
		if !strings.Contains(message, want) {
			t.Errorf("the refusal does not mention %q:\n%s", want, message)
		}
	}
}

// Test_goldenUpdateAllowsARemovalWhenSaidSo proves the escape hatch works, so a
// deliberate behaviour change is not blocked outright -- only made to say its
// name.
func Test_goldenUpdateAllowsARemovalWhenSaidSo(t *testing.T) {
	path := filepath.Join(t.TempDir(), "golden.txt")
	if err := os.WriteFile(path, []byte("# head\nalpha\nbeta\n"), 0o644); err != nil {
		t.Fatalf("seeding golden: %v", err)
	}
	t.Setenv(allowGoldenRemovalEnv, "1")

	writeGolden(t, path, "# head\n", []string{"alpha"})

	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading golden: %v", err)
	}
	if strings.Contains(string(body), "beta") {
		t.Errorf("the declared removal was not applied:\n%s", body)
	}
}

// Test_goldenUpdateWritesAMissingGolden separates a first run from a removal of
// everything. Without this, creating a new golden would need the removal flag,
// which would teach people to set it by default.
func Test_goldenUpdateWritesAMissingGolden(t *testing.T) {
	path := filepath.Join(t.TempDir(), "absent.txt")
	writeGolden(t, path, "# head\n", []string{"alpha"})
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("a fresh golden was not created: %v", err)
	}
}
