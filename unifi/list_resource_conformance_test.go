package unifi

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/list"
	listschema "github.com/hashicorp/terraform-plugin-framework/list/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
)

// The list-resource surfaces are GENERATED, like every other surface, from a
// listresources member of the provider code specification.
//
// Nothing upstream does that. terraform-plugin-codegen-spec v0.2.0 carries
// exactly DataSources, Provider, Resources and Version, and
// tfplugingen-framework v0.4.1 offers all, data-sources, provider and resources
// as subcommands: there is no member to emit into and no step to call, at any
// published version. Both halves are ours — the member is added in
// internal/providercompiler, and cmd/list-resource-gen renders it into Go.
//
// This test predates that emitter and is what made building it safe. Twenty-two
// of the twenty-five config schemas are structurally identical; three differ,
// each by exactly one top-level attribute. Written against the hand-written
// schemas, it turned "they are uniform" from an observation someone made once
// into a measurement, which is what justified a straight-line emitter over a
// general templating layer. It keeps that job now: a generated surface that
// comes out the wrong shape fails here by name.
//
// WHAT IS COMPARED: the attribute set, each attribute's type and disposition,
// and the filter block's structure. WHAT IS NOT: prose. Measured across the
// twenty-five, there are twenty-five distinct schema descriptions, twenty-four
// distinct site descriptions and thirteen distinct filter.name descriptions —
// all correct, because each names its own surface and its own supported filter
// keys. Pinning them here would produce a test that fights every wording change
// while catching nothing structural.
//
// Prose is not therefore unchecked. TestListResourceConfigSchemaGolden pins all
// hundred and one of those strings, which is what a generated list schema needs:
// structure is easy to reproduce and easy to check, prose is neither, and a
// schema that is structurally perfect and silently undescribed would pass every
// gate but that one.
//
// This test replaced TestListResourceConfigSchemasMatchOneScaffold, which read
// the same four facts — the surface count, the canonical shape, the three named
// outliers and the filter block's members — out of the package's AST by looking
// for a listschema.Schema literal in each ListResourceConfigSchema method. That
// worked while every schema was a literal in this package and stops working the
// moment one is generated, because there is no literal left to find. It is not
// a coverage loss: this test reads the same facts from the schema the provider
// actually serves, which is what a practitioner gets and is indifferent to
// where the Go came from. The one thing the AST version could see and this one
// cannot is a list resource defined in the package but never registered with
// the provider — a surface that serves nothing to anybody.

// canonicalListShape is the shape twenty-two surfaces share: one optional site
// string, and a filter block whose members are two required strings.
const canonicalListShape = "attributes[site:string:optional] " +
	"blocks[filter:list_nested[name:string:required value:string:required]]"

// listOutliers are the three surfaces that differ, with the whole shape stated
// rather than the difference. Stating the difference would let the rest of the
// shape drift unnoticed underneath a test that only checked the exception.
var listOutliers = map[string]string{
	// A client is listed within a group as well as a site.
	"unifi_client": "attributes[group:string:optional site:string:optional] " +
		"blocks[filter:list_nested[name:string:required value:string:required]]",
	// Sites are not scoped by a site.
	"unifi_site": "attributes[] " +
		"blocks[filter:list_nested[name:string:required value:string:required]]",
	// A peer must be listed within a network, so network_id is required rather
	// than optional — the one disposition difference in the whole set.
	"unifi_wireguard_peer": "attributes[network_id:string:required site:string:optional] " +
		"blocks[filter:list_nested[name:string:required value:string:required]]",
}

func TestListResourceConfigSchemasAreUniform(t *testing.T) {
	ctx := context.Background()
	shapes := listConfigShapes(ctx, t)

	if len(shapes) != 25 {
		t.Fatalf("the provider registers %d list resources, want 25 — a surface was added "+
			"or removed, so this count, the outlier table below and the golden inventory "+
			"in testdata/list_resource_schemas.txt all describe a provider that no longer "+
			"exists", len(shapes))
	}

	canonical := 0
	for _, name := range sortedShapeNames(shapes) {
		shape := shapes[name]
		want, isOutlier := listOutliers[name]
		if !isOutlier {
			want = canonicalListShape
		}
		if shape == want {
			if !isOutlier {
				canonical++
			}
			continue
		}
		if isOutlier {
			t.Errorf("%s is a known outlier and has drifted further:\n  want %s\n  got  %s\n\n"+
				"    Its recorded shape is the whole schema, not the difference, so this fires\n"+
				"    when any part of it moves — not only the attribute that made it an outlier.",
				name, want, shape)
			continue
		}
		t.Errorf("%s no longer matches the shape the other list resources share:\n"+
			"  want %s\n  got  %s\n\n"+
			"    cmd/list-resource-gen is a straight-line emitter rather than a general\n"+
			"    templating layer, and this uniformity is the measurement that justified\n"+
			"    that. A fourth distinct shape means the emitter is now carrying an\n"+
			"    assumption it was not measured against: either bring this surface back\n"+
			"    to the shared shape, or record it in listOutliers with its whole shape\n"+
			"    and say why.",
			name, canonicalListShape, shape)
	}

	if canonical != 22 {
		t.Errorf("%d surfaces match the canonical shape, want 22 — the uniformity that "+
			"justified a straight-line emitter over a general templating layer has changed",
			canonical)
	}
}

// listConfigShapes renders each registered list resource's config schema as a
// comparable string. A rendering rather than a struct compare, so a failure
// prints what differs instead of two nested values a reader has to diff.
func listConfigShapes(ctx context.Context, t *testing.T) map[string]string {
	t.Helper()
	out := map[string]string{}
	for surface, schema := range listConfigSchemas(ctx, t) {
		out[surface] = renderListSchema(t, surface, schema)
	}
	return out
}

// listConfigSchemas walks the provider's list-resource registrations once and
// returns each surface's schema unrendered. Both list gates read it, so they
// cannot come to disagree about which surfaces exist -- a surface dropped from
// the provider must not be able to vanish from one gate while the other still
// counts it.
func listConfigSchemas(ctx context.Context, t *testing.T) map[string]listschema.Schema {
	t.Helper()
	out := map[string]listschema.Schema{}
	for _, newListResource := range (&unifiProvider{}).ListResources(ctx) {
		listResource := newListResource()

		var meta resource.MetadataResponse
		withMetadata, ok := listResource.(interface {
			Metadata(context.Context, resource.MetadataRequest, *resource.MetadataResponse)
		})
		if !ok {
			t.Fatalf("%T does not report metadata, so its surface cannot be named", listResource)
		}
		withMetadata.Metadata(ctx, resource.MetadataRequest{ProviderTypeName: "unifi"}, &meta)

		var got list.ListResourceSchemaResponse
		listResource.ListResourceConfigSchema(ctx, list.ListResourceSchemaRequest{}, &got)
		if _, exists := out[meta.TypeName]; exists {
			t.Fatalf("%s is registered as a list resource twice", meta.TypeName)
		}
		out[meta.TypeName] = got.Schema
	}
	return out
}

func renderListSchema(t *testing.T, surface string, schema listschema.Schema) string {
	t.Helper()
	attributes := make([]string, 0, len(schema.Attributes))
	for _, name := range sortedListAttributes(schema.Attributes) {
		attributes = append(attributes, name+":"+describeListAttribute(t, surface, name, schema.Attributes[name]))
	}

	blocks := make([]string, 0, len(schema.Blocks))
	for _, name := range sortedListBlocks(schema.Blocks) {
		nested, ok := schema.Blocks[name].(listschema.ListNestedBlock)
		if !ok {
			t.Fatalf("%s: block %q is %T; every list resource here uses a list-nested filter",
				surface, name, schema.Blocks[name])
		}
		members := make([]string, 0, len(nested.NestedObject.Attributes))
		for _, member := range sortedListAttributes(nested.NestedObject.Attributes) {
			members = append(members, member+":"+
				describeListAttribute(t, surface, name+"."+member, nested.NestedObject.Attributes[member]))
		}
		blocks = append(blocks, fmt.Sprintf("%s:list_nested[%s]", name, strings.Join(members, " ")))
	}

	return fmt.Sprintf("attributes[%s] blocks[%s]",
		strings.Join(attributes, " "), strings.Join(blocks, " "))
}

// describeListAttribute renders type and disposition. An attribute that is
// neither required nor optional nor computed is rendered as such rather than
// omitted, so a disposition lost entirely shows up as a difference.
func describeListAttribute(t *testing.T, surface, path string, attribute listschema.Attribute) string {
	t.Helper()
	kind := "string"
	if _, ok := attribute.(listschema.StringAttribute); !ok {
		kind = fmt.Sprintf("%T", attribute)
	}
	dispositions := []string{}
	if attribute.IsRequired() {
		dispositions = append(dispositions, "required")
	}
	if attribute.IsOptional() {
		dispositions = append(dispositions, "optional")
	}
	if attribute.IsComputed() {
		dispositions = append(dispositions, "computed")
	}
	if len(dispositions) == 0 {
		dispositions = append(dispositions, "no_disposition")
	}
	return kind + ":" + strings.Join(dispositions, "+")
}

func sortedListAttributes(m map[string]listschema.Attribute) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func sortedListBlocks(m map[string]listschema.Block) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func sortedShapeNames(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
