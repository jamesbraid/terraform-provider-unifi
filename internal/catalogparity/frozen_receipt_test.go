package catalogparity

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

// build/migration-baseline holds what the SHELL pipeline emitted on pipeline
// 232, frozen because its producers are being deleted and CI logs rotate.
//
// The acceptance criterion for the rewrite is that the Go implementation
// reproduces those receipts. That comparison needs both implementations running
// on one tree, which is a CI-shaped job. This test is the half of it that can
// run on every push, and it is the half that fails first: whether the Go TYPES
// can carry what the shell wrote at all.
//
// A field the shell emits and no Go field receives is evidence silently
// dropped by the port. A field a Go type emits that the shell never wrote is a
// claim the rewrite invented. Both show up here, years before anyone diffs two
// live runs.
//
// THE COMPARISON IS CANONICAL, NOT `cmp`, AND THAT IS A FINDING RATHER THAN A
// CONVENIENCE. The frozen files are not the shell's bytes. jq builds an object
// literal in insertion order and terminates it with a newline -- measured, not
// assumed: `jq -n '{zulu:1,alpha:2}'` prints zulu first, and every jq output
// ends in \n. All four frozen files are sorted at every level and end at the
// closing brace. So they were normalised on the way out of the step logs, and
// no implementation -- including the shell that produced them -- can match them
// byte for byte. Anything claiming to be an exact diff against these files has
// to say what it normalises first.
func TestTheFrozenShellReceiptsFitTheirGoTypes(t *testing.T) {
	for _, c := range frozenReceiptCases() {
		t.Run(c.file, func(t *testing.T) {
			raw := readFrozenReceipt(t, c.file)

			target := c.newReceipt()
			decoder := json.NewDecoder(bytes.NewReader(raw))
			decoder.DisallowUnknownFields()
			if err := decoder.Decode(target); err != nil {
				t.Fatalf("%s does not fit %T: %v\n"+
					"The shell wrote a field this type cannot hold. Porting the script without "+
					"adding it would drop that evidence from every receipt, and nothing downstream "+
					"would report an absence.", c.file, target, err)
			}

			rendered, err := MarshalReceipt(target)
			if err != nil {
				t.Fatalf("render %s: %v", c.file, err)
			}
			want := canonicaliseFrozen(t, raw)
			if !bytes.Equal(rendered, want) {
				t.Fatalf("%s does not survive the round trip through %T.\n"+
					"A key here is one the type adds or drops.\nfrozen:\n%s\nrendered:\n%s",
					c.file, target, want, rendered)
			}
		})
	}
}

// TestDroppingAnyReceiptKeyIsVisible is the control for the test above, and it
// runs once per top-level key rather than once per file, because a comparison
// that only ever sees matching inputs proves nothing about what it would reject.
//
// It also names the exemption instead of tolerating it. A field marked
// omitempty round-trips absent-to-absent, so its loss is invisible here by
// construction. tree_state is the one field that is deliberately that way -- a
// receipt without a measured tree state must not gain an empty one, which would
// read as "clean" to anyone scanning for the key. The assertion is set equality
// against the expected exemptions, so adding omitempty to a second field fails
// this test rather than quietly widening the blind spot.
func TestDroppingAnyReceiptKeyIsVisible(t *testing.T) {
	for _, c := range frozenReceiptCases() {
		t.Run(c.file, func(t *testing.T) {
			raw := readFrozenReceipt(t, c.file)
			var document map[string]json.RawMessage
			if err := json.Unmarshal(raw, &document); err != nil {
				t.Fatalf("read %s as an object: %v", c.file, err)
			}
			if len(document) == 0 {
				t.Fatalf("%s has no keys, so this control checks nothing", c.file)
			}

			var invisible []string
			for key := range document {
				short := make(map[string]json.RawMessage, len(document)-1)
				for k, v := range document {
					if k != key {
						short[k] = v
					}
				}
				shortened, err := json.Marshal(short)
				if err != nil {
					t.Fatalf("re-render %s without %q: %v", c.file, key, err)
				}
				target := c.newReceipt()
				if err := json.Unmarshal(shortened, target); err != nil {
					t.Fatalf("decode %s without %q: %v", c.file, key, err)
				}
				rendered, err := MarshalReceipt(target)
				if err != nil {
					t.Fatalf("render %s without %q: %v", c.file, key, err)
				}
				if bytes.Equal(rendered, canonicaliseFrozen(t, shortened)) {
					invisible = append(invisible, key)
				}
			}
			sort.Strings(invisible)

			want := append([]string(nil), c.omitEmptyKeys...)
			sort.Strings(want)
			if len(invisible) != len(want) {
				t.Fatalf("%s: keys whose loss the round trip cannot see = %v, expected exactly %v.\n"+
					"An unexpected name here is a field marked omitempty, which makes its absence "+
					"indistinguishable from its zero value. A missing name means an exemption was "+
					"removed and this list was not updated.", c.file, invisible, want)
			}
			for i := range want {
				if invisible[i] != want[i] {
					t.Fatalf("%s: invisible keys %v, expected %v", c.file, invisible, want)
				}
			}
		})
	}
}

type frozenReceiptCase struct {
	file          string
	newReceipt    func() any
	omitEmptyKeys []string
}

func frozenReceiptCases() []frozenReceiptCase {
	return []frozenReceiptCase{
		{
			file:          "catalog-build-schema.json",
			newReceipt:    func() any { return &BuildSchemaReceipt{} },
			omitEmptyKeys: []string{"tree_state"},
		},
		{
			file:          "catalog-unit-differential.json",
			newReceipt:    func() any { return &UnitDifferentialReceipt{} },
			omitEmptyKeys: []string{"tree_state"},
		},
		{
			file:          "catalog-controller-differential.json",
			newReceipt:    func() any { return &ControllerDifferentialReceipt{} },
			omitEmptyKeys: []string{"tree_state"},
		},
	}
}

func readFrozenReceipt(t *testing.T, name string) []byte {
	t.Helper()
	path := filepath.Join("..", "..", "build", "migration-baseline", name)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the frozen shell receipt %s: %v\n"+
			"These files are the only surviving record of what the shell emitted. If they are "+
			"gone before the Go output was compared against them, the comparison cannot be made "+
			"later -- the producers are deleted and the CI logs have rotated.", path, err)
	}
	return raw
}

// canonicaliseFrozen puts the frozen bytes through the same renderer as the Go
// receipt, so the comparison is about content and not about who wrote the file.
// Without it the diff would be dominated by key order and a trailing newline,
// neither of which any consumer reads.
func canonicaliseFrozen(t *testing.T, raw []byte) []byte {
	t.Helper()
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var generic any
	if err := decoder.Decode(&generic); err != nil {
		t.Fatalf("decode frozen receipt: %v", err)
	}
	rendered, err := MarshalReceipt(generic)
	if err != nil {
		t.Fatalf("render frozen receipt: %v", err)
	}
	return rendered
}
