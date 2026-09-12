package unifi

// The usw section descriptor: an unconditional-mirror hydration with no
// specials, shaped exactly like setting_locale_descriptor.go. See
// setting_mgmt_descriptor.go for the shape every section descriptor
// follows.
//
// The pinned go-unifi SDK's settings.Usw defines exactly one field of its
// own, dhcp_snoop; everything else on the document is BaseSetting. This is
// the site's legacy "usw" settings key, distinct from the newer
// global_switch document (which carries its own dhcp_snoop among thirteen
// other fields) -- the two are separate controller documents and the
// provider models each from its own definition.

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/types"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/resourcekit"
)

// uswKitSpec maps the one attribute of the generated usw schema
// (resource_setting/setting_resource_gen.go's "usw" SingleNestedAttribute)
// onto settings.Usw. dhcp_snoop is a plain bool, which carries no Elide at
// all.
func uswKitSpec() resourcekit.Spec[settingUswModel, settings.Usw] {
	return resourcekit.Spec[settingUswModel, settings.Usw]{
		TypeName: "setting_usw",
		Subject:  "USW Setting",
		New:      func() *settings.Usw { return &settings.Usw{} },
		Fields:   settingUswGenFields(),
	}
}

// uswKitBackend binds uswKitSpec to a client: Read is GetSetting[*Usw],
// UpdateFields is the masked UpdateSettingFields -- naming only the fields
// the plan set.
func uswKitBackend(client *ui.ApiClient) resourcekit.Backend[settings.Usw] {
	return resourcekit.Backend[settings.Usw]{
		Read: func(ctx context.Context, site, _ string) (*settings.Usw, error) {
			_, usw, err := ui.GetSetting[*settings.Usw](client, ctx, site)
			if err != nil {
				return nil, err
			}
			return usw, nil
		},
		UpdateFields: func(
			ctx context.Context, site string, in *settings.Usw, fields ...string,
		) (*settings.Usw, error) {
			if err := client.UpdateSettingFields(ctx, site, in, fields...); err != nil {
				return nil, err
			}
			return in, nil
		},
	}
}

// uswKitSection builds the usw entry for settingResource's Sections,
// bound to client via settingKitSections, which calls it with
// r.client.ApiClient.
func uswKitSection(client *ui.ApiClient) resourcekit.Section[settingResourceModel] {
	spec := uswKitSpec()
	spec.Backend = uswKitBackend(client)
	return resourcekit.SpecSection[settingResourceModel, settingUswModel, settings.Usw]{
		SectionName: "usw",
		Get:         func(m *settingResourceModel) *types.Object { return &m.Usw },
		Set:         func(m *settingResourceModel, o types.Object) { m.Usw = o },
		AttrTypes:   uswAttrTypes,
		Spec:        spec,
	}
}
