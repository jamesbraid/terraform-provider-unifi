package unifi

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
)

const goldenTrafficFlow = `{"enabled_allowed_traffic":true,"gateway_dns_enabled":false,"key":"traffic_flow","unifi_device_management_enabled":true,"unifi_services_enabled":false}`

func TestTrafficFlowSection_GoldenReproduction(t *testing.T) {
	ctx := context.Background()
	sec := trafficFlowSection{}

	m := settingTrafficFlowModel{
		EnabledAllowedTraffic:        types.BoolValue(true),
		GatewayDNSEnabled:            types.BoolValue(false),
		UnifiDeviceManagementEnabled: types.BoolValue(true),
		UnifiServicesEnabled:         types.BoolValue(false),
	}
	obj, diags := types.ObjectValueFrom(ctx, trafficFlowAttrTypes, m)
	if diags.HasError() {
		t.Fatalf("building traffic_flow object: %v", diags)
	}

	model := settingResourceModel{TrafficFlow: obj}
	prior := settingResourceModel{}
	snap := newRawSettings(nil)

	rs, configured, oDiags := sec.overlay(ctx, model, prior, snap)
	if oDiags.HasError() {
		t.Fatalf("overlay diagnostics: %v", oDiags)
	}
	if !configured {
		t.Fatalf("overlay configured = false, want true")
	}
	if rs.Key != "traffic_flow" {
		t.Errorf("rs.Key = %q, want %q", rs.Key, "traffic_flow")
	}

	assertPUTBodyMatchesGolden(t, rs, goldenTrafficFlow)
}

func TestTrafficFlowSection_DecodeRoundTrip(t *testing.T) {
	ctx := context.Background()
	sec := trafficFlowSection{}

	snap := newRawSettings([]settings.RawSetting{{
		BaseSetting: settings.BaseSetting{Key: "traffic_flow"},
		Data: map[string]any{
			"enabled_allowed_traffic":         true,
			"gateway_dns_enabled":             false,
			"unifi_device_management_enabled": true,
			"unifi_services_enabled":          false,
		},
	}})

	prior := settingResourceModel{}
	model := settingResourceModel{}

	diags := sec.decode(ctx, snap, prior, &model)
	if diags.HasError() {
		t.Fatalf("decode diagnostics: %v", diags)
	}

	var got settingTrafficFlowModel
	if diags := model.TrafficFlow.As(ctx, &got, basetypes.ObjectAsOptions{}); diags.HasError() {
		t.Fatalf("extracting settingTrafficFlowModel: %v", diags)
	}

	if !got.EnabledAllowedTraffic.ValueBool() {
		t.Errorf("EnabledAllowedTraffic = %v, want true", got.EnabledAllowedTraffic.ValueBool())
	}
	if got.GatewayDNSEnabled.ValueBool() {
		t.Errorf("GatewayDNSEnabled = %v, want false", got.GatewayDNSEnabled.ValueBool())
	}
	if !got.UnifiDeviceManagementEnabled.ValueBool() {
		t.Errorf("UnifiDeviceManagementEnabled = %v, want true", got.UnifiDeviceManagementEnabled.ValueBool())
	}
	if got.UnifiServicesEnabled.ValueBool() {
		t.Errorf("UnifiServicesEnabled = %v, want false", got.UnifiServicesEnabled.ValueBool())
	}
}

func TestTrafficFlowSection_Preservation(t *testing.T) {
	ctx := context.Background()
	sec := trafficFlowSection{}

	snap := newRawSettings([]settings.RawSetting{{
		BaseSetting: settings.BaseSetting{Key: "traffic_flow"},
		Data: map[string]any{
			"enabled_allowed_traffic": true,
			"x_unmanaged":             "keep",
		},
	}})

	m := settingTrafficFlowModel{
		EnabledAllowedTraffic:        types.BoolValue(true),
		GatewayDNSEnabled:            types.BoolValue(false),
		UnifiDeviceManagementEnabled: types.BoolValue(false),
		UnifiServicesEnabled:         types.BoolValue(false),
	}
	obj, diags := types.ObjectValueFrom(ctx, trafficFlowAttrTypes, m)
	if diags.HasError() {
		t.Fatalf("building traffic_flow object: %v", diags)
	}

	model := settingResourceModel{TrafficFlow: obj}
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

func TestTrafficFlowSection_NotConfigured(t *testing.T) {
	ctx := context.Background()
	sec := trafficFlowSection{}

	model := settingResourceModel{TrafficFlow: types.ObjectNull(trafficFlowAttrTypes)}
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

func TestTrafficFlowSection_InterfaceWiring(t *testing.T) {
	sec := trafficFlowSection{}
	if sec.key() != "traffic_flow" {
		t.Errorf("key() = %q, want %q", sec.key(), "traffic_flow")
	}
	if sec.attrName() != "traffic_flow" {
		t.Errorf("attrName() = %q, want %q", sec.attrName(), "traffic_flow")
	}

	var found bool
	for _, s := range settingSections {
		if _, ok := s.(trafficFlowSection); ok {
			found = true
		}
	}
	if !found {
		t.Errorf("trafficFlowSection not found in settingSections registry")
	}
}
