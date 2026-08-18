package unifi

import (
	"testing"

	fwdatasource "github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func TestAccClientDataSource_basic(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccClientDataSourceConfig_basic(),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"data.unifi_client.test",
						"mac",
						"01:23:45:67:89:ae",
					),
					resource.TestCheckResourceAttr(
						"data.unifi_client.test",
						"name",
						"tfacc-data-user",
					),
					resource.TestCheckResourceAttrSet("data.unifi_client.test", "id"),
				),
			},
		},
	})
}

func testAccClientDataSourceConfig_basic() string {
	return `
resource "unifi_client" "test" {
	name = "tfacc-data-user"
	mac  = "01:23:45:67:89:ae"
}

data "unifi_client" "test" {
	mac = unifi_client.test.mac
}
`
}

func TestNewClientDataSource(t *testing.T) {
	d := NewClientDataSource()
	if d == nil {
		t.Fatal("NewClientDataSource() returned nil")
	}
	if _, ok := d.(fwdatasource.DataSourceWithConfigure); !ok {
		t.Error("expected DataSourceWithConfigure interface")
	}
}
