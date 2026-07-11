package unifi

import (
	"context"
	"testing"

	fwresource "github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
)

func Test_trafficFlowModelToData(t *testing.T) {
	m := &settingTrafficFlowModel{
		EnabledAllowedTraffic:        types.BoolValue(true),
		GatewayDNSEnabled:            types.BoolValue(false),
		UnifiDeviceManagementEnabled: types.BoolNull(),
		UnifiServicesEnabled:         types.BoolValue(true),
	}
	// Pre-existing raw fields must be preserved, not clobbered.
	data := map[string]any{
		"unmodeled_field":                 "keep",
		"unifi_device_management_enabled": true,
	}

	trafficFlowModelToData(m, data)

	if data["enabled_allowed_traffic"] != true {
		t.Fatalf("enabled_allowed_traffic = %v", data["enabled_allowed_traffic"])
	}
	if data["gateway_dns_enabled"] != false {
		t.Fatalf("gateway_dns_enabled = %v", data["gateway_dns_enabled"])
	}
	if data["unifi_services_enabled"] != true {
		t.Fatalf("unifi_services_enabled = %v", data["unifi_services_enabled"])
	}
	if data["unifi_device_management_enabled"] != true {
		t.Fatal("null unifi_device_management_enabled overwrote remote value")
	}
	if data["unmodeled_field"] != "keep" {
		t.Fatal("unmodeled_field was clobbered")
	}
}

func Test_trafficFlowSettingToModel(t *testing.T) {
	m := trafficFlowSettingToModel(&settings.TrafficFlow{
		EnabledAllowedTraffic:        true,
		GatewayDNSEnabled:            true,
		UnifiDeviceManagementEnabled: false,
		UnifiServicesEnabled:         true,
	})
	if !m.EnabledAllowedTraffic.ValueBool() || !m.GatewayDNSEnabled.ValueBool() ||
		!m.UnifiServicesEnabled.ValueBool() {
		t.Fatalf("bools not mapped: %+v", m)
	}
	if m.UnifiDeviceManagementEnabled.ValueBool() {
		t.Fatalf("unifi_device_management_enabled = %v", m.UnifiDeviceManagementEnabled)
	}
}

func Test_settingResource_Schema_trafficFlow(t *testing.T) {
	r := &settingResource{}
	var resp fwresource.SchemaResponse
	r.Schema(context.Background(), fwresource.SchemaRequest{}, &resp)
	if _, ok := resp.Schema.Attributes["traffic_flow"]; !ok {
		t.Fatal("schema is missing the traffic_flow section attribute")
	}
}
