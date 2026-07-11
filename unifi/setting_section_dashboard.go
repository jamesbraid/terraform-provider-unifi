package unifi

import (
	"context"
	"errors"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/objectplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
	"github.com/ubiquiti-community/terraform-provider-unifi/unifi/util"
)

// settingDashboardModel is the nested dashboard block: cosmetic layout
// preferences for the controller UI dashboard.
type settingDashboardModel struct {
	LayoutPreference types.String `tfsdk:"layout_preference"`
	Widgets          types.List   `tfsdk:"widgets"`
}

type settingDashboardWidgetModel struct {
	Name    types.String `tfsdk:"name"`
	Enabled types.Bool   `tfsdk:"enabled"`
}

var (
	dashboardWidgetAttrTypes = map[string]attr.Type{
		"name":    types.StringType,
		"enabled": types.BoolType,
	}
	dashboardAttrTypes = map[string]attr.Type{
		"layout_preference": types.StringType,
		"widgets": types.ListType{
			ElemType: types.ObjectType{AttrTypes: dashboardWidgetAttrTypes},
		},
	}
)

type dashboardSection struct{}

func (dashboardSection) key() string { return "dashboard" }

func (dashboardSection) attrTypes() map[string]attr.Type { return dashboardAttrTypes }

func (dashboardSection) schemaAttribute() schema.SingleNestedAttribute {
	return schema.SingleNestedAttribute{
		MarkdownDescription: "Controller dashboard layout settings (cosmetic).",
		Optional:            true,
		Computed:            true,
		PlanModifiers: []planmodifier.Object{
			objectplanmodifier.UseStateForUnknown(),
		},
		Attributes: map[string]schema.Attribute{
			"layout_preference": schema.StringAttribute{
				MarkdownDescription: "Dashboard layout preference: `auto` or `manual`.",
				Optional:            true,
				Computed:            true,
				Validators: []validator.String{
					stringvalidator.OneOf("auto", "manual"),
				},
			},
			"widgets": schema.ListNestedAttribute{
				MarkdownDescription: "Ordered dashboard widget list (only meaningful with `layout_preference = \"manual\"`). Widget names vary by controller version and are not validated.",
				Optional:            true,
				Computed:            true,
				PlanModifiers: []planmodifier.List{
					listplanmodifier.UseStateForUnknown(),
				},
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"name": schema.StringAttribute{
							MarkdownDescription: "Widget identifier (e.g. `wan_activity`).",
							Required:            true,
						},
						"enabled": schema.BoolAttribute{
							MarkdownDescription: "Whether the widget is shown.",
							Required:            true,
						},
					},
				},
			},
		},
	}
}

func (dashboardSection) get(m *settingResourceModel) types.Object { return m.Dashboard }

func (dashboardSection) set(m *settingResourceModel, obj types.Object) { m.Dashboard = obj }

func (dashboardSection) overlay(
	ctx context.Context,
	obj types.Object,
	data map[string]any,
) diag.Diagnostics {
	var diags diag.Diagnostics
	var m settingDashboardModel
	diags.Append(obj.As(ctx, &m, basetypes.ObjectAsOptions{})...)
	if diags.HasError() {
		return diags
	}
	dashboardModelToData(ctx, &m, data, &diags)
	return diags
}

// dashboardModelToData writes only the user-set fields into the raw section
// document; unset fields keep their remote values.
func dashboardModelToData(
	ctx context.Context,
	m *settingDashboardModel,
	data map[string]any,
	diags *diag.Diagnostics,
) {
	if !m.LayoutPreference.IsNull() && !m.LayoutPreference.IsUnknown() {
		data["layout_preference"] = m.LayoutPreference.ValueString()
	}
	if !m.Widgets.IsNull() && !m.Widgets.IsUnknown() {
		var widgets []settingDashboardWidgetModel
		diags.Append(m.Widgets.ElementsAs(ctx, &widgets, false)...)
		out := make([]map[string]any, 0, len(widgets))
		for _, w := range widgets {
			out = append(out, map[string]any{
				"name":    w.Name.ValueString(),
				"enabled": w.Enabled.ValueBool(),
			})
		}
		data["widgets"] = out
	}
}

func (dashboardSection) read(
	ctx context.Context,
	client *Client,
	site string,
) (types.Object, diag.Diagnostics) {
	var diags diag.Diagnostics
	_, setting, err := ui.GetSetting[*settings.Dashboard](client.ApiClient, ctx, site)
	if err != nil {
		var notFound *ui.NotFoundError
		if errors.As(err, &notFound) {
			return types.ObjectNull(dashboardAttrTypes), diags
		}
		diags.AddError("Error Reading Dashboard Setting", err.Error())
		return types.ObjectNull(dashboardAttrTypes), diags
	}
	model := dashboardSettingToModel(ctx, setting, &diags)
	if diags.HasError() {
		return types.ObjectNull(dashboardAttrTypes), diags
	}
	return types.ObjectValueFrom(ctx, dashboardAttrTypes, model)
}

func dashboardSettingToModel(
	ctx context.Context,
	s *settings.Dashboard,
	diags *diag.Diagnostics,
) settingDashboardModel {
	widgetType := types.ObjectType{AttrTypes: dashboardWidgetAttrTypes}
	widgets := types.ListNull(widgetType)
	if len(s.Widgets) > 0 {
		models := make([]settingDashboardWidgetModel, 0, len(s.Widgets))
		for _, w := range s.Widgets {
			models = append(models, settingDashboardWidgetModel{
				Name:    util.StringValueOrNull(w.Name),
				Enabled: types.BoolValue(w.Enabled),
			})
		}
		l, d := types.ListValueFrom(ctx, widgetType, models)
		diags.Append(d...)
		widgets = l
	}
	return settingDashboardModel{
		LayoutPreference: util.StringValueOrNull(s.LayoutPreference),
		Widgets:          widgets,
	}
}
