package unifi

// The magic_site_to_site_vpn section descriptor: an unconditional-mirror
// hydration with no specials, shaped exactly like setting_locale_descriptor.go.
//
// The dispatch brief for this section assumed settings.MagicSiteToSiteVpn
// carries a controller-generated secret field. It does not: the pinned
// go-unifi SDK's magic_site_to_site_vpn.generated.go declares exactly one
// field, enabled (bool), and nothing else -- confirmed by reading the
// generated struct directly, not by inference. There is therefore no
// generated-value preservation to model here beyond what every other
// section already gets for free: resourcekit's masked write only ever
// sends the fields the plan names, so a future SDK regeneration that adds
// a real field to this struct would surface as a new, unmapped member
// (caught by go generate's own unaccounted-field check) rather than being
// silently overwritten.
import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/types"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/resourcekit"
)

// magicSiteToSiteVpnKitSpec maps the one attribute of the generated
// magic_site_to_site_vpn schema (resource_setting/setting_resource_gen.go's
// "magic_site_to_site_vpn" SingleNestedAttribute) onto
// settings.MagicSiteToSiteVpn.
func magicSiteToSiteVpnKitSpec() resourcekit.Spec[settingMagicSiteToSiteVpnModel, settings.MagicSiteToSiteVpn] {
	return resourcekit.Spec[settingMagicSiteToSiteVpnModel, settings.MagicSiteToSiteVpn]{
		TypeName: "setting_magic_site_to_site_vpn",
		Subject:  "Magic Site-to-Site VPN Setting",
		New:      func() *settings.MagicSiteToSiteVpn { return &settings.MagicSiteToSiteVpn{} },
		Fields:   settingMagicSiteToSiteVpnGenFields(),
	}
}

// magicSiteToSiteVpnKitBackend binds magicSiteToSiteVpnKitSpec to a
// client: Read is GetSetting[*MagicSiteToSiteVpn], UpdateFields is the
// masked UpdateSettingFields -- naming only the fields the plan set.
func magicSiteToSiteVpnKitBackend(client *ui.ApiClient) resourcekit.Backend[settings.MagicSiteToSiteVpn] {
	return resourcekit.Backend[settings.MagicSiteToSiteVpn]{
		Read: func(ctx context.Context, site, _ string) (*settings.MagicSiteToSiteVpn, error) {
			_, magicSiteToSiteVpn, err := ui.GetSetting[*settings.MagicSiteToSiteVpn](client, ctx, site)
			if err != nil {
				return nil, err
			}
			return magicSiteToSiteVpn, nil
		},
		UpdateFields: func(
			ctx context.Context, site string, in *settings.MagicSiteToSiteVpn, fields ...string,
		) (*settings.MagicSiteToSiteVpn, error) {
			if err := client.UpdateSettingFields(ctx, site, in, fields...); err != nil {
				return nil, err
			}
			return in, nil
		},
	}
}

// magicSiteToSiteVpnKitSection builds the magic_site_to_site_vpn entry for
// settingResource's Sections, bound to client via settingKitSections,
// which calls it with r.client.ApiClient.
func magicSiteToSiteVpnKitSection(client *ui.ApiClient) resourcekit.Section[settingResourceModel] {
	spec := magicSiteToSiteVpnKitSpec()
	spec.Backend = magicSiteToSiteVpnKitBackend(client)
	return resourcekit.SpecSection[settingResourceModel, settingMagicSiteToSiteVpnModel, settings.MagicSiteToSiteVpn]{
		SectionName: "magic_site_to_site_vpn",
		Get:         func(m *settingResourceModel) *types.Object { return &m.MagicSiteToSiteVpn },
		Set:         func(m *settingResourceModel, o types.Object) { m.MagicSiteToSiteVpn = o },
		AttrTypes:   magicSiteToSiteVpnAttrTypes,
		Spec:        spec,
	}
}
