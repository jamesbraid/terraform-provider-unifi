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

	"github.com/hashicorp/terraform-plugin-framework/types"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/resourcekit"
)

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
		Fields:   settingAutoSpeedtestGenFields(),
	}
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
// settingResource's Sections, bound to client via settingKitSections, which
// calls it with r.client.ApiClient.
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
