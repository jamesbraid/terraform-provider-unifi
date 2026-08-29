package unifi

// The auto_speedtest section descriptor: an unconditional-mirror hydration
// with no specials, replacing the hand-written writeAutoSpeedtestSection /
// readAutoSpeedtestSection (setting_sections.go) and their
// autoSpeedtestModelToSetting / autoSpeedtestSettingToModel mappers (deleted
// from setting_resource.go). The model type and attribute-type map moved
// here too, from setting_resource.go: descriptor_mapping_test.go's
// loadDescriptors reads a descriptor's model tags from the same file the
// Spec literal is in. See setting_mgmt_descriptor.go for the shape every
// section descriptor follows.

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

// settingAutoSpeedtestModel is auto_speedtest's own section model, decoded
// out of settingResourceModel.AutoSpeedtest.
type settingAutoSpeedtestModel struct {
	CronExpr types.String `tfsdk:"cron_expr"`
	Enabled  types.Bool   `tfsdk:"enabled"`
}

// autoSpeedtestAttrTypes types auto_speedtest's own object in state; it must
// match the generated schema exactly.
var autoSpeedtestAttrTypes = map[string]attr.Type{
	"cron_expr": types.StringType,
	"enabled":   types.BoolType,
}

// autoSpeedtestKitSpec maps every attribute of the generated auto_speedtest
// schema (resource_setting/setting_resource_gen.go's "auto_speedtest"
// SingleNestedAttribute) onto settings.AutoSpeedtest. Elide judgments follow
// resourcekit.ElideProblems' schema-driven rule, not a transcription of the
// old autoSpeedtestSettingToModel: cron_expr is Optional+Computed with no
// validator rejecting an empty value, so KeepZero is what the check demands
// -- diverging from the deleted mapper, which nulled an empty CronExpr via
// util.StringValueOrNull. No existing test covered that null-on-empty
// behaviour (TestAutoSpeedtestSettingRoundTrip only ever exercised a
// non-empty cron_expr), so the instrument's KeepZero stands as written here.
// enabled is a plain bool, which carries no Elide claim at all.
func autoSpeedtestKitSpec() resourcekit.Spec[settingAutoSpeedtestModel, settings.AutoSpeedtest] {
	return resourcekit.Spec[settingAutoSpeedtestModel, settings.AutoSpeedtest]{
		TypeName: "setting_auto_speedtest",
		Subject:  "Auto Speedtest Setting",
		New:      func() *settings.AutoSpeedtest { return &settings.AutoSpeedtest{} },
		Fields: []resourcekit.Field[settingAutoSpeedtestModel, settings.AutoSpeedtest]{
			resourcekit.StringField[settingAutoSpeedtestModel, settings.AutoSpeedtest]{
				Wire:  "cron_expr",
				Model: func(m *settingAutoSpeedtestModel) *types.String { return &m.CronExpr },
				SDK:   func(s *settings.AutoSpeedtest) *string { return &s.CronExpr },
				Elide: resourcekit.KeepZero,
			},
			resourcekit.BoolField[settingAutoSpeedtestModel, settings.AutoSpeedtest]{
				Wire:  "enabled",
				Model: func(m *settingAutoSpeedtestModel) *types.Bool { return &m.Enabled },
				SDK:   func(s *settings.AutoSpeedtest) *bool { return &s.Enabled },
			},
		},
	}
}

// autoSpeedtestNestedSchema is the auto_speedtest SingleNestedAttribute's own
// Attributes, wrapped as a schema.Schema so resourcekit's conformance checks
// -- built for a whole resource's top-level schema -- can run against one
// section of unifi_setting instead.
func autoSpeedtestNestedSchema(ctx context.Context) schema.Schema {
	built := resource_setting.SettingResourceSchema(ctx)
	autoSpeedtest := built.Attributes["auto_speedtest"].(schema.SingleNestedAttribute) //nolint:forcetypeassert // auto_speedtest is declared as SingleNestedAttribute in the generated schema; a mismatch here is a generator regression this is meant to catch loudly.
	return schema.Schema{Attributes: autoSpeedtest.Attributes}
}

// autoSpeedtestKitBackend binds autoSpeedtestKitSpec to a client: Read is
// GetSetting[*AutoSpeedtest], UpdateFields is the masked UpdateSettingFields
// -- naming only the fields the plan set instead of the read-modify-write
// whole-document PUT writeAutoSpeedtestSection used.
func autoSpeedtestKitBackend(client *ui.ApiClient) resourcekit.Backend[settings.AutoSpeedtest] {
	return resourcekit.Backend[settings.AutoSpeedtest]{
		Read: func(ctx context.Context, site, _ string) (*settings.AutoSpeedtest, error) {
			_, as, err := ui.GetSetting[*settings.AutoSpeedtest](client, ctx, site)
			if err != nil {
				return nil, err
			}
			return as, nil
		},
		UpdateFields: func(
			ctx context.Context, site string, in *settings.AutoSpeedtest, fields ...string,
		) (*settings.AutoSpeedtest, error) {
			if err := client.UpdateSettingFields(ctx, site, in, fields...); err != nil {
				return nil, err
			}
			return in, nil
		},
	}
}

// autoSpeedtestKitSection builds the auto_speedtest entry for
// settingResource's Sections, bound to client the same way legacySectionsFor
// binds *settingResource.
func autoSpeedtestKitSection(client *ui.ApiClient) resourcekit.Section[settingResourceModel] {
	spec := autoSpeedtestKitSpec()
	spec.Backend = autoSpeedtestKitBackend(client)
	return resourcekit.SpecSection[settingResourceModel, settingAutoSpeedtestModel, settings.AutoSpeedtest]{
		SectionName: "auto_speedtest",
		Get:         func(m *settingResourceModel) *types.Object { return &m.AutoSpeedtest },
		Set:         func(m *settingResourceModel, o types.Object) { m.AutoSpeedtest = o },
		AttrTypes:   autoSpeedtestAttrTypes,
		Spec:        spec,
	}
}
