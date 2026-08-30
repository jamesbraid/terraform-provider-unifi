package unifi

// The mgmt section descriptor: unifi_setting's spike surface for
// resourcekit.SpecSection, replacing the hand-written writeMgmtSection /
// readMgmtSection (setting_sections.go) and their mgmtModelToSetting /
// mgmtSettingToModel mappers (deleted from setting_resource.go). The model
// types and attribute-type maps moved here too, from setting_resource.go:
// descriptor_mapping_test.go's loadDescriptors reads a descriptor's model
// tags from the same file the Spec literal is in.

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
	resource_setting "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_setting"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/resourcekit"
)

// sshKeyModel is one element of mgmt's ssh_keys list.
type sshKeyModel struct {
	Name    types.String `tfsdk:"name"`
	Type    types.String `tfsdk:"type"`
	Key     types.String `tfsdk:"key"`
	Comment types.String `tfsdk:"comment"`
}

// settingMgmtModel is mgmt's own section model, decoded out of
// settingResourceModel.Mgmt.
type settingMgmtModel struct {
	AutoUpgrade            types.Bool   `tfsdk:"auto_upgrade"`
	AutoUpgradeHour        types.Int64  `tfsdk:"auto_upgrade_hour"`
	SSHEnabled             types.Bool   `tfsdk:"ssh_enabled"`
	SSHKeys                types.List   `tfsdk:"ssh_keys"`
	AdvancedFeatureEnabled types.Bool   `tfsdk:"advanced_feature_enabled"`
	DebugToolsEnabled      types.Bool   `tfsdk:"debug_tools_enabled"`
	DirectConnectEnabled   types.Bool   `tfsdk:"direct_connect_enabled"`
	UnifiIdpEnabled        types.Bool   `tfsdk:"unifi_idp_enabled"`
	WifimanEnabled         types.Bool   `tfsdk:"wifiman_enabled"`
	SSHUsername            types.String `tfsdk:"ssh_username"`
	SSHPassword            types.String `tfsdk:"ssh_password"`
	SSHAuthPasswordEnabled types.Bool   `tfsdk:"ssh_auth_password_enabled"`
}

// mgmtSSHKeyAttrTypes and mgmtAttrTypes type mgmt's ssh_keys elements and
// mgmt's own object in state; both must match the generated schema exactly.
var (
	mgmtSSHKeyAttrTypes = map[string]attr.Type{
		"name":    types.StringType,
		"type":    types.StringType,
		"key":     types.StringType,
		"comment": types.StringType,
	}
	mgmtAttrTypes = map[string]attr.Type{
		"auto_upgrade":      types.BoolType,
		"auto_upgrade_hour": types.Int64Type,
		"ssh_enabled":       types.BoolType,
		"ssh_keys": types.ListType{
			ElemType: types.ObjectType{AttrTypes: mgmtSSHKeyAttrTypes},
		},
		"advanced_feature_enabled":  types.BoolType,
		"debug_tools_enabled":       types.BoolType,
		"direct_connect_enabled":    types.BoolType,
		"unifi_idp_enabled":         types.BoolType,
		"wifiman_enabled":           types.BoolType,
		"ssh_username":              types.StringType,
		"ssh_password":              types.StringType,
		"ssh_auth_password_enabled": types.BoolType,
	}
)

// mgmtKitSpec maps every attribute of the generated mgmt schema
// (resource_setting/setting_resource_gen.go's "mgmt" SingleNestedAttribute)
// onto settings.Mgmt. Elide judgments follow resourcekit.ElideProblems'
// schema-driven rule, not a transcription of the old mgmtSettingToModel:
// every attribute here is Optional+Computed with no validator rejecting an
// empty value, so KeepZero is what the check demands for all but
// ssh_password (Optional, not Computed -- NullZero), ssh_username (an
// SDK-derived RegexMatches now rejects "" -- NullZero; mgmtAfterReceive's
// own stringOrNull already plan-conditions it on the unconfigured path, so
// this only brings the descriptor into agreement with the check) and the
// eight plain bools (BoolField carries no Elide at all). The plan-conditioned
// nulls mgmtSettingToModel applied on top of that live in mgmtAfterReceive
// instead, attribute by attribute -- see its own comment.
func mgmtKitSpec() resourcekit.Spec[settingMgmtModel, settings.Mgmt] {
	return resourcekit.Spec[settingMgmtModel, settings.Mgmt]{
		TypeName: "setting_mgmt",
		Subject:  "Mgmt Setting",
		New:      func() *settings.Mgmt { return &settings.Mgmt{} },
		Fields: []resourcekit.Field[settingMgmtModel, settings.Mgmt]{
			resourcekit.BoolField[settingMgmtModel, settings.Mgmt]{
				Wire:  "advanced_feature_enabled",
				Model: func(m *settingMgmtModel) *types.Bool { return &m.AdvancedFeatureEnabled },
				SDK:   func(s *settings.Mgmt) *bool { return &s.AdvancedFeatureEnabled },
			},
			resourcekit.BoolField[settingMgmtModel, settings.Mgmt]{
				Wire:  "auto_upgrade",
				Model: func(m *settingMgmtModel) *types.Bool { return &m.AutoUpgrade },
				SDK:   func(s *settings.Mgmt) *bool { return &s.AutoUpgrade },
			},
			resourcekit.Int64PtrField[settingMgmtModel, settings.Mgmt]{
				Wire:  "auto_upgrade_hour",
				Model: func(m *settingMgmtModel) *types.Int64 { return &m.AutoUpgradeHour },
				SDK:   func(s *settings.Mgmt) **int64 { return &s.AutoUpgradeHour },
				Elide: resourcekit.KeepZero,
			},
			resourcekit.BoolField[settingMgmtModel, settings.Mgmt]{
				Wire:  "debug_tools_enabled",
				Model: func(m *settingMgmtModel) *types.Bool { return &m.DebugToolsEnabled },
				SDK:   func(s *settings.Mgmt) *bool { return &s.DebugToolsEnabled },
			},
			resourcekit.BoolField[settingMgmtModel, settings.Mgmt]{
				Wire:  "direct_connect_enabled",
				Model: func(m *settingMgmtModel) *types.Bool { return &m.DirectConnectEnabled },
				SDK:   func(s *settings.Mgmt) *bool { return &s.DirectConnectEnabled },
			},
			resourcekit.BoolField[settingMgmtModel, settings.Mgmt]{
				Wire:  "x_ssh_auth_password_enabled",
				Model: func(m *settingMgmtModel) *types.Bool { return &m.SSHAuthPasswordEnabled },
				SDK:   func(s *settings.Mgmt) *bool { return &s.SSHAuthPasswordEnabled },
			},
			resourcekit.BoolField[settingMgmtModel, settings.Mgmt]{
				Wire:  "x_ssh_enabled",
				Model: func(m *settingMgmtModel) *types.Bool { return &m.SSHEnabled },
				SDK:   func(s *settings.Mgmt) *bool { return &s.SSHEnabled },
			},
			resourcekit.ObjectListField[settingMgmtModel, settings.Mgmt, settings.SettingMgmtSSHKeys]{
				Wire:      "x_ssh_keys",
				Model:     func(m *settingMgmtModel) *types.List { return &m.SSHKeys },
				SDK:       func(s *settings.Mgmt) *[]settings.SettingMgmtSSHKeys { return &s.SSHKeys },
				AttrTypes: mgmtSSHKeyAttrTypes,
				Encode:    mgmtSSHKeyEncode,
				Decode:    mgmtSSHKeyDecode,
				// date and fingerprint are controller-assigned (a key's
				// upload timestamp and its computed fingerprint) and
				// force-emitted by SettingMgmtSSHKeys' own json tags; the
				// schema doesn't model either, so a write sends their Go
				// zero, which the controller re-derives from the key.
				Unmodelled: []string{"date", "fingerprint"},
				Elide:      resourcekit.KeepZero,
			},
			resourcekit.StringField[settingMgmtModel, settings.Mgmt]{
				Wire:  "x_ssh_password",
				Model: func(m *settingMgmtModel) *types.String { return &m.SSHPassword },
				SDK:   func(s *settings.Mgmt) *string { return &s.SSHPassword },
				Elide: resourcekit.NullZero,
			},
			resourcekit.StringField[settingMgmtModel, settings.Mgmt]{
				Wire:  "x_ssh_username",
				Model: func(m *settingMgmtModel) *types.String { return &m.SSHUsername },
				SDK:   func(s *settings.Mgmt) *string { return &s.SSHUsername },
				Elide: resourcekit.NullZero,
			},
			resourcekit.BoolField[settingMgmtModel, settings.Mgmt]{
				Wire:  "unifi_idp_enabled",
				Model: func(m *settingMgmtModel) *types.Bool { return &m.UnifiIdpEnabled },
				SDK:   func(s *settings.Mgmt) *bool { return &s.UniFiIdentityProviderEnabled },
			},
			resourcekit.BoolField[settingMgmtModel, settings.Mgmt]{
				Wire:  "wifiman_enabled",
				Model: func(m *settingMgmtModel) *types.Bool { return &m.WifimanEnabled },
				SDK:   func(s *settings.Mgmt) *bool { return &s.WifimanEnabled },
			},
		},
	}
}

func mgmtSSHKeyEncode(
	ctx context.Context, object types.Object,
) (settings.SettingMgmtSSHKeys, diag.Diagnostics) {
	var model sshKeyModel
	diags := object.As(ctx, &model, basetypes.ObjectAsOptions{})
	return settings.SettingMgmtSSHKeys{
		Name:    model.Name.ValueString(),
		KeyType: model.Type.ValueString(),
		Key:     model.Key.ValueString(),
		Comment: model.Comment.ValueString(),
	}, diags
}

func mgmtSSHKeyDecode(
	ctx context.Context, element settings.SettingMgmtSSHKeys,
) (types.Object, diag.Diagnostics) {
	return types.ObjectValueFrom(ctx, mgmtSSHKeyAttrTypes, sshKeyModel{
		Name:    types.StringValue(element.Name),
		Type:    types.StringValue(element.KeyType),
		Key:     types.StringValue(element.Key),
		Comment: types.StringValue(element.Comment),
	})
}

// mgmtAfterReceive reproduces today's mgmtSettingToModel: every mgmt
// attribute is plan-conditioned -- null unless the practitioner's own
// config (prior) set it, so an unmanaged mgmt attribute never drifts.
// ssh_password is never read from the wire at all (the controller returns
// only a hash); ssh_keys additionally nulls a configured-but-empty read,
// matching mgmtSettingToModel's own nested else-branch.
func mgmtAfterReceive(
	_ context.Context, _ *settings.Mgmt, model *settingMgmtModel, prior settingMgmtModel,
) diag.Diagnostics {
	boolOrNull := func(priorValue, modelValue types.Bool) types.Bool {
		if priorValue.IsNull() || priorValue.IsUnknown() {
			return types.BoolNull()
		}
		return modelValue
	}
	model.AutoUpgrade = boolOrNull(prior.AutoUpgrade, model.AutoUpgrade)
	model.SSHEnabled = boolOrNull(prior.SSHEnabled, model.SSHEnabled)
	model.AdvancedFeatureEnabled = boolOrNull(prior.AdvancedFeatureEnabled, model.AdvancedFeatureEnabled)
	model.DebugToolsEnabled = boolOrNull(prior.DebugToolsEnabled, model.DebugToolsEnabled)
	model.DirectConnectEnabled = boolOrNull(prior.DirectConnectEnabled, model.DirectConnectEnabled)
	model.UnifiIdpEnabled = boolOrNull(prior.UnifiIdpEnabled, model.UnifiIdpEnabled)
	model.WifimanEnabled = boolOrNull(prior.WifimanEnabled, model.WifimanEnabled)
	model.SSHAuthPasswordEnabled = boolOrNull(prior.SSHAuthPasswordEnabled, model.SSHAuthPasswordEnabled)

	if prior.AutoUpgradeHour.IsNull() || prior.AutoUpgradeHour.IsUnknown() {
		model.AutoUpgradeHour = types.Int64Null()
	}
	if prior.SSHUsername.IsNull() || prior.SSHUsername.IsUnknown() {
		model.SSHUsername = types.StringNull()
	}

	// The controller never echoes plaintext (only a hash), so the read
	// always restores whatever the plan/prior held -- configured or not.
	model.SSHPassword = prior.SSHPassword

	sshKeysType := types.ObjectType{AttrTypes: mgmtSSHKeyAttrTypes}
	switch {
	case prior.SSHKeys.IsNull() || prior.SSHKeys.IsUnknown():
		model.SSHKeys = types.ListNull(sshKeysType)
	case len(model.SSHKeys.Elements()) == 0:
		model.SSHKeys = types.ListNull(sshKeysType)
	}

	return nil
}

// mgmtNestedSchema is the mgmt SingleNestedAttribute's own Attributes,
// wrapped as a schema.Schema so resourcekit's conformance checks -- built
// for a whole resource's top-level schema -- can run against one section of
// unifi_setting instead.
func mgmtNestedSchema(ctx context.Context) schema.Schema {
	built := resource_setting.SettingResourceSchema(ctx)
	mgmt := built.Attributes["mgmt"].(schema.SingleNestedAttribute) //nolint:forcetypeassert // mgmt is declared as SingleNestedAttribute in the generated schema; a mismatch here is a generator regression this is meant to catch loudly.
	return schema.Schema{Attributes: mgmt.Attributes}
}

// mgmtKitBackend binds mgmtKitSpec to a client: Read is GetSetting[*Mgmt],
// UpdateFields is the masked UpdateSettingFields -- the spike's whole point,
// since it lets Write name only the fields the plan set instead of the
// read-modify-write whole-document PUT writeMgmtSection used.
func mgmtKitBackend(client *ui.ApiClient) resourcekit.Backend[settings.Mgmt] {
	return resourcekit.Backend[settings.Mgmt]{
		Read: func(ctx context.Context, site, _ string) (*settings.Mgmt, error) {
			_, mgmt, err := ui.GetSetting[*settings.Mgmt](client, ctx, site)
			if err != nil {
				return nil, err
			}
			return mgmt, nil
		},
		UpdateFields: func(
			ctx context.Context, site string, in *settings.Mgmt, fields ...string,
		) (*settings.Mgmt, error) {
			if err := client.UpdateSettingFields(ctx, site, in, fields...); err != nil {
				return nil, err
			}
			return in, nil
		},
	}
}

// mgmtKitSection builds the mgmt entry for settingResource's Sections, bound
// to client via settingKitSections, which calls it with r.client.ApiClient.
func mgmtKitSection(client *ui.ApiClient) resourcekit.Section[settingResourceModel] {
	spec := mgmtKitSpec()
	spec.Backend = mgmtKitBackend(client)
	return resourcekit.SpecSection[settingResourceModel, settingMgmtModel, settings.Mgmt]{
		SectionName:  "mgmt",
		Get:          func(m *settingResourceModel) *types.Object { return &m.Mgmt },
		Set:          func(m *settingResourceModel, o types.Object) { m.Mgmt = o },
		AttrTypes:    mgmtAttrTypes,
		Spec:         spec,
		AfterReceive: mgmtAfterReceive,
	}
}
