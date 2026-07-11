package unifi

import (
	"context"
	"errors"
	"fmt"
	"regexp"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/objectplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/setplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
)

var etherLightingColorHexRegexp = regexp.MustCompile(`^[0-9A-Fa-f]{6}$`)

// settingEtherLightingModel is the nested ether_lighting block: the
// site-level Etherlighting palette used by switches with per-port LEDs
// (USW Pro Max line). Attribute names align with filipowm's
// unifi_setting_ether_lighting: network_id/speed + color_hex map to the raw
// key/raw_color_hex. The controller's built-in default palettes
// (network_defaults/speed_defaults) are controller-managed and preserved by
// the raw merge, not exposed.
type settingEtherLightingModel struct {
	NetworkOverrides types.Set `tfsdk:"network_overrides"`
	SpeedOverrides   types.Set `tfsdk:"speed_overrides"`
}

type settingEtherLightingNetworkOverrideModel struct {
	NetworkID types.String `tfsdk:"network_id"`
	ColorHex  types.String `tfsdk:"color_hex"`
}

type settingEtherLightingSpeedOverrideModel struct {
	Speed    types.String `tfsdk:"speed"`
	ColorHex types.String `tfsdk:"color_hex"`
}

var (
	etherLightingNetworkOverrideAttrTypes = map[string]attr.Type{
		"network_id": types.StringType,
		"color_hex":  types.StringType,
	}
	etherLightingSpeedOverrideAttrTypes = map[string]attr.Type{
		"speed":     types.StringType,
		"color_hex": types.StringType,
	}
	etherLightingAttrTypes = map[string]attr.Type{
		"network_overrides": types.SetType{
			ElemType: types.ObjectType{AttrTypes: etherLightingNetworkOverrideAttrTypes},
		},
		"speed_overrides": types.SetType{
			ElemType: types.ObjectType{AttrTypes: etherLightingSpeedOverrideAttrTypes},
		},
	}
)

type etherLightingSection struct{}

func (etherLightingSection) key() string { return "ether_lighting" }

func (etherLightingSection) attrTypes() map[string]attr.Type { return etherLightingAttrTypes }

func (etherLightingSection) schemaAttribute() schema.SingleNestedAttribute {
	return schema.SingleNestedAttribute{
		MarkdownDescription: "Etherlighting palette for switches with " +
			"per-port LEDs (USW Pro Max line). `network_overrides` colors " +
			"ports by network/VLAN, `speed_overrides` by link speed. " +
			"NOTE: the controller silently drops an override whose color " +
			"equals the built-in default for that key — declare only " +
			"colors that differ from the defaults or the entry will not " +
			"round-trip.",
		Optional: true,
		Computed: true,
		PlanModifiers: []planmodifier.Object{
			objectplanmodifier.UseStateForUnknown(),
		},
		Attributes: map[string]schema.Attribute{
			"network_overrides": schema.SetNestedAttribute{
				MarkdownDescription: "Per-network LED colors, used when a device's Etherlighting `mode` is `network`.",
				Optional:            true,
				Computed:            true,
				PlanModifiers: []planmodifier.Set{
					setplanmodifier.UseStateForUnknown(),
				},
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"network_id": schema.StringAttribute{
							MarkdownDescription: "ID of the network/VLAN this color applies to.",
							Required:            true,
						},
						"color_hex": schema.StringAttribute{
							MarkdownDescription: "LED color as a 6-digit RGB hex string without `#` (e.g. `ff6c14`).",
							Required:            true,
							Validators: []validator.String{
								stringvalidator.RegexMatches(
									etherLightingColorHexRegexp,
									"must be a 6-digit RGB hex string without '#'",
								),
							},
						},
					},
				},
			},
			"speed_overrides": schema.SetNestedAttribute{
				MarkdownDescription: "Per-link-speed LED colors, used when a device's Etherlighting `mode` is `speed`.",
				Optional:            true,
				Computed:            true,
				PlanModifiers: []planmodifier.Set{
					setplanmodifier.UseStateForUnknown(),
				},
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"speed": schema.StringAttribute{
							MarkdownDescription: "Link-speed class this color applies to.",
							Required:            true,
							Validators: []validator.String{
								stringvalidator.OneOf(
									"FE", "GbE", "2.5GbE", "5GbE",
									"10GbE", "25GbE", "40GbE", "100GbE",
								),
							},
						},
						"color_hex": schema.StringAttribute{
							MarkdownDescription: "LED color as a 6-digit RGB hex string without `#` (e.g. `ffc107`).",
							Required:            true,
							Validators: []validator.String{
								stringvalidator.RegexMatches(
									etherLightingColorHexRegexp,
									"must be a 6-digit RGB hex string without '#'",
								),
							},
						},
					},
				},
			},
		},
	}
}

func (etherLightingSection) get(m *settingResourceModel) types.Object { return m.EtherLighting }

func (etherLightingSection) set(m *settingResourceModel, obj types.Object) { m.EtherLighting = obj }

func (etherLightingSection) overlay(
	ctx context.Context,
	obj types.Object,
	data map[string]any,
) diag.Diagnostics {
	var diags diag.Diagnostics
	var m settingEtherLightingModel
	diags.Append(obj.As(ctx, &m, basetypes.ObjectAsOptions{})...)
	if diags.HasError() {
		return diags
	}
	etherLightingModelToData(ctx, &m, data, &diags)
	return diags
}

// etherLightingModelToData writes only the user-set fields into the raw
// section document; unset fields — including the controller's built-in
// default palettes (network_defaults, speed_defaults), which go-unifi does
// not model — keep their remote values. A set of objects only dedupes whole
// objects, so two entries sharing a key but differing in color both survive
// the plan; reject that here rather than sending conflicting colors.
func etherLightingModelToData(
	ctx context.Context,
	m *settingEtherLightingModel,
	data map[string]any,
	diags *diag.Diagnostics,
) {
	if !m.NetworkOverrides.IsNull() && !m.NetworkOverrides.IsUnknown() {
		var overrides []settingEtherLightingNetworkOverrideModel
		diags.Append(m.NetworkOverrides.ElementsAs(ctx, &overrides, false)...)
		if diags.HasError() {
			return
		}
		seen := make(map[string]struct{}, len(overrides))
		out := make([]map[string]any, 0, len(overrides))
		for _, o := range overrides {
			key := o.NetworkID.ValueString()
			if _, dup := seen[key]; dup {
				diags.AddError(
					"Duplicate network_overrides entry",
					fmt.Sprintf("network_id %q appears more than once in "+
						"ether_lighting.network_overrides; each network may "+
						"set only one color.", key),
				)
				return
			}
			seen[key] = struct{}{}
			out = append(out, map[string]any{
				"key":           key,
				"raw_color_hex": o.ColorHex.ValueString(),
			})
		}
		data["network_overrides"] = out
	}
	if !m.SpeedOverrides.IsNull() && !m.SpeedOverrides.IsUnknown() {
		var overrides []settingEtherLightingSpeedOverrideModel
		diags.Append(m.SpeedOverrides.ElementsAs(ctx, &overrides, false)...)
		if diags.HasError() {
			return
		}
		seen := make(map[string]struct{}, len(overrides))
		out := make([]map[string]any, 0, len(overrides))
		for _, o := range overrides {
			key := o.Speed.ValueString()
			if _, dup := seen[key]; dup {
				diags.AddError(
					"Duplicate speed_overrides entry",
					fmt.Sprintf("speed %q appears more than once in "+
						"ether_lighting.speed_overrides; each speed may set "+
						"only one color.", key),
				)
				return
			}
			seen[key] = struct{}{}
			out = append(out, map[string]any{
				"key":           key,
				"raw_color_hex": o.ColorHex.ValueString(),
			})
		}
		data["speed_overrides"] = out
	}
}

func (etherLightingSection) read(
	ctx context.Context,
	client *Client,
	site string,
) (types.Object, diag.Diagnostics) {
	var diags diag.Diagnostics
	_, setting, err := ui.GetSetting[*settings.EtherLighting](client.ApiClient, ctx, site)
	if err != nil {
		var notFound *ui.NotFoundError
		if errors.As(err, &notFound) {
			return types.ObjectNull(etherLightingAttrTypes), diags
		}
		diags.AddError("Error Reading Ether Lighting Setting", err.Error())
		return types.ObjectNull(etherLightingAttrTypes), diags
	}
	model := etherLightingSettingToModel(ctx, setting, &diags)
	if diags.HasError() {
		return types.ObjectNull(etherLightingAttrTypes), diags
	}
	return types.ObjectValueFrom(ctx, etherLightingAttrTypes, model)
}

func etherLightingSettingToModel(
	ctx context.Context,
	s *settings.EtherLighting,
	diags *diag.Diagnostics,
) settingEtherLightingModel {
	nets := make([]settingEtherLightingNetworkOverrideModel, 0, len(s.NetworkOverrides))
	for _, o := range s.NetworkOverrides {
		nets = append(nets, settingEtherLightingNetworkOverrideModel{
			NetworkID: types.StringValue(o.Key),
			ColorHex:  types.StringValue(o.RawColorHex),
		})
	}
	networkSet, d := types.SetValueFrom(ctx,
		types.ObjectType{AttrTypes: etherLightingNetworkOverrideAttrTypes}, nets)
	diags.Append(d...)

	speeds := make([]settingEtherLightingSpeedOverrideModel, 0, len(s.SpeedOverrides))
	for _, o := range s.SpeedOverrides {
		speeds = append(speeds, settingEtherLightingSpeedOverrideModel{
			Speed:    types.StringValue(o.Key),
			ColorHex: types.StringValue(o.RawColorHex),
		})
	}
	speedSet, d := types.SetValueFrom(ctx,
		types.ObjectType{AttrTypes: etherLightingSpeedOverrideAttrTypes}, speeds)
	diags.Append(d...)

	return settingEtherLightingModel{
		NetworkOverrides: networkSet,
		SpeedOverrides:   speedSet,
	}
}
