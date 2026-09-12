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

	"github.com/hashicorp/terraform-plugin-framework/types"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/resourcekit"
)

// trafficFlowKitSpec maps every attribute of the generated traffic_flow
// schema (resource_setting/setting_resource_gen.go's "traffic_flow"
// SingleNestedAttribute) onto settings.TrafficFlow.
func trafficFlowKitSpec() resourcekit.Spec[settingTrafficFlowModel, settings.TrafficFlow] {
	return resourcekit.Spec[settingTrafficFlowModel, settings.TrafficFlow]{
		TypeName: "setting_traffic_flow",
		Subject:  "Traffic Flow Setting",
		New:      func() *settings.TrafficFlow { return &settings.TrafficFlow{} },
		Fields:   settingTrafficFlowGenFields(),
	}
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
