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

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
	resource_setting "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_setting"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/resourcekit"
)

// settingMagicSiteToSiteVpnModel is magic_site_to_site_vpn's own section
// model, decoded out of settingResourceModel.MagicSiteToSiteVpn.
type settingMagicSiteToSiteVpnModel struct {
	Enabled types.Bool `tfsdk:"enabled"`
}

// magicSiteToSiteVpnAttrTypes types magic_site_to_site_vpn's own object in
// state; it must match the generated schema exactly.
var magicSiteToSiteVpnAttrTypes = map[string]attr.Type{
	"enabled": types.BoolType,
}

// magicSiteToSiteVpnKitSpec maps the one attribute of the generated
// magic_site_to_site_vpn schema (resource_setting/setting_resource_gen.go's
// "magic_site_to_site_vpn" SingleNestedAttribute) onto
// settings.MagicSiteToSiteVpn.
func magicSiteToSiteVpnKitSpec() resourcekit.Spec[settingMagicSiteToSiteVpnModel, settings.MagicSiteToSiteVpn] {
	return resourcekit.Spec[settingMagicSiteToSiteVpnModel, settings.MagicSiteToSiteVpn]{
		TypeName: "setting_magic_site_to_site_vpn",
		Subject:  "Magic Site-to-Site VPN Setting",
		New:      func() *settings.MagicSiteToSiteVpn { return &settings.MagicSiteToSiteVpn{} },
		Fields: []resourcekit.Field[settingMagicSiteToSiteVpnModel, settings.MagicSiteToSiteVpn]{
			resourcekit.BoolField[settingMagicSiteToSiteVpnModel, settings.MagicSiteToSiteVpn]{
				Wire:  "enabled",
				Model: func(m *settingMagicSiteToSiteVpnModel) *types.Bool { return &m.Enabled },
				SDK:   func(s *settings.MagicSiteToSiteVpn) *bool { return &s.Enabled },
			},
		},
	}
}

// magicSiteToSiteVpnNestedSchema is the magic_site_to_site_vpn
// SingleNestedAttribute's own Attributes, wrapped as a schema.Schema so
// resourcekit's conformance checks -- built for a whole resource's
// top-level schema -- can run against one section of unifi_setting
// instead.
func magicSiteToSiteVpnNestedSchema(ctx context.Context) schema.Schema {
	built := resource_setting.SettingResourceSchema(ctx)
	magicSiteToSiteVpn := built.Attributes["magic_site_to_site_vpn"].(schema.SingleNestedAttribute) //nolint:forcetypeassert // magic_site_to_site_vpn is declared as SingleNestedAttribute in the generated schema; a mismatch here is a generator regression this is meant to catch loudly.
	return schema.Schema{Attributes: magicSiteToSiteVpn.Attributes}
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
