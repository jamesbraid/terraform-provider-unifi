package unifi

import (
	"testing"

	fwdatasource "github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func TestAccDNSRecordDataSource_basic(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: `
resource "unifi_dns_record" "test" {
  name        = "tf-acc-dns-data.example.invalid"
  enabled     = true
  record_type = "A"
  ttl         = "5m0s"
  value       = "192.0.2.10"
}

data "unifi_dns_record" "test" {
  name = unifi_dns_record.test.name
}
`,
			Check: resource.ComposeTestCheckFunc(
				resource.TestCheckResourceAttrSet("data.unifi_dns_record.test", "id"),
				resource.TestCheckResourceAttr("data.unifi_dns_record.test", "name", "tf-acc-dns-data.example.invalid"),
				resource.TestCheckResourceAttr("data.unifi_dns_record.test", "type", "A"),
				resource.TestCheckResourceAttr("data.unifi_dns_record.test", "value", "192.0.2.10"),
				resource.TestCheckResourceAttr("data.unifi_dns_record.test", "ttl", "5m0s"),
			),
		}},
	})
}

func TestNewDNSRecordDataSource(t *testing.T) {
	d := NewDNSRecordDataSource()
	if d == nil {
		t.Fatal("NewDNSRecordDataSource() returned nil")
	}
	if _, ok := d.(fwdatasource.DataSourceWithConfigure); !ok {
		t.Error("expected DataSourceWithConfigure interface")
	}
}
