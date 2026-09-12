package unifi

// The global_nat section descriptor: an unconditional-mirror hydration with
// no specials. See setting_mgmt_descriptor.go for the shape every section
// descriptor follows.

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/types"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/resourcekit"
)

// globalNatKitSpec maps every attribute of the generated global_nat schema
// (resource_setting/setting_resource_gen.go's "global_nat"
// SingleNestedAttribute) onto settings.GlobalNat. Elide judgments follow
// resourcekit.ElideProblems' schema-driven rule: excluded_network_ids is
// Optional+Computed with no validator rejecting an empty value, so
// KeepZero is what the check demands; mode carries a OneOf("auto",
// "custom", "off") validator that rejects "", so the check demands
// NullZero there instead.
func globalNatKitSpec() resourcekit.Spec[settingGlobalNatModel, settings.GlobalNat] {
	return resourcekit.Spec[settingGlobalNatModel, settings.GlobalNat]{
		TypeName: "setting_global_nat",
		Subject:  "Global NAT Setting",
		New:      func() *settings.GlobalNat { return &settings.GlobalNat{} },
		Fields:   settingGlobalNatGenFields(),
	}
}

// globalNatKitBackend binds globalNatKitSpec to a client: Read is
// GetSetting[*GlobalNat], UpdateFields is the masked UpdateSettingFields --
// naming only the fields the plan set.
func globalNatKitBackend(client *ui.ApiClient) resourcekit.Backend[settings.GlobalNat] {
	return resourcekit.Backend[settings.GlobalNat]{
		Read: func(ctx context.Context, site, _ string) (*settings.GlobalNat, error) {
			_, globalNat, err := ui.GetSetting[*settings.GlobalNat](client, ctx, site)
			if err != nil {
				return nil, err
			}
			return globalNat, nil
		},
		UpdateFields: func(
			ctx context.Context, site string, in *settings.GlobalNat, fields ...string,
		) (*settings.GlobalNat, error) {
			if err := client.UpdateSettingFields(ctx, site, in, fields...); err != nil {
				return nil, err
			}
			return in, nil
		},
	}
}

// globalNatKitSection builds the global_nat entry for settingResource's
// Sections, bound to client via settingKitSections, which calls it with
// r.client.ApiClient.
func globalNatKitSection(client *ui.ApiClient) resourcekit.Section[settingResourceModel] {
	spec := globalNatKitSpec()
	spec.Backend = globalNatKitBackend(client)
	return resourcekit.SpecSection[settingResourceModel, settingGlobalNatModel, settings.GlobalNat]{
		SectionName: "global_nat",
		Get:         func(m *settingResourceModel) *types.Object { return &m.GlobalNat },
		Set:         func(m *settingResourceModel, o types.Object) { m.GlobalNat = o },
		AttrTypes:   globalNatAttrTypes,
		Spec:        spec,
	}
}
