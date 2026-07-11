package unifi

import (
	"context"
	"testing"

	fwresource "github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
)

func Test_localeModelToData(t *testing.T) {
	tz := "America/Vancouver"
	m := &settingLocaleModel{Timezone: types.StringValue(tz)}
	// Pre-existing raw fields must be preserved, not clobbered.
	data := map[string]any{"unmodeled_field": true}

	localeModelToData(m, data)

	if got := data["timezone"]; got != tz {
		t.Fatalf("timezone = %v, want %q", got, tz)
	}
	if got := data["unmodeled_field"]; got != true {
		t.Fatalf("unmodeled_field was clobbered: %v", got)
	}
}

func Test_localeModelToData_nullLeavesRemoteValue(t *testing.T) {
	m := &settingLocaleModel{Timezone: types.StringNull()}
	data := map[string]any{"timezone": "Etc/UTC"}

	localeModelToData(m, data)

	if got := data["timezone"]; got != "Etc/UTC" {
		t.Fatalf("null timezone overwrote remote value: %v", got)
	}
}

func Test_localeSettingToModel(t *testing.T) {
	m := localeSettingToModel(&settings.Locale{Timezone: "America/Vancouver"})
	if m.Timezone.ValueString() != "America/Vancouver" {
		t.Fatalf("timezone = %v", m.Timezone)
	}
	empty := localeSettingToModel(&settings.Locale{})
	if !empty.Timezone.IsNull() {
		t.Fatalf("empty timezone should map to null, got %v", empty.Timezone)
	}
}

func Test_settingResource_Schema_locale(t *testing.T) {
	r := &settingResource{}
	var resp fwresource.SchemaResponse
	r.Schema(context.Background(), fwresource.SchemaRequest{}, &resp)
	if _, ok := resp.Schema.Attributes["locale"]; !ok {
		t.Fatal("schema is missing the locale section attribute")
	}
}

func TestAccSettingResource_locale(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccSettingConfig_locale("America/Vancouver"),
				Check: resource.TestCheckResourceAttr(
					"unifi_setting.test", "locale.timezone", "America/Vancouver",
				),
			},
			{
				Config: testAccSettingConfig_locale("Etc/UTC"),
				Check: resource.TestCheckResourceAttr(
					"unifi_setting.test", "locale.timezone", "Etc/UTC",
				),
			},
		},
	})
}

func testAccSettingConfig_locale(tz string) string {
	return `
resource "unifi_setting" "test" {
  locale = {
    timezone = "` + tz + `"
  }
}
`
}
