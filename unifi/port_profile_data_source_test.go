package unifi

import (
	"context"
	"testing"

	fwdatasource "github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func TestAccPortProfileDataSource_basic(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: testAccPortProfileFrameworkConfig_basic() + `
data "unifi_port_profile" "test" {
  name = unifi_port_profile.test.name
}
`,
			Check: resource.ComposeTestCheckFunc(
				resource.TestCheckResourceAttrSet("data.unifi_port_profile.test", "id"),
				resource.TestCheckResourceAttr("data.unifi_port_profile.test", "name", "Test Port Profile"),
				resource.TestCheckResourceAttrSet("data.unifi_port_profile.test", "site"),
			),
		}},
	})
}

func TestNewPortProfileDataSource(t *testing.T) {
	d := NewPortProfileDataSource()
	if d == nil {
		t.Fatal("NewPortProfileDataSource() returned nil")
	}
	if _, ok := d.(fwdatasource.DataSourceWithConfigure); !ok {
		t.Error("expected DataSourceWithConfigure interface")
	}
}

func Test_portProfileDataSource_Schema(t *testing.T) {
	d := &portProfileDataSource{}
	resp := &fwdatasource.SchemaResponse{}
	d.Schema(context.Background(), fwdatasource.SchemaRequest{}, resp)
	if resp.Diagnostics.HasError() {
		t.Errorf("Schema() produced errors: %v", resp.Diagnostics)
	}
	for _, attr := range []string{
		"id",
		"site",
		"name",
		"forward",
		"native_networkconf_id",
		"tagged_networkconf_ids",
		"excluded_networkconf_ids",
		"tagged_vlan_mgmt",
	} {
		if _, ok := resp.Schema.Attributes[attr]; !ok {
			t.Errorf("missing attribute %q", attr)
		}
	}
}
