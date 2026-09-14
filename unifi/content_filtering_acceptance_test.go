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

// TestAccContentFiltering_basic creates a content filtering policy, updates
// it, confirms a clean re-plan, and imports by id. categories, name and
// schedule are required on create -- the behaviour artifact's
// writes.ContentFiltering -- so the config sets all three; the controller
// also refuses a rule that addresses neither a network nor a client (a
// cross-field constraint the schema cannot express), so network_ids is set
// too and a network is created first to supply it. "ADULT" is a category
// the SDK's own live-capture probe (behavior_probe_integration_test.go)
// confirmed the controller accepts, not an invented value.
func TestAccContentFiltering_basic(t *testing.T) {
	name := acctest.RandomWithPrefix("tf-acc-content-filtering")
	const resourceName = "unifi_content_filtering.test"

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccContentFilteringCheckDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccContentFilteringConfig(name, true),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet(resourceName, "id"),
					resource.TestCheckResourceAttr(resourceName, "name", name),
					resource.TestCheckResourceAttr(resourceName, "enabled", "true"),
					resource.TestCheckResourceAttr(resourceName, "categories.#", "1"),
					resource.TestCheckResourceAttr(resourceName, "categories.0", "ADULT"),
					resource.TestCheckResourceAttr(resourceName, "schedule.mode", "ALWAYS"),
					resource.TestCheckResourceAttr(resourceName, "network_ids.#", "1"),
					resource.TestCheckResourceAttrPair(
						resourceName, "network_ids.0", "unifi_network.content_filtering", "id"),
				),
			},
			{
				Config: testAccContentFilteringConfig(name, false),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(resourceName, "enabled", "false"),
					resource.TestCheckResourceAttr(resourceName, "block_list.#", "1"),
					resource.TestCheckResourceAttr(resourceName, "block_list.0", "example.test"),
				),
			},
			{
				Config:   testAccContentFilteringConfig(name, false),
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

func testAccContentFilteringCheckDestroy(state *terraform.State) error {
	ctx := context.Background()
	client, err := testAccContentFilteringRawClient(ctx)
	if err != nil {
		return nil //nolint:nilerr // The test framework already reports destroy failures.
	}

	for _, rs := range state.RootModule().Resources {
		if rs.Type != "unifi_content_filtering" {
			continue
		}
		site := rs.Primary.Attributes["site"]
		if site == "" {
			site = "default"
		}
		if _, err := client.GetContentFiltering(ctx, site, rs.Primary.ID); err == nil {
			return fmt.Errorf("unifi_content_filtering %s still exists", rs.Primary.ID)
		} else if _, ok := err.(*ui.NotFoundError); !ok { //nolint:errorlint // the SDK returns this concrete type directly.
			return err
		}
	}
	return nil
}

func testAccContentFilteringRawClient(ctx context.Context) (*ui.ApiClient, error) {
	return ui.New(ctx, &ui.Config{
		BaseURL:       os.Getenv("UNIFI_API"),
		Username:      os.Getenv("UNIFI_USERNAME"),
		Password:      os.Getenv("UNIFI_PASSWORD"),
		AllowInsecure: true,
	})
}

func testAccContentFilteringConfig(name string, enabled bool) string {
	// block_list is omitted rather than set to [] when there is nothing to
	// block: an Optional, non-Computed list elides an empty API response to
	// null, so a config that instead writes the attribute as a literal []
	// disagrees with that null state on every plan.
	blockList := ""
	if !enabled {
		blockList = `block_list  = ["example.test"]` + "\n"
	}
	return fmt.Sprintf(`
resource "unifi_network" "content_filtering" {
  name   = "tf-acc-content-filtering-net"
  subnet = "192.168.79.1/24"
  vlan   = 79
}

resource "unifi_content_filtering" "test" {
  name        = %[1]q
  enabled     = %[2]t
  categories  = ["ADULT"]
  network_ids = [unifi_network.content_filtering.id]
  %[3]s
  schedule = {
    mode = "ALWAYS"
  }
}
`, name, enabled, blockList)
}
