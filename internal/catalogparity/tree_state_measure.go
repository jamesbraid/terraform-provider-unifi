package catalogparity

import (
	"encoding/json"
	"fmt"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/cmdio"
	"sort"
	"strings"
)

// MeasureTreeState answers one question -- does this evidence describe a commit
// that exists -- and it replaces .woodpecker/scripts/tree-state.sh.
//
// WHY THE GUARD EXISTS. An evidence artifact generated from a dirty working tree
// pins content that is in no commit. It is honest about what it saw and wrong
// about what exists, and it heals silently once the files land, so the window
// where it was wrong leaves no trace. catalog-evidence-inventory.json was
// produced that way once: it digested files that had not been committed, and the
// mismatch was diagnosed three times before the cause was found, because the
// artifact looked internally consistent.
//
// THE THREE OUTCOMES, and the third is the point:
//
//	clean tree               record the commit, proceed
//	dirty, not acknowledged  REFUSE, and name the files
//	dirty, acknowledged      proceed, and RECORD the dirt in the artifact
//
// Forbidding a dirty run outright would be worse than useless. Somebody
// iterating locally has a real reason to generate evidence against uncommitted
// work, and a guard that blocks it is one they work around by editing the
// artifact afterwards -- at which point nothing records anything. So the
// artifact is made unable to claim a commit it does not describe, whichever way
// the run went.
//
// ROOT RESOLUTION IS FROM A CALLER-SUPPLIED DIRECTORY, NEVER THE PROCESS CWD.
// The shell version resolved from BASH_SOURCE for a measured reason: it had
// resolved from the working directory, which agreed with every caller except the
// one that cd's first, and that single disagreement took down the controller
// campaign on its first command for seven manual pipelines, with a message that
// reads like a broken checkout. In Go the caller passes the directory, so the
// ambiguity cannot arise -- and a test can pass a temporary one, which the shell
// version could not.
func MeasureTreeState(dir, what string, allowDirty bool) (*TreeState, error) {
	if what == "" {
		return nil, fmt.Errorf("tree state needs the name of the artifact it is guarding")
	}

	root, err := cmdio.GitLines(dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return nil, fmt.Errorf("%s: not a git repository, so the tree state cannot be recorded", what)
	}
	commit, err := cmdio.GitLines(root, "rev-parse", "HEAD")
	if err != nil {
		return nil, fmt.Errorf("%s: HEAD cannot be resolved, so the evidence cannot name its commit", what)
	}

	// UNTRACKED FILES COUNT. The inventory failure was untracked evidence files
	// being digested before they were added, so excluding them would leave the
	// exact hole this exists to close.
	porcelain, err := cmdio.GitLines(root, "status", "--porcelain")
	if err != nil {
		return nil, fmt.Errorf("%s: the working tree state cannot be read: %w", what, err)
	}

	dirty := dirtyPaths(porcelain)
	if len(dirty) == 0 {
		return &TreeState{Status: "clean", Commit: commit, DirtyPaths: []string{}}, nil
	}

	if !allowDirty {
		return nil, fmt.Errorf(
			"%s: refusing to generate evidence from a dirty tree.\n\n%s\n\n"+
				"    The artifact would pin content that is in no commit -- honest about\n"+
				"    what it saw and wrong about what exists, and it heals silently once\n"+
				"    the files land, so the window where it was wrong leaves no trace.\n\n"+
				"    Commit the changes, or set EVIDENCE_ALLOW_DIRTY_TREE=1 to proceed --\n"+
				"    which records the dirty state IN the artifact rather than hiding it.",
			what, strings.Join(dirty, "\n"))
	}
	return &TreeState{Status: "dirty", Commit: commit, DirtyPaths: dirty}, nil
}

// dirtyPaths splits porcelain output into entries, sorted so two runs over the
// same tree produce byte-identical evidence.
//
// git already sorts its porcelain output, so this is belt and braces rather than
// a correction -- but the artifact is digested and compared, and a receipt whose
// bytes depend on git's iteration order is one that reproduces by luck.
func dirtyPaths(porcelain string) []string {
	var paths []string
	for _, line := range strings.Split(porcelain, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		paths = append(paths, line)
	}
	sort.Strings(paths)
	return paths
}

// JSON renders the state for embedding in a receipt, replacing the shell's
// evidence_tree_json.
//
// One object rather than three fields, so a consumer finds the whole claim in
// one place and adding a field later does not mean editing every generator --
// which is the shape the shell version chose for the same reason.
//
// The shell built this with two piped jq invocations and an array-length branch,
// because it had to distinguish an empty list from an unset variable. Go has no
// such ambiguity: DirtyPaths is a slice, and MeasureTreeState guarantees it is
// non-nil so the field marshals as [] rather than null. ParseTreeState uses
// DisallowUnknownFields, so a null there would be a decode error in the
// consumer, discovered only inside a pipeline.
func (t TreeState) JSON() (string, error) {
	if t.DirtyPaths == nil {
		t.DirtyPaths = []string{}
	}
	encoded, err := json.Marshal(t)
	if err != nil {
		return "", fmt.Errorf("rendering tree state: %w", err)
	}
	return string(encoded), nil
}
