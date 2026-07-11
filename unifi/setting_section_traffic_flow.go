package unifi

import (
	"context"
	"errors"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/objectplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
)

// settingTrafficFlowModel is the nested traffic_flow block: what the
// controller's Traffic Flows insights record. Naming follows the
// controller's JSON (no filipowm equivalent exists).
type settingTrafficFlowModel struct {
	EnabledAllowedTraffic        types.Bool `tfsdk:"enabled_allowed_traffic"`
	GatewayDNSEnabled            types.Bool `tfsdk:"gateway_dns_enabled"`
	UnifiDeviceManagementEnabled types.Bool `tfsdk:"unifi_device_management_enabled"`
	UnifiServicesEnabled         types.Bool `tfsdk:"unifi_services_enabled"`
}

var trafficFlowAttrTypes = map[string]attr.Type{
	"enabled_allowed_traffic":         types.BoolType,
	"gateway_dns_enabled":             types.BoolType,
	"unifi_device_management_enabled": types.BoolType,
	"unifi_services_enabled":          types.BoolType,
}

type trafficFlowSection struct{}

func (trafficFlowSection) key() string { return "traffic_flow" }

func (trafficFlowSection) attrTypes() map[string]attr.Type { return trafficFlowAttrTypes }

func (trafficFlowSection) schemaAttribute() schema.SingleNestedAttribute {
	return schema.SingleNestedAttribute{
		MarkdownDescription: "Traffic Flows recording settings: which flow " +
			"classes the controller records for insights.",
		Optional: true,
		Computed: true,
		PlanModifiers: []planmodifier.Object{
			objectplanmodifier.UseStateForUnknown(),
		},
		Attributes: map[string]schema.Attribute{
			"enabled_allowed_traffic": schema.BoolAttribute{
				MarkdownDescription: "Record allowed traffic flows (not just blocked ones).",
				Optional:            true,
				Computed:            true,
			},
			"gateway_dns_enabled": schema.BoolAttribute{
				MarkdownDescription: "Record gateway DNS queries.",
				Optional:            true,
				Computed:            true,
			},
			"unifi_device_management_enabled": schema.BoolAttribute{
				MarkdownDescription: "Record management traffic between UniFi devices.",
				Optional:            true,
				Computed:            true,
			},
			"unifi_services_enabled": schema.BoolAttribute{
				MarkdownDescription: "Record traffic to UniFi cloud services.",
				Optional:            true,
				Computed:            true,
			},
		},
	}
}

func (trafficFlowSection) get(m *settingResourceModel) types.Object { return m.TrafficFlow }

func (trafficFlowSection) set(m *settingResourceModel, obj types.Object) { m.TrafficFlow = obj }

func (trafficFlowSection) overlay(
	ctx context.Context,
	obj types.Object,
	data map[string]any,
) diag.Diagnostics {
	var diags diag.Diagnostics
	var m settingTrafficFlowModel
	diags.Append(obj.As(ctx, &m, basetypes.ObjectAsOptions{})...)
	if diags.HasError() {
		return diags
	}
	trafficFlowModelToData(&m, data)
	return diags
}

// trafficFlowModelToData writes only the user-set fields into the raw
// section document; unset fields keep their remote values.
func trafficFlowModelToData(m *settingTrafficFlowModel, data map[string]any) {
	if !m.EnabledAllowedTraffic.IsNull() && !m.EnabledAllowedTraffic.IsUnknown() {
		data["enabled_allowed_traffic"] = m.EnabledAllowedTraffic.ValueBool()
	}
	if !m.GatewayDNSEnabled.IsNull() && !m.GatewayDNSEnabled.IsUnknown() {
		data["gateway_dns_enabled"] = m.GatewayDNSEnabled.ValueBool()
	}
	if !m.UnifiDeviceManagementEnabled.IsNull() && !m.UnifiDeviceManagementEnabled.IsUnknown() {
		data["unifi_device_management_enabled"] = m.UnifiDeviceManagementEnabled.ValueBool()
	}
	if !m.UnifiServicesEnabled.IsNull() && !m.UnifiServicesEnabled.IsUnknown() {
		data["unifi_services_enabled"] = m.UnifiServicesEnabled.ValueBool()
	}
}

func (trafficFlowSection) read(
	ctx context.Context,
	client *Client,
	site string,
) (types.Object, diag.Diagnostics) {
	var diags diag.Diagnostics
	_, setting, err := ui.GetSetting[*settings.TrafficFlow](client.ApiClient, ctx, site)
	if err != nil {
		var notFound *ui.NotFoundError
		if errors.As(err, &notFound) {
			return types.ObjectNull(trafficFlowAttrTypes), diags
		}
		diags.AddError("Error Reading Traffic Flow Setting", err.Error())
		return types.ObjectNull(trafficFlowAttrTypes), diags
	}
	return types.ObjectValueFrom(ctx, trafficFlowAttrTypes, trafficFlowSettingToModel(setting))
}

func trafficFlowSettingToModel(s *settings.TrafficFlow) settingTrafficFlowModel {
	return settingTrafficFlowModel{
		EnabledAllowedTraffic:        types.BoolValue(s.EnabledAllowedTraffic),
		GatewayDNSEnabled:            types.BoolValue(s.GatewayDNSEnabled),
		UnifiDeviceManagementEnabled: types.BoolValue(s.UnifiDeviceManagementEnabled),
		UnifiServicesEnabled:         types.BoolValue(s.UnifiServicesEnabled),
	}
}
