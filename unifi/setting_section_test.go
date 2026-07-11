package unifi

import (
	"context"
	"fmt"
	"sort"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
)

// fakeSection is a minimal settingSection used only to exercise
// orderedSections' sort behavior. Every method besides key() is a trivial
// stub returning a zero value, since TestOrderedSectionsDeterministic only
// cares about ordering.
type fakeSection struct {
	k string
}

func (f fakeSection) key() string      { return f.k }
func (f fakeSection) attrName() string { return f.k }
func (f fakeSection) schemaAttribute() schema.Attribute {
	return schema.BoolAttribute{}
}
func (f fakeSection) ownership() map[string]ownershipClass { return nil }
func (f fakeSection) decode(ctx context.Context, snap rawSettings, prior settingResourceModel, model *settingResourceModel) diag.Diagnostics {
	return nil
}
func (f fakeSection) overlay(ctx context.Context, model, prior settingResourceModel, snap rawSettings) (settings.RawSetting, bool, diag.Diagnostics) {
	return settings.RawSetting{}, false, nil
}
func (f fakeSection) capability(snap rawSettings) capabilityState { return capUnknown }
func (f fakeSection) carryBestEffort(dst *settingResourceModel, plan, prior settingResourceModel) diag.Diagnostics {
	return nil
}

func TestOrderedSectionsDeterministic(t *testing.T) {
	in := []settingSection{
		fakeSection{k: "c"},
		fakeSection{k: "a"},
		fakeSection{k: "b"},
	}
	inSnapshot := append([]settingSection(nil), in...)

	got := orderedSections(in)

	wantKeys := []string{"a", "b", "c"}
	if len(got) != len(wantKeys) {
		t.Fatalf("orderedSections returned %d sections, want %d", len(got), len(wantKeys))
	}
	for i, want := range wantKeys {
		if got[i].key() != want {
			t.Errorf("orderedSections()[%d].key() = %q, want %q", i, got[i].key(), want)
		}
	}

	// The input slice must be unchanged (orderedSections must copy, not
	// sort in place).
	for i, want := range []string{"c", "a", "b"} {
		if in[i].key() != want {
			t.Errorf("input slice mutated: in[%d].key() = %q, want %q (orderedSections must not sort in place)", i, in[i].key(), want)
		}
	}
	if len(in) != len(inSnapshot) {
		t.Fatalf("input slice length changed: got %d, want %d", len(in), len(inSnapshot))
	}

	// Calling it again must yield the same order (determinism).
	got2 := orderedSections(in)
	for i := range wantKeys {
		if got2[i].key() != got[i].key() {
			t.Errorf("second call diverged at [%d]: got %q, want %q (must be deterministic)", i, got2[i].key(), got[i].key())
		}
	}
}

// TestRegistryKeysUnique is a structural guard over the real settingSections
// registry: key() and attrName() must be unique and non-empty across all
// registered sections. This task registers no real sections, so it passes
// trivially over the empty registry; it gains teeth as Tasks 9-22 register
// sections and must keep passing throughout.
func TestRegistryKeysUnique(t *testing.T) {
	seenKeys := make(map[string]bool, len(settingSections))
	seenAttrs := make(map[string]bool, len(settingSections))

	for _, s := range settingSections {
		k := s.key()
		if k == "" {
			t.Errorf("section %T has empty key()", s)
		}
		if seenKeys[k] {
			t.Errorf("duplicate key() %q", k)
		}
		seenKeys[k] = true

		a := s.attrName()
		if a == "" {
			t.Errorf("section %T has empty attrName()", s)
		}
		if seenAttrs[a] {
			t.Errorf("duplicate attrName() %q", a)
		}
		seenAttrs[a] = true
	}
}

// leafPaths walks a schema.Attribute tree and returns the set of leaf
// attribute paths it contains. A leaf is any attribute that is not
// SingleNestedAttribute or ListNestedAttribute. Leaf-path convention: a
// top-level leaf's path is its own attribute name; a leaf nested under a
// SingleNestedAttribute or inside a ListNestedAttribute's NestedObject is
// "parent.child" (dot-joined from the root). This matches how ownership()
// is documented in the settingSection interface ("attr path -> class; every
// schema leaf present").
//
// Only the attribute kinds PR-A actually uses are handled: Bool, String,
// Int64, List (leaf), SingleNested, and ListNested. Any other kind is
// unhandled and fails loudly (via panic) rather than being silently
// skipped, since a silent skip would let a real section slip an uncovered
// leaf past gate 10.
func leafPaths(prefix string, attrs map[string]schema.Attribute) map[string]bool {
	out := make(map[string]bool)
	for name, a := range attrs {
		path := name
		if prefix != "" {
			path = prefix + "." + name
		}
		switch v := a.(type) {
		case schema.SingleNestedAttribute:
			for p := range leafPaths(path, v.Attributes) {
				out[p] = true
			}
		case schema.ListNestedAttribute:
			for p := range leafPaths(path, v.NestedObject.Attributes) {
				out[p] = true
			}
		case schema.BoolAttribute, schema.StringAttribute, schema.Int64Attribute, schema.ListAttribute:
			out[path] = true
		default:
			panic(fmt.Sprintf("leafPaths: unhandled schema.Attribute kind %T at path %q", a, path))
		}
	}
	return out
}

// ownershipCoverageMismatches compares the leaf paths of sec's
// schemaAttribute() against the key set of sec's ownership() map and
// returns a human-readable mismatch description for every leaf path present
// in exactly one of the two. An empty result means full coverage in both
// directions.
func ownershipCoverageMismatches(sec settingSection) []string {
	root, ok := sec.schemaAttribute().(schema.SingleNestedAttribute)
	if !ok {
		panic(fmt.Sprintf("ownershipCoverageMismatches: section %q schemaAttribute() is %T, want schema.SingleNestedAttribute", sec.key(), sec.schemaAttribute()))
	}
	schemaLeaves := leafPaths("", root.Attributes)
	ownershipLeaves := sec.ownership()

	var mismatches []string
	for p := range schemaLeaves {
		if _, present := ownershipLeaves[p]; !present {
			mismatches = append(mismatches, fmt.Sprintf("section %q: schema leaf %q missing from ownership()", sec.key(), p))
		}
	}
	for p := range ownershipLeaves {
		if !schemaLeaves[p] {
			mismatches = append(mismatches, fmt.Sprintf("section %q: ownership() key %q is not a schema leaf", sec.key(), p))
		}
	}
	sort.Strings(mismatches)
	return mismatches
}

// checkOwnershipCoversSchema asserts (via t.Errorf, one per mismatch) that
// sec's schemaAttribute() leaves and ownership() keys are identical sets.
func checkOwnershipCoversSchema(t *testing.T, sec settingSection) {
	t.Helper()
	for _, m := range ownershipCoverageMismatches(sec) {
		t.Errorf("%s", m)
	}
}

// TestSectionOwnershipCoversSchema is the gate-10 structural check: every
// leaf attribute path in a section's schemaAttribute() must appear in its
// ownership() map, and vice-versa.
//
// This is exercised two ways:
//  1. Over the real settingSections registry (empty at this task, so
//     trivially passes; gains teeth as Tasks 9-22 register sections).
//  2. Against inline fake sections with a real nested schema, so the
//     schema-tree-walking logic itself is proven correct now rather than
//     left dead until Task 9. One fake has matching ownership (must pass);
//     one fake omits a leaf from ownership (must be detected/fail).
func TestSectionOwnershipCoversSchema(t *testing.T) {
	t.Run("real registry", func(t *testing.T) {
		for _, s := range settingSections {
			checkOwnershipCoversSchema(t, s)
		}
	})

	t.Run("fake with complete ownership", func(t *testing.T) {
		sec := coverageFakeSection{
			k: "fake_ok",
			schema: schema.SingleNestedAttribute{
				Attributes: map[string]schema.Attribute{
					"enabled": schema.BoolAttribute{},
					"name":    schema.StringAttribute{},
				},
			},
			own: map[string]ownershipClass{
				"enabled": ownerManaged,
				"name":    ownerManaged,
			},
		}

		// Exercise the t.Errorf-driven path directly: with complete
		// ownership coverage it must report zero errors.
		checkOwnershipCoversSchema(t, sec)

		// And independently assert the pure comparison found nothing
		// wrong, proving the walker itself (not just the absence of a
		// t.Errorf call) saw full coverage.
		if got := ownershipCoverageMismatches(sec); len(got) != 0 {
			t.Errorf("ownershipCoverageMismatches() = %v, want none", got)
		}
	})

	t.Run("fake with omitted leaf is detected", func(t *testing.T) {
		sec := coverageFakeSection{
			k: "fake_missing",
			schema: schema.SingleNestedAttribute{
				Attributes: map[string]schema.Attribute{
					"enabled": schema.BoolAttribute{},
					"name":    schema.StringAttribute{},
				},
			},
			own: map[string]ownershipClass{
				// "name" is deliberately omitted.
				"enabled": ownerManaged,
			},
		}

		// Call the pure comparison directly (not the t.Errorf-driven
		// wrapper) so the deliberate mismatch we're proving the walker
		// catches doesn't fail this test itself.
		got := ownershipCoverageMismatches(sec)
		want := []string{`section "fake_missing": schema leaf "name" missing from ownership()`}
		if len(got) != len(want) || (len(got) > 0 && got[0] != want[0]) {
			t.Errorf("ownershipCoverageMismatches() = %v, want %v", got, want)
		}
	})
}

// coverageFakeSection is an inline fake settingSection used only to exercise
// the ownership/schema coverage walker in TestSectionOwnershipCoversSchema.
// All methods besides key(), schemaAttribute(), and ownership() are trivial
// stubs.
type coverageFakeSection struct {
	k      string
	schema schema.Attribute
	own    map[string]ownershipClass
}

func (f coverageFakeSection) key() string                       { return f.k }
func (f coverageFakeSection) attrName() string                  { return f.k }
func (f coverageFakeSection) schemaAttribute() schema.Attribute { return f.schema }
func (f coverageFakeSection) ownership() map[string]ownershipClass {
	return f.own
}
func (f coverageFakeSection) decode(ctx context.Context, snap rawSettings, prior settingResourceModel, model *settingResourceModel) diag.Diagnostics {
	return nil
}
func (f coverageFakeSection) overlay(ctx context.Context, model, prior settingResourceModel, snap rawSettings) (settings.RawSetting, bool, diag.Diagnostics) {
	return settings.RawSetting{}, false, nil
}
func (f coverageFakeSection) capability(snap rawSettings) capabilityState { return capUnknown }
func (f coverageFakeSection) carryBestEffort(dst *settingResourceModel, plan, prior settingResourceModel) diag.Diagnostics {
	return nil
}
