package unifi

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"

	listschema "github.com/hashicorp/terraform-plugin-framework/list/schema"
)

const goldenListResourceSchemas = "testdata/list_resource_schemas.txt"

const listResourceHeader = `# Every list resource's config schema, at full fidelity.
#
# The list surfaces are the only schemas in this provider that nothing else
# pins. The baseline projection test covers managed resources and says so
# explicitly; a sibling covers data sources; identity and list-resource schemas
# are named there as separate fact families and are not compared. The behaviour
# inventory covers managed resources at every depth and reports list resources
# as uncovered. So before this file existed, a list config schema could lose
# every description it has and the whole suite would stay green.
#
# That mattered the moment these surfaces started being GENERATED. A generator
# reproduces what the specification tells it to reproduce. Structure is easy to
# get right and easy to check; prose is neither, and prose is what a
# practitioner reads in terraform-ls, in the registry docs and in an error. A
# generated schema that is structurally perfect and silently undescribed is the
# exact shape of defect this estate keeps producing: a fact nobody was asking
# about.
#
# The uniformity gate next to this one compares STRUCTURE across all
# twenty-five and deliberately ignores prose, because twenty-five distinct
# block descriptions are correct and pinning them there would fight every
# wording change while catching nothing structural. This file is the other
# half: it pins the prose, per surface, so a rewording is a visible diff rather
# than an invisible loss.
#
# Each line is "<resource>.<path>  <type:disposition>  <flags>  <description>",
# where flags is sensitive, deprecated, or "-" for neither. A schema's or a
# block's own description is recorded as "<resource>[.<block>]  schema|block  -
# <text>", and an attribute whose plain Description differs from its markdown
# one gets a second "plain-description" line. There are none today: every
# hand-written list schema here sets MarkdownDescription alone, while the
# upstream generator's resource output sets both, so an emitter that copied that
# convention would quietly start serving a field the released provider left
# empty.
#
# Regenerate with: UPDATE_GOLDEN=1 go test ./unifi/ -run TestListResourceConfigSchemaGolden
#
# That refuses to drop a line. A rewrite that would remove one has to say so
# with UPDATE_GOLDEN_ALLOW_REMOVAL=1 as well.
`

// TestListResourceConfigSchemaGolden pins each list resource's config schema
// including its prose, so generating one cannot quietly serve a schema that is
// the right shape and says nothing.
//
// This is the oracle the list-resource emitter is checked against. The emitter
// is straight-line code that renders a specification member into Go; what makes
// its output trustworthy is not that the Go looks right but that the schema the
// provider serves afterwards is identical to the one it served before, down to
// the description strings. Cutting this file from the hand-written schemas
// FIRST and requiring it not to move is what makes that a measurement instead
// of an inspection.
func TestListResourceConfigSchemaGolden(t *testing.T) {
	ctx := context.Background()
	got := listResourceSchemaFacts(ctx, t)

	if len(got) == 0 {
		t.Fatal("no list resource facts were collected, so this file would pin nothing — " +
			"either the provider registers no list resources or the walk below reads " +
			"fields the framework has since renamed")
	}

	if os.Getenv(updateGoldenEnv) != "" {
		writeGolden(t, goldenListResourceSchemas, listResourceHeader, got)
		return
	}

	want, err := os.ReadFile(goldenListResourceSchemas)
	if err != nil {
		t.Fatalf("reading %s: %v", goldenListResourceSchemas, err)
	}
	added, removed := diffSorted(splitNonEmpty(string(want)), got)

	if len(removed) > 0 {
		t.Errorf(
			"%d list config fact(s) the provider no longer serves:\n    %s\n\n"+
				"    Each of these was in the released schema and is not there now.\n"+
				"    A generated list schema restates its descriptions from the\n"+
				"    specification; if the specification omits one, the attribute is\n"+
				"    still emitted, still the right type and still the right\n"+
				"    disposition — the uniformity gate stays green and only this file\n"+
				"    notices. Put the description back in the specification rather than\n"+
				"    regenerating this file.",
			len(removed), strings.Join(removed, "\n    "))
	}
	if len(added) > 0 {
		t.Errorf(
			"%d list config fact(s) the provider did not previously serve:\n    %s\n\n"+
				"    A change to a practitioner-visible schema. Confirm it is intended,\n"+
				"    then update %s.",
			len(added), strings.Join(added, "\n    "), goldenListResourceSchemas)
	}
}

// listResourceSchemaFacts renders every registered list resource's config
// schema as sorted lines. It reuses listConfigShapes' registration walk rather
// than repeating it, so the two gates cannot disagree about which surfaces
// exist.
func listResourceSchemaFacts(ctx context.Context, t *testing.T) []string {
	t.Helper()
	facts := []string{}
	for surface, schema := range listConfigSchemas(ctx, t) {
		if description := schema.MarkdownDescription; description != "" {
			facts = append(facts, fmt.Sprintf("%s  schema  -  %s", surface, description))
		}
		facts = append(facts, listAttributeFacts(t, surface, "", schema.Attributes)...)
		for name, block := range schema.Blocks {
			nested, ok := block.(listschema.ListNestedBlock)
			if !ok {
				t.Fatalf("%s: block %q is %T; every list resource here uses a list-nested filter",
					surface, name, block)
			}
			if description := nested.MarkdownDescription; description != "" {
				facts = append(facts, fmt.Sprintf("%s.%s  block  -  %s", surface, name, description))
			}
			facts = append(facts, listAttributeFacts(t, surface, name+".", nested.NestedObject.Attributes)...)
		}
	}
	sort.Strings(facts)
	return facts
}

func listAttributeFacts(
	t *testing.T, surface, prefix string, attributes map[string]listschema.Attribute,
) []string {
	t.Helper()
	facts := make([]string, 0, len(attributes))
	for _, name := range sortedListAttributes(attributes) {
		attribute := attributes[name]
		// A missing description is recorded as such rather than skipped. An
		// omitted line and an empty description would be the same absence here,
		// and the whole point of this file is that losing prose is visible.
		description := attribute.GetMarkdownDescription()
		if description == "" {
			description = "(no description)"
		}
		facts = append(facts, fmt.Sprintf("%s.%s%s  %s  %s  %s",
			surface, prefix, name,
			describeListAttribute(t, surface, prefix+name, attribute),
			listDisposition(attribute),
			description))

		// The plain Description is recorded separately, and only when it differs
		// from the markdown one. Every hand-written list schema in this provider
		// sets MarkdownDescription alone, so today this emits nothing — while
		// tfplugingen-framework's resource output sets BOTH. An emitter written to
		// match that convention would start populating a practitioner-visible
		// field that was empty in the released provider, and every other gate
		// would stay green: the shape is unchanged and the markdown text is
		// unchanged. These lines appear as additions the moment that happens.
		if plain := attribute.GetDescription(); plain != description && plain != "" {
			facts = append(facts, fmt.Sprintf("%s.%s%s  plain-description  -  %s",
				surface, prefix, name, plain))
		}
	}
	return facts
}

// listDisposition is separate from describeListAttribute's rendering so a
// sensitive or deprecated attribute is recorded too. Both are practitioner
// visible and neither appears in the uniformity gate's shape string.
func listDisposition(attribute listschema.Attribute) string {
	parts := []string{}
	if attribute.IsSensitive() {
		parts = append(parts, "sensitive")
	}
	if attribute.GetDeprecationMessage() != "" {
		parts = append(parts, "deprecated")
	}
	if len(parts) == 0 {
		return "-"
	}
	return strings.Join(parts, "+")
}
