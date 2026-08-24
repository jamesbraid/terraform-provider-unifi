package cmdio

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func fixtureRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, args := range [][]string{
		{"init", "--quiet"},
		{"config", "user.email", "fixture@example.invalid"},
		{"config", "user.name", "cmdio fixture"},
		{"config", "commit.gpgsign", "false"},
	} {
		out, err := exec.Command("git", append([]string{"-C", root}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "tracked.txt"), []byte("one\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"add", "-A"}, {"commit", "--quiet", "-m", "fixture"}} {
		out, err := exec.Command("git", append([]string{"-C", root}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
	return root
}

// TestGitLinesKeepsTheLeadingStatusColumn is the reason this function exists
// separately from GitOutput: `git status --porcelain` emits " M tracked.txt"
// for an unstaged modification, and TrimSpace would turn that into
// "M tracked.txt", which reads as staged-modified instead.
func TestGitLinesKeepsTheLeadingStatusColumn(t *testing.T) {
	repo := fixtureRepo(t)
	if err := os.WriteFile(filepath.Join(repo, "tracked.txt"), []byte("two\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	lines, err := GitLines(repo, "status", "--porcelain")
	if err != nil {
		t.Fatalf("GitLines() error = %v", err)
	}
	if !strings.HasPrefix(lines, " M ") {
		t.Errorf("porcelain output = %q, want it to start with the leading status space \" M \".\n"+
			"    Left-trimming this changes an unstaged modification into a staged one.", lines)
	}

	// The control: GitOutput on the same command DOES strip it, which is why the
	// porcelain callers must not use GitOutput.
	trimmed, err := GitOutput(repo, "status", "--porcelain")
	if err != nil {
		t.Fatal(err)
	}
	if strings.HasPrefix(trimmed, " ") {
		t.Error("GitOutput preserved the leading space; the two functions are then the same " +
			"and the split proves nothing")
	}
}

// TestGitOutputTrimsASingleValue covers the ordinary case: a commit SHA with no
// trailing newline.
func TestGitOutputTrimsASingleValue(t *testing.T) {
	repo := fixtureRepo(t)
	head, err := GitOutput(repo, "rev-parse", "HEAD")
	if err != nil {
		t.Fatalf("GitOutput() error = %v", err)
	}
	if len(head) != 40 || strings.ContainsAny(head, " \n\t") {
		t.Errorf("rev-parse HEAD = %q, want a bare 40-character SHA", head)
	}
}

// TestFailureCarriesWhatGitSaid is the whole reason stderr is captured.
func TestFailureCarriesWhatGitSaid(t *testing.T) {
	repo := fixtureRepo(t)
	_, err := GitOutput(repo, "rev-parse", "definitely-not-a-ref")
	if err == nil {
		t.Fatal("resolving a nonexistent ref succeeded")
	}
	if !strings.Contains(err.Error(), "definitely-not-a-ref") {
		t.Errorf("the error does not carry git's own message, only an exit status: %v", err)
	}
}

// TestEmptyRepoRunsInTheCurrentDirectory covers a caller that passes no
// repository at all.
func TestEmptyRepoRunsInTheCurrentDirectory(t *testing.T) {
	// The package's own directory is inside this repository, so a rev-parse
	// succeeds without -C.
	if _, err := GitOutput("", "rev-parse", "--is-inside-work-tree"); err != nil {
		t.Errorf("GitOutput with no repository failed: %v", err)
	}
}
