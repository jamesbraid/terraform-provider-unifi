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

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/resourcekit"
)

// mgmtKitSpec maps every attribute of the generated mgmt schema
// (resource_setting/setting_resource_gen.go's "mgmt" SingleNestedAttribute)
// onto settings.Mgmt. Elide judgments follow resourcekit.ElideProblems'
// schema-driven rule, not a transcription of the old mgmtSettingToModel:
// every attribute here is Optional+Computed with no validator rejecting an
// empty value, so KeepZero is what the check demands for all but
// ssh_password (Optional, not Computed -- NullZero), ssh_username (an
// SDK-derived RegexMatches now rejects "" -- NullZero) and the eight plain
// bools (BoolField carries no Elide at all). ssh_username's flip is a real
// behaviour change: mgmtAfterReceive only nulls it when the prior is null
// or unknown (unconfigured); for a configured prior it leaves the model
// value untouched, so a configured ssh_username whose controller read
// comes back "" now surfaces as null rather than "". The plan-conditioned
// nulls mgmtSettingToModel applied on top of that live in mgmtAfterReceive
// instead, attribute by attribute -- see its own comment.
func mgmtKitSpec() resourcekit.Spec[settingMgmtModel, settings.Mgmt] {
	return resourcekit.Spec[settingMgmtModel, settings.Mgmt]{
		TypeName: "setting_mgmt",
		Subject:  "Mgmt Setting",
		New:      func() *settings.Mgmt { return &settings.Mgmt{} },
		Fields: resourcekit.Override(settingMgmtGenFields(), []resourcekit.Field[settingMgmtModel, settings.Mgmt]{
			resourcekit.ObjectListField[settingMgmtModel, settings.Mgmt, settings.SettingMgmtSSHKeys]{
				Wire:      "x_ssh_keys",
				Model:     func(m *settingMgmtModel) *types.List { return &m.SSHKeys },
				SDK:       func(s *settings.Mgmt) *[]settings.SettingMgmtSSHKeys { return &s.SSHKeys },
				AttrTypes: mgmtSshKeysAttrTypes,
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
		}),
	}
}

func mgmtSSHKeyEncode(
	ctx context.Context, object types.Object,
) (settings.SettingMgmtSSHKeys, diag.Diagnostics) {
	var model mgmtSshKeysModel
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
	return types.ObjectValueFrom(ctx, mgmtSshKeysAttrTypes, mgmtSshKeysModel{
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

	sshKeysType := types.ObjectType{AttrTypes: mgmtSshKeysAttrTypes}
	switch {
	case prior.SSHKeys.IsNull() || prior.SSHKeys.IsUnknown():
		model.SSHKeys = types.ListNull(sshKeysType)
	case len(model.SSHKeys.Elements()) == 0:
		model.SSHKeys = types.ListNull(sshKeysType)
	}

	return nil
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
