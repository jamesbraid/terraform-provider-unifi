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
// Optional+Computed attribute that the controller derives from another
// attribute, unless the named sibling is changing in the same plan.
//
// Sibling is the attribute name at the root of the resource. A sibling that
// does not exist is reported as an error rather than treated as unchanged.
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
	// Mirrors UseStateForUnknown's guards: nothing to carry on create, config
	// wins when set, and an already-known plan isn't ours to touch.
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

// siblingIsChanging reports whether the named sibling differs between state
// and plan; an unknown planned sibling counts as changing.
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

// diagText renders diagnostics into a single string for an error message.
func diagText(diags diag.Diagnostics) string {
	parts := make([]string, 0, len(diags.Errors()))
	for _, d := range diags.Errors() {
		parts = append(parts, d.Summary()+": "+d.Detail())
	}
	return strings.Join(parts, "; ")
}
