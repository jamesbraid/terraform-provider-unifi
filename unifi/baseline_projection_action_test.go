package unifi

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/action"
	actionschema "github.com/hashicorp/terraform-plugin-framework/action/schema"
)

// TestActionSchemaMatchesReleasedBaseline compares the served action schema
// against the released schema projection.
//
// The estate has one action and, before this, nothing read its schema at all.
// The baseline projection test covers managed resources, a sibling covers data
// sources, another covers list resources, and provider_test only counts how
// many actions are registered. So unifi_port could have changed any attribute's
// type, disposition or description and the whole suite would have stayed green.
//
// This is the conformance half. The regression half is
// Test_schemaBehaviourInventory, which now walks actions too and pins the fact
// this comparison structurally cannot see: device_mac is a
// hwtypes.MACAddressType, the protocol renders it as "string", and the released
// baseline therefore records "string". A generated schema that dropped the
// custom type would satisfy every check here and accept any string as a MAC
// address.
//
// Written before unifi_port is generated, deliberately. An oracle cut after the
// fact describes the output; cut before, it judges it.
func TestActionSchemaMatchesReleasedBaseline(t *testing.T) {
	ctx := context.Background()

	baseline := loadBaselineSchemas(t)
	actionSchemas, ok := baseline["action_schemas"].(map[string]any)
	if !ok {
		t.Fatalf("%s: action_schemas missing or not an object — the projection predates "+
			"actions, and this test would otherwise pass by comparing nothing", baselinePath)
	}

	registered := (&unifiProvider{}).Actions(ctx)
	if len(registered) == 0 {
		t.Fatal("the provider registers no actions, so this test would compare nothing")
	}

	compared := 0
	for _, newAction := range registered {
		act := newAction()

		var meta action.MetadataResponse
		act.Metadata(ctx, action.MetadataRequest{ProviderTypeName: "unifi"}, &meta)

		var got action.SchemaResponse
		act.Schema(ctx, action.SchemaRequest{}, &got)

		entry, found := actionSchemas[meta.TypeName].(map[string]any)
		if !found {
			t.Errorf("%s: registered by the provider, absent from the baseline (%s)",
				meta.TypeName, baselinePath)
			continue
		}
		block, _ := entry["block"].(map[string]any)

		want := baselineAttrFacts(t, meta.TypeName, baselineObject(block["attributes"]), "")
		have := actionAttrFacts(ctx, t, meta.TypeName, got.Schema.Attributes, "")

		compareBaselineFacts(t, meta.TypeName, want, have)
		compared++
	}

	if compared != len(registered) {
		t.Errorf("compared %d of %d registered actions", compared, len(registered))
	}
}

// actionAttrFacts is frameworkAttrFacts for an action schema. Separate for the
// same reason the list one is: actionschema.Attribute and rschema.Attribute are
// distinct types that happen to share their accessors.
//
// Nested attributes are reached by type switch. timeouts arrives as a
// SingleNestedAttribute carrying invoke, and it is provider-owned rather than
// generated -- the baseline records it, so the comparison must reach it.
func actionAttrFacts(
	ctx context.Context, t *testing.T, surface string,
	attrs map[string]actionschema.Attribute, prefix string,
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
			for key, child := range actionAttrFacts(ctx, t, surface, children, path+".") {
				out[key] = child
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
