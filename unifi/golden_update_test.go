package unifi

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
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

// writeGolden rewrites a golden inventory, refusing to drop an entry unless
// the caller sets a second variable. Rewriting is dangerous: the golden IS
// the record of what the provider used to do, so a regenerate-until-green
// erases the evidence of a regression along with it. Adding lines stays a
// one-variable operation since every migration does that; removing one needs
// the second variable to say so explicitly.
// goldenTB is the slice of testing.TB writeGolden needs -- an interface, not
// *testing.T, so the refusal path can be tested (t.Fatal would end the
// goroutine on a real T).
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

	if len(removed) > 0 && !goldenRemovalAuthorised() {
		t.Fatal(goldenRemovalRefusal(path, removed))
		// Unreachable on a real *testing.T (Fatal ends the goroutine); kept
		// because refusing and writing anyway would erase the evidence
		// regardless.
		return
	}

	body := header + strings.Join(got, "\n") + "\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(fmt.Sprintf("writing %s: %v", path, err))
	}
	t.Logf("wrote %d entries to %s", len(got), path)
}

// goldenRemovalAuthorised reports whether the caller has actually said yes to
// dropping entries. Fails closed: anything ParseBool can't read as true
// (including a plain "yes") refuses, since an unreadable answer isn't
// consent.
func goldenRemovalAuthorised() bool {
	authorised, err := strconv.ParseBool(os.Getenv(allowGoldenRemovalEnv))
	return err == nil && authorised
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

// Test_goldenUpdateAllowsAnAddition keeps additions a one-variable operation:
// if a normal landing needed the second variable too, people would set both
// by habit and the guard would protect nothing.
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

	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading golden: %v", err)
	}
	if !strings.Contains(string(body), "beta") {
		t.Errorf("the entry was dropped despite the refusal:\n%s", body)
	}
}

// Test_goldenUpdateRemovalNamesTheEntries keeps the refusal useful: "set
// another variable" alone tells the reader nothing about what they're
// agreeing to.
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

// Test_goldenUpdateAllowsARemovalWhenSaidSo proves the escape hatch works: a
// deliberate behaviour change is blocked only until it says its name.
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

// Test_goldenUpdateRefusesARemovalWhenTheAnswerIsNo covers the values that
// mean no: "0" and "false" must refuse, and "yes" -- a plain-language yes
// ParseBool can't read -- must refuse too, since an answer the guard can't
// interpret isn't consent.
func Test_goldenUpdateRefusesARemovalWhenTheAnswerIsNo(t *testing.T) {
	for _, value := range []string{"0", "false", "FALSE", "no", "off", "yes", " "} {
		t.Run(value, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "golden.txt")
			if err := os.WriteFile(path, []byte("# head\nalpha\nbeta\n"), 0o644); err != nil {
				t.Fatalf("seeding golden: %v", err)
			}
			t.Setenv(allowGoldenRemovalEnv, value)

			rec := &recordingTB{}
			writeGolden(rec, path, "# head\n", []string{"alpha"})

			if !rec.fired {
				t.Errorf("%s=%q authorised dropping an entry; only a value that reads as true "+
					"may do that, and this one does not", allowGoldenRemovalEnv, value)
			}
			body, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("reading golden: %v", err)
			}
			if !strings.Contains(string(body), "beta") {
				t.Errorf("%s=%q erased the entry:\n%s", allowGoldenRemovalEnv, value, body)
			}
		})
	}
}

// Test_goldenUpdateAcceptsTheDocumentedSpellings pairs with the refusals
// above: without it, the guard could be tightened to refuse everything and
// still look correct.
func Test_goldenUpdateAcceptsTheDocumentedSpellings(t *testing.T) {
	for _, value := range []string{"1", "t", "true", "TRUE", "True"} {
		t.Run(value, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "golden.txt")
			if err := os.WriteFile(path, []byte("# head\nalpha\nbeta\n"), 0o644); err != nil {
				t.Fatalf("seeding golden: %v", err)
			}
			t.Setenv(allowGoldenRemovalEnv, value)

			rec := &recordingTB{}
			writeGolden(rec, path, "# head\n", []string{"alpha"})

			if rec.fired {
				t.Errorf("%s=%q was refused, but it is one of the spellings the refusal "+
					"message tells the reader to use:\n%s", allowGoldenRemovalEnv, value, rec.fatal)
			}
		})
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
