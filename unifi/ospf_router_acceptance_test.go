package unifi

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
)

// TestAccOspfRouter_basic creates an OSPF router with one backbone area over a
// real network, updates announce_default_route, confirms a clean re-plan, and
// imports by id. router_id, areas and areas[].network_ids are required on
// create -- the behaviour artifact's writes.OSPFRouter -- so the config sets
// all three; a network is created first because network_ids references its id.
func TestAccOspfRouter_basic(t *testing.T) {
	const resourceName = "unifi_ospf_router.test"

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccOspfRouterCheckDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccOspfRouterConfig("10.255.0.1", false),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet(resourceName, "id"),
					resource.TestCheckResourceAttr(resourceName, "router_id", "10.255.0.1"),
					resource.TestCheckResourceAttr(resourceName, "enabled", "true"),
					resource.TestCheckResourceAttr(resourceName, "announce_default_route", "false"),
					resource.TestCheckResourceAttr(resourceName, "areas.#", "1"),
					resource.TestCheckResourceAttr(resourceName, "areas.0.area_id", "0.0.0.0"),
					resource.TestCheckResourceAttr(resourceName, "areas.0.name", "backbone"),
					resource.TestCheckResourceAttr(resourceName, "areas.0.network_ids.#", "1"),
					resource.TestCheckResourceAttrPair(
						resourceName, "areas.0.network_ids.0", "unifi_network.ospf", "id"),
				),
			},
			{
				Config: testAccOspfRouterConfig("10.255.0.1", true),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(resourceName, "announce_default_route", "true"),
					resource.TestCheckResourceAttr(resourceName, "router_id", "10.255.0.1"),
				),
			},
			{
				Config:   testAccOspfRouterConfig("10.255.0.1", true),
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

func testAccOspfRouterCheckDestroy(state *terraform.State) error {
	ctx := context.Background()
	client, err := testAccOspfRouterRawClient(ctx)
	if err != nil {
		return nil //nolint:nilerr // The test framework already reports destroy failures.
	}

	for _, rs := range state.RootModule().Resources {
		if rs.Type != "unifi_ospf_router" {
			continue
		}
		site := rs.Primary.Attributes["site"]
		if site == "" {
			site = "default"
		}
		if _, err := client.GetOSPFRouter(ctx, site, rs.Primary.ID); err == nil {
			return fmt.Errorf("unifi_ospf_router %s still exists", rs.Primary.ID)
		} else if _, ok := err.(*ui.NotFoundError); !ok { //nolint:errorlint // the SDK returns this concrete type directly.
			return err
		}
	}
	return nil
}

func testAccOspfRouterRawClient(ctx context.Context) (*ui.ApiClient, error) {
	return ui.New(ctx, &ui.Config{
		BaseURL:       os.Getenv("UNIFI_API"),
		Username:      os.Getenv("UNIFI_USERNAME"),
		Password:      os.Getenv("UNIFI_PASSWORD"),
		AllowInsecure: true,
	})
}

func testAccOspfRouterConfig(routerID string, announceDefaultRoute bool) string {
	return fmt.Sprintf(`
resource "unifi_network" "ospf" {
  name   = "tf-acc-ospf-net"
  subnet = "192.168.77.1/24"
  vlan   = 77
}

resource "unifi_ospf_router" "test" {
  router_id              = %[1]q
  announce_default_route = %[2]t

  areas = [{
    area_id     = "0.0.0.0"
    name        = "backbone"
    area_type   = "normal"
    network_ids = [unifi_network.ospf.id]
  }]
}
`, routerID, announceDefaultRoute)
}
