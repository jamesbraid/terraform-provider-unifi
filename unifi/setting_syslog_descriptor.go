package unifi

// The syslog section descriptor: an unconditional-mirror hydration whose
// specials are the #303 omit-not-zero guard on port/netconsole_port and the
// controller key -- settings.Rsyslogd's own GetSettingKey answer is
// "rsyslogd", not "syslog" -- replacing the hand-written writeSyslogSection
// / readSyslogSection (setting_sections.go) and their
// syslogModelToSetting / syslogSettingToModel mappers (deleted from
// setting_resource.go). The model type and attribute-type map moved here
// too, from setting_resource.go: descriptor_mapping_test.go's
// loadDescriptors reads a descriptor's model tags from the same file the
// Spec literal is in. See setting_mgmt_descriptor.go for the shape every
// section descriptor follows.
//
// syslog also carries a plan-time rule, separate from this descriptor: the
// controller rejects enabled=true with no ip (api.err.Invalid), enforced by
// settingResource's own ValidateConfig -- see setting_syslog_validate.go.

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/types"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/resourcekit"
)

// syslogKitSpec maps every attribute of the generated syslog schema
// (resource_setting/setting_resource_gen.go's "syslog" SingleNestedAttribute)
// onto settings.Rsyslogd. Elide judgments follow resourcekit.ElideProblems'
// schema-driven rule, not a transcription of the old syslogSettingToModel:
// ip and netconsole_host are Optional+Computed with no validator rejecting
// an empty value, so KeepZero is what the check demands -- diverging from
// the deleted mapper, which nulled an empty read via util.StringValueOrNull.
// No existing test covered that null-on-empty behaviour (TestSettingBlocksRoundTrip's
// syslog subtest and TestSyslogOmitsUnsetPorts both used a non-empty ip), so
// the instrument's KeepZero stands as written here, the same precedent
// auto_speedtest's cron_expr set. contents carries the same KeepZero for the
// same reason: no validator on a ListAttribute can drive zeroIsRejected,
// which only ever inspects a StringAttribute's. port and netconsole_port
// carry the #303 write-side OmitZero guard alongside KeepZero, for the
// reason lcm's brightness/idle_timeout do (see setting_lcm_descriptor.go).
func syslogKitSpec() resourcekit.Spec[settingSyslogModel, settings.Rsyslogd] {
	return resourcekit.Spec[settingSyslogModel, settings.Rsyslogd]{
		TypeName: "setting_syslog",
		Subject:  "Syslog Setting",
		New:      func() *settings.Rsyslogd { return &settings.Rsyslogd{} },
		Fields:   settingSyslogGenFields(),
	}
}

// syslogKitBackend binds syslogKitSpec to a client: Read is
// GetSetting[*Rsyslogd], UpdateFields is the masked UpdateSettingFields --
// naming only the fields the plan set instead of the read-modify-write
// whole-document PUT writeSyslogSection used. settings.Rsyslogd's own
// GetSettingKey answer is "rsyslogd" -- both calls address that key, not
// "syslog", entirely inside go-unifi; TestSyslogSpecKeyIsRsyslogd pins it.
func syslogKitBackend(client *ui.ApiClient) resourcekit.Backend[settings.Rsyslogd] {
	return resourcekit.Backend[settings.Rsyslogd]{
		Read: func(ctx context.Context, site, _ string) (*settings.Rsyslogd, error) {
			_, syslog, err := ui.GetSetting[*settings.Rsyslogd](client, ctx, site)
			if err != nil {
				return nil, err
			}
			return syslog, nil
		},
		UpdateFields: func(
			ctx context.Context, site string, in *settings.Rsyslogd, fields ...string,
		) (*settings.Rsyslogd, error) {
			if err := client.UpdateSettingFields(ctx, site, in, fields...); err != nil {
				return nil, err
			}
			return in, nil
		},
	}
}

// syslogKitSection builds the syslog entry for settingResource's Sections,
// bound to client via settingKitSections, which calls it with
// r.client.ApiClient.
func syslogKitSection(client *ui.ApiClient) resourcekit.Section[settingResourceModel] {
	spec := syslogKitSpec()
	spec.Backend = syslogKitBackend(client)
	return resourcekit.SpecSection[settingResourceModel, settingSyslogModel, settings.Rsyslogd]{
		SectionName: "syslog",
		Get:         func(m *settingResourceModel) *types.Object { return &m.Syslog },
		Set:         func(m *settingResourceModel, o types.Object) { m.Syslog = o },
		AttrTypes:   syslogAttrTypes,
		Spec:        spec,
	}
}
