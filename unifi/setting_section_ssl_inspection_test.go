package unifi

import (
	"context"
	"testing"

	fwresource "github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
)

func Test_sslInspectionModelToData(t *testing.T) {
	m := &settingSslInspectionModel{State: types.StringValue("simple")}
	// The live controller document carries identity-certificate fields that
	// go-unifi does not model; the raw merge must preserve them verbatim.
	data := map[string]any{
		"identity_certificate_all_users": true,
		"identity_certificate_groups":    []any{},
	}

	sslInspectionModelToData(m, data)

	if data["state"] != "simple" {
		t.Fatalf("state = %v", data["state"])
	}
	if data["identity_certificate_all_users"] != true {
		t.Fatal("unmodeled identity_certificate_all_users was clobbered")
	}
	if _, present := data["identity_certificate_groups"]; !present {
		t.Fatal("unmodeled identity_certificate_groups was dropped")
	}
}

func Test_sslInspectionModelToData_nullLeavesRemoteValue(t *testing.T) {
	m := &settingSslInspectionModel{State: types.StringNull()}
	data := map[string]any{"state": "advanced"}

	sslInspectionModelToData(m, data)

	if data["state"] != "advanced" {
		t.Fatalf("null state overwrote remote value: %v", data["state"])
	}
}

func Test_sslInspectionSettingToModel(t *testing.T) {
	m := sslInspectionSettingToModel(&settings.SslInspection{State: "off"})
	if m.State.ValueString() != "off" {
		t.Fatalf("state = %v", m.State)
	}
	empty := sslInspectionSettingToModel(&settings.SslInspection{})
	if !empty.State.IsNull() {
		t.Fatalf("empty state should map to null, got %v", empty.State)
	}
}

func Test_settingResource_Schema_sslInspection(t *testing.T) {
	r := &settingResource{}
	var resp fwresource.SchemaResponse
	r.Schema(context.Background(), fwresource.SchemaRequest{}, &resp)
	if _, ok := resp.Schema.Attributes["ssl_inspection"]; !ok {
		t.Fatal("schema is missing the ssl_inspection section attribute")
	}
}

func TestAccSettingResource_sslInspection(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccSettingConfig_sslInspection("off"),
				Check: resource.TestCheckResourceAttr(
					"unifi_setting.test", "ssl_inspection.state", "off",
				),
			},
			{
				Config: testAccSettingConfig_sslInspection("simple"),
				Check: resource.TestCheckResourceAttr(
					"unifi_setting.test", "ssl_inspection.state", "simple",
				),
			},
		},
	})
}

func testAccSettingConfig_sslInspection(state string) string {
	return `
resource "unifi_setting" "test" {
  ssl_inspection = {
    state = "` + state + `"
  }
}
`
}
