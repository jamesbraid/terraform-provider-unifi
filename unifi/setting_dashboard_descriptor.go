package unifi

// The dashboard section descriptor: an unconditional-mirror hydration with
// no specials; widgets is dashboard's own ObjectListField, the same kind
// doh's custom_servers and mgmt's ssh_keys use. See
// setting_mgmt_descriptor.go for the shape every section descriptor
// follows, and setting_doh_descriptor.go for the ObjectListField pattern
// this one repeats.

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
	resource_setting "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_setting"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/resourcekit"
)

// settingDashboardWidgetModel is one element of dashboard's widgets list.
type settingDashboardWidgetModel struct {
	Enabled types.Bool   `tfsdk:"enabled"`
	Name    types.String `tfsdk:"name"`
}

// settingDashboardModel is dashboard's own section model, decoded out of
// settingResourceModel.Dashboard.
type settingDashboardModel struct {
	LayoutPreference types.String `tfsdk:"layout_preference"`
	Widgets          types.List   `tfsdk:"widgets"`
}

// dashboardWidgetAttrTypes and dashboardAttrTypes type dashboard's widgets
// elements and dashboard's own object in state; both must match the
// generated schema exactly.
var (
	dashboardWidgetAttrTypes = map[string]attr.Type{
		"enabled": types.BoolType,
		"name":    types.StringType,
	}
	dashboardAttrTypes = map[string]attr.Type{
		"layout_preference": types.StringType,
		"widgets": types.ListType{
			ElemType: types.ObjectType{AttrTypes: dashboardWidgetAttrTypes},
		},
	}
)

// dashboardKitSpec maps every attribute of the generated dashboard schema
// (resource_setting/setting_resource_gen.go's "dashboard"
// SingleNestedAttribute) onto settings.Dashboard. layout_preference carries
// a OneOf("auto", "manual") validator that rejects "", so
// resourcekit.ElideProblems' schema-driven rule demands NullZero; widgets
// is Optional+Computed with no zero-rejecting validator of its own kind, so
// it wants KeepZero, same as doh's custom_servers.
func dashboardKitSpec() resourcekit.Spec[settingDashboardModel, settings.Dashboard] {
	return resourcekit.Spec[settingDashboardModel, settings.Dashboard]{
		TypeName: "setting_dashboard",
		Subject:  "Dashboard Setting",
		New:      func() *settings.Dashboard { return &settings.Dashboard{} },
		Fields: []resourcekit.Field[settingDashboardModel, settings.Dashboard]{
			resourcekit.StringField[settingDashboardModel, settings.Dashboard]{
				Wire:  "layout_preference",
				Model: func(m *settingDashboardModel) *types.String { return &m.LayoutPreference },
				SDK:   func(s *settings.Dashboard) *string { return &s.LayoutPreference },
				Elide: resourcekit.NullZero,
			},
			resourcekit.ObjectListField[settingDashboardModel, settings.Dashboard, settings.SettingDashboardWidgets]{
				Wire:      "widgets",
				Model:     func(m *settingDashboardModel) *types.List { return &m.Widgets },
				SDK:       func(s *settings.Dashboard) *[]settings.SettingDashboardWidgets { return &s.Widgets },
				AttrTypes: dashboardWidgetAttrTypes,
				Encode:    dashboardWidgetEncode,
				Decode:    dashboardWidgetDecode,
				Elide:     resourcekit.KeepZero,
			},
		},
	}
}

func dashboardWidgetEncode(
	ctx context.Context, object types.Object,
) (settings.SettingDashboardWidgets, diag.Diagnostics) {
	var model settingDashboardWidgetModel
	diags := object.As(ctx, &model, basetypes.ObjectAsOptions{})
	return settings.SettingDashboardWidgets{
		Enabled: model.Enabled.ValueBool(),
		Name:    model.Name.ValueString(),
	}, diags
}

func dashboardWidgetDecode(
	ctx context.Context, element settings.SettingDashboardWidgets,
) (types.Object, diag.Diagnostics) {
	return types.ObjectValueFrom(ctx, dashboardWidgetAttrTypes, settingDashboardWidgetModel{
		Enabled: types.BoolValue(element.Enabled),
		Name:    types.StringValue(element.Name),
	})
}

// dashboardNestedSchema is the dashboard SingleNestedAttribute's own
// Attributes, wrapped as a schema.Schema so resourcekit's conformance
// checks -- built for a whole resource's top-level schema -- can run
// against one section of unifi_setting instead.
func dashboardNestedSchema(ctx context.Context) schema.Schema {
	built := resource_setting.SettingResourceSchema(ctx)
	dashboard := built.Attributes["dashboard"].(schema.SingleNestedAttribute) //nolint:forcetypeassert // dashboard is declared as SingleNestedAttribute in the generated schema; a mismatch here is a generator regression this is meant to catch loudly.
	return schema.Schema{Attributes: dashboard.Attributes}
}

// dashboardKitBackend binds dashboardKitSpec to a client: Read is
// GetSetting[*Dashboard], UpdateFields is the masked UpdateSettingFields --
// naming only the fields the plan set.
func dashboardKitBackend(client *ui.ApiClient) resourcekit.Backend[settings.Dashboard] {
	return resourcekit.Backend[settings.Dashboard]{
		Read: func(ctx context.Context, site, _ string) (*settings.Dashboard, error) {
			_, dashboard, err := ui.GetSetting[*settings.Dashboard](client, ctx, site)
			if err != nil {
				return nil, err
			}
			return dashboard, nil
		},
		UpdateFields: func(
			ctx context.Context, site string, in *settings.Dashboard, fields ...string,
		) (*settings.Dashboard, error) {
			if err := client.UpdateSettingFields(ctx, site, in, fields...); err != nil {
				return nil, err
			}
			return in, nil
		},
	}
}

// dashboardKitSection builds the dashboard entry for settingResource's
// Sections, bound to client via settingKitSections, which calls it with
// r.client.ApiClient.
func dashboardKitSection(client *ui.ApiClient) resourcekit.Section[settingResourceModel] {
	spec := dashboardKitSpec()
	spec.Backend = dashboardKitBackend(client)
	return resourcekit.SpecSection[settingResourceModel, settingDashboardModel, settings.Dashboard]{
		SectionName: "dashboard",
		Get:         func(m *settingResourceModel) *types.Object { return &m.Dashboard },
		Set:         func(m *settingResourceModel, o types.Object) { m.Dashboard = o },
		AttrTypes:   dashboardAttrTypes,
		Spec:        spec,
	}
}
