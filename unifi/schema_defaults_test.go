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
# configuration. Only correct where the provider owns the value and the
# controller stores nothing of its own. This is an inventory of what still
# needs checking against a live controller, not a list of approved patterns.
#
# Regenerate with: UPDATE_GOLDEN=1 go test ./unifi/ -run Test_schemaOptionalComputedDefaults
#
# Removing an entry also requires UPDATE_GOLDEN_ALLOW_REMOVAL=1.
`

// Test_schemaOptionalComputedDefaults pins the existing Optional + Computed
// attributes that also carry a Default: the framework fills the default in
// before the controller is consulted, so a resource whose stored value
// differs plans a change back to the default and silently turns the setting
// off. The combination isn't always wrong -- only where the provider owns
// the value -- so this ratchets the set rather than banning it: a new entry
// must be justified deliberately, after confirming a refresh with the
// attribute omitted plans empty.
//
// walkBlocks walks nested schema declared as a Block -- a separate Go type
// from Attributes that walk alone does not see -- and both walk's attribute
// type switch and walkBlocks' block type switch fail loudly on a type
// neither recognizes, so a newly added attribute or block type cannot
// silently skip the ratchet.
func Test_schemaOptionalComputedDefaults(t *testing.T) {
	got := optionalComputedDefaults(t)

	// UPDATE_GOLDEN=1 rewrites the inventory. Only reach for it after deciding
	// each new entry is correct — the point of the list is that adding to it is
	// a conscious act.
	if os.Getenv(updateGoldenEnv) != "" {
		writeGolden(t, goldenOptionalComputedDefaults, header, got)
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
	var walkBlocks func(prefix string, blocks map[string]schema.Block)

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
			default:
				t.Fatalf("%s: %T is not a recognized attribute type; add a case to walk "+
					"so a new attribute type cannot silently skip the defaults ratchet", path, a)
			}

			if hasDefault && a.IsOptional() && a.IsComputed() {
				found = append(found, path)
			}
		}
	}

	walkBlocks = func(prefix string, blocks map[string]schema.Block) {
		for name, b := range blocks {
			path := prefix + name

			switch v := b.(type) {
			case schema.SingleNestedBlock:
				walk(path+".", v.Attributes)
				walkBlocks(path+".", v.Blocks)
			case schema.ListNestedBlock:
				walk(path+".", v.NestedObject.Attributes)
				walkBlocks(path+".", v.NestedObject.Blocks)
			case schema.SetNestedBlock:
				walk(path+".", v.NestedObject.Attributes)
				walkBlocks(path+".", v.NestedObject.Blocks)
			default:
				t.Fatalf("%s: %T is not a recognized block type; add a case to walkBlocks "+
					"so a new block type cannot silently skip the defaults ratchet", path, b)
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
		walkBlocks(meta.TypeName+".", resp.Schema.Blocks)
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
