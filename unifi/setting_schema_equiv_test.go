package unifi

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
)

// setting_schema_equiv_test.go guards Task 24a's riskiest step: rewiring
// Schema() to build its 13 section attributes from the settingSections
// registry (via schemaAttribute()) instead of the old inline blocks. The
// golden at testdata/setting_schema_legacy.json was captured from the
// LEGACY (inline-block) Schema() BEFORE the rewire — see
// captureLegacySchemaGolden below, which is not itself a test (it was run
// once, by hand, pre-rewire, to produce the checked-in golden; it is kept
// here, unused by any Test, as the documented provenance of that file).
// TestSettingSchema_equivalence compares the NEW registry-built schema
// against that frozen golden, so it keeps proving equivalence even after
// Task 24c deletes the legacy Schema code entirely.

// normAttr is a deterministic, JSON-serializable, reflect-free snapshot of a
// schema.Attribute. Validators and plan modifiers are not comparable via
// reflect.DeepEqual (they're often closures or unexported structs), so their
// .Description(ctx) strings stand in as the stable signal that two
// attributes carry equivalent validation/plan-modification behavior.
type normAttr struct {
	Name                string     `json:"name"`
	Type                string     `json:"type"`
	Required            bool       `json:"required"`
	Optional            bool       `json:"optional"`
	Computed            bool       `json:"computed"`
	Sensitive           bool       `json:"sensitive"`
	Default             string     `json:"default,omitempty"`
	MarkdownDescription string     `json:"markdown_description,omitempty"`
	Validators          []string   `json:"validators,omitempty"`
	PlanModifiers       []string   `json:"plan_modifiers,omitempty"`
	Attributes          []normAttr `json:"attributes,omitempty"`    // SingleNestedAttribute children, sorted by Name
	NestedObject        *normAttr  `json:"nested_object,omitempty"` // ListNestedAttribute's per-element object, itself carrying .Attributes
}

// normalizeSchemaAttr captures name, type, Required/Optional/Computed/
// Sensitive, the rendered default's description (if any), the attribute's
// MarkdownDescription (via GetMarkdownDescription(), part of every
// schema.Attribute's common fwschema.Attribute interface — so a user-facing
// doc-text change is caught here too, not just structural/behavioral
// changes), and each validator's/plan-modifier's Description(ctx) for a
// schema.Attribute, recursing into SingleNestedAttribute.Attributes and
// ListNestedAttribute.NestedObject.Attributes (each nested attribute's own
// MarkdownDescription is captured too, since normalizeSchemaAttr recurses
// through normalizeAttributeMap). The result sorts non-deterministic map
// iteration (nested Attributes) by name so two structurally-equal schemas
// normalize identically regardless of Go map iteration order.
func normalizeSchemaAttr(ctx context.Context, name string, a schema.Attribute) normAttr {
	out := normAttr{
		Name:                name,
		Type:                a.GetType().String(),
		Required:            a.IsRequired(),
		Optional:            a.IsOptional(),
		Computed:            a.IsComputed(),
		Sensitive:           a.IsSensitive(),
		MarkdownDescription: a.GetMarkdownDescription(),
	}

	switch v := a.(type) {
	case schema.BoolAttribute:
		if v.Default != nil {
			out.Default = v.Default.Description(ctx)
		}
		out.Validators = describeAll(ctx, len(v.Validators), func(i int) describer { return v.Validators[i] })
		out.PlanModifiers = describeAll(ctx, len(v.PlanModifiers), func(i int) describer { return v.PlanModifiers[i] })
	case schema.StringAttribute:
		if v.Default != nil {
			out.Default = v.Default.Description(ctx)
		}
		out.Validators = describeAll(ctx, len(v.Validators), func(i int) describer { return v.Validators[i] })
		out.PlanModifiers = describeAll(ctx, len(v.PlanModifiers), func(i int) describer { return v.PlanModifiers[i] })
	case schema.Int64Attribute:
		if v.Default != nil {
			out.Default = v.Default.Description(ctx)
		}
		out.Validators = describeAll(ctx, len(v.Validators), func(i int) describer { return v.Validators[i] })
		out.PlanModifiers = describeAll(ctx, len(v.PlanModifiers), func(i int) describer { return v.PlanModifiers[i] })
	case schema.ListAttribute:
		if v.Default != nil {
			out.Default = v.Default.Description(ctx)
		}
		out.Validators = describeAll(ctx, len(v.Validators), func(i int) describer { return v.Validators[i] })
		out.PlanModifiers = describeAll(ctx, len(v.PlanModifiers), func(i int) describer { return v.PlanModifiers[i] })
	case schema.ListNestedAttribute:
		if v.Default != nil {
			out.Default = v.Default.Description(ctx)
		}
		out.Validators = describeAll(ctx, len(v.Validators), func(i int) describer { return v.Validators[i] })
		out.PlanModifiers = describeAll(ctx, len(v.PlanModifiers), func(i int) describer { return v.PlanModifiers[i] })
		nested := normalizeAttributeMap(ctx, v.NestedObject.Attributes)
		out.NestedObject = &normAttr{Attributes: nested}
	case schema.SingleNestedAttribute:
		if v.Default != nil {
			out.Default = v.Default.Description(ctx)
		}
		out.Validators = describeAll(ctx, len(v.Validators), func(i int) describer { return v.Validators[i] })
		out.PlanModifiers = describeAll(ctx, len(v.PlanModifiers), func(i int) describer { return v.PlanModifiers[i] })
		out.Attributes = normalizeAttributeMap(ctx, v.Attributes)
	default:
		panic(fmt.Sprintf("normalizeSchemaAttr: unhandled attribute type %T for %q — add a case", a, name))
	}

	return out
}

// describer is the common shape of validator.* and planmodifier.* types:
// both expose Description(ctx) string as their stable, reflect-free
// identity signal.
type describer interface {
	Description(context.Context) string
}

// describeAll renders n describers (validators or plan modifiers) to their
// Description(ctx) strings via get, for a deterministic, order-preserving
// snapshot.
func describeAll(ctx context.Context, n int, get func(i int) describer) []string {
	if n == 0 {
		return nil
	}
	out := make([]string, n)
	for i := 0; i < n; i++ {
		out[i] = get(i).Description(ctx)
	}
	return out
}

// normalizeAttributeMap normalizes every entry of a nested attribute map and
// sorts the result by name, so map iteration order never affects the
// snapshot.
func normalizeAttributeMap(ctx context.Context, attrs map[string]schema.Attribute) []normAttr {
	if len(attrs) == 0 {
		return nil
	}
	out := make([]normAttr, 0, len(attrs))
	for name, a := range attrs {
		out = append(out, normalizeSchemaAttr(ctx, name, a))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// settingSectionAttrNames is the frozen list of the 13 section attribute
// names on the setting resource, in the order captured for the legacy
// golden. It intentionally does NOT derive from settingSections (the new
// registry) — it is the golden's own manifest so a bug that dropped a
// section from the registry would show up as a missing-key diff rather than
// silently shrinking the comparison.
var settingSectionAttrNames = []string{
	"auto_speedtest", "country", "dpi", "lcm", "network_optimization",
	"ntp", "syslog", "doh", "ips", "mgmt", "radius", "usg", "igmp_snooping",
}

const legacySchemaGoldenPath = "testdata/setting_schema_legacy.json"

// captureLegacySchemaGolden snapshots the 13 section attributes of a built
// schema.Schema to legacySchemaGoldenPath. It is exported as a function
// (not a Test) so it can be invoked deliberately from a throwaway TestMain
// override or a one-off `go run`-style harness. It was run exactly once,
// against the pre-rewire (legacy inline-block) Schema(), to produce the
// checked-in golden; it must NEVER be run again against the post-rewire
// schema, since that would silently launder a real divergence into the
// "truth" file. Retained for provenance/documentation only.
func captureLegacySchemaGolden(t *testing.T, s schema.Schema) {
	t.Helper()
	ctx := context.Background()

	golden := make(map[string]normAttr, len(settingSectionAttrNames))
	for _, name := range settingSectionAttrNames {
		a, ok := s.Attributes[name]
		if !ok {
			t.Fatalf("captureLegacySchemaGolden: schema missing section attribute %q", name)
		}
		golden[name] = normalizeSchemaAttr(ctx, name, a)
	}

	b, err := json.MarshalIndent(golden, "", "  ")
	if err != nil {
		t.Fatalf("captureLegacySchemaGolden: marshal: %v", err)
	}
	b = append(b, '\n')
	if err := os.WriteFile(legacySchemaGoldenPath, b, 0o644); err != nil {
		t.Fatalf("captureLegacySchemaGolden: write %s: %v", legacySchemaGoldenPath, err)
	}
}

// TestSettingSchema_equivalence asserts the CURRENT (registry-built, post
// Task-24a-rewire) schema's 13 section attributes normalize identically to
// the frozen legacy golden. It is the load-bearing regression guard for the
// Schema() rewire: each section's schemaAttribute() was hand-written to be
// byte-identical to the inline block it replaced, and this test is what
// proves that claim mechanically rather than by eyeball. It keeps working
// after Task 24c deletes the legacy Schema code, since it only ever compares
// against the checked-in golden file, never against legacy code.
func TestSettingSchema_equivalence(t *testing.T) {
	ctx := context.Background()

	goldenBytes, err := os.ReadFile(legacySchemaGoldenPath)
	if err != nil {
		t.Fatalf("read golden %s: %v (did Task 24a capture it before rewiring Schema()?)", legacySchemaGoldenPath, err)
	}
	var golden map[string]normAttr
	if err := json.Unmarshal(goldenBytes, &golden); err != nil {
		t.Fatalf("unmarshal golden %s: %v", legacySchemaGoldenPath, err)
	}

	r := &settingResource{}
	var resp resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("Schema() produced diagnostics: %v", resp.Diagnostics)
	}

	if len(golden) != len(settingSectionAttrNames) {
		t.Fatalf("golden has %d sections, want %d (%v)", len(golden), len(settingSectionAttrNames), settingSectionAttrNames)
	}

	for _, name := range settingSectionAttrNames {
		want, ok := golden[name]
		if !ok {
			t.Errorf("golden missing section %q", name)
			continue
		}
		a, ok := resp.Schema.Attributes[name]
		if !ok {
			t.Errorf("built schema missing section %q", name)
			continue
		}
		got := normalizeSchemaAttr(ctx, name, a)

		gotJSON, _ := json.MarshalIndent(got, "", "  ")
		wantJSON, _ := json.MarshalIndent(want, "", "  ")
		if string(gotJSON) != string(wantJSON) {
			t.Errorf("section %q diverges from legacy golden:\n--- got (registry-built) ---\n%s\n--- want (legacy golden) ---\n%s", name, gotJSON, wantJSON)
		}
	}
}
