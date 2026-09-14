package unifi

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

// TestAccHotspotPackage_basic creates a free-trial hotspot package, updates
// trial_duration_minutes (an unrelated update also has to carry it -- see
// hotspot_package_descriptor.go), confirms a clean re-plan, and imports by
// id. amount is left unset so the package stays in the free-trial branch:
// the controller's sanitizer refuses a package carrying both duration
// fields, and the schema forces trial_duration_minutes to always be one of
// them.
func TestAccHotspotPackage_basic(t *testing.T) {
	name := acctest.RandomWithPrefix("tf-acc-hotspot-package")
	const resourceName = "unifi_hotspot_package.test"

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccHotspotPackageCheckDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccHotspotPackageConfig(name, 60),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet(resourceName, "id"),
					resource.TestCheckResourceAttr(resourceName, "name", name),
					resource.TestCheckResourceAttr(
						resourceName, "trial_duration_minutes", "60"),
					resource.TestCheckResourceAttr(
						resourceName, "custom_payment_fields_enabled", "false"),
					resource.TestCheckResourceAttr(resourceName, "limit_overwrite", "false"),
				),
			},
			{
				Config: testAccHotspotPackageConfig(name, 120),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						resourceName, "trial_duration_minutes", "120"),
				),
			},
			{
				Config:   testAccHotspotPackageConfig(name, 120),
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

func testAccHotspotPackageCheckDestroy(state *terraform.State) error {
	ctx := context.Background()
	client, err := testAccHotspotPackageRawClient(ctx)
	if err != nil {
		return nil //nolint:nilerr // The test framework already reports destroy failures.
	}

	for _, rs := range state.RootModule().Resources {
		if rs.Type != "unifi_hotspot_package" {
			continue
		}
		site := rs.Primary.Attributes["site"]
		if site == "" {
			site = "default"
		}
		if _, err := client.GetHotspotPackage(ctx, site, rs.Primary.ID); err == nil {
			return fmt.Errorf("unifi_hotspot_package %s still exists", rs.Primary.ID)
		} else if _, ok := err.(*ui.NotFoundError); !ok { //nolint:errorlint // the SDK returns this concrete type directly.
			return err
		}
	}
	return nil
}

func testAccHotspotPackageRawClient(ctx context.Context) (*ui.ApiClient, error) {
	return ui.New(ctx, &ui.Config{
		BaseURL:       os.Getenv("UNIFI_API"),
		Username:      os.Getenv("UNIFI_USERNAME"),
		Password:      os.Getenv("UNIFI_PASSWORD"),
		AllowInsecure: true,
	})
}

func testAccHotspotPackageConfig(name string, trialDurationMinutes int) string {
	return fmt.Sprintf(`
resource "unifi_hotspot_package" "test" {
  name                    = %[1]q
  trial_duration_minutes  = %[2]d
}
`, name, trialDurationMinutes)
}
