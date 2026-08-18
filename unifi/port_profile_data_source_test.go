package unifi

import (
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
