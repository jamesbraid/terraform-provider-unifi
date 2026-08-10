package unifi

import (
	"context"
	"encoding/json"
	"testing"

	listschema "github.com/hashicorp/terraform-plugin-framework/list/schema"
)

// TestListResourceSchemaMatchesReleasedBaseline compares every served list
// config schema against the released schema projection.
//
// It closes the gap that made a declared baseline_digests.list_resource
// vacuous. The compiler cross-checks that digest against the digest manifest,
// which is a comparison of two records of the released schema with each other:
// both sides move together and neither is the schema the compiler just built.
// A policy that omitted the filter block entirely compiled to exit 0 with that
// check satisfied, which is how the hole was found.
//
// The compiler cannot close it. It emits a provider code specification, not a
// Terraform schema, and turning one into the other is precisely what this
// package does by building the framework schema and projecting it. So the
// missing comparison was never a missing branch in validateBaseline -- it was a
// missing layer, and this is that layer.
//
// It is deliberately a SECOND referee alongside
// TestListResourceConfigSchemaGolden rather than a replacement. The golden pins
// what the provider serves, and it was cut from the provider itself: it proves
// generation reproduced what was there, but a reader a year from now cannot
// tell from the file whether "what was there" was the released contract or
// whatever happened to be in the tree that afternoon. This reads the contract
// directly, from the same released projection the managed surfaces are checked
// against. The two can disagree, which is the only reason their agreement is
// worth anything.
func TestListResourceSchemaMatchesReleasedBaseline(t *testing.T) {
	ctx := context.Background()

	baseline := loadBaselineSchemas(t)
	listSchemas, ok := baseline["list_resource_schemas"].(map[string]any)
	if !ok {
		t.Fatalf("%s: list_resource_schemas missing or not an object — the projection "+
			"predates list resources, and this test would otherwise pass by comparing "+
			"nothing", baselinePath)
	}

	served := listConfigSchemas(ctx, t)
	if len(served) == 0 {
		t.Fatal("the provider registers no list resources, so this test would compare nothing")
	}

	compared := 0
	for _, name := range sortedShapeNames(namesOf(served)) {
		entry, found := listSchemas[name].(map[string]any)
		if !found {
			t.Errorf("%s: registered by the provider, absent from the baseline (%s)",
				name, baselinePath)
			continue
		}
		block, _ := entry["block"].(map[string]any)

		want := baselineAttrFacts(t, name, baselineObject(block["attributes"]), "")
		for path, fact := range baselineBlockFacts(t, name, baselineObject(block["block_types"]), "") {
			want[path] = fact
		}

		schema := served[name]
		have := listAttrFacts(t, name, schema.Attributes, "")
		for path, fact := range listBlockFacts(t, name, schema.Blocks, "") {
			have[path] = fact
		}

		compareBaselineFacts(t, name, want, have)
		compared++
	}

	// A surface silently dropping out of the served set would otherwise reduce
	// this to a smaller comparison that still passes.
	if compared != len(served) {
		t.Errorf("compared %d of %d served list surfaces", compared, len(served))
	}
}

// listAttrFacts is frameworkAttrFacts for a list config schema. Separate rather
// than generic because the two schema packages share no attribute interface:
// listschema.Attribute and rschema.Attribute are distinct types that happen to
// carry the same accessors.
func listAttrFacts(
	t *testing.T, surface string, attrs map[string]listschema.Attribute, prefix string,
) map[string]attrFact {
	t.Helper()
	out := map[string]attrFact{}
	for name, a := range attrs {
		path := prefix + name

		fact := attrFact{
			Required:    a.IsRequired(),
			Optional:    a.IsOptional(),
			Computed:    a.IsComputed(),
			Sensitive:   a.IsSensitive(),
			WriteOnly:   a.IsWriteOnly(),
			Deprecation: a.GetDeprecationMessage(),
		}
		// Same rule as the managed projection: markdown wins where present,
		// which is what the baseline records.
		if md := a.GetMarkdownDescription(); md != "" {
			fact.DescriptionKind, fact.Description = "markdown", md
		} else {
			fact.DescriptionKind, fact.Description = "plain", a.GetDescription()
		}

		encoded, err := json.Marshal(a.GetType().TerraformType(context.Background()))
		if err != nil {
			t.Fatalf("%s: attribute %q: marshal terraform type: %v", surface, path, err)
		}
		fact.Type = string(encoded)
		out[path] = fact
	}
	return out
}

// listBlockFacts is the block half. Only list nesting appears in this estate,
// and an unrecognised block fails by name rather than being skipped -- a
// skipped block would be a missing fact, and a missing fact on both sides
// compares equal.
func listBlockFacts(
	t *testing.T, surface string, blocks map[string]listschema.Block, prefix string,
) map[string]attrFact {
	t.Helper()
	out := map[string]attrFact{}
	for name, block := range blocks {
		path := prefix + name
		nested, ok := block.(listschema.ListNestedBlock)
		if !ok {
			t.Fatalf("%s: block %q is %T; only list-nested blocks are projected here, "+
				"and skipping it would drop the fact from both sides of the comparison",
				surface, path, block)
		}

		fact := attrFact{
			NestingMode: "list",
			Deprecation: nested.GetDeprecationMessage(),
			IsBlock:     true,
		}
		if md := nested.GetMarkdownDescription(); md != "" {
			fact.DescriptionKind, fact.Description = "markdown", md
		} else {
			fact.DescriptionKind, fact.Description = "plain", nested.GetDescription()
		}
		out[path] = fact

		for key, member := range listAttrFacts(t, surface, nested.NestedObject.Attributes, path+".") {
			out[key] = member
		}
	}
	return out
}

func namesOf(m map[string]listschema.Schema) map[string]string {
	out := make(map[string]string, len(m))
	for k := range m {
		out[k] = k
	}
	return out
}
