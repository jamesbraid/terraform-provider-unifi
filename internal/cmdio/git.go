package cmdio

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
)

// GitOutput runs git and returns its output with surrounding whitespace
// removed. Use this for a single value (a commit SHA, a branch name); for
// output whose leading whitespace carries meaning, use GitLines. repo may
// be empty, which runs git in the current directory.
func GitOutput(repo string, args ...string) (string, error) {
	out, err := runGit(repo, args)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// GitLines runs git and removes only trailing newlines. The leading space
// is data: `git status --porcelain` emits " M file" for unstaged-modified,
// and TrimSpace would turn that into "M file", a different status.
func GitLines(repo string, args ...string) (string, error) {
	out, err := runGit(repo, args)
	if err != nil {
		return "", err
	}
	return strings.TrimRight(out, "\n"), nil
}

// runGit invokes git and, on failure, reports what git actually said on
// stderr. That costs nothing on the success path: the buffer is only read
// when the command fails.
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
