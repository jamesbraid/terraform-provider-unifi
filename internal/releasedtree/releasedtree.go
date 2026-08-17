// Package releasedtree materialises the released side of a comparison.
//
// Two gates need the same two operations -- resolve a tag to a commit, and
// unpack that tag's tree somewhere -- and both had their own copy in shell.
// They are here rather than in either command because the released side is the
// thing every catalog comparison is measured against: two implementations of it
// is two answers to "what was released", and the receipts would not say which
// one they used.
package releasedtree

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// ResolveCommit reports the commit a tag names.
//
// It takes the tag rather than assuming one. Both scripts this replaces wrote
// v0.101.2 as a literal beside a policy file that also records the version, so
// the two could drift with nothing comparing them.
func ResolveCommit(repository, tag string) (string, error) {
	out, err := exec.Command("git", "-C", repository, "rev-parse", tag+"^{commit}").Output()
	if err != nil {
		// NAME THE PREREQUISITE. `git rev-parse` on an absent tag exits 128 with
		// "ambiguous argument", which reads like a broken invocation rather than
		// a clone that has no tags -- and a tagless or shallow clone is exactly
		// what a CI checkout can be. A reader who is told the tag is missing
		// fetches it; a reader who is told the argument is ambiguous goes
		// looking at this code.
		return "", fmt.Errorf("cannot resolve %s in %s: %w. The released tag must be present "+
			"locally: a shallow or tagless clone does not have it, and nothing here fetches it",
			tag, repository, err)
	}
	commit := strings.TrimSpace(string(out))
	if commit == "" {
		return "", fmt.Errorf("%s resolved to nothing in %s", tag, repository)
	}
	return commit, nil
}

// RequireCommit resolves the tag and requires it to be the commit a policy or
// manifest declares.
//
// Without this the released side of every downstream comparison is whatever the
// tag currently points at. A tag can be moved, and a moved tag produces a
// complete, plausible, entirely unidentified set of receipts.
func RequireCommit(repository, tag, declared string) (string, error) {
	if declared == "" {
		return "", fmt.Errorf("no released commit is declared for %s, so nothing identifies "+
			"the released side of this comparison", tag)
	}
	resolved, err := ResolveCommit(repository, tag)
	if err != nil {
		return "", err
	}
	if resolved != declared {
		return "", fmt.Errorf("%s resolves to %s but %s is declared as the released commit. "+
			"The released side would be an unidentified tree", tag, resolved, declared)
	}
	return resolved, nil
}

// Extract unpacks a tag's tree into destination, which must already exist.
//
// git archive rather than a worktree or a checkout: it yields exactly the
// tracked content at that tag with no .git directory, no ignored files and no
// state left behind in the repository it was read from.
func Extract(repository, tag, destination string) error {
	info, err := os.Stat(destination)
	if err != nil {
		return fmt.Errorf("extract %s: %w", tag, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("extract %s: %s is not a directory", tag, destination)
	}

	archive := exec.Command("git", "-C", repository, "archive", "--format=tar", tag)
	untar := exec.Command("tar", "-x", "-C", destination)
	pipe, err := archive.StdoutPipe()
	if err != nil {
		return err
	}
	untar.Stdin = pipe
	var complaint strings.Builder
	archive.Stderr = &complaint
	untar.Stderr = &complaint
	if err := untar.Start(); err != nil {
		return err
	}
	if err := archive.Run(); err != nil {
		_ = untar.Wait()
		return fmt.Errorf("git archive %s: %w: %s", tag, err, complaint.String())
	}
	if err := untar.Wait(); err != nil {
		return fmt.Errorf("unpack %s into %s: %w: %s", tag, destination, err, complaint.String())
	}

	// An empty destination means the pipeline "succeeded" and produced nothing,
	// which every later comparison would read as the released side having no
	// files rather than as an extraction that did not happen.
	entries, err := os.ReadDir(destination)
	if err != nil {
		return err
	}
	if len(entries) == 0 {
		return fmt.Errorf("extracting %s produced no files in %s", tag, destination)
	}
	return nil
}

// ExtractTemp makes a directory under parent and extracts into it, returning
// the path. The caller owns it.
func ExtractTemp(repository, tag, parent string) (string, error) {
	destination, err := os.MkdirTemp(parent, "released-")
	if err != nil {
		return "", err
	}
	if err := Extract(repository, tag, destination); err != nil {
		_ = os.RemoveAll(destination)
		return "", err
	}
	return filepath.Clean(destination), nil
}
