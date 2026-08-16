package catalogparity

import (
	"strings"
	"testing"
)

// TestParseTreeStateRefusesWhatItCannotVouchFor is the guard against the shape
// we keep finding: a field whose value no production path can vary.
//
// If a missing -tree-state defaulted to something -- "unknown", or a zero
// struct -- every receipt would carry a tree_state that no measurement
// produced, and the gate reading it would compare a constant against itself.
// That is the AttemptResult defect rebuilt deliberately. So absence is an
// error, and so is a state that does not say what tree-state.sh says.
func TestParseTreeStateRefusesWhatItCannotVouchFor(t *testing.T) {
	for name, test := range map[string]struct {
		raw  string
		want string
	}{
		"absent": {
			raw:  "",
			want: "required",
		},
		"not JSON": {
			raw:  "clean",
			want: "not valid JSON",
		},
		"unknown member": {
			raw:  `{"status":"clean","commit":"abc","dirty_paths":[],"extra":1}`,
			want: "unknown field",
		},
		"status the guard never emits": {
			raw:  `{"status":"unknown","commit":"abc","dirty_paths":[]}`,
			want: `status "unknown"`,
		},
		"no commit": {
			raw:  `{"status":"clean","commit":"","dirty_paths":[]}`,
			want: "names no commit",
		},
		"clean but carrying dirt": {
			raw:  `{"status":"clean","commit":"abc","dirty_paths":[" M a.go"]}`,
			want: "clean tree with 1 dirty path",
		},
		"dirty but carrying none": {
			raw:  `{"status":"dirty","commit":"abc","dirty_paths":[]}`,
			want: "dirty tree with no dirty paths",
		},
	} {
		t.Run(name, func(t *testing.T) {
			state, err := ParseTreeState(test.raw)
			if err == nil {
				t.Fatalf("ParseTreeState(%q) = %#v, want an error", test.raw, state)
			}
			if !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want it to mention %q", err, test.want)
			}
		})
	}
}

// TestParseTreeStateAcceptsWhatTheGuardEmits is the control. Without it the
// cases above would pass against a parser that rejected everything.
func TestParseTreeStateAcceptsWhatTheGuardEmits(t *testing.T) {
	clean, err := ParseTreeState(`{"status":"clean","commit":"1a2b3c","dirty_paths":[]}`)
	if err != nil {
		t.Fatalf("clean: %v", err)
	}
	if clean.Status != "clean" || clean.Commit != "1a2b3c" || len(clean.DirtyPaths) != 0 {
		t.Fatalf("clean = %#v", clean)
	}

	dirty, err := ParseTreeState(`{"status":"dirty","commit":"1a2b3c","dirty_paths":[" M a.go","?? b.json"]}`)
	if err != nil {
		t.Fatalf("dirty: %v", err)
	}
	if dirty.Status != "dirty" || len(dirty.DirtyPaths) != 2 {
		t.Fatalf("dirty = %#v", dirty)
	}
}
