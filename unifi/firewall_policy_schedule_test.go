package unifi

import (
	"context"
	"fmt"
	"reflect"
	"testing"

	fwresource "github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
)

// TestAccFirewallPolicyScheduleIsManageable is the other half of
// TestAnUnrelatedApplyResetsAFirewallPolicySchedule's story: that one asks
// whether an unrelated apply destroys a schedule the practitioner set in the
// UI; this asks whether they can set one through Terraform at all, which
// until the attribute existed they could not.
//
// Step 1 declares a business-hours schedule. Step 2 changes ONLY the
// description, with the schedule block still declared, and the schedule must
// survive -- the same unrelated-apply shape, now with the practitioner as the
// owner of the value rather than the controller.
func TestAccFirewallPolicyScheduleIsManageable(t *testing.T) {
	check := func(wantMode, wantStart, wantEnd string) resource.TestCheckFunc {
		return func(*terraform.State) error {
			client, site := probeClient(t)
			policies, err := client.ListFirewallPolicy(context.Background(), site)
			if err != nil {
				return err
			}
			for i := range policies {
				if policies[i].Name == "tfacc-sched-managed" {
					s := policies[i].Schedule
					if s == nil {
						return fmt.Errorf("the controller holds no schedule at all")
					}
					if s.Mode != wantMode || s.TimeRangeStart != wantStart || s.TimeRangeEnd != wantEnd {
						return fmt.Errorf(
							"controller holds mode=%q %q-%q, want mode=%q %q-%q",
							s.Mode, s.TimeRangeStart, s.TimeRangeEnd, wantMode, wantStart, wantEnd)
					}
					t.Logf("controller holds mode=%q %s-%s", s.Mode, s.TimeRangeStart, s.TimeRangeEnd)
					return nil
				}
			}
			return fmt.Errorf("the policy the provider created is not on the controller")
		}
	}

	config := func(description string) string {
		return fmt.Sprintf(`
resource "unifi_network" "sched" {
  name   = "tfacc-sched-net"
  subnet = "10.182.0.1/24"
  vlan   = 182
}

resource "unifi_firewall_zone" "sched" {
  name        = "tfacc-sched-zone"
  network_ids = [unifi_network.sched.id]
}

resource "unifi_firewall_policy" "sched" {
  name        = "tfacc-sched-managed"
  action      = "ALLOW"
  protocol    = "all"
  description = %q

  schedule = {
    mode             = "EVERY_DAY"
    time_range_start = "09:00"
    time_range_end   = "17:00"
  }

  source = {
    zone_id         = unifi_firewall_zone.sched.id
    matching_target = "ANY"
  }

  destination = {
    zone_id         = unifi_firewall_zone.sched.id
    matching_target = "ANY"
  }
}
`, description)
	}

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{Config: config("before"), Check: check("EVERY_DAY", "09:00", "17:00")},
			{Config: config("after"), Check: check("EVERY_DAY", "09:00", "17:00")},
		},
	})
}

// The tests below are adopted from upstream's firewall_policy_schedule_test.go,
// adapted to kit reality. Upstream added the schedule attribute
// Computed-only: importable and preserved across an unrelated update, but
// never something a practitioner declares. This kit's descriptor goes
// further -- every schedule member is Optional+Computed, so a schedule can
// be written through Terraform too, a strict superset of upstream's
// anti-reset guarantee (see firewallPolicyCarrySchedule's doc comment).
// Upstream also serves repeat_on_days as a Set, this kit as a List, but
// TestCheckTypeSetElemAttr doesn't care which, so no case was dropped.
//
// TestFirewallPolicySchemaExposesComputedSchedule (upstream's name) is
// renamed and inverted below: the two schemas disagree on purpose.

// TestFirewallPolicySchemaExposesConfigurableSchedule is upstream's schema
// shape test with its expectation flipped: this provider makes schedule
// Optional+Computed on every member (upstream asserts computed-only) so the
// attribute is settable. A regression to computed-only would make the
// attribute unwritable again.
func TestFirewallPolicySchemaExposesConfigurableSchedule(t *testing.T) {
	var response fwresource.SchemaResponse
	NewFirewallPolicyResource().Schema(
		context.Background(), fwresource.SchemaRequest{}, &response,
	)
	if response.Diagnostics.HasError() {
		t.Fatalf("schema diagnostics: %v", response.Diagnostics)
	}

	attribute, ok := response.Schema.Attributes["schedule"]
	if !ok {
		t.Fatal("firewall policy schema has no schedule attribute")
	}
	sched, ok := attribute.(schema.SingleNestedAttribute)
	if !ok {
		t.Fatalf("schedule schema type = %T, want schema.SingleNestedAttribute", attribute)
	}
	if !sched.Optional || !sched.Computed {
		t.Fatalf(
			"schedule Optional=%v Computed=%v, want settable (Optional and Computed)",
			sched.Optional,
			sched.Computed,
		)
	}
	for name, member := range sched.Attributes {
		switch field := member.(type) {
		case schema.StringAttribute:
			if !field.Optional || !field.Computed {
				t.Errorf("%s is not settable (Optional+Computed)", name)
			}
		case schema.BoolAttribute:
			if !field.Optional || !field.Computed {
				t.Errorf("%s is not settable (Optional+Computed)", name)
			}
		case schema.ListAttribute:
			// repeat_on_days: this kit serves it as a List where upstream
			// serves a Set. Both round-trip the same []string the controller
			// returns, and this kit's List still needs to be settable.
			if !field.Optional || !field.Computed {
				t.Errorf("%s is not settable (Optional+Computed)", name)
			}
		default:
			t.Errorf("unexpected schedule attribute type for %s: %T", name, member)
		}
	}
}

// TestFirewallPolicyScheduleRoundTripsControllerValue is upstream's test,
// unchanged apart from going through the kit's shim adapters
// (firewallPolicyToModel / modelToFirewallPolicy) instead of the hand-written
// mapper functions of the same name.
func TestFirewallPolicyScheduleRoundTripsControllerValue(t *testing.T) {
	allDay := false
	want := &ui.FirewallPolicySchedule{
		Date:           "2026-07-10",
		DateStart:      "2026-07-01",
		DateEnd:        "2026-07-31",
		Mode:           "EVERY_WEEK",
		RepeatOnDays:   []string{"mon", "wed", "fri"},
		TimeAllDay:     &allDay,
		TimeRangeStart: "09:00",
		TimeRangeEnd:   "17:30",
	}
	assertFirewallPolicyScheduleRoundTrip(t, want)
}

// TestFirewallPolicySchedulePreservesLegacyAlwaysMetadata is upstream's test,
// unchanged apart from the same adapter substitution.
func TestFirewallPolicySchedulePreservesLegacyAlwaysMetadata(t *testing.T) {
	allDay := false
	want := &ui.FirewallPolicySchedule{
		DateStart:      "2025-06-20",
		DateEnd:        "2025-06-27",
		Mode:           "ALWAYS",
		RepeatOnDays:   []string{},
		TimeAllDay:     &allDay,
		TimeRangeStart: "09:00",
		TimeRangeEnd:   "12:00",
	}
	assertFirewallPolicyScheduleRoundTrip(t, want)
}

// TestFirewallPolicyOmittedScheduleFallsBackToAlways is upstream's test,
// unchanged apart from the same adapter substitution: an omitted schedule
// still falls back to ALWAYS, which is consistent with -- not redundant
// with -- the two round-trip tests above asserting a provided schedule is
// preserved.
func TestFirewallPolicyOmittedScheduleFallsBackToAlways(t *testing.T) {
	policy := testScheduledFirewallPolicy(nil)
	var model firewallPolicyModel
	if diags := firewallPolicyToModel(context.Background(), policy, &model); diags.HasError() {
		t.Fatalf("API to resource model conversion failed: %v", diags)
	}
	roundTripped, diags := modelToFirewallPolicy(context.Background(), model)
	if diags.HasError() {
		t.Fatalf("resource model to API conversion failed: %v", diags)
	}
	if roundTripped.Schedule == nil || roundTripped.Schedule.Mode != "ALWAYS" {
		t.Fatalf("omitted schedule = %#v, want ALWAYS fallback", roundTripped.Schedule)
	}
}

func assertFirewallPolicyScheduleRoundTrip(t *testing.T, want *ui.FirewallPolicySchedule) {
	t.Helper()
	var model firewallPolicyModel
	if diags := firewallPolicyToModel(
		context.Background(), testScheduledFirewallPolicy(want), &model,
	); diags.HasError() {
		t.Fatalf("API to resource model conversion failed: %v", diags)
	}
	roundTripped, diags := modelToFirewallPolicy(context.Background(), model)
	if diags.HasError() {
		t.Fatalf("resource model to API conversion failed: %v", diags)
	}
	if !reflect.DeepEqual(roundTripped.Schedule, want) {
		t.Fatalf(
			"schedule changed during round-trip:\n got: %#v\nwant: %#v",
			roundTripped.Schedule,
			want,
		)
	}
}

func testScheduledFirewallPolicy(schedule *ui.FirewallPolicySchedule) *ui.FirewallPolicy {
	return &ui.FirewallPolicy{
		Name:     "scheduled policy",
		Action:   "BLOCK",
		Enabled:  true,
		Protocol: "all",
		Version:  "BOTH",
		Schedule: schedule,
		Source: &ui.FirewallPolicySource{
			ZoneID:           "zone-internal",
			MatchingTarget:   "ANY",
			PortMatchingType: "ANY",
		},
		Destination: &ui.FirewallPolicyDestination{
			ZoneID:           "zone-external",
			MatchingTarget:   "ANY",
			PortMatchingType: "ANY",
		},
	}
}
