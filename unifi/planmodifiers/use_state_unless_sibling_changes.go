package planmodifiers

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
)

// UseStateUnlessSiblingChanges keeps the prior state value for an unknown
// Optional+Computed attribute, except when a named sibling attribute is
// changing in the same plan.
//
// It exists for attributes the controller derives from another attribute.
// int64planmodifier.UseStateForUnknown alone is wrong for those: it promises
// Terraform the old value will survive the apply, and when the controller
// recomputes it the apply dies with "provider produced inconsistent result
// after apply". Measured on unifi_wlan, where the only planned change was
// minrate_setting_preference "manual" -> "auto":
//
//	.minimum_data_rate_5g_kbps: was cty.NumberIntVal(0), but now cty.NumberIntVal(6000)
//
// Switching the preference to auto hands the rates to the controller. Leaving
// them unknown for that plan lets it supply them. When the sibling is not
// changing the attribute is stable, and keeping the state value avoids
// showing a practitioner a spurious "(known after apply)" on every plan.
//
// Sibling is the attribute name at the root of the resource. A sibling that
// does not exist is reported as an error rather than silently treated as
// unchanged, because that failure would otherwise look exactly like the bug
// this modifier exists to prevent.
type UseStateUnlessSiblingChanges struct {
	Sibling string
}

func (m UseStateUnlessSiblingChanges) Description(_ context.Context) string {
	return fmt.Sprintf(
		"Once set, this value is kept in the plan unless %q is changing, in which case the controller supplies it.",
		m.Sibling,
	)
}

func (m UseStateUnlessSiblingChanges) MarkdownDescription(ctx context.Context) string {
	return m.Description(ctx)
}

func (m UseStateUnlessSiblingChanges) PlanModifyInt64(
	ctx context.Context,
	req planmodifier.Int64Request,
	resp *planmodifier.Int64Response,
) {
	// Same three guards as UseStateForUnknown: nothing to carry forward on
	// create, the configuration wins when it names a value, and a plan that is
	// already known is not ours to touch.
	if req.State.Raw.IsNull() {
		return
	}
	if req.Plan.Raw.IsNull() {
		return
	}
	if !req.ConfigValue.IsNull() {
		return
	}
	if !resp.PlanValue.IsUnknown() {
		return
	}

	changing, err := m.siblingIsChanging(ctx, req)
	if err != nil {
		resp.Diagnostics.AddAttributeError(
			req.Path,
			"Plan modifier cannot read its sibling attribute",
			fmt.Sprintf(
				"UseStateUnlessSiblingChanges on %s names sibling %q, which could not be read: %v. "+
					"Refusing to guess, because guessing 'unchanged' here reintroduces the "+
					"inconsistent-result failure this modifier prevents.",
				req.Path, m.Sibling, err,
			),
		)
		return
	}

	if changing {
		// Leave the plan unknown so the controller may supply a new value.
		return
	}

	resp.PlanValue = req.StateValue
}

// siblingIsChanging reports whether the named sibling differs between state and
// plan. An unknown planned sibling counts as changing: unknown is precisely the
// case where we cannot promise the derived value survives.
func (m UseStateUnlessSiblingChanges) siblingIsChanging(
	ctx context.Context,
	req planmodifier.Int64Request,
) (bool, error) {
	p := path.Root(m.Sibling)

	var planned, stored attr.Value
	if diags := req.Plan.GetAttribute(ctx, p, &planned); diags.HasError() {
		return false, fmt.Errorf("reading %s from the plan: %s", p, diagText(diags))
	}
	if diags := req.State.GetAttribute(ctx, p, &stored); diags.HasError() {
		return false, fmt.Errorf("reading %s from the state: %s", p, diagText(diags))
	}

	if planned == nil || stored == nil {
		return false, fmt.Errorf("%s resolved to no value in the plan or the state", p)
	}
	if planned.IsUnknown() {
		return true, nil
	}

	return !planned.Equal(stored), nil
}

var _ planmodifier.Int64 = UseStateUnlessSiblingChanges{}

// diagText renders diagnostics compactly, so the reason a sibling could not be
// read reaches the practitioner instead of being flattened to "error".
func diagText(diags diag.Diagnostics) string {
	parts := make([]string, 0, len(diags.Errors()))
	for _, d := range diags.Errors() {
		parts = append(parts, d.Summary()+": "+d.Detail())
	}
	return strings.Join(parts, "; ")
}
