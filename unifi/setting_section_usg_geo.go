package unifi

import (
	"context"
	"errors"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/objectplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
	"github.com/ubiquiti-community/terraform-provider-unifi/unifi/util"
)

// settingUsgGeoModel is the nested usg_geo block: gateway GeoIP filtering.
type settingUsgGeoModel struct {
	IPFiltering types.Object `tfsdk:"ip_filtering"`
}

type settingUsgGeoIPFilteringModel struct {
	Action           types.String `tfsdk:"action"`
	Countries        types.String `tfsdk:"countries"`
	Enabled          types.Bool   `tfsdk:"enabled"`
	TrafficDirection types.String `tfsdk:"traffic_direction"`
}

var (
	usgGeoIPFilteringAttrTypes = map[string]attr.Type{
		"action":            types.StringType,
		"countries":         types.StringType,
		"enabled":           types.BoolType,
		"traffic_direction": types.StringType,
	}
	usgGeoAttrTypes = map[string]attr.Type{
		"ip_filtering": types.ObjectType{AttrTypes: usgGeoIPFilteringAttrTypes},
	}
)

type usgGeoSection struct{}

func (usgGeoSection) key() string { return "usg_geo" }

func (usgGeoSection) attrTypes() map[string]attr.Type { return usgGeoAttrTypes }

func (usgGeoSection) schemaAttribute() schema.SingleNestedAttribute {
	return schema.SingleNestedAttribute{
		MarkdownDescription: "Gateway GeoIP filtering settings.",
		Optional:            true,
		Computed:            true,
		PlanModifiers: []planmodifier.Object{
			objectplanmodifier.UseStateForUnknown(),
		},
		Attributes: map[string]schema.Attribute{
			"ip_filtering": schema.SingleNestedAttribute{
				MarkdownDescription: "GeoIP-based traffic filtering.",
				Optional:            true,
				Computed:            true,
				PlanModifiers: []planmodifier.Object{
					objectplanmodifier.UseStateForUnknown(),
				},
				Attributes: map[string]schema.Attribute{
					"action": schema.StringAttribute{
						MarkdownDescription: "Whether the country list is blocked or allowed: `block` or `allow`.",
						Optional:            true,
						Computed:            true,
						Validators: []validator.String{
							stringvalidator.OneOf("block", "allow"),
						},
					},
					"countries": schema.StringAttribute{
						MarkdownDescription: "Comma-separated ISO 3166-1 alpha-2 country codes (e.g. `NZ,AU`).",
						Optional:            true,
						Computed:            true,
					},
					"enabled": schema.BoolAttribute{
						MarkdownDescription: "Enable GeoIP filtering.",
						Optional:            true,
						Computed:            true,
					},
					"traffic_direction": schema.StringAttribute{
						MarkdownDescription: "Filtered traffic direction: `both`, `ingress`, or `egress`.",
						Optional:            true,
						Computed:            true,
						Validators: []validator.String{
							stringvalidator.OneOf("both", "ingress", "egress"),
						},
					},
				},
			},
		},
	}
}

func (usgGeoSection) get(m *settingResourceModel) types.Object { return m.UsgGeo }

func (usgGeoSection) set(m *settingResourceModel, obj types.Object) { m.UsgGeo = obj }

func (usgGeoSection) overlay(
	ctx context.Context,
	obj types.Object,
	data map[string]any,
) diag.Diagnostics {
	var diags diag.Diagnostics
	var m settingUsgGeoModel
	diags.Append(obj.As(ctx, &m, basetypes.ObjectAsOptions{})...)
	if diags.HasError() {
		return diags
	}
	usgGeoModelToData(ctx, &m, data, &diags)
	return diags
}

// usgGeoModelToData writes only the user-set fields into the raw section
// document. The nested ip_filtering object merges into the existing nested
// map so unmodeled nested fields keep their remote values too.
func usgGeoModelToData(
	ctx context.Context,
	m *settingUsgGeoModel,
	data map[string]any,
	diags *diag.Diagnostics,
) {
	if m.IPFiltering.IsNull() || m.IPFiltering.IsUnknown() {
		return
	}
	var f settingUsgGeoIPFilteringModel
	diags.Append(m.IPFiltering.As(ctx, &f, basetypes.ObjectAsOptions{})...)
	if diags.HasError() {
		return
	}
	nested, _ := data["ip_filtering"].(map[string]any)
	if nested == nil {
		nested = map[string]any{}
	}
	if !f.Action.IsNull() && !f.Action.IsUnknown() {
		nested["action"] = f.Action.ValueString()
	}
	if !f.Countries.IsNull() && !f.Countries.IsUnknown() {
		nested["countries"] = f.Countries.ValueString()
	}
	if !f.Enabled.IsNull() && !f.Enabled.IsUnknown() {
		nested["enabled"] = f.Enabled.ValueBool()
	}
	if !f.TrafficDirection.IsNull() && !f.TrafficDirection.IsUnknown() {
		nested["traffic_direction"] = f.TrafficDirection.ValueString()
	}
	data["ip_filtering"] = nested
}

func (usgGeoSection) read(
	ctx context.Context,
	client *Client,
	site string,
) (types.Object, diag.Diagnostics) {
	var diags diag.Diagnostics
	_, setting, err := ui.GetSetting[*settings.UsgGeo](client.ApiClient, ctx, site)
	if err != nil {
		var notFound *ui.NotFoundError
		if errors.As(err, &notFound) {
			return types.ObjectNull(usgGeoAttrTypes), diags
		}
		diags.AddError("Error Reading USG Geo Setting", err.Error())
		return types.ObjectNull(usgGeoAttrTypes), diags
	}
	model := usgGeoSettingToModel(ctx, setting, &diags)
	if diags.HasError() {
		return types.ObjectNull(usgGeoAttrTypes), diags
	}
	return types.ObjectValueFrom(ctx, usgGeoAttrTypes, model)
}

func usgGeoSettingToModel(
	ctx context.Context,
	s *settings.UsgGeo,
	diags *diag.Diagnostics,
) settingUsgGeoModel {
	if s.IPFiltering == nil {
		return settingUsgGeoModel{IPFiltering: types.ObjectNull(usgGeoIPFilteringAttrTypes)}
	}
	obj, d := types.ObjectValueFrom(ctx, usgGeoIPFilteringAttrTypes, settingUsgGeoIPFilteringModel{
		Action:           util.StringValueOrNull(s.IPFiltering.Action),
		Countries:        util.StringValueOrNull(s.IPFiltering.Countries),
		Enabled:          types.BoolValue(s.IPFiltering.Enabled),
		TrafficDirection: util.StringValueOrNull(s.IPFiltering.TrafficDirection),
	})
	diags.Append(d...)
	return settingUsgGeoModel{IPFiltering: obj}
}
