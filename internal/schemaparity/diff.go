package schemaparity

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// Difference is one leaf on which two canonical schemas disagree.
//
// Surface, Attribute and Field are the ledger's coordinates, derived from the
// JSON path so that a difference and a declaration can be compared without a
// human translating between them. Path is kept because the derivation can fail
// -- a difference outside the resource/attribute shape has no ledger
// coordinates, and reporting the raw path is better than dropping it.
type Difference struct {
	Path      string
	Surface   string
	Attribute string
	Field     string
	Old       string
	New       string
}

func (d Difference) String() string {
	if d.Surface == "" {
		return fmt.Sprintf("%s: %s -> %s", d.Path, d.Old, d.New)
	}
	return fmt.Sprintf("%s.%s %s: %s -> %s", d.Surface, d.Attribute, d.Field, d.Old, d.New)
}

// Diff walks two canonical schema documents and returns every differing leaf.
//
// It reports ALL of them. That is the entire reason this replaces a byte
// comparison: `cmp` stops at the first difference, so it can say where
// divergence begins and nothing about how much follows. "Is that one attribute
// or forty" was unanswerable from the old gate's output, and the answer turned
// out to be six.
//
// Absence is a difference. A key present on one side and missing on the other
// is reported as a transition to or from the empty string, which is how the
// canonical form encodes a false boolean -- it omits rather than emits it.
// Reconciling that with the ledger's spelling of "false" happens in sameValue,
// not here. This comment previously said the two "must compare equal" as though
// stating the requirement satisfied it.
func Diff(released, candidate any) []Difference {
	var out []Difference
	walk(released, candidate, "", &out)
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

func walk(a, b any, path string, out *[]Difference) {
	switch left := a.(type) {
	case map[string]any:
		right, ok := b.(map[string]any)
		if !ok {
			*out = append(*out, difference(path, a, b))
			return
		}
		for _, key := range unionKeys(left, right) {
			lv, lok := left[key]
			rv, rok := right[key]
			switch {
			case !lok:
				*out = append(*out, difference(path+"/"+key, nil, rv))
			case !rok:
				*out = append(*out, difference(path+"/"+key, lv, nil))
			default:
				walk(lv, rv, path+"/"+key, out)
			}
		}
	case []any:
		right, ok := b.([]any)
		if !ok || len(left) != len(right) {
			*out = append(*out, difference(path, a, b))
			return
		}
		for i := range left {
			walk(left[i], right[i], fmt.Sprintf("%s[%d]", path, i), out)
		}
	default:
		if !scalarEqual(a, b) {
			*out = append(*out, difference(path, a, b))
		}
	}
}

func unionKeys(a, b map[string]any) []string {
	seen := make(map[string]struct{}, len(a)+len(b))
	keys := make([]string, 0, len(a)+len(b))
	for k := range a {
		seen[k] = struct{}{}
		keys = append(keys, k)
	}
	for k := range b {
		if _, ok := seen[k]; !ok {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	return keys
}

func scalarEqual(a, b any) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a == b
}

func difference(path string, old, new any) Difference {
	surface, attribute, field := coordinates(path)
	return Difference{
		Path: path, Surface: surface, Attribute: attribute, Field: field,
		Old: scalar(old), New: scalar(new),
	}
}

// scalar renders an observed value. A missing key becomes the empty string.
//
// It does NOT reconcile that with the ledger's spelling -- see sameValue, which
// does, and which this comment used to claim was unnecessary. The first version
// of this file asserted the reconciliation happened here and it did not, so
// every Computed flip in the real ledger failed to match. That went unnoticed
// because the unit fixtures were written with `old: ""`, agreeing with the
// implementation instead of with the artifact. Running the gate against
// verify-main found it in one pass.
func scalar(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case bool:
		return fmt.Sprintf("%t", t)
	default:
		raw, err := json.Marshal(t)
		if err != nil {
			return fmt.Sprintf("%v", t)
		}
		return string(raw)
	}
}

// coordinates translates a canonical JSON path into the ledger's vocabulary.
//
// The attribute is the dotted path a practitioner would write in their
// configuration, so a declaration reads the way the thing being declared is
// spelled: dhcp_v6_server.start, not "the start field of the dhcp_v6_server
// block". The final segment is the field that moved.
//
// Four containers nest attributes, and all four have to be walked or the
// difference cannot be declared at all:
//
//	block/attributes/A                     a plain attribute
//	.../nested_type/attributes/A           a nested object attribute, to any depth
//	.../block_types/B/block/attributes/A   the older block syntax, still in the schema
//	provider/block/attributes/A            the provider's own configuration
//
// MEASURED BEFORE GENERALISING, over the committed contract's 6318 leaf paths.
// The first version handled only the first two shapes at one level of nesting
// and resolved 5151, leaving 1167 undeclarable: 448 under block_types, 184
// nested two deep, 78 nested_type metadata, 35 on the provider, and the rest
// block-level. That is not a cosmetic gap. A difference with no coordinates
// cannot be matched to a ledger entry, so any real change in those 1167 places
// would have been reported and then been impossible to declare -- a gate with
// no way to go green, which is worse than one that cannot go red because it
// stops the release rather than passing it.
//
// After generalising, 5851 of the 6318 resolve. The remaining 467 are
// CONTAINER-level, not attribute-level: nesting_mode (147), description_kind
// (97), version (93), description (72), type (27), required_for_import (26)
// and a handful of deprecation fields. They return empty coordinates and are
// reported by Path alone.
//
// That residue is a real limit and not a rounding error. A surface's `version`
// is in it, and a schema version bump is exactly what a state upgrader moves.
// The ledger's vocabulary is surface plus attribute plus field, so a
// container-level change can be REPORTED but cannot be DECLARED, and the gate
// would stay red until the ledger format grows a way to name it. Inventing
// coordinates here instead would be worse: it would let an entry naming an
// attribute license a change to its container.
func coordinates(path string) (surface, attribute, field string) {
	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	if len(parts) < 4 {
		return "", "", ""
	}

	var rest []string
	switch {
	case strings.HasSuffix(parts[0], "_schemas") && len(parts) >= 6 && parts[2] == "block":
		surface, rest = parts[1], parts[3:]
	case parts[0] == "provider" && parts[1] == "block":
		surface, rest = "provider", parts[2:]
	default:
		return "", "", ""
	}

	// Walk container/name pairs, collecting the names that a configuration
	// would write, until the remainder is a single attribute and its field.
	var names []string
	for len(rest) >= 2 {
		switch rest[0] {
		case "attributes":
			if len(rest) == 3 { // attributes/<name>/<field>
				return surface, strings.Join(append(names, rest[1]), "."), rest[2]
			}
			names = append(names, rest[1])
			rest = rest[2:]
		case "nested_type":
			rest = rest[1:] // the next segment is "attributes"
		case "block_types":
			if len(rest) < 4 || rest[2] != "block" {
				return "", "", ""
			}
			names = append(names, rest[1])
			rest = rest[3:]
		default:
			return "", "", ""
		}
	}
	return "", "", ""
}

// Classify splits differences into those the ledger licenses and those it does
// not, and reports declarations that matched nothing.
//
// The third return value is what makes this set equality rather than a subset
// check. A ledger entry that matches no observed difference is a defect in the
// other direction: it says a change was made that was not, so either the change
// was reverted and the entry outlived it, or the entry describes a transition
// nobody produced. Both are worth failing on, and a gate that only asked
// "is every difference declared" would report success on either.
func Classify(diffs []Difference, ledger Ledger) (declared, undeclared []Difference, unmatched []Entry) {
	used := make(map[int]bool, len(ledger.Entries))
	for _, d := range diffs {
		matched := false
		for i, e := range ledger.Entries {
			if e.Surface == d.Surface && e.Attribute == d.Attribute && e.Field == d.Field &&
				sameValue(d.Old, e.Old) && sameValue(d.New, e.New) {
				used[i] = true
				matched = true
				break
			}
		}
		if matched {
			declared = append(declared, d)
		} else {
			undeclared = append(undeclared, d)
		}
	}
	for i, e := range ledger.Entries {
		if !used[i] {
			unmatched = append(unmatched, e)
		}
	}
	return declared, undeclared, unmatched
}

// sameValue compares an observed value against a value written in the ledger.
//
// The canonical schema NEVER EMITS false. Measured over the committed contract:
// 1924 occurrences of `:true`, zero of `:false`. A boolean field is present and
// true, or absent and thereby false -- there is no third state, and that is a
// property of the format rather than a convention worth arguing with.
//
// So a ledger entry authored by hand as `"old": "false"` describes exactly what
// the schema spells as an absent key, which Diff renders as "". Collapsing the
// two is unambiguous BECAUSE false is never emitted: no observed value can be
// the string "false", so nothing else can be confused with it.
//
// Written after the gate reported five undeclared differences and five
// unmatched declarations on verify-main -- the same five, seen from both sides,
// which is what set equality looks like when the two spellings never meet.
func sameValue(observed, declared string) bool {
	if declared == "false" {
		declared = ""
	}
	return observed == declared
}
