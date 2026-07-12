package unifi

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// setting_discriminator.go is the C4 mechanism: a reusable object validator
// and plan modifier for discriminated-union nested objects, where the set of
// valid children depends on the value of a "discriminator" child attribute
// (e.g. NAT `type`, firewall `matching_target`, mDNS `mode`).
//
// This file is framework only. It is intentionally NOT wired into any
// settings section or setting_resource.go in this PR — it exists so later
// PRs (mDNS, firewall, NAT, content-filtering) can bind their own
// (discriminator, ownership) config to it.
//
// The C4 contract:
//
//  1. Contradictory config -> plan-time error. Configuring a child that the
//     ACTIVE discriminator value does not own is rejected at plan time
//     (requireChildrenFor / ValidateObject).
//  2. Stale prior-state children -> cleared by a plan modifier when the
//     discriminator value CHANGES, before validation, so a legitimate
//     transition (discriminator A -> B) does not error on leftover A-owned
//     state (clearInactiveChildren / PlanModifyObject).
//  3. Unknown discriminator value -> defer child validation (no error); the
//     value isn't known yet at plan time.

// discriminatorConfig is the shared (discriminator, ownership) pairing used
// by both the validator and the plan modifier. discriminator is the name of
// the child attribute (within the nested object) whose value selects which
// other children are valid. ownership maps each discriminator value to the
// set of child attribute names that value owns; a child not listed for the
// active value is "inactive" for that value. A discriminator value absent
// from ownership is treated as owning no optional children. The
// discriminator attribute itself is always allowed and is never considered
// inactive.
type discriminatorConfig struct {
	discriminator string
	ownership     map[string][]string
}

// ownedSet returns the set of child attribute names owned by the given
// discriminator value, as a lookup set. The discriminator attribute itself
// is always included.
func (c discriminatorConfig) ownedSet(value string) map[string]struct{} {
	owned := make(map[string]struct{})
	owned[c.discriminator] = struct{}{}
	for _, name := range c.ownership[value] {
		owned[name] = struct{}{}
	}
	return owned
}

// requireChildrenFor returns an object validator enforcing C4 rules 1 and 3:
// a child configured that the active discriminator value does not own is a
// plan-time error; an unknown discriminator value defers validation.
func requireChildrenFor(discriminator string, ownership map[string][]string) validator.Object {
	return discriminatorValidator{discriminatorConfig{discriminator: discriminator, ownership: ownership}}
}

// clearInactiveChildren returns an object plan modifier enforcing C4 rule 2:
// when the discriminator value changes between state and plan, it nulls the
// children not owned by the NEW value (before validation runs).
func clearInactiveChildren(discriminator string, ownership map[string][]string) planmodifier.Object {
	return discriminatorPlanModifier{discriminatorConfig{discriminator: discriminator, ownership: ownership}}
}

// --- validator.Object ---

type discriminatorValidator struct {
	discriminatorConfig
}

func (v discriminatorValidator) Description(ctx context.Context) string {
	return fmt.Sprintf(
		"Ensures only children owned by the active %q value are configured.",
		v.discriminator,
	)
}

func (v discriminatorValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v discriminatorValidator) ValidateObject(ctx context.Context, req validator.ObjectRequest, resp *validator.ObjectResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}

	attrs := req.ConfigValue.Attributes()

	discVal, ok := attrs[v.discriminator]
	if !ok {
		return
	}
	discStr, ok := discVal.(types.String)
	if !ok {
		return
	}
	// Rule 3: unknown (or null) discriminator defers child validation.
	if discStr.IsUnknown() || discStr.IsNull() {
		return
	}

	owned := v.ownedSet(discStr.ValueString())

	for name, val := range attrs {
		if name == v.discriminator {
			continue
		}
		if _, isOwned := owned[name]; isOwned {
			continue
		}
		if isConfigured(val) {
			resp.Diagnostics.AddAttributeError(
				req.Path,
				"Invalid Discriminated Configuration",
				fmt.Sprintf(
					"Attribute %q may not be configured when %q is %q.",
					name, v.discriminator, discStr.ValueString(),
				),
			)
		}
	}
}

// isConfigured reports whether an attr.Value is "configured" for validation
// purposes: present, non-null, and non-unknown.
func isConfigured(v attr.Value) bool {
	if v == nil {
		return false
	}
	return !v.IsNull() && !v.IsUnknown()
}

// --- planmodifier.Object ---

type discriminatorPlanModifier struct {
	discriminatorConfig
}

func (m discriminatorPlanModifier) Description(ctx context.Context) string {
	return fmt.Sprintf(
		"Clears children no longer owned by %q when its value changes.",
		m.discriminator,
	)
}

func (m discriminatorPlanModifier) MarkdownDescription(ctx context.Context) string {
	return m.Description(ctx)
}

func (m discriminatorPlanModifier) PlanModifyObject(ctx context.Context, req planmodifier.ObjectRequest, resp *planmodifier.ObjectResponse) {
	if req.StateValue.IsNull() || req.StateValue.IsUnknown() {
		return
	}
	if req.PlanValue.IsNull() || req.PlanValue.IsUnknown() {
		return
	}

	stateAttrs := req.StateValue.Attributes()
	planAttrs := req.PlanValue.Attributes()

	stateDisc, ok := attrAsKnownString(stateAttrs[m.discriminator])
	if !ok {
		return
	}
	planDisc, ok := attrAsKnownString(planAttrs[m.discriminator])
	if !ok {
		return
	}

	if stateDisc == planDisc {
		return
	}

	owned := m.ownedSet(planDisc)
	attrTypes := req.PlanValue.AttributeTypes(ctx)

	rebuilt := make(map[string]attr.Value, len(planAttrs))
	for name, val := range planAttrs {
		if name == m.discriminator {
			rebuilt[name] = val
			continue
		}
		if _, isOwned := owned[name]; isOwned {
			rebuilt[name] = val
			continue
		}
		nullVal, err := nullOfType(ctx, attrTypes[name])
		if err != nil {
			resp.Diagnostics.AddAttributeError(
				req.Path,
				"Discriminator Plan Modification Error",
				fmt.Sprintf("Unable to null inactive attribute %q: %s", name, err),
			)
			return
		}
		rebuilt[name] = nullVal
	}

	newPlan, diags := types.ObjectValue(attrTypes, rebuilt)
	resp.Diagnostics.Append(diags...)
	if diags.HasError() {
		return
	}
	resp.PlanValue = newPlan
}

// attrAsKnownString extracts a known, non-null string value from an
// attr.Value that is expected to be a types.String. Returns ok=false if the
// value is missing, not a string, null, or unknown.
func attrAsKnownString(v attr.Value) (string, bool) {
	if v == nil {
		return "", false
	}
	s, ok := v.(types.String)
	if !ok {
		return "", false
	}
	if s.IsNull() || s.IsUnknown() {
		return "", false
	}
	return s.ValueString(), true
}

// nullOfType returns the null attr.Value for an arbitrary attr.Type, so this
// plan modifier works for any child attribute kind (string, object, list,
// ...), not just the string-typed children in the synthetic test schema.
func nullOfType(ctx context.Context, t attr.Type) (attr.Value, error) {
	return t.ValueFromTerraform(ctx, tftypes.NewValue(t.TerraformType(ctx), nil))
}
