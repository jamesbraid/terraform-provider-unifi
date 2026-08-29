package unifi

// The country section descriptor: an unconditional-mirror hydration with no
// specials, replacing the hand-written writeCountrySection /
// readCountrySection (setting_sections.go) and their countryModelToSetting /
// countrySettingToModel mappers (deleted from setting_resource.go). The
// model type and attribute-type map moved here too, from setting_resource.go:
// descriptor_mapping_test.go's loadDescriptors reads a descriptor's model
// tags from the same file the Spec literal is in. See
// setting_mgmt_descriptor.go for the shape every section descriptor follows.

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

// settingCountryModel is country's own section model, decoded out of
// settingResourceModel.Country.
type settingCountryModel struct {
	Code types.Int64 `tfsdk:"code"`
}

// countryAttrTypes types country's own object in state; it must match the
// generated schema exactly.
var countryAttrTypes = map[string]attr.Type{
	"code": types.Int64Type,
}

// countryKitSpec maps every attribute of the generated country schema
// (resource_setting/setting_resource_gen.go's "country" SingleNestedAttribute)
// onto settings.Country. code is Required, so resourcekit.ElideProblems'
// schema-driven rule demands KeepZero regardless of field kind -- config
// always supplies a Required attribute, so there is no zero-as-absence case
// to elide.
func countryKitSpec() resourcekit.Spec[settingCountryModel, settings.Country] {
	return resourcekit.Spec[settingCountryModel, settings.Country]{
		TypeName: "setting_country",
		Subject:  "Country Setting",
		New:      func() *settings.Country { return &settings.Country{} },
		Fields: []resourcekit.Field[settingCountryModel, settings.Country]{
			resourcekit.Int64PtrField[settingCountryModel, settings.Country]{
				Wire:  "code",
				Model: func(m *settingCountryModel) *types.Int64 { return &m.Code },
				SDK:   func(s *settings.Country) **int64 { return &s.Code },
				Elide: resourcekit.KeepZero,
			},
		},
	}
}

// countryNestedSchema is the country SingleNestedAttribute's own Attributes,
// wrapped as a schema.Schema so resourcekit's conformance checks -- built for
// a whole resource's top-level schema -- can run against one section of
// unifi_setting instead.
func countryNestedSchema(ctx context.Context) schema.Schema {
	built := resource_setting.SettingResourceSchema(ctx)
	country := built.Attributes["country"].(schema.SingleNestedAttribute) //nolint:forcetypeassert // country is declared as SingleNestedAttribute in the generated schema; a mismatch here is a generator regression this is meant to catch loudly.
	return schema.Schema{Attributes: country.Attributes}
}

// countryKitBackend binds countryKitSpec to a client: Read is
// GetSetting[*Country], UpdateFields is the masked UpdateSettingFields --
// naming only the fields the plan set instead of the read-modify-write
// whole-document PUT writeCountrySection used.
func countryKitBackend(client *ui.ApiClient) resourcekit.Backend[settings.Country] {
	return resourcekit.Backend[settings.Country]{
		Read: func(ctx context.Context, site, _ string) (*settings.Country, error) {
			_, country, err := ui.GetSetting[*settings.Country](client, ctx, site)
			if err != nil {
				return nil, err
			}
			return country, nil
		},
		UpdateFields: func(
			ctx context.Context, site string, in *settings.Country, fields ...string,
		) (*settings.Country, error) {
			if err := client.UpdateSettingFields(ctx, site, in, fields...); err != nil {
				return nil, err
			}
			return in, nil
		},
	}
}

// countryKitSection builds the country entry for settingResource's Sections,
// bound to client via settingKitSections, which calls it with
// r.client.ApiClient.
func countryKitSection(client *ui.ApiClient) resourcekit.Section[settingResourceModel] {
	spec := countryKitSpec()
	spec.Backend = countryKitBackend(client)
	return resourcekit.SpecSection[settingResourceModel, settingCountryModel, settings.Country]{
		SectionName: "country",
		Get:         func(m *settingResourceModel) *types.Object { return &m.Country },
		Set:         func(m *settingResourceModel, o types.Object) { m.Country = o },
		AttrTypes:   countryAttrTypes,
		Spec:        spec,
	}
}
