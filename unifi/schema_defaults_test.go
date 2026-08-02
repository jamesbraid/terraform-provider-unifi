package unifi

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	fwresource "github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
)

// goldenOptionalComputedDefaults lists every Optional + Computed attribute that
// also carries a Default.
const goldenOptionalComputedDefaults = "testdata/optional_computed_defaults.txt"

// header explains the file to whoever opens it without the test in front of them.
const header = `# Optional + Computed attributes that also carry a Default.
#
# A default overrides whatever the controller stores, so any attribute here
# plans a change back to its default the moment it is left out of a
# configuration -- and silently changes the device if the controller held
# something else. That is only correct where the provider owns the value and
# the controller stores nothing of its own.
#
# This list is an inventory of what still needs checking against a live
# controller, not a list of approved patterns. Prefer no Default plus
# UseStateForUnknown for anything the controller reports.
#
# Regenerate with: UPDATE_GOLDEN=1 go test ./unifi/ -run Test_schemaOptionalComputedDefaults
`

// Test_schemaOptionalComputedDefaults is a ratchet on a whole class of bug, not
// a check on any one attribute.
//
// Optional + Computed says the controller may own the value when the
// configuration leaves the attribute out. A Default contradicts that: the
// framework fills the default in before the controller is ever consulted, so a
// resource whose stored value differs plans a change back to the default and
// silently turns the setting off. v0.101.0 shipped exactly that on
// unifi_wlan.roaming_assistant_na_enabled — every WLAN with 5GHz roaming
// assistance already on planned it off. #323 was the same fault on
// minimum_data_rate_*_kbps.
//
// The combination is not always wrong. It is correct where the provider owns
// the value and the controller stores nothing of its own. It cannot be decided
// statically, which is why this test pins the existing set rather than banning
// it: anything new has to be justified deliberately, and the list is the
// standing inventory of what still needs auditing against a live controller.
//
// Adding an entry is a decision, not a formality. Before doing so, confirm the
// controller does not return its own value for that field — create the resource
// without the attribute, then check a refresh plans empty. If it does not, drop
// the Default and use UseStateForUnknown instead.
func Test_schemaOptionalComputedDefaults(t *testing.T) {
	got := optionalComputedDefaults(t)

	// UPDATE_GOLDEN=1 rewrites the inventory. Only reach for it after deciding
	// each new entry is correct — the point of the list is that adding to it is
	// a conscious act.
	if os.Getenv("UPDATE_GOLDEN") != "" {
		body := header + strings.Join(got, "\n") + "\n"
		if err := os.WriteFile(goldenOptionalComputedDefaults, []byte(body), 0o644); err != nil {
			t.Fatalf("writing %s: %v", goldenOptionalComputedDefaults, err)
		}
		t.Logf("wrote %d entries to %s", len(got), goldenOptionalComputedDefaults)
		return
	}

	want, err := os.ReadFile(goldenOptionalComputedDefaults)
	if err != nil {
		t.Fatalf("reading %s: %v", goldenOptionalComputedDefaults, err)
	}
	wantList := splitNonEmpty(string(want))

	added, removed := diffSorted(wantList, got)

	for _, a := range added {
		t.Errorf(
			"%s is Optional + Computed and has a Default.\n"+
				"    A default overrides whatever the controller stores, so a resource that "+
				"already has this set to something else will plan a change back to the "+
				"default the moment the attribute is left out of a configuration.\n"+
				"    Prefer no Default plus UseStateForUnknown. If the provider genuinely "+
				"owns this value, add it to %s with a note saying why.",
			a, goldenOptionalComputedDefaults,
		)
	}
	if len(removed) > 0 {
		t.Errorf(
			"these no longer have a Default, which is good — drop them from %s:\n    %s",
			goldenOptionalComputedDefaults, strings.Join(removed, "\n    "),
		)
	}
}

// optionalComputedDefaults walks every resource schema the provider serves and
// returns the sorted "<resource>.<path>" of each Optional + Computed attribute
// carrying a Default. Nested attributes are included at their full path.
func optionalComputedDefaults(t *testing.T) []string {
	t.Helper()
	ctx := context.Background()

	var found []string

	var walk func(prefix string, attrs map[string]schema.Attribute)
	walk = func(prefix string, attrs map[string]schema.Attribute) {
		for name, a := range attrs {
			path := prefix + name

			var hasDefault bool
			switch v := a.(type) {
			case schema.BoolAttribute:
				hasDefault = v.Default != nil
			case schema.StringAttribute:
				hasDefault = v.Default != nil
			case schema.Int64Attribute:
				hasDefault = v.Default != nil
			case schema.Float64Attribute:
				hasDefault = v.Default != nil
			case schema.NumberAttribute:
				hasDefault = v.Default != nil
			case schema.ListAttribute:
				hasDefault = v.Default != nil
			case schema.SetAttribute:
				hasDefault = v.Default != nil
			case schema.MapAttribute:
				hasDefault = v.Default != nil
			case schema.ObjectAttribute:
				hasDefault = v.Default != nil
			case schema.SingleNestedAttribute:
				hasDefault = v.Default != nil
				walk(path+".", v.Attributes)
			case schema.ListNestedAttribute:
				hasDefault = v.Default != nil
				walk(path+".", v.NestedObject.Attributes)
			case schema.SetNestedAttribute:
				hasDefault = v.Default != nil
				walk(path+".", v.NestedObject.Attributes)
			case schema.MapNestedAttribute:
				hasDefault = v.Default != nil
				walk(path+".", v.NestedObject.Attributes)
			}

			if hasDefault && a.IsOptional() && a.IsComputed() {
				found = append(found, path)
			}
		}
	}

	for _, fn := range New().Resources(ctx) {
		r := fn()

		var meta fwresource.MetadataResponse
		r.Metadata(ctx, fwresource.MetadataRequest{ProviderTypeName: "unifi"}, &meta)

		var resp fwresource.SchemaResponse
		r.Schema(ctx, fwresource.SchemaRequest{}, &resp)
		if resp.Diagnostics.HasError() {
			t.Fatalf("schema for %s: %v", meta.TypeName, resp.Diagnostics)
		}

		walk(meta.TypeName+".", resp.Schema.Attributes)
	}

	sort.Strings(found)
	return found
}

func splitNonEmpty(s string) []string {
	var out []string
	for _, line := range strings.Split(s, "\n") {
		if line = strings.TrimSpace(line); line != "" && !strings.HasPrefix(line, "#") {
			out = append(out, line)
		}
	}
	sort.Strings(out)
	return out
}

// diffSorted returns the entries present only in got, and only in want.
func diffSorted(want, got []string) (added, removed []string) {
	inWant := make(map[string]bool, len(want))
	for _, w := range want {
		inWant[w] = true
	}
	inGot := make(map[string]bool, len(got))
	for _, g := range got {
		inGot[g] = true
		if !inWant[g] {
			added = append(added, g)
		}
	}
	for _, w := range want {
		if !inGot[w] {
			removed = append(removed, w)
		}
	}
	return added, removed
}

// Test_goldenOptionalComputedDefaultsPath keeps the golden file discoverable
// from the repo root as well as the package directory.
func Test_goldenOptionalComputedDefaultsPath(t *testing.T) {
	if _, err := os.Stat(filepath.FromSlash(goldenOptionalComputedDefaults)); err != nil {
		t.Fatalf("golden file missing: %v", err)
	}
}
