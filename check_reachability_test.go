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
func TestEveryCheckIsReachable(t *testing.T) {
	sources := loadRepositorySources(t)

	var shellChecks, producers, commands []string
	entries, err := os.ReadDir(filepath.Join(".woodpecker", "scripts"))
	if err != nil {
		t.Fatalf("reading .woodpecker/scripts: %v", err)
	}
	for _, entry := range entries {
		switch {
		case strings.HasSuffix(entry.Name(), "_test.sh"):
			shellChecks = append(shellChecks, entry.Name())
		case strings.HasSuffix(entry.Name(), ".sh"):
			// PRODUCERS, ADDED AFTER THIS TEST MISSED ONE. The walk used to
			// cover *_test.sh and cmd/ and nothing else, so a production script
			// that no pipeline invokes was invisible to it -- and there was one:
			// m0-uos-dns-qualification.sh builds build/m0/uos-dns-qualification.json
			// and is called by no workflow, no Makefile and no other script. I
			// had filed that artifact as "producer runs but writes nowhere"
			// because its output variable is never set. The truer statement is
			// that the producer never runs at all, and THIS TEST SHOULD HAVE
			// BEEN THE THING THAT TOLD ME.
			//
			// A producer nothing invokes is the same defect as a check nothing
			// invokes: a file whose existence implies a guarantee the tree does
			// not have. The ledger holds both, tagged by kind.
			producers = append(producers, entry.Name())
		}
	}
	commandEntries, err := os.ReadDir("cmd")
	if err != nil {
		t.Fatalf("reading cmd: %v", err)
	}
	for _, entry := range commandEntries {
		if entry.IsDir() {
			commands = append(commands, entry.Name())
		}
	}
	// Without this the whole test passes by finding nothing to check, which is
	// the shape it exists to detect.
	if len(shellChecks) < 5 || len(producers) < 5 || len(commands) < 5 {
		t.Fatalf("found %d shell self-test(s), %d producer(s) and %d command(s); the walk is not reaching the tree",
			len(shellChecks), len(producers), len(commands))
	}

	unreachable := map[string]bool{}
	for _, name := range shellChecks {
		if callers := shellCallersOf(name, filepath.Join(".woodpecker", "scripts", name), sources); len(callers) == 0 {
			unreachable["script "+name] = true
		}
	}
	for _, name := range producers {
		if callers := shellCallersOf(name, filepath.Join(".woodpecker", "scripts", name), sources); len(callers) == 0 {
			unreachable["producer "+name] = true
		}
	}
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
		t.Log(classifiedTreeIdentity(t, filepath.Join(".woodpecker", "scripts")))
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

	t.Logf("%d shell self-test(s), %d producer(s) and %d command(s) examined; %d declared unreachable",
		len(shellChecks), len(producers), len(commands), len(knownUnreachableChecks))
}

// knownUnreachableChecks is a LEDGER, not a blessing. Every entry is a check
// that exists, is presumably correct, and that nothing executes. It is written
// down so the set cannot grow without somebody saying so, and so a reader can
// see what is unguarded without running anything.
var knownUnreachableChecks = map[string]string{
	"producer m0-uos-dns-qualification.sh": "builds build/m0/uos-dns-qualification.json and is " +
		"invoked by no workflow, no Makefile and no other script. Its output variable " +
		"M0_UOS_RECEIPT_OUTPUT is also never set, so even if something did run it the receipt " +
		"would go nowhere. This is the ONLY producer in .woodpecker/scripts that nothing runs, " +
		"and it is not a one-line fix: wiring it needs a UOS qualification step nobody can " +
		"verify from here. Task 114 P4, escalated to James rather than guessed at.",
	// LANDED BESIDE THE SHELL, ON PURPOSE, AND FOR A BOUNDED TIME. Both replace
	// a .woodpecker/scripts gate that is still authoritative, and both are
	// compared against it by a test that runs on every push -- the unit
	// differential extracts the deployed jq program from the script rather than
	// copying it, and the dependency gate was diffed field by field against the
	// shell's own output. That comparison is only possible while BOTH exist,
	// which is the whole reason these are unwired rather than swapped in.
	//
	// They leave this ledger when .woodpecker/*.yml points at them, which is
	// the YAML lane's cutover. Both declare -tree-state with no default and
	// refuse without it, so every call site written for them must pass it.
	"command catalog-controller-differential": "replaces " +
		"catalog-controller-differential.sh, which still runs. Its plan is reproduced from the " +
		"two committed inputs, its suite summariser is compared against the script's own jq " +
		"program, and its two controller-free modes -- -plan-only and -prepare-only -- are run " +
		"for real by tests. Wires in at the YAML cutover; task 148.",
	"command catalog-unit-differential": "replaces catalog-unit-differential.sh, which still " +
		"runs. Its summariser is compared against the script's own jq program on every push. " +
		"Wires in at the YAML cutover; task 148.",
	"command catalog-dependency-publishability": "replaces " +
		"catalog-dependency-publishability.sh, which still runs. Diffed field by field against " +
		"the shell's output; three differences, two declared and one a defect (task 158). " +
		"Wires in at the YAML cutover; task 148.",

	"command catalog-release-ready": "the terminal release gate. No pipeline invokes it and " +
		"three of its eight inputs have no producer. Task 106.",
	"command policy-scaffold":  "authoring tool, run by hand when a surface is migrated.",
	"command schema-behaviour": "authoring tool, run by hand when a surface is migrated.",
	"command list-policy-scaffold": "authoring tool for list policies, run by hand. Its output " +
		"is checked by the compiler and by rename_binding_test.go.",
	"script m1-evidence-lib_test.sh": "tests m1-evidence-lib.sh, which sweep is porting to Go; " +
		"the test goes with the port. Its only two callers were m1-dns-compiler.yml and the " +
		"promotion-receipt step of m3-dns-qualification.yml, both removed here because they " +
		"invoked scripts that no longer exist. Worth noting that BOTH were event: manual, so " +
		"the self-test for a shared library has never had an automatic runner -- removing " +
		"those steps exposed that rather than causing it.",
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

// shellCallersOf finds the files that invoke a script.
//
// TWO INVOCATION FORMS, AND ONLY ONE OF THEM USED TO BE DETECTED:
//
//	bash .woodpecker/scripts/foo.sh     an interpreter and a path
//	.woodpecker/scripts/foo.sh          a bare path, executable with a shebang
//	script=${root}/.woodpecker/scripts/foo.sh   assigned, then run through the variable
//
// The self-tests all use the first form, so requiring an interpreter was
// invisibly sufficient while this test only walked *_test.sh. Extending it to
// production scripts made the gap real at once: m1-dns-compiler.sh and
// m3-dns-operation.sh are invoked bare from their workflows and were reported as
// orphans. Accusing a wired script of being dead is the worse direction to fail
// in -- it costs the reader's trust in every other row.
//
// A PATH IS REQUIRED IN EVERY FORM, and that is load-bearing rather than
// incidental. tree-state-coverage_test.sh carries two arrays of BARE script
// names, m0-uos-dns-qualification.sh among them. Matching a bare name would read
// those arrays as call sites and report the one genuinely orphaned producer in
// this repository as wired. The slash is what separates naming a script from
// running one.
func shellCallersOf(name, ownPath string, sources map[string]string) []string {
	pattern := regexp.MustCompile(
		`(?:^|[\s;&|(=])(?:(?:bash|sh|source|\.)\s+)?\S*/` + regexp.QuoteMeta(name) + `\b`)
	var callers []string
	for path, body := range sources {
		if path == ownPath {
			continue
		}
		if pattern.MatchString(joinShellContinuations(body)) {
			callers = append(callers, path)
		}
	}
	return callers
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
