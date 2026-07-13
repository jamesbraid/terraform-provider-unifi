package unifi

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/listvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/objectplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
	"github.com/ubiquiti-community/terraform-provider-unifi/unifi/validators"
)

// radioAiChannelsBlacklistChannelValidator validates channels_blacklist's
// "channel" leaf against SettingRadioAiChannelsBlacklist.Channel's struct
// comment (radio_ai.generated.go):
// "[1-9]|[1-9][0-9]|1[0-9][0-9]|2[0-9]|2[0-1][0-9]|22[0-1]|22[5-9]|233" —
// expanded, this is the near-contiguous integer set 1-221, 225-233 (227
// values total; excludes 222-224 and 234+). This spans all bands (2.4/5/6
// GHz combined) constrained per-row by the sibling "radio" field, not a
// short discrete enum like channel_width's five/six values, so it is
// implemented as two Between ranges via int64validator.Any rather than a
// 227-literal OneOf.
func radioAiChannelsBlacklistChannelValidator() validator.Int64 {
	return int64validator.Any(
		int64validator.Between(1, 221),
		int64validator.Between(225, 233),
	)
}

// radioAiChannelsNgValues is the closed set for SettingRadioAi.ChannelsNg
// per its struct comment (radio_ai.generated.go):
// "1|2|3|4|5|6|7|8|9|10|11|12|13|14" — the 2.4GHz channel numbers 1-14.
var radioAiChannelsNgValues = []int64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14}

// radioAiChannelsNaValues is the closed set for SettingRadioAi.ChannelsNa
// per its struct comment (radio_ai.generated.go): "34|36|38|40|42|44|46|48|
// 52|56|60|64|100|104|108|112|116|120|124|128|132|136|140|144|149|153|157|
// 161|165|169" — 30 discrete 5GHz channel numbers, transcribed verbatim (not
// a contiguous range, so a []int64 literal + OneOf is the right shape, not
// int64validator.Between).
var radioAiChannelsNaValues = []int64{
	34, 36, 38, 40, 42, 44, 46, 48, 52, 56, 60, 64, 100, 104, 108, 112, 116,
	120, 124, 128, 132, 136, 140, 144, 149, 153, 157, 161, 165, 169,
}

// radioAiChannels6EValidator validates channels_6e against
// SettingRadioAi.Channels6E's struct comment (radio_ai.generated.go):
// "[1-9]|[1-2][0-9]|3[3-9]|[4-5][0-9]|6[0-1]|6[5-9]|[7-8][0-9]|9[0-3]|9[7-9]|
// 1[0-1][0-9]|12[0-5]|129|1[3-4][0-9]|15[0-7]|16[1-9]|1[7-8][0-9]|19[3-9]|
// 2[0-1][0-9]|22[0-1]|22[5-9]|233" — expanded programmatically against the
// regex, this is the union of 9 contiguous ranges (1-29, 33-61, 65-93,
// 97-125, 129-157, 161-189, 193-221, 225-229) plus the single isolated value
// 233; it excludes 30-32, 62-64, 94-96, 126-128, 158-160, 190-192, 222-224,
// and 230-232/234+. Same shape as radioAiChannelsBlacklistChannelValidator:
// a wide, gapped 6GHz channel set, not a short discrete enum, so
// int64validator.Any + Between ranges is used rather than a ~200-literal
// OneOf.
func radioAiChannels6EValidator() validator.Int64 {
	return int64validator.Any(
		int64validator.Between(1, 29),
		int64validator.Between(33, 61),
		int64validator.Between(65, 93),
		int64validator.Between(97, 125),
		int64validator.Between(129, 157),
		int64validator.Between(161, 189),
		int64validator.Between(193, 221),
		int64validator.Between(225, 229),
		int64validator.Between(233, 233),
	)
}

// radioAiSection is the settingSection implementation for the "radio_ai"
// settings section: UniFi's AI-driven RF optimization feature. See
// settingRadioAiModel's doc comment (setting_resource.go) for the
// Managed/CoManaged field split and the setting_preference discriminator's
// deliberately soft (non-C4) treatment. "default"/"useXY" are never modeled
// — both survive every apply untouched via overlay()'s snap.dataCopy(key())
// RMW base, exactly like magic_site_to_site_vpn's hypothesized generated
// field.
type radioAiSection struct{}

func init() {
	registerSection(radioAiSection{})
}

func (radioAiSection) key() string      { return "radio_ai" }
func (radioAiSection) attrName() string { return "radio_ai" }

// schemaAttribute returns the "radio_ai" SingleNestedAttribute. The 7
// CoManaged-flavored attributes (channels_na, channels_ng, channels_6e,
// ht_modes_na, ht_modes_ng, radios_configuration, channels_blacklist) get
// UseStateForUnknown on their plan modifiers so an out-of-band controller
// rewrite under setting_preference=auto (hypothesized, unconfirmed) does
// not produce a spurious plan diff when the user never configured them.
// channels_na/channels_ng/channels_6e each DO have a closed allowed-value
// set per their struct comments (radio_ai.generated.go) and are validated
// accordingly: channels_ng (1-14) and channels_na (30 discrete 5GHz channel
// numbers) via int64validator.OneOf; channels_6e (a wide, gapped ~200-value
// 6GHz range) via the same Any+Between-ranges shape as
// radioAiChannelsBlacklistChannelValidator. "default"/"useXY" have no schema
// attribute at all.
func (radioAiSection) schemaAttribute() schema.Attribute {
	return schema.SingleNestedAttribute{
		MarkdownDescription: "AI-driven RF (radio) optimization settings. `setting_preference` is a " +
			"soft discriminator: unlike `mdns.mode`, it does not gate whether the channel/width " +
			"fields are meaningful — they remain real values in both `auto` and `manual`. The " +
			"channel/width-selection attributes below use `UseStateForUnknown` so the optimizer's " +
			"out-of-band rewrites (if `setting_preference = \"auto\"`) don't produce plan diffs when " +
			"left unconfigured; a configured value still re-asserts on every apply.",
		Optional: true,
		Computed: true,
		PlanModifiers: []planmodifier.Object{
			objectplanmodifier.UseStateForUnknown(),
		},
		Attributes: map[string]schema.Attribute{
			"enabled": schema.BoolAttribute{
				MarkdownDescription: "Whether AI radio optimization is enabled.",
				Optional:            true,
				Computed:            true,
			},
			"setting_preference": schema.StringAttribute{
				MarkdownDescription: "Optimization preference: `auto` (controller-driven) or `manual` " +
					"(user-pinned). Does not gate whether the channel/width fields below are " +
					"meaningful — no plan-time validator rejects configuring them under `auto`.",
				Optional: true,
				Computed: true,
				Validators: []validator.String{
					stringvalidator.OneOf("auto", "manual"),
				},
			},
			"auto_channel_presets_type": schema.StringAttribute{
				MarkdownDescription: "Automatic channel preset: `maximum_speed`, `conservative`, or `custom`.",
				Optional:            true,
				Computed:            true,
				Validators: []validator.String{
					stringvalidator.OneOf("maximum_speed", "conservative", "custom"),
				},
			},
			"auto_adjust_channels_to_country": schema.BoolAttribute{
				MarkdownDescription: "Whether to automatically adjust channels to the configured " +
					"regulatory country.",
				Optional: true,
				Computed: true,
			},
			"radios": schema.ListAttribute{
				MarkdownDescription: "Radio bands this section applies to.",
				ElementType:         types.StringType,
				Optional:            true,
				Computed:            true,
				PlanModifiers: []planmodifier.List{
					listplanmodifier.UseStateForUnknown(),
				},
				Validators: []validator.List{
					listvalidator.ValueStringsAre(stringvalidator.OneOf("na", "ng", "6e")),
				},
			},
			"optimize": schema.ListAttribute{
				MarkdownDescription: "What the optimizer adjusts: `channel`, `power`.",
				ElementType:         types.StringType,
				Optional:            true,
				Computed:            true,
				PlanModifiers: []planmodifier.List{
					listplanmodifier.UseStateForUnknown(),
				},
				Validators: []validator.List{
					listvalidator.ValueStringsAre(stringvalidator.OneOf("channel", "power")),
				},
			},
			"exclude_devices": schema.ListAttribute{
				MarkdownDescription: "MAC addresses of devices excluded from AI radio optimization.",
				ElementType:         types.StringType,
				Optional:            true,
				Computed:            true,
				PlanModifiers: []planmodifier.List{
					listplanmodifier.UseStateForUnknown(),
				},
				Validators: []validator.List{
					listvalidator.ValueStringsAre(validators.MACAddressValidator()),
				},
			},
			"high_priority_devices": schema.ListAttribute{
				MarkdownDescription: "MAC addresses of devices prioritized during AI radio optimization.",
				ElementType:         types.StringType,
				Optional:            true,
				Computed:            true,
				PlanModifiers: []planmodifier.List{
					listplanmodifier.UseStateForUnknown(),
				},
				Validators: []validator.List{
					listvalidator.ValueStringsAre(validators.MACAddressValidator()),
				},
			},
			"cron_expr": schema.StringAttribute{
				MarkdownDescription: "Cron expression controlling when the optimizer runs. Opaque; " +
					"no syntax validator (not worth one for this section, same as other cron_expr " +
					"leaves in this schema).",
				Optional: true,
				Computed: true,
			},
			// --- CoManaged-flavored: UseStateForUnknown, no plan-time
			// rejection of configuring them under setting_preference=auto.
			"channels_na": schema.ListAttribute{
				MarkdownDescription: "Pinned 5 GHz (na) channel list. Closed set: 34, 36, 38, 40, 42, " +
					"44, 46, 48, 52, 56, 60, 64, 100, 104, 108, 112, 116, 120, 124, 128, 132, 136, 140, " +
					"144, 149, 153, 157, 161, 165, 169.",
				ElementType: types.Int64Type,
				Optional:    true,
				Computed:    true,
				PlanModifiers: []planmodifier.List{
					listplanmodifier.UseStateForUnknown(),
				},
				Validators: []validator.List{
					listvalidator.ValueInt64sAre(int64validator.OneOf(radioAiChannelsNaValues...)),
				},
			},
			"channels_ng": schema.ListAttribute{
				MarkdownDescription: "Pinned 2.4 GHz (ng) channel list. Closed set: 1-14.",
				ElementType:         types.Int64Type,
				Optional:            true,
				Computed:            true,
				PlanModifiers: []planmodifier.List{
					listplanmodifier.UseStateForUnknown(),
				},
				Validators: []validator.List{
					listvalidator.ValueInt64sAre(int64validator.OneOf(radioAiChannelsNgValues...)),
				},
			},
			"channels_6e": schema.ListAttribute{
				MarkdownDescription: "Pinned 6 GHz (6e) channel list. A wide, gapped range spanning " +
					"1-221 with exclusions (1-29, 33-61, 65-93, 97-125, 129-157, 161-189, 193-221, " +
					"225-229, 233).",
				ElementType: types.Int64Type,
				Optional:    true,
				Computed:    true,
				PlanModifiers: []planmodifier.List{
					listplanmodifier.UseStateForUnknown(),
				},
				Validators: []validator.List{
					listvalidator.ValueInt64sAre(radioAiChannels6EValidator()),
				},
			},
			"ht_modes_na": schema.ListAttribute{
				MarkdownDescription: "Pinned 5 GHz (na) HT/channel-width list (MHz): 20, 40, 80, 160.",
				ElementType:         types.Int64Type,
				Optional:            true,
				Computed:            true,
				PlanModifiers: []planmodifier.List{
					listplanmodifier.UseStateForUnknown(),
				},
				Validators: []validator.List{
					listvalidator.ValueInt64sAre(int64validator.OneOf(20, 40, 80, 160)),
				},
			},
			"ht_modes_ng": schema.ListAttribute{
				MarkdownDescription: "Pinned 2.4 GHz (ng) HT/channel-width list (MHz): 20, 40.",
				ElementType:         types.Int64Type,
				Optional:            true,
				Computed:            true,
				PlanModifiers: []planmodifier.List{
					listplanmodifier.UseStateForUnknown(),
				},
				Validators: []validator.List{
					listvalidator.ValueInt64sAre(int64validator.OneOf(20, 40)),
				},
			},
			"radios_configuration": schema.ListNestedAttribute{
				MarkdownDescription: "Per-radio channel-width/DFS configuration.",
				Optional:            true,
				Computed:            true,
				PlanModifiers: []planmodifier.List{
					listplanmodifier.UseStateForUnknown(),
				},
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"radio": schema.StringAttribute{
							MarkdownDescription: "Radio band: `na`, `ng`, or `6e`.",
							Required:            true,
							Validators: []validator.String{
								stringvalidator.OneOf("na", "ng", "6e"),
							},
						},
						"channel_width": schema.Int64Attribute{
							MarkdownDescription: "Channel width (MHz): 20, 40, 80, 160, or 320. " +
								"NOTE: this enum has 5 values and does NOT include 240 — " +
								"channels_blacklist.channel_width is a different, 6-value enum " +
								"that does include 240; the two are not interchangeable.",
							Optional: true,
							Computed: true,
							PlanModifiers: []planmodifier.Int64{
								int64planmodifier.UseStateForUnknown(),
							},
							Validators: []validator.Int64{
								int64validator.OneOf(20, 40, 80, 160, 320),
							},
						},
						"dfs": schema.BoolAttribute{
							MarkdownDescription: "Whether DFS (Dynamic Frequency Selection) is enabled " +
								"for this radio.",
							Optional: true,
							Computed: true,
						},
					},
				},
			},
			"channels_blacklist": schema.ListNestedAttribute{
				MarkdownDescription: "Per-radio channel/width blacklist.",
				Optional:            true,
				Computed:            true,
				PlanModifiers: []planmodifier.List{
					listplanmodifier.UseStateForUnknown(),
				},
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"radio": schema.StringAttribute{
							MarkdownDescription: "Radio band: `na`, `ng`, or `6e`.",
							Required:            true,
							Validators: []validator.String{
								stringvalidator.OneOf("na", "ng", "6e"),
							},
						},
						"channel": schema.Int64Attribute{
							MarkdownDescription: "Channel number to blacklist (1-221, 225-233; a " +
								"controller-wide range spanning all bands, constrained per-row by " +
								"the sibling `radio` field rather than a short enum).",
							Optional: true,
							Computed: true,
							PlanModifiers: []planmodifier.Int64{
								int64planmodifier.UseStateForUnknown(),
							},
							Validators: []validator.Int64{
								radioAiChannelsBlacklistChannelValidator(),
							},
						},
						"channel_width": schema.Int64Attribute{
							MarkdownDescription: "Channel width (MHz): 20, 40, 80, 160, 240, or 320. " +
								"NOTE: this enum has 6 values and DOES include 240, unlike " +
								"radios_configuration.channel_width's 5-value enum.",
							Optional: true,
							Computed: true,
							PlanModifiers: []planmodifier.Int64{
								int64planmodifier.UseStateForUnknown(),
							},
							Validators: []validator.Int64{
								int64validator.OneOf(20, 40, 80, 160, 240, 320),
							},
						},
					},
				},
			},
		},
	}
}

// decode populates model.RadioAi from snap's "radio_ai" section data. Only
// the 16 modeled fields are read; "default"/"useXY" are never decoded
// because they are not in the model.
func (s radioAiSection) decode(ctx context.Context, snap rawSettings, prior settingResourceModel, model *settingResourceModel) diag.Diagnostics {
	var diags diag.Diagnostics

	var priorModel settingRadioAiModel
	if !prior.RadioAi.IsNull() && !prior.RadioAi.IsUnknown() {
		diags.Append(prior.RadioAi.As(ctx, &priorModel, basetypes.ObjectAsOptions{})...)
	}

	sec, _ := snap.section(s.key())
	data := sec.Data

	enabled, d := decodeBool(data, "enabled", priorModel.Enabled)
	diags.Append(d...)
	settingPreference, d := decodeString(data, "setting_preference", priorModel.SettingPreference)
	diags.Append(d...)
	autoChannelPresetsType, d := decodeString(data, "auto_channel_presets_type", priorModel.AutoChannelPresetsType)
	diags.Append(d...)
	autoAdjustChannelsToCountry, d := decodeBool(data, "auto_adjust_channels_to_country", priorModel.AutoAdjustChannelsToCountry)
	diags.Append(d...)
	radios, d := decodeStringList(ctx, data, "radios", priorModel.Radios)
	diags.Append(d...)
	optimize, d := decodeStringList(ctx, data, "optimize", priorModel.Optimize)
	diags.Append(d...)
	excludeDevices, d := decodeStringList(ctx, data, "exclude_devices", priorModel.ExcludeDevices)
	diags.Append(d...)
	highPriorityDevices, d := decodeStringList(ctx, data, "high_priority_devices", priorModel.HighPriorityDevices)
	diags.Append(d...)
	cronExpr, d := decodeString(data, "cron_expr", priorModel.CronExpr)
	diags.Append(d...)
	channelsNa, d := decodeInt64List(ctx, data, "channels_na", priorModel.ChannelsNa)
	diags.Append(d...)
	channelsNg, d := decodeInt64List(ctx, data, "channels_ng", priorModel.ChannelsNg)
	diags.Append(d...)
	channels6E, d := decodeInt64List(ctx, data, "channels_6e", priorModel.Channels6E)
	diags.Append(d...)
	htModesNa, d := decodeInt64List(ctx, data, "ht_modes_na", priorModel.HtModesNa)
	diags.Append(d...)
	htModesNg, d := decodeInt64List(ctx, data, "ht_modes_ng", priorModel.HtModesNg)
	diags.Append(d...)
	radiosConfiguration, d := decodeObjectList(ctx, data, "radios_configuration", priorModel.RadiosConfiguration, types.ObjectType{AttrTypes: radioAiRadiosConfigurationAttrTypes})
	diags.Append(d...)
	channelsBlacklist, d := decodeObjectList(ctx, data, "channels_blacklist", priorModel.ChannelsBlacklist, types.ObjectType{AttrTypes: radioAiChannelsBlacklistAttrTypes})
	diags.Append(d...)
	if diags.HasError() {
		return diags
	}

	m := settingRadioAiModel{
		Enabled:                     enabled,
		SettingPreference:           settingPreference,
		AutoChannelPresetsType:      autoChannelPresetsType,
		AutoAdjustChannelsToCountry: autoAdjustChannelsToCountry,
		Radios:                      radios,
		Optimize:                    optimize,
		ExcludeDevices:              excludeDevices,
		HighPriorityDevices:         highPriorityDevices,
		CronExpr:                    cronExpr,
		ChannelsNa:                  channelsNa,
		ChannelsNg:                  channelsNg,
		Channels6E:                  channels6E,
		HtModesNa:                   htModesNa,
		HtModesNg:                   htModesNg,
		RadiosConfiguration:         radiosConfiguration,
		ChannelsBlacklist:           channelsBlacklist,
	}

	obj, objDiags := types.ObjectValueFrom(ctx, radioAiAttrTypes, m)
	diags.Append(objDiags...)
	if diags.HasError() {
		return diags
	}

	model.RadioAi = obj
	return diags
}

// overlay computes the "radio_ai" PUT body from model.RadioAi, starting
// from a deep copy of the snapshot's current section data
// (snap.dataCopy(s.key())) so "default"/"useXY" (never modeled) and any
// other unmodeled field survive untouched. CoManaged-flavored fields use
// the identical overlay call shape as Managed fields — the CoManaged
// distinction is entirely in the SCHEMA plan modifier (UseStateForUnknown),
// not here: both classes write on a known cfg value and omit on cfg null,
// per C1's decision table. Returns configured == false (no write) when the
// section is not configured (null/unknown) in model.
func (s radioAiSection) overlay(ctx context.Context, model, prior settingResourceModel, snap rawSettings) (settings.RawSetting, bool, diag.Diagnostics) {
	var diags diag.Diagnostics

	if model.RadioAi.IsNull() || model.RadioAi.IsUnknown() {
		return settings.RawSetting{}, false, diags
	}

	var m settingRadioAiModel
	diags.Append(model.RadioAi.As(ctx, &m, basetypes.ObjectAsOptions{})...)
	if diags.HasError() {
		return settings.RawSetting{}, false, diags
	}

	base := snap.dataCopy(s.key()) // "default" and "useXY" survive here untouched — never modeled
	overlayBool(base, "enabled", m.Enabled)
	overlayString(base, "setting_preference", m.SettingPreference)
	overlayString(base, "auto_channel_presets_type", m.AutoChannelPresetsType)
	overlayBool(base, "auto_adjust_channels_to_country", m.AutoAdjustChannelsToCountry)
	diags.Append(overlayStringList(ctx, base, "radios", m.Radios)...)
	diags.Append(overlayStringList(ctx, base, "optimize", m.Optimize)...)
	diags.Append(overlayStringList(ctx, base, "exclude_devices", m.ExcludeDevices)...)
	diags.Append(overlayStringList(ctx, base, "high_priority_devices", m.HighPriorityDevices)...)
	overlayString(base, "cron_expr", m.CronExpr)
	// CoManaged fields: identical overlay call shape to Managed fields — the
	// CoManaged/Managed distinction is entirely in the SCHEMA plan modifier
	// (UseStateForUnknown), not in overlay, which always writes a known cfg
	// value regardless of class.
	diags.Append(overlayInt64List(ctx, base, "channels_na", m.ChannelsNa)...)
	diags.Append(overlayInt64List(ctx, base, "channels_ng", m.ChannelsNg)...)
	diags.Append(overlayInt64List(ctx, base, "channels_6e", m.Channels6E)...)
	diags.Append(overlayInt64List(ctx, base, "ht_modes_na", m.HtModesNa)...)
	diags.Append(overlayInt64List(ctx, base, "ht_modes_ng", m.HtModesNg)...)
	diags.Append(overlayObjectList(ctx, base, "radios_configuration", m.RadiosConfiguration)...)
	diags.Append(overlayObjectList(ctx, base, "channels_blacklist", m.ChannelsBlacklist)...)
	if diags.HasError() {
		return settings.RawSetting{}, false, diags
	}

	rs := settings.RawSetting{
		BaseSetting: settings.BaseSetting{Key: s.key()},
		Data:        base,
	}
	return rs, true, diags
}

// carryBestEffort copies the plan's radio_ai value onto dst. This section
// holds no secret leaves, so it is a straight copy with no per-leaf
// plan/prior choice needed.
func (radioAiSection) carryBestEffort(dst *settingResourceModel, plan settingResourceModel) diag.Diagnostics {
	dst.RadioAi = plan.RadioAi
	return nil
}

// isConfigured reports whether m.RadioAi is set (non-null, non-unknown),
// gating whether Create/Update push this section to the controller at all.
func (radioAiSection) isConfigured(m settingResourceModel) bool {
	return !m.RadioAi.IsNull() && !m.RadioAi.IsUnknown()
}
