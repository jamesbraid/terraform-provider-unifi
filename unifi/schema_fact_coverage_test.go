package unifi

import (
	"reflect"
	"sort"
	"strings"
	"testing"

	specresource "github.com/hashicorp/terraform-plugin-codegen-spec/resource"
)

// factCoverage says, for one key the provider code specification can carry,
// what FAILS if it changes and nobody intended the change. Every entry must
// name a mechanism that fails -- being named in a log is not being compared.
type factCoverage struct {
	// By names the test that fails when this fact moves.
	By string
	// Compiles records that a wrong value cannot build.
	Compiles bool
	// Unused records a key this estate never emits, so no mechanism has ever
	// been exercised on it -- honest, and not the same as covered.
	Unused bool
	// Because carries the reasoning, and is required whenever no test is named.
	Because string
}

// specFactCoverage accounts for every key the specification can carry; a key
// nobody asks about is a fact that can move silently. Adding a key to the
// specification without a line here fails Test_everySpecFactIsAccountedFor.
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

	"blocks": {By: "TestBuiltSchemaMatchesReleasedBaseline"},
	// A custom type produces no schema fact but parses and validates while
	// staying a string on the wire, so only the behaviour inventory sees one
	// being dropped.
	"custom_type": {By: "Test_schemaBehaviourInventory"},
	"associated_external_type": {Unused: true, Because: "Generates conversion helpers between the " +
		"framework model and an SDK struct. No policy in this estate declares one, so whether a " +
		"wrong one converts silently or refuses to build is untested. Answer that before the " +
		"first policy uses it."},
	"import":  {Compiles: true, Because: "An import path for a custom validator, plan modifier or default. A wrong one names a package that does not exist."},
	"imports": {Compiles: true, Because: "As import."},
	"path":    {Compiles: true, Because: "The package path within an import. As import."},
	"alias": {Unused: true, Because: "An optional package alias within an import. No policy here sets " +
		"one, so the claim that a wrong alias fails to compile has never been exercised and is " +
		"not asserted."},
	"custom": {By: "Test_schemaBehaviourInventory", Because: "Wrapper around a custom validator, plan modifier or default; its contents are what the inventory reads."},
	"schema_definition": {By: "Test_schemaBehaviourInventory", Because: "The Go expression for a " +
		"custom behaviour. The inventory does not compare this text — that would pin how a " +
		"behaviour is spelled rather than what it does — but it compares the resulting " +
		"behaviour's own description, which carries its values."},
	"value_type": {Compiles: true, Because: "The Go type behind a custom type. A wrong one does not typecheck against the framework's interfaces."},
	"type":       {Unused: true, Because: "Names an associated external type, which nothing here declares."},
}

// Test_everySpecFactIsAccountedFor walks the provider code specification's
// own Go types and requires each key to be either compared by a named test
// or unchecked for a stated reason.
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
		case coverage.By == "" && !coverage.Compiles && !coverage.Unused:
			t.Errorf("specification key %q names no mechanism that fails when it changes. "+
				"Name the test, or record that a wrong value cannot compile, or say the key is "+
				"unused. A key that is merely reported somewhere is not covered.", key)
		case coverage.By == "" && coverage.Because == "":
			t.Errorf("specification key %q claims coverage without naming a test, and gives no reasoning", key)
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

	// The reverse direction: stale accounting reads as coverage.
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
