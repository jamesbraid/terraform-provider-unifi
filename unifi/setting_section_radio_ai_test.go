package unifi

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
)

// goldenRadioAi is this section's fresh golden PUT body constant (the
// "configured, manual mode" fixture from the design spec), captured from
// overlay()'s actual output against an empty snapshot — see
// TestRadioAiSection_GoldenReproduction.
const goldenRadioAi = `{
  "key": "radio_ai",
  "enabled": true,
  "setting_preference": "manual",
  "auto_channel_presets_type": "custom",
  "auto_adjust_channels_to_country": false,
  "radios": ["na", "ng"],
  "optimize": ["channel"],
  "channels_na": [36, 40, 44],
  "channels_ng": [1, 6, 11],
  "ht_modes_na": [40],
  "exclude_devices": ["aa:bb:cc:00:00:01"],
  "cron_expr": "0 3 * * *"
}`

// radioAiNullListsModel returns a settingRadioAiModel with every List field
// set to an explicit null-of-its-element-type. Tests override only the
// fields they care about, so types.ObjectValueFrom always sees a valid
// (non-zero-value) types.List for every list-typed attribute.
func radioAiNullListsModel() settingRadioAiModel {
	return settingRadioAiModel{
		Radios:              types.ListNull(types.StringType),
		Optimize:            types.ListNull(types.StringType),
		ExcludeDevices:      types.ListNull(types.StringType),
		HighPriorityDevices: types.ListNull(types.StringType),
		ChannelsNa:          types.ListNull(types.Int64Type),
		ChannelsNg:          types.ListNull(types.Int64Type),
		Channels6E:          types.ListNull(types.Int64Type),
		HtModesNa:           types.ListNull(types.Int64Type),
		HtModesNg:           types.ListNull(types.Int64Type),
		RadiosConfiguration: types.ListNull(types.ObjectType{AttrTypes: radioAiRadiosConfigurationAttrTypes}),
		ChannelsBlacklist:   types.ListNull(types.ObjectType{AttrTypes: radioAiChannelsBlacklistAttrTypes}),
	}
}

// TestRadioAiSection_GoldenReproduction proves overlay() reproduces this
// section's golden PUT body for the "configured, manual mode" fixture.
func TestRadioAiSection_GoldenReproduction(t *testing.T) {
	ctx := context.Background()
	sec := radioAiSection{}

	radios, diags := types.ListValueFrom(ctx, types.StringType, []string{"na", "ng"})
	if diags.HasError() {
		t.Fatalf("building radios: %v", diags)
	}
	optimize, diags := types.ListValueFrom(ctx, types.StringType, []string{"channel"})
	if diags.HasError() {
		t.Fatalf("building optimize: %v", diags)
	}
	channelsNa, diags := types.ListValueFrom(ctx, types.Int64Type, []int64{36, 40, 44})
	if diags.HasError() {
		t.Fatalf("building channels_na: %v", diags)
	}
	channelsNg, diags := types.ListValueFrom(ctx, types.Int64Type, []int64{1, 6, 11})
	if diags.HasError() {
		t.Fatalf("building channels_ng: %v", diags)
	}
	htModesNa, diags := types.ListValueFrom(ctx, types.Int64Type, []int64{40})
	if diags.HasError() {
		t.Fatalf("building ht_modes_na: %v", diags)
	}
	excludeDevices, diags := types.ListValueFrom(ctx, types.StringType, []string{"aa:bb:cc:00:00:01"})
	if diags.HasError() {
		t.Fatalf("building exclude_devices: %v", diags)
	}

	m := radioAiNullListsModel()
	m.Enabled = types.BoolValue(true)
	m.SettingPreference = types.StringValue("manual")
	m.AutoChannelPresetsType = types.StringValue("custom")
	m.AutoAdjustChannelsToCountry = types.BoolValue(false)
	m.CronExpr = types.StringValue("0 3 * * *")
	m.Radios = radios
	m.Optimize = optimize
	m.ChannelsNa = channelsNa
	m.ChannelsNg = channelsNg
	m.HtModesNa = htModesNa
	m.ExcludeDevices = excludeDevices

	obj, objDiags := types.ObjectValueFrom(ctx, radioAiAttrTypes, m)
	if objDiags.HasError() {
		t.Fatalf("building radio_ai object: %v", objDiags)
	}

	model := settingResourceModel{RadioAi: obj}
	prior := settingResourceModel{}
	snap := newRawSettings(nil) // empty snapshot: section absent

	rs, configured, oDiags := sec.overlay(ctx, model, prior, snap)
	if oDiags.HasError() {
		t.Fatalf("overlay diagnostics: %v", oDiags)
	}
	if !configured {
		t.Fatalf("overlay configured = false, want true")
	}
	if rs.Key != "radio_ai" {
		t.Errorf("rs.Key = %q, want %q", rs.Key, "radio_ai")
	}

	assertPUTBodyMatchesGolden(t, rs, goldenRadioAi)
}

// TestRadioAiSection_DecodeRoundTrip proves decode() reads the "configured,
// manual mode" fixture's fields from a snapshot section's data.
func TestRadioAiSection_DecodeRoundTrip(t *testing.T) {
	ctx := context.Background()
	sec := radioAiSection{}

	snap := newRawSettings([]settings.RawSetting{{
		BaseSetting: settings.BaseSetting{Key: "radio_ai"},
		Data: map[string]any{
			"enabled":                         true,
			"setting_preference":              "manual",
			"auto_channel_presets_type":       "custom",
			"auto_adjust_channels_to_country": false,
			"radios":                          []any{"na", "ng"},
			"optimize":                        []any{"channel"},
			"channels_na":                     []any{float64(36), float64(40), float64(44)},
			"channels_ng":                     []any{float64(1), float64(6), float64(11)},
			"ht_modes_na":                     []any{float64(40)},
			"exclude_devices":                 []any{"aa:bb:cc:00:00:01"},
			"cron_expr":                       "0 3 * * *",
			"default":                         false,
			"useXY":                           false,
		},
	}})

	prior := settingResourceModel{}
	model := settingResourceModel{}

	diags := sec.decode(ctx, snap, prior, &model)
	if diags.HasError() {
		t.Fatalf("decode diagnostics: %v", diags)
	}

	if model.RadioAi.IsNull() || model.RadioAi.IsUnknown() {
		t.Fatalf("model.RadioAi is null/unknown after decode")
	}

	var got settingRadioAiModel
	if diags := model.RadioAi.As(ctx, &got, basetypes.ObjectAsOptions{}); diags.HasError() {
		t.Fatalf("extracting settingRadioAiModel: %v", diags)
	}

	if !got.Enabled.ValueBool() {
		t.Errorf("Enabled = %v, want true", got.Enabled)
	}
	if got.SettingPreference.ValueString() != "manual" {
		t.Errorf("SettingPreference = %q, want %q", got.SettingPreference.ValueString(), "manual")
	}
	if got.AutoChannelPresetsType.ValueString() != "custom" {
		t.Errorf("AutoChannelPresetsType = %q, want %q", got.AutoChannelPresetsType.ValueString(), "custom")
	}
	if got.AutoAdjustChannelsToCountry.ValueBool() {
		t.Errorf("AutoAdjustChannelsToCountry = %v, want false", got.AutoAdjustChannelsToCountry)
	}
	var radios []string
	if diags := got.Radios.ElementsAs(ctx, &radios, false); diags.HasError() {
		t.Fatalf("extracting Radios: %v", diags)
	}
	if len(radios) != 2 || radios[0] != "na" || radios[1] != "ng" {
		t.Errorf("Radios = %v, want [na ng]", radios)
	}
	var channelsNa []int64
	if diags := got.ChannelsNa.ElementsAs(ctx, &channelsNa, false); diags.HasError() {
		t.Fatalf("extracting ChannelsNa: %v", diags)
	}
	if len(channelsNa) != 3 || channelsNa[0] != 36 || channelsNa[1] != 40 || channelsNa[2] != 44 {
		t.Errorf("ChannelsNa = %v, want [36 40 44]", channelsNa)
	}
	if got.CronExpr.ValueString() != "0 3 * * *" {
		t.Errorf("CronExpr = %q, want %q", got.CronExpr.ValueString(), "0 3 * * *")
	}
}

// TestRadioAiSection_Preservation proves overlay() preserves the "default"
// and "useXY" fields (deliberately unmodeled) plus any other unmodeled key,
// using the design spec's "auto mode, controller-rewritten channels"
// fixture.
func TestRadioAiSection_Preservation(t *testing.T) {
	ctx := context.Background()
	sec := radioAiSection{}

	snap := newRawSettings([]settings.RawSetting{{
		BaseSetting: settings.BaseSetting{Key: "radio_ai"},
		Data: map[string]any{
			"enabled":              true,
			"setting_preference":   "auto",
			"channels_na":          []any{float64(36), float64(149)},
			"channels_ng":          []any{float64(6)},
			"radios_configuration": []any{map[string]any{"radio": "na", "channel_width": float64(80), "dfs": true}},
			"default":              true,
			"useXY":                false,
		},
	}})

	m := radioAiNullListsModel()
	m.Enabled = types.BoolValue(true)
	m.SettingPreference = types.StringValue("auto")
	obj, diags := types.ObjectValueFrom(ctx, radioAiAttrTypes, m)
	if diags.HasError() {
		t.Fatalf("building radio_ai object: %v", diags)
	}

	model := settingResourceModel{RadioAi: obj}
	prior := settingResourceModel{}

	rs, configured, oDiags := sec.overlay(ctx, model, prior, snap)
	if oDiags.HasError() {
		t.Fatalf("overlay diagnostics: %v", oDiags)
	}
	if !configured {
		t.Fatalf("overlay configured = false, want true")
	}

	if rs.Data["default"] != true {
		t.Errorf(`rs.Data["default"] = %v, want true (unmodeled field must survive RMW untouched)`, rs.Data["default"])
	}
	if rs.Data["useXY"] != false {
		t.Errorf(`rs.Data["useXY"] = %v, want false (unmodeled field must survive RMW untouched)`, rs.Data["useXY"])
	}
}

// TestRadioAiSection_NotConfigured proves overlay() returns configured ==
// false and a zero-value RawSetting when model.RadioAi is null.
func TestRadioAiSection_NotConfigured(t *testing.T) {
	ctx := context.Background()
	sec := radioAiSection{}

	model := settingResourceModel{RadioAi: types.ObjectNull(radioAiAttrTypes)}
	prior := settingResourceModel{}
	snap := newRawSettings(nil)

	rs, configured, oDiags := sec.overlay(ctx, model, prior, snap)
	if oDiags.HasError() {
		t.Fatalf("overlay diagnostics: %v", oDiags)
	}
	if configured {
		t.Fatalf("overlay configured = true, want false")
	}
	if rs.Key != "" || len(rs.Data) != 0 {
		t.Errorf("overlay returned non-zero RawSetting when not configured: %+v", rs)
	}
}

// TestRadioAiSection_InterfaceWiring is a light structural check that the
// section is registered and key()/attrName() both return "radio_ai".
func TestRadioAiSection_InterfaceWiring(t *testing.T) {
	sec := radioAiSection{}
	if sec.key() != "radio_ai" {
		t.Errorf("key() = %q, want %q", sec.key(), "radio_ai")
	}
	if sec.attrName() != "radio_ai" {
		t.Errorf("attrName() = %q, want %q", sec.attrName(), "radio_ai")
	}

	var found bool
	for _, s := range settingSections {
		if s.key() == "radio_ai" && s.attrName() == "radio_ai" {
			found = true
		}
	}
	if !found {
		t.Errorf("no section in settingSections registry has key()==attrName()==%q", "radio_ai")
	}
}

// TestRadioAiSection_CoManagedPreservesControllerChurn proves the section's
// actual design decision: when setting_preference = "auto" and the user
// never configured channels_na (model value null), overlay() must NOT
// clobber a controller-optimizer-rewritten value already in the snapshot;
// but when the user DOES configure channels_na, overlay() sends the user's
// value every apply regardless of setting_preference (last-writer-wins is
// the apply, matching plain Managed semantics — the CoManaged/Managed
// distinction is schema-plan-modifier-only, not an overlay-layer branch).
func TestRadioAiSection_CoManagedPreservesControllerChurn(t *testing.T) {
	ctx := context.Background()
	sec := radioAiSection{}

	// Controller snapshot: setting_preference=auto, channels_na was
	// rewritten by the optimizer to [36, 149] since the user last applied.
	snap := newRawSettings([]settings.RawSetting{{
		BaseSetting: settings.BaseSetting{Key: "radio_ai"},
		Data: map[string]any{
			"enabled":              true,
			"setting_preference":   "auto",
			"channels_na":          []any{float64(36), float64(149)},
			"channels_ng":          []any{float64(6)},
			"radios_configuration": []any{map[string]any{"radio": "na", "channel_width": float64(80), "dfs": true}},
			"default":              true,
			"useXY":                false,
		},
	}})

	t.Run("user never configured channels_na: overlay preserves the controller's value", func(t *testing.T) {
		m := radioAiNullListsModel()
		m.Enabled = types.BoolValue(true)
		m.SettingPreference = types.StringValue("auto")
		// ChannelsNa left null: never configured.
		obj, diags := types.ObjectValueFrom(ctx, radioAiAttrTypes, m)
		if diags.HasError() {
			t.Fatalf("building radio_ai object: %v", diags)
		}
		model := settingResourceModel{RadioAi: obj}
		rs, configured, oDiags := sec.overlay(ctx, model, settingResourceModel{}, snap)
		if oDiags.HasError() {
			t.Fatalf("overlay diagnostics: %v", oDiags)
		}
		if !configured {
			t.Fatal("overlay configured = false, want true")
		}
		got, ok := rs.Data["channels_na"].([]any)
		if !ok || len(got) != 2 || got[0] != float64(36) || got[1] != float64(149) {
			t.Errorf("rs.Data[channels_na] = %v, want the controller's untouched [36, 149] (unconfigured CoManaged field must survive RMW, not be clobbered by a null write)", rs.Data["channels_na"])
		}
	})

	t.Run("user pins channels_na: overlay sends the pinned value even under auto", func(t *testing.T) {
		// 104/108/112 are valid NA (5GHz) channels per RadioAi.ChannelsNa's
		// struct comment. Deliberately NOT [1, 6, 11] — those are
		// 2.4GHz-only channel numbers, invalid for channels_na's enum.
		// Also chosen distinct from the snapshot's pre-existing [36, 149]
		// so the assertion actually proves the pinned value wins.
		pinned, diags := types.ListValueFrom(ctx, types.Int64Type, []int64{104, 108, 112})
		if diags.HasError() {
			t.Fatalf("building channels_na list: %v", diags)
		}
		m := radioAiNullListsModel()
		m.Enabled = types.BoolValue(true)
		m.SettingPreference = types.StringValue("auto") // still auto
		m.ChannelsNa = pinned                           // but user pinned a value
		obj, diags := types.ObjectValueFrom(ctx, radioAiAttrTypes, m)
		if diags.HasError() {
			t.Fatalf("building radio_ai object: %v", diags)
		}
		model := settingResourceModel{RadioAi: obj}
		rs, configured, oDiags := sec.overlay(ctx, model, settingResourceModel{}, snap)
		if oDiags.HasError() {
			t.Fatalf("overlay diagnostics: %v", oDiags)
		}
		if !configured {
			t.Fatal("overlay configured = false, want true")
		}
		got, ok := rs.Data["channels_na"].([]any)
		if !ok || len(got) != 3 || got[0] != float64(104) || got[1] != float64(108) || got[2] != float64(112) {
			t.Errorf("rs.Data[channels_na] = %v, want the user-pinned [104, 108, 112] (configured CoManaged field must write like Managed, even under setting_preference=auto — no plan-time rejection per design spec)", rs.Data["channels_na"])
		}
	})

	t.Run("default and useXY are never modeled, always preserved", func(t *testing.T) {
		m := radioAiNullListsModel()
		m.Enabled = types.BoolValue(true)
		m.SettingPreference = types.StringValue("auto")
		obj, diags := types.ObjectValueFrom(ctx, radioAiAttrTypes, m)
		if diags.HasError() {
			t.Fatalf("building radio_ai object: %v", diags)
		}
		model := settingResourceModel{RadioAi: obj}
		rs, _, oDiags := sec.overlay(ctx, model, settingResourceModel{}, snap)
		if oDiags.HasError() {
			t.Fatalf("overlay diagnostics: %v", oDiags)
		}
		if rs.Data["default"] != true {
			t.Errorf(`rs.Data["default"] = %v, want true (unmodeled field must survive RMW untouched)`, rs.Data["default"])
		}
		if rs.Data["useXY"] != false {
			t.Errorf(`rs.Data["useXY"] = %v, want false (unmodeled field must survive RMW untouched)`, rs.Data["useXY"])
		}
	})
}
