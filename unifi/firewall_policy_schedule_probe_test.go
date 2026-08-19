package unifi

import (
	"context"
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
)

// Probes rather than scenarios, and named so on purpose.
//
// These drive a live controller under TF_ACC, but none is a TestAcc function
// and this file is not a scenario owner: they exist to answer what the
// controller does with firewall_policy's schedule, not to cover the surface.
// unifi/firewall_policy_resource_acc_test.go is the scenario.

// TestDoesTheControllerHoldAFirewallPolicySchedule is task 200's second layer,
// and the first layer is already settled without measuring a wire.
//
// firewall_policy_resource.go:566 builds every policy with
// Schedule: &FirewallPolicySchedule{Mode: "ALWAYS"} -- a literal, from no model
// at all: zero tfsdk tags, zero schema attributes, and "schedule" IS in the
// landed mask, so it goes on every write. That makes this different from the
// four fields measured before it. Those were force-emitted BY A TAG, so the
// wire probe could still find them dormant; this one is force-WRITTEN, and no
// tag can withhold it.
//
// So the only open question is whether the controller stores a schedule that is
// not ALWAYS. If it does not -- if schedule is inert here the way wlangroup_id
// is -- the constant is untidy and harmless. If it does, an apply that touches
// anything at all resets a policy the practitioner scheduled for business hours
// to always-on, and the rule silently starts enforcing outside its window.
//
// A HINT IS NOT A MEASUREMENT. The controller refuses a policy whose schedule is
// null and names the field, which was hit while building task 198's fixture.
// That is evidence it VALIDATES the shape. Whether it ACTS on the value is a
// different claim, and this is the test that separates them.
//
// THE MODES ARE SWEPT RATHER THAN GUESSED. The SDK comment lists five and says
// nothing about which operands each needs, and a single refused shape would
// otherwise read as "the controller does not hold schedules" when it only means
// the probe wrote a schedule the controller rejects.
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
			"controller the hardcoded ALWAYS destroys nothing and task 200 is a tidiness " +
			"finding rather than a defect")
		return
	}
	t.Logf("LAYER 2 SATISFIED: the controller holds %d of %d non-ALWAYS schedules, so a "+
		"write that sends ALWAYS unconditionally has something to destroy",
		held, len(candidates))
}

// TestAnUnrelatedApplyResetsAFirewallPolicySchedule closes task 200 end to end.
//
// The provider creates the policy, a schedule is set out of band the way a
// practitioner would set one in the UI, and then an apply changes the
// DESCRIPTION and nothing else. If the schedule comes back ALWAYS, a rule
// written to run during business hours is now running permanently, and nothing
// in the plan said so -- schedule is not in the schema, so terraform shows no
// diff for it.
//
// A MASK DOES NOT FIX THIS ONE, which was the first thing tried and the
// controller refuted it. Dropping "schedule" from firewallPolicyManagedWireFields
// makes the key absent from the PUT body, and the controller rejects an absent
// schedule exactly as it rejects an explicit null -- `on field 'schedule':
// rejected value [null] ... must not be null` (400) -- so every update failed,
// including the surface's own acceptance test. The field must be present on
// every write, so the fix reads the policy and sends back the schedule the
// controller already holds.
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
					"    The only change in the config was the description. schedule has no\n"+
					"    tfsdk tag and no schema attribute, so terraform showed no diff for\n"+
					"    it -- the provider writes Schedule{Mode: \"ALWAYS\"} as a literal on\n"+
					"    every request (firewall_policy_resource.go:566) and schedule is in\n"+
					"    the mask.\n\n"+
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

// TestCanAFirewallPolicyBeCreatedWithoutASchedule decides the SHAPE of task
// 200's fix, and the obvious fix does not survive it if the answer is no.
//
// The mask is derived from the fields modelToFirewallPolicy assigns
// (TestPlainMasksMatchTheirResource), so dropping "schedule" from the mask
// alone makes that check fail: the literal is still assigned. The constant has
// to leave the shared mapper too, and then a create carries no schedule at all
// -- which is fine only if the controller accepts one.
//
// It is worth measuring rather than assuming, because a refusal was seen while
// building task 198's fixture. That refusal was for a schedule explicitly set
// to null, which is a different request from one that omits the key: Schedule
// is a pointer with omitempty, so a nil one is absent rather than null.
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
