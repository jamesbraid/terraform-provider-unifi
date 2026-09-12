package unifi

// The network_optimization section descriptor: an unconditional-mirror
// hydration with no specials, replacing the hand-written
// writeNetworkOptimizationSection / readNetworkOptimizationSection
// (setting_sections.go) and their networkOptimizationModelToSetting /
// networkOptimizationSettingToModel mappers (deleted from
// setting_resource.go). The model type and attribute-type map moved here
// too, from setting_resource.go: descriptor_mapping_test.go's
// loadDescriptors reads a descriptor's model tags from the same file the
// Spec literal is in. See setting_mgmt_descriptor.go for the shape every
// section descriptor follows.

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/types"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/resourcekit"
)

// networkOptimizationKitSpec maps every attribute of the generated
// network_optimization schema (resource_setting/setting_resource_gen.go's
// "network_optimization" SingleNestedAttribute) onto
// settings.NetworkOptimization. enabled is a plain bool, which carries no
// Elide claim at all.
func networkOptimizationKitSpec() resourcekit.Spec[settingNetworkOptimizationModel, settings.NetworkOptimization] {
	return resourcekit.Spec[settingNetworkOptimizationModel, settings.NetworkOptimization]{
		TypeName: "setting_network_optimization",
		Subject:  "Network Optimization Setting",
		New:      func() *settings.NetworkOptimization { return &settings.NetworkOptimization{} },
		Fields:   settingNetworkOptimizationGenFields(),
	}
}

// networkOptimizationKitBackend binds networkOptimizationKitSpec to a
// client: Read is GetSetting[*NetworkOptimization], UpdateFields is the
// masked UpdateSettingFields -- naming only the fields the plan set instead
// of the read-modify-write whole-document PUT
// writeNetworkOptimizationSection used.
func networkOptimizationKitBackend(client *ui.ApiClient) resourcekit.Backend[settings.NetworkOptimization] {
	return resourcekit.Backend[settings.NetworkOptimization]{
		Read: func(ctx context.Context, site, _ string) (*settings.NetworkOptimization, error) {
			_, netOpt, err := ui.GetSetting[*settings.NetworkOptimization](client, ctx, site)
			if err != nil {
				return nil, err
			}
			return netOpt, nil
		},
		UpdateFields: func(
			ctx context.Context, site string, in *settings.NetworkOptimization, fields ...string,
		) (*settings.NetworkOptimization, error) {
			if err := client.UpdateSettingFields(ctx, site, in, fields...); err != nil {
				return nil, err
			}
			return in, nil
		},
	}
}

// networkOptimizationKitSection builds the network_optimization entry for
// settingResource's Sections, bound to client via settingKitSections, which
// calls it with r.client.ApiClient.
func networkOptimizationKitSection(client *ui.ApiClient) resourcekit.Section[settingResourceModel] {
	spec := networkOptimizationKitSpec()
	spec.Backend = networkOptimizationKitBackend(client)
	return resourcekit.SpecSection[settingResourceModel, settingNetworkOptimizationModel, settings.NetworkOptimization]{
		SectionName: "network_optimization",
		Get:         func(m *settingResourceModel) *types.Object { return &m.NetworkOpt },
		Set:         func(m *settingResourceModel, o types.Object) { m.NetworkOpt = o },
		AttrTypes:   networkOptimizationAttrTypes,
		Spec:        spec,
	}
}
