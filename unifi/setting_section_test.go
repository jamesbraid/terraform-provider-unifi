package unifi

import (
	"context"
	"reflect"
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
func (f fakeSection) decode(ctx context.Context, snap rawSettings, prior settingResourceModel, model *settingResourceModel) diag.Diagnostics {
	return nil
}
func (f fakeSection) overlay(ctx context.Context, model, prior settingResourceModel, snap rawSettings) (settings.RawSetting, bool, diag.Diagnostics) {
	return settings.RawSetting{}, false, nil
}
func (f fakeSection) carryBestEffort(dst *settingResourceModel, plan settingResourceModel) diag.Diagnostics {
	return nil
}
func (f fakeSection) isConfigured(m settingResourceModel) bool { return false }

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

// modelHasField reports whether settingResourceModel has a field whose
// `tfsdk` struct tag equals name. Used by TestSectionStructuralCoverage to
// verify each registered section's attrName() actually has a backing field
// on the resource model, via reflection rather than a hardcoded name list.
func modelHasField(name string) bool {
	t := reflect.TypeOf(settingResourceModel{})
	for i := 0; i < t.NumField(); i++ {
		if t.Field(i).Tag.Get("tfsdk") == name {
			return true
		}
	}
	return false
}

// TestSectionStructuralCoverage asserts each registered section is wired:
// unique key, a schema attribute, and a matching model field. It does NOT
// prove decode/overlay behavior — the per-section golden and lifecycle
// tests do that.
func TestSectionStructuralCoverage(t *testing.T) {
	seen := map[string]bool{}
	for _, s := range settingSections {
		if seen[s.key()] {
			t.Errorf("duplicate section key %q", s.key())
		}
		seen[s.key()] = true
		if s.schemaAttribute() == nil {
			t.Errorf("section %q has no schema attribute", s.key())
		}
		if !modelHasField(s.attrName()) {
			t.Errorf("section %q attrName %q has no settingResourceModel field", s.key(), s.attrName())
		}
	}
}
