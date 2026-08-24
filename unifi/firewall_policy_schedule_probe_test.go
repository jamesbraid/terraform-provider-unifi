package unifi

import (
	"context"
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
)

// These probes drive a live controller under TF_ACC to answer what it does
// with firewall_policy's schedule; none is a scenario test.
// unifi/firewall_policy_resource_acc_test.go owns the scenario.

// TestDoesTheControllerHoldAFirewallPolicySchedule asks whether the
// controller stores a schedule that isn't ALWAYS. firewallPolicyCarrySchedule
// (unifi/firewall_policy_descriptor.go) falls back to Schedule: {Mode:
// "ALWAYS"} whenever nothing else supplies one, so if the controller does
// hold a different schedule, a write that hits that fallback would still
// reset it to always-on.
// Modes are swept rather than guessed, since a single refused shape can't
// tell "the controller holds no schedules" apart from "this shape was
// rejected".
func TestDoesTheControllerHoldAFirewallPolicySchedule(t *testing.T) {
	client, site := probeClient(t)
	ctx := context.Background()

	zones, err := client.ListFirewallZone(ctx, site)
	if err != nil {
		t.Fatal(err)
	}
	if len(zones) == 0 {
		t.Skip("no firewall zone to hang a policy from")
	}
	zone := zones[0].ID

	candidates := []struct {
		label    string
		schedule *ui.FirewallPolicySchedule
	}{
		{"EVERY_DAY with a time range", &ui.FirewallPolicySchedule{
			Mode: "EVERY_DAY", TimeAllDay: boolPtrForTest(false),
			TimeRangeStart: "09:00", TimeRangeEnd: "17:00",
		}},
		{"EVERY_WEEK on weekdays", &ui.FirewallPolicySchedule{
			Mode: "EVERY_WEEK", TimeAllDay: boolPtrForTest(true),
			RepeatOnDays: []string{"mon", "tue", "wed", "thu", "fri"},
		}},
		{"ONE_TIME_ONLY on a date", &ui.FirewallPolicySchedule{
			Mode: "ONE_TIME_ONLY", TimeAllDay: boolPtrForTest(false),
			Date: "2030-01-01", TimeRangeStart: "09:00", TimeRangeEnd: "17:00",
		}},
	}

	held := 0
	for index, candidate := range candidates {
		policy := &ui.FirewallPolicy{
			Name:             fmt.Sprintf("tfacc-schedule-probe-%d", index),
			Action:           "ALLOW",
			Enabled:          true,
			Protocol:         "all",
			Version:          "IPV4",
			ConnectionStates: []string{},
			Source: &ui.FirewallPolicySource{
				ZoneID: zone, MatchingTarget: "ANY", PortMatchingType: "ANY",
			},
			Destination: &ui.FirewallPolicyDestination{
				ZoneID: zone, MatchingTarget: "ANY", PortMatchingType: "ANY",
			},
			Schedule: candidate.schedule,
		}
		created, err := client.CreateFirewallPolicy(ctx, site, policy)
		if err != nil {
			t.Logf("  REFUSED  %s: %v", candidate.label, err)
			continue
		}
		defer func(id string) {
			if err := client.DeleteFirewallPolicy(ctx, site, id); err != nil {
				t.Logf("cleaning up %s: %v", id, err)
			}
		}(created.ID)

		// Read it back rather than trust the create response. A create echo can
		// repeat the request; a fresh GET is what the controller stored.
		back, err := client.GetFirewallPolicy(ctx, site, created.ID)
		if err != nil {
			t.Fatal(err)
		}
		switch {
		case back.Schedule == nil:
			t.Logf("  DROPPED  %s: the controller returns no schedule at all", candidate.label)
		case back.Schedule.Mode == candidate.schedule.Mode:
			held++
			t.Logf("  HELD     %s: mode=%q all_day=%v range=%q-%q days=%v",
				candidate.label, back.Schedule.Mode, back.Schedule.TimeAllDay,
				back.Schedule.TimeRangeStart, back.Schedule.TimeRangeEnd,
				back.Schedule.RepeatOnDays)
		default:
			t.Logf("  RESET    %s: asked for mode=%q, the controller stored %q",
				candidate.label, candidate.schedule.Mode, back.Schedule.Mode)
		}
	}

	if held == 0 {
		t.Log("LAYER 2 UNESTABLISHED: no non-ALWAYS schedule survived a create, so on this " +
			"controller the hardcoded ALWAYS destroys nothing and the null-schedule " +
			"rejection is a tidiness finding rather than a defect")
		return
	}
	t.Logf("LAYER 2 SATISFIED: the controller holds %d of %d non-ALWAYS schedules, so a "+
		"write that sends ALWAYS unconditionally has something to destroy",
		held, len(candidates))
}

// TestAnUnrelatedApplyResetsAFirewallPolicySchedule closes the null-schedule
// rejection end to end: a schedule is set out of band, then an apply that
// only changes the description must not reset it back to ALWAYS.
func TestAnUnrelatedApplyResetsAFirewallPolicySchedule(t *testing.T) {
	var policyID string
	var wanted *ui.FirewallPolicySchedule

	setScheduleOutOfBand := func() {
		client, site := probeClient(t)
		ctx := context.Background()
		policies, err := client.ListFirewallPolicy(ctx, site)
		if err != nil {
			t.Fatal(err)
		}
		for i := range policies {
			if policies[i].Name == "tfacc-schedule-victim" {
				policyID = policies[i].ID
				break
			}
		}
		if policyID == "" {
			t.Fatal("the policy the provider created is not on the controller")
		}
		target, err := client.GetFirewallPolicy(ctx, site, policyID)
		if err != nil {
			t.Fatal(err)
		}
		wanted = &ui.FirewallPolicySchedule{
			Mode: "EVERY_DAY", TimeAllDay: boolPtrForTest(false),
			TimeRangeStart: "09:00", TimeRangeEnd: "17:00",
		}
		target.Schedule = wanted
		if _, err := client.UpdateFirewallPolicy(ctx, site, target); err != nil {
			t.Skipf("the controller refused a business-hours schedule (%v)", err)
		}
		back, err := client.GetFirewallPolicy(ctx, site, policyID)
		if err != nil {
			t.Fatal(err)
		}
		if back.Schedule == nil || back.Schedule.Mode != "EVERY_DAY" {
			t.Skip("the controller did not store the schedule, so there is nothing " +
				"for the apply to destroy")
		}
		t.Logf("POSITIVE CONTROL: controller holds schedule mode=%q %s-%s",
			back.Schedule.Mode, back.Schedule.TimeRangeStart, back.Schedule.TimeRangeEnd)
	}

	check := func(*terraform.State) error {
		client, site := probeClient(t)
		back, err := client.GetFirewallPolicy(context.Background(), site, policyID)
		if err != nil {
			return err
		}
		if back.Schedule == nil {
			return fmt.Errorf("the policy now carries no schedule at all")
		}
		t.Logf("after the apply: schedule mode=%q range=%q-%q",
			back.Schedule.Mode, back.Schedule.TimeRangeStart, back.Schedule.TimeRangeEnd)
		if back.Schedule.Mode != "EVERY_DAY" {
			return fmt.Errorf(
				"THIS FIREWALL RULE NOW RUNS AROUND THE CLOCK.\n"+
					"    Before the apply it ran %s-%s every day. Its mode is now %q.\n"+
					"    The rule is still present, still enabled and still names the same\n"+
					"    zones, so it reads as correct and enforces outside the window it\n"+
					"    was written for.\n\n"+
					"    The only change in the config was the description. This config never\n"+
					"    declares a schedule block, so schedule (Optional+Computed) is left to\n"+
					"    firewallPolicyCarrySchedule's BeforeSend hook (unifi/firewall_policy_\n"+
					"    descriptor.go), which must re-read the controller's own value on an\n"+
					"    update rather than fall back to Schedule{Mode: \"ALWAYS\"}.\n\n"+
					"    A MASK DOES NOT FIX IT: the controller rejects an absent schedule\n"+
					"    exactly as it rejects a null one, so the key has to be on every\n"+
					"    write and the update has to carry the controller's own value.",
				wanted.TimeRangeStart, wanted.TimeRangeEnd, back.Schedule.Mode)
		}
		return nil
	}

	config := func(description string) string {
		return fmt.Sprintf(`
resource "unifi_firewall_zone" "z" {
	name = "tfacc-schedule-zone"
}

resource "unifi_firewall_policy" "victim" {
	name        = "tfacc-schedule-victim"
	action      = "ALLOW"
	description = %q
	source = {
		zone_id            = unifi_firewall_zone.z.id
		matching_target    = "ANY"
		port_matching_type = "ANY"
	}
	destination = {
		zone_id            = unifi_firewall_zone.z.id
		matching_target    = "ANY"
		port_matching_type = "ANY"
	}
}
`, description)
	}

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{Config: config("before")},
			{PreConfig: setScheduleOutOfBand, Config: config("after"), Check: check},
		},
	})
}

// TestCanAFirewallPolicyBeCreatedWithoutASchedule decides the shape of the
// null-schedule rejection's fix: dropping "schedule" from the mask alone
// isn't enough (TestPlainMasksMatchTheirResource would fail, since the
// literal is still assigned) -- the constant has to leave the shared mapper
// too, which is fine only if the controller accepts a create with no
// schedule at all.
func TestCanAFirewallPolicyBeCreatedWithoutASchedule(t *testing.T) {
	client, site := probeClient(t)
	ctx := context.Background()

	zones, err := client.ListFirewallZone(ctx, site)
	if err != nil {
		t.Fatal(err)
	}
	if len(zones) == 0 {
		t.Skip("no firewall zone to hang a policy from")
	}

	created, err := client.CreateFirewallPolicy(ctx, site, &ui.FirewallPolicy{
		Name:             "tfacc-no-schedule-probe",
		Action:           "ALLOW",
		Enabled:          true,
		Protocol:         "all",
		Version:          "IPV4",
		ConnectionStates: []string{},
		Source: &ui.FirewallPolicySource{
			ZoneID: zones[0].ID, MatchingTarget: "ANY", PortMatchingType: "ANY",
		},
		Destination: &ui.FirewallPolicyDestination{
			ZoneID: zones[0].ID, MatchingTarget: "ANY", PortMatchingType: "ANY",
		},
		// Schedule deliberately nil: omitempty makes the key absent.
	})
	if err != nil {
		t.Logf("THE CONTROLLER REFUSES A POLICY WITH NO SCHEDULE: %v", err)
		t.Log("    so the fix cannot simply delete the literal; the create path has to " +
			"keep supplying ALWAYS while the update path stops sending it")
		return
	}
	defer func() {
		if err := client.DeleteFirewallPolicy(ctx, site, created.ID); err != nil {
			t.Logf("cleaning up: %v", err)
		}
	}()

	back, err := client.GetFirewallPolicy(ctx, site, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if back.Schedule == nil {
		t.Log("the controller accepts a policy with no schedule and stores none")
		return
	}
	t.Logf("the controller accepts a policy with no schedule and defaults it to mode=%q",
		back.Schedule.Mode)
}
