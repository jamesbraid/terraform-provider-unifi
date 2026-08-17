// Command tree-state guards an evidence artifact against being generated from a
// dirty working tree, and prints the tree state for embedding in the receipt.
//
// It replaces .woodpecker/scripts/tree-state.sh, which was 170 lines with 653
// more testing it. The whole contract is: refuse a dirty run unless it is
// acknowledged, and when it is, record the dirt in the artifact rather than
// hiding it.
//
// Usage:
//
//	tree_state=$(go run ./cmd/tree-state -what "the M1 compiler receipt") || exit 1
//
// The JSON goes to stdout and nothing else does, so the caller can capture it
// directly. Refusals go to stderr and exit 1, so a caller under `set -e` stops
// whether or not it reads the message.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/ubiquiti-community/terraform-provider-unifi/internal/catalogparity"
)

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr *os.File) error {
	flags := flag.NewFlagSet("tree-state", flag.ContinueOnError)
	flags.SetOutput(stderr)
	what := flags.String("what", "", "the artifact being guarded, named in any refusal")
	dir := flags.String("dir", ".", "a directory inside the repository to measure")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *what == "" {
		return fmt.Errorf("-what is required: a refusal that does not say what it is guarding " +
			"sends the reader to the wrong file")
	}

	// The environment variable keeps the name the shell used, because it appears
	// in operator runbooks and in the refusal message itself. Any non-empty value
	// acknowledges, matching the shell's [[ -z ]] test rather than parsing a
	// boolean -- EVIDENCE_ALLOW_DIRTY_TREE=0 acknowledged under the old guard and
	// changing that silently would be a trap for whoever set it that way.
	allowDirty := os.Getenv("EVIDENCE_ALLOW_DIRTY_TREE") != ""

	state, err := catalogparity.MeasureTreeState(*dir, *what, allowDirty)
	if err != nil {
		return err
	}
	if state.Status == "dirty" {
		fmt.Fprintf(stderr, "%s: generating from a DIRTY tree, as permitted by EVIDENCE_ALLOW_DIRTY_TREE.\n",
			*what)
		fmt.Fprintf(stderr, "  The artifact will record this. %d path(s) differ from %s.\n",
			len(state.DirtyPaths), state.Commit)
	}

	encoded, err := state.JSON()
	if err != nil {
		return err
	}
	fmt.Fprintln(stdout, encoded)
	return nil
}
