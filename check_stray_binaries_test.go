package main

import (
	"errors"
	"os"
	"os/exec"
	"sort"
	"testing"
)

// TestEveryCommandBinaryIsIgnored answers: IF SOMEONE RUNS `go build ./cmd/X`
// FROM THE ROOT, DOES THE EXECUTABLE IT LEAVES BEHIND GET IGNORED?
//
// Without an -o, go build writes the binary into the working directory named
// after the command's directory, and the root is where every other go command
// in this repository is run. The result is an untracked multi-megabyte
// executable sitting beside the source, which `git add -A` will happily take.
// One commit did exactly that with five of them -- 19 MB, on a shared branch,
// and the binaries carried the absolute build path of the machine that made
// them.
//
// A HAND-WRITTEN LIST OF NAMES WOULD ROT, AND THE PROOF IS ALREADY IN
// .gitignore. Someone wrote this rule once, for upgrade-plan-runner, and a
// missing newline joined it to the line above:
//
//	/.woodpecker/fixtures/catalog-acceptance/devices.fleet.json/upgrade-plan-runner
//
// which is one path matching nothing rather than two matching something. It was
// correct when written, has never worked, and nothing noticed -- there was no
// check to notice with. So the list is bound to the tree here: every directory
// under cmd/ must be ignored at the root, and a twentieth command that nobody
// adds a rule for fails this test by name.
//
// THE ORACLE IS GIT ITSELF. Parsing .gitignore here would put the matching
// semantics in two places -- git's and ours -- which is the drift this file
// exists to prevent, one level up. `git check-ignore` needs no file on disk, so
// nothing is created or cleaned up.
//
// PROVEN TO FAIL: creating cmd/scope-probe/ with no matching rule fails with
// `cmd/scope-probe builds a root executable named "scope-probe" that
// .gitignore does not cover`.
func TestEveryCommandBinaryIsIgnored(t *testing.T) {
	entries, err := os.ReadDir("cmd")
	if err != nil {
		t.Fatalf("read cmd/: %v", err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)

	// Without this, an empty or unreadable cmd/ makes the loop below iterate
	// nothing and the test pass having checked nothing -- the shape where a
	// zero-length population reports success.
	if len(names) == 0 {
		t.Fatal("cmd/ contains no command directories, so this test would pass without checking anything")
	}

	// Without this, an oracle that answers "ignored" to everything -- git
	// missing, wrong working directory, a rule that swallows the root -- passes
	// every command vacuously. main.go is tracked and matched by no rule, so a
	// working oracle must report it NOT ignored.
	if ignored, err := pathIsIgnored("main.go"); err != nil {
		t.Fatalf("control: %v", err)
	} else if ignored {
		t.Fatal("control: git reports main.go as ignored, so the oracle cannot distinguish ignored from not and every case below would pass vacuously")
	}

	for _, name := range names {
		ignored, err := pathIsIgnored(name)
		if err != nil {
			t.Fatalf("cmd/%s: %v", name, err)
		}
		if !ignored {
			t.Errorf("cmd/%s builds a root executable named %q that .gitignore does not cover; add /%s",
				name, name, name)
		}
	}
}

// pathIsIgnored asks git whether path would be ignored. It distinguishes "not
// ignored" (exit 1) from a genuine failure such as git being absent or the
// directory not being a repository (exit 128), because treating the second as
// the first would report every command as covered.
func pathIsIgnored(path string) (bool, error) {
	err := exec.Command("git", "check-ignore", "--quiet", "--no-index", path).Run()
	if err == nil {
		return true, nil
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() == 1 {
		return false, nil
	}
	return false, err
}
