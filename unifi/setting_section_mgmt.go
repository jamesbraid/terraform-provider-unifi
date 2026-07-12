package unifi

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/objectplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
)

// mgmtSection is the settingSection implementation for the "mgmt"
// (Management) settings section — the trickiest section in the migration.
// It combines several features seen individually in earlier sections plus
// two new ones together in a single section:
//
//   - A write-only secret leaf (ssh_password, wire key x_ssh_password) —
//     same handling as radius's secret/x_secret: decode always preserves
//     prior state for this leaf rather than reading the masked wire value,
//     and overlay deletes x_ssh_password from the PUT body when the config
//     value is null/unknown rather than ever re-sending a masked value; a
//     configured value (including an explicit empty string) is written
//     verbatim.
//
//   - A nested object-list leaf (ssh_keys, wire key x_ssh_keys) of PUBLIC
//     keys, decoded/overlaid through the generalized nested codec
//     (decodeObjectList/overlayObjectList).
//
//   - Many ssh_*->x_ssh_* wire-key remaps: the model's tfsdk names are
//     ssh_enabled/ssh_username/ssh_password/ssh_keys/
//     ssh_auth_password_enabled, but the controller's wire keys for all five
//     are x_ssh_-prefixed. Every other leaf is 1:1.
//
//     TODO(go-unifi): these read/write raw "x_ssh_*" map keys rather than
//     settings.Mgmt's SSHEnabled/SSHUsername/SSHPassword/SSHKeys
//     fields (already correctly tagged `json:"x_ssh_*"` in go-unifi).
//     PERMANENT: "x_ssh_" is the controller's own wire naming, not a
//     go-unifi gap — the tfsdk-name-to-wire-key remap table here would be
//     needed even against the typed struct, and raw map access is required
//     regardless for this section's unmodeled-field RMW (dataCopy's TODO in
//     setting_snapshot.go).
//
//   - Top-level read-modify-write (RMW): the controller's stored data for
//     this key carries fields the model does not expose at all
//     (alert_enabled, boot_sound, led_enabled, outdoor_mode_enabled,
//     x_ssh_bind_wildcard). overlay() starts from a copy of the snapshot's
//     current section data so those unmodeled fields survive the merge
//     untouched.
//
//   - Per-element controller metadata inside ssh_keys: each wire element
//     also carries unmodeled date/fingerprint fields (assigned by the
//     controller when a key is added) that are not part of the model's
//     ssh_keys schema at all. Unlike the top-level RMW above, these are NOT
//     preserved by position: overlayObjectList builds each element fresh
//     from the model's leaves, and overlay() explicitly blanks date/
//     fingerprint to "" on every element (blankSSHKeyControllerMetadata),
//     matching legacy byte-for-byte. Preserving them by list index would
//     mis-attach one key's metadata onto a different key across a reorder
//     or replace — the ssh "key" value is not a durable identity to match
//     on either, since rotation changes it.
type mgmtSection struct{}

func init() {
	registerSection(mgmtSection{})
}

func (mgmtSection) key() string      { return "mgmt" }
func (mgmtSection) attrName() string { return "mgmt" }

// schemaAttribute is byte-identical to the inline "mgmt" block in
// setting_resource.go's schema (setting_resource.go:876-969).
func (mgmtSection) schemaAttribute() schema.Attribute {
	return schema.SingleNestedAttribute{
		MarkdownDescription: "Management settings.",
		Optional:            true,
		Computed:            true,
		PlanModifiers: []planmodifier.Object{
			objectplanmodifier.UseStateForUnknown(),
		},
		Attributes: map[string]schema.Attribute{
			"auto_upgrade": schema.BoolAttribute{
				MarkdownDescription: "Automatically upgrade device firmware.",
				Optional:            true,
				Computed:            true,
			},
			"ssh_enabled": schema.BoolAttribute{
				MarkdownDescription: "Enable SSH authentication.",
				Optional:            true,
				Computed:            true,
			},
			"auto_upgrade_hour": schema.Int64Attribute{
				MarkdownDescription: "Hour of day (0-23) for automatic firmware upgrades.",
				Optional:            true,
				Computed:            true,
				Validators:          []validator.Int64{int64validator.Between(0, 23)},
			},
			"advanced_feature_enabled": schema.BoolAttribute{
				MarkdownDescription: "Enable advanced features.",
				Optional:            true,
				Computed:            true,
			},
			"debug_tools_enabled": schema.BoolAttribute{
				MarkdownDescription: "Enable debug tools.",
				Optional:            true,
				Computed:            true,
			},
			"direct_connect_enabled": schema.BoolAttribute{
				MarkdownDescription: "Enable Direct Connect (remote access).",
				Optional:            true,
				Computed:            true,
			},
			"unifi_idp_enabled": schema.BoolAttribute{
				MarkdownDescription: "Enable the UniFi Identity Provider.",
				Optional:            true,
				Computed:            true,
			},
			"wifiman_enabled": schema.BoolAttribute{
				MarkdownDescription: "Enable WiFiman.",
				Optional:            true,
				Computed:            true,
			},
			"ssh_username": schema.StringAttribute{
				MarkdownDescription: "SSH username for device access.",
				Optional:            true,
				Computed:            true,
			},
			"ssh_password": schema.StringAttribute{
				MarkdownDescription: "SSH password for device access. Sensitive — the controller " +
					"stores only a hash, so this value is kept from configuration and not read back.",
				Optional:  true,
				Sensitive: true,
			},
			"ssh_auth_password_enabled": schema.BoolAttribute{
				MarkdownDescription: "Allow SSH password authentication (in addition to keys).",
				Optional:            true,
				Computed:            true,
			},
			"ssh_keys": schema.ListNestedAttribute{
				MarkdownDescription: "SSH keys.",
				Optional:            true,
				Computed:            true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"name": schema.StringAttribute{
							MarkdownDescription: "Name of SSH key.",
							Required:            true,
						},
						"type": schema.StringAttribute{
							MarkdownDescription: "Type of SSH key, e.g. ssh-rsa.",
							Required:            true,
						},
						"key": schema.StringAttribute{
							MarkdownDescription: "Public SSH key.",
							Optional:            true,
							Computed:            true,
						},
						"comment": schema.StringAttribute{
							MarkdownDescription: "Comment.",
							Optional:            true,
							Computed:            true,
						},
					},
				},
			},
		},
	}
}

// decode populates model.Mgmt from snap's "mgmt" section data. Every ssh_*
// leaf reads from its x_ssh_*-prefixed wire key; every other leaf is 1:1.
// The write-only secret leaf (SSHPassword) reads from priorModel.SSHPassword
// unconditionally — the controller never returns secret values, only a
// mask, so "x_ssh_password" in data is never inspected. ssh_keys is decoded
// through the generalized nested-object-list codec (decodeObjectList),
// whose per-element children (name/type/key/comment) are 1:1; the wire's
// extra date/fingerprint per element are unmodeled and not decoded.
func (s mgmtSection) decode(ctx context.Context, snap rawSettings, prior settingResourceModel, model *settingResourceModel) diag.Diagnostics {
	var diags diag.Diagnostics

	var priorModel settingMgmtModel
	if !prior.Mgmt.IsNull() && !prior.Mgmt.IsUnknown() {
		diags.Append(prior.Mgmt.As(ctx, &priorModel, basetypes.ObjectAsOptions{})...)
	}

	sec, _ := snap.section(s.key())
	data := sec.Data

	autoUpgrade, d := decodeBool(data, "auto_upgrade", priorModel.AutoUpgrade)
	diags.Append(d...)
	autoUpgradeHour, d := decodeInt64(data, "auto_upgrade_hour", priorModel.AutoUpgradeHour)
	diags.Append(d...)
	advancedFeatureEnabled, d := decodeBool(data, "advanced_feature_enabled", priorModel.AdvancedFeatureEnabled)
	diags.Append(d...)
	debugToolsEnabled, d := decodeBool(data, "debug_tools_enabled", priorModel.DebugToolsEnabled)
	diags.Append(d...)
	directConnectEnabled, d := decodeBool(data, "direct_connect_enabled", priorModel.DirectConnectEnabled)
	diags.Append(d...)
	unifiIdpEnabled, d := decodeBool(data, "unifi_idp_enabled", priorModel.UnifiIdpEnabled)
	diags.Append(d...)
	wifimanEnabled, d := decodeBool(data, "wifiman_enabled", priorModel.WifimanEnabled)
	diags.Append(d...)
	sshEnabled, d := decodeBool(data, "x_ssh_enabled", priorModel.SSHEnabled)
	diags.Append(d...)
	sshAuthPasswordEnabled, d := decodeBool(data, "x_ssh_auth_password_enabled", priorModel.SSHAuthPasswordEnabled)
	diags.Append(d...)
	sshUsername, d := decodeString(data, "x_ssh_username", priorModel.SSHUsername)
	diags.Append(d...)
	// ssh_password (wire x_ssh_password) is write-only: the controller never
	// returns it, so decode always preserves prior instead of reading data.
	sshPassword := priorModel.SSHPassword
	sshKeys, d := decodeObjectList(ctx, data, "x_ssh_keys", priorModel.SSHKeys, types.ObjectType{AttrTypes: mgmtSSHKeyAttrTypes})
	diags.Append(d...)
	if diags.HasError() {
		return diags
	}

	m := settingMgmtModel{
		AutoUpgrade:            autoUpgrade,
		AutoUpgradeHour:        autoUpgradeHour,
		SSHEnabled:             sshEnabled,
		SSHKeys:                sshKeys,
		AdvancedFeatureEnabled: advancedFeatureEnabled,
		DebugToolsEnabled:      debugToolsEnabled,
		DirectConnectEnabled:   directConnectEnabled,
		UnifiIdpEnabled:        unifiIdpEnabled,
		WifimanEnabled:         wifimanEnabled,
		SSHUsername:            sshUsername,
		SSHPassword:            sshPassword,
		SSHAuthPasswordEnabled: sshAuthPasswordEnabled,
	}

	obj, objDiags := types.ObjectValueFrom(ctx, mgmtAttrTypes, m)
	diags.Append(objDiags...)
	if diags.HasError() {
		return diags
	}

	model.Mgmt = obj
	return diags
}

// overlay computes the "mgmt" PUT body from model.Mgmt, starting from a deep
// copy of the snapshot's current section data so any unmodeled key already
// present on the controller (alert_enabled, boot_sound, led_enabled,
// outdoor_mode_enabled, x_ssh_bind_wildcard) is preserved (RMW). Every
// ssh_* leaf writes to its x_ssh_*-prefixed wire key. The secret leaf
// (x_ssh_password) deletes from base when the config value is null/unknown
// (never re-sends a masked value) and writes it when set, including an
// explicit empty string. ssh_keys is overlaid through the generalized
// nested-object-list codec (overlayObjectList), which now builds every
// output element fresh from the model's leaves (no positional carry from
// the base); this section then explicitly blanks each element's date/
// fingerprint (see the blankSSHKeyControllerMetadata call below). Returns
// configured == false (no write) when the section is not configured
// (null/unknown) in model.
func (s mgmtSection) overlay(ctx context.Context, model, prior settingResourceModel, snap rawSettings) (settings.RawSetting, bool, diag.Diagnostics) {
	var diags diag.Diagnostics

	if model.Mgmt.IsNull() || model.Mgmt.IsUnknown() {
		return settings.RawSetting{}, false, diags
	}

	var m settingMgmtModel
	diags.Append(model.Mgmt.As(ctx, &m, basetypes.ObjectAsOptions{})...)
	if diags.HasError() {
		return settings.RawSetting{}, false, diags
	}

	base := snap.dataCopy(s.key())
	overlayBool(base, "auto_upgrade", m.AutoUpgrade)
	overlayInt64(base, "auto_upgrade_hour", m.AutoUpgradeHour)
	overlayBool(base, "advanced_feature_enabled", m.AdvancedFeatureEnabled)
	overlayBool(base, "debug_tools_enabled", m.DebugToolsEnabled)
	overlayBool(base, "direct_connect_enabled", m.DirectConnectEnabled)
	overlayBool(base, "unifi_idp_enabled", m.UnifiIdpEnabled)
	overlayBool(base, "wifiman_enabled", m.WifimanEnabled)
	overlayBool(base, "x_ssh_enabled", m.SSHEnabled)
	overlayBool(base, "x_ssh_auth_password_enabled", m.SSHAuthPasswordEnabled)
	overlayString(base, "x_ssh_username", m.SSHUsername)
	if !m.SSHPassword.IsNull() && !m.SSHPassword.IsUnknown() {
		base["x_ssh_password"] = m.SSHPassword.ValueString()
	} else {
		delete(base, "x_ssh_password") // never replay a read-back mask
	}
	diags.Append(overlayObjectList(ctx, base, "x_ssh_keys", m.SSHKeys)...)
	if diags.HasError() {
		return settings.RawSetting{}, false, diags
	}
	// Only blank date/fingerprint when ssh_keys is actually being replaced
	// (configured and known) this apply. When ssh_keys is omitted
	// (null/unknown), overlayObjectList above already left base's
	// "x_ssh_keys" untouched — blanking unconditionally would still zero out
	// the existing keys' controller-assigned metadata even though nothing
	// about ssh_keys changed. Legacy preserved omitted ssh_keys verbatim,
	// including their metadata.
	if !m.SSHKeys.IsNull() && !m.SSHKeys.IsUnknown() {
		blankSSHKeyControllerMetadata(base)
	}

	rs := settings.RawSetting{
		BaseSetting: settings.BaseSetting{Key: s.key()},
		Data:        base,
	}
	return rs, true, diags
}

// blankSSHKeyControllerMetadata sets "date" and "fingerprint" to "" on every
// element of base["x_ssh_keys"], if present. Its caller (overlay) invokes it
// only when ssh_keys is actually configured (non-null/known) in the model —
// i.e. only when this apply is actually replacing the list — so an omitted
// ssh_keys block leaves the snapshot's existing elements, metadata included,
// untouched (legacy parity on omit).
//
// These two wire fields are controller-computed metadata (assigned when a
// key is added/rotated) that the schema does not model or echo back.
// overlayObjectList builds each element fresh from the model's leaves, so it
// never carries these fields forward from the base by list position —
// deliberately, because doing so by position mis-attaches one key's metadata
// onto a different key across a reorder or replace. Blanking here matches
// legacy byte-for-byte: the legacy client always sent freshly constructed
// SettingMgmtSSHKeys structs whose date/fingerprint serialize as "" (see
// goldenMgmt's "date":"","fingerprint":"").
func blankSSHKeyControllerMetadata(base map[string]any) {
	items, ok := base["x_ssh_keys"].([]any)
	if !ok {
		return
	}
	for _, item := range items {
		elem, ok := item.(map[string]any)
		if !ok {
			continue
		}
		elem["date"] = ""
		elem["fingerprint"] = ""
	}
}

// carryBestEffort copies the plan's mgmt value onto dst via
// carrySecretObject: this section holds a write-only secret leaf
// (ssh_password), so a straight plan copy would be wrong when best-effort
// state recovery needs to fall back to prior's secret for a null/unknown
// plan secret. carrySecretObject copies every other
// (non-secret) leaf from plan verbatim — including the ssh_keys list — and,
// for ssh_password specifically, keeps prior's value (read off dst, which
// bestEffortState seeds as prior) when plan's is null/unknown, and keeps
// plan's value (including an explicit empty string) when set.
func (mgmtSection) carryBestEffort(dst *settingResourceModel, plan settingResourceModel) diag.Diagnostics {
	obj, diags := carrySecretObject(plan.Mgmt, dst.Mgmt, "ssh_password")
	dst.Mgmt = obj
	return diags
}

func (mgmtSection) isConfigured(m settingResourceModel) bool {
	return !m.Mgmt.IsNull() && !m.Mgmt.IsUnknown()
}
