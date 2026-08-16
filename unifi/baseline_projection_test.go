package unifi

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	rschema "github.com/hashicorp/terraform-plugin-framework/resource/schema"
)

// baselinePath is the released v0.101.2 projection: the provider_schemas
// subtree of raw CLI output, exactly what schemabaseline.Canonicalize returns.
const (
	baselinePath = "../provider-contracts/schema/terraform-1.15.8.json"
	// baselineDisplayPath is repo-relative, because a failure message read in
	// CI output should name the file as the repository does.
	baselineDisplayPath = "provider-contracts/schema/terraform-1.15.8.json"
)

// attrFact is one attribute's public contract, flattened to a path key so
// nesting needs no recursion in the comparison itself.
//
// Type carries the tftypes MarshalJSON encoding rather than a name this
// package invents, and is empty for a nested attribute, which describes its
// shape through NestingMode and its children instead.
type attrFact struct {
	Required        bool
	Optional        bool
	Computed        bool
	Sensitive       bool
	WriteOnly       bool
	Type            string
	Description     string
	DescriptionKind string
	NestingMode     string
	// Deprecation is the message, not a bool: the baseline carries both a
	// deprecated flag and its text, and losing the text is as much a public
	// change as losing the flag.
	Deprecation string
	// IsBlock separates a block from a nested attribute. Both carry a nesting
	// mode, so without this a block silently turning into an attribute of the
	// same name and shape would compare equal — and that is a protocol-level
	// change, since configuration written for one does not parse as the other.
	IsBlock bool
}

// TestBuiltSchemaMatchesReleasedBaseline compares each registered surface's
// in-process Framework schema against the released v0.101.2 projection in
// provider-contracts/schema/terraform-1.15.8.json.
//
// It exists because `go generate` + `git diff --exit-code` proves only that
// generation is deterministic, not that it produced the same public schema.
// A policy edit that regenerates cleanly and keeps its declared
// baseline_digests self-consistent can still move the schema; nothing in
// `go test ./...` catches that today.
//
// THIS IS NOT THE COMPATIBILITY GATE. It compares semantic facts, not the
// wire shape, and deliberately does not reproduce Terraform's tfjson byte
// encoding -- doing so would be a second implementation of protov6
// marshalling and a second source of truth. Reproducing the digest means
// emitting the exact key set, description vs description_kind, the omission
// of required/optional/computed when false, nested_type vs block_types
// placement, and every protocol field added in future. That second encoder
// drifts, and when it does it fails one of two ways: false failures that
// teach people to regenerate the golden, or false passes that let real drift
// through. It cannot detect byte-shape-only differences, cannot prove
// Terraform and OpenTofu agree, and reads a checked-in snapshot rather than a
// freshly built binary.
//
// The real gate is the dual-CLI installed-binary comparison in
// .woodpecker/scripts/m1-dns-compiler.sh and catalog-build-schema.sh.
// This test fails fast at authoring time so that gate is not where a
// schema change is first discovered.
//
// Coverage today is managed resources, their attributes at any depth, and their
// blocks. Data sources, identity and list-resource schemas are separate fact
// sets and are not read here.
func TestBuiltSchemaMatchesReleasedBaseline(t *testing.T) {
	ctx := context.Background()

	baseline := loadBaselineSchemas(t)
	resourceSchemas, ok := baseline["resource_schemas"].(map[string]any)
	if !ok {
		t.Fatalf("%s: resource_schemas missing or not an object", baselinePath)
	}

	for _, newResource := range (&unifiProvider{}).Resources(ctx) {
		res := newResource()

		var meta resource.MetadataResponse
		res.Metadata(ctx, resource.MetadataRequest{ProviderTypeName: "unifi"}, &meta)
		name := meta.TypeName

		// The kernel's Schema is the subject, not the generated function.
		// Attributes marked provider_owned with "generated": false -- timeouts
		// among them -- are grafted here and are absent from the generated
		// schema, so comparing that instead reports a spurious missing
		// attribute on every surface.
		var got resource.SchemaResponse
		res.Schema(ctx, resource.SchemaRequest{}, &got)

		entry, found := resourceSchemas[name].(map[string]any)
		if !found {
			t.Errorf("%s: registered by the provider, absent from the baseline (%s)", name, baselinePath)
			continue
		}
		block, _ := entry["block"].(map[string]any)

		compareBaselineVersion(t, name, got.Schema.Version, entry["version"])
		compareBaselineRootDescription(t, name, got.Schema, block)

		want := baselineAttrFacts(t, name, baselineObject(block["attributes"]), "")
		for path, fact := range baselineBlockFacts(t, name, baselineObject(block["block_types"]), "") {
			want[path] = fact
		}
		have := frameworkAttrFacts(ctx, t, name, got.Schema.Attributes, "")
		for path, fact := range frameworkBlockFacts(ctx, t, name, got.Schema.Blocks, "") {
			have[path] = fact
		}
		compareBaselineFacts(t, name, want, have)
	}
}

// baselineBlockFacts projects block_types into the same fact set as attributes.
//
// Blocks used to be reported as uncovered and passed over. That was safe only
// while every surface was hand-written: a generated schema simply omits what the
// compiler cannot describe, and the compiler has no concept of a block, so
// migrating one of the three surfaces that carry them would have deleted the
// block and left this test logging its usual note and passing.
func baselineBlockFacts(t *testing.T, surface string, blocks map[string]any, prefix string) map[string]attrFact {
	t.Helper()
	out := map[string]attrFact{}
	for name, raw := range blocks {
		declaration := baselineObject(raw)
		path := prefix + name
		inner := baselineObject(declaration["block"])

		out[path] = attrFact{
			NestingMode:     baselineString(declaration["nesting_mode"]),
			Description:     baselineString(inner["description"]),
			DescriptionKind: baselineString(inner["description_kind"]),
			Deprecation:     baselineString(inner["deprecation_message"]),
			IsBlock:         true,
		}
		for key, fact := range baselineAttrFacts(t, surface, baselineObject(inner["attributes"]), path+".") {
			out[key] = fact
		}
		for key, fact := range baselineBlockFacts(t, surface, baselineObject(inner["block_types"]), path+".") {
			out[key] = fact
		}
	}
	return out
}

// frameworkBlockFacts is the in-process counterpart. Nesting mode comes from the
// concrete type, as it does for nested attributes.
func frameworkBlockFacts(
	ctx context.Context,
	t *testing.T,
	surface string,
	blocks map[string]rschema.Block,
	prefix string,
) map[string]attrFact {
	t.Helper()
	out := map[string]attrFact{}
	for name, block := range blocks {
		path := prefix + name

		fact := attrFact{
			Description:     block.GetDescription(),
			DescriptionKind: "plain",
			Deprecation:     block.GetDeprecationMessage(),
			IsBlock:         true,
		}
		if markdown := block.GetMarkdownDescription(); markdown != "" {
			fact.DescriptionKind, fact.Description = "markdown", markdown
		}

		var attributes map[string]rschema.Attribute
		var nested map[string]rschema.Block
		switch shaped := block.(type) {
		case rschema.ListNestedBlock:
			fact.NestingMode = "list"
			attributes, nested = shaped.NestedObject.Attributes, shaped.NestedObject.Blocks
		case rschema.SetNestedBlock:
			fact.NestingMode = "set"
			attributes, nested = shaped.NestedObject.Attributes, shaped.NestedObject.Blocks
		case rschema.SingleNestedBlock:
			fact.NestingMode = "single"
			attributes, nested = shaped.Attributes, shaped.Blocks
		default:
			t.Fatalf("%s: block %q has unhandled type %T", surface, path, block)
		}

		out[path] = fact
		for key, value := range frameworkAttrFacts(ctx, t, surface, attributes, path+".") {
			out[key] = value
		}
		for key, value := range frameworkBlockFacts(ctx, t, surface, nested, path+".") {
			out[key] = value
		}
	}
	return out
}

func loadBaselineSchemas(t *testing.T) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(filepath.Clean(baselinePath))
	if err != nil {
		t.Fatalf("read baseline: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parse %s: %v", baselinePath, err)
	}
	return doc
}

func compareBaselineVersion(t *testing.T, surface string, have int64, rawWant any) {
	t.Helper()
	var want int64
	if f, ok := rawWant.(float64); ok {
		want = int64(f)
	}
	if have != want {
		t.Errorf("%s: schema version\n%s", surface, baselineMismatch(fmt.Sprint(want), fmt.Sprint(have)))
	}
}

// compareBaselineRootDescription applies the same markdown-else-plain rule the
// attributes use. Four surfaces set the plain Description at the root rather
// than the markdown one, and the baseline records description_kind
// accordingly, so reading only MarkdownDescription reports every one of them
// as having lost its description.
func compareBaselineRootDescription(t *testing.T, surface string, s rschema.Schema, block map[string]any) {
	deprecation := s.DeprecationMessage
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
	compareBaselineDeprecation(t, surface, deprecation, block)
}

// compareBaselineDeprecation guards the surface-level deprecation notice. A
// deprecation that quietly disappears is a public change in the same way an
// attribute vanishing is, and one surface carries one today.
func compareBaselineDeprecation(t *testing.T, surface, have string, block map[string]any) {
	t.Helper()
	if want := baselineString(block["deprecation_message"]); have != want {
		t.Errorf("%s: block deprecation_message\n%s", surface, baselineMismatch(want, have))
	}
}

// baselineAttrFacts projects the checked-in JSON into the fact set. It reads
// the snapshot rather than re-deriving it, so neither side reimplements the
// other's serialization.
func baselineAttrFacts(t *testing.T, surface string, attrs map[string]any, prefix string) map[string]attrFact {
	t.Helper()
	out := map[string]attrFact{}
	for name, rawAttr := range attrs {
		a := baselineObject(rawAttr)
		path := prefix + name

		fact := attrFact{
			Required:        baselineBool(a["required"]),
			Optional:        baselineBool(a["optional"]),
			Computed:        baselineBool(a["computed"]),
			Sensitive:       baselineBool(a["sensitive"]),
			WriteOnly:       baselineBool(a["write_only"]),
			Description:     baselineString(a["description"]),
			DescriptionKind: baselineString(a["description_kind"]),
			Deprecation:     baselineString(a["deprecation_message"]),
		}

		if nested, ok := a["nested_type"].(map[string]any); ok {
			fact.NestingMode = baselineString(nested["nesting_mode"])
			out[path] = fact
			for k, v := range baselineAttrFacts(t, surface, baselineObject(nested["attributes"]), path+".") {
				out[k] = v
			}
			continue
		}

		// Re-marshalled so the comparison is against a canonical encoding
		// rather than the snapshot's incidental whitespace.
		encoded, err := json.Marshal(a["type"])
		if err != nil {
			t.Fatalf("%s: attribute %q: re-marshal baseline type: %v", surface, path, err)
		}
		fact.Type = string(encoded)
		out[path] = fact
	}
	return out
}

// frameworkAttrFacts projects the in-process schema into the same fact set.
//
// Nested attributes are reached by type-switching on the public concrete
// types. GetNestedObject and GetNestingMode live on an internal interface, but
// the concrete types expose their children directly and the nesting mode is
// implied by the arm.
func frameworkAttrFacts(
	ctx context.Context,
	t *testing.T,
	surface string,
	attrs map[string]rschema.Attribute,
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
		// Generated code sets both descriptions to the same string, so
		// markdown wins where present -- which is what the baseline records.
		if md := a.GetMarkdownDescription(); md != "" {
			fact.DescriptionKind, fact.Description = "markdown", md
		} else {
			fact.DescriptionKind, fact.Description = "plain", a.GetDescription()
		}

		var children map[string]rschema.Attribute
		switch nested := a.(type) {
		case rschema.SingleNestedAttribute:
			fact.NestingMode, children = "single", nested.Attributes
		case rschema.ListNestedAttribute:
			fact.NestingMode, children = "list", nested.NestedObject.Attributes
		case rschema.SetNestedAttribute:
			fact.NestingMode, children = "set", nested.NestedObject.Attributes
		case rschema.MapNestedAttribute:
			fact.NestingMode, children = "map", nested.NestedObject.Attributes
		}

		if fact.NestingMode != "" {
			out[path] = fact
			for k, v := range frameworkAttrFacts(ctx, t, surface, children, path+".") {
				out[k] = v
			}
			continue
		}

		// tftypes.Type implements MarshalJSON -- the encoder Terraform itself
		// uses. A hand-rolled type-name switch here would be a second source
		// of truth for the wire shape.
		encoded, err := json.Marshal(a.GetType().TerraformType(ctx))
		if err != nil {
			t.Fatalf("%s: attribute %q: marshal terraform type: %v", surface, path, err)
		}
		fact.Type = string(encoded)
		out[path] = fact
	}
	return out
}

// compareBaselineFacts reports one line per moved fact, naming the surface, the
// attribute path and the single field that differs. Diffing whole structs is
// unreadable on a resource with fifty-odd attributes.
func compareBaselineFacts(t *testing.T, surface string, want, have map[string]attrFact) {
	t.Helper()

	// Deliberate, declared changes are licensed one named transition at a time.
	// See baseline_schema_change_ledger_test.go for why this exists and what an
	// entry has to argue.
	declared := loadSchemaChangeLedger(t)

	for _, path := range sortedFactPaths(have) {
		if _, ok := want[path]; !ok {
			t.Errorf("%s: attribute %q present in built schema, absent from baseline", surface, path)
		}
	}
	for _, path := range sortedFactPaths(want) {
		if _, ok := have[path]; !ok {
			t.Errorf("%s: attribute %q present in baseline, absent from built schema", surface, path)
			continue
		}
		w, h := want[path], have[path]

		for _, f := range []struct {
			field      string
			want, have string
		}{
			{"required", fmt.Sprint(w.Required), fmt.Sprint(h.Required)},
			{"optional", fmt.Sprint(w.Optional), fmt.Sprint(h.Optional)},
			{"computed", fmt.Sprint(w.Computed), fmt.Sprint(h.Computed)},
			{"sensitive", fmt.Sprint(w.Sensitive), fmt.Sprint(h.Sensitive)},
			{"write_only", fmt.Sprint(w.WriteOnly), fmt.Sprint(h.WriteOnly)},
			{"type", w.Type, h.Type},
			{"description", w.Description, h.Description},
			{"description_kind", w.DescriptionKind, h.DescriptionKind},
			{"nesting_mode", w.NestingMode, h.NestingMode},
			{"is_block", fmt.Sprint(w.IsBlock), fmt.Sprint(h.IsBlock)},
			{"deprecation_message", w.Deprecation, h.Deprecation},
		} {
			if f.want != f.have {
				if schemaChangeDeclared(declared, surface, path, f.field, f.want, f.have) {
					t.Logf("%s: attribute %q: %s changed by declaration in %s",
						surface, path, f.field, schemaChangeLedgerPath)
					continue
				}
				t.Errorf("%s: attribute %q: %s\n%s", surface, path, f.field, baselineMismatch(f.want, f.have))
			}
		}
	}
}

// baselineMismatch renders the two readings aligned in one column and says which one
// is authoritative, so nobody resolves a failure by editing the baseline.
func baselineMismatch(want, have string) string {
	baselineLabel := fmt.Sprintf("  baseline (%s): ", baselineDisplayPath)
	builtLabel := "  built schema:"
	if pad := len(baselineLabel) - len(builtLabel); pad > 0 {
		builtLabel += strings.Repeat(" ", pad)
	}
	return fmt.Sprintf(
		"%s%s\n%s%s\n\n"+
			"The released v0.101.2 schema is authoritative. If this change is\n"+
			"intentional, declare it in\n"+
			"provider-codegen/schema-changes/v0.101.2-to-next.json: an entry names\n"+
			"the surface, attribute and field, carries the exact old and new values,\n"+
			"and must argue why the change cannot break an existing configuration.\n"+
			"Do not edit the baseline.\n"+
			"\n"+
			"An entry licenses ONE named transition. Drifting to a third value fails\n"+
			"here again, which is deliberate.",
		baselineLabel, want, builtLabel, have,
	)
}

func sortedFactPaths(m map[string]attrFact) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func baselineObject(v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return map[string]any{}
}

func baselineBool(v any) bool {
	b, _ := v.(bool)
	return b
}

func baselineString(v any) string {
	s, _ := v.(string)
	return s
}
