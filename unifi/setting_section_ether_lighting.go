package unifi

import (
	"context"
	"regexp"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
)

// hexColorValidator matches go-unifi's own SettingEtherLighting*Overrides
// raw_color_hex doc-comment regex: exactly 6 hex digits, no leading "#".
var hexColorValidator = stringvalidator.RegexMatches(
	regexp.MustCompile(`^[0-9A-Fa-f]{6}$`),
	"must be a 6-digit hex color (e.g. \"00ff88\"), no leading \"#\"",
)

// etherLightingSection is the settingSection implementation for the
// "ether_lighting" settings section: two independent nested object lists
// (network_overrides, speed_overrides), each a flat {key, raw_color_hex}
// shape controlling per-network / per-link-speed switch port LED colors.
// "key" here is a wire field name coincidentally shared with the section's
// own key()/BaseSetting.Key routing token — the two are unrelated.
type etherLightingSection struct{}

func init() {
	registerSection(etherLightingSection{})
}

func (etherLightingSection) key() string      { return "ether_lighting" }
func (etherLightingSection) attrName() string { return "ether_lighting" }

func (etherLightingSection) schemaAttribute() schema.Attribute {
	return schema.SingleNestedAttribute{
		MarkdownDescription: "Ethernet port LED lighting overrides.",
		Optional:            true,
		Computed:            true,
		Attributes: map[string]schema.Attribute{
			"network_overrides": schema.ListNestedAttribute{
				MarkdownDescription: "Per-network port LED color overrides.",
				Optional:            true,
				Computed:            true,
				PlanModifiers: []planmodifier.List{
					listplanmodifier.UseStateForUnknown(),
				},
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"key": schema.StringAttribute{
							MarkdownDescription: "Network ID this override applies to.",
							Required:            true,
						},
						"raw_color_hex": schema.StringAttribute{
							MarkdownDescription: "LED color, as a 6-digit hex string (no leading \"#\").",
							Optional:            true,
							Computed:            true,
							Validators:          []validator.String{hexColorValidator},
						},
					},
				},
			},
			"speed_overrides": schema.ListNestedAttribute{
				MarkdownDescription: "Per-link-speed port LED color overrides.",
				Optional:            true,
				Computed:            true,
				PlanModifiers: []planmodifier.List{
					listplanmodifier.UseStateForUnknown(),
				},
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"key": schema.StringAttribute{
							MarkdownDescription: "Link speed this override applies to.",
							Required:            true,
							Validators: []validator.String{
								stringvalidator.OneOf("FE", "GbE", "2.5GbE", "5GbE", "10GbE", "25GbE", "40GbE", "100GbE"),
							},
						},
						"raw_color_hex": schema.StringAttribute{
							MarkdownDescription: "LED color, as a 6-digit hex string (no leading \"#\").",
							Optional:            true,
							Computed:            true,
							Validators:          []validator.String{hexColorValidator},
						},
					},
				},
			},
		},
	}
}

func (s etherLightingSection) decode(ctx context.Context, snap rawSettings, prior settingResourceModel, model *settingResourceModel) diag.Diagnostics {
	var diags diag.Diagnostics

	var priorModel settingEtherLightingModel
	if !prior.EtherLighting.IsNull() && !prior.EtherLighting.IsUnknown() {
		diags.Append(prior.EtherLighting.As(ctx, &priorModel, basetypes.ObjectAsOptions{})...)
	}

	sec, _ := snap.section(s.key())
	data := sec.Data

	networkOverrides, d := decodeObjectList(ctx, data, "network_overrides", priorModel.NetworkOverrides, types.ObjectType{AttrTypes: etherLightingNetworkOverrideAttrTypes})
	diags.Append(d...)
	speedOverrides, d := decodeObjectList(ctx, data, "speed_overrides", priorModel.SpeedOverrides, types.ObjectType{AttrTypes: etherLightingSpeedOverrideAttrTypes})
	diags.Append(d...)
	if diags.HasError() {
		return diags
	}

	m := settingEtherLightingModel{
		NetworkOverrides: networkOverrides,
		SpeedOverrides:   speedOverrides,
	}

	obj, objDiags := types.ObjectValueFrom(ctx, etherLightingAttrTypes, m)
	diags.Append(objDiags...)
	if diags.HasError() {
		return diags
	}

	model.EtherLighting = obj
	return diags
}

func (s etherLightingSection) overlay(ctx context.Context, model, prior settingResourceModel, snap rawSettings) (settings.RawSetting, bool, diag.Diagnostics) {
	var diags diag.Diagnostics

	if model.EtherLighting.IsNull() || model.EtherLighting.IsUnknown() {
		return settings.RawSetting{}, false, diags
	}

	var m settingEtherLightingModel
	diags.Append(model.EtherLighting.As(ctx, &m, basetypes.ObjectAsOptions{})...)
	if diags.HasError() {
		return settings.RawSetting{}, false, diags
	}

	base := snap.dataCopy(s.key())
	diags.Append(overlayObjectList(ctx, base, "network_overrides", m.NetworkOverrides)...)
	diags.Append(overlayObjectList(ctx, base, "speed_overrides", m.SpeedOverrides)...)
	if diags.HasError() {
		return settings.RawSetting{}, false, diags
	}

	rs := settings.RawSetting{
		BaseSetting: settings.BaseSetting{Key: s.key()},
		Data:        base,
	}
	return rs, true, diags
}

func (etherLightingSection) carryBestEffort(dst *settingResourceModel, plan settingResourceModel) diag.Diagnostics {
	dst.EtherLighting = plan.EtherLighting
	return nil
}

func (etherLightingSection) isConfigured(m settingResourceModel) bool {
	return !m.EtherLighting.IsNull() && !m.EtherLighting.IsUnknown()
}
