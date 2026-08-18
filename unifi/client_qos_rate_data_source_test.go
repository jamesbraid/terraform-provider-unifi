package unifi

import (
	"testing"

	fwdatasource "github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func TestAccClientQosRateDataSource_basic(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: testAccClientQosRateConfig_qos() + `
data "unifi_client_qos_rate" "test" {
  name = unifi_client_qos_rate.test.name
}
`,
			Check: resource.ComposeTestCheckFunc(
				resource.TestCheckResourceAttrSet("data.unifi_client_qos_rate.test", "id"),
				resource.TestCheckResourceAttr("data.unifi_client_qos_rate.test", "name", "tfacc-qos-group"),
				resource.TestCheckResourceAttr("data.unifi_client_qos_rate.test", "qos_rate_max_down", "1000"),
				resource.TestCheckResourceAttr("data.unifi_client_qos_rate.test", "qos_rate_max_up", "500"),
			),
		}},
	})
}

func TestNewClientQosRateDataSource(t *testing.T) {
	d := NewClientQosRateDataSource()
	if d == nil {
		t.Fatal("NewClientQosRateDataSource() returned nil")
	}
	if _, ok := d.(fwdatasource.DataSourceWithConfigure); !ok {
		t.Error("expected DataSourceWithConfigure interface")
	}
}
