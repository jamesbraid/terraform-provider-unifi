package unifi

import (
	"context"
	"errors"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
)

// TestTheScheduleHookKeepsWhatTheControllerHolds is the fast-loop guard for
// the controller's null-schedule rejection. The defect is silent by
// construction: schedule had no schema attribute, so terraform showed no
// diff, and an apply that changed only a description turned a rule running
// 09:00-17:00 into one running permanently. The original fix shipped only
// with an acceptance test, which needs a live controller to run at all.
//
// It's guardable here only because of how the migration shaped it:
// firewallPolicyCarrySchedule takes the read as a parameter (the
// hand-written Update did the read inline against a live client, with no
// seam a unit test could reach), so the whole behaviour is exercisable
// with a function literal and no controller.
func TestTheScheduleHookKeepsWhatTheControllerHolds(t *testing.T) {
	businessHours := &ui.FirewallPolicySchedule{
		Mode: "EVERY_DAY", TimeRangeStart: "09:00", TimeRangeEnd: "17:00",
	}

	t.Run("a declared schedule is left alone", func(t *testing.T) {
		// The newest clause, and the one whose loss is worst: ToSDK runs
		// first, so a practitioner's block is already encoded; carrying the
		// controller's over the top would make the attribute unsettable --
		// the previous defect wearing the shape of its own fix.
		declared := &ui.FirewallPolicySchedule{Mode: "EVERY_WEEK"}
		sdk := &ui.FirewallPolicy{Schedule: declared}
		hook := firewallPolicyCarrySchedule(
			func(context.Context, string, string) (*ui.FirewallPolicy, error) {
				t.Error("the controller was read for a policy that declares its own schedule; " +
					"the practitioner's value is about to be overwritten by it")
				return &ui.FirewallPolicy{Schedule: businessHours}, nil
			}, "default")
		hook(context.Background(), nil, &firewallPolicyKitModel{
			ID: types.StringValue("policy-1"), Site: types.StringValue("default"),
		}, sdk, nil)
		if sdk.Schedule != declared {
			t.Errorf("the declared schedule was replaced (%+v); the attribute cannot be set",
				sdk.Schedule)
		}
	})

	t.Run("a create gets the literal", func(t *testing.T) {
		sdk := &ui.FirewallPolicy{}
		hook := firewallPolicyCarrySchedule(
			func(context.Context, string, string) (*ui.FirewallPolicy, error) {
				t.Error("the controller was read on a create; there is no policy to read yet")
				return nil, nil
			}, "default")
		hook(context.Background(), nil, &firewallPolicyKitModel{ID: types.StringNull()}, sdk, nil)
		if sdk.Schedule == nil || sdk.Schedule.Mode != firewallPolicyScheduleAlways {
			t.Fatalf("a create sent %+v; the controller refuses a null schedule outright",
				sdk.Schedule)
		}
	})

	t.Run("an update carries the controller's own schedule", func(t *testing.T) {
		sdk := &ui.FirewallPolicy{}
		hook := firewallPolicyCarrySchedule(
			func(_ context.Context, site, id string) (*ui.FirewallPolicy, error) {
				return &ui.FirewallPolicy{Schedule: businessHours}, nil
			}, "default")
		hook(context.Background(), nil, &firewallPolicyKitModel{
			ID: types.StringValue("policy-1"), Site: types.StringValue("default"),
		}, sdk, nil)
		if sdk.Schedule == nil || sdk.Schedule.Mode != "EVERY_DAY" {
			t.Fatalf("an update sent %+v, not the controller's EVERY_DAY schedule.\n"+
				"    A rule the practitioner scheduled for business hours runs "+
				"permanently after an apply that changed something else, and terraform "+
				"shows no diff for it.", sdk.Schedule)
		}
	})

	t.Run("the site falls back to the provider's", func(t *testing.T) {
		// A read against the wrong site fails into the defect silently: it
		// errors, the literal stands, and the schedule resets with nothing
		// to show for it. So the site the read is called with is asserted,
		// not just the outcome.
		var sawSite string
		sdk := &ui.FirewallPolicy{}
		hook := firewallPolicyCarrySchedule(
			func(_ context.Context, site, _ string) (*ui.FirewallPolicy, error) {
				sawSite = site
				return &ui.FirewallPolicy{Schedule: businessHours}, nil
			}, "the-provider-site")
		hook(context.Background(), nil, &firewallPolicyKitModel{
			ID: types.StringValue("policy-1"), Site: types.StringNull(),
		}, sdk, nil)
		if sawSite != "the-provider-site" {
			t.Errorf("read against site %q; a model with no site must fall back to the "+
				"provider's, and a wrong site resets the schedule with no error to show",
				sawSite)
		}
	})

	t.Run("a failed read leaves the literal rather than failing the apply", func(t *testing.T) {
		sdk := &ui.FirewallPolicy{}
		hook := firewallPolicyCarrySchedule(
			func(context.Context, string, string) (*ui.FirewallPolicy, error) {
				return nil, errors.New("controller unreachable")
			}, "default")
		diags := hook(context.Background(), nil, &firewallPolicyKitModel{
			ID: types.StringValue("policy-1"),
		}, sdk, nil)
		if diags.HasError() {
			t.Errorf("a transient read error failed the apply: %v", diags.Errors())
		}
		if sdk.Schedule == nil || sdk.Schedule.Mode != firewallPolicyScheduleAlways {
			t.Fatalf("a failed read sent %+v; the controller refuses a null schedule",
				sdk.Schedule)
		}
	})
}
