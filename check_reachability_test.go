package main

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// TestEveryCheckIsReachable answers one of the two questions this repository
// cannot currently answer about itself: DOES ANYTHING RUN THIS CHECK?
//
// The other question -- has this check ever been shown to fail -- is not
// mechanically answerable and is not attempted here. When this test was written
// the great majority of check files said nowhere in the file that they had ever
// been watched failing; a large minority said it only in a commit message, which
// is invisible to anyone reading a checkout.
//
// THIS PARAGRAPH USED TO CARRY THE EXACT COUNTS AND THEY WENT STALE INSIDE A DAY.
// It said 142 files, 16 with in-file proof, 11 commit-message-only, 115 silent.
// Adding this file and its two siblings took the total past it, and a later
// measurement put the in-file figure at 27 rather than 16 -- so two of the three
// components and the total were wrong, while 16+11+115 still summed to 142 and
// read as reliable for exactly that reason. Nothing checked any of it: the test
// below is executable and cannot go stale without going red, and the prose beside
// it drifted within the hour.
//
// A COUNT IN A COMMENT IS A CLAIM WITH NO CHECK ON IT, and the discriminator is
// TENSE. A figure describing a SUPERSEDED state is history: nothing can move it,
// so the numbers above are safe and are kept, because deleting them would delete
// the story. A figure describing the CURRENT tree is a claim with no oracle. The
// first draft of this very paragraph named what the total had moved TO, which is
// the second kind, in the sentence explaining why the second kind is dangerous.
// Never cite a property of the file the comment lives in, for the same reason:
// the next edit invalidates it, including the edit that improves it.
//
// WHY THIS IS A GO TEST AND NOT A SHELL SCRIPT. `go test ./...` is the one thing
// a push to main executes, and fast-loop is the only workflow in this repository
// with an automatic trigger at all. A check about whether other checks run has to
// live somewhere that itself always runs, or it inherits the defect it looks for.
//
// That sentence used to say "the one thing every push executes", which is false
// and was measured so: fast-loop's push trigger is a literal three-branch list --
// main and two campaign branches, no globs -- and the other five workflows are
// event: manual only. A push to a work branch runs nothing. The conclusion holds
// and the claim behind it did not, which is the second stale justification this
// paragraph has carried.
//
// This paragraph used to argue from tree-state-coverage_test.sh instead: that it
// was invoked by nothing, RED, and falsely claimed by tree-state.sh:46 to be
// enforcing its list. All three were true when written and all three were false
// within hours -- sweep wired it at 9dfbb44e, struck the false claim at e322b7b9
// and made it green at 3612d278. The conclusion survived; the argument did not.
// Kept as a marker, because a comment explaining why something is correct does
// not expire when its reason does, and nothing in this repository checks that it
// still holds.
//
// DETECTION IS NOT GREP, AND BOTH DIRECTIONS OF THAT BIT ME WHILE WRITING IT:
//
//   - A MENTION IS NOT A CALL. tree-state.sh:46 names a script in a comment and
//     that read as an invocation. So did a comment I had added an hour earlier
//     to catalog-unit-differential.sh.
//   - A CALL CAN LOOK LIKE A COMMENT. Stripping every `//` line removed the
//     `//go:generate` directives and reported seven live generators as orphans.
//
// So comments are excluded, except go:generate, which is executable.
//
// PROVEN TO FAIL, both directions, and recorded here because a check that
// asserts other checks are wired had better be able to demonstrate its own.
// Commenting out fast-loop's invocation of catalog-evidence-inventory_test.sh
// names it as unreachable and undeclared. Adding a ledger entry for
// upgrade-plan-runner, which the upgrade pipeline does invoke, reports that
// entry as stale. Without the second direction the ledger would only ever grow.
//
// PROVEN AGAIN AFTER THE WALK GREW TO COVER PRODUCERS:
//
//   - UNDECLARED direction, by a REAL defect rather than a mutation. The first
//     run of the widened walk named m0-uos-dns-qualification.sh, which no
//     workflow, Makefile or script invokes. That is the finding, not a rehearsal
//     of one.
//   - RESOLVED direction, by mutation. Adding
//     `bash .woodpecker/scripts/m0-uos-dns-qualification.sh` to fast-loop reports
//     the new ledger entry as stale. The edit was confirmed present in the file
//     by a separate grep before the test ran, because verifying a mutation with
//     the mechanism that performed it is how a mutation that never applied
//     returns what looks like proof.
//
// AND THE WIDENING'S FIRST RESULT WAS THREE FALSE ACCUSATIONS, which is worth
// keeping because it is the failure mode that costs most. Requiring an
// interpreter before the path was invisibly sufficient while this walked only
// *_test.sh; production scripts are invoked bare, so catalog-upgrade-plan.sh,
// m1-dns-compiler.sh and m3-dns-operation.sh were all reported dead while wired.
// Fixing the pattern dropped exactly those three and kept the one real orphan.
//
// THE SHELL HALF IS GONE, WITH ITS SUBJECT. This walked .woodpecker/scripts for
// self-tests and producers until that directory held nothing and ceased to
// exist; a walk of a directory that cannot be there is not a narrower check, it
// is a check with no population, and the ReadDir would have failed rather than
// reported an empty one. What it caught -- a producer nothing invokes -- is the
// same question the command half asks, now that every producer is a command.
func TestEveryCheckIsReachable(t *testing.T) {
	sources := loadRepositorySources(t)

	var commands []string
	commandEntries, err := os.ReadDir("cmd")
	if err != nil {
		t.Fatalf("reading cmd: %v", err)
	}
	for _, entry := range commandEntries {
		if entry.IsDir() {
			commands = append(commands, entry.Name())
		}
	}
	// cmd/ KEEPS ITS COUNT, and dropping it was a mistake this comment exists to
	// stop being repeated. An earlier fix removed every floor here, reasoning
	// that a number which happens to be comfortable is not a property. That
	// misses the case os.Stat cannot see: a directory that resolves, reads
	// without error, and returns three entries where there should be twenty-six.
	// Pipeline 255 was exactly that -- a workspace holding another branch's
	// files -- and only a count catches it.
	//
	// The floor belongs here and belonged nowhere else, because cmd/ is the one
	// population that only grows. The shell populations were being emptied on
	// purpose, so a count there would have fired on the migration succeeding.
	if len(commands) < 5 {
		t.Fatalf("found %d command(s) under cmd/, a population that only grows; a truncated "+
			"read here would leave every verdict below describing a tree that is not this one",
			len(commands))
	}
	if len(sources) == 0 {
		t.Fatal("no sources were loaded, so every reachability verdict below would be vacuous")
	}

	unreachable := map[string]bool{}
	for _, name := range commands {
		if callers := commandCallersOf(name, sources); len(callers) == 0 {
			unreachable["command "+name] = true
		}
	}

	var undeclared, resolved []string
	for name := range unreachable {
		if _, declared := knownUnreachableChecks[name]; !declared {
			undeclared = append(undeclared, name)
		}
	}
	for name := range knownUnreachableChecks {
		if !unreachable[name] {
			resolved = append(resolved, name)
		}
	}
	sort.Strings(undeclared)
	sort.Strings(resolved)

	// Same reason as in the coverage check: this walks the working tree, so a
	// verdict has to say which files it read. See classifiedTreeIdentity.
	if len(undeclared) > 0 || len(resolved) > 0 {
		t.Log(classifiedTreeIdentity(t, "cmd"))
	}
	if len(undeclared) > 0 {
		t.Errorf("%d check(s) are invoked by nothing and are not in the ledger below:\n    %s\n\n"+
			"    A check nothing runs is not a check. Either wire it into a pipeline, or add it\n"+
			"    to knownUnreachableChecks with the reason it is not wired yet -- so that the\n"+
			"    next person reads a list of what is unguarded rather than assuming this one is.",
			len(undeclared), strings.Join(undeclared, "\n    "))
	}
	if len(resolved) > 0 {
		t.Errorf("%d check(s) are declared unreachable but something now invokes them:\n    %s\n\n"+
			"    Remove them from the ledger. A stale entry understates what is guarded, and the\n"+
			"    next reader has no way to tell a stale excuse from a live one.",
			len(resolved), strings.Join(resolved, "\n    "))
	}

	t.Logf("%d command(s) examined; %d declared unreachable",
		len(commands), len(knownUnreachableChecks))
}

// knownUnreachableChecks is a LEDGER, not a blessing. Every entry is a check
// that exists, is presumably correct, and that nothing executes. It is written
// down so the set cannot grow without somebody saying so, and so a reader can
// see what is unguarded without running anything.
var knownUnreachableChecks = map[string]string{
	// THREE ENTRIES LEFT THIS LEDGER AT THE CUTOVER, which is the event their
	// own reasons named: catalog-controller-differential, catalog-unit-differential
	// and catalog-dependency-publishability were unwired on purpose while the
	// scripts they replace were still authoritative, so each could be compared
	// against the deployed shell by a test that runs on every push. The workflows
	// now invoke all three and the scripts are deleted, so this test reports them
	// reachable and the entries had to go with them. An exemption that has stopped
	// being true reads as a live excuse.

	"command export-gate": "answers what a publication would ship, and is deliberately not wired. Its denied_paths are a FIRST-PASS declaration of the publication boundary and it reports 376 findings on this tree today -- every one a file that genuinely exists and would genuinely ship, not a defect. Wiring it before that boundary is agreed would make every push red for a judgement nobody has made yet, and a gate people learn to ignore is worse than one they have not switched on. Task 130.",
	// A FOURTH ENTRY LEFT ON THE SAME TERMS, and its reason had been sitting in
	// this ledger accurately for weeks: catalog-release-ready read "three of its
	// eight inputs have no producer. Task 106." That was measured again and was
	// exactly right. Two of the three -- contract parity and fleet soak -- named
	// receipts nothing implemented, and were removed with their validators. The
	// third, confidentiality, has an implemented check and no producer, so it was
	// dropped from the required set instead and the gate now names it in
	// unverified_gates. Five inputs remain, all produced by the controller
	// differential workflow, which now invokes the gate at the end of it.
	//
	// Worth saying plainly: nothing here was discovered. The entry stated the
	// blocker correctly the whole time, and what was missing was acting on it.
	// A ledger that records a defect faithfully still lets it sit.
	// NAMED BY THIS TEST AT THE CUTOVER, WHICH IS THE FINDING RATHER THAN A
	// REHEARSAL OF ONE. catalog-build-schema.sh built and ran it over five lines,
	// and was its only caller; deleting the script left it with none. The
	// binary that replaced the script does the same work by calling
	// internal/schemabaseline directly, so this command exists for the OTHER
	// caller it always had and nothing enforces: a person regenerating
	// build/m0/provider-schema-digests.json, which build/m0/README.md documents
	// with the exact invocation.
	//
	// That artifact is the EXPECTATION side of every parity check in this
	// repository. Producing it from a pipeline that runs against the tree being
	// checked would make the comparison vacuous, which is why the tool is
	// deliberately hand-run and why this is an entry rather than a deletion.
	"command schema-baseline": "authoring tool: it renders a CLI's raw schema dump into the " +
		"committed M0 baseline, by hand, per build/m0/README.md. Its only automated caller was " +
		"catalog-build-schema.sh, and the binary that replaced that script uses the library " +
		"rather than the command.",
	"command policy-scaffold": "authoring tool, run by hand when a surface is migrated.",
	"command list-policy-scaffold": "authoring tool for list policies, run by hand. Its output " +
		"is checked by the compiler and by rename_binding_test.go.",
}

// loadRepositorySources reads every file that could invoke something, with
// comments removed -- except go:generate, which is executable.
func loadRepositorySources(t *testing.T) map[string]string {
	t.Helper()
	sources := map[string]string{}
	err := filepath.Walk(".", func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			if info.Name() == ".git" || info.Name() == "vendor" {
				return filepath.SkipDir
			}
			return nil
		}
		switch {
		case strings.HasSuffix(path, ".yml"), strings.HasSuffix(path, ".yaml"),
			strings.HasSuffix(path, ".sh"), strings.HasSuffix(path, ".go"),
			info.Name() == "Makefile":
		default:
			return nil
		}
		body, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		var kept []string
		for _, line := range strings.Split(string(body), "\n") {
			trimmed := strings.TrimSpace(line)
			hashComment := strings.HasPrefix(strings.TrimPrefix(trimmed, "- "), "#")
			slashComment := strings.HasPrefix(trimmed, "//") && !strings.HasPrefix(trimmed, "//go:generate")
			if hashComment || slashComment {
				continue
			}
			kept = append(kept, line)
		}
		sources[path] = strings.Join(kept, "\n")
		return nil
	})
	if err != nil {
		t.Fatalf("walking the repository: %v", err)
	}
	if len(sources) < 50 {
		t.Fatalf("read only %d source files; the walk is not reaching the tree", len(sources))
	}
	return sources
}

func commandCallersOf(name string, sources map[string]string) []string {
	pattern := regexp.MustCompile(`go\s+(?:run|build)[^\n]*\./cmd/` + regexp.QuoteMeta(name) + `\b` +
		`|go:generate[^\n]*\bcmd/` + regexp.QuoteMeta(name) + `\b`)
	var callers []string
	for path, body := range sources {
		if strings.HasPrefix(path, filepath.Join("cmd", name)+string(filepath.Separator)) {
			continue
		}
		if pattern.MatchString(joinShellContinuations(body)) {
			callers = append(callers, path)
		}
	}
	return callers
}

// joinShellContinuations folds `\` line continuations into one line, because
// both patterns above are anchored with [^\n]* and a shell command split across
// lines is still one command.
//
// FOUND BY A FALSE ACCUSATION, not by review. catalog-build-schema.sh really
// does build ./cmd/schema-baseline, over five lines with the `go build` on one
// and the package path on the next. The check called the command unreachable
// and was wrong. It stayed hidden because m1-dns-compiler.sh happened to invoke
// the same command on a single line, so one real caller was masking the fact
// that the other could never be seen -- and deleting the dead script is what
// exposed it.
//
// The pattern was narrower than the claim it made. Nothing in the tree is
// obliged to keep a command on one line for a regex's benefit.
func joinShellContinuations(body string) string {
	return strings.NewReplacer("\\\n", " ", "\\\r\n", " ").Replace(body)
}
