// Command workspace-provenance prints what a CI step is about to read, so a run
// that read the wrong thing says so at the top of its own log.
//
// WHY IT EXISTS. Pipelines 255 and 257 both reset --hard to 7e0def8d4dc8 and
// disagreed: 255 failed a script that 257 passed, with coverage counts of 11
// and 12 of 15. 255 ran a self-test that is absent from the branch whose copy
// of the classified script it was simultaneously reading. One workspace, two
// branches' files.
//
// FOUR CONFIDENT AND CONFLICTING STATEMENTS WERE MADE ABOUT THIS ONE INCIDENT
// before it settled: contamination, then an amend and a force-push, then
// contamination again once the SHAs were read from the clone steps and the
// reflog showed no amend, and separately a wrong workflow -- the failing step
// was in fast-loop, misread from the branch name accept/m3-port.
//
// Nobody was careless. The runs recorded verdicts and never recorded what
// produced them, so every explanation had to be reconstructed from outside, and
// each was plausible enough to stop at. Two of the four reached this comment
// and were removed from it later.
//
// That is the argument for this command, and it is stronger than any single
// cause would have been. A run that names its own tree ends the reconstruction
// before it starts.
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
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

func main() {
	// -strict turns the report into a gate: on CI the clone step ends with a
	// reset --hard, so a workspace that already differs from its commit at the
	// first command of the first step has been written into by something else.
	// That is exactly the 255 condition, and it would have been caught before a
	// single check ran rather than argued about for a day.
	//
	// NOT WIRED INTO ANY WORKFLOW, deliberately. Whether every pipeline really
	// starts clean is a property of the runner, and it cannot be measured from
	// a laptop -- turning it on blind would trade an intermittent wrong answer
	// for five pipelines that might refuse to start. Run it once unstrict, read
	// the tree= field across a few pipelines, then add the flag.
	strict := flag.Bool("strict", false, "exit non-zero if the workspace does not match its commit")
	flag.Parse()
	if err := run(os.Stdout, *strict); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(out *os.File, strict bool) error {
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

	if strict && state == "dirty" {
		return fmt.Errorf("workspace provenance: this workspace does not match %s:\n%s\n\n"+
			"    The clone step ends with a reset --hard, so nothing here should differ from\n"+
			"    the commit yet. Something else wrote into this directory, and every verdict\n"+
			"    below would describe files that are not this commit's.",
			commit, strings.TrimRight(status, "\n"))
	}
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
