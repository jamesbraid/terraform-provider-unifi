package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// TestEveryGeneratorCallSitePassesTheTreeState is the half that survived the
// shell going away, and it is now the whole file.
//
// THIS FILE USED TO HOLD TWO MORE TESTS AND THEIR SUBJECTS ARE GONE.
// TestEveryEvidenceGeneratorIsGuarded walked .woodpecker/scripts/*.sh for
// generators that write an artifact without establishing which tree it
// describes, and TestEveryGuardedGeneratorRefusesADirtyTree ran each of those
// scripts against a deliberately dirtied tree and required it to refuse. Both
// were deleted with the last script, in the commit that deleted it.
//
// WHAT THAT COST, STATED RATHER THAN GLOSSED. The second one was the only
// END-TO-END proof that the guard fires: it dirtied the tree, ran the real
// generator, and read the refusal. What remains is a unit test of
// catalogparity.MeasureTreeState, which is where the refusal is decided, plus
// this static check that every call site passes the flag, plus per-command
// tests that a binary refuses when the flag is absent. The composition of those
// three is not watched failing anywhere; nothing runs `go run ./cmd/tree-state`
// on a dirty tree and reads the exit code.
//
// A BINARY'S GUARD LIVES AT ITS CALL SITE, not inside it. It is handed the tree
// state rather than measuring it -- deliberately, so there is one measurement
// rather than two that can disagree -- which means the thing that can be wrong
// is a workflow line that invokes the generator and omits the flag. That line
// is in .woodpecker/*.yml, which is why this half is the one that keeps working
// with no script left.
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
//
// AND ONE THAT DOES EXIST, ARRIVING WITH THE CUTOVER. catalog-controller-differential
// is invoked a second time as `-followup <receipt>`, which reads a receipt this
// command already wrote and prints one word. It produces no evidence, so there
// is no tree for it to describe and passing the flag would assert something the
// mode does not do. The exception is keyed on that flag by name rather than on
// the command, so it covers exactly the mode that earned it -- and it would stop
// covering the day -followup starts writing something, which is the moment it
// should stop.
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
			// A CLASSIFYING INVOCATION IS NOT A GENERATING ONE. See the
			// -followup paragraph above: this mode reads an existing receipt
			// and writes nothing.
			if strings.Contains(trimmed, "-followup ") {
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
