package unifi

import (
	"context"
	"encoding/json"
	"sort"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	dschema "github.com/hashicorp/terraform-plugin-framework/datasource/schema"
)

// TestBuiltDataSourceSchemasMatchReleasedBaseline is the data source half of
// TestBuiltSchemaMatchesReleasedBaseline, sharing its fact record, comparison
// core and failure formatter. Read that test's doc comment first: the same
// limits apply, and for the same reasons. This is not the compatibility gate.
//
// Data sources differ from managed resources in one way that matters.
// datasource.Schema has no Version field, so there is no version to compare.
// The baseline still records "version": 0 for all thirteen, because the
// protocol emits the field regardless, and comparing a Go zero against it
// would pass by coincidence rather than by agreement. Asserting a coincidence
// is worse than asserting nothing, so this test says nothing about version and
// the doc comment says so.
//
// Deprecation is compared at both levels, surface and attribute, because one
// data source carries a deprecation notice today and losing it silently would
// be a public change.
//
// Coverage is every registered data source and its attributes, nested to any
// depth. No data source carries blocks today; if one gains them this test
// reports the surface by name rather than passing silently over it.
func TestBuiltDataSourceSchemasMatchReleasedBaseline(t *testing.T) {
	ctx := context.Background()

	baseline := loadBaselineSchemas(t)
	dataSourceSchemas, ok := baseline["data_source_schemas"].(map[string]any)
	if !ok {
		t.Fatalf("%s: data_source_schemas missing or not an object", baselinePath)
	}

	var (
		uncoveredBlocks []string
		compared        int
	)

	for _, newDataSource := range (&unifiProvider{}).DataSources(ctx) {
		ds := newDataSource()

		var meta datasource.MetadataResponse
		ds.Metadata(ctx, datasource.MetadataRequest{ProviderTypeName: "unifi"}, &meta)
		name := meta.TypeName

		var got datasource.SchemaResponse
		ds.Schema(ctx, datasource.SchemaRequest{}, &got)

		entry, found := dataSourceSchemas[name].(map[string]any)
		if !found {
			t.Errorf("%s: registered by the provider, absent from the baseline (%s)", name, baselinePath)
			continue
		}
		block, _ := entry["block"].(map[string]any)

		if len(baselineObject(block["block_types"])) > 0 {
			uncoveredBlocks = append(uncoveredBlocks, name)
		}

		compareDataSourceRootDescription(t, name, got.Schema, block)

		want := baselineAttrFacts(t, name, baselineObject(block["attributes"]), "")
		have := frameworkDataSourceAttrFacts(ctx, t, name, got.Schema.Attributes, "")
		compareBaselineFacts(t, name, want, have)
		compared++
	}

	// A projection that silently reaches nothing passes vacuously. The count
	// is asserted so an empty or renamed baseline category fails loudly rather
	// than reporting success over zero surfaces.
	if compared != len(dataSourceSchemas) {
		t.Errorf("compared %d data sources, baseline declares %d -- the registered set and the baseline disagree",
			compared, len(dataSourceSchemas))
	}

	if len(uncoveredBlocks) > 0 {
		sort.Strings(uncoveredBlocks)
		t.Logf("blocks are not projected by this test; %d data source(s) carry block_types "+
			"whose contents nothing here compares: %v", len(uncoveredBlocks), uncoveredBlocks)
	}
}

func compareDataSourceRootDescription(t *testing.T, surface string, s dschema.Schema, block map[string]any) {
	t.Helper()

	haveKind, haveDesc := "plain", s.Description
	if s.MarkdownDescription != "" {
		haveKind, haveDesc = "markdown", s.MarkdownDescription
	}

	if wantDesc := baselineString(block["description"]); haveDesc != wantDesc {
		t.Errorf("%s: block description\n%s", surface, baselineMismatch(wantDesc, haveDesc))
	}
	if wantKind := baselineString(block["description_kind"]); haveKind != wantKind {
		t.Errorf("%s: block description_kind\n%s", surface, baselineMismatch(wantKind, haveKind))
	}
	compareBaselineDeprecation(t, surface, s.DeprecationMessage, block)
}

// frameworkDataSourceAttrFacts mirrors frameworkAttrFacts over the datasource
// schema types. The two cannot be shared: resource and datasource attributes
// are distinct interfaces with distinct concrete nested types, and the fact
// extraction is the one part §6 says differs per kind.
func frameworkDataSourceAttrFacts(
	ctx context.Context,
	t *testing.T,
	surface string,
	attrs map[string]dschema.Attribute,
	prefix string,
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
		if md := a.GetMarkdownDescription(); md != "" {
			fact.DescriptionKind, fact.Description = "markdown", md
		} else {
			fact.DescriptionKind, fact.Description = "plain", a.GetDescription()
		}

		var children map[string]dschema.Attribute
		switch nested := a.(type) {
		case dschema.SingleNestedAttribute:
			fact.NestingMode, children = "single", nested.Attributes
		case dschema.ListNestedAttribute:
			fact.NestingMode, children = "list", nested.NestedObject.Attributes
		case dschema.SetNestedAttribute:
			fact.NestingMode, children = "set", nested.NestedObject.Attributes
		case dschema.MapNestedAttribute:
			fact.NestingMode, children = "map", nested.NestedObject.Attributes
		}

		if fact.NestingMode != "" {
			out[path] = fact
			for k, v := range frameworkDataSourceAttrFacts(ctx, t, surface, children, path+".") {
				out[k] = v
			}
			continue
		}

		encoded, err := json.Marshal(a.GetType().TerraformType(ctx))
		if err != nil {
			t.Fatalf("%s: attribute %q: marshal terraform type: %v", surface, path, err)
		}
		fact.Type = string(encoded)
		out[path] = fact
	}
	return out
}
