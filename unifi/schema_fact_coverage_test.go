package unifi

import (
	"reflect"
	"sort"
	"strings"
	"testing"

	specresource "github.com/hashicorp/terraform-plugin-codegen-spec/resource"
)

// factCoverage says, for one key the provider code specification can carry,
// which referee reads it — or why nothing does.
type factCoverage struct {
	// By names the test that would fail if this fact moved. Empty means
	// nothing checks it, which is allowed only with a Because.
	By string
	// Because justifies an unchecked key. It is required when By is empty, so
	// "nothing checks this" cannot be expressed by omission.
	Because string
}

// specFactCoverage accounts for every key the specification can carry.
//
// A migrated surface's behaviour is whatever its policy states, and the policy
// states it through these keys. So the question "is the migration faithful?"
// decomposes into one question per key, and a key nobody asks about is a fact
// that can move silently — which is exactly how firewall_policy lost nine
// validators with the suite green.
//
// Adding a key to the specification without adding a line here fails
// Test_everySpecFactIsAccountedFor. That is the point: the list of questions
// has to be maintained as deliberately as the answers.
var specFactCoverage = map[string]factCoverage{
	// Compared against the released v0.101.2 schema.
	"computed_optional_required": {By: "TestBuiltSchemaMatchesReleasedBaseline"},
	"description":                {By: "TestBuiltSchemaMatchesReleasedBaseline"},
	"markdown_description":       {By: "TestBuiltSchemaMatchesReleasedBaseline"},
	"sensitive":                  {By: "TestBuiltSchemaMatchesReleasedBaseline"},
	"deprecation_message":        {By: "TestBuiltSchemaMatchesReleasedBaseline"},

	// Compared against the pinned behaviour inventory. None of these appear in
	// a Terraform schema at all, so no comparison against the released
	// baseline could ever reach them.
	"validators":     {By: "Test_schemaBehaviourInventory"},
	"plan_modifiers": {By: "Test_schemaBehaviourInventory"},
	"default":        {By: "Test_schemaBehaviourInventory"},
	"static":         {By: "Test_schemaBehaviourInventory"},

	// Type shape. The projection test marshals the framework's own
	// tftypes.Type and compares it to the baseline's, so every one of these is
	// covered through that single encoding rather than key by key.
	"bool": {By: "TestBuiltSchemaMatchesReleasedBaseline"}, "string": {By: "TestBuiltSchemaMatchesReleasedBaseline"},
	"int64": {By: "TestBuiltSchemaMatchesReleasedBaseline"}, "float64": {By: "TestBuiltSchemaMatchesReleasedBaseline"},
	"number": {By: "TestBuiltSchemaMatchesReleasedBaseline"}, "dynamic": {By: "TestBuiltSchemaMatchesReleasedBaseline"},
	"list": {By: "TestBuiltSchemaMatchesReleasedBaseline"}, "set": {By: "TestBuiltSchemaMatchesReleasedBaseline"},
	"map": {By: "TestBuiltSchemaMatchesReleasedBaseline"}, "object": {By: "TestBuiltSchemaMatchesReleasedBaseline"},
	"element_type": {By: "TestBuiltSchemaMatchesReleasedBaseline"}, "attribute_types": {By: "TestBuiltSchemaMatchesReleasedBaseline"},
	"single_nested": {By: "TestBuiltSchemaMatchesReleasedBaseline"}, "list_nested": {By: "TestBuiltSchemaMatchesReleasedBaseline"},
	"set_nested": {By: "TestBuiltSchemaMatchesReleasedBaseline"}, "map_nested": {By: "TestBuiltSchemaMatchesReleasedBaseline"},
	"nested_object": {By: "TestBuiltSchemaMatchesReleasedBaseline"},
	"attributes":    {By: "TestBuiltSchemaMatchesReleasedBaseline"},
	"name":          {By: "TestBuiltSchemaMatchesReleasedBaseline"},
	"schema":        {By: "TestBuiltSchemaMatchesReleasedBaseline"},

	// Deliberately unchecked.
	"blocks": {Because: "Blocks are not projected. Three surfaces carry block_types " +
		"and TestBuiltSchemaMatchesReleasedBaseline names them on every run rather than " +
		"passing over them silently."},
	"custom_type": {Because: "Names the Go type the generator emits, which the protocol " +
		"never sees, so no schema comparison can reach it. It is not inert: a custom object " +
		"type overrides an attribute map, which is what broke firewall_policy's v0 state " +
		"upgrader. Guarded per surface where it matters — see " +
		"TestFirewallPolicyV0SchemaDescribesPortAsAnInteger — because what a custom type " +
		"means depends on what the surrounding code does with it."},
	"associated_external_type": {Because: "Generates conversion helpers between the framework " +
		"model and an SDK struct. It produces no schema fact and no runtime behaviour; a " +
		"mistake here fails to compile."},
	"import":  {Because: "Import paths for a custom validator, plan modifier or default. Wrong ones fail to compile, and the behaviour they carry is compared by Test_schemaBehaviourInventory."},
	"imports": {Because: "As import."},
	"path":    {Because: "The package path within an import. As import."},
	"alias": {Because: "An optional package alias within an import, used in Go to avoid a name " +
		"collision or to import for side effects only. It changes how generated code spells a " +
		"reference, never what the reference does; a wrong alias fails to compile."},
	"custom": {Because: "Wrapper around a custom validator, plan modifier or default. Its contents are compared by Test_schemaBehaviourInventory."},
	"schema_definition": {Because: "The Go expression for a custom validator, plan modifier or " +
		"default. Comparing the text would pin how a behaviour is spelled rather than what it " +
		"does; Test_schemaBehaviourInventory compares the behaviour's own description instead, " +
		"which carries its values."},
	"value_type": {Because: "The Go type a custom default returns. A mismatch fails to compile."},
	"type":       {Because: "Names an associated external type. Schema types are carried by the per-kind keys above."},
}

// Test_everySpecFactIsAccountedFor walks the provider code specification's own
// Go types and requires each key to be either compared by a named test or
// unchecked for a stated reason.
//
// This is coverage of questions rather than coverage of inputs. Running more
// surfaces through the existing tests never widens a comparison that does not
// look at a key, so no amount of migration would have revealed the validators
// gap. Only enumerating the keys does.
func Test_everySpecFactIsAccountedFor(t *testing.T) {
	keys := specJSONKeys()
	if len(keys) < 20 {
		t.Fatalf("found only %d specification keys, so the reflection below is not "+
			"reaching the specification's types", len(keys))
	}

	var unaccounted []string
	for _, key := range keys {
		coverage, known := specFactCoverage[key]
		switch {
		case !known:
			unaccounted = append(unaccounted, key)
		case coverage.By == "" && coverage.Because == "":
			t.Errorf("specification key %q is recorded as unchecked with no reason given", key)
		}
	}
	if len(unaccounted) > 0 {
		t.Errorf(
			"the provider code specification carries %d key(s) nothing accounts for:\n    %s\n\n"+
				"    Each is a fact a policy can state and a migration can therefore drop.\n"+
				"    Add a line to specFactCoverage naming the test that compares it, or\n"+
				"    stating why nothing does. Do not leave it out: a fact nobody asks\n"+
				"    about is one that moves silently, which is how nine validators were\n"+
				"    lost with every test passing.",
			len(unaccounted), strings.Join(unaccounted, "\n    "))
	}

	// The reverse direction. An entry for a key the specification no longer has
	// is stale, and stale accounting reads as coverage.
	present := map[string]bool{}
	for _, key := range keys {
		present[key] = true
	}
	for key := range specFactCoverage {
		if !present[key] {
			t.Errorf("specFactCoverage accounts for %q, which the specification no longer carries", key)
		}
	}
}

// specJSONKeys collects every json tag reachable from the specification's
// resource types, following struct fields, pointers, slices and maps.
func specJSONKeys() []string {
	seen := map[reflect.Type]bool{}
	keys := map[string]bool{}

	var walk func(reflect.Type)
	walk = func(t reflect.Type) {
		for t.Kind() == reflect.Pointer || t.Kind() == reflect.Slice || t.Kind() == reflect.Array || t.Kind() == reflect.Map {
			t = t.Elem()
		}
		if t.Kind() != reflect.Struct || seen[t] {
			return
		}
		seen[t] = true
		for index := range t.NumField() {
			field := t.Field(index)
			if tag := field.Tag.Get("json"); tag != "" && tag != "-" {
				if name, _, _ := strings.Cut(tag, ","); name != "" {
					keys[name] = true
				}
			}
			walk(field.Type)
		}
	}
	walk(reflect.TypeOf(specresource.Resource{}))
	walk(reflect.TypeOf(specresource.Attribute{}))

	out := make([]string, 0, len(keys))
	for key := range keys {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}
