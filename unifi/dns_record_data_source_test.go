package unifi

import (
	"context"
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

func Test_dnsRecordDataSource_Schema(t *testing.T) {
	d := &dnsRecordDataSource{}
	resp := &fwdatasource.SchemaResponse{}
	d.Schema(context.Background(), fwdatasource.SchemaRequest{}, resp)
	if resp.Diagnostics.HasError() {
		t.Errorf("Schema() produced errors: %v", resp.Diagnostics)
	}
	for _, attr := range []string{"id", "site", "name", "type", "value"} {
		if _, ok := resp.Schema.Attributes[attr]; !ok {
			t.Errorf("missing attribute %q", attr)
		}
	}
}

func Test_dnsRecordDataSource_Configure(t *testing.T) {
	tests := []struct {
		name      string
		data      any
		wantError bool
	}{
		{"nil provider data", nil, false},
		{"wrong type", "wrong", true},
		{"correct client type", &Client{Site: "default"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := &dnsRecordDataSource{}
			resp := &fwdatasource.ConfigureResponse{}
			d.Configure(
				context.Background(),
				fwdatasource.ConfigureRequest{ProviderData: tt.data},
				resp,
			)
			if tt.wantError && !resp.Diagnostics.HasError() {
				t.Error("expected error in diagnostics")
			}
			if !tt.wantError && resp.Diagnostics.HasError() {
				t.Errorf("unexpected error: %v", resp.Diagnostics)
			}
		})
	}
}
