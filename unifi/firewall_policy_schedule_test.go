package unifi

import (
	"context"
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

// TestAccFirewallPolicyScheduleIsManageable is the other half of #200. That one
// asked whether an unrelated apply destroys a schedule the practitioner set in
// the UI; this asks whether they can set one through Terraform at all, which
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
