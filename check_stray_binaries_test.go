package main

import (
	"errors"
	"os"
	"os/exec"
	"sort"
	"strings"
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
	names := commandNames(t)

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

// TestNoCommandBinaryIsTracked answers the question the test above CANNOT:
// is one of these executables already committed?
//
// THE TWO ARE NOT THE SAME QUESTION AND THE FIRST ONE PASSED WHILE FIVE WERE IN
// THE TREE. .gitignore has no authority over a tracked path, so "is this name
// covered by an ignore rule" answers yes for a file git is already carrying.
// When the rule above landed, main tracked catalog-admission,
// catalog-hardware-disposition, catalog-management-contract,
// catalog-migration-recovery and catalog-pragmatic-evidence -- 19,116,522 bytes
// -- and the check written to prevent exactly that reported no problem. It
// verified the DECLARATION and not the STATE.
//
// So this asks git what it is tracking rather than what it would ignore. The two
// oracles are independent: no single failure -- a broken rule, a missing rule, a
// rule that matches everything -- can satisfy both, which is the property that
// makes keeping them separate worth the duplication.
//
// RETRODICTION: run against eafdcd8f this reports five paths; against e3acf5d7,
// where they were removed, none. It describes a defect that was really there
// rather than one imagined for the future.
//
// PROVEN TO FAIL: `git add`ing a root file named after a command reports
// `catalog-parity is tracked at the repository root`.
func TestNoCommandBinaryIsTracked(t *testing.T) {
	names := commandNames(t)

	// Without this, an oracle that reports nothing as tracked -- git absent, the
	// wrong directory, a flag that silences it -- passes every command by saying
	// the tree is empty. main.go is tracked, so a working oracle must find it.
	if tracked, err := pathIsTracked("main.go"); err != nil {
		t.Fatalf("control: %v", err)
	} else if !tracked {
		t.Fatal("control: git does not report main.go as tracked, so the oracle cannot see the index and every case below would pass vacuously")
	}

	for _, name := range names {
		tracked, err := pathIsTracked(name)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if tracked {
			t.Errorf("%s is tracked at the repository root; it is the executable `go build ./cmd/%s` leaves behind, and .gitignore cannot untrack it -- `git rm --cached %s`",
				name, name, name)
		}
	}
}

// commandNames lists the directories under cmd/. Both checks above share it so
// they cannot drift apart into covering different populations, which would let
// one report a clean tree the other has never looked at.
func commandNames(t *testing.T) []string {
	t.Helper()
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
	if len(names) == 0 {
		t.Fatal("cmd/ contains no command directories, so the caller would pass without checking anything")
	}
	return names
}

// pathIsTracked asks git whether path is in the index at the repository root.
// ls-tree prints the name when it is tracked and nothing when it is not, so the
// answer is the presence of output rather than an exit code -- ls-tree exits 0
// either way, which is the distinction a status check would lose.
func pathIsTracked(path string) (bool, error) {
	out, err := exec.Command("git", "ls-tree", "--name-only", "HEAD", "--", path).Output()
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(string(out)) == path, nil
}
