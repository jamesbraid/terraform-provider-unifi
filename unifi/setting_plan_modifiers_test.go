package unifi

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// nonNullResourceState returns a tfsdk.State whose Raw is non-null, i.e. an
// EXISTING resource (update/refresh), so useStateForUnknownObject's create
// guard (req.State.Raw.IsNull()) does not fire. The concrete value is
// irrelevant — the modifier only checks IsNull().
func nonNullResourceState() tfsdk.State {
	return tfsdk.State{Raw: tftypes.NewValue(tftypes.String, "exists")}
}

// planModNestedAttrTypes / planModTestAttrTypes model a section-like object
// with a nested object child, exercising fillUnknownFromState's recursion.
var (
	planModNestedAttrTypes = map[string]attr.Type{
		"x": types.Int64Type,
	}
	planModTestAttrTypes = map[string]attr.Type{
		"a":      types.BoolType,
		"b":      types.StringType,
		"nested": types.ObjectType{AttrTypes: planModNestedAttrTypes},
	}
)

func mustObject(t *testing.T, attrTypes map[string]attr.Type, attrs map[string]attr.Value) types.Object {
	t.Helper()
	obj, diags := types.ObjectValue(attrTypes, attrs)
	if diags.HasError() {
		t.Fatalf("building object: %v", diags)
	}
	return obj
}

// TestFillUnknownFromState_partialObject: an unconfigured Computed child left
// UNKNOWN in the plan is filled from prior state; a changed (known) child is
// kept; a nested object's unknown grandchild is filled recursively.
func TestFillUnknownFromState_partialObject(t *testing.T) {
	ctx := context.Background()

	state := mustObject(t, planModTestAttrTypes, map[string]attr.Value{
		"a":      types.BoolValue(true),
		"b":      types.StringValue("prior"),
		"nested": mustObject(t, planModNestedAttrTypes, map[string]attr.Value{"x": types.Int64Value(5)}),
	})
	// User changed a=false, left b and nested.x unset (null in config, unknown
	// in plan because they are Computed).
	plan := mustObject(t, planModTestAttrTypes, map[string]attr.Value{
		"a":      types.BoolValue(false),
		"b":      types.StringUnknown(),
		"nested": mustObject(t, planModNestedAttrTypes, map[string]attr.Value{"x": types.Int64Unknown()}),
	})
	config := mustObject(t, planModTestAttrTypes, map[string]attr.Value{
		"a":      types.BoolValue(false),
		"b":      types.StringNull(),
		"nested": mustObject(t, planModNestedAttrTypes, map[string]attr.Value{"x": types.Int64Null()}),
	})

	got, diags := fillUnknownFromState(ctx, plan, state, config)
	if diags.HasError() {
		t.Fatalf("unexpected diags: %v", diags)
	}
	obj, ok := got.(types.Object)
	if !ok {
		t.Fatalf("result is %T, want types.Object", got)
	}
	attrs := obj.Attributes()
	if a := attrs["a"].(types.Bool); a.ValueBool() != false {
		t.Errorf("a = %v, want false (changed config value kept, not overwritten by state)", a)
	}
	if b := attrs["b"].(types.String); b.IsUnknown() || b.ValueString() != "prior" {
		t.Errorf("b = %v, want \"prior\" (unknown filled from state)", b)
	}
	nestedObj := attrs["nested"].(types.Object)
	if nestedObj.IsUnknown() {
		t.Fatalf("nested is unknown, want a known object")
	}
	if x := nestedObj.Attributes()["x"].(types.Int64); x.IsUnknown() || x.ValueInt64() != 5 {
		t.Errorf("nested.x = %v, want 5 (unknown grandchild filled recursively from state)", x)
	}
}

// TestFillUnknownFromState_nullStateChildFilledAsNull: a Computed child that is
// UNKNOWN in the plan but NULL in prior state is filled to null (known), not
// left unknown — otherwise it perpetually plans as "known after apply".
func TestFillUnknownFromState_nullStateChildFilledAsNull(t *testing.T) {
	ctx := context.Background()
	state := mustObject(t, planModTestAttrTypes, map[string]attr.Value{
		"a":      types.BoolNull(), // controller never returned it -> null in state
		"b":      types.StringValue("prior"),
		"nested": mustObject(t, planModNestedAttrTypes, map[string]attr.Value{"x": types.Int64Value(5)}),
	})
	plan := mustObject(t, planModTestAttrTypes, map[string]attr.Value{
		"a":      types.BoolUnknown(),
		"b":      types.StringValue("prior"),
		"nested": mustObject(t, planModNestedAttrTypes, map[string]attr.Value{"x": types.Int64Value(5)}),
	})
	config := mustObject(t, planModTestAttrTypes, map[string]attr.Value{
		"a":      types.BoolNull(),
		"b":      types.StringValue("prior"),
		"nested": mustObject(t, planModNestedAttrTypes, map[string]attr.Value{"x": types.Int64Value(5)}),
	})

	got, diags := fillUnknownFromState(ctx, plan, state, config)
	if diags.HasError() {
		t.Fatalf("unexpected diags: %v", diags)
	}
	obj := got.(types.Object)
	a := obj.Attributes()["a"].(types.Bool)
	if a.IsUnknown() {
		t.Errorf("a is unknown, want null (null prior state carried forward so the leaf is known)")
	}
	if !a.IsNull() {
		t.Errorf("a = %v, want null", a)
	}
}

// TestFillUnknownFromState_wholeObjectUnknown: a wholly-unknown object is
// replaced by prior state (classic UseStateForUnknown behavior).
func TestFillUnknownFromState_wholeObjectUnknown(t *testing.T) {
	ctx := context.Background()
	state := mustObject(t, planModTestAttrTypes, map[string]attr.Value{
		"a":      types.BoolValue(true),
		"b":      types.StringValue("prior"),
		"nested": mustObject(t, planModNestedAttrTypes, map[string]attr.Value{"x": types.Int64Value(9)}),
	})
	plan := types.ObjectUnknown(planModTestAttrTypes)
	config := types.ObjectNull(planModTestAttrTypes)

	got, diags := fillUnknownFromState(ctx, plan, state, config)
	if diags.HasError() {
		t.Fatalf("unexpected diags: %v", diags)
	}
	obj := got.(types.Object)
	if obj.IsUnknown() {
		t.Fatalf("result is unknown, want state carried forward")
	}
	if b := obj.Attributes()["b"].(types.String); b.ValueString() != "prior" {
		t.Errorf("b = %v, want prior (whole-object USFU)", b)
	}
}

// TestFillUnknownFromState_configDrivenUnknownKept: a child unknown BECAUSE its
// config value is unknown (e.g. references another resource) must stay unknown,
// not be overwritten with stale state.
func TestFillUnknownFromState_configDrivenUnknownKept(t *testing.T) {
	ctx := context.Background()
	state := mustObject(t, planModTestAttrTypes, map[string]attr.Value{
		"a":      types.BoolValue(true),
		"b":      types.StringValue("prior"),
		"nested": mustObject(t, planModNestedAttrTypes, map[string]attr.Value{"x": types.Int64Value(5)}),
	})
	plan := mustObject(t, planModTestAttrTypes, map[string]attr.Value{
		"a":      types.BoolValue(true),
		"b":      types.StringUnknown(), // unknown...
		"nested": mustObject(t, planModNestedAttrTypes, map[string]attr.Value{"x": types.Int64Value(5)}),
	})
	config := mustObject(t, planModTestAttrTypes, map[string]attr.Value{
		"a":      types.BoolValue(true),
		"b":      types.StringUnknown(), // ...because config is unknown (interpolated)
		"nested": mustObject(t, planModNestedAttrTypes, map[string]attr.Value{"x": types.Int64Value(5)}),
	})

	got, diags := fillUnknownFromState(ctx, plan, state, config)
	if diags.HasError() {
		t.Fatalf("unexpected diags: %v", diags)
	}
	obj := got.(types.Object)
	if b := obj.Attributes()["b"].(types.String); !b.IsUnknown() {
		t.Errorf("b = %v, want it to STAY unknown (config-driven unknown must not be overwritten by state)", b)
	}
}

// TestUseStateForUnknownObject_noOpOnCreate: with a null prior state (create),
// the modifier leaves the plan untouched so the provider's Create can hydrate.
func TestUseStateForUnknownObject_noOpOnCreate(t *testing.T) {
	ctx := context.Background()
	m := useStateForUnknownObject()

	planObj := types.ObjectUnknown(planModTestAttrTypes)
	req := planmodifier.ObjectRequest{
		State:       tfsdk.State{Raw: tftypes.NewValue(tftypes.String, nil)}, // null resource state = create
		PlanValue:   planObj,
		StateValue:  types.ObjectNull(planModTestAttrTypes),
		ConfigValue: types.ObjectNull(planModTestAttrTypes),
	}
	resp := &planmodifier.ObjectResponse{PlanValue: planObj}
	m.PlanModifyObject(ctx, req, resp)
	if !resp.PlanValue.Equal(planObj) {
		t.Errorf("create: plan changed to %v, want unchanged unknown %v", resp.PlanValue, planObj)
	}
}

// TestUseStateForUnknownObject_absentSectionNullOnUpdate: on an EXISTING
// resource, a section that is null in state (absent from this controller) and
// unknown in the plan must be carried forward as NULL — not left unknown, which
// would perpetually plan as "known after apply". This is the regression the
// req.State.Raw create guard (vs req.StateValue) prevents.
func TestUseStateForUnknownObject_absentSectionNullOnUpdate(t *testing.T) {
	ctx := context.Background()
	m := useStateForUnknownObject()

	planObj := types.ObjectUnknown(planModTestAttrTypes)
	req := planmodifier.ObjectRequest{
		State:       nonNullResourceState(), // resource exists (update)
		PlanValue:   planObj,
		StateValue:  types.ObjectNull(planModTestAttrTypes), // section absent -> null in state
		ConfigValue: types.ObjectNull(planModTestAttrTypes),
	}
	resp := &planmodifier.ObjectResponse{PlanValue: planObj}
	m.PlanModifyObject(ctx, req, resp)
	if resp.PlanValue.IsUnknown() {
		t.Errorf("absent section still unknown after plan modify, want null (clean plan)")
	}
	if !resp.PlanValue.IsNull() {
		t.Errorf("absent section = %v, want null carried forward from state", resp.PlanValue)
	}
}

// TestUseStateForUnknownObject_fillsOnUpdate: with a real prior state, the
// modifier fills the plan's unknown children from state (the partial-config
// clean-plan fix).
func TestUseStateForUnknownObject_fillsOnUpdate(t *testing.T) {
	ctx := context.Background()
	m := useStateForUnknownObject()

	state := mustObject(t, planModTestAttrTypes, map[string]attr.Value{
		"a":      types.BoolValue(true),
		"b":      types.StringValue("prior"),
		"nested": mustObject(t, planModNestedAttrTypes, map[string]attr.Value{"x": types.Int64Value(5)}),
	})
	plan := mustObject(t, planModTestAttrTypes, map[string]attr.Value{
		"a":      types.BoolValue(true),
		"b":      types.StringUnknown(),
		"nested": mustObject(t, planModNestedAttrTypes, map[string]attr.Value{"x": types.Int64Value(5)}),
	})
	config := mustObject(t, planModTestAttrTypes, map[string]attr.Value{
		"a":      types.BoolValue(true),
		"b":      types.StringNull(),
		"nested": mustObject(t, planModNestedAttrTypes, map[string]attr.Value{"x": types.Int64Value(5)}),
	})
	req := planmodifier.ObjectRequest{State: nonNullResourceState(), PlanValue: plan, StateValue: state, ConfigValue: config}
	resp := &planmodifier.ObjectResponse{PlanValue: plan}
	m.PlanModifyObject(ctx, req, resp)

	if b := resp.PlanValue.Attributes()["b"].(types.String); b.IsUnknown() || b.ValueString() != "prior" {
		t.Errorf("b = %v, want prior (filled from state on update)", b)
	}
}
