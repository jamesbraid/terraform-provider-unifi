package unifi

// The ipsec section descriptor: an unconditional-mirror hydration with no
// specials, shaped exactly like setting_country_descriptor.go. See
// setting_mgmt_descriptor.go for the shape every section descriptor
// follows.
//
// settings.Ipsec is hand-maintained in the SDK rather than generated from
// the locked field spec (the controller exposes it at
// /api/s/<site>/{get,set}/setting/ipsec on newer releases, ahead of the
// spec capture) -- see its own doc comment. It is still one of the
// settings GetSettingKey recognises, so it derives from the controller
// the same way every generated section does.

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/types"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/resourcekit"
)

// ipsecKitSpec maps the one attribute of the generated ipsec schema
// (resource_setting/setting_resource_gen.go's "ipsec" SingleNestedAttribute)
// onto settings.Ipsec. ikev2_reauthentication_method is Optional+Computed
// with no validator rejecting an empty value -- the SDK records only one
// observed value, not a formal enum -- so resourcekit.ElideProblems'
// schema-driven rule demands KeepZero.
func ipsecKitSpec() resourcekit.Spec[settingIpsecModel, settings.Ipsec] {
	return resourcekit.Spec[settingIpsecModel, settings.Ipsec]{
		TypeName: "setting_ipsec",
		Subject:  "IPsec Setting",
		New:      func() *settings.Ipsec { return &settings.Ipsec{} },
		Fields:   settingIpsecGenFields(),
	}
}

// ipsecKitBackend binds ipsecKitSpec to a client: Read is
// GetSetting[*Ipsec], UpdateFields is the masked UpdateSettingFields --
// naming only the fields the plan set.
func ipsecKitBackend(client *ui.ApiClient) resourcekit.Backend[settings.Ipsec] {
	return resourcekit.Backend[settings.Ipsec]{
		Read: func(ctx context.Context, site, _ string) (*settings.Ipsec, error) {
			_, ipsec, err := ui.GetSetting[*settings.Ipsec](client, ctx, site)
			if err != nil {
				return nil, err
			}
			return ipsec, nil
		},
		UpdateFields: func(
			ctx context.Context, site string, in *settings.Ipsec, fields ...string,
		) (*settings.Ipsec, error) {
			if err := client.UpdateSettingFields(ctx, site, in, fields...); err != nil {
				return nil, err
			}
			return in, nil
		},
	}
}

// ipsecKitSection builds the ipsec entry for settingResource's Sections,
// bound to client via settingKitSections, which calls it with
// r.client.ApiClient.
func ipsecKitSection(client *ui.ApiClient) resourcekit.Section[settingResourceModel] {
	spec := ipsecKitSpec()
	spec.Backend = ipsecKitBackend(client)
	return resourcekit.SpecSection[settingResourceModel, settingIpsecModel, settings.Ipsec]{
		SectionName: "ipsec",
		Get:         func(m *settingResourceModel) *types.Object { return &m.Ipsec },
		Set:         func(m *settingResourceModel, o types.Object) { m.Ipsec = o },
		AttrTypes:   ipsecAttrTypes,
		Spec:        spec,
	}
}
