package unifi

// The global_network section descriptor: an unconditional-mirror hydration
// with no specials, shaped exactly like setting_ipsec_descriptor.go.
//
// settings.GlobalNetwork is hand-maintained in the SDK rather than
// generated from the locked field spec (the controller exposes it at
// /api/s/<site>/{get,set}/setting/global_network on newer releases, ahead
// of the spec capture) -- see its own doc comment. It is still one of the
// settings GetSettingKey recognises, so it derives from the controller the
// same way every generated section does.

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/types"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/resourcekit"
)

// globalNetworkKitSpec maps the one attribute of the generated
// global_network schema (resource_setting/setting_resource_gen.go's
// "global_network" SingleNestedAttribute) onto settings.GlobalNetwork.
// default_security_posture is Optional+Computed with no validator rejecting
// an empty value -- the SDK records only one observed value, not a formal
// enum -- so resourcekit.ElideProblems' schema-driven rule demands
// KeepZero.
func globalNetworkKitSpec() resourcekit.Spec[settingGlobalNetworkModel, settings.GlobalNetwork] {
	return resourcekit.Spec[settingGlobalNetworkModel, settings.GlobalNetwork]{
		TypeName: "setting_global_network",
		Subject:  "Global Network Setting",
		New:      func() *settings.GlobalNetwork { return &settings.GlobalNetwork{} },
		Fields:   settingGlobalNetworkGenFields(),
	}
}

// globalNetworkKitBackend binds globalNetworkKitSpec to a client: Read is
// GetSetting[*GlobalNetwork], UpdateFields is the masked
// UpdateSettingFields -- naming only the fields the plan set.
func globalNetworkKitBackend(client *ui.ApiClient) resourcekit.Backend[settings.GlobalNetwork] {
	return resourcekit.Backend[settings.GlobalNetwork]{
		Read: func(ctx context.Context, site, _ string) (*settings.GlobalNetwork, error) {
			_, globalNetwork, err := ui.GetSetting[*settings.GlobalNetwork](client, ctx, site)
			if err != nil {
				return nil, err
			}
			return globalNetwork, nil
		},
		UpdateFields: func(
			ctx context.Context, site string, in *settings.GlobalNetwork, fields ...string,
		) (*settings.GlobalNetwork, error) {
			if err := client.UpdateSettingFields(ctx, site, in, fields...); err != nil {
				return nil, err
			}
			return in, nil
		},
	}
}

// globalNetworkKitSection builds the global_network entry for
// settingResource's Sections, bound to client via settingKitSections, which
// calls it with r.client.ApiClient.
func globalNetworkKitSection(client *ui.ApiClient) resourcekit.Section[settingResourceModel] {
	spec := globalNetworkKitSpec()
	spec.Backend = globalNetworkKitBackend(client)
	return resourcekit.SpecSection[settingResourceModel, settingGlobalNetworkModel, settings.GlobalNetwork]{
		SectionName: "global_network",
		Get:         func(m *settingResourceModel) *types.Object { return &m.GlobalNetwork },
		Set:         func(m *settingResourceModel, o types.Object) { m.GlobalNetwork = o },
		AttrTypes:   globalNetworkAttrTypes,
		Spec:        spec,
	}
}
