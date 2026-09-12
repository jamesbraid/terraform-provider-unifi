package cmdio

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// The repository root holds a .gitignore rule for every `go build ./cmd/<name>`
// binary, because go build without an -o writes one into the working directory
// and a multi-megabyte executable beside the source is easy to sweep into a
// commit. gitignore has no way to derive that list from cmd/, so it is written
// out by hand -- and a hand list drifts. It did: descriptor-emitter arrived
// with no line, under a comment claiming the set was complete.
//
// These live in cmdio because it is the package the cmd/ tools share.
const (
	gitignorePath       = "../../.gitignore"
	commandsDir         = "../../cmd"
	commandBlockBegin   = "# cmd binaries begin"
	commandBlockEnd     = "# cmd binaries end"
	gitignoreFixMessage = "\n\n" +
		"    The rule only works if the list is complete: a name missing from it is a\n" +
		"    binary git will happily stage, and the comment above the list will go on\n" +
		"    saying otherwise. Add or remove the line, do not adjust this test."
)

// TestGitignoreListsEveryCommand compares the ignore block against cmd/ in
// both directions: a command with no line is a binary that can be committed,
// and a line with no command is an entry that outlived its tool and makes the
// list read as maintained when nobody is maintaining it.
func TestGitignoreListsEveryCommand(t *testing.T) {
	commands := commandNames(t)
	ignored := ignoredCommandNames(t)

	if problems := commandIgnoreProblems(commands, ignored); len(problems) > 0 {
		t.Errorf("%s and cmd/ disagree about %d name(s):\n    %s%s",
			gitignorePath, len(problems), strings.Join(problems, "\n    "), gitignoreFixMessage)
	}
}

// TestCommandIgnoreProblemsReportsBothDirections is the comparison's positive
// control. The real tree is expected to be clean, so the only way to show the
// check can fail is to feed it a tree that is not.
func TestCommandIgnoreProblemsReportsBothDirections(t *testing.T) {
	problems := commandIgnoreProblems(
		[]string{"kept", "unignored"},
		[]string{"kept", "departed"},
	)
	if len(problems) != 2 {
		t.Fatalf("one unignored command and one stale entry produced %d problem(s), want 2: %v",
			len(problems), problems)
	}
	joined := strings.Join(problems, "\n")
	if !strings.Contains(joined, "unignored") || !strings.Contains(joined, "departed") {
		t.Errorf("the problems do not name both sides of the disagreement:\n%s", joined)
	}
	if strings.Contains(joined, "kept") {
		t.Errorf("a name present on both sides was reported anyway:\n%s", joined)
	}

	if problems := commandIgnoreProblems([]string{"same"}, []string{"same"}); len(problems) != 0 {
		t.Errorf("an agreeing pair was reported as a problem: %v", problems)
	}
}

// commandIgnoreProblems reports every name the two sides disagree about.
func commandIgnoreProblems(commands, ignored []string) []string {
	inIgnore := map[string]bool{}
	for _, name := range ignored {
		inIgnore[name] = true
	}
	inCommands := map[string]bool{}
	for _, name := range commands {
		inCommands[name] = true
	}

	var problems []string
	for _, name := range sortedNames(commands) {
		if !inIgnore[name] {
			problems = append(problems, fmt.Sprintf(
				"cmd/%s builds a binary nothing in the ignore block matches", name))
		}
	}
	for _, name := range sortedNames(ignored) {
		if !inCommands[name] {
			problems = append(problems, fmt.Sprintf(
				"the ignore block lists /%s, and there is no cmd/%s", name, name))
		}
	}
	return problems
}

// commandNames reads the directory names under cmd/.
func commandNames(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir(commandsDir)
	if err != nil {
		t.Fatalf("reading %s: %v", commandsDir, err)
	}
	var names []string
	for _, entry := range entries {
		if entry.IsDir() {
			names = append(names, entry.Name())
		}
	}
	if len(names) == 0 {
		t.Fatalf("%s holds no command directories, so every ignore entry would read as "+
			"stale and the check would fail for the wrong reason", commandsDir)
	}
	return names
}

// ignoredCommandNames reads the names between the two markers, with the
// leading slash stripped.
func ignoredCommandNames(t *testing.T) []string {
	t.Helper()
	raw, err := os.ReadFile(filepath.FromSlash(gitignorePath))
	if err != nil {
		t.Fatalf("reading %s: %v", gitignorePath, err)
	}

	var names []string
	var inBlock, sawBlock bool
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		switch {
		case line == commandBlockBegin:
			inBlock, sawBlock = true, true
		case line == commandBlockEnd:
			inBlock = false
		case inBlock && line != "":
			names = append(names, strings.TrimPrefix(line, "/"))
		}
	}
	if !sawBlock {
		t.Fatalf("%s carries no %q marker, so the block cannot be located and every "+
			"command would read as unignored", gitignorePath, commandBlockBegin)
	}
	if len(names) == 0 {
		t.Fatalf("the block in %s is empty", gitignorePath)
	}
	return names
}

func sortedNames(names []string) []string {
	out := append([]string(nil), names...)
	sort.Strings(out)
	return out
}
