package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/ubiquiti-community/terraform-provider-unifi/internal/catalogparity"
)

// TestEveryEvidenceGeneratorIsGuarded replaces tree-state-coverage_test.sh, 396
// lines of shell deriving a population by grep and then grepping each member for
// a call site.
//
// THE RULE IT ENFORCES IS UNCHANGED, because the rule is the value: anything that
// writes an evidence artifact must first establish which tree it is describing.
// An artifact generated from a dirty working tree pins content that is in no
// commit, and it heals silently once the files land, so the window where it was
// wrong leaves no trace.
//
// WHAT CHANGED IS WHERE THE GUARD LIVES. It used to be a bash function the
// generator sourced, so "guarded" meant "greps for evidence_tree_state". It is
// now cmd/tree-state, so shell generators invoke it and Go commands are HANDED
// the answer through -tree-state. That distinction is load-bearing and predates
// this rewrite: a Go binary MEASURING the tree would be a second implementation
// of the rule that can disagree with the first, while one being handed the answer
// is the same measurement carried across a boundary.
//
// The shell version encoded that as two greps forbidding any Go file from
// reading EVIDENCE_ALLOW_DIRTY_TREE or running git status. Those greps were
// correct when the measurement lived in bash and become exactly wrong when it
// moves to Go, so they are not carried over -- the single-home property is now
// held by there being one package that measures, which the compiler enforces
// better than a grep can.
func TestEveryEvidenceGeneratorIsGuarded(t *testing.T) {
	scripts, err := filepath.Glob(filepath.Join(".woodpecker", "scripts", "*.sh"))
	if err != nil {
		t.Fatal(err)
	}
	// Without this the whole test passes by finding nothing to check, which is
	// the shape it exists to detect.
	if len(scripts) < 10 {
		t.Fatalf("found %d shell script(s); the walk is not reaching the tree", len(scripts))
	}

	writesEvidence := regexp.MustCompile(`build/[a-z0-9-]+/[a-z0-9-]+\.json|\$\{?[A-Z0-9_]*OUTPUT[A-Z0-9_]*`)

	var unguarded []string
	population := 0
	for _, path := range scripts {
		name := filepath.Base(path)
		if strings.HasSuffix(name, "_test.sh") {
			continue
		}
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		code := withoutShellComments(string(body))
		if !writesEvidence.MatchString(code) {
			continue
		}
		population++
		if strings.Contains(code, "cmd/tree-state") {
			continue
		}
		if reason, exempt := unguardedGenerators[name]; exempt {
			t.Logf("exempt: %-34s %s", name, reason)
			continue
		}
		unguarded = append(unguarded, name)
	}

	// The same floor in the other direction: an exemption for something that is
	// no longer in the population, or has since been guarded, understates what
	// is unprotected and reads as a live excuse.
	var stale []string
	for name := range unguardedGenerators {
		path := filepath.Join(".woodpecker", "scripts", name)
		body, err := os.ReadFile(path)
		if err != nil {
			stale = append(stale, name+" (no such script)")
			continue
		}
		if strings.Contains(withoutShellComments(string(body)), "cmd/tree-state") {
			stale = append(stale, name+" (now guarded)")
		}
	}
	sort.Strings(unguarded)
	sort.Strings(stale)

	if len(unguarded) > 0 {
		t.Errorf("%d evidence generator(s) write an artifact without establishing which tree it describes:\n    %s\n\n"+
			"    Add `evidence_tree_json=$(cd \"${repository_root}\" && go run ./cmd/tree-state -what \"...\") || exit 1`\n"+
			"    before anything the artifact depends on, or add the script to unguardedGenerators\n"+
			"    with the reason it cannot be guarded yet -- so the next reader sees a list of what\n"+
			"    is unprotected rather than assuming this one is complete.",
			len(unguarded), strings.Join(unguarded, "\n    "))
	}
	if len(stale) > 0 {
		t.Errorf("%d exemption(s) no longer describe anything:\n    %s\n\n"+
			"    Remove them. An exemption that has been resolved understates what is unguarded,\n"+
			"    and the next reader cannot tell a stale excuse from a live one.",
			len(stale), strings.Join(stale, "\n    "))
	}
	t.Logf("%d evidence-writing script(s) examined; %d exempt", population, len(unguardedGenerators))
}

// unguardedGenerators is a LEDGER, not a blessing: scripts that write evidence
// and do not establish their tree.
//
// Every entry is the same reason, and it is an honest one. These three cannot be
// executed to verify, because the pipeline that exercises them is down. A call
// site never seen to run is the shape of change that looks like coverage and is
// not, so they are left undone and written down rather than done blind.
var unguardedGenerators = map[string]string{
	"m1-dns-compiler.sh":          "cannot be executed to verify: the pipeline that exercises it is down",
	"m0-uos-dns-qualification.sh": "cannot be executed to verify: the pipeline that exercises it is down",
	"m3-dns-qualification.sh":     "cannot be executed to verify: the pipeline that exercises it is down",
}

// withoutShellComments removes whole-line comments so a script that merely
// mentions a build/ path in prose does not join the population, and so a
// commented-out guard does not count as one.
//
// Whole-line only. A `#` after code can sit inside a quoted jq program, and
// trimming from the first one would cut into exactly the literals these scripts
// are built from -- which is the mistake that cost an hour on the receipt-field
// scanner earlier.
func withoutShellComments(body string) string {
	var kept []string
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		kept = append(kept, line)
	}
	return strings.Join(kept, "\n")
}

// TestEveryGuardedGeneratorRefusesADirtyTree replaces tree-state-guard-fires_test.sh
// and is the half that makes the coverage check above mean anything.
//
// THE COVERAGE CHECK IS STATIC. It reports a script as guarded because the text
// "cmd/tree-state" appears in it, which is a claim about a string. This one
// dirties the tree on purpose and runs each guarded script, requiring it to
// refuse and to say why. Without it, "guarded" is exactly the kind of assertion
// this whole family exists to catch: true of the source and unverified of the
// behaviour.
//
// EVERY SCRIPT MUST FAIL EARLY, which is what makes this affordable. The guard
// runs before anything it protects -- before any build, any network, any
// controller -- so a refusal costs a process start and nothing else. A script
// that got as far as building a provider before refusing would show up here as a
// slow test, which is its own signal.
func TestEveryGuardedGeneratorRefusesADirtyTree(t *testing.T) {
	if testing.Short() {
		t.Skip("runs every guarded generator; skipped under -short")
	}
	const root = "."

	probe := filepath.Join(root, ".tree-state-guard-probe")
	if err := os.WriteFile(probe, []byte("making the tree dirty on purpose\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(probe) })

	// Without this every case below would pass against a clean tree, proving
	// nothing -- the same floor the shell version carried, for the same reason.
	state, err := catalogparity.MeasureTreeState(root, "the guard-fires probe", true)
	if err != nil {
		t.Fatal(err)
	}
	if state.Status != "dirty" {
		t.Fatal("the probe did not make the tree dirty, so every case below would prove nothing")
	}

	guarded := guardedGenerators(t)
	if len(guarded) == 0 {
		t.Fatal("no guarded generator was derived, so this suite asserts nothing")
	}

	for _, name := range guarded {
		t.Run(name, func(t *testing.T) {
			command := exec.Command("bash", filepath.Join(".woodpecker", "scripts", name))
			command.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + os.Getenv("HOME")}
			output, err := command.CombinedOutput()
			if err == nil {
				t.Fatalf("%s ran to completion on a dirty tree; its guard did not stop it", name)
			}
			if !strings.Contains(string(output), "refusing to generate evidence from a dirty tree") {
				first := strings.SplitN(strings.TrimSpace(string(output)), "\n", 2)[0]
				t.Fatalf("%s failed on a dirty tree but not because of the guard; it said: %s", name, first)
			}
		})
	}
}

func guardedGenerators(t *testing.T) []string {
	t.Helper()
	scripts, err := filepath.Glob(filepath.Join(".woodpecker", "scripts", "*.sh"))
	if err != nil {
		t.Fatal(err)
	}
	var guarded []string
	for _, path := range scripts {
		name := filepath.Base(path)
		if strings.HasSuffix(name, "_test.sh") {
			continue
		}
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(withoutShellComments(string(body)), "cmd/tree-state") {
			guarded = append(guarded, name)
		}
	}
	sort.Strings(guarded)
	return guarded
}
