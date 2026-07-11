package unifi

import (
	"context"
	"errors"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/objectplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/setplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
	"github.com/ubiquiti-community/terraform-provider-unifi/unifi/util"
)

// settingRadioAiModel is the nested radio_ai block: the controller's
// automatic channel/power optimization. Co-managed by the controller — see
// the schema description's churn warning.
type settingRadioAiModel struct {
	Enabled                     types.Bool   `tfsdk:"enabled"`
	SettingPreference           types.String `tfsdk:"setting_preference"`
	AutoAdjustChannelsToCountry types.Bool   `tfsdk:"auto_adjust_channels_to_country"`
	AutoChannelPresetsType      types.String `tfsdk:"auto_channel_presets_type"`
	Channels6E                  types.Set    `tfsdk:"channels_6e"`
	ChannelsNa                  types.Set    `tfsdk:"channels_na"`
	ChannelsNg                  types.Set    `tfsdk:"channels_ng"`
	ChannelsBlacklist           types.Set    `tfsdk:"channels_blacklist"`
	CronExpr                    types.String `tfsdk:"cron_expr"`
	ExcludeDevices              types.Set    `tfsdk:"exclude_devices"`
	HighPriorityDevices         types.Set    `tfsdk:"high_priority_devices"`
	HtModesNa                   types.Set    `tfsdk:"ht_modes_na"`
	HtModesNg                   types.Set    `tfsdk:"ht_modes_ng"`
	Optimize                    types.Set    `tfsdk:"optimize"`
	Radios                      types.Set    `tfsdk:"radios"`
	RadiosConfiguration         types.Set    `tfsdk:"radios_configuration"`
}

type settingRadioAiChannelsBlacklistModel struct {
	Channel      types.Int64  `tfsdk:"channel"`
	ChannelWidth types.Int64  `tfsdk:"channel_width"`
	Radio        types.String `tfsdk:"radio"`
}

type settingRadioAiRadiosConfigurationModel struct {
	ChannelWidth types.Int64  `tfsdk:"channel_width"`
	Dfs          types.Bool   `tfsdk:"dfs"`
	Radio        types.String `tfsdk:"radio"`
}

var (
	radioAiChannelsBlacklistAttrTypes = map[string]attr.Type{
		"channel":       types.Int64Type,
		"channel_width": types.Int64Type,
		"radio":         types.StringType,
	}
	radioAiRadiosConfigurationAttrTypes = map[string]attr.Type{
		"channel_width": types.Int64Type,
		"dfs":           types.BoolType,
		"radio":         types.StringType,
	}
	radioAiAttrTypes = map[string]attr.Type{
		"enabled":                         types.BoolType,
		"setting_preference":              types.StringType,
		"auto_adjust_channels_to_country": types.BoolType,
		"auto_channel_presets_type":       types.StringType,
		"channels_6e":                     types.SetType{ElemType: types.Int64Type},
		"channels_na":                     types.SetType{ElemType: types.Int64Type},
		"channels_ng":                     types.SetType{ElemType: types.Int64Type},
		"channels_blacklist": types.SetType{
			ElemType: types.ObjectType{AttrTypes: radioAiChannelsBlacklistAttrTypes},
		},
		"cron_expr":             types.StringType,
		"exclude_devices":       types.SetType{ElemType: types.StringType},
		"high_priority_devices": types.SetType{ElemType: types.StringType},
		"ht_modes_na":           types.SetType{ElemType: types.Int64Type},
		"ht_modes_ng":           types.SetType{ElemType: types.Int64Type},
		"optimize":              types.SetType{ElemType: types.StringType},
		"radios":                types.SetType{ElemType: types.StringType},
		"radios_configuration": types.SetType{
			ElemType: types.ObjectType{AttrTypes: radioAiRadiosConfigurationAttrTypes},
		},
	}
)

type radioAiSection struct{}

func (radioAiSection) key() string { return "radio_ai" }

func (radioAiSection) attrTypes() map[string]attr.Type { return radioAiAttrTypes }

func (radioAiSection) schemaAttribute() schema.SingleNestedAttribute {
	usfuBool := []planmodifier.Bool{boolplanmodifier.UseStateForUnknown()}
	usfuString := []planmodifier.String{stringplanmodifier.UseStateForUnknown()}
	usfuSet := []planmodifier.Set{setplanmodifier.UseStateForUnknown()}

	return schema.SingleNestedAttribute{
		MarkdownDescription: "Radio AI (automatic channel/power optimization). " +
			"**Co-managed by the controller:** while `setting_preference` is `auto` the controller " +
			"rewrites channel plans, radio configuration, and schedules on its own, so any attribute " +
			"you set here may drift and churn plans. Most users should manage only `enabled` and " +
			"`setting_preference`; set `setting_preference = \"manual\"` before pinning channels or " +
			"radio parameters. Unset attributes always follow the controller.",
		Optional: true,
		Computed: true,
		PlanModifiers: []planmodifier.Object{
			objectplanmodifier.UseStateForUnknown(),
		},
		Attributes: map[string]schema.Attribute{
			"enabled": schema.BoolAttribute{
				MarkdownDescription: "Enable Radio AI optimization.",
				Optional:            true,
				Computed:            true,
				PlanModifiers:       usfuBool,
			},
			"setting_preference": schema.StringAttribute{
				MarkdownDescription: "`auto` (controller manages the plan) or `manual`.",
				Optional:            true,
				Computed:            true,
				PlanModifiers:       usfuString,
				Validators: []validator.String{
					stringvalidator.OneOf("auto", "manual"),
				},
			},
			"auto_adjust_channels_to_country": schema.BoolAttribute{
				MarkdownDescription: "Restrict automatic channel selection to the site country's allowed channels.",
				Optional:            true,
				Computed:            true,
				PlanModifiers:       usfuBool,
			},
			"auto_channel_presets_type": schema.StringAttribute{
				MarkdownDescription: "Channel preset: `maximum_speed`, `conservative`, or `custom`.",
				Optional:            true,
				Computed:            true,
				PlanModifiers:       usfuString,
				Validators: []validator.String{
					stringvalidator.OneOf("maximum_speed", "conservative", "custom"),
				},
			},
			"channels_6e": schema.SetAttribute{
				MarkdownDescription: "Candidate 6 GHz channels.",
				Optional:            true,
				Computed:            true,
				ElementType:         types.Int64Type,
				PlanModifiers:       usfuSet,
			},
			"channels_na": schema.SetAttribute{
				MarkdownDescription: "Candidate 5 GHz channels.",
				Optional:            true,
				Computed:            true,
				ElementType:         types.Int64Type,
				PlanModifiers:       usfuSet,
			},
			"channels_ng": schema.SetAttribute{
				MarkdownDescription: "Candidate 2.4 GHz channels.",
				Optional:            true,
				Computed:            true,
				ElementType:         types.Int64Type,
				PlanModifiers:       usfuSet,
			},
			"channels_blacklist": schema.SetNestedAttribute{
				MarkdownDescription: "Channels excluded from optimization.",
				Optional:            true,
				Computed:            true,
				PlanModifiers:       usfuSet,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"channel": schema.Int64Attribute{
							MarkdownDescription: "Channel number.",
							Required:            true,
						},
						"channel_width": schema.Int64Attribute{
							MarkdownDescription: "Channel width in MHz (20/40/80/160/240/320).",
							Required:            true,
						},
						"radio": schema.StringAttribute{
							MarkdownDescription: "Radio band: `ng`, `na`, or `6e`.",
							Required:            true,
							Validators: []validator.String{
								stringvalidator.OneOf("ng", "na", "6e"),
							},
						},
					},
				},
			},
			"cron_expr": schema.StringAttribute{
				MarkdownDescription: "Cron schedule for optimization runs (e.g. `0 3 * * *`).",
				Optional:            true,
				Computed:            true,
				PlanModifiers:       usfuString,
			},
			"exclude_devices": schema.SetAttribute{
				MarkdownDescription: "AP MAC addresses excluded from optimization.",
				Optional:            true,
				Computed:            true,
				ElementType:         types.StringType,
				PlanModifiers:       usfuSet,
			},
			"high_priority_devices": schema.SetAttribute{
				MarkdownDescription: "AP MAC addresses prioritized during optimization.",
				Optional:            true,
				Computed:            true,
				ElementType:         types.StringType,
				PlanModifiers:       usfuSet,
			},
			"ht_modes_na": schema.SetAttribute{
				MarkdownDescription: "Allowed 5 GHz channel widths in MHz.",
				Optional:            true,
				Computed:            true,
				ElementType:         types.Int64Type,
				PlanModifiers:       usfuSet,
			},
			"ht_modes_ng": schema.SetAttribute{
				MarkdownDescription: "Allowed 2.4 GHz channel widths in MHz.",
				Optional:            true,
				Computed:            true,
				ElementType:         types.Int64Type,
				PlanModifiers:       usfuSet,
			},
			"optimize": schema.SetAttribute{
				MarkdownDescription: "What to optimize: `channel` and/or `power`.",
				Optional:            true,
				Computed:            true,
				ElementType:         types.StringType,
				PlanModifiers:       usfuSet,
			},
			"radios": schema.SetAttribute{
				MarkdownDescription: "Radio bands under optimization: `ng`, `na`, `6e`.",
				Optional:            true,
				Computed:            true,
				ElementType:         types.StringType,
				PlanModifiers:       usfuSet,
			},
			"radios_configuration": schema.SetNestedAttribute{
				MarkdownDescription: "Per-band optimization parameters.",
				Optional:            true,
				Computed:            true,
				PlanModifiers:       usfuSet,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"channel_width": schema.Int64Attribute{
							MarkdownDescription: "Target channel width in MHz.",
							Optional:            true,
							Computed:            true,
						},
						"dfs": schema.BoolAttribute{
							MarkdownDescription: "Allow DFS channels.",
							Required:            true,
						},
						"radio": schema.StringAttribute{
							MarkdownDescription: "Radio band: `ng`, `na`, or `6e`.",
							Required:            true,
							Validators: []validator.String{
								stringvalidator.OneOf("ng", "na", "6e"),
							},
						},
					},
				},
			},
		},
	}
}

func (radioAiSection) get(m *settingResourceModel) types.Object { return m.RadioAi }

func (radioAiSection) set(m *settingResourceModel, obj types.Object) { m.RadioAi = obj }

func (radioAiSection) overlay(
	ctx context.Context,
	obj types.Object,
	data map[string]any,
) diag.Diagnostics {
	var diags diag.Diagnostics
	var m settingRadioAiModel
	diags.Append(obj.As(ctx, &m, basetypes.ObjectAsOptions{})...)
	if diags.HasError() {
		return diags
	}
	radioAiModelToData(ctx, &m, data, &diags)
	return diags
}

// radioAiModelToData writes only the user-set fields into the raw section
// document; unset fields — including controller fields go-unifi does not
// model, like auto_enabled — keep their remote values.
func radioAiModelToData(
	ctx context.Context,
	m *settingRadioAiModel,
	data map[string]any,
	diags *diag.Diagnostics,
) {
	if !m.Enabled.IsNull() && !m.Enabled.IsUnknown() {
		data["enabled"] = m.Enabled.ValueBool()
	}
	if !m.SettingPreference.IsNull() && !m.SettingPreference.IsUnknown() {
		data["setting_preference"] = m.SettingPreference.ValueString()
	}
	if !m.AutoAdjustChannelsToCountry.IsNull() && !m.AutoAdjustChannelsToCountry.IsUnknown() {
		data["auto_adjust_channels_to_country"] = m.AutoAdjustChannelsToCountry.ValueBool()
	}
	if !m.AutoChannelPresetsType.IsNull() && !m.AutoChannelPresetsType.IsUnknown() {
		data["auto_channel_presets_type"] = m.AutoChannelPresetsType.ValueString()
	}
	if !m.CronExpr.IsNull() && !m.CronExpr.IsUnknown() {
		data["cron_expr"] = m.CronExpr.ValueString()
	}

	writeInt64Set := func(key string, v types.Set) {
		if v.IsNull() || v.IsUnknown() {
			return
		}
		var vals []int64
		diags.Append(v.ElementsAs(ctx, &vals, false)...)
		data[key] = vals
	}
	writeStringSet := func(key string, v types.Set) {
		if v.IsNull() || v.IsUnknown() {
			return
		}
		var vals []string
		diags.Append(v.ElementsAs(ctx, &vals, false)...)
		data[key] = vals
	}
	writeInt64Set("channels_6e", m.Channels6E)
	writeInt64Set("channels_na", m.ChannelsNa)
	writeInt64Set("channels_ng", m.ChannelsNg)
	writeInt64Set("ht_modes_na", m.HtModesNa)
	writeInt64Set("ht_modes_ng", m.HtModesNg)
	writeStringSet("exclude_devices", m.ExcludeDevices)
	writeStringSet("high_priority_devices", m.HighPriorityDevices)
	writeStringSet("optimize", m.Optimize)
	writeStringSet("radios", m.Radios)

	if !m.ChannelsBlacklist.IsNull() && !m.ChannelsBlacklist.IsUnknown() {
		var entries []settingRadioAiChannelsBlacklistModel
		diags.Append(m.ChannelsBlacklist.ElementsAs(ctx, &entries, false)...)
		out := make([]map[string]any, 0, len(entries))
		for _, e := range entries {
			out = append(out, map[string]any{
				"channel":       e.Channel.ValueInt64(),
				"channel_width": e.ChannelWidth.ValueInt64(),
				"radio":         e.Radio.ValueString(),
			})
		}
		data["channels_blacklist"] = out
	}
	if !m.RadiosConfiguration.IsNull() && !m.RadiosConfiguration.IsUnknown() {
		var entries []settingRadioAiRadiosConfigurationModel
		diags.Append(m.RadiosConfiguration.ElementsAs(ctx, &entries, false)...)
		out := make([]map[string]any, 0, len(entries))
		for _, e := range entries {
			entry := map[string]any{
				"dfs":   e.Dfs.ValueBool(),
				"radio": e.Radio.ValueString(),
			}
			if !e.ChannelWidth.IsNull() && !e.ChannelWidth.IsUnknown() {
				entry["channel_width"] = e.ChannelWidth.ValueInt64()
			}
			out = append(out, entry)
		}
		data["radios_configuration"] = out
	}
}

func (radioAiSection) read(
	ctx context.Context,
	client *Client,
	site string,
) (types.Object, diag.Diagnostics) {
	var diags diag.Diagnostics
	_, setting, err := ui.GetSetting[*settings.RadioAi](client.ApiClient, ctx, site)
	if err != nil {
		var notFound *ui.NotFoundError
		if errors.As(err, &notFound) {
			return types.ObjectNull(radioAiAttrTypes), diags
		}
		diags.AddError("Error Reading Radio AI Setting", err.Error())
		return types.ObjectNull(radioAiAttrTypes), diags
	}
	model := radioAiSettingToModel(ctx, setting, &diags)
	if diags.HasError() {
		return types.ObjectNull(radioAiAttrTypes), diags
	}
	return types.ObjectValueFrom(ctx, radioAiAttrTypes, model)
}

func radioAiSettingToModel(
	ctx context.Context,
	s *settings.RadioAi,
	diags *diag.Diagnostics,
) settingRadioAiModel {
	int64Set := func(vals []int64) types.Set {
		set, d := types.SetValueFrom(ctx, types.Int64Type, vals)
		diags.Append(d...)
		return set
	}
	stringSet := func(vals []string) types.Set {
		set, d := types.SetValueFrom(ctx, types.StringType, vals)
		diags.Append(d...)
		return set
	}

	blacklist := make([]settingRadioAiChannelsBlacklistModel, 0, len(s.ChannelsBlacklist))
	for _, e := range s.ChannelsBlacklist {
		blacklist = append(blacklist, settingRadioAiChannelsBlacklistModel{
			Channel:      types.Int64PointerValue(e.Channel),
			ChannelWidth: types.Int64PointerValue(e.ChannelWidth),
			Radio:        util.StringValueOrNull(e.Radio),
		})
	}
	blacklistSet, d := types.SetValueFrom(ctx,
		types.ObjectType{AttrTypes: radioAiChannelsBlacklistAttrTypes}, blacklist)
	diags.Append(d...)

	configs := make([]settingRadioAiRadiosConfigurationModel, 0, len(s.RadiosConfiguration))
	for _, e := range s.RadiosConfiguration {
		configs = append(configs, settingRadioAiRadiosConfigurationModel{
			ChannelWidth: types.Int64PointerValue(e.ChannelWidth),
			Dfs:          types.BoolValue(e.Dfs),
			Radio:        util.StringValueOrNull(e.Radio),
		})
	}
	configSet, d := types.SetValueFrom(ctx,
		types.ObjectType{AttrTypes: radioAiRadiosConfigurationAttrTypes}, configs)
	diags.Append(d...)

	return settingRadioAiModel{
		Enabled:                     types.BoolValue(s.Enabled),
		SettingPreference:           util.StringValueOrNull(s.SettingPreference),
		AutoAdjustChannelsToCountry: types.BoolValue(s.AutoAdjustChannelsToCountry),
		AutoChannelPresetsType:      util.StringValueOrNull(s.AutoChannelPresetsType),
		Channels6E:                  int64Set(s.Channels6E),
		ChannelsNa:                  int64Set(s.ChannelsNa),
		ChannelsNg:                  int64Set(s.ChannelsNg),
		ChannelsBlacklist:           blacklistSet,
		CronExpr:                    util.StringValueOrNull(s.CronExpr),
		ExcludeDevices:              stringSet(s.ExcludeDevices),
		HighPriorityDevices:         stringSet(s.HighPriorityDevices),
		HtModesNa:                   int64Set(s.HtModesNa),
		HtModesNg:                   int64Set(s.HtModesNg),
		Optimize:                    stringSet(s.Optimize),
		Radios:                      stringSet(s.Radios),
		RadiosConfiguration:         configSet,
	}
}
