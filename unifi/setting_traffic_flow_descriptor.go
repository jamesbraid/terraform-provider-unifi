package unifi

// The traffic_flow section descriptor: an unconditional-mirror hydration
// with no specials, shaped exactly like setting_lcm_descriptor.go minus its
// int64 leaves. All four of settings.TrafficFlow's members are plain,
// non-omitempty bools -- the same force-emitted shape ips's
// content_filtering_blocking_page_enabled/honeypot_enabled/memory_optimized/
// restrict_torrents already have, so none of them carries an Elide (see
// resourcekit.BoolField's own comment: a false is a value, not an absence).

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
	resource_setting "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_setting"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/resourcekit"
)

// settingTrafficFlowModel is traffic_flow's own section model, decoded out
// of settingResourceModel.TrafficFlow.
type settingTrafficFlowModel struct {
	EnabledAllowedTraffic        types.Bool `tfsdk:"enabled_allowed_traffic"`
	GatewayDNSEnabled            types.Bool `tfsdk:"gateway_dns_enabled"`
	UnifiDeviceManagementEnabled types.Bool `tfsdk:"unifi_device_management_enabled"`
	UnifiServicesEnabled         types.Bool `tfsdk:"unifi_services_enabled"`
}

// trafficFlowAttrTypes types traffic_flow's own object in state; it must
// match the generated schema exactly.
var trafficFlowAttrTypes = map[string]attr.Type{
	"enabled_allowed_traffic":         types.BoolType,
	"gateway_dns_enabled":             types.BoolType,
	"unifi_device_management_enabled": types.BoolType,
	"unifi_services_enabled":          types.BoolType,
}

// trafficFlowKitSpec maps every attribute of the generated traffic_flow
// schema (resource_setting/setting_resource_gen.go's "traffic_flow"
// SingleNestedAttribute) onto settings.TrafficFlow.
func trafficFlowKitSpec() resourcekit.Spec[settingTrafficFlowModel, settings.TrafficFlow] {
	return resourcekit.Spec[settingTrafficFlowModel, settings.TrafficFlow]{
		TypeName: "setting_traffic_flow",
		Subject:  "Traffic Flow Setting",
		New:      func() *settings.TrafficFlow { return &settings.TrafficFlow{} },
		Fields: []resourcekit.Field[settingTrafficFlowModel, settings.TrafficFlow]{
			resourcekit.BoolField[settingTrafficFlowModel, settings.TrafficFlow]{
				Wire:  "enabled_allowed_traffic",
				Model: func(m *settingTrafficFlowModel) *types.Bool { return &m.EnabledAllowedTraffic },
				SDK:   func(s *settings.TrafficFlow) *bool { return &s.EnabledAllowedTraffic },
			},
			resourcekit.BoolField[settingTrafficFlowModel, settings.TrafficFlow]{
				Wire:  "gateway_dns_enabled",
				Model: func(m *settingTrafficFlowModel) *types.Bool { return &m.GatewayDNSEnabled },
				SDK:   func(s *settings.TrafficFlow) *bool { return &s.GatewayDNSEnabled },
			},
			resourcekit.BoolField[settingTrafficFlowModel, settings.TrafficFlow]{
				Wire:  "unifi_device_management_enabled",
				Model: func(m *settingTrafficFlowModel) *types.Bool { return &m.UnifiDeviceManagementEnabled },
				SDK:   func(s *settings.TrafficFlow) *bool { return &s.UnifiDeviceManagementEnabled },
			},
			resourcekit.BoolField[settingTrafficFlowModel, settings.TrafficFlow]{
				Wire:  "unifi_services_enabled",
				Model: func(m *settingTrafficFlowModel) *types.Bool { return &m.UnifiServicesEnabled },
				SDK:   func(s *settings.TrafficFlow) *bool { return &s.UnifiServicesEnabled },
			},
		},
	}
}

// trafficFlowNestedSchema is the traffic_flow SingleNestedAttribute's own
// Attributes, wrapped as a schema.Schema so resourcekit's conformance
// checks -- built for a whole resource's top-level schema -- can run
// against one section of unifi_setting instead.
func trafficFlowNestedSchema(ctx context.Context) schema.Schema {
	built := resource_setting.SettingResourceSchema(ctx)
	trafficFlow := built.Attributes["traffic_flow"].(schema.SingleNestedAttribute) //nolint:forcetypeassert // traffic_flow is declared as SingleNestedAttribute in the generated schema; a mismatch here is a generator regression this is meant to catch loudly.
	return schema.Schema{Attributes: trafficFlow.Attributes}
}

// trafficFlowKitBackend binds trafficFlowKitSpec to a client: Read is
// GetSetting[*TrafficFlow], UpdateFields is the masked
// UpdateSettingFields -- naming only the fields the plan set.
func trafficFlowKitBackend(client *ui.ApiClient) resourcekit.Backend[settings.TrafficFlow] {
	return resourcekit.Backend[settings.TrafficFlow]{
		Read: func(ctx context.Context, site, _ string) (*settings.TrafficFlow, error) {
			_, trafficFlow, err := ui.GetSetting[*settings.TrafficFlow](client, ctx, site)
			if err != nil {
				return nil, err
			}
			return trafficFlow, nil
		},
		UpdateFields: func(
			ctx context.Context, site string, in *settings.TrafficFlow, fields ...string,
		) (*settings.TrafficFlow, error) {
			if err := client.UpdateSettingFields(ctx, site, in, fields...); err != nil {
				return nil, err
			}
			return in, nil
		},
	}
}

// trafficFlowKitSection builds the traffic_flow entry for settingResource's
// Sections, bound to client via settingKitSections, which calls it with
// r.client.ApiClient.
func trafficFlowKitSection(client *ui.ApiClient) resourcekit.Section[settingResourceModel] {
	spec := trafficFlowKitSpec()
	spec.Backend = trafficFlowKitBackend(client)
	return resourcekit.SpecSection[settingResourceModel, settingTrafficFlowModel, settings.TrafficFlow]{
		SectionName: "traffic_flow",
		Get:         func(m *settingResourceModel) *types.Object { return &m.TrafficFlow },
		Set:         func(m *settingResourceModel, o types.Object) { m.TrafficFlow = o },
		AttrTypes:   trafficFlowAttrTypes,
		Spec:        spec,
	}
}
