package unifi

import (
	"testing"

	fwdatasource "github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func TestAccClientInfoListDataSource_basic(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccClientInfoListDataSourceConfig_basic(),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet(
						"data.unifi_client_info_list.test",
						"clients.#",
					),
					resource.TestCheckResourceAttr(
						"data.unifi_client_info_list.test",
						"site",
						"default",
					),
				),
			},
		},
	})
}

func testAccClientInfoListDataSourceConfig_basic() string {
	return `
data "unifi_client_info_list" "test" {
}
`
}

func TestNewClientInfoListDataSource(t *testing.T) {
	d := NewClientInfoListDataSource()
	if d == nil {
		t.Fatal("NewClientInfoListDataSource() returned nil")
	}
	if _, ok := d.(fwdatasource.DataSourceWithConfigure); !ok {
		t.Error("expected DataSourceWithConfigure interface")
	}
}
