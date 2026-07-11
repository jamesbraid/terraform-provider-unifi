package unifi

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	fwresource "github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
)

func Test_dashboardModelToData(t *testing.T) {
	ctx := context.Background()
	var diags diag.Diagnostics

	widget, d := types.ObjectValueFrom(ctx, dashboardWidgetAttrTypes,
		settingDashboardWidgetModel{
			Name:    types.StringValue("wan_activity"),
			Enabled: types.BoolValue(true),
		})
	if d.HasError() {
		t.Fatal(d)
	}
	widgets, d := types.ListValueFrom(ctx,
		types.ObjectType{AttrTypes: dashboardWidgetAttrTypes},
		[]types.Object{widget})
	if d.HasError() {
		t.Fatal(d)
	}

	m := &settingDashboardModel{
		LayoutPreference: types.StringValue("manual"),
		Widgets:          widgets,
	}
	// Raw fields go-unifi does not model must round-trip untouched.
	data := map[string]any{"unmodeled_field": "keep", "layout_preference": "auto"}

	dashboardModelToData(ctx, m, data, &diags)
	if diags.HasError() {
		t.Fatal(diags)
	}

	if data["layout_preference"] != "manual" {
		t.Fatalf("layout_preference = %v", data["layout_preference"])
	}
	if data["unmodeled_field"] != "keep" {
		t.Fatal("unmodeled_field was clobbered")
	}
	entries, ok := data["widgets"].([]map[string]any)
	if !ok || len(entries) != 1 || entries[0]["name"] != "wan_activity" ||
		entries[0]["enabled"] != true {
		t.Fatalf("widgets = %v", data["widgets"])
	}
}

func Test_dashboardModelToData_nullsLeaveRemoteValues(t *testing.T) {
	ctx := context.Background()
	var diags diag.Diagnostics
	m := &settingDashboardModel{
		LayoutPreference: types.StringNull(),
		Widgets:          types.ListNull(types.ObjectType{AttrTypes: dashboardWidgetAttrTypes}),
	}
	data := map[string]any{"layout_preference": "auto"}

	dashboardModelToData(ctx, m, data, &diags)

	if data["layout_preference"] != "auto" {
		t.Fatalf("null layout_preference overwrote remote value: %v", data["layout_preference"])
	}
	if _, present := data["widgets"]; present {
		t.Fatal("null widgets should not be written")
	}
}

func Test_dashboardSettingToModel(t *testing.T) {
	ctx := context.Background()
	var diags diag.Diagnostics

	m := dashboardSettingToModel(ctx, &settings.Dashboard{
		LayoutPreference: "auto",
		Widgets: []settings.SettingDashboardWidgets{
			{Name: "wan_activity", Enabled: true},
		},
	}, &diags)
	if diags.HasError() {
		t.Fatal(diags)
	}
	if m.LayoutPreference.ValueString() != "auto" {
		t.Fatalf("layout_preference = %v", m.LayoutPreference)
	}
	var widgets []settingDashboardWidgetModel
	diags.Append(m.Widgets.ElementsAs(ctx, &widgets, false)...)
	if len(widgets) != 1 || widgets[0].Name.ValueString() != "wan_activity" ||
		!widgets[0].Enabled.ValueBool() {
		t.Fatalf("widgets = %v", widgets)
	}

	empty := dashboardSettingToModel(ctx, &settings.Dashboard{}, &diags)
	if !empty.LayoutPreference.IsNull() {
		t.Fatalf("empty layout_preference should be null, got %v", empty.LayoutPreference)
	}
	if !empty.Widgets.IsNull() {
		t.Fatalf("empty widgets should be null, got %v", empty.Widgets)
	}
}

func Test_settingResource_Schema_dashboard(t *testing.T) {
	r := &settingResource{}
	var resp fwresource.SchemaResponse
	r.Schema(context.Background(), fwresource.SchemaRequest{}, &resp)
	if _, ok := resp.Schema.Attributes["dashboard"]; !ok {
		t.Fatal("schema is missing the dashboard section attribute")
	}
}

func TestAccSettingResource_dashboard(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccSettingConfig_dashboard("auto"),
				Check: resource.TestCheckResourceAttr(
					"unifi_setting.test", "dashboard.layout_preference", "auto",
				),
			},
			{
				Config: testAccSettingConfig_dashboard("manual"),
				Check: resource.TestCheckResourceAttr(
					"unifi_setting.test", "dashboard.layout_preference", "manual",
				),
			},
		},
	})
}

func testAccSettingConfig_dashboard(pref string) string {
	return `
resource "unifi_setting" "test" {
  dashboard = {
    layout_preference = "` + pref + `"
  }
}
`
}
