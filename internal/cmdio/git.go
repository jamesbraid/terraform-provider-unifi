package cmdio

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
)

// GitOutput runs git and returns its output with surrounding whitespace removed.
//
// USE THIS FOR A SINGLE VALUE -- a commit SHA, a branch name, a describe. For
// output whose LEADING whitespace carries meaning, use GitLines.
//
// repo may be empty, which runs git in the current directory. Seven of the nine
// original callers passed a repository and two did not; that is a real
// difference in what the command means, so it is a parameter rather than a
// default.
func GitOutput(repo string, args ...string) (string, error) {
	out, err := runGit(repo, args)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// GitLines runs git and removes only trailing newlines.
//
// THE LEADING SPACE IS DATA. `git status --porcelain` emits "XY filename" where
// a leading space is a valid status: " M file" is unstaged-modified. TrimSpace
// would strip it and turn that into "M file", which is a different status on the
// line a tree-state check reads first.
//
// THE THREE ORIGINAL CALLERS THAT TRIMMED THIS WAY ARE EXACTLY THE THREE THAT
// PARSE PORCELAIN -- gounifipin, catalogparity's tree-state measurement, and
// workspace-provenance. That was not a coincidence and it was not a style
// choice; it was correct behaviour preserved by accident of which copy each
// command started from. Splitting the function is what stops a later majority
// vote from breaking them.
func GitLines(repo string, args ...string) (string, error) {
	out, err := runGit(repo, args)
	if err != nil {
		return "", err
	}
	return strings.TrimRight(out, "\n"), nil
}

// runGit invokes git and, on failure, reports what git actually said.
//
// EIGHT OF THE NINE ORIGINAL COPIES DISCARDED STDERR and reported only an exit
// status, which is the "hidden output is missing information" defect
// institutionalised in eight places. The single copy that captured it --
// m3-dns-qualification -- was the best of the nine and the least copied. Its
// behaviour is the default here.
//
// It costs nothing on the success path: the buffer is only read when the command
// fails.
func runGit(repo string, args []string) (string, error) {
	full := args
	if repo != "" {
		full = append([]string{"-C", repo}, args...)
	}
	var stdout, stderr bytes.Buffer
	command := exec.Command("git", full...) //nolint:gosec // arguments are built by callers, not user input
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		said := strings.TrimSpace(stderr.String())
		if said == "" {
			return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
		}
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, said)
	}
	return stdout.String(), nil
}
