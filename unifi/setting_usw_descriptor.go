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

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
	resource_setting "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_setting"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/resourcekit"
)

// settingUswModel is usw's own section model, decoded out of
// settingResourceModel.Usw.
type settingUswModel struct {
	DHCPSnoop types.Bool `tfsdk:"dhcp_snoop"`
}

// uswAttrTypes types usw's own object in state; it must match the
// generated schema exactly.
var uswAttrTypes = map[string]attr.Type{
	"dhcp_snoop": types.BoolType,
}

// uswKitSpec maps the one attribute of the generated usw schema
// (resource_setting/setting_resource_gen.go's "usw" SingleNestedAttribute)
// onto settings.Usw. dhcp_snoop is a plain bool, which carries no Elide at
// all.
func uswKitSpec() resourcekit.Spec[settingUswModel, settings.Usw] {
	return resourcekit.Spec[settingUswModel, settings.Usw]{
		TypeName: "setting_usw",
		Subject:  "USW Setting",
		New:      func() *settings.Usw { return &settings.Usw{} },
		Fields: []resourcekit.Field[settingUswModel, settings.Usw]{
			resourcekit.BoolField[settingUswModel, settings.Usw]{
				Wire:  "dhcp_snoop",
				Model: func(m *settingUswModel) *types.Bool { return &m.DHCPSnoop },
				SDK:   func(s *settings.Usw) *bool { return &s.DHCPSnoop },
			},
		},
	}
}

// uswNestedSchema is the usw SingleNestedAttribute's own Attributes,
// wrapped as a schema.Schema so resourcekit's conformance checks -- built for
// a whole resource's top-level schema -- can run against one section of
// unifi_setting instead.
func uswNestedSchema(ctx context.Context) schema.Schema {
	built := resource_setting.SettingResourceSchema(ctx)
	usw := built.Attributes["usw"].(schema.SingleNestedAttribute) //nolint:forcetypeassert // usw is declared as SingleNestedAttribute in the generated schema; a mismatch here is a generator regression this is meant to catch loudly.
	return schema.Schema{Attributes: usw.Attributes}
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
