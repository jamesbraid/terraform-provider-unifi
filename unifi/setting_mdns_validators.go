package unifi

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// mdnsObjectValidator implements C4 rule 1 for the mdns section: reject
// configuring predefined_services or custom_services while mode is known
// and not "custom". There is no existing "forbid field X when sibling Y has
// value Z" helper in this codebase (grepped: only unconditional
// ConflictsWith/AlsoRequires precedent, e.g. bgp_resource.go — neither is
// value-gated), so this is a small, purpose-built validator rather than a
// reuse of one.
//
// It operates on the whole "mdns" object (not per-list attribute paths)
// since mode/predefined_services/custom_services are siblings inside the
// same SingleNestedAttribute and are most simply read together off
// req.ConfigValue.
type mdnsObjectValidator struct{}

func (mdnsObjectValidator) Description(ctx context.Context) string {
	return `predefined_services/custom_services are only valid when mode = "custom"`
}

func (v mdnsObjectValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (mdnsObjectValidator) ValidateObject(ctx context.Context, req validator.ObjectRequest, resp *validator.ObjectResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}

	attrs := req.ConfigValue.Attributes()

	mode, ok := attrs["mode"].(types.String)
	if !ok || mode.IsNull() || mode.IsUnknown() {
		// mode unknown/unset: nothing to gate against yet.
		return
	}
	if mode.ValueString() == "custom" {
		return
	}

	predefined, _ := attrs["predefined_services"].(types.List)
	custom, _ := attrs["custom_services"].(types.List)

	configuredPredefined := !predefined.IsNull() && !predefined.IsUnknown() && len(predefined.Elements()) > 0
	configuredCustom := !custom.IsNull() && !custom.IsUnknown() && len(custom.Elements()) > 0

	if configuredPredefined || configuredCustom {
		resp.Diagnostics.AddAttributeError(
			req.Path,
			"mdns.mode discriminator violation",
			`predefined_services/custom_services are only valid when mode = "custom"`,
		)
	}
}

// mdnsStaleChildrenPlanModifier implements C4 rule 2 for the mdns section:
// when mode is changing away from "custom", set predefined_services/
// custom_services to an explicit empty list in the plan BEFORE
// mdnsObjectValidator runs. Without this, a legitimate custom->auto
// transition where the user only changed mode (leaving the two
// Optional+Computed lists untouched/null in config) would either fail the
// validator on stale state, or silently carry the stale custom-mode lists
// forward via UseStateForUnknown and PUT them back to the controller.
//
// It only rewrites a child list when the plan's proposed value for that
// list is unknown (the framework's "might be recomputed" placeholder for an
// omitted Optional+Computed attribute) — a plan value that is already known
// reflects either an explicit config value (which the validator above will
// reject if non-empty under a non-custom mode) or a prior apply's resolved
// value; this modifier's job is only to neutralize the unknown case so the
// validator sees an explicit empty list instead of state's stale contents
// leaking through UseStateForUnknown.
type mdnsStaleChildrenPlanModifier struct{}

func (mdnsStaleChildrenPlanModifier) Description(ctx context.Context) string {
	return `clears predefined_services/custom_services to an empty list when mode changes away from "custom"`
}

func (m mdnsStaleChildrenPlanModifier) MarkdownDescription(ctx context.Context) string {
	return m.Description(ctx)
}

func (mdnsStaleChildrenPlanModifier) PlanModifyObject(ctx context.Context, req planmodifier.ObjectRequest, resp *planmodifier.ObjectResponse) {
	if resp.PlanValue.IsNull() || resp.PlanValue.IsUnknown() {
		return
	}

	planAttrs := resp.PlanValue.Attributes()

	mode, ok := planAttrs["mode"].(types.String)
	if !ok || mode.IsNull() || mode.IsUnknown() {
		return
	}
	if mode.ValueString() == "custom" {
		return
	}

	predefined, ok := planAttrs["predefined_services"].(types.List)
	if !ok {
		return
	}
	custom, ok := planAttrs["custom_services"].(types.List)
	if !ok {
		return
	}

	changed := false
	newAttrs := make(map[string]attr.Value, len(planAttrs))
	for k, v := range planAttrs {
		newAttrs[k] = v
	}

	if predefined.IsUnknown() {
		newAttrs["predefined_services"] = types.ListValueMust(predefined.ElementType(ctx), []attr.Value{})
		changed = true
	}
	if custom.IsUnknown() {
		newAttrs["custom_services"] = types.ListValueMust(custom.ElementType(ctx), []attr.Value{})
		changed = true
	}

	if !changed {
		return
	}

	obj, diags := types.ObjectValue(resp.PlanValue.AttributeTypes(ctx), newAttrs)
	resp.Diagnostics.Append(diags...)
	if diags.HasError() {
		return
	}
	resp.PlanValue = obj
}
