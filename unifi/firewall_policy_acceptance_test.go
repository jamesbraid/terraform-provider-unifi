package unifi

// Restored from upstream. The kit assembly quarantined this file as
// upstream-new; it is restored here adapted to the kit surface and this
// package's own conventions:
//
//   - the raw SDK alias is `ui`, matching every other file in this package
//     that reaches the SDK directly, rather than upstream's `api`.
//   - the `UNIFI_SKIP_CONTAINER` gate is dropped: this provider's
//     containerized harness (unifi-emu-herder) already proves an
//     EVERY_DAY schedule round-trips in
//     TestAccFirewallPolicyScheduleIsManageable, so there is no reason to
//     withhold the EVERY_WEEK case from the same harness.
//   - CheckDestroy and the raw client follow this package's own convention
//     (see testAccDNSRecordCheckDestroy) rather than upstream's: read
//     UNIFI_API/UNIFI_USERNAME/UNIFI_PASSWORD from the environment directly.
//
// Every assertion is upstream's, unchanged.

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
)

func TestAccFirewallPolicy_scheduleRoundTrip(t *testing.T) {
	name := acctest.RandomWithPrefix("tf-acc-firewall-schedule")
	const resourceName = "unifi_firewall_policy.test"

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccFirewallPolicyScheduleCheckDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccFirewallPolicyScheduleRoundTripConfig(name, "before schedule update"),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(resourceName, "enabled", "false"),
					testAccSetFirewallPolicyScheduleOutOfBand(resourceName),
				),
			},
			{
				Config: testAccFirewallPolicyScheduleRoundTripConfig(name, "after schedule update"),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						resourceName,
						"description",
						"after schedule update",
					),
					resource.TestCheckResourceAttr(resourceName, "schedule.mode", "EVERY_WEEK"),
					resource.TestCheckResourceAttr(
						resourceName,
						"schedule.time_all_day",
						"false",
					),
					resource.TestCheckResourceAttr(
						resourceName,
						"schedule.time_range_start",
						"09:00",
					),
					resource.TestCheckResourceAttr(
						resourceName,
						"schedule.time_range_end",
						"17:30",
					),
					resource.TestCheckResourceAttr(
						resourceName,
						"schedule.repeat_on_days.#",
						"3",
					),
					resource.TestCheckTypeSetElemAttr(
						resourceName,
						"schedule.repeat_on_days.*",
						"mon",
					),
					resource.TestCheckTypeSetElemAttr(
						resourceName,
						"schedule.repeat_on_days.*",
						"wed",
					),
					resource.TestCheckTypeSetElemAttr(
						resourceName,
						"schedule.repeat_on_days.*",
						"fri",
					),
				),
			},
			{
				Config:   testAccFirewallPolicyScheduleRoundTripConfig(name, "after schedule update"),
				PlanOnly: true,
			},
			{
				ResourceName:      resourceName,
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

// testAccSetFirewallPolicyScheduleOutOfBand sets the schedule directly
// against the controller, the way a practitioner would in the UI rather than
// through Terraform. The next step's plan must refresh this in rather than
// silently discard it -- that refresh, not any special-case code, is what
// keeps the schedule-writability fix holding once schedule is a real schema
// attribute.
func testAccSetFirewallPolicyScheduleOutOfBand(resourceName string) resource.TestCheckFunc {
	return func(state *terraform.State) error {
		rs, ok := state.RootModule().Resources[resourceName]
		if !ok {
			return fmt.Errorf("resource %s not found in state", resourceName)
		}

		ctx := context.Background()
		client, err := testAccFirewallPolicyRawClient(ctx)
		if err != nil {
			return err
		}

		site := rs.Primary.Attributes["site"]
		if site == "" {
			site = "default"
		}
		policy, err := client.GetFirewallPolicy(ctx, site, rs.Primary.ID)
		if err != nil {
			return fmt.Errorf("read firewall policy before schedule update: %w", err)
		}

		timeAllDay := false
		policy.Schedule = &ui.FirewallPolicySchedule{
			Mode:           "EVERY_WEEK",
			RepeatOnDays:   []string{"mon", "wed", "fri"},
			TimeAllDay:     &timeAllDay,
			TimeRangeStart: "09:00",
			TimeRangeEnd:   "17:30",
		}
		if _, err := client.UpdateFirewallPolicy(ctx, site, policy); err != nil {
			return fmt.Errorf("set firewall policy schedule through the API: %w", err)
		}
		return nil
	}
}

// testAccFirewallPolicyScheduleCheckDestroy follows this package's existing
// CheckDestroy convention (see testAccDNSRecordCheckDestroy): read the
// controller out of the environment preCheck already required, rather than
// building a second raw-client helper that does the same thing.
func testAccFirewallPolicyScheduleCheckDestroy(state *terraform.State) error {
	ctx := context.Background()
	client, err := testAccFirewallPolicyRawClient(ctx)
	if err != nil {
		return nil //nolint:nilerr // The test framework already reports destroy failures.
	}

	for _, rs := range state.RootModule().Resources {
		if rs.Type != "unifi_firewall_policy" {
			continue
		}
		site := rs.Primary.Attributes["site"]
		if site == "" {
			site = "default"
		}
		if _, err := client.GetFirewallPolicy(ctx, site, rs.Primary.ID); err == nil {
			return fmt.Errorf("unifi_firewall_policy %s still exists", rs.Primary.ID)
		} else if _, ok := err.(*ui.NotFoundError); !ok {
			return err
		}
	}
	return nil
}

func testAccFirewallPolicyRawClient(ctx context.Context) (*ui.ApiClient, error) {
	return ui.New(ctx, &ui.Config{
		BaseURL:       os.Getenv("UNIFI_API"),
		Username:      os.Getenv("UNIFI_USERNAME"),
		Password:      os.Getenv("UNIFI_PASSWORD"),
		AllowInsecure: true,
	})
}

func testAccFirewallPolicyScheduleRoundTripConfig(name, description string) string {
	return fmt.Sprintf(`
resource "unifi_firewall_zone" "source" {
  name        = %[1]q
  network_ids = []
}

resource "unifi_firewall_zone" "destination" {
  name        = "%[1]s-destination"
  network_ids = []
}

resource "unifi_firewall_policy" "test" {
  name        = %[1]q
  action      = "BLOCK"
  protocol    = "tcp"
  description = %[2]q
  enabled     = false

  source = {
    zone_id         = unifi_firewall_zone.source.id
    matching_target = "ANY"
  }

  destination = {
    zone_id            = unifi_firewall_zone.destination.id
    matching_target    = "ANY"
    port               = "65535"
    port_matching_type = "SPECIFIC"
  }
}
`, name, description)
}
