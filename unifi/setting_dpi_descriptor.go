package unifi

// The dpi section descriptor: an unconditional-mirror hydration with no
// specials, replacing the hand-written writeDpiSection / readDpiSection
// (setting_sections.go) and their dpiModelToSetting / dpiSettingToModel
// mappers (deleted from setting_resource.go). The model type and
// attribute-type map moved here too, from setting_resource.go:
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

// settingDpiModel is dpi's own section model, decoded out of
// settingResourceModel.Dpi.
type settingDpiModel struct {
	Enabled               types.Bool `tfsdk:"enabled"`
	FingerprintingEnabled types.Bool `tfsdk:"fingerprinting_enabled"`
}

// dpiAttrTypes types dpi's own object in state; it must match the generated
// schema exactly.
var dpiAttrTypes = map[string]attr.Type{
	"enabled":                types.BoolType,
	"fingerprinting_enabled": types.BoolType,
}

// dpiKitSpec maps every attribute of the generated dpi schema
// (resource_setting/setting_resource_gen.go's "dpi" SingleNestedAttribute)
// onto settings.Dpi. Both attributes are plain bools, which carry no Elide
// claim at all. settings.Dpi.FingerprintingEnabled is fingerprintingEnabled
// on the wire (camelCase, unlike its snake_case Terraform name), which is
// why Wire and the tfsdk tag differ here.
func dpiKitSpec() resourcekit.Spec[settingDpiModel, settings.Dpi] {
	return resourcekit.Spec[settingDpiModel, settings.Dpi]{
		TypeName: "setting_dpi",
		Subject:  "DPI Setting",
		New:      func() *settings.Dpi { return &settings.Dpi{} },
		Fields: []resourcekit.Field[settingDpiModel, settings.Dpi]{
			resourcekit.BoolField[settingDpiModel, settings.Dpi]{
				Wire:  "enabled",
				Model: func(m *settingDpiModel) *types.Bool { return &m.Enabled },
				SDK:   func(s *settings.Dpi) *bool { return &s.Enabled },
			},
			resourcekit.BoolField[settingDpiModel, settings.Dpi]{
				Wire:  "fingerprintingEnabled",
				Model: func(m *settingDpiModel) *types.Bool { return &m.FingerprintingEnabled },
				SDK:   func(s *settings.Dpi) *bool { return &s.FingerprintingEnabled },
			},
		},
	}
}

// dpiNestedSchema is the dpi SingleNestedAttribute's own Attributes, wrapped
// as a schema.Schema so resourcekit's conformance checks -- built for a whole
// resource's top-level schema -- can run against one section of
// unifi_setting instead.
func dpiNestedSchema(ctx context.Context) schema.Schema {
	built := resource_setting.SettingResourceSchema(ctx)
	dpi := built.Attributes["dpi"].(schema.SingleNestedAttribute) //nolint:forcetypeassert // dpi is declared as SingleNestedAttribute in the generated schema; a mismatch here is a generator regression this is meant to catch loudly.
	return schema.Schema{Attributes: dpi.Attributes}
}

// dpiKitBackend binds dpiKitSpec to a client: Read is GetSetting[*Dpi],
// UpdateFields is the masked UpdateSettingFields -- naming only the fields
// the plan set instead of the read-modify-write whole-document PUT
// writeDpiSection used.
func dpiKitBackend(client *ui.ApiClient) resourcekit.Backend[settings.Dpi] {
	return resourcekit.Backend[settings.Dpi]{
		Read: func(ctx context.Context, site, _ string) (*settings.Dpi, error) {
			_, dpi, err := ui.GetSetting[*settings.Dpi](client, ctx, site)
			if err != nil {
				return nil, err
			}
			return dpi, nil
		},
		UpdateFields: func(
			ctx context.Context, site string, in *settings.Dpi, fields ...string,
		) (*settings.Dpi, error) {
			if err := client.UpdateSettingFields(ctx, site, in, fields...); err != nil {
				return nil, err
			}
			return in, nil
		},
	}
}

// dpiKitSection builds the dpi entry for settingResource's Sections, bound to
// client the same way legacySectionsFor binds *settingResource.
func dpiKitSection(client *ui.ApiClient) resourcekit.Section[settingResourceModel] {
	spec := dpiKitSpec()
	spec.Backend = dpiKitBackend(client)
	return resourcekit.SpecSection[settingResourceModel, settingDpiModel, settings.Dpi]{
		SectionName: "dpi",
		Get:         func(m *settingResourceModel) *types.Object { return &m.Dpi },
		Set:         func(m *settingResourceModel, o types.Object) { m.Dpi = o },
		AttrTypes:   dpiAttrTypes,
		Spec:        spec,
	}
}
