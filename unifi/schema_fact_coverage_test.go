package unifi

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	specresource "github.com/hashicorp/terraform-plugin-codegen-spec/resource"
)

// factCoverage says, for one key the provider code specification can carry,
// what FAILS if it changes and nobody intended the change. Every entry must
// name a mechanism that fails -- being named in a log is not being compared.
//
// Both claims an entry can make are checked against the tree, not taken on
// trust: By must name a test this package really declares, and Unused must
// name a key the compiler really never writes. An unchecked claim is how
// twenty-six entries here spent months citing a test that had been deleted.
type factCoverage struct {
	// By names the test that fails when this fact moves. It must be a Test
	// function declared in this package.
	By string
	// Compiles records that a wrong value cannot build.
	Compiles bool
	// Unused records a key this estate never emits, so no mechanism has ever
	// been exercised on it -- honest, and not the same as covered.
	Unused bool
	// Because carries the reasoning, and is required whenever no test is named.
	Because string
}

// neverEmittedShape is the reason shared by every type shape no compiled code
// specification in this estate carries. Nothing has ever been compared on
// them, and saying so beats claiming a comparison that never runs.
const neverEmittedShape = "No compiled code specification under " +
	"provider-codegen/generated carries this shape, so no comparison has ever reached it. " +
	"The claim is checked: an entry here fails the moment a policy starts emitting the shape."

// specFactCoverage accounts for every key the specification can carry; a key
// nobody asks about is a fact that can move silently. Adding a key to the
// specification without a line here fails Test_everySpecFactIsAccountedFor.
var specFactCoverage = map[string]factCoverage{
	// Contract and documentation facts. testdata/schema-snapshot.json records
	// each of these per attribute, per block and per surface, and the
	// comparison diffs them field by field.
	"computed_optional_required": {By: "TestProviderSchemaSnapshot"},
	"description":                {By: "TestProviderSchemaSnapshot"},
	"markdown_description":       {By: "TestProviderSchemaSnapshot"},
	"sensitive":                  {By: "TestProviderSchemaSnapshot"},
	"deprecation_message":        {By: "TestProviderSchemaSnapshot"},

	// Framework-only behaviour. None of these appears in a Terraform wire
	// schema at all, so no comparison of the wire contract could reach them.
	// The snapshot does: it reflects each attribute's Default, PlanModifiers,
	// Validators and CustomType and records every one's Go type together with
	// its own Description.
	"validators":     {By: "TestProviderSchemaSnapshot"},
	"plan_modifiers": {By: "TestProviderSchemaSnapshot"},
	"default":        {By: "TestProviderSchemaSnapshot"},
	"custom_type":    {By: "TestProviderSchemaSnapshot"},
	"static": {By: "TestProviderSchemaSnapshot", Because: "The literal value a default " +
		"carries. A default's own Description spells it out -- \"value defaults to false\" -- " +
		"so the recorded string moves when the literal does."},

	// Type shape. The snapshot marshals the framework's own tftypes.Type and
	// diffs the encoding, so these are covered through that single encoding
	// rather than key by key. Nesting is recorded separately, as the nesting
	// mode, and every nested attribute appears again under its own dotted path.
	"bool": {By: "TestProviderSchemaSnapshot"}, "string": {By: "TestProviderSchemaSnapshot"},
	"int64": {By: "TestProviderSchemaSnapshot"}, "float64": {By: "TestProviderSchemaSnapshot"},
	"list": {By: "TestProviderSchemaSnapshot"},
	"set":  {By: "TestProviderSchemaSnapshot"}, "element_type": {By: "TestProviderSchemaSnapshot"},
	"single_nested": {By: "TestProviderSchemaSnapshot"}, "list_nested": {By: "TestProviderSchemaSnapshot"},
	"set_nested": {By: "TestProviderSchemaSnapshot"}, "nested_object": {By: "TestProviderSchemaSnapshot"},
	"attributes": {By: "TestProviderSchemaSnapshot"},
	"blocks":     {By: "TestProviderSchemaSnapshot"},
	"name":       {By: "TestProviderSchemaSnapshot"},
	"schema":     {By: "TestProviderSchemaSnapshot"},

	// Type shapes this estate has never compiled. Every attribute it emits is
	// a bool, string, int64, float64, list, set, single-nested or
	// list-nested one, so the framework's number, dynamic, map, map-nested
	// and object attributes have never been through the pipeline at all.
	// float64 joined the compiled set with unifi_hotspot_package's amount
	// and trial_reset, the SDK's own fractional fields.
	"number":          {Unused: true, Because: neverEmittedShape},
	"dynamic":         {Unused: true, Because: neverEmittedShape},
	"map":             {Unused: true, Because: neverEmittedShape},
	"map_nested":      {Unused: true, Because: neverEmittedShape},
	"object":          {Unused: true, Because: neverEmittedShape},
	"attribute_types": {Unused: true, Because: neverEmittedShape},

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
	"custom": {By: "TestProviderSchemaSnapshot", Because: "Wrapper around a custom validator, plan modifier or default; its contents are what the snapshot reads."},
	"schema_definition": {By: "TestProviderSchemaSnapshot", Because: "The Go expression for a " +
		"custom behaviour. The snapshot does not compare this text — that would pin how a " +
		"behaviour is spelled rather than what it does — but it compares the resulting " +
		"behaviour's own description, which carries its values."},
	"value_type": {Compiles: true, Because: "The Go type behind a custom type. A wrong one does not typecheck against the framework's interfaces."},
	"type": {Compiles: true, Because: "The Go type expression for a custom type, and also the " +
		"name of an associated external type. This estate declares the first on every " +
		"custom-typed attribute; a wrong expression does not typecheck. As value_type."},
}

// Test_everySpecFactIsAccountedFor walks the provider code specification's
// own Go types and requires each key to be either compared by a named test
// or unchecked for a stated reason -- then checks both claims against the
// tree, so neither can be decoration.
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

	problems := coverageProblems(specFactCoverage, declaredTestNames(t), emittedSpecKeys(t))
	if len(problems) > 0 {
		t.Errorf("%d ledger entr(ies) claim something this tree does not support:\n    %s\n\n"+
			"    A By nobody resolves and an Unused nobody measures are both decoration:\n"+
			"    they read as coverage and assert nothing, which is worse than leaving the\n"+
			"    key out. Point By at the test that really fails, or drop the claim and say\n"+
			"    what is left unchecked.",
			len(problems), strings.Join(problems, "\n    "))
	}
}

// Test_specFactCoverageRejectsAnAbsentTest is the existence check's positive
// control. It has to be shown going red on a citation nobody declares, because
// that is exactly the state this ledger was in: twenty-six entries naming a
// test that had been deleted along with the baseline comparison it belonged
// to, and a check that only ever asked whether the string was empty.
func Test_specFactCoverageRejectsAnAbsentTest(t *testing.T) {
	declared := declaredTestNames(t)
	emitted := emittedSpecKeys(t)

	if problems := coverageProblems(specFactCoverage, declared, emitted); len(problems) > 0 {
		t.Fatalf("the real ledger already reports %d problem(s), so a planted one proves "+
			"nothing about the check:\n    %s", len(problems), strings.Join(problems, "\n    "))
	}

	planted := map[string]factCoverage{
		"description": {By: "TestNoSuchTestIsDeclaredAnywhere"},
		"sensitive":   {By: "TestProviderSchemaSnapshot"},
	}
	problems := coverageProblems(planted, declared, emitted)
	if len(problems) != 1 {
		t.Fatalf("one absent citation beside one real one produced %d problem(s), want 1: %v",
			len(problems), problems)
	}
	if !strings.Contains(problems[0], "TestNoSuchTestIsDeclaredAnywhere") ||
		!strings.Contains(problems[0], "description") {
		t.Errorf("the problem names neither the key nor the missing test: %q", problems[0])
	}
}

// Test_specFactCoverageRejectsAnEmittedUnusedKey is the other half's control:
// a key recorded as never emitted has to go red once the compiler emits it,
// or "unused" is one more unchecked claim.
func Test_specFactCoverageRejectsAnEmittedUnusedKey(t *testing.T) {
	declared := declaredTestNames(t)
	emitted := emittedSpecKeys(t)

	planted := map[string]factCoverage{
		"description": {Unused: true, Because: "planted for the control"},
		"dynamic":     {Unused: true, Because: "genuinely absent from every compiled specification"},
	}
	problems := coverageProblems(planted, declared, emitted)
	if len(problems) != 1 {
		t.Fatalf("one emitted key beside one absent key, both marked unused, produced "+
			"%d problem(s), want 1: %v", len(problems), problems)
	}
	if !strings.Contains(problems[0], "description") {
		t.Errorf("the problem does not name the emitted key: %q", problems[0])
	}
}

// coverageProblems reports every ledger entry whose claim does not hold
// against the package's real tests and the compiler's real output.
func coverageProblems(ledger map[string]factCoverage, declared, emitted map[string]bool) []string {
	keys := make([]string, 0, len(ledger))
	for key := range ledger {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	var problems []string
	for _, key := range keys {
		coverage := ledger[key]
		if coverage.By != "" && !declared[coverage.By] {
			problems = append(problems, fmt.Sprintf(
				"%q names %s as the test that fails when it moves, and this package declares "+
					"no such test", key, coverage.By))
		}
		if coverage.Unused && emitted[key] {
			problems = append(problems, fmt.Sprintf(
				"%q is recorded as a key this estate never emits, and the compiled code "+
					"specifications carry it", key))
		}
	}
	return problems
}

// declaredTestNames returns every top-level Test function this package
// declares. Build tags are deliberately not applied: a test behind
// //go:build acceptance still exists, and the ledger's claim is that the named
// function is real, not that it runs in every configuration.
func declaredTestNames(t *testing.T) map[string]bool {
	t.Helper()
	paths, err := filepath.Glob("*_test.go")
	if err != nil {
		t.Fatalf("globbing *_test.go: %v", err)
	}
	if len(paths) == 0 {
		t.Fatal("no _test.go file was found in the package directory, so every citation " +
			"would read as missing and the check would fail for the wrong reason")
	}

	names := map[string]bool{}
	fset := token.NewFileSet()
	for _, path := range paths {
		file, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parsing %s: %v", path, err)
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv != nil || !strings.HasPrefix(fn.Name.Name, "Test") {
				continue
			}
			names[fn.Name.Name] = true
		}
	}
	if !names["Test_everySpecFactIsAccountedFor"] {
		t.Fatalf("the walk over %d file(s) did not find this file's own test, so it is not "+
			"reading the package it claims to", len(paths))
	}
	return names
}

// emittedSpecKeys collects every JSON key the compiled code specifications
// actually carry, so an entry claiming a key is unused is checked against what
// the compiler writes rather than against somebody's recollection.
func emittedSpecKeys(t *testing.T) map[string]bool {
	t.Helper()
	pattern := filepath.Join("..", "provider-codegen", "generated", "*.provider-code-spec.json")
	paths, err := filepath.Glob(pattern)
	if err != nil {
		t.Fatalf("globbing %s: %v", pattern, err)
	}
	if len(paths) == 0 {
		t.Fatalf("no compiled code specification matched %s, so every key would read as "+
			"never emitted", pattern)
	}

	keys := map[string]bool{}
	var walk func(node any)
	walk = func(node any) {
		switch shaped := node.(type) {
		case map[string]any:
			for key, value := range shaped {
				keys[key] = true
				walk(value)
			}
		case []any:
			for _, value := range shaped {
				walk(value)
			}
		}
	}
	for _, path := range paths {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("reading %s: %v", path, err)
		}
		var document any
		if err := json.Unmarshal(raw, &document); err != nil {
			t.Fatalf("parsing %s: %v", path, err)
		}
		walk(document)
	}
	if !keys["computed_optional_required"] {
		t.Fatalf("the %d compiled specification(s) carry no computed_optional_required, so "+
			"the walk is not reaching attributes", len(paths))
	}
	return keys
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
