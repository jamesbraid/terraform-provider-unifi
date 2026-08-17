package main

import (
	"fmt"
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
	// THE FLOOR IS THAT THE WALK WORKED, NOT THAT IT FOUND A LOT.
	//
	// It used to be `len(scripts) < 10`, which was right when the population was
	// shell and could only shrink by accident. It is wrong now: this lane exists
	// to delete those scripts, so the guard would have fired on the migration
	// SUCCEEDING -- 20 today, 11 deletions from tripping, and the target is zero.
	//
	// Lowering the number moves the same collision a few deletions later. The
	// defect is that a count cannot tell "found nothing because the walk broke"
	// from "found nothing because we finished", which is the two-causes-one-zero
	// shape this repository keeps producing -- the stub's catch-all exit 0, the
	// jq select that wrote a zero-byte file, and now this.
	//
	// So the question becomes one that stays answerable at zero scripts: did I
	// reach and read the directory. An unreadable or missing directory is a
	// broken walk; an empty one is a finished migration.
	if _, err := os.Stat(filepath.Join(".woodpecker", "scripts")); err != nil {
		t.Fatalf("the scripts directory cannot be read, so an empty population would mean "+
			"nothing: %v", err)
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

	if len(unguarded) > 0 || len(stale) > 0 {
		t.Log(classifiedTreeIdentity(t, filepath.Join(".woodpecker", "scripts")))
	}
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

// TestEveryGeneratorCallSitePassesTheTreeState is the half that survives the
// shell going away, and without it this file's coverage SHRINKS every time the
// port succeeds.
//
// The test above walks *.sh. Binaries cannot enter that population at all, so a
// script converted to a binary leaves the check rather than joining it, and the
// guard stops being verified for that artifact at the moment the port lands.
// Seven binaries already take -tree-state and nothing checked any of them.
//
// A BINARY'S GUARD LIVES AT ITS CALL SITE, not inside it. It is handed the tree
// state rather than measuring it -- deliberately, so there is one measurement
// rather than two that can disagree -- which means the thing that can be wrong
// is a workflow line that invokes the generator and omits the flag. That line
// is in .woodpecker/*.yml, so it is checkable now and stays checkable when no
// script is left.
//
// DECLARING THE FLAG IS THE POPULATION TEST, and it is the binary saying so
// itself. A generator whose author never added -tree-state is outside this
// check by construction, which is a real limit: it catches a forgotten call
// site, not a forgotten flag. The flags have no defaults and the binaries
// refuse without them, so a missed call site also fails at run time -- this
// turns that into a failure at push time, naming the line.
//
// A KNOWN FALSE-POSITIVE SHAPE THAT DOES NOT EXIST YET, written down before
// it does. A command can be BUILT and then run rather than `go run`, and the
// build line names ./cmd/x while carrying no flags -- those appear on the
// separate line that runs the binary. catalog-upgrade-plan.yml does exactly
// this for upgrade-plan-runner today and is not caught, only because that
// command measures its own tree state instead of declaring the flag. The
// first flag-taking command invoked that way will read as a missing call
// site. The fix then is to recognise the build form and check the line that
// runs the binary, not to loosen the match.
func TestEveryGeneratorCallSitePassesTheTreeState(t *testing.T) {
	workflows, err := filepath.Glob(filepath.Join(".woodpecker", "*.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(workflows) == 0 {
		t.Fatal("no workflows found, so every assertion below would be vacuous")
	}

	guarded := commandsDeclaringTreeState(t)
	if len(guarded) == 0 {
		t.Fatal("no command declares a -tree-state flag; the walk is not reaching cmd/")
	}

	var missing []string
	for _, workflow := range workflows {
		body, err := os.ReadFile(workflow)
		if err != nil {
			t.Fatal(err)
		}
		for _, line := range strings.Split(string(body), "\n") {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "#") {
				continue
			}
			for _, command := range guarded {
				// The command name must be followed by a boundary, or
				// cmd/catalog-admission matches cmd/catalog-admission-extra.
				marker := "./cmd/" + command
				index := strings.Index(trimmed, marker)
				if index < 0 {
					continue
				}
				if rest := trimmed[index+len(marker):]; rest != "" &&
					!strings.HasPrefix(rest, " ") && !strings.HasPrefix(rest, "'") {
					continue
				}
				if strings.Contains(trimmed, "-tree-state") {
					continue
				}
				missing = append(missing, fmt.Sprintf("%s: %s", filepath.Base(workflow), command))
			}
		}
	}
	sort.Strings(missing)

	if len(missing) > 0 {
		t.Errorf("%d call site(s) invoke a generator that requires a tree state without passing one:\n    %s\n\n"+
			"    Add -tree-state \"$(go run ./cmd/tree-state -what '...')\" to the command. The\n"+
			"    binary refuses without it, so this is a pipeline that fails at the step rather\n"+
			"    than a receipt that lies -- but it fails after everything above it has run.",
			len(missing), strings.Join(missing, "\n    "))
	}
	t.Logf("%d workflow(s) examined; %d command(s) require a tree state", len(workflows), len(guarded))
}

// commandsDeclaringTreeState lists the binaries that take the tree state as an
// argument, which is how a Go generator participates in this rule.
func commandsDeclaringTreeState(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir("cmd")
	if err != nil {
		t.Fatalf("reading cmd: %v", err)
	}
	var commands []string
	for _, entry := range entries {
		if !entry.IsDir() || entry.Name() == "tree-state" {
			continue
		}
		sources, err := filepath.Glob(filepath.Join("cmd", entry.Name(), "*.go"))
		if err != nil {
			t.Fatal(err)
		}
		for _, source := range sources {
			body, err := os.ReadFile(source)
			if err != nil {
				continue
			}
			if strings.Contains(string(body), `"tree-state"`) {
				commands = append(commands, entry.Name())
				break
			}
		}
	}
	sort.Strings(commands)
	return commands
}

// unguardedGenerators is a LEDGER, not a blessing: scripts that write evidence
// and do not establish their tree.
//
// None of these three can be executed to verify, and a call site never seen to
// run is the shape of change that looks like coverage and is not -- so they are
// left undone and written down rather than done blind.
//
// The reasons used to read "the pipeline that exercises it is down" for all
// three, which was one wrong word applied three times. "Down" says the pipeline
// tried and failed, and the correct answer differs per entry: one has no caller
// at all, one lost its workflow, and one runs correctly and is killed. That
// matters because it decides what would have to change for the exemption to
// clear, which is the only thing an exemption is for.
var unguardedGenerators = map[string]string{}

// classifiedTreeIdentity reports which files this check actually read, for the
// failure message, so a wrong verdict says what it was looking at.
//
// WRITTEN AFTER TWO RUNS OF ONE COMMIT DISAGREED. Pipelines 255 and 257 both
// reset --hard to 7e0def8d4dc8, read from their own clone steps; 255 reported
// catalog-controller-differential.sh as unguarded and 257 passed it, with a
// coverage count differing by one. The workspace held two branches' files at
// once: 255 ran tree-state-coverage_test.sh, which is ABSENT from the branch
// whose copy of the classified script it was reading.
//
// THE DIVERGENCE HALF IS THE ONE THAT CATCHES THIS, and the commit half would
// have shown nothing, because the two SHAs were identical. That is the opposite
// of what an earlier version of this comment said. An amend was proposed as the
// cause and refuted by reading the clone steps; I recorded the refuted version
// as established, having measured none of it myself.
//
// So the commit line is the cheap half and not the load-bearing one. It settles
// "was this even the same commit" in one field; the divergence settles "were
// these the commit's files", which is the question that was actually open here
// and cannot be answered from .git at all.
//
// Three readings of one incident, each confident, because the runs recorded a
// verdict and never what produced it. That is the argument for this function,
// and the fact that the comment describing it was wrong twice is the argument
// restated.
//
// It is not an assertion. A dirty scripts directory is normal while someone is
// editing one, and failing on that would make the check unusable locally. This
// only has to turn "why did that fail" into something the log already answers.
func classifiedTreeIdentity(t *testing.T, directory string) string {
	t.Helper()
	head, err := exec.Command("git", "rev-parse", "HEAD").Output()
	if err != nil {
		return "could not identify the tree this check classified: " + err.Error()
	}
	commit := strings.TrimSpace(string(head))

	status, err := exec.Command("git", "status", "--porcelain", "--", directory).Output()
	if err != nil {
		return fmt.Sprintf("classified %s at %s; its state could not be read: %v", directory, commit, err)
	}
	if strings.TrimSpace(string(status)) == "" {
		return fmt.Sprintf("classified %s at commit %s, matching the commit", directory, commit)
	}
	return fmt.Sprintf("classified %s at commit %s, but the files DIVERGE from it:\n%s\n"+
		"    The verdict above describes those files, not that commit. If nobody is editing\n"+
		"    them, something else wrote into this checkout.", directory, commit, strings.TrimRight(string(status), "\n"))
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
