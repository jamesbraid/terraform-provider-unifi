package unifi

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// setting_plan_modifiers.go holds the object plan modifier every
// Optional+Computed settings section uses. The stock
// objectplanmodifier.UseStateForUnknown only carries prior state forward when
// the WHOLE section object is unknown. A settings section is almost always
// only PARTIALLY configured — the user sets a few leaves and leaves the rest
// to the controller — so the object is KNOWN but its unconfigured Computed
// child leaves plan as unknown ("known after apply"), and the plan never
// settles. useStateForUnknownObject fixes that by filling every unknown
// descendant from prior state, recursing into nested known objects.

// useStateForUnknownObjectModifier is a planmodifier.Object that preserves
// prior state for any attribute — top-level or nested — left unknown by the
// plan, so a partially-configured section reaches a clean plan.
type useStateForUnknownObjectModifier struct{}

// useStateForUnknownObject returns the shared recursive UseStateForUnknown
// object plan modifier used by every Optional+Computed settings section.
func useStateForUnknownObject() planmodifier.Object {
	return useStateForUnknownObjectModifier{}
}

// Description reuses the stock UseStateForUnknown wording verbatim: from a
// practitioner's view the behavior is the same (set values persist), and the
// schema-equivalence golden compares plan-modifier descriptions, so matching
// the string keeps sections that already used the stock modifier golden-stable.
func (m useStateForUnknownObjectModifier) Description(ctx context.Context) string {
	return "Once set, the value of this attribute in state will not change."
}

// MarkdownDescription mirrors Description.
func (m useStateForUnknownObjectModifier) MarkdownDescription(ctx context.Context) string {
	return m.Description(ctx)
}

// PlanModifyObject fills the plan's unknown descendants from prior state.
func (m useStateForUnknownObjectModifier) PlanModifyObject(ctx context.Context, req planmodifier.ObjectRequest, resp *planmodifier.ObjectResponse) {
	// Create (the whole RESOURCE has no prior state): leave the plan's unknowns
	// for the provider's Create to resolve (see hydrateAllSections). This gates
	// on the resource state, NOT req.StateValue — a section that is simply
	// absent from this controller is null in an EXISTING resource's state, and
	// its null must still be carried forward (below) so it plans clean rather
	// than "known after apply". Matches the stock modifier's create guard.
	if req.State.Raw.IsNull() {
		return
	}
	// Destroy / explicit null plan: nothing to fill.
	if req.PlanValue.IsNull() {
		return
	}

	merged, diags := fillUnknownFromState(ctx, req.PlanValue, req.StateValue, req.ConfigValue)
	resp.Diagnostics.Append(diags...)
	if diags.HasError() {
		return
	}
	if obj, ok := merged.(types.Object); ok {
		resp.PlanValue = obj
	}
}

// fillUnknownFromState returns plan with every unknown descendant replaced by
// the corresponding prior-state value, recursing into nested known objects.
// plan, state and config must share a type (config may be nil at a nesting
// level the caller cannot supply). The rules:
//
//   - A value whose CONFIG is unknown is left unknown — that unknown comes from
//     configuration (an interpolated/other-resource value), not from a Computed
//     leaf the user omitted, so prior state must not overwrite it.
//   - A wholly-unknown value is replaced by state (classic UseStateForUnknown),
//     provided state carries a usable value.
//   - A known object is recursed into so its own unknown children are filled;
//     lists and scalars are whole-values here (an unknown one is handled by the
//     rule above, a known one is kept as-is — no per-element list descent).
func fillUnknownFromState(ctx context.Context, plan, state, config attr.Value) (attr.Value, diag.Diagnostics) {
	var diags diag.Diagnostics

	// Config-driven unknown: keep the plan's unknown, do not carry state.
	if config != nil && config.IsUnknown() {
		return plan, diags
	}

	if plan.IsUnknown() {
		if state == nil || state.IsUnknown() {
			return plan, diags // no prior value to carry forward
		}
		// Carry state forward even when it is NULL: a Computed leaf the
		// controller did not return is null in state, and planning it as
		// known-null (rather than leaving it unknown) is what makes the plan
		// settle. state is never unknown post-apply.
		return state, diags
	}

	planObj, ok := plan.(types.Object)
	if !ok {
		return plan, diags // known scalar or list — keep as-is
	}
	if planObj.IsNull() {
		return plan, diags
	}
	stateObj, ok := state.(types.Object)
	if !ok || stateObj.IsNull() || stateObj.IsUnknown() {
		return plan, diags // no prior object to fill from
	}

	var configAttrs map[string]attr.Value
	if configObj, ok := config.(types.Object); ok && !configObj.IsNull() && !configObj.IsUnknown() {
		configAttrs = configObj.Attributes()
	}

	planAttrs := planObj.Attributes()
	stateAttrs := stateObj.Attributes()
	out := make(map[string]attr.Value, len(planAttrs))
	for name, pv := range planAttrs {
		sv := stateAttrs[name] // nil if absent
		var cv attr.Value
		if configAttrs != nil {
			cv = configAttrs[name] // nil if absent
		}
		merged, d := fillUnknownFromState(ctx, pv, sv, cv)
		diags.Append(d...)
		out[name] = merged
	}

	obj, d := types.ObjectValue(planObj.AttributeTypes(ctx), out)
	diags.Append(d...)
	if diags.HasError() {
		return plan, diags
	}
	return obj, diags
}

// Compile-time assertion the modifier satisfies the framework interface.
var _ planmodifier.Object = useStateForUnknownObjectModifier{}
