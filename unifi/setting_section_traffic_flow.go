package unifi

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
)

// trafficFlowSection is the settingSection implementation for the
// "traffic_flow" settings section: four flat, always-present bool toggles
// for gateway traffic classes, no nested objects/lists and no secrets.
type trafficFlowSection struct{}

func init() {
	registerSection(trafficFlowSection{})
}

func (trafficFlowSection) key() string      { return "traffic_flow" }
func (trafficFlowSection) attrName() string { return "traffic_flow" }

func (trafficFlowSection) schemaAttribute() schema.Attribute {
	return schema.SingleNestedAttribute{
		MarkdownDescription: "Traffic flow settings.",
		Optional:            true,
		Computed:            true,
		Attributes: map[string]schema.Attribute{
			"enabled_allowed_traffic": schema.BoolAttribute{
				MarkdownDescription: "Whether the allowed-traffic list is enforced.",
				Optional:            true,
				Computed:            true,
				Default:             booldefault.StaticBool(false),
			},
			"gateway_dns_enabled": schema.BoolAttribute{
				MarkdownDescription: "Whether gateway DNS traffic flow tracking is enabled.",
				Optional:            true,
				Computed:            true,
				Default:             booldefault.StaticBool(false),
			},
			"unifi_device_management_enabled": schema.BoolAttribute{
				MarkdownDescription: "Whether UniFi device management traffic flow tracking is enabled.",
				Optional:            true,
				Computed:            true,
				Default:             booldefault.StaticBool(false),
			},
			"unifi_services_enabled": schema.BoolAttribute{
				MarkdownDescription: "Whether UniFi services traffic flow tracking is enabled.",
				Optional:            true,
				Computed:            true,
				Default:             booldefault.StaticBool(false),
			},
		},
	}
}

func (s trafficFlowSection) decode(ctx context.Context, snap rawSettings, prior settingResourceModel, model *settingResourceModel) diag.Diagnostics {
	var diags diag.Diagnostics

	var priorModel settingTrafficFlowModel
	if !prior.TrafficFlow.IsNull() && !prior.TrafficFlow.IsUnknown() {
		diags.Append(prior.TrafficFlow.As(ctx, &priorModel, basetypes.ObjectAsOptions{})...)
	}

	sec, _ := snap.section(s.key())
	data := sec.Data

	enabledAllowedTraffic, d := decodeBool(data, "enabled_allowed_traffic", priorModel.EnabledAllowedTraffic)
	diags.Append(d...)
	gatewayDNSEnabled, d := decodeBool(data, "gateway_dns_enabled", priorModel.GatewayDNSEnabled)
	diags.Append(d...)
	unifiDeviceManagementEnabled, d := decodeBool(data, "unifi_device_management_enabled", priorModel.UnifiDeviceManagementEnabled)
	diags.Append(d...)
	unifiServicesEnabled, d := decodeBool(data, "unifi_services_enabled", priorModel.UnifiServicesEnabled)
	diags.Append(d...)
	if diags.HasError() {
		return diags
	}

	m := settingTrafficFlowModel{
		EnabledAllowedTraffic:        enabledAllowedTraffic,
		GatewayDNSEnabled:            gatewayDNSEnabled,
		UnifiDeviceManagementEnabled: unifiDeviceManagementEnabled,
		UnifiServicesEnabled:         unifiServicesEnabled,
	}

	obj, objDiags := types.ObjectValueFrom(ctx, trafficFlowAttrTypes, m)
	diags.Append(objDiags...)
	if diags.HasError() {
		return diags
	}

	model.TrafficFlow = obj
	return diags
}

func (s trafficFlowSection) overlay(ctx context.Context, model, prior settingResourceModel, snap rawSettings) (settings.RawSetting, bool, diag.Diagnostics) {
	var diags diag.Diagnostics

	if model.TrafficFlow.IsNull() || model.TrafficFlow.IsUnknown() {
		return settings.RawSetting{}, false, diags
	}

	var m settingTrafficFlowModel
	diags.Append(model.TrafficFlow.As(ctx, &m, basetypes.ObjectAsOptions{})...)
	if diags.HasError() {
		return settings.RawSetting{}, false, diags
	}

	base := snap.dataCopy(s.key())
	overlayBool(base, "enabled_allowed_traffic", m.EnabledAllowedTraffic)
	overlayBool(base, "gateway_dns_enabled", m.GatewayDNSEnabled)
	overlayBool(base, "unifi_device_management_enabled", m.UnifiDeviceManagementEnabled)
	overlayBool(base, "unifi_services_enabled", m.UnifiServicesEnabled)

	rs := settings.RawSetting{
		BaseSetting: settings.BaseSetting{Key: s.key()},
		Data:        base,
	}
	return rs, true, diags
}

func (trafficFlowSection) carryBestEffort(dst *settingResourceModel, plan settingResourceModel) diag.Diagnostics {
	dst.TrafficFlow = plan.TrafficFlow
	return nil
}

func (trafficFlowSection) isConfigured(m settingResourceModel) bool {
	return !m.TrafficFlow.IsNull() && !m.TrafficFlow.IsUnknown()
}
