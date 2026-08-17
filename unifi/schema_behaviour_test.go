package unifi

import (
	"context"
	"fmt"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/action"
	actionschema "github.com/hashicorp/terraform-plugin-framework/action/schema"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	dschema "github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	rschema "github.com/hashicorp/terraform-plugin-framework/resource/schema"
)

const goldenSchemaBehaviour = "testdata/schema_behaviour.txt"

const behaviourHeader = `# Validators, plan modifiers, defaults and custom types, per attribute, for
# every managed resource, data source and action the provider registers.
#
# These never appear in a Terraform schema. The protocol carries types,
# dispositions, descriptions and deprecation, and nothing else -- so the
# baseline projection test cannot see any of this, however far it is extended.
# The released v0.101.2 baseline contains no validators, plan_modifiers or
# default key anywhere in the document.
#
# They are still public behaviour. A lost OneOf accepts configuration the
# controller will reject; a lost UseStateForUnknown plans a change on every
# refresh; a lost Default changes what an absent attribute means. Migrating a
# surface from a hand-written schema to a generated one drops all three
# silently, because the policy has to restate them and nothing checks that it
# did. firewall_policy lost nine validators that way and the whole suite
# stayed green.
#
# A custom type is here for the same reason: it is the validation on the
# attribute it sits on -- unifi_port.device_mac is a hwtypes.MACAddressType,
# and the protocol renders that as a plain string, so the released baseline
# records "string" and a schema that dropped the type compares equal to the
# contract while accepting any string as a MAC address.
#
# Each line is "<surface>.<attribute path>  <kind>  <implementation>  <description>".
# The description is the validator's own, so it carries the VALUES: changing
# an enum member changes the line rather than leaving the count intact.
#
# A removal here is a behaviour regression until someone says otherwise.
#
# Regenerate with: UPDATE_GOLDEN=1 go test ./unifi/ -run Test_schemaBehaviourInventory
#
# That refuses to drop an entry. A rewrite that would remove one has to say
# so with UPDATE_GOLDEN_ALLOW_REMOVAL=1 as well, because removing a line here
# is the direction a regression takes and rewriting the file is what erases
# the evidence of it.
`

// Test_schemaBehaviourInventory pins every validator, plan modifier and default
// the provider serves, so migrating a surface cannot drop one silently.
//
// It exists because the baseline projection test cannot cover this and no
// extension of it could. Terraform's schema protocol does not carry validators,
// plan modifiers or defaults -- they run inside the provider and are invisible
// to the CLI -- so the released baseline has nothing to compare against. That
// is not a gap in the projection test's thoroughness; it is a fact about what a
// schema is. This inventory is the second referee those facts need.
//
// Coverage is managed resources and data sources at every attribute depth, and
// actions at their single depth. List resources and identity schemas are
// separate schemas and are reported as uncovered rather than passed over.
//
// PROVEN TO FAIL, recorded here rather than only in the commits that proved it,
// so a reader with a checkout can tell this from a check nobody has seen go red.
//
//   - Seven mutations of a policy -- dropping a validator, changing an enum
//     value, dropping a plan modifier, dropping a default, changing a default's
//     value, adding a validator, dropping a nested validator -- each fail and
//     name the attribute. Reordering the policy does not. (37313392)
//   - Deleting wlan's schedule block fails this and the baseline projection,
//     each naming the surface; deleting one validator inside that block fails
//     this one alone, because the protocol cannot express a validator. (5d1b8bf5)
//   - Retrodicted against the migration it was written for: regenerated at the
//     commit before the firewall_policy rewire and at the commit after, all 731
//     lines were identical -- while that same class of migration had silently
//     dropped nine validators elsewhere, seven of them enum constraints. (37313392)
//
// THE PROOFS ARE WORTH WHAT THE REMOVAL GUARD IS WORTH. Each says this test goes
// red; none says the red survives. writeGolden's refusal is what stops a
// regeneration erasing the evidence, and that refusal is defeated by deleting
// the golden first. See golden_update_test.go.
func Test_schemaBehaviourInventory(t *testing.T) {
	ctx := context.Background()
	got, opaque := schemaBehaviourFacts(ctx, t)

	if os.Getenv(updateGoldenEnv) != "" {
		writeGolden(t, goldenSchemaBehaviour, behaviourHeader, got)
		return
	}

	want, err := os.ReadFile(goldenSchemaBehaviour)
	if err != nil {
		t.Fatalf("reading %s: %v", goldenSchemaBehaviour, err)
	}
	added, removed := diffSorted(splitNonEmpty(string(want)), got)

	// Removals are reported first and in full. A migration drops behaviour; it
	// rarely invents any, so this is the direction that matters.
	if len(removed) > 0 {
		t.Errorf(
			"%d behaviour(s) the provider no longer applies:\n    %s\n\n"+
				"    Each of these ran in the released provider and does not run now.\n"+
				"    A migrated surface must restate its validators, plan modifiers and\n"+
				"    defaults in its policy -- the generator cannot infer them from a\n"+
				"    catalog, and no schema comparison can see that they are gone.\n"+
				"    If a removal is intended, it is a behaviour change: land it on its\n"+
				"    own with its own evidence, not inside a migration.",
			len(removed), strings.Join(removed, "\n    "),
		)
	}
	if len(added) > 0 {
		t.Errorf(
			"%d behaviour(s) the provider did not previously apply:\n    %s\n\n"+
				"    Adding one is a public change. Confirm it is intended, then update\n"+
				"    %s.",
			len(added), strings.Join(added, "\n    "), goldenSchemaBehaviour,
		)
	}

	// An attribute type that carries its behaviour through differently named
	// fields would contribute nothing and look identical to one that carries no
	// behaviour at all. Naming them keeps the absence deliberate.
	if len(opaque) > 0 {
		t.Logf("attribute types exposing none of Validators/PlanModifiers/Default, "+
			"so nothing here reads their behaviour: %v", opaque)
	}
	if len(got) == 0 {
		t.Fatal("the inventory is empty, so the reflection below found nothing — " +
			"the framework's field names have most likely changed")
	}
}

// schemaBehaviourFacts walks every registered managed resource, data source and
// action, and returns one sorted line per behaviour, plus the attribute types it
// could not read.
//
// Reflection rather than a type switch over the ten concrete attribute types:
// a switch silently ignores any type added later, which is the same class of
// hole this test exists to close. The same reflection reads a data source's
// attributes without changes, because it looks for the Validators, PlanModifiers
// and Default FIELDS rather than for a known type.
//
// Data sources are prefixed "data." so they cannot collide with the resource of
// the same name. unifi_account is registered as both, and without the prefix
// their lines would be indistinguishable -- which would let a validator move
// from one to the other and keep the inventory identical.
func schemaBehaviourFacts(ctx context.Context, t *testing.T) ([]string, []string) {
	t.Helper()
	var facts []string
	opaque := map[string]bool{}

	for _, newResource := range (&unifiProvider{}).Resources(ctx) {
		res := newResource()
		var meta resource.MetadataResponse
		res.Metadata(ctx, resource.MetadataRequest{ProviderTypeName: "unifi"}, &meta)

		var got resource.SchemaResponse
		res.Schema(ctx, resource.SchemaRequest{}, &got)

		facts = append(facts, attributeBehaviour(ctx, meta.TypeName+".", got.Schema.Attributes, opaque)...)
		facts = append(facts, blockBehaviour(ctx, meta.TypeName+".", got.Schema.Blocks, opaque)...)
	}

	// Data sources carry fifteen validators between them and were covered by
	// nothing until they were migrated -- the schema referee reads them
	// (baseline_projection_datasource_test.go) but a validator is invisible to
	// the protocol, so losing one during a migration would have looked exactly
	// like success. That is the fault firewall_policy already shipped once.
	for _, newDataSource := range (&unifiProvider{}).DataSources(ctx) {
		ds := newDataSource()
		var meta datasource.MetadataResponse
		ds.Metadata(ctx, datasource.MetadataRequest{ProviderTypeName: "unifi"}, &meta)

		var got datasource.SchemaResponse
		ds.Schema(ctx, datasource.SchemaRequest{}, &got)

		prefix := "data." + meta.TypeName + "."
		facts = append(facts, dataSourceAttributeBehaviour(ctx, prefix, got.Schema.Attributes, opaque)...)
	}

	// Actions carry the same kind of invisible behaviour and were not read
	// here. The estate has one, and its device_mac attribute is a
	// hwtypes.MACAddressType: the protocol renders that as a plain string, so
	// the released baseline records "string" and a generated schema that
	// dropped the custom type would compare equal to the contract. The type IS
	// the validation on that attribute -- losing it means accepting any string
	// as a MAC address -- and until this loop existed nothing in the package
	// would have noticed.
	//
	// behaviourOf reads by reflection and takes any, so action attributes need
	// no separate reader; only the walk is new.
	for _, newAction := range (&unifiProvider{}).Actions(ctx) {
		act := newAction()

		var meta action.MetadataResponse
		act.Metadata(ctx, action.MetadataRequest{ProviderTypeName: "unifi"}, &meta)

		var got action.SchemaResponse
		act.Schema(ctx, action.SchemaRequest{}, &got)

		for _, name := range sortedActionAttributes(got.Schema.Attributes) {
			lines, read := behaviourOf(ctx, meta.TypeName+"."+name, got.Schema.Attributes[name])
			facts = append(facts, lines...)
			if !read {
				opaque[fmt.Sprintf("%T", got.Schema.Attributes[name])] = true
			}
		}
	}

	sort.Strings(facts)
	names := make([]string, 0, len(opaque))
	for name := range opaque {
		names = append(names, name)
	}
	sort.Strings(names)
	return facts, names
}

// dataSourceAttributeBehaviour mirrors attributeBehaviour over the data source
// attribute types. The two trees share no interface -- datasource/schema and
// resource/schema declare separate Attribute types -- so the descent has to be
// written twice even though behaviourOf reads both.
func dataSourceAttributeBehaviour(
	ctx context.Context,
	prefix string,
	attrs map[string]dschema.Attribute,
	opaque map[string]bool,
) []string {
	var facts []string
	for name, attribute := range attrs {
		path := prefix + name
		lines, read := behaviourOf(ctx, path, attribute)
		facts = append(facts, lines...)
		if !read {
			opaque[fmt.Sprintf("%T", attribute)] = true
		}

		switch nested := attribute.(type) {
		case dschema.SingleNestedAttribute:
			facts = append(facts, dataSourceAttributeBehaviour(ctx, path+".", nested.Attributes, opaque)...)
		case dschema.ListNestedAttribute:
			facts = append(facts, dataSourceAttributeBehaviour(ctx, path+".", nested.NestedObject.Attributes, opaque)...)
		case dschema.SetNestedAttribute:
			facts = append(facts, dataSourceAttributeBehaviour(ctx, path+".", nested.NestedObject.Attributes, opaque)...)
		case dschema.MapNestedAttribute:
			facts = append(facts, dataSourceAttributeBehaviour(ctx, path+".", nested.NestedObject.Attributes, opaque)...)
		}
	}
	return facts
}

func attributeBehaviour(
	ctx context.Context,
	prefix string,
	attrs map[string]rschema.Attribute,
	opaque map[string]bool,
) []string {
	var facts []string
	for name, attribute := range attrs {
		path := prefix + name
		lines, read := behaviourOf(ctx, path, attribute)
		facts = append(facts, lines...)
		if !read {
			opaque[fmt.Sprintf("%T", attribute)] = true
		}

		switch nested := attribute.(type) {
		case rschema.SingleNestedAttribute:
			facts = append(facts, attributeBehaviour(ctx, path+".", nested.Attributes, opaque)...)
		case rschema.ListNestedAttribute:
			facts = append(facts, attributeBehaviour(ctx, path+".", nested.NestedObject.Attributes, opaque)...)
		case rschema.SetNestedAttribute:
			facts = append(facts, attributeBehaviour(ctx, path+".", nested.NestedObject.Attributes, opaque)...)
		case rschema.MapNestedAttribute:
			facts = append(facts, attributeBehaviour(ctx, path+".", nested.NestedObject.Attributes, opaque)...)
		}
	}
	return facts
}

// blockBehaviour walks blocks, which hold their own validators and plan
// modifiers and contain attributes that hold theirs. Reading only
// Schema.Attributes missed all of it: the estate has four blocks carrying
// fifty-six attributes between them, and nothing here saw any of them.
func blockBehaviour(
	ctx context.Context,
	prefix string,
	blocks map[string]rschema.Block,
	opaque map[string]bool,
) []string {
	var facts []string
	for name, block := range blocks {
		path := prefix + name
		lines, read := behaviourOf(ctx, path, block)
		facts = append(facts, lines...)
		if !read {
			opaque[fmt.Sprintf("%T", block)] = true
		}

		switch shaped := block.(type) {
		case rschema.ListNestedBlock:
			facts = append(facts, attributeBehaviour(ctx, path+".", shaped.NestedObject.Attributes, opaque)...)
			facts = append(facts, blockBehaviour(ctx, path+".", shaped.NestedObject.Blocks, opaque)...)
		case rschema.SetNestedBlock:
			facts = append(facts, attributeBehaviour(ctx, path+".", shaped.NestedObject.Attributes, opaque)...)
			facts = append(facts, blockBehaviour(ctx, path+".", shaped.NestedObject.Blocks, opaque)...)
		case rschema.SingleNestedBlock:
			facts = append(facts, attributeBehaviour(ctx, path+".", shaped.Attributes, opaque)...)
			facts = append(facts, blockBehaviour(ctx, path+".", shaped.Blocks, opaque)...)
		}
	}
	return facts
}

// behaviourOf reads one attribute's validators, plan modifiers and default. The
// second return says whether any of the three fields existed, which is what
// separates "this attribute has no behaviour" from "this attribute type keeps
// its behaviour somewhere this test does not look".
func behaviourOf(ctx context.Context, path string, attribute any) ([]string, bool) {
	value := reflect.ValueOf(attribute)
	if value.Kind() == reflect.Pointer {
		value = value.Elem()
	}
	if value.Kind() != reflect.Struct {
		return nil, false
	}

	var facts []string
	read := false
	for _, field := range []struct{ name, kind string }{
		{"Validators", "validator"},
		{"PlanModifiers", "plan_modifier"},
		{"Default", "default"},
		// A custom type carries parsing and validation of its own and never
		// changes the wire type -- timetypes.GoDurationType and
		// hwtypes.MACAddressType are both strings to Terraform. So losing one
		// is invisible to a schema comparison and silently stops a value being
		// checked, which is the same failure as losing a validator.
		{"CustomType", "custom_type"},
	} {
		found := value.FieldByName(field.name)
		if !found.IsValid() {
			continue
		}
		read = true
		switch found.Kind() {
		case reflect.Slice:
			for index := range found.Len() {
				facts = append(facts, behaviourLine(ctx, path, field.kind, found.Index(index).Interface()))
			}
		case reflect.Interface:
			if !found.IsNil() {
				facts = append(facts, behaviourLine(ctx, path, field.kind, found.Interface()))
			}
		}
	}
	return facts, read
}

// behaviourLine renders one fact. The description is quoted rather than written
// bare: a default of the empty string describes itself as "value defaults to "
// with a trailing space, and reading the golden file back trims it, so an
// unquoted line would report seven behaviours as lost on every run.
func behaviourLine(ctx context.Context, path, kind string, behaviour any) string {
	description := "<carries no description>"
	if describer, ok := behaviour.(interface {
		Description(context.Context) string
	}); ok {
		description = describer.Description(ctx)
	}
	return fmt.Sprintf("%s\t%s\t%T\t%q", path, kind, behaviour, description)
}

// sortedActionAttributes keeps the walk deterministic. Go map order would make
// the inventory differ run to run and every regeneration a diff.
func sortedActionAttributes(m map[string]actionschema.Attribute) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
