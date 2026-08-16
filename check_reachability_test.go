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
// mechanically answerable and is not attempted here. Measured while writing
// this: of 142 check files, 16 say in the file that they have been shown to
// fail, 11 more say it only in a commit message and are therefore invisible to
// anyone reading a checkout, and 115 say it nowhere.
//
// WHY THIS IS A GO TEST AND NOT A SHELL SCRIPT. There is already a shell
// script that checks a version of this: .woodpecker/scripts/tree-state-coverage_test.sh.
// It is invoked by nothing, it is RED on the current tree, and tree-state.sh:46
// claims it "enforces this list". A coverage check that nothing runs is the
// defect it exists to find, wearing its own clothes. `go test ./...` is the one
// thing every push executes, so that is where a check about reachability has to
// live.
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
func TestEveryCheckIsReachable(t *testing.T) {
	sources := loadRepositorySources(t)

	var shellChecks, commands []string
	entries, err := os.ReadDir(filepath.Join(".woodpecker", "scripts"))
	if err != nil {
		t.Fatalf("reading .woodpecker/scripts: %v", err)
	}
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), "_test.sh") {
			shellChecks = append(shellChecks, entry.Name())
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
	if len(shellChecks) < 5 || len(commands) < 5 {
		t.Fatalf("found %d shell self-test(s) and %d command(s); the walk is not reaching the tree",
			len(shellChecks), len(commands))
	}

	unreachable := map[string]bool{}
	for _, name := range shellChecks {
		if callers := shellCallersOf(name, filepath.Join(".woodpecker", "scripts", name), sources); len(callers) == 0 {
			unreachable["script "+name] = true
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

	t.Logf("%d shell self-test(s) and %d command(s) examined; %d declared unreachable",
		len(shellChecks), len(commands), len(knownUnreachableChecks))
}

// knownUnreachableChecks is a LEDGER, not a blessing. Every entry is a check
// that exists, is presumably correct, and that nothing executes. It is written
// down so the set cannot grow without somebody saying so, and so a reader can
// see what is unguarded without running anything.
var knownUnreachableChecks = map[string]string{
	"script catalog-build-schema_test.sh": "self-test for the schema gate two pipelines run. " +
		"Its assertions are all constants the script writes after its own cmp calls, so wiring " +
		"it up would add little until it is rewritten. Filed as task 103.",
	"script catalog-unit-differential_test.sh": "its one informative assertion, that " +
		"package_pass_count is above zero, was moved into the script itself in 8aeef4c7 " +
		"because the script is what runs.",
	"script tree-state_test.sh": "the best-constructed failing-input test in the shell layer, " +
		"guarding tree-state.sh, which five production scripts source. Task 103.",
	"script tree-state-coverage_test.sh": "RED on the current tree, and tree-state.sh:46 claims " +
		"it enforces its list. It does not. Task 103.",
	"command catalog-release-ready": "the terminal release gate. No pipeline invokes it and " +
		"three of its eight inputs have no producer. Task 106.",
	"command policy-scaffold":  "authoring tool, run by hand when a surface is migrated.",
	"command schema-behaviour": "authoring tool, run by hand when a surface is migrated.",
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

func shellCallersOf(name, ownPath string, sources map[string]string) []string {
	pattern := regexp.MustCompile(`(?:^|[\s;&|(])(?:bash|sh|source|\.)\s+\S*` + regexp.QuoteMeta(name) + `\b`)
	var callers []string
	for path, body := range sources {
		if path == ownPath {
			continue
		}
		if pattern.MatchString(body) {
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
		if pattern.MatchString(body) {
			callers = append(callers, path)
		}
	}
	return callers
}
