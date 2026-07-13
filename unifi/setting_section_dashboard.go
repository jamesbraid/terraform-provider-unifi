package unifi

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
)

// dashboardSection is the settingSection implementation for the
// "dashboard" settings section: a layout preference plus a list of
// per-widget enabled/name entries. Both layout_preference and widgets[].
// name are OneOf-validated against their respective closed doc-comment
// enums (2 values and 19 values respectively) — a new controller-side
// widget identifier requires a provider bump to add to the widgets[].name
// set, matching this tranche's default enum-validation policy.
type dashboardSection struct{}

func init() {
	registerSection(dashboardSection{})
}

func (dashboardSection) key() string      { return "dashboard" }
func (dashboardSection) attrName() string { return "dashboard" }

func (dashboardSection) schemaAttribute() schema.Attribute {
	return schema.SingleNestedAttribute{
		MarkdownDescription: "Dashboard layout and widget settings.",
		Optional:            true,
		Computed:            true,
		Attributes: map[string]schema.Attribute{
			"layout_preference": schema.StringAttribute{
				MarkdownDescription: "Dashboard layout preference.",
				Optional:            true,
				Computed:            true,
				Validators: []validator.String{
					stringvalidator.OneOf("auto", "manual"),
				},
			},
			"widgets": schema.ListNestedAttribute{
				MarkdownDescription: "Per-widget visibility overrides.",
				Optional:            true,
				Computed:            true,
				PlanModifiers: []planmodifier.List{
					listplanmodifier.UseStateForUnknown(),
				},
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"enabled": schema.BoolAttribute{
							MarkdownDescription: "Whether the widget is shown.",
							Optional:            true,
							Computed:            true,
							Default:             booldefault.StaticBool(false),
						},
						"name": schema.StringAttribute{
							MarkdownDescription: "Widget identifier.",
							Optional:            true,
							Computed:            true,
							Validators: []validator.String{
								stringvalidator.OneOf(
									"critical_traffic_prioritization",
									"cybersecure",
									"traffic_identification",
									"wifi_technology",
									"wifi_channels",
									"wifi_client_experience",
									"wifi_tx_retries",
									"most_active_apps_aps_clients",
									"most_active_apps_clients",
									"most_active_aps_clients",
									"most_active_apps_aps",
									"most_active_apps",
									"v2_most_active_aps",
									"v2_most_active_clients",
									"wifi_connectivity",
									"ap_radio_density",
									"wifi_channel_preset_configuration",
									"most_common_client_fingerprints",
									"wan_activity",
								),
							},
						},
					},
				},
			},
		},
	}
}

func (s dashboardSection) decode(ctx context.Context, snap rawSettings, prior settingResourceModel, model *settingResourceModel) diag.Diagnostics {
	var diags diag.Diagnostics

	var priorModel settingDashboardModel
	if !prior.Dashboard.IsNull() && !prior.Dashboard.IsUnknown() {
		diags.Append(prior.Dashboard.As(ctx, &priorModel, basetypes.ObjectAsOptions{})...)
	}

	sec, _ := snap.section(s.key())
	data := sec.Data

	layoutPreference, d := decodeString(data, "layout_preference", priorModel.LayoutPreference)
	diags.Append(d...)
	widgets, d := decodeObjectList(ctx, data, "widgets", priorModel.Widgets, types.ObjectType{AttrTypes: dashboardWidgetAttrTypes})
	diags.Append(d...)
	if diags.HasError() {
		return diags
	}

	m := settingDashboardModel{
		LayoutPreference: layoutPreference,
		Widgets:          widgets,
	}

	obj, objDiags := types.ObjectValueFrom(ctx, dashboardAttrTypes, m)
	diags.Append(objDiags...)
	if diags.HasError() {
		return diags
	}

	model.Dashboard = obj
	return diags
}

func (s dashboardSection) overlay(ctx context.Context, model, prior settingResourceModel, snap rawSettings) (settings.RawSetting, bool, diag.Diagnostics) {
	var diags diag.Diagnostics

	if model.Dashboard.IsNull() || model.Dashboard.IsUnknown() {
		return settings.RawSetting{}, false, diags
	}

	var m settingDashboardModel
	diags.Append(model.Dashboard.As(ctx, &m, basetypes.ObjectAsOptions{})...)
	if diags.HasError() {
		return settings.RawSetting{}, false, diags
	}

	base := snap.dataCopy(s.key())
	overlayString(base, "layout_preference", m.LayoutPreference)
	diags.Append(overlayObjectList(ctx, base, "widgets", m.Widgets)...)
	if diags.HasError() {
		return settings.RawSetting{}, false, diags
	}

	rs := settings.RawSetting{
		BaseSetting: settings.BaseSetting{Key: s.key()},
		Data:        base,
	}
	return rs, true, diags
}

func (dashboardSection) carryBestEffort(dst *settingResourceModel, plan settingResourceModel) diag.Diagnostics {
	dst.Dashboard = plan.Dashboard
	return nil
}

func (dashboardSection) isConfigured(m settingResourceModel) bool {
	return !m.Dashboard.IsNull() && !m.Dashboard.IsUnknown()
}
