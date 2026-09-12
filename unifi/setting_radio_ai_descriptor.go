package unifi

// The radio_ai section descriptor: the largest and structurally most
// awkward section in this dispatch. RadioAi is UniFi's AI-driven radio
// channel/power optimization feature -- the controller is expected to
// actively rewrite channel and power assignments while it runs, so this
// section cannot use the plain unconditional-mirror hydration every other
// section in this dispatch uses (see radioAiAfterReceive's own comment).
// It also needs a Field kind nothing before it required: five of its own
// top-level fields (channels_6e, channels_na, channels_ng, ht_modes_na,
// ht_modes_ng) are []int64, and no existing resourcekit.Field kind covered
// that shape -- see internal/resourcekit/field.go's Int64ListField, added
// in this same commit, which mirrors StringListField almost exactly.
//
// default and useXY are settings.RadioAi's remaining two top-level fields
// and are deliberately NOT modeled: default reads as a controller-owned
// "is this the factory-default profile" marker rather than a practitioner
// lever (no `attr_*` prefix, but the same shape), and useXY's meaning
// cannot be determined at all -- no wire comment, no sibling field to infer
// it from, and its own wire name (`useXY`, camelCase) is already an
// outlier in an otherwise snake_case vocabulary, itself a signal this is
// an internal/legacy flag rather than something a config author would set.
//
// channels_blacklist and radios_configuration's element members
// (channel/channel_width/radio, channel_width/dfs/radio) are left
// Optional+Computed rather than Required, unlike ether_lighting's two
// override lists: ether_lighting's {key, raw_color_hex} pair is
// meaningless with either field missing, but a channels_blacklist entry
// specifying only `radio` (blacklisting a whole band) or only `radio` +
// `channel` (no explicit width) is a plausible partial specification the
// SDK's own all-omitempty tags don't rule out -- there is no sibling-field
// argument here the way there was for ether_lighting.
//
// Like every other top-level settings document in this batch, RadioAi's
// own fields get no compiler-derived validator (see
// setting_global_switch_descriptor.go's comment for the mechanism), so
// auto_channel_presets_type's and setting_preference's OneOf are
// hand-transcribed in provider-codegen/policy/setting.json. The two nested
// list_nested element types (SettingRadioAiChannelsBlacklist,
// SettingRadioAiRadiosConfiguration) DO match a settings.FieldConstraints
// key exactly, so the compiler derives channel_width's and radio's OneOf
// on its own -- confirmed by reading the generated schema after `go
// generate`, no hand validator was added or needed for either.
//
// Three nested *int64 element fields reject a literal zero and are
// invisible to every existing OmitZero conformance check (that check only
// walks a Spec's own top-level Int64PtrFields; these three live inside a
// hand-written ObjectListField Encode closure instead):
//   - channels_blacklist[].channel: pattern excludes 0 (starts at [1-9]).
//   - channels_blacklist[].channel_width: Int64Values {20,40,80,160,240,320}
//     does not contain 0.
//   - radios_configuration[].channel_width: Int64Values {20,40,80,160,320}
//     does not contain 0.
//
// radioAiOmitZeroInt64 is the by-hand equivalent of Int64PtrField's own
// OmitZero: it returns nil (omitted from the wire, matching the struct's
// own omitempty tag) rather than a pointer to zero, for exactly the same
// reason Int64PtrField.ToSDK's own OmitZero branch exists.
import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/resourcekit"
)

// radioAiOmitZeroInt64 returns nil -- omitted from the wire by the SDK's
// own omitempty tag -- rather than a pointer to zero, for a model value
// that is null, unknown, or an explicit 0. See this file's own top comment
// for which three nested fields need it and why.
func radioAiOmitZeroInt64(value types.Int64) *int64 {
	if value.IsNull() || value.IsUnknown() {
		return nil
	}
	v := value.ValueInt64()
	if v == 0 {
		return nil
	}
	return &v
}

// radioAiKitSpec maps every attribute of the generated radio_ai schema
// (resource_setting/setting_resource_gen.go's "radio_ai"
// SingleNestedAttribute) onto settings.RadioAi.
// auto_channel_presets_type and setting_preference each carry a OneOf that
// rejects "", so both want NullZero; every list wants KeepZero, matching
// every other list in this dispatch (no list attribute here carries a
// zero-rejecting validator of its own).
func radioAiKitSpec() resourcekit.Spec[settingRadioAiModel, settings.RadioAi] {
	return resourcekit.Spec[settingRadioAiModel, settings.RadioAi]{
		TypeName: "setting_radio_ai",
		Subject:  "Radio AI Setting",
		New:      func() *settings.RadioAi { return &settings.RadioAi{} },
		Fields: resourcekit.Override(settingRadioAiGenFields(), []resourcekit.Field[settingRadioAiModel, settings.RadioAi]{
			resourcekit.ObjectListField[
				settingRadioAiModel, settings.RadioAi, settings.SettingRadioAiChannelsBlacklist,
			]{
				Wire:  "channels_blacklist",
				Model: func(m *settingRadioAiModel) *types.List { return &m.ChannelsBlacklist },
				SDK: func(s *settings.RadioAi) *[]settings.SettingRadioAiChannelsBlacklist {
					return &s.ChannelsBlacklist
				},
				AttrTypes: radioAiChannelsBlacklistAttrTypes,
				Encode:    radioAiChannelsBlacklistEncode,
				Decode:    radioAiChannelsBlacklistDecode,
				Elide:     resourcekit.KeepZero,
			},
			resourcekit.ObjectListField[
				settingRadioAiModel, settings.RadioAi, settings.SettingRadioAiRadiosConfiguration,
			]{
				Wire:  "radios_configuration",
				Model: func(m *settingRadioAiModel) *types.List { return &m.RadiosConfiguration },
				SDK: func(s *settings.RadioAi) *[]settings.SettingRadioAiRadiosConfiguration {
					return &s.RadiosConfiguration
				},
				AttrTypes: radioAiRadiosConfigurationAttrTypes,
				Encode:    radioAiRadiosConfigurationEncode,
				Decode:    radioAiRadiosConfigurationDecode,
				Elide:     resourcekit.KeepZero,
			},
		}),
	}
}

func radioAiChannelsBlacklistEncode(
	ctx context.Context, object types.Object,
) (settings.SettingRadioAiChannelsBlacklist, diag.Diagnostics) {
	var model radioAiChannelsBlacklistModel
	diags := object.As(ctx, &model, basetypes.ObjectAsOptions{})
	return settings.SettingRadioAiChannelsBlacklist{
		Channel:      radioAiOmitZeroInt64(model.Channel),
		ChannelWidth: radioAiOmitZeroInt64(model.ChannelWidth),
		Radio:        model.Radio.ValueString(),
	}, diags
}

func radioAiChannelsBlacklistDecode(
	ctx context.Context, element settings.SettingRadioAiChannelsBlacklist,
) (types.Object, diag.Diagnostics) {
	return types.ObjectValueFrom(ctx, radioAiChannelsBlacklistAttrTypes, radioAiChannelsBlacklistModel{
		Channel:      types.Int64PointerValue(element.Channel),
		ChannelWidth: types.Int64PointerValue(element.ChannelWidth),
		Radio:        types.StringValue(element.Radio),
	})
}

func radioAiRadiosConfigurationEncode(
	ctx context.Context, object types.Object,
) (settings.SettingRadioAiRadiosConfiguration, diag.Diagnostics) {
	var model radioAiRadiosConfigurationModel
	diags := object.As(ctx, &model, basetypes.ObjectAsOptions{})
	return settings.SettingRadioAiRadiosConfiguration{
		ChannelWidth: radioAiOmitZeroInt64(model.ChannelWidth),
		Dfs:          model.Dfs.ValueBool(),
		Radio:        model.Radio.ValueString(),
	}, diags
}

func radioAiRadiosConfigurationDecode(
	ctx context.Context, element settings.SettingRadioAiRadiosConfiguration,
) (types.Object, diag.Diagnostics) {
	return types.ObjectValueFrom(ctx, radioAiRadiosConfigurationAttrTypes, radioAiRadiosConfigurationModel{
		ChannelWidth: types.Int64PointerValue(element.ChannelWidth),
		Dfs:          types.BoolValue(element.Dfs),
		Radio:        types.StringValue(element.Radio),
	})
}

// radioAiAfterReceive is radio_ai's own CoManaged treatment: unlike every
// other section in this batch, radio_ai cannot use a plain
// unconditional-mirror hydration, because the controller is expected to
// actively rewrite channel and power assignments while AI optimization
// runs. Left unmasked, an unmanaged attribute would pick up the
// controller's own AI-invented value on the very next read and show a
// permanent diff against a config that never set it.
//
// The fix is the same plan-conditioned-null shape mgmt and usg already use
// for their own unconditional-mirror-shaped sections: every one of
// radio_ai's sixteen exposed attributes is null unless the practitioner's
// own config (prior) set it. For a CONFIGURED attribute this hook does
// nothing -- model already holds whatever the controller's read returned,
// and if the AI has since changed a configured value, the next plan
// correctly shows that as drift against config. That asymmetry is
// intentional, not a gap: the July design's own definition of CoManaged
// treats a configured field showing drift against the controller's
// AI-driven value as the expected behaviour, not something to suppress.
func radioAiAfterReceive(
	_ context.Context, _ *settings.RadioAi, model *settingRadioAiModel, prior settingRadioAiModel,
) diag.Diagnostics {
	boolOrNull := func(priorValue, modelValue types.Bool) types.Bool {
		if priorValue.IsNull() || priorValue.IsUnknown() {
			return types.BoolNull()
		}
		return modelValue
	}
	stringOrNull := func(priorValue, modelValue types.String) types.String {
		if priorValue.IsNull() || priorValue.IsUnknown() {
			return types.StringNull()
		}
		return modelValue
	}
	listOrNull := func(elemType attr.Type, priorValue, modelValue types.List) types.List {
		if priorValue.IsNull() || priorValue.IsUnknown() {
			return types.ListNull(elemType)
		}
		return modelValue
	}

	model.AutoAdjustChannelsToCountry = boolOrNull(prior.AutoAdjustChannelsToCountry, model.AutoAdjustChannelsToCountry)
	model.Enabled = boolOrNull(prior.Enabled, model.Enabled)

	model.AutoChannelPresetsType = stringOrNull(prior.AutoChannelPresetsType, model.AutoChannelPresetsType)
	model.CronExpr = stringOrNull(prior.CronExpr, model.CronExpr)
	model.SettingPreference = stringOrNull(prior.SettingPreference, model.SettingPreference)

	model.Channels6E = listOrNull(types.Int64Type, prior.Channels6E, model.Channels6E)
	model.ChannelsNa = listOrNull(types.Int64Type, prior.ChannelsNa, model.ChannelsNa)
	model.ChannelsNg = listOrNull(types.Int64Type, prior.ChannelsNg, model.ChannelsNg)
	model.HtModesNa = listOrNull(types.Int64Type, prior.HtModesNa, model.HtModesNa)
	model.HtModesNg = listOrNull(types.Int64Type, prior.HtModesNg, model.HtModesNg)

	model.ExcludeDevices = listOrNull(types.StringType, prior.ExcludeDevices, model.ExcludeDevices)
	model.HighPriorityDevices = listOrNull(types.StringType, prior.HighPriorityDevices, model.HighPriorityDevices)
	model.Optimize = listOrNull(types.StringType, prior.Optimize, model.Optimize)
	model.Radios = listOrNull(types.StringType, prior.Radios, model.Radios)

	channelsBlacklistType := types.ObjectType{AttrTypes: radioAiChannelsBlacklistAttrTypes}
	model.ChannelsBlacklist = listOrNull(channelsBlacklistType, prior.ChannelsBlacklist, model.ChannelsBlacklist)

	radiosConfigurationType := types.ObjectType{AttrTypes: radioAiRadiosConfigurationAttrTypes}
	model.RadiosConfiguration = listOrNull(radiosConfigurationType, prior.RadiosConfiguration, model.RadiosConfiguration)

	return nil
}

// radioAiKitBackend binds radioAiKitSpec to a client: Read is
// GetSetting[*RadioAi], UpdateFields is the masked UpdateSettingFields --
// naming only the fields the plan set.
func radioAiKitBackend(client *ui.ApiClient) resourcekit.Backend[settings.RadioAi] {
	return resourcekit.Backend[settings.RadioAi]{
		Read: func(ctx context.Context, site, _ string) (*settings.RadioAi, error) {
			_, radioAi, err := ui.GetSetting[*settings.RadioAi](client, ctx, site)
			if err != nil {
				return nil, err
			}
			return radioAi, nil
		},
		UpdateFields: func(
			ctx context.Context, site string, in *settings.RadioAi, fields ...string,
		) (*settings.RadioAi, error) {
			if err := client.UpdateSettingFields(ctx, site, in, fields...); err != nil {
				return nil, err
			}
			return in, nil
		},
	}
}

// radioAiKitSection builds the radio_ai entry for settingResource's
// Sections, bound to client via settingKitSections, which calls it with
// r.client.ApiClient.
func radioAiKitSection(client *ui.ApiClient) resourcekit.Section[settingResourceModel] {
	spec := radioAiKitSpec()
	spec.Backend = radioAiKitBackend(client)
	return resourcekit.SpecSection[settingResourceModel, settingRadioAiModel, settings.RadioAi]{
		SectionName:  "radio_ai",
		Get:          func(m *settingResourceModel) *types.Object { return &m.RadioAi },
		Set:          func(m *settingResourceModel, o types.Object) { m.RadioAi = o },
		AttrTypes:    radioAiAttrTypes,
		Spec:         spec,
		AfterReceive: radioAiAfterReceive,
	}
}
