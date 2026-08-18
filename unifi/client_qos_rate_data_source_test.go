package unifi

import (
	"context"
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

func Test_clientQosRateDataSource_Schema(t *testing.T) {
	d := &clientQosRateDataSource{}
	resp := &fwdatasource.SchemaResponse{}
	d.Schema(context.Background(), fwdatasource.SchemaRequest{}, resp)
	if resp.Diagnostics.HasError() {
		t.Errorf("Schema() produced errors: %v", resp.Diagnostics)
	}
	for _, attr := range []string{"id", "site", "name", "qos_rate_max_down", "qos_rate_max_up"} {
		if _, ok := resp.Schema.Attributes[attr]; !ok {
			t.Errorf("missing attribute %q", attr)
		}
	}
}

func Test_clientQosRateDataSource_Configure(t *testing.T) {
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
			d := &clientQosRateDataSource{}
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
