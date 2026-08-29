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

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
	resource_setting "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_setting"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/resourcekit"
)

// settingSyslogModel is syslog's own section model, decoded out of
// settingResourceModel.Syslog.
type settingSyslogModel struct {
	Contents                    types.List   `tfsdk:"contents"`
	Debug                       types.Bool   `tfsdk:"debug"`
	Enabled                     types.Bool   `tfsdk:"enabled"`
	IP                          types.String `tfsdk:"ip"`
	LogAllContents              types.Bool   `tfsdk:"log_all_contents"`
	NetconsoleEnabled           types.Bool   `tfsdk:"netconsole_enabled"`
	NetconsoleHost              types.String `tfsdk:"netconsole_host"`
	NetconsolePort              types.Int64  `tfsdk:"netconsole_port"`
	Port                        types.Int64  `tfsdk:"port"`
	ThisController              types.Bool   `tfsdk:"this_controller"`
	ThisControllerEncryptedOnly types.Bool   `tfsdk:"this_controller_encrypted_only"`
}

// syslogAttrTypes types syslog's own object in state; it must match the
// generated schema exactly.
var syslogAttrTypes = map[string]attr.Type{
	"contents":                       types.ListType{ElemType: types.StringType},
	"debug":                          types.BoolType,
	"enabled":                        types.BoolType,
	"ip":                             types.StringType,
	"log_all_contents":               types.BoolType,
	"netconsole_enabled":             types.BoolType,
	"netconsole_host":                types.StringType,
	"netconsole_port":                types.Int64Type,
	"port":                           types.Int64Type,
	"this_controller":                types.BoolType,
	"this_controller_encrypted_only": types.BoolType,
}

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
		Fields: []resourcekit.Field[settingSyslogModel, settings.Rsyslogd]{
			resourcekit.StringListField[settingSyslogModel, settings.Rsyslogd]{
				Wire:  "contents",
				Model: func(m *settingSyslogModel) *types.List { return &m.Contents },
				SDK:   func(s *settings.Rsyslogd) *[]string { return &s.Contents },
				Elide: resourcekit.KeepZero,
			},
			resourcekit.BoolField[settingSyslogModel, settings.Rsyslogd]{
				Wire:  "debug",
				Model: func(m *settingSyslogModel) *types.Bool { return &m.Debug },
				SDK:   func(s *settings.Rsyslogd) *bool { return &s.Debug },
			},
			resourcekit.BoolField[settingSyslogModel, settings.Rsyslogd]{
				Wire:  "enabled",
				Model: func(m *settingSyslogModel) *types.Bool { return &m.Enabled },
				SDK:   func(s *settings.Rsyslogd) *bool { return &s.Enabled },
			},
			resourcekit.StringField[settingSyslogModel, settings.Rsyslogd]{
				Wire:  "ip",
				Model: func(m *settingSyslogModel) *types.String { return &m.IP },
				SDK:   func(s *settings.Rsyslogd) *string { return &s.IP },
				Elide: resourcekit.KeepZero,
			},
			resourcekit.BoolField[settingSyslogModel, settings.Rsyslogd]{
				Wire:  "log_all_contents",
				Model: func(m *settingSyslogModel) *types.Bool { return &m.LogAllContents },
				SDK:   func(s *settings.Rsyslogd) *bool { return &s.LogAllContents },
			},
			resourcekit.BoolField[settingSyslogModel, settings.Rsyslogd]{
				Wire:  "netconsole_enabled",
				Model: func(m *settingSyslogModel) *types.Bool { return &m.NetconsoleEnabled },
				SDK:   func(s *settings.Rsyslogd) *bool { return &s.NetconsoleEnabled },
			},
			resourcekit.StringField[settingSyslogModel, settings.Rsyslogd]{
				Wire:  "netconsole_host",
				Model: func(m *settingSyslogModel) *types.String { return &m.NetconsoleHost },
				SDK:   func(s *settings.Rsyslogd) *string { return &s.NetconsoleHost },
				Elide: resourcekit.KeepZero,
			},
			resourcekit.Int64PtrField[settingSyslogModel, settings.Rsyslogd]{
				Wire:     "netconsole_port",
				Model:    func(m *settingSyslogModel) *types.Int64 { return &m.NetconsolePort },
				SDK:      func(s *settings.Rsyslogd) **int64 { return &s.NetconsolePort },
				Elide:    resourcekit.KeepZero,
				OmitZero: true,
			},
			resourcekit.Int64PtrField[settingSyslogModel, settings.Rsyslogd]{
				Wire:     "port",
				Model:    func(m *settingSyslogModel) *types.Int64 { return &m.Port },
				SDK:      func(s *settings.Rsyslogd) **int64 { return &s.Port },
				Elide:    resourcekit.KeepZero,
				OmitZero: true,
			},
			resourcekit.BoolField[settingSyslogModel, settings.Rsyslogd]{
				Wire:  "this_controller",
				Model: func(m *settingSyslogModel) *types.Bool { return &m.ThisController },
				SDK:   func(s *settings.Rsyslogd) *bool { return &s.ThisController },
			},
			resourcekit.BoolField[settingSyslogModel, settings.Rsyslogd]{
				Wire:  "this_controller_encrypted_only",
				Model: func(m *settingSyslogModel) *types.Bool { return &m.ThisControllerEncryptedOnly },
				SDK:   func(s *settings.Rsyslogd) *bool { return &s.ThisControllerEncryptedOnly },
			},
		},
	}
}

// syslogNestedSchema is the syslog SingleNestedAttribute's own Attributes,
// wrapped as a schema.Schema so resourcekit's conformance checks -- built
// for a whole resource's top-level schema -- can run against one section of
// unifi_setting instead.
func syslogNestedSchema(ctx context.Context) schema.Schema {
	built := resource_setting.SettingResourceSchema(ctx)
	syslog := built.Attributes["syslog"].(schema.SingleNestedAttribute) //nolint:forcetypeassert // syslog is declared as SingleNestedAttribute in the generated schema; a mismatch here is a generator regression this is meant to catch loudly.
	return schema.Schema{Attributes: syslog.Attributes}
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
