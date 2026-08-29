package unifi

// The lcm section descriptor: an unconditional-mirror hydration whose only
// special is the #288 omit-not-zero guard on brightness/idle_timeout,
// replacing the hand-written writeLcmSection / readLcmSection
// (setting_sections.go) and their lcmModelToSetting / lcmSettingToModel
// mappers (deleted from setting_resource.go). The model type and
// attribute-type map moved here too, from setting_resource.go:
// descriptor_mapping_test.go's loadDescriptors reads a descriptor's model
// tags from the same file the Spec literal is in. See
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

// settingLcmModel is lcm's own section model, decoded out of
// settingResourceModel.Lcm.
type settingLcmModel struct {
	Brightness  types.Int64 `tfsdk:"brightness"`
	Enabled     types.Bool  `tfsdk:"enabled"`
	IdleTimeout types.Int64 `tfsdk:"idle_timeout"`
	Sync        types.Bool  `tfsdk:"sync"`
	TouchEvent  types.Bool  `tfsdk:"touch_event"`
}

// lcmAttrTypes types lcm's own object in state; it must match the generated
// schema exactly.
var lcmAttrTypes = map[string]attr.Type{
	"brightness":   types.Int64Type,
	"enabled":      types.BoolType,
	"idle_timeout": types.Int64Type,
	"sync":         types.BoolType,
	"touch_event":  types.BoolType,
}

// lcmKitSpec maps every attribute of the generated lcm schema
// (resource_setting/setting_resource_gen.go's "lcm" SingleNestedAttribute)
// onto settings.Lcm. Elide judgments follow resourcekit.ElideProblems'
// schema-driven rule: brightness and idle_timeout are Optional+Computed
// Int64 attributes, and ElideProblems' zeroIsRejected only ever inspects a
// StringAttribute's validators, so an Int64 range validator (1-100,
// 10-3600) can't drive it to NullZero the way ntp's setting_preference
// OneOf did -- KeepZero is what the check demands for both, matching the
// old mapper's own Int64PointerValue passthrough (nil stays null, a
// pointer to zero stays zero). OmitZero is the separate, write-side #288
// guard: an unknown (unset Optional+Computed) value's ValueInt64Pointer()
// resolves to a pointer to zero, which the controller rejects as out of
// range, so it must never reach the wire.
func lcmKitSpec() resourcekit.Spec[settingLcmModel, settings.Lcm] {
	return resourcekit.Spec[settingLcmModel, settings.Lcm]{
		TypeName: "setting_lcm",
		Subject:  "LCM Setting",
		New:      func() *settings.Lcm { return &settings.Lcm{} },
		Fields: []resourcekit.Field[settingLcmModel, settings.Lcm]{
			resourcekit.Int64PtrField[settingLcmModel, settings.Lcm]{
				Wire:     "brightness",
				Model:    func(m *settingLcmModel) *types.Int64 { return &m.Brightness },
				SDK:      func(s *settings.Lcm) **int64 { return &s.Brightness },
				Elide:    resourcekit.KeepZero,
				OmitZero: true,
			},
			resourcekit.BoolField[settingLcmModel, settings.Lcm]{
				Wire:  "enabled",
				Model: func(m *settingLcmModel) *types.Bool { return &m.Enabled },
				SDK:   func(s *settings.Lcm) *bool { return &s.Enabled },
			},
			resourcekit.Int64PtrField[settingLcmModel, settings.Lcm]{
				Wire:     "idle_timeout",
				Model:    func(m *settingLcmModel) *types.Int64 { return &m.IdleTimeout },
				SDK:      func(s *settings.Lcm) **int64 { return &s.IDleTimeout },
				Elide:    resourcekit.KeepZero,
				OmitZero: true,
			},
			resourcekit.BoolField[settingLcmModel, settings.Lcm]{
				Wire:  "sync",
				Model: func(m *settingLcmModel) *types.Bool { return &m.Sync },
				SDK:   func(s *settings.Lcm) *bool { return &s.Sync },
			},
			resourcekit.BoolField[settingLcmModel, settings.Lcm]{
				Wire:  "touch_event",
				Model: func(m *settingLcmModel) *types.Bool { return &m.TouchEvent },
				SDK:   func(s *settings.Lcm) *bool { return &s.TouchEvent },
			},
		},
	}
}

// lcmNestedSchema is the lcm SingleNestedAttribute's own Attributes, wrapped
// as a schema.Schema so resourcekit's conformance checks -- built for a
// whole resource's top-level schema -- can run against one section of
// unifi_setting instead.
func lcmNestedSchema(ctx context.Context) schema.Schema {
	built := resource_setting.SettingResourceSchema(ctx)
	lcm := built.Attributes["lcm"].(schema.SingleNestedAttribute) //nolint:forcetypeassert // lcm is declared as SingleNestedAttribute in the generated schema; a mismatch here is a generator regression this is meant to catch loudly.
	return schema.Schema{Attributes: lcm.Attributes}
}

// lcmKitBackend binds lcmKitSpec to a client: Read is GetSetting[*Lcm],
// UpdateFields is the masked UpdateSettingFields -- naming only the fields
// the plan set instead of the read-modify-write whole-document PUT
// writeLcmSection used.
func lcmKitBackend(client *ui.ApiClient) resourcekit.Backend[settings.Lcm] {
	return resourcekit.Backend[settings.Lcm]{
		Read: func(ctx context.Context, site, _ string) (*settings.Lcm, error) {
			_, lcm, err := ui.GetSetting[*settings.Lcm](client, ctx, site)
			if err != nil {
				return nil, err
			}
			return lcm, nil
		},
		UpdateFields: func(
			ctx context.Context, site string, in *settings.Lcm, fields ...string,
		) (*settings.Lcm, error) {
			if err := client.UpdateSettingFields(ctx, site, in, fields...); err != nil {
				return nil, err
			}
			return in, nil
		},
	}
}

// lcmKitSection builds the lcm entry for settingResource's Sections, bound
// to client the same way legacySectionsFor binds *settingResource.
func lcmKitSection(client *ui.ApiClient) resourcekit.Section[settingResourceModel] {
	spec := lcmKitSpec()
	spec.Backend = lcmKitBackend(client)
	return resourcekit.SpecSection[settingResourceModel, settingLcmModel, settings.Lcm]{
		SectionName: "lcm",
		Get:         func(m *settingResourceModel) *types.Object { return &m.Lcm },
		Set:         func(m *settingResourceModel, o types.Object) { m.Lcm = o },
		AttrTypes:   lcmAttrTypes,
		Spec:        spec,
	}
}
