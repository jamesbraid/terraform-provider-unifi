package unifi

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/action"
	actionschema "github.com/hashicorp/terraform-plugin-framework/action/schema"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	dschema "github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	listschema "github.com/hashicorp/terraform-plugin-framework/list/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	rschema "github.com/hashicorp/terraform-plugin-framework/resource/schema"
)

// schemaSnapshotPath is the committed record of the entire served provider
// schema: every resource, data source, list resource and action, at every
// attribute and block depth.
const schemaSnapshotPath = "testdata/schema-snapshot.json"

// updateSchemaSnapshotEnv regenerates schemaSnapshotPath. Unlike
// UPDATE_GOLDEN elsewhere in this package there is deliberately no removal
// guard: this is a full record, not an append-only inventory, and the diff
// on the committed file is the guard.
const updateSchemaSnapshotEnv = "UPDATE_SCHEMA_SNAPSHOT"

// schemaSnapshotFact is one attribute or block's complete contract, wire
// shape and framework-only behaviour together -- including Default and
// PlanModifiers, which the wire protocol cannot express.
type schemaSnapshotFact struct {
	Kind            string   `json:"kind"`
	Type            string   `json:"type,omitempty"`
	NestingMode     string   `json:"nesting_mode,omitempty"`
	Required        bool     `json:"required,omitempty"`
	Optional        bool     `json:"optional,omitempty"`
	Computed        bool     `json:"computed,omitempty"`
	Sensitive       bool     `json:"sensitive,omitempty"`
	WriteOnly       bool     `json:"write_only,omitempty"`
	Description     string   `json:"description,omitempty"`
	DescriptionKind string   `json:"description_kind,omitempty"`
	Deprecation     string   `json:"deprecation_message,omitempty"`
	Default         string   `json:"default,omitempty"`
	PlanModifiers   []string `json:"plan_modifiers,omitempty"`
	Validators      []string `json:"validators,omitempty"`
	CustomType      string   `json:"custom_type,omitempty"`
}

// schemaSnapshotSurface is one served resource, data source, list resource
// or action, with its attributes and blocks flattened to dotted paths so
// diffs report one path at a time.
type schemaSnapshotSurface struct {
	Version         int64                         `json:"version"`
	Description     string                        `json:"description,omitempty"`
	DescriptionKind string                        `json:"description_kind,omitempty"`
	Deprecation     string                        `json:"deprecation_message,omitempty"`
	Attributes      map[string]schemaSnapshotFact `json:"attributes,omitempty"`
}

// providerSchemaSnapshot is the entire served schema.
type providerSchemaSnapshot struct {
	Resources     map[string]schemaSnapshotSurface `json:"resources"`
	DataSources   map[string]schemaSnapshotSurface `json:"data_sources"`
	ListResources map[string]schemaSnapshotSurface `json:"list_resources"`
	Actions       map[string]schemaSnapshotSurface `json:"actions"`
}

// TestProviderSchemaSnapshot diffs the live provider schema against the
// committed snapshot at schemaSnapshotPath. A missing snapshot fails with
// the regeneration command rather than being silently created.
func TestProviderSchemaSnapshot(t *testing.T) {
	ctx := context.Background()
	got := buildSchemaSnapshot(ctx, t)

	if os.Getenv(updateSchemaSnapshotEnv) != "" {
		writeSchemaSnapshot(t, got)
		return
	}

	raw, err := os.ReadFile(schemaSnapshotPath)
	if err != nil {
		if os.IsNotExist(err) {
			t.Fatalf("%s does not exist yet.\n"+
				"    Run UPDATE_SCHEMA_SNAPSHOT=1 go test ./unifi/ -run TestProviderSchemaSnapshot\n"+
				"    to create it, review the diff, and commit it.",
				schemaSnapshotPath)
		}
		t.Fatalf("reading %s: %v", schemaSnapshotPath, err)
	}

	var want providerSchemaSnapshot
	if err := json.Unmarshal(raw, &want); err != nil {
		t.Fatalf("parsing %s: %v", schemaSnapshotPath, err)
	}

	if diffs := diffSchemaSnapshots(want, got); len(diffs) > 0 {
		t.Errorf("the served provider schema disagrees with %s at %d path(s):\n    %s\n\n"+
			"    If this change is intended, run\n"+
			"    UPDATE_SCHEMA_SNAPSHOT=1 go test ./unifi/ -run TestProviderSchemaSnapshot,\n"+
			"    review the diff to %s, and commit it.",
			schemaSnapshotPath, len(diffs), strings.Join(diffs, "\n    "), schemaSnapshotPath)
	}
}

// TestProviderSchemaSnapshotDetectsAMutation is the check's positive
// control: the comparison has to be shown failing on a document that
// actually differs.
func TestProviderSchemaSnapshotDetectsAMutation(t *testing.T) {
	ctx := context.Background()
	got := buildSchemaSnapshot(ctx, t)

	mutated := got
	mutated.Resources = map[string]schemaSnapshotSurface{}
	for name, surface := range got.Resources {
		mutated.Resources[name] = surface
	}

	var mutatedName string
	for name, surface := range mutated.Resources {
		if len(surface.Attributes) == 0 {
			continue
		}
		attrs := map[string]schemaSnapshotFact{}
		for path, fact := range surface.Attributes {
			attrs[path] = fact
		}
		var mutatedPath string
		for path, fact := range attrs {
			fact.Sensitive = !fact.Sensitive
			attrs[path] = fact
			mutatedPath = path
			break
		}
		surface.Attributes = attrs
		mutated.Resources[name] = surface
		mutatedName = name
		t.Logf("flipped %s.%s.sensitive for the control", name, mutatedPath)
		break
	}
	if mutatedName == "" {
		t.Fatal("no resource with at least one attribute was found to mutate; the control cannot run")
	}

	diffs := diffSchemaSnapshots(mutated, got)
	if len(diffs) == 0 {
		t.Fatal("mutating one attribute's sensitive flag produced no diff; the comparison " +
			"cannot go red, which means a real regression would pass silently too")
	}
	if len(diffs) != 1 {
		t.Fatalf("one mutated field produced %d diffs, want 1: %v", len(diffs), diffs)
	}
	if !strings.Contains(diffs[0], mutatedName) || !strings.Contains(diffs[0], "sensitive") {
		t.Errorf("the diff does not name the mutated surface and field: %q", diffs[0])
	}
}

// TestProviderSchemaSnapshotDetectsAValidatorsMutation is the same positive
// control, aimed at Validators specifically: that field folded the
// now-deleted schema_behaviour inventory's coverage into this snapshot, and
// this proves the comparison can still go red over it.
func TestProviderSchemaSnapshotDetectsAValidatorsMutation(t *testing.T) {
	ctx := context.Background()
	got := buildSchemaSnapshot(ctx, t)

	mutated := got
	mutated.Resources = map[string]schemaSnapshotSurface{}
	for name, surface := range got.Resources {
		mutated.Resources[name] = surface
	}

	var mutatedName string
	for name, surface := range mutated.Resources {
		attrs := map[string]schemaSnapshotFact{}
		for path, fact := range surface.Attributes {
			attrs[path] = fact
		}
		var mutatedPath string
		var found bool
		for path, fact := range attrs {
			if len(fact.Validators) == 0 {
				continue
			}
			fact.Validators = append([]string{"planted control validator"}, fact.Validators...)
			attrs[path] = fact
			mutatedPath = path
			found = true
			break
		}
		if !found {
			continue
		}
		surface.Attributes = attrs
		mutated.Resources[name] = surface
		mutatedName = name
		t.Logf("planted an extra validator on %s.%s for the control", name, mutatedPath)
		break
	}
	if mutatedName == "" {
		t.Fatal("no resource with at least one validator was found to mutate; the control cannot run")
	}

	diffs := diffSchemaSnapshots(mutated, got)
	if len(diffs) == 0 {
		t.Fatal("planting one extra validator produced no diff; a real validator regression " +
			"would pass silently too")
	}
	if len(diffs) != 1 {
		t.Fatalf("one mutated field produced %d diffs, want 1: %v", len(diffs), diffs)
	}
	if !strings.Contains(diffs[0], mutatedName) || !strings.Contains(diffs[0], "validators") {
		t.Errorf("the diff does not name the mutated surface and field: %q", diffs[0])
	}
}

// writeSchemaSnapshot regenerates schemaSnapshotPath. No removal guard --
// see updateSchemaSnapshotEnv.
func writeSchemaSnapshot(t *testing.T, snap providerSchemaSnapshot) {
	t.Helper()
	encoded, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		t.Fatalf("marshaling schema snapshot: %v", err)
	}
	if err := os.WriteFile(schemaSnapshotPath, append(encoded, '\n'), 0o644); err != nil {
		t.Fatalf("writing %s: %v", schemaSnapshotPath, err)
	}
	t.Logf("wrote schema snapshot to %s", schemaSnapshotPath)
}

// buildSchemaSnapshot serves every resource, data source, list resource and
// action the provider registers and projects each into a
// schemaSnapshotSurface.
func buildSchemaSnapshot(ctx context.Context, t *testing.T) providerSchemaSnapshot {
	t.Helper()
	snap := providerSchemaSnapshot{
		Resources:     map[string]schemaSnapshotSurface{},
		DataSources:   map[string]schemaSnapshotSurface{},
		ListResources: map[string]schemaSnapshotSurface{},
		Actions:       map[string]schemaSnapshotSurface{},
	}

	for _, newResource := range (&unifiProvider{}).Resources(ctx) {
		res := newResource()

		var meta resource.MetadataResponse
		res.Metadata(ctx, resource.MetadataRequest{ProviderTypeName: "unifi"}, &meta)

		var got resource.SchemaResponse
		res.Schema(ctx, resource.SchemaRequest{}, &got)
		if got.Diagnostics.HasError() {
			t.Fatalf("schema for %s: %v", meta.TypeName, got.Diagnostics)
		}

		attrs := snapshotResourceAttributes(ctx, t, got.Schema.Attributes, "")
		for path, fact := range snapshotResourceBlocks(ctx, t, got.Schema.Blocks, "") {
			attrs[path] = fact
		}

		descKind, desc := surfaceDescription(got.Schema.Description, got.Schema.MarkdownDescription)
		snap.Resources[meta.TypeName] = schemaSnapshotSurface{
			Version:         got.Schema.GetVersion(),
			Description:     desc,
			DescriptionKind: descKind,
			Deprecation:     got.Schema.DeprecationMessage,
			Attributes:      attrs,
		}
	}

	for _, newDataSource := range (&unifiProvider{}).DataSources(ctx) {
		ds := newDataSource()

		var meta datasource.MetadataResponse
		ds.Metadata(ctx, datasource.MetadataRequest{ProviderTypeName: "unifi"}, &meta)

		var got datasource.SchemaResponse
		ds.Schema(ctx, datasource.SchemaRequest{}, &got)
		if got.Diagnostics.HasError() {
			t.Fatalf("schema for %s: %v", meta.TypeName, got.Diagnostics)
		}

		attrs := snapshotDataSourceAttributes(ctx, t, got.Schema.Attributes, "")
		for path, fact := range snapshotDataSourceBlocks(ctx, t, got.Schema.Blocks, "") {
			attrs[path] = fact
		}

		descKind, desc := surfaceDescription(got.Schema.Description, got.Schema.MarkdownDescription)
		snap.DataSources[meta.TypeName] = schemaSnapshotSurface{
			Version:         got.Schema.GetVersion(),
			Description:     desc,
			DescriptionKind: descKind,
			Deprecation:     got.Schema.DeprecationMessage,
			Attributes:      attrs,
		}
	}

	for name, schema := range listConfigSchemas(ctx, t) {
		attrs := snapshotListAttributes(ctx, t, schema.Attributes, "")
		for path, fact := range snapshotListBlocks(ctx, t, schema.Blocks, "") {
			attrs[path] = fact
		}

		descKind, desc := surfaceDescription(schema.Description, schema.MarkdownDescription)
		snap.ListResources[name] = schemaSnapshotSurface{
			Version:         schema.GetVersion(),
			Description:     desc,
			DescriptionKind: descKind,
			Deprecation:     schema.DeprecationMessage,
			Attributes:      attrs,
		}
	}

	for _, newAction := range (&unifiProvider{}).Actions(ctx) {
		act := newAction()

		var meta action.MetadataResponse
		act.Metadata(ctx, action.MetadataRequest{ProviderTypeName: "unifi"}, &meta)

		var got action.SchemaResponse
		act.Schema(ctx, action.SchemaRequest{}, &got)
		if got.Diagnostics.HasError() {
			t.Fatalf("schema for %s: %v", meta.TypeName, got.Diagnostics)
		}

		attrs := snapshotActionAttributes(ctx, t, got.Schema.Attributes, "")
		for path, fact := range snapshotActionBlocks(ctx, t, got.Schema.Blocks, "") {
			attrs[path] = fact
		}

		descKind, desc := surfaceDescription(got.Schema.Description, got.Schema.MarkdownDescription)
		snap.Actions[meta.TypeName] = schemaSnapshotSurface{
			Version:         got.Schema.GetVersion(),
			Description:     desc,
			DescriptionKind: descKind,
			Deprecation:     got.Schema.DeprecationMessage,
			Attributes:      attrs,
		}
	}

	return snap
}

// surfaceDescription applies the markdown-else-plain rule every projection in
// this package uses: generated code sets both descriptions to the same
// string, so markdown wins where present.
func surfaceDescription(plain, markdown string) (kind, description string) {
	if markdown != "" {
		return "markdown", markdown
	}
	return "plain", plain
}

// snapshotAttribute is the method subset all four schema packages' Attribute
// interfaces share; structural satisfaction lets any of them convert to it,
// so commonAttributeFact needs one implementation, not four. Nesting mode
// and the leaf wire type still need the concrete per-package types.
type snapshotAttribute interface {
	IsRequired() bool
	IsOptional() bool
	IsComputed() bool
	IsSensitive() bool
	IsWriteOnly() bool
	GetDescription() string
	GetMarkdownDescription() string
	GetDeprecationMessage() string
	GetType() attr.Type
}

// snapshotBlock is the block equivalent of snapshotAttribute.
type snapshotBlock interface {
	GetDescription() string
	GetMarkdownDescription() string
	GetDeprecationMessage() string
}

// commonAttributeFact reads every field that means the same thing regardless
// of which schema package the attribute came from; the caller type-switches
// the concrete value for NestingMode and the leaf Type.
func commonAttributeFact(ctx context.Context, a snapshotAttribute) schemaSnapshotFact {
	fact := schemaSnapshotFact{
		Kind:        "attribute",
		Required:    a.IsRequired(),
		Optional:    a.IsOptional(),
		Computed:    a.IsComputed(),
		Sensitive:   a.IsSensitive(),
		WriteOnly:   a.IsWriteOnly(),
		Deprecation: a.GetDeprecationMessage(),
	}
	fact.DescriptionKind, fact.Description = surfaceDescription(a.GetDescription(), a.GetMarkdownDescription())
	fact.Default, fact.PlanModifiers, fact.Validators, fact.CustomType = behaviourFields(ctx, a)
	return fact
}

// commonBlockFact is commonAttributeFact for a block. Blocks carry no
// Default and no CustomType, but list/set/single-nested blocks all carry
// PlanModifiers and Validators, so the same reflection helper applies.
func commonBlockFact(ctx context.Context, nestingMode string, b snapshotBlock) schemaSnapshotFact {
	fact := schemaSnapshotFact{
		Kind:        "block",
		NestingMode: nestingMode,
		Deprecation: b.GetDeprecationMessage(),
	}
	fact.DescriptionKind, fact.Description = surfaceDescription(b.GetDescription(), b.GetMarkdownDescription())
	_, fact.PlanModifiers, fact.Validators, _ = behaviourFields(ctx, b)
	return fact
}

// behaviourFields reads the four fields a schema attribute or block carries
// that the wire protocol cannot express -- Validators, PlanModifiers,
// Default and CustomType -- off it by reflection, the same way
// schema_behaviour_test.go's behaviourOf did before this snapshot absorbed
// it: the two must have agreed on what counts, and now there is only one
// reading of it to keep current.
func behaviourFields(ctx context.Context, attribute any) (def string, mods, validators []string, customType string) {
	value := reflect.ValueOf(attribute)
	if value.Kind() == reflect.Pointer {
		value = value.Elem()
	}
	if value.Kind() != reflect.Struct {
		return "", nil, nil, ""
	}

	if found := value.FieldByName("Default"); found.IsValid() && found.Kind() == reflect.Interface && !found.IsNil() {
		def = behaviourDescription(ctx, found.Interface())
	}
	if found := value.FieldByName("PlanModifiers"); found.IsValid() && found.Kind() == reflect.Slice {
		for i := range found.Len() {
			mods = append(mods, behaviourDescription(ctx, found.Index(i).Interface()))
		}
	}
	if found := value.FieldByName("Validators"); found.IsValid() && found.Kind() == reflect.Slice {
		for i := range found.Len() {
			validators = append(validators, behaviourDescription(ctx, found.Index(i).Interface()))
		}
	}
	if found := value.FieldByName("CustomType"); found.IsValid() && found.Kind() == reflect.Interface && !found.IsNil() {
		customType = behaviourDescription(ctx, found.Interface())
	}
	return def, mods, validators, customType
}

// behaviourDescription renders one Default or PlanModifier value as its
// concrete Go type plus its own Description.
func behaviourDescription(ctx context.Context, behaviour any) string {
	description := "<carries no description>"
	if describer, ok := behaviour.(interface{ Description(context.Context) string }); ok {
		description = describer.Description(ctx)
	}
	return fmt.Sprintf("%T: %s", behaviour, description)
}

// snapshotResourceAttributes walks a resource schema's attributes to any
// nesting depth.
func snapshotResourceAttributes(
	ctx context.Context, t *testing.T, attrs map[string]rschema.Attribute, prefix string,
) map[string]schemaSnapshotFact {
	t.Helper()
	out := map[string]schemaSnapshotFact{}
	for name, a := range attrs {
		path := prefix + name
		fact := commonAttributeFact(ctx, a)

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
			for k, v := range snapshotResourceAttributes(ctx, t, children, path+".") {
				out[k] = v
			}
			continue
		}

		encoded, err := json.Marshal(a.GetType().TerraformType(ctx))
		if err != nil {
			t.Fatalf("attribute %q: marshal terraform type: %v", path, err)
		}
		fact.Type = string(encoded)
		out[path] = fact
	}
	return out
}

// snapshotResourceBlocks is the block half of snapshotResourceAttributes.
func snapshotResourceBlocks(
	ctx context.Context, t *testing.T, blocks map[string]rschema.Block, prefix string,
) map[string]schemaSnapshotFact {
	t.Helper()
	out := map[string]schemaSnapshotFact{}
	for name, block := range blocks {
		path := prefix + name

		var nestingMode string
		var attributes map[string]rschema.Attribute
		var nested map[string]rschema.Block
		switch shaped := block.(type) {
		case rschema.ListNestedBlock:
			nestingMode = "list"
			attributes, nested = shaped.NestedObject.Attributes, shaped.NestedObject.Blocks
		case rschema.SetNestedBlock:
			nestingMode = "set"
			attributes, nested = shaped.NestedObject.Attributes, shaped.NestedObject.Blocks
		case rschema.SingleNestedBlock:
			nestingMode = "single"
			attributes, nested = shaped.Attributes, shaped.Blocks
		default:
			t.Fatalf("block %q has unhandled type %T", path, block)
		}

		out[path] = commonBlockFact(ctx, nestingMode, block)
		for k, v := range snapshotResourceAttributes(ctx, t, attributes, path+".") {
			out[k] = v
		}
		for k, v := range snapshotResourceBlocks(ctx, t, nested, path+".") {
			out[k] = v
		}
	}
	return out
}

// snapshotDataSourceAttributes mirrors snapshotResourceAttributes over the
// datasource/schema types, which share no interface with resource/schema's.
func snapshotDataSourceAttributes(
	ctx context.Context, t *testing.T, attrs map[string]dschema.Attribute, prefix string,
) map[string]schemaSnapshotFact {
	t.Helper()
	out := map[string]schemaSnapshotFact{}
	for name, a := range attrs {
		path := prefix + name
		fact := commonAttributeFact(ctx, a)

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
			for k, v := range snapshotDataSourceAttributes(ctx, t, children, path+".") {
				out[k] = v
			}
			continue
		}

		encoded, err := json.Marshal(a.GetType().TerraformType(ctx))
		if err != nil {
			t.Fatalf("attribute %q: marshal terraform type: %v", path, err)
		}
		fact.Type = string(encoded)
		out[path] = fact
	}
	return out
}

// snapshotDataSourceBlocks is the block half. An unrecognised block fails by
// name rather than being skipped, so one added later cannot vanish from both
// sides of the comparison silently.
func snapshotDataSourceBlocks(
	ctx context.Context, t *testing.T, blocks map[string]dschema.Block, prefix string,
) map[string]schemaSnapshotFact {
	t.Helper()
	out := map[string]schemaSnapshotFact{}
	for name, block := range blocks {
		path := prefix + name

		var nestingMode string
		var attributes map[string]dschema.Attribute
		var nested map[string]dschema.Block
		switch shaped := block.(type) {
		case dschema.ListNestedBlock:
			nestingMode = "list"
			attributes, nested = shaped.NestedObject.Attributes, shaped.NestedObject.Blocks
		case dschema.SetNestedBlock:
			nestingMode = "set"
			attributes, nested = shaped.NestedObject.Attributes, shaped.NestedObject.Blocks
		case dschema.SingleNestedBlock:
			nestingMode = "single"
			attributes, nested = shaped.Attributes, shaped.Blocks
		default:
			t.Fatalf("block %q has unhandled type %T", path, block)
		}

		out[path] = commonBlockFact(ctx, nestingMode, block)
		for k, v := range snapshotDataSourceAttributes(ctx, t, attributes, path+".") {
			out[k] = v
		}
		for k, v := range snapshotDataSourceBlocks(ctx, t, nested, path+".") {
			out[k] = v
		}
	}
	return out
}

// snapshotActionAttributes mirrors snapshotResourceAttributes over
// action/schema.
func snapshotActionAttributes(
	ctx context.Context, t *testing.T, attrs map[string]actionschema.Attribute, prefix string,
) map[string]schemaSnapshotFact {
	t.Helper()
	out := map[string]schemaSnapshotFact{}
	for name, a := range attrs {
		path := prefix + name
		fact := commonAttributeFact(ctx, a)

		var children map[string]actionschema.Attribute
		switch nested := a.(type) {
		case actionschema.SingleNestedAttribute:
			fact.NestingMode, children = "single", nested.Attributes
		case actionschema.ListNestedAttribute:
			fact.NestingMode, children = "list", nested.NestedObject.Attributes
		case actionschema.SetNestedAttribute:
			fact.NestingMode, children = "set", nested.NestedObject.Attributes
		case actionschema.MapNestedAttribute:
			fact.NestingMode, children = "map", nested.NestedObject.Attributes
		}

		if fact.NestingMode != "" {
			out[path] = fact
			for k, v := range snapshotActionAttributes(ctx, t, children, path+".") {
				out[k] = v
			}
			continue
		}

		encoded, err := json.Marshal(a.GetType().TerraformType(ctx))
		if err != nil {
			t.Fatalf("attribute %q: marshal terraform type: %v", path, err)
		}
		fact.Type = string(encoded)
		out[path] = fact
	}
	return out
}

// snapshotActionBlocks is the block half.
func snapshotActionBlocks(
	ctx context.Context, t *testing.T, blocks map[string]actionschema.Block, prefix string,
) map[string]schemaSnapshotFact {
	t.Helper()
	out := map[string]schemaSnapshotFact{}
	for name, block := range blocks {
		path := prefix + name

		var nestingMode string
		var attributes map[string]actionschema.Attribute
		var nested map[string]actionschema.Block
		switch shaped := block.(type) {
		case actionschema.ListNestedBlock:
			nestingMode = "list"
			attributes, nested = shaped.NestedObject.Attributes, shaped.NestedObject.Blocks
		case actionschema.SetNestedBlock:
			nestingMode = "set"
			attributes, nested = shaped.NestedObject.Attributes, shaped.NestedObject.Blocks
		case actionschema.SingleNestedBlock:
			nestingMode = "single"
			attributes, nested = shaped.Attributes, shaped.Blocks
		default:
			t.Fatalf("block %q has unhandled type %T", path, block)
		}

		out[path] = commonBlockFact(ctx, nestingMode, block)
		for k, v := range snapshotActionAttributes(ctx, t, attributes, path+".") {
			out[k] = v
		}
		for k, v := range snapshotActionBlocks(ctx, t, nested, path+".") {
			out[k] = v
		}
	}
	return out
}

// snapshotListAttributes mirrors snapshotResourceAttributes over
// list/schema, which has no Set-nested variant of either an attribute or a
// block.
func snapshotListAttributes(
	ctx context.Context, t *testing.T, attrs map[string]listschema.Attribute, prefix string,
) map[string]schemaSnapshotFact {
	t.Helper()
	out := map[string]schemaSnapshotFact{}
	for name, a := range attrs {
		path := prefix + name
		fact := commonAttributeFact(ctx, a)

		var children map[string]listschema.Attribute
		switch nested := a.(type) {
		case listschema.SingleNestedAttribute:
			fact.NestingMode, children = "single", nested.Attributes
		case listschema.ListNestedAttribute:
			fact.NestingMode, children = "list", nested.NestedObject.Attributes
		case listschema.MapNestedAttribute:
			fact.NestingMode, children = "map", nested.NestedObject.Attributes
		}

		if fact.NestingMode != "" {
			out[path] = fact
			for k, v := range snapshotListAttributes(ctx, t, children, path+".") {
				out[k] = v
			}
			continue
		}

		encoded, err := json.Marshal(a.GetType().TerraformType(ctx))
		if err != nil {
			t.Fatalf("attribute %q: marshal terraform type: %v", path, err)
		}
		fact.Type = string(encoded)
		out[path] = fact
	}
	return out
}

// snapshotListBlocks is the block half; an unrecognised block fails by name.
func snapshotListBlocks(
	ctx context.Context, t *testing.T, blocks map[string]listschema.Block, prefix string,
) map[string]schemaSnapshotFact {
	t.Helper()
	out := map[string]schemaSnapshotFact{}
	for name, block := range blocks {
		path := prefix + name

		var nestingMode string
		var attributes map[string]listschema.Attribute
		switch shaped := block.(type) {
		case listschema.ListNestedBlock:
			nestingMode = "list"
			attributes = shaped.NestedObject.Attributes
		case listschema.SingleNestedBlock:
			nestingMode = "single"
			attributes = shaped.Attributes
		default:
			t.Fatalf("block %q has unhandled type %T", path, block)
		}

		out[path] = commonBlockFact(ctx, nestingMode, block)
		for k, v := range snapshotListAttributes(ctx, t, attributes, path+".") {
			out[k] = v
		}
	}
	return out
}

// diffSchemaSnapshots reports one line per field that moved between two
// snapshots, naming the surface kind, surface, path and field.
func diffSchemaSnapshots(want, got providerSchemaSnapshot) []string {
	wantFlat := flattenSchemaSnapshot(want)
	gotFlat := flattenSchemaSnapshot(got)

	keys := map[string]bool{}
	for k := range wantFlat {
		keys[k] = true
	}
	for k := range gotFlat {
		keys[k] = true
	}
	sorted := make([]string, 0, len(keys))
	for k := range keys {
		sorted = append(sorted, k)
	}
	sort.Strings(sorted)

	var diffs []string
	for _, k := range sorted {
		w, wok := wantFlat[k]
		g, gok := gotFlat[k]
		switch {
		case wok && !gok:
			diffs = append(diffs, fmt.Sprintf("%s: removed (was %q)", k, w))
		case !wok && gok:
			diffs = append(diffs, fmt.Sprintf("%s: added (now %q)", k, g))
		case w != g:
			diffs = append(diffs, fmt.Sprintf("%s: %q -> %q", k, w, g))
		}
	}
	return diffs
}

// flattenSchemaSnapshot turns the whole snapshot into one map of
// "<kind>:<surface>.<path>.<field>" to its scalar value, so two snapshots can
// be diffed field by field rather than compared as nested structures.
func flattenSchemaSnapshot(snap providerSchemaSnapshot) map[string]string {
	out := map[string]string{}

	add := func(kind string, surfaces map[string]schemaSnapshotSurface) {
		for name, surface := range surfaces {
			base := kind + ":" + name
			out[base+".version"] = fmt.Sprint(surface.Version)
			out[base+".description"] = surface.Description
			out[base+".description_kind"] = surface.DescriptionKind
			out[base+".deprecation_message"] = surface.Deprecation

			for path, fact := range surface.Attributes {
				p := base + "." + path
				out[p+".kind"] = fact.Kind
				out[p+".type"] = fact.Type
				out[p+".nesting_mode"] = fact.NestingMode
				out[p+".required"] = fmt.Sprint(fact.Required)
				out[p+".optional"] = fmt.Sprint(fact.Optional)
				out[p+".computed"] = fmt.Sprint(fact.Computed)
				out[p+".sensitive"] = fmt.Sprint(fact.Sensitive)
				out[p+".write_only"] = fmt.Sprint(fact.WriteOnly)
				out[p+".description"] = fact.Description
				out[p+".description_kind"] = fact.DescriptionKind
				out[p+".deprecation_message"] = fact.Deprecation
				out[p+".default"] = fact.Default
				out[p+".plan_modifiers"] = strings.Join(fact.PlanModifiers, "; ")
				out[p+".validators"] = strings.Join(fact.Validators, "; ")
				out[p+".custom_type"] = fact.CustomType
			}
		}
	}

	add("resource", snap.Resources)
	add("data_source", snap.DataSources)
	add("list_resource", snap.ListResources)
	add("action", snap.Actions)

	return out
}
