// Command workspace-provenance prints what a CI step is about to read, so a run
// that read the wrong thing says so at the top of its own log.
//
// WHY IT EXISTS. Pipeline 255 called a script unguarded and 257 passed the same
// script with nothing changed between them. Both clone steps show the same SHA.
// The unguarded state existed in exactly one tree in the repository, and two
// builds of that other branch bracketed 255: the workspace did not match its
// commit.
//
// It took three attempts to diagnose -- contamination, then an amend and a
// force-push, then contamination again on SHAs read from the clone steps. Not
// because anyone was careless, but because the runs recorded verdicts and never
// recorded what produced them, so every explanation had to be reconstructed
// afterwards from outside, and each was plausible enough to stop at.
//
// THE COMMIT ALONE IS NOT ENOUGH, and that is the whole design. `git rev-parse
// HEAD` reads .git; the checks read files. A workspace written into after
// checkout reports a perfectly correct commit, which is exactly how this hid.
// The digests are of what will actually be read.
//
// This does not fix the sharing. It makes the next occurrence self-evident
// rather than something you have to be lucky to catch -- and, just as
// importantly, it makes a PASS attributable. A contaminated run can pass on
// evidence it should never have seen, and that failure mode leaves no trace at
// all.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

func main() {
	if err := run(os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(out *os.File) error {
	commit, err := gitOutput("rev-parse", "HEAD")
	if err != nil {
		return fmt.Errorf("workspace provenance: HEAD cannot be resolved: %w", err)
	}
	status, err := gitOutput("status", "--porcelain")
	if err != nil {
		return fmt.Errorf("workspace provenance: the working tree cannot be read: %w", err)
	}
	state := "clean"
	if strings.TrimSpace(status) != "" {
		state = "dirty"
	}

	scripts, err := digestOf(filepath.Join(".woodpecker", "scripts"), ".sh")
	if err != nil {
		return err
	}
	workflows, err := digestOf(".woodpecker", ".yml")
	if err != nil {
		return err
	}

	// No colon in this line. Woodpecker step output is read by humans, but the
	// same string tends to get pasted into a `commands:` printf, and YAML reads
	// `printf 'x: ...` as a mapping rather than a command.
	fmt.Fprintf(out, "workspace commit=%s tree=%s scripts=%s workflows=%s\n",
		commit, state, scripts, workflows)
	return nil
}

// digestOf hashes every file with the given suffix directly inside a directory.
//
// Names are included in the hash, not just contents, so a file appearing or
// disappearing changes the digest -- which is the case that matters here, since
// contamination showed up as a script whose guard was absent rather than
// altered.
func digestOf(directory, suffix string) (string, error) {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return "", fmt.Errorf("workspace provenance: %s cannot be listed: %w", directory, err)
	}
	var names []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), suffix) {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)

	overall := sha256.New()
	for _, name := range names {
		body, err := os.ReadFile(filepath.Join(directory, name))
		if err != nil {
			return "", fmt.Errorf("workspace provenance: %s cannot be read: %w", name, err)
		}
		each := sha256.Sum256(body)
		fmt.Fprintf(overall, "%s %x\n", name, each)
	}
	// Twelve characters is enough to compare two runs by eye in a log, which is
	// the only thing this is ever used for.
	return hex.EncodeToString(overall.Sum(nil))[:12], nil
}

func gitOutput(args ...string) (string, error) {
	output, err := exec.Command("git", args...).Output()
	if err != nil {
		return "", err
	}
	return strings.TrimRight(string(output), "\n"), nil
}
