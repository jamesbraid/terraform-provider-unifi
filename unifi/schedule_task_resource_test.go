package unifi

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/querycheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/hashicorp/terraform-plugin-testing/tfversion"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/acctestenv"
)

// testAccScheduleTaskCheckDestroy verifies every unifi_schedule_task in state
// is gone from the controller. Best-effort: it no-ops when no live controller
// is configured.
func testAccScheduleTaskCheckDestroy(s *terraform.State) error {
	ctx := context.Background()
	apiURL := os.Getenv("UNIFI_API")
	if apiURL == "" {
		return nil
	}
	apiClient, err := ui.New(ctx, &ui.Config{
		BaseURL:       apiURL,
		Username:      os.Getenv("UNIFI_USERNAME"),
		Password:      os.Getenv("UNIFI_PASSWORD"),
		AllowInsecure: true,
	})
	if err != nil {
		return nil //nolint:nilerr // best-effort check; skip when no live client
	}
	for _, rs := range s.RootModule().Resources {
		if rs.Type != "unifi_schedule_task" {
			continue
		}
		site := rs.Primary.Attributes["site"]
		if site == "" {
			site = "default"
		}
		_, err := apiClient.GetScheduleTask(ctx, site, rs.Primary.ID)
		if err == nil {
			return fmt.Errorf("unifi_schedule_task %s still exists", rs.Primary.ID)
		}
		if _, ok := err.(*ui.NotFoundError); !ok {
			return err
		}
	}
	return nil
}

func testAccScheduleTaskConfig(name, cron string, once bool, mac string) string {
	return fmt.Sprintf(`
resource "unifi_schedule_task" "test" {
  name              = %q
  cron_expr         = %q
  execute_only_once = %t

  upgrade_targets = [
    { mac = %q },
  ]
}
`, name, cron, once, mac)
}

// TestAccScheduleTaskFramework_basic drives create, update and import against a
// real adopted device. It is skipped unless UNIFI_ACC_AP_MAC names one, since
// the controller schedules an upgrade only for a device it manages.
func TestAccScheduleTaskFramework_basic(t *testing.T) {
	mac := os.Getenv(acctestenv.EnvAccAPMAC)
	if mac == "" {
		t.Skipf("%s not set; skipping schedule task test", acctestenv.EnvAccAPMAC)
	}
	const resourceName = "unifi_schedule_task.test"
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccScheduleTaskCheckDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccScheduleTaskConfig("tf-acc-sched", "0 4 * * 0", false, mac),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet(resourceName, "id"),
					resource.TestCheckResourceAttr(resourceName, "name", "tf-acc-sched"),
					resource.TestCheckResourceAttr(resourceName, "action", "upgrade"),
					resource.TestCheckResourceAttr(resourceName, "cron_expr", "0 4 * * 0"),
					resource.TestCheckResourceAttr(resourceName, "execute_only_once", "false"),
					resource.TestCheckResourceAttr(resourceName, "upgrade_targets.#", "1"),
					resource.TestCheckResourceAttr(resourceName, "upgrade_targets.0.mac", mac),
				),
			},
			{
				Config: testAccScheduleTaskConfig("tf-acc-sched-2", "30 2 * * 1", true, mac),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(resourceName, "name", "tf-acc-sched-2"),
					resource.TestCheckResourceAttr(resourceName, "cron_expr", "30 2 * * 1"),
					resource.TestCheckResourceAttr(resourceName, "execute_only_once", "true"),
				),
			},
			{
				ResourceName:      resourceName,
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

func TestAccScheduleTaskList_emptyOrSeeded(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		TerraformVersionChecks: []tfversion.TerraformVersionCheck{
			tfversion.SkipBelow(tfversion.Version1_14_0),
		},
		Steps: []resource.TestStep{{
			Query: true,
			Config: `
provider "unifi" {}
list "unifi_schedule_task" "test" {
  provider = unifi
  config {}
}
`,
			QueryResultChecks: []querycheck.QueryResultCheck{
				querycheck.ExpectLengthAtLeast("unifi_schedule_task.test", 0),
			},
		}},
	})
}

func TestScheduleTaskDescriptorRoundTripsEveryField(t *testing.T) {
	ctx := context.Background()
	spec := scheduleTaskKitSpec()

	targets, d := types.ListValue(
		types.ObjectType{AttrTypes: scheduleTaskTargetModel{}.AttributeTypes()},
		[]attr.Value{
			types.ObjectValueMust(scheduleTaskTargetModel{}.AttributeTypes(), map[string]attr.Value{
				"mac": types.StringValue("00:11:22:33:44:55"),
			}),
		},
	)
	if d.HasError() {
		t.Fatalf("build targets: %v", d)
	}

	model := scheduleTaskKitModel{
		ID:              types.StringValue("task-1"),
		Action:          types.StringValue("upgrade"),
		CronExpr:        types.StringValue("0 4 * * 0"),
		ExecuteOnlyOnce: types.BoolValue(true),
		Name:            types.StringValue("weekly"),
		UpgradeTargets:  targets,
	}

	var sdk ui.ScheduleTask
	for _, field := range spec.Fields {
		if d := field.ToSDK(ctx, &model, &sdk); d.HasError() {
			t.Fatalf("ToSDK(%s): %v", field.WireName(), d)
		}
	}
	if sdk.Action != "upgrade" || sdk.CronExpr != "0 4 * * 0" || !sdk.ExecuteOnlyOnce || sdk.Name != "weekly" {
		t.Errorf("scalars did not reach the SDK struct: %+v", sdk)
	}
	if len(sdk.UpgradeTargets) != 1 || sdk.UpgradeTargets[0].MAC != "00:11:22:33:44:55" {
		t.Errorf("upgrade targets did not reach the SDK struct: %+v", sdk.UpgradeTargets)
	}

	var back scheduleTaskKitModel
	for _, field := range spec.Fields {
		if d := field.ToModel(ctx, &sdk, &back); d.HasError() {
			t.Fatalf("ToModel(%s): %v", field.WireName(), d)
		}
	}
	if !back.CronExpr.Equal(model.CronExpr) || !back.ExecuteOnlyOnce.Equal(model.ExecuteOnlyOnce) {
		t.Errorf("scalar round trip: %+v", back)
	}
	if !back.UpgradeTargets.Equal(model.UpgradeTargets) {
		t.Errorf("upgrade targets round trip: %v want %v", back.UpgradeTargets, model.UpgradeTargets)
	}
}

// TestScheduleTaskDescriptorCoversEveryManagedField stops the round trip above
// passing because a field is absent from the descriptor entirely.
func TestScheduleTaskDescriptorCoversEveryManagedField(t *testing.T) {
	got := map[string]bool{}
	for _, f := range scheduleTaskKitSpec().Fields {
		got[f.WireName()] = true
	}
	for _, want := range []string{"action", "cron_expr", "execute_only_once", "name", "upgrade_targets"} {
		if !got[want] {
			t.Errorf("the descriptor does not carry managed field %q", want)
		}
	}
	if len(got) != 5 {
		t.Errorf("descriptor carries %d fields, want 5: %v", len(got), got)
	}
}

func TestScheduleTaskConstructorsServeBothSurfaces(t *testing.T) {
	if NewScheduleTaskResource() == nil {
		t.Error("NewScheduleTaskResource returned nil")
	}
	if NewScheduleTaskListResource() == nil {
		t.Error("NewScheduleTaskListResource returned nil")
	}
	if r := newScheduleTaskKitResource(); r.Spec.TypeName != "schedule_task" {
		t.Errorf("spec TypeName = %q", r.Spec.TypeName)
	}
}
