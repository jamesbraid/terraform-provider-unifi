package unifi

import (
	"testing"

	fwdatasource "github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func TestAccAPGroupDataSource_basic(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: testAccAPGroupFrameworkConfig_basic("tf-acc-apgroup-ds") + `
data "unifi_ap_group" "test" {
  name = unifi_ap_group.test.name
}
`,
			Check: resource.ComposeTestCheckFunc(
				resource.TestCheckResourceAttrSet("data.unifi_ap_group.test", "id"),
				resource.TestCheckResourceAttr("data.unifi_ap_group.test", "name", "tf-acc-apgroup-ds"),
				resource.TestCheckResourceAttr("data.unifi_ap_group.test", "device_macs.#", "0"),
			),
		}},
	})
}

func TestNewAPGroupDataSource(t *testing.T) {
	d := NewAPGroupDataSource()
	if d == nil {
		t.Fatal("NewAPGroupDataSource() returned nil")
	}
	if _, ok := d.(fwdatasource.DataSourceWithConfigure); !ok {
		t.Error("expected DataSourceWithConfigure interface")
	}
}
