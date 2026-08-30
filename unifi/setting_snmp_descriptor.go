package unifi

// The snmp section descriptor: shaped like setting_radius_descriptor.go, not
// setting_mgmt_descriptor.go's ssh_password. Task 0 (this dispatch's own
// live-controller probe, run before any of this file was written) found the
// controller echoes community, username and x_password back verbatim on
// read -- no mask, no hash, no empty string, no absence -- so snmp is
// radius-shaped: both secrets are Optional+Computed+Sensitive with an
// AfterReceive that takes the controller's own decoded echo and nulls it
// only when the plan never named it, the same rule radiusAfterReceive
// applies to secret. mgmt's ssh_password pattern (an unconditional carry of
// whatever the plan held, because the controller never echoes a usable
// value at all) does not apply to any field here.
//
// settings.Snmp has exactly five members on the pinned SDK (go-unifi
// v1.111.0): Community, Enabled, EnabledV3, Password (wire x_password) and
// Username. There is no authentication-protocol or privacy-protocol field
// on this controller generation -- every member is modelled, none omitted.
// username is v3 identity, not a secret, and reads back as an ordinary
// Optional+Computed StringField with no AfterReceive treatment of its own.

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
	resource_setting "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_setting"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/resourcekit"
)

// settingSnmpModel is snmp's own section model, decoded out of
// settingResourceModel.Snmp.
type settingSnmpModel struct {
	Community types.String `tfsdk:"community"`
	Enabled   types.Bool   `tfsdk:"enabled"`
	EnabledV3 types.Bool   `tfsdk:"enabled_v3"`
	Password  types.String `tfsdk:"password"`
	Username  types.String `tfsdk:"username"`
}

// snmpAttrTypes types snmp's own object in state; it must match the
// generated schema exactly.
var snmpAttrTypes = map[string]attr.Type{
	"community":  types.StringType,
	"enabled":    types.BoolType,
	"enabled_v3": types.BoolType,
	"password":   types.StringType,
	"username":   types.StringType,
}

// snmpKitSpec maps every attribute of the generated snmp schema
// (resource_setting/setting_resource_gen.go's "snmp" SingleNestedAttribute)
// onto settings.Snmp. community, password and username each carry a
// RegexMatches validator (transcribed from the SDK's own field comments --
// see setting.json's snmp grouping) that rejects an empty string, so all
// three want NullZero by resourcekit.ElideProblems' schema-driven rule, the
// same reasoning radius.secret's own comment applies. enabled/enabled_v3
// are plain bools, which carry no Elide at all.
func snmpKitSpec() resourcekit.Spec[settingSnmpModel, settings.Snmp] {
	return resourcekit.Spec[settingSnmpModel, settings.Snmp]{
		TypeName: "setting_snmp",
		Subject:  "Snmp Setting",
		New:      func() *settings.Snmp { return &settings.Snmp{} },
		Fields: []resourcekit.Field[settingSnmpModel, settings.Snmp]{
			resourcekit.StringField[settingSnmpModel, settings.Snmp]{
				Wire:  "community",
				Model: func(m *settingSnmpModel) *types.String { return &m.Community },
				SDK:   func(s *settings.Snmp) *string { return &s.Community },
				Elide: resourcekit.NullZero,
			},
			resourcekit.BoolField[settingSnmpModel, settings.Snmp]{
				Wire:  "enabled",
				Model: func(m *settingSnmpModel) *types.Bool { return &m.Enabled },
				SDK:   func(s *settings.Snmp) *bool { return &s.Enabled },
			},
			resourcekit.BoolField[settingSnmpModel, settings.Snmp]{
				Wire:  "enabledV3",
				Model: func(m *settingSnmpModel) *types.Bool { return &m.EnabledV3 },
				SDK:   func(s *settings.Snmp) *bool { return &s.EnabledV3 },
			},
			resourcekit.StringField[settingSnmpModel, settings.Snmp]{
				Wire:  "x_password",
				Model: func(m *settingSnmpModel) *types.String { return &m.Password },
				SDK:   func(s *settings.Snmp) *string { return &s.Password },
				Elide: resourcekit.NullZero,
			},
			resourcekit.StringField[settingSnmpModel, settings.Snmp]{
				Wire:  "username",
				Model: func(m *settingSnmpModel) *types.String { return &m.Username },
				SDK:   func(s *settings.Snmp) *string { return &s.Username },
				Elide: resourcekit.NullZero,
			},
		},
	}
}

// snmpAfterReceive plan-conditions snmp's two secrets exactly the way
// radiusAfterReceive plan-conditions radius.secret: an unconfigured secret
// (prior null or unknown) always comes back null, and a configured one
// surfaces whatever Spec.ToModel already decoded off the wire -- the
// controller's own echo, pinned verbatim by Task 0's live probe -- not the
// prior/plan string. username carries no such treatment: it is not a
// secret, so its ordinary unconditional-mirror read (Spec.ToModel alone) is
// exactly what a practitioner wants.
func snmpAfterReceive(
	_ context.Context, _ *settings.Snmp, model *settingSnmpModel, prior settingSnmpModel,
) diag.Diagnostics {
	if prior.Community.IsNull() || prior.Community.IsUnknown() {
		model.Community = types.StringNull()
	}
	if prior.Password.IsNull() || prior.Password.IsUnknown() {
		model.Password = types.StringNull()
	}
	return nil
}

// snmpNestedSchema is the snmp SingleNestedAttribute's own Attributes,
// wrapped as a schema.Schema so resourcekit's conformance checks -- built
// for a whole resource's top-level schema -- can run against one section of
// unifi_setting instead.
func snmpNestedSchema(ctx context.Context) schema.Schema {
	built := resource_setting.SettingResourceSchema(ctx)
	snmp := built.Attributes["snmp"].(schema.SingleNestedAttribute) //nolint:forcetypeassert // snmp is declared as SingleNestedAttribute in the generated schema; a mismatch here is a generator regression this is meant to catch loudly.
	return schema.Schema{Attributes: snmp.Attributes}
}

// snmpKitBackend binds snmpKitSpec to a client: Read is GetSetting[*Snmp],
// UpdateFields is the masked UpdateSettingFields -- naming only the fields
// the plan set.
func snmpKitBackend(client *ui.ApiClient) resourcekit.Backend[settings.Snmp] {
	return resourcekit.Backend[settings.Snmp]{
		Read: func(ctx context.Context, site, _ string) (*settings.Snmp, error) {
			_, snmp, err := ui.GetSetting[*settings.Snmp](client, ctx, site)
			if err != nil {
				return nil, err
			}
			return snmp, nil
		},
		UpdateFields: func(
			ctx context.Context, site string, in *settings.Snmp, fields ...string,
		) (*settings.Snmp, error) {
			if err := client.UpdateSettingFields(ctx, site, in, fields...); err != nil {
				return nil, err
			}
			return in, nil
		},
	}
}

// snmpKitSection builds the snmp entry for settingResource's Sections,
// bound to client via settingKitSections, which calls it with
// r.client.ApiClient.
func snmpKitSection(client *ui.ApiClient) resourcekit.Section[settingResourceModel] {
	spec := snmpKitSpec()
	spec.Backend = snmpKitBackend(client)
	return resourcekit.SpecSection[settingResourceModel, settingSnmpModel, settings.Snmp]{
		SectionName:  "snmp",
		Get:          func(m *settingResourceModel) *types.Object { return &m.Snmp },
		Set:          func(m *settingResourceModel, o types.Object) { m.Snmp = o },
		AttrTypes:    snmpAttrTypes,
		Spec:         spec,
		AfterReceive: snmpAfterReceive,
	}
}
