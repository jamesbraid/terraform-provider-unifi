package unifi

import (
	"context"
	"testing"

	fwdatasource "github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func TestAccFirewallZoneDataSource_basic(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: `
resource "unifi_network" "firewall_zone_data_test" {
  name   = "Firewall Zone Data Test Network"
  subnet = "192.168.251.1/24"
  vlan   = 251

  # A zone claiming this network flips it to manual controller-side, while the
  # schema default asks for auto. Declare the end state so the post-apply
  # refresh plan stays empty.
  setting_preference = "manual"
}

resource "unifi_firewall_zone" "firewall_zone_data_test" {
  name        = "Firewall Zone Data Test"
  network_ids = [unifi_network.firewall_zone_data_test.id]
}

data "unifi_firewall_zone" "test" {
  name = unifi_firewall_zone.firewall_zone_data_test.name
}
`,
			Check: resource.TestCheckResourceAttr(
				"data.unifi_firewall_zone.test", "name", "Firewall Zone Data Test",
			),
		}},
	})
}

func TestNewFirewallZoneDataSource(t *testing.T) {
	d := NewFirewallZoneDataSource()
	if d == nil {
		t.Fatal("NewFirewallZoneDataSource() returned nil")
	}
	if _, ok := d.(fwdatasource.DataSourceWithConfigure); !ok {
		t.Error("expected DataSourceWithConfigure interface")
	}
}

func Test_firewallZoneDataSource_Schema(t *testing.T) {
	d := &firewallZoneDataSource{}
	resp := &fwdatasource.SchemaResponse{}
	d.Schema(context.Background(), fwdatasource.SchemaRequest{}, resp)
	if resp.Diagnostics.HasError() {
		t.Errorf("Schema() produced errors: %v", resp.Diagnostics)
	}
	for _, attr := range []string{"id", "site", "name", "zone_key", "network_ids"} {
		if _, ok := resp.Schema.Attributes[attr]; !ok {
			t.Errorf("missing attribute %q", attr)
		}
	}
}
