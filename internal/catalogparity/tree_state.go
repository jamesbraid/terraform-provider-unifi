package catalogparity

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
)

// ParseTreeState reads the JSON that cmd/tree-state renders
// and refuses anything it cannot vouch for.
//
// THE MEASUREMENT IS NOT HERE AND MUST NOT MOVE HERE. What counts as dirty, and
// what a dirty-but-acknowledged run is permitted to do, is decided in one place
// -- the shell library -- because it has to run before anything it protects,
// including before a Go toolchain is known to work. A second implementation in
// Go would be one rule with two homes that can disagree, which is the drift we
// are cataloguing elsewhere rather than a fix for it. This validates a value
// someone else measured; it never measures.
//
// ABSENCE IS AN ERROR, deliberately, and there is no default. A missing state
// that fell back to "unknown" or to a zero struct would put a value in every
// receipt that no measurement produced, and a gate reading it would compare a
// constant against itself -- exactly the defect where one producer writes a
// literal and two consumers check it. Refusing keeps the field meaning
// something.
//
// The internal-consistency checks are cheap and they are the ones that catch a
// hand-edited or half-built value: a clean tree cannot carry dirty paths, and a
// dirty one cannot carry none, because cmd/tree-state sets them together.
func ParseTreeState(raw string) (*TreeState, error) {
	if raw == "" {
		return nil, fmt.Errorf("tree state is required: pass the JSON from evidence_tree_json, " +
			"generated in the same step so it describes the tree at the moment this receipt is written")
	}

	var state TreeState
	decoder := json.NewDecoder(bytes.NewReader([]byte(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&state); err != nil {
		return nil, fmt.Errorf("tree state is not valid JSON from evidence_tree_json: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return nil, fmt.Errorf("tree state carries multiple JSON values")
		}
		return nil, fmt.Errorf("tree state: %w", err)
	}

	switch state.Status {
	case "clean", "dirty":
	default:
		return nil, fmt.Errorf("tree state has status %q; cmd/tree-state emits only clean or dirty", state.Status)
	}
	if state.Commit == "" {
		return nil, fmt.Errorf("tree state names no commit, so the receipt cannot say which tree it describes")
	}
	if state.Status == "clean" && len(state.DirtyPaths) != 0 {
		return nil, fmt.Errorf("tree state reports a clean tree with %d dirty path(s)", len(state.DirtyPaths))
	}
	if state.Status == "dirty" && len(state.DirtyPaths) == 0 {
		return nil, fmt.Errorf("tree state reports a dirty tree with no dirty paths, so it cannot say what differed")
	}
	return &state, nil
}
