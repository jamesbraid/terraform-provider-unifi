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

// TestAccNat_basic creates a MASQUERADE rule against a WAN interface, updates
// one attribute, and imports by id. out_interface must be a WAN networkconf
// _id (the controller answers api.err.NatRuleInvalidNetworkConf for anything
// else, and the string "wan" is not one), and the sim's default site ships no
// WAN, so the test creates one with unifi_wan first and references its id --
// unifi_network cannot stand in, its purpose validator rejects "wan".
func TestAccNat_basic(t *testing.T) {
	name := acctest.RandomWithPrefix("tf-acc-nat")
	const resourceName = "unifi_nat.test"

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccNatCheckDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccNatConfig(name, "created by tfacc", false),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet(resourceName, "id"),
					resource.TestCheckResourceAttrPair(
						resourceName, "out_interface", "unifi_wan.test", "id"),
					resource.TestCheckResourceAttr(resourceName, "type", "MASQUERADE"),
					resource.TestCheckResourceAttr(resourceName, "ip_version", "IPV4"),
					resource.TestCheckResourceAttr(resourceName, "protocol", "all"),
					resource.TestCheckResourceAttr(resourceName, "enabled", "true"),
					resource.TestCheckResourceAttr(resourceName, "logging", "false"),
					resource.TestCheckResourceAttr(resourceName, "description", "created by tfacc"),
					resource.TestCheckResourceAttr(
						resourceName, "source_filter.filter_type", "NONE"),
					resource.TestCheckResourceAttr(
						resourceName, "destination_filter.filter_type", "NONE"),
					// The controller adds these on create; the read-back must
					// carry them.
					resource.TestCheckResourceAttr(resourceName, "exclude", "false"),
					resource.TestCheckResourceAttr(resourceName, "is_predefined", "false"),
					resource.TestCheckResourceAttr(
						resourceName, "pppoe_use_base_interface", "false"),
				),
			},
			{
				Config: testAccNatConfig(name, "updated by tfacc", true),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						resourceName, "description", "updated by tfacc"),
					resource.TestCheckResourceAttr(resourceName, "logging", "true"),
				),
			},
			{
				Config:   testAccNatConfig(name, "updated by tfacc", true),
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

func testAccNatCheckDestroy(state *terraform.State) error {
	ctx := context.Background()
	client, err := testAccNatRawClient(ctx)
	if err != nil {
		return nil //nolint:nilerr // The test framework already reports destroy failures.
	}

	for _, rs := range state.RootModule().Resources {
		if rs.Type != "unifi_nat" {
			continue
		}
		site := rs.Primary.Attributes["site"]
		if site == "" {
			site = "default"
		}
		if _, err := client.GetNat(ctx, site, rs.Primary.ID); err == nil {
			return fmt.Errorf("unifi_nat %s still exists", rs.Primary.ID)
		} else if _, ok := err.(*ui.NotFoundError); !ok {
			return err
		}
	}
	return nil
}

func testAccNatRawClient(ctx context.Context) (*ui.ApiClient, error) {
	return ui.New(ctx, &ui.Config{
		BaseURL:       os.Getenv("UNIFI_API"),
		Username:      os.Getenv("UNIFI_USERNAME"),
		Password:      os.Getenv("UNIFI_PASSWORD"),
		AllowInsecure: true,
	})
}

func testAccNatConfig(name, description string, logging bool) string {
	return fmt.Sprintf(`
resource "unifi_wan" "test" {
  name    = %[1]q
  type    = "dhcp"
  enabled = true
}

resource "unifi_nat" "test" {
  description   = %[2]q
  type          = "MASQUERADE"
  out_interface = unifi_wan.test.id
  logging       = %[3]t

  source_filter = {
    filter_type = "NONE"
  }

  destination_filter = {
    filter_type = "NONE"
  }
}
`, name, description, logging)
}
