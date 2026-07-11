package unifi

import (
	"context"
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	fwresource "github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
	"github.com/ubiquiti-community/terraform-provider-unifi/unifi/util"
)

func Test_radioAiModelToData(t *testing.T) {
	ctx := context.Background()
	var diags diag.Diagnostics

	channelsNg, d := types.SetValueFrom(ctx, types.Int64Type, []int64{1, 6, 11})
	if d.HasError() {
		t.Fatal(d)
	}
	radios, d := types.SetValueFrom(ctx, types.StringType, []string{"ng", "na"})
	if d.HasError() {
		t.Fatal(d)
	}
	blEntry, d := types.ObjectValueFrom(ctx, radioAiChannelsBlacklistAttrTypes,
		settingRadioAiChannelsBlacklistModel{
			Channel:      types.Int64Value(2),
			ChannelWidth: types.Int64Value(20),
			Radio:        types.StringValue("ng"),
		})
	if d.HasError() {
		t.Fatal(d)
	}
	blacklist, d := types.SetValueFrom(ctx,
		types.ObjectType{AttrTypes: radioAiChannelsBlacklistAttrTypes},
		[]types.Object{blEntry})
	if d.HasError() {
		t.Fatal(d)
	}

	m := &settingRadioAiModel{
		Enabled:                     types.BoolValue(true),
		SettingPreference:           types.StringValue("manual"),
		AutoAdjustChannelsToCountry: types.BoolNull(),
		AutoChannelPresetsType:      types.StringNull(),
		Channels6E:                  types.SetNull(types.Int64Type),
		ChannelsNa:                  types.SetNull(types.Int64Type),
		ChannelsNg:                  channelsNg,
		ChannelsBlacklist:           blacklist,
		CronExpr:                    types.StringValue("0 3 * * *"),
		ExcludeDevices:              types.SetNull(types.StringType),
		HighPriorityDevices:         types.SetNull(types.StringType),
		HtModesNa:                   types.SetNull(types.Int64Type),
		HtModesNg:                   types.SetNull(types.Int64Type),
		Optimize:                    types.SetNull(types.StringType),
		Radios:                      radios,
		RadiosConfiguration: types.SetNull(
			types.ObjectType{AttrTypes: radioAiRadiosConfigurationAttrTypes}),
	}

	// Live controllers carry auto_enabled, which go-unifi does not model:
	// the raw merge must preserve it verbatim.
	data := map[string]any{
		"auto_enabled":                    false,
		"auto_adjust_channels_to_country": true,
	}

	radioAiModelToData(ctx, m, data, &diags)
	if diags.HasError() {
		t.Fatal(diags)
	}

	if data["auto_enabled"] != false {
		t.Fatal("unmodeled auto_enabled was clobbered")
	}
	if data["auto_adjust_channels_to_country"] != true {
		t.Fatal("null auto_adjust_channels_to_country overwrote remote value")
	}
	if data["enabled"] != true || data["setting_preference"] != "manual" {
		t.Fatalf("enabled/setting_preference wrong: %v", data)
	}
	if data["cron_expr"] != "0 3 * * *" {
		t.Fatalf("cron_expr = %v", data["cron_expr"])
	}
	ng, ok := data["channels_ng"].([]int64)
	if !ok || len(ng) != 3 {
		t.Fatalf("channels_ng = %v", data["channels_ng"])
	}
	if _, present := data["channels_na"]; present {
		t.Fatal("null channels_na should not be written")
	}
	bl, ok := data["channels_blacklist"].([]map[string]any)
	if !ok || len(bl) != 1 || bl[0]["channel"] != int64(2) ||
		bl[0]["channel_width"] != int64(20) || bl[0]["radio"] != "ng" {
		t.Fatalf("channels_blacklist = %v", data["channels_blacklist"])
	}
	rd, ok := data["radios"].([]string)
	if !ok || len(rd) != 2 {
		t.Fatalf("radios = %v", data["radios"])
	}
}

func Test_radioAiSettingToModel(t *testing.T) {
	ctx := context.Background()
	var diags diag.Diagnostics

	m := radioAiSettingToModel(ctx, &settings.RadioAi{
		Enabled:                     true,
		SettingPreference:           "auto",
		AutoAdjustChannelsToCountry: true,
		AutoChannelPresetsType:      "maximum_speed",
		ChannelsNg:                  []int64{1, 6, 11},
		ChannelsBlacklist: []settings.SettingRadioAiChannelsBlacklist{
			{Channel: util.Ptr(int64(2)), ChannelWidth: util.Ptr(int64(20)), Radio: "ng"},
		},
		CronExpr: "0 3 * * *",
		Radios:   []string{"ng", "na"},
		RadiosConfiguration: []settings.SettingRadioAiRadiosConfiguration{
			{ChannelWidth: util.Ptr(int64(80)), Dfs: true, Radio: "na"},
		},
	}, &diags)
	if diags.HasError() {
		t.Fatal(diags)
	}

	if !m.Enabled.ValueBool() || m.SettingPreference.ValueString() != "auto" {
		t.Fatalf("enabled/setting_preference = %v/%v", m.Enabled, m.SettingPreference)
	}
	if m.AutoChannelPresetsType.ValueString() != "maximum_speed" {
		t.Fatalf("auto_channel_presets_type = %v", m.AutoChannelPresetsType)
	}
	var ng []int64
	diags.Append(m.ChannelsNg.ElementsAs(ctx, &ng, false)...)
	if len(ng) != 3 {
		t.Fatalf("channels_ng = %v", ng)
	}
	var bl []settingRadioAiChannelsBlacklistModel
	diags.Append(m.ChannelsBlacklist.ElementsAs(ctx, &bl, false)...)
	if len(bl) != 1 || bl[0].Channel.ValueInt64() != 2 || bl[0].Radio.ValueString() != "ng" {
		t.Fatalf("channels_blacklist = %v", bl)
	}
	var rc []settingRadioAiRadiosConfigurationModel
	diags.Append(m.RadiosConfiguration.ElementsAs(ctx, &rc, false)...)
	if len(rc) != 1 || rc[0].ChannelWidth.ValueInt64() != 80 || !rc[0].Dfs.ValueBool() {
		t.Fatalf("radios_configuration = %v", rc)
	}
}

func Test_settingResource_Schema_radioAi(t *testing.T) {
	r := &settingResource{}
	var resp fwresource.SchemaResponse
	r.Schema(context.Background(), fwresource.SchemaRequest{}, &resp)
	if _, ok := resp.Schema.Attributes["radio_ai"]; !ok {
		t.Fatal("schema is missing the radio_ai section attribute")
	}
}

func TestAccSettingResource_radioAi(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccSettingConfig_radioAi(true, "manual"),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"unifi_setting.test", "radio_ai.enabled", "true",
					),
					resource.TestCheckResourceAttr(
						"unifi_setting.test", "radio_ai.setting_preference", "manual",
					),
				),
			},
			{
				Config: testAccSettingConfig_radioAi(true, "auto"),
				Check: resource.TestCheckResourceAttr(
					"unifi_setting.test", "radio_ai.setting_preference", "auto",
				),
			},
		},
	})
}

func testAccSettingConfig_radioAi(enabled bool, pref string) string {
	return fmt.Sprintf(`
resource "unifi_setting" "test" {
  radio_ai = {
    enabled            = %t
    setting_preference = %q
  }
}
`, enabled, pref)
}
