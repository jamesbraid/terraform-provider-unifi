package unifi

import (
	"context"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// Synthetic discriminator schema shared by all tests in this file:
//
//	{mode: string (discriminator), a: string, b: string}
//
// with ownership = {"x": {"a"}, "y": {"b"}}. "mode" itself is always allowed.
func discriminatorTestAttrTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"mode": types.StringType,
		"a":    types.StringType,
		"b":    types.StringType,
	}
}

func discriminatorTestOwnership() map[string][]string {
	return map[string][]string{
		"x": {"a"},
		"y": {"b"},
	}
}

// discriminatorTestObject builds a types.Object for the synthetic schema.
// Pass nil for a string pointer to get a null value. Tests that need an
// unknown value build the types.Object directly (no shorthand for that).
func discriminatorTestObject(t *testing.T, mode, a, b *string) types.Object {
	t.Helper()
	attrTypes := discriminatorTestAttrTypes()
	values := map[string]attr.Value{
		"mode": discriminatorTestStringOrNull(mode),
		"a":    discriminatorTestStringOrNull(a),
		"b":    discriminatorTestStringOrNull(b),
	}
	obj, diags := types.ObjectValue(attrTypes, values)
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics building fixture object: %v", diags)
	}
	return obj
}

func discriminatorTestStringOrNull(v *string) types.String {
	if v == nil {
		return types.StringNull()
	}
	return types.StringValue(*v)
}

func strp(s string) *string { return &s }

func TestDiscriminatorValidator_OwnedChildSetOtherNull_NoError(t *testing.T) {
	// 1. mode="x", a set, b null -> OK (no error).
	ctx := context.Background()
	v := requireChildrenFor("mode", discriminatorTestOwnership())

	obj := discriminatorTestObject(t, strp("x"), strp("configured-a"), nil)
	req := validator.ObjectRequest{
		Path:        path.Root("nested"),
		ConfigValue: obj,
	}
	resp := &validator.ObjectResponse{}

	v.ValidateObject(ctx, req, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("expected no error, got: %v", resp.Diagnostics)
	}
}

func TestDiscriminatorValidator_InactiveChildConfigured_Errors(t *testing.T) {
	// 2. mode="x", b set (b not owned by x) -> error naming b. (non-vacuous:
	// also assert the SAME config with b null does NOT error, to prove this
	// isn't erroring unconditionally.)
	ctx := context.Background()
	v := requireChildrenFor("mode", discriminatorTestOwnership())

	obj := discriminatorTestObject(t, strp("x"), nil, strp("stale-b"))
	req := validator.ObjectRequest{
		Path:        path.Root("nested"),
		ConfigValue: obj,
	}
	resp := &validator.ObjectResponse{}

	v.ValidateObject(ctx, req, resp)

	if !resp.Diagnostics.HasError() {
		t.Fatalf("expected an error naming %q, got none", "b")
	}
	found := false
	for _, d := range resp.Diagnostics {
		if strings.Contains(d.Detail(), `"b"`) || strings.Contains(d.Summary(), `"b"`) {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected diagnostic to name child %q, got: %v", "b", resp.Diagnostics)
	}

	// Control case: same mode, b null -> no error. Proves the validator is
	// reacting to b's configured-ness, not always erroring for mode=x.
	controlObj := discriminatorTestObject(t, strp("x"), nil, nil)
	controlReq := validator.ObjectRequest{
		Path:        path.Root("nested"),
		ConfigValue: controlObj,
	}
	controlResp := &validator.ObjectResponse{}
	v.ValidateObject(ctx, controlReq, controlResp)
	if controlResp.Diagnostics.HasError() {
		t.Fatalf("control case: expected no error when b is null, got: %v", controlResp.Diagnostics)
	}
}

func TestDiscriminatorValidator_OtherInactiveChildConfigured_Errors(t *testing.T) {
	// 3. mode="y", a set -> error naming a.
	ctx := context.Background()
	v := requireChildrenFor("mode", discriminatorTestOwnership())

	obj := discriminatorTestObject(t, strp("y"), strp("stale-a"), nil)
	req := validator.ObjectRequest{
		Path:        path.Root("nested"),
		ConfigValue: obj,
	}
	resp := &validator.ObjectResponse{}

	v.ValidateObject(ctx, req, resp)

	if !resp.Diagnostics.HasError() {
		t.Fatalf("expected an error naming %q, got none", "a")
	}
	found := false
	for _, d := range resp.Diagnostics {
		if strings.Contains(d.Detail(), `"a"`) || strings.Contains(d.Summary(), `"a"`) {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected diagnostic to name child %q, got: %v", "a", resp.Diagnostics)
	}
}

func TestDiscriminatorValidator_UnknownDiscriminator_DefersEvenIfBothSet(t *testing.T) {
	// 4. mode unknown -> no error even if a+b both set (rule 3 defer).
	ctx := context.Background()
	v := requireChildrenFor("mode", discriminatorTestOwnership())

	attrTypes := discriminatorTestAttrTypes()
	obj, diags := types.ObjectValue(attrTypes, map[string]attr.Value{
		"mode": types.StringUnknown(),
		"a":    types.StringValue("a-set"),
		"b":    types.StringValue("b-set"),
	})
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics building fixture: %v", diags)
	}

	req := validator.ObjectRequest{
		Path:        path.Root("nested"),
		ConfigValue: obj,
	}
	resp := &validator.ObjectResponse{}

	v.ValidateObject(ctx, req, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("expected no error when discriminator is unknown, got: %v", resp.Diagnostics)
	}
}

func TestDiscriminatorValidator_NullOrUnknownConfigObject_NoOp(t *testing.T) {
	// 5. config object null/unknown -> no-op.
	ctx := context.Background()
	v := requireChildrenFor("mode", discriminatorTestOwnership())
	attrTypes := discriminatorTestAttrTypes()

	nullReq := validator.ObjectRequest{
		Path:        path.Root("nested"),
		ConfigValue: types.ObjectNull(attrTypes),
	}
	nullResp := &validator.ObjectResponse{}
	v.ValidateObject(ctx, nullReq, nullResp)
	if nullResp.Diagnostics.HasError() {
		t.Fatalf("expected no-op for null config object, got: %v", nullResp.Diagnostics)
	}

	unknownReq := validator.ObjectRequest{
		Path:        path.Root("nested"),
		ConfigValue: types.ObjectUnknown(attrTypes),
	}
	unknownResp := &validator.ObjectResponse{}
	v.ValidateObject(ctx, unknownReq, unknownResp)
	if unknownResp.Diagnostics.HasError() {
		t.Fatalf("expected no-op for unknown config object, got: %v", unknownResp.Diagnostics)
	}
}

func TestDiscriminatorPlanModifier_StaleChildClearedOnTransition(t *testing.T) {
	// 6. state mode="x" (a set), plan mode="y" (a still set from stale
	// state) -> after PlanModifyObject, plan's a is nulled (inactive under
	// y), b preserved. Non-vacuous: assert plan.a is populated BEFORE the
	// call and null AFTER, and that plan.b is untouched.
	ctx := context.Background()
	pm := clearInactiveChildren("mode", discriminatorTestOwnership())

	stateObj := discriminatorTestObject(t, strp("x"), strp("stale-a-from-state"), nil)
	planObj := discriminatorTestObject(t, strp("y"), strp("stale-a-from-state"), strp("fresh-b"))

	// Sanity: prove the plan object actually has 'a' populated before the
	// plan modifier runs, so the post-call assertion is non-vacuous.
	beforeA, ok := planObj.Attributes()["a"].(types.String)
	if !ok || beforeA.IsNull() || beforeA.ValueString() != "stale-a-from-state" {
		t.Fatalf("test setup broken: expected plan.a populated before PlanModifyObject, got %v", planObj.Attributes()["a"])
	}

	req := planmodifier.ObjectRequest{
		Path:       path.Root("nested"),
		StateValue: stateObj,
		PlanValue:  planObj,
	}
	resp := &planmodifier.ObjectResponse{
		PlanValue: planObj,
	}

	pm.PlanModifyObject(ctx, req, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics)
	}

	afterA, ok := resp.PlanValue.Attributes()["a"].(types.String)
	if !ok || !afterA.IsNull() {
		t.Fatalf("expected plan.a nulled after transition x->y, got %v", resp.PlanValue.Attributes()["a"])
	}

	afterB, ok := resp.PlanValue.Attributes()["b"].(types.String)
	if !ok || afterB.IsNull() || afterB.ValueString() != "fresh-b" {
		t.Fatalf("expected plan.b preserved as %q, got %v", "fresh-b", resp.PlanValue.Attributes()["b"])
	}

	afterMode, ok := resp.PlanValue.Attributes()["mode"].(types.String)
	if !ok || afterMode.ValueString() != "y" {
		t.Fatalf("expected plan.mode preserved as %q, got %v", "y", resp.PlanValue.Attributes()["mode"])
	}
}

func TestDiscriminatorPlanModifier_DiscriminatorUnchanged_NoOp(t *testing.T) {
	// 7. discriminator unchanged (state mode="x", plan mode="x") -> plan
	// unchanged (no-op).
	ctx := context.Background()
	pm := clearInactiveChildren("mode", discriminatorTestOwnership())

	stateObj := discriminatorTestObject(t, strp("x"), strp("a-value"), nil)
	planObj := discriminatorTestObject(t, strp("x"), strp("a-value"), nil)

	req := planmodifier.ObjectRequest{
		Path:       path.Root("nested"),
		StateValue: stateObj,
		PlanValue:  planObj,
	}
	resp := &planmodifier.ObjectResponse{
		PlanValue: planObj,
	}

	pm.PlanModifyObject(ctx, req, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics)
	}
	if !resp.PlanValue.Equal(planObj) {
		t.Fatalf("expected plan value unchanged when discriminator is unchanged, got %v", resp.PlanValue)
	}
}

func TestDiscriminatorPlanModifier_NullOrUnknownStateOrPlan_NoOp(t *testing.T) {
	// 8. state or plan null/unknown -> no-op.
	ctx := context.Background()
	pm := clearInactiveChildren("mode", discriminatorTestOwnership())
	attrTypes := discriminatorTestAttrTypes()

	planObj := discriminatorTestObject(t, strp("y"), strp("a-value"), nil)

	// state null
	req1 := planmodifier.ObjectRequest{
		Path:       path.Root("nested"),
		StateValue: types.ObjectNull(attrTypes),
		PlanValue:  planObj,
	}
	resp1 := &planmodifier.ObjectResponse{PlanValue: planObj}
	pm.PlanModifyObject(ctx, req1, resp1)
	if !resp1.PlanValue.Equal(planObj) {
		t.Fatalf("expected no-op when state is null, got %v", resp1.PlanValue)
	}

	// state unknown
	req2 := planmodifier.ObjectRequest{
		Path:       path.Root("nested"),
		StateValue: types.ObjectUnknown(attrTypes),
		PlanValue:  planObj,
	}
	resp2 := &planmodifier.ObjectResponse{PlanValue: planObj}
	pm.PlanModifyObject(ctx, req2, resp2)
	if !resp2.PlanValue.Equal(planObj) {
		t.Fatalf("expected no-op when state is unknown, got %v", resp2.PlanValue)
	}

	stateObj := discriminatorTestObject(t, strp("x"), strp("a-value"), nil)

	// plan null
	req3 := planmodifier.ObjectRequest{
		Path:       path.Root("nested"),
		StateValue: stateObj,
		PlanValue:  types.ObjectNull(attrTypes),
	}
	resp3 := &planmodifier.ObjectResponse{PlanValue: types.ObjectNull(attrTypes)}
	pm.PlanModifyObject(ctx, req3, resp3)
	if !resp3.PlanValue.IsNull() {
		t.Fatalf("expected no-op (still null) when plan is null, got %v", resp3.PlanValue)
	}

	// plan unknown
	req4 := planmodifier.ObjectRequest{
		Path:       path.Root("nested"),
		StateValue: stateObj,
		PlanValue:  types.ObjectUnknown(attrTypes),
	}
	resp4 := &planmodifier.ObjectResponse{PlanValue: types.ObjectUnknown(attrTypes)}
	pm.PlanModifyObject(ctx, req4, resp4)
	if !resp4.PlanValue.IsUnknown() {
		t.Fatalf("expected no-op (still unknown) when plan is unknown, got %v", resp4.PlanValue)
	}
}
