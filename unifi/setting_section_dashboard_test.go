package unifi

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
)

const goldenDashboard = `{"key":"dashboard","layout_preference":"manual","widgets":[{"enabled":true,"name":"wifi_channels"},{"enabled":false,"name":"wan_activity"}]}`

func dashboardRepresentativeModel(ctx context.Context, t *testing.T) types.Object {
	t.Helper()
	widgets, diags := types.ListValueFrom(ctx, types.ObjectType{AttrTypes: dashboardWidgetAttrTypes}, []settingDashboardWidgetModel{
		{Enabled: types.BoolValue(true), Name: types.StringValue("wifi_channels")},
		{Enabled: types.BoolValue(false), Name: types.StringValue("wan_activity")},
	})
	if diags.HasError() {
		t.Fatalf("building widgets list: %v", diags)
	}
	m := settingDashboardModel{
		LayoutPreference: types.StringValue("manual"),
		Widgets:          widgets,
	}
	obj, diags2 := types.ObjectValueFrom(ctx, dashboardAttrTypes, m)
	if diags2.HasError() {
		t.Fatalf("building dashboard object: %v", diags2)
	}
	return obj
}

func TestDashboardSection_GoldenReproduction(t *testing.T) {
	ctx := context.Background()
	sec := dashboardSection{}

	model := settingResourceModel{Dashboard: dashboardRepresentativeModel(ctx, t)}
	prior := settingResourceModel{}
	snap := newRawSettings(nil)

	rs, configured, oDiags := sec.overlay(ctx, model, prior, snap)
	if oDiags.HasError() {
		t.Fatalf("overlay diagnostics: %v", oDiags)
	}
	if !configured {
		t.Fatalf("overlay configured = false, want true")
	}
	if rs.Key != "dashboard" {
		t.Errorf("rs.Key = %q, want %q", rs.Key, "dashboard")
	}

	assertPUTBodyMatchesGolden(t, rs, goldenDashboard)
}

func TestDashboardSection_DecodeRoundTrip(t *testing.T) {
	ctx := context.Background()
	sec := dashboardSection{}

	snap := newRawSettings([]settings.RawSetting{{
		BaseSetting: settings.BaseSetting{Key: "dashboard"},
		Data: map[string]any{
			"layout_preference": "manual",
			"widgets": []any{
				map[string]any{"enabled": true, "name": "wifi_channels"},
				map[string]any{"enabled": false, "name": "wan_activity"},
			},
		},
	}})

	prior := settingResourceModel{}
	model := settingResourceModel{}

	diags := sec.decode(ctx, snap, prior, &model)
	if diags.HasError() {
		t.Fatalf("decode diagnostics: %v", diags)
	}

	var got settingDashboardModel
	if diags := model.Dashboard.As(ctx, &got, basetypes.ObjectAsOptions{}); diags.HasError() {
		t.Fatalf("extracting settingDashboardModel: %v", diags)
	}

	if got.LayoutPreference.ValueString() != "manual" {
		t.Errorf("LayoutPreference = %q, want %q", got.LayoutPreference.ValueString(), "manual")
	}

	var widgets []settingDashboardWidgetModel
	if diags := got.Widgets.ElementsAs(ctx, &widgets, false); diags.HasError() {
		t.Fatalf("extracting Widgets: %v", diags)
	}
	if len(widgets) != 2 {
		t.Fatalf("len(Widgets) = %d, want 2", len(widgets))
	}
	if widgets[0].Name.ValueString() != "wifi_channels" || !widgets[0].Enabled.ValueBool() {
		t.Errorf("Widgets[0] = %+v, want {enabled:true name:wifi_channels}", widgets[0])
	}
	if widgets[1].Name.ValueString() != "wan_activity" || widgets[1].Enabled.ValueBool() {
		t.Errorf("Widgets[1] = %+v, want {enabled:false name:wan_activity}", widgets[1])
	}
}

func TestDashboardSection_Preservation(t *testing.T) {
	ctx := context.Background()
	sec := dashboardSection{}

	snap := newRawSettings([]settings.RawSetting{{
		BaseSetting: settings.BaseSetting{Key: "dashboard"},
		Data: map[string]any{
			"layout_preference": "auto",
			"x_unmanaged":       "keep",
		},
	}})

	model := settingResourceModel{Dashboard: dashboardRepresentativeModel(ctx, t)}
	prior := settingResourceModel{}

	rs, configured, oDiags := sec.overlay(ctx, model, prior, snap)
	if oDiags.HasError() {
		t.Fatalf("overlay diagnostics: %v", oDiags)
	}
	if !configured {
		t.Fatalf("overlay configured = false, want true")
	}

	if got, ok := rs.Data["x_unmanaged"]; !ok || got != "keep" {
		t.Errorf("rs.Data[%q] = %v (ok=%v), want %q", "x_unmanaged", got, ok, "keep")
	}
}

func TestDashboardSection_NotConfigured(t *testing.T) {
	ctx := context.Background()
	sec := dashboardSection{}

	model := settingResourceModel{Dashboard: types.ObjectNull(dashboardAttrTypes)}
	prior := settingResourceModel{}
	snap := newRawSettings(nil)

	rs, configured, oDiags := sec.overlay(ctx, model, prior, snap)
	if oDiags.HasError() {
		t.Fatalf("overlay diagnostics: %v", oDiags)
	}
	if configured {
		t.Fatalf("overlay configured = true, want false")
	}
	if rs.Key != "" || len(rs.Data) != 0 {
		t.Errorf("overlay returned non-zero RawSetting when not configured: %+v", rs)
	}
}

func TestDashboardSection_InterfaceWiring(t *testing.T) {
	sec := dashboardSection{}
	if sec.key() != "dashboard" {
		t.Errorf("key() = %q, want %q", sec.key(), "dashboard")
	}
	if sec.attrName() != "dashboard" {
		t.Errorf("attrName() = %q, want %q", sec.attrName(), "dashboard")
	}

	var found bool
	for _, s := range settingSections {
		if _, ok := s.(dashboardSection); ok {
			found = true
		}
	}
	if !found {
		t.Errorf("dashboardSection not found in settingSections registry")
	}
}
