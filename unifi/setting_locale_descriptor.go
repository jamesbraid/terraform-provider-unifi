package unifi

// The locale section descriptor: an unconditional-mirror hydration with no
// specials, shaped exactly like setting_country_descriptor.go. See
// setting_mgmt_descriptor.go for the shape every section descriptor
// follows.

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

// settingLocaleModel is locale's own section model, decoded out of
// settingResourceModel.Locale.
type settingLocaleModel struct {
	Timezone types.String `tfsdk:"timezone"`
}

// localeAttrTypes types locale's own object in state; it must match the
// generated schema exactly.
var localeAttrTypes = map[string]attr.Type{
	"timezone": types.StringType,
}

// localeKitSpec maps the one attribute of the generated locale schema
// (resource_setting/setting_resource_gen.go's "locale" SingleNestedAttribute)
// onto settings.Locale. timezone is Optional+Computed with no validator
// rejecting an empty value, so resourcekit.ElideProblems' schema-driven rule
// demands KeepZero.
func localeKitSpec() resourcekit.Spec[settingLocaleModel, settings.Locale] {
	return resourcekit.Spec[settingLocaleModel, settings.Locale]{
		TypeName: "setting_locale",
		Subject:  "Locale Setting",
		New:      func() *settings.Locale { return &settings.Locale{} },
		Fields: []resourcekit.Field[settingLocaleModel, settings.Locale]{
			resourcekit.StringField[settingLocaleModel, settings.Locale]{
				Wire:  "timezone",
				Model: func(m *settingLocaleModel) *types.String { return &m.Timezone },
				SDK:   func(s *settings.Locale) *string { return &s.Timezone },
				Elide: resourcekit.KeepZero,
			},
		},
	}
}

// localeNestedSchema is the locale SingleNestedAttribute's own Attributes,
// wrapped as a schema.Schema so resourcekit's conformance checks -- built for
// a whole resource's top-level schema -- can run against one section of
// unifi_setting instead.
func localeNestedSchema(ctx context.Context) schema.Schema {
	built := resource_setting.SettingResourceSchema(ctx)
	locale := built.Attributes["locale"].(schema.SingleNestedAttribute) //nolint:forcetypeassert // locale is declared as SingleNestedAttribute in the generated schema; a mismatch here is a generator regression this is meant to catch loudly.
	return schema.Schema{Attributes: locale.Attributes}
}

// localeKitBackend binds localeKitSpec to a client: Read is
// GetSetting[*Locale], UpdateFields is the masked UpdateSettingFields --
// naming only the fields the plan set.
func localeKitBackend(client *ui.ApiClient) resourcekit.Backend[settings.Locale] {
	return resourcekit.Backend[settings.Locale]{
		Read: func(ctx context.Context, site, _ string) (*settings.Locale, error) {
			_, locale, err := ui.GetSetting[*settings.Locale](client, ctx, site)
			if err != nil {
				return nil, err
			}
			return locale, nil
		},
		UpdateFields: func(
			ctx context.Context, site string, in *settings.Locale, fields ...string,
		) (*settings.Locale, error) {
			if err := client.UpdateSettingFields(ctx, site, in, fields...); err != nil {
				return nil, err
			}
			return in, nil
		},
	}
}

// localeKitSection builds the locale entry for settingResource's Sections,
// bound to client via settingKitSections, which calls it with
// r.client.ApiClient.
func localeKitSection(client *ui.ApiClient) resourcekit.Section[settingResourceModel] {
	spec := localeKitSpec()
	spec.Backend = localeKitBackend(client)
	return resourcekit.SpecSection[settingResourceModel, settingLocaleModel, settings.Locale]{
		SectionName: "locale",
		Get:         func(m *settingResourceModel) *types.Object { return &m.Locale },
		Set:         func(m *settingResourceModel, o types.Object) { m.Locale = o },
		AttrTypes:   localeAttrTypes,
		Spec:        spec,
	}
}
