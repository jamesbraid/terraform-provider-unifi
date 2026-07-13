package unifi

import (
	"context"
	"regexp"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
)

// settingSnmpModel is the Terraform model for the "snmp" section.
type settingSnmpModel struct {
	Enabled   types.Bool   `tfsdk:"enabled"`
	Community types.String `tfsdk:"community"`
	EnabledV3 types.Bool   `tfsdk:"enabled_v3"`
	Username  types.String `tfsdk:"username"`
	Password  types.String `tfsdk:"password"`
}

// snmpAttrTypes is the object attribute-type map for settingSnmpModel. There
// is no pre-existing package-level var for this section — following
// radiusAttrTypes' placement (setting_section_radius.go), it lives here
// rather than in setting_resource.go.
var snmpAttrTypes = map[string]attr.Type{
	"enabled":    types.BoolType,
	"community":  types.StringType,
	"enabled_v3": types.BoolType,
	"username":   types.StringType,
	"password":   types.StringType,
}

// snmpSection is the settingSection implementation for the "snmp" settings
// section: SNMP monitoring configuration with TWO write-only secret leaves
// in the same section -- community (SNMPv2 community string, wire key
// "community") and password (SNMPv3 auth passphrase, wire key "x_password").
// Both follow the same handling as radius's secret/x_secret and mgmt's
// ssh_password/x_ssh_password: decode always preserves prior state for these
// leaves rather than reading the controller's masked wire value, and overlay
// deletes the wire key from the PUT body when the config value is
// null/unknown rather than ever re-sending a masked value; a configured
// value is written verbatim. Unlike radius/mgmt, snmp has NO
// empty-clear/rotate-to-empty contract -- community's validator
// (LengthBetween(1, 256)) and password's (LengthBetween(8, 32)) both reject
// "", so an explicit empty string can never reach overlay for either leaf.
type snmpSection struct{}

func init() {
	registerSection(snmpSection{})
}

func (snmpSection) key() string      { return "snmp" }
func (snmpSection) attrName() string { return "snmp" }

// schemaAttribute defines the "snmp" nested block. community and password
// are Optional + Computed + Sensitive, matching radius.secret's schema shape
// exactly (not mgmt.ssh_password's Optional+Sensitive-only shape): both are
// children of an Optional+Computed parent (snmp {} itself), and decode()
// always sets them from the prior model on every refresh, so Computed is
// required for the nesting to resolve correctly under the parent block.
func (snmpSection) schemaAttribute() schema.Attribute {
	return schema.SingleNestedAttribute{
		MarkdownDescription: "SNMP settings.",
		Optional:            true,
		Computed:            true,
		Attributes: map[string]schema.Attribute{
			"enabled": schema.BoolAttribute{
				MarkdownDescription: "Enable SNMP.",
				Optional:            true,
				Computed:            true,
			},
			"community": schema.StringAttribute{
				MarkdownDescription: "SNMPv2 community string. Sensitive — " +
					"the controller never returns this value; state " +
					"preserves the last-configured value and omits it from " +
					"the write payload when unset in configuration.",
				Optional:  true,
				Computed:  true,
				Sensitive: true,
				Validators: []validator.String{
					stringvalidator.LengthBetween(1, 256),
					stringvalidator.RegexMatches(
						regexp.MustCompile(`\A[^\n]{1,256}\z`),
						"must be 1-256 characters and must not contain newlines",
					),
				},
			},
			"enabled_v3": schema.BoolAttribute{
				MarkdownDescription: "Enable SNMPv3.",
				Optional:            true,
				Computed:            true,
			},
			"username": schema.StringAttribute{
				MarkdownDescription: "SNMPv3 username.",
				Optional:            true,
				Computed:            true,
				Validators: []validator.String{
					stringvalidator.LengthBetween(1, 30),
					stringvalidator.RegexMatches(
						regexp.MustCompile(`^[a-zA-Z0-9_-]+$`),
						"must contain only letters, numbers, underscores, and hyphens",
					),
				},
			},
			"password": schema.StringAttribute{
				MarkdownDescription: "SNMPv3 passphrase. Sensitive — the " +
					"controller never returns this value; state preserves " +
					"the last-configured value and omits it from the write " +
					"payload when unset in configuration.",
				Optional:  true,
				Computed:  true,
				Sensitive: true,
				Validators: []validator.String{
					stringvalidator.LengthBetween(8, 32),
					stringvalidator.RegexMatches(
						regexp.MustCompile(`^[^'"]+$`),
						"must not contain single or double quotes",
					),
				},
			},
		},
	}
}

// decode populates model.Snmp from snap's "snmp" section data. Both secret
// leaves (community, password) read from priorModel unconditionally -- the
// controller never returns secret values (only a mask), so "community" and
// "x_password" in data are never inspected.
func (s snmpSection) decode(ctx context.Context, snap rawSettings, prior settingResourceModel, model *settingResourceModel) diag.Diagnostics {
	var diags diag.Diagnostics

	var priorModel settingSnmpModel
	if !prior.Snmp.IsNull() && !prior.Snmp.IsUnknown() {
		diags.Append(prior.Snmp.As(ctx, &priorModel, basetypes.ObjectAsOptions{})...)
	}

	sec, _ := snap.section(s.key())
	data := sec.Data

	enabled, d := decodeBool(data, "enabled", priorModel.Enabled)
	diags.Append(d...)
	enabledV3, d := decodeBool(data, "enabledV3", priorModel.EnabledV3)
	diags.Append(d...)
	username, d := decodeString(data, "username", priorModel.Username)
	diags.Append(d...)
	// community (wire "community") and password (wire "x_password") are
	// write-only: the controller never returns them, so decode always
	// preserves prior instead of reading data.
	community := priorModel.Community
	password := priorModel.Password
	if diags.HasError() {
		return diags
	}

	m := settingSnmpModel{
		Enabled:   enabled,
		Community: community,
		EnabledV3: enabledV3,
		Username:  username,
		Password:  password,
	}

	obj, objDiags := types.ObjectValueFrom(ctx, snmpAttrTypes, m)
	diags.Append(objDiags...)
	if diags.HasError() {
		return diags
	}

	model.Snmp = obj
	return diags
}

// overlay computes the "snmp" PUT body from model.Snmp, starting from a deep
// copy of the snapshot's current section data (RMW) even though no specific
// unmodeled field is currently known for this section (defensive
// preservation, matching every other section's standing template). Each
// secret leaf writes to its own wire key inline: a null/unknown config value
// deletes the wire key from base (never re-sends a masked value); a
// configured value is written verbatim. Returns configured == false (no
// write) when the section is not configured (null/unknown) in model.
func (s snmpSection) overlay(ctx context.Context, model, prior settingResourceModel, snap rawSettings) (settings.RawSetting, bool, diag.Diagnostics) {
	var diags diag.Diagnostics

	if model.Snmp.IsNull() || model.Snmp.IsUnknown() {
		return settings.RawSetting{}, false, diags
	}

	var m settingSnmpModel
	diags.Append(model.Snmp.As(ctx, &m, basetypes.ObjectAsOptions{})...)
	if diags.HasError() {
		return settings.RawSetting{}, false, diags
	}

	base := snap.dataCopy(s.key())
	overlayBool(base, "enabled", m.Enabled)
	overlayBool(base, "enabledV3", m.EnabledV3)
	overlayString(base, "username", m.Username)
	if !m.Community.IsNull() && !m.Community.IsUnknown() {
		base["community"] = m.Community.ValueString()
	} else {
		delete(base, "community") // never replay a read-back mask
	}
	if !m.Password.IsNull() && !m.Password.IsUnknown() {
		base["x_password"] = m.Password.ValueString()
	} else {
		delete(base, "x_password") // never replay a read-back mask
	}
	if diags.HasError() {
		return settings.RawSetting{}, false, diags
	}

	rs := settings.RawSetting{
		BaseSetting: settings.BaseSetting{Key: s.key()},
		Data:        base,
	}
	return rs, true, diags
}

// carryBestEffort copies the plan's snmp value onto dst via TWO chained
// carrySecretObject calls, one per secret leaf (community, then password),
// each passing the ORIGINAL prior object (dst.Snmp, before this function
// mutates it) as the 2nd argument -- threading the first call's returned
// object through as the SECOND call's plan argument (not its prior
// argument) is required for the second call to still see the first call's
// resolved community leaf rather than clobber it. carrySecretObject's
// per-leaf logic only ever touches its named secretLeaf and passes every
// other attribute through from plan untouched, so chaining two calls with
// the same original prior correctly resolves both secrets independently.
func (snmpSection) carryBestEffort(dst *settingResourceModel, plan settingResourceModel) diag.Diagnostics {
	var diags diag.Diagnostics
	priorObj := dst.Snmp
	out, d := carrySecretObject(plan.Snmp, priorObj, "community")
	diags.Append(d...)
	out, d = carrySecretObject(out, priorObj, "password")
	diags.Append(d...)
	dst.Snmp = out
	return diags
}

// isConfigured reports whether m.Snmp is set (non-null, non-unknown),
// gating whether Create/Update push this section -- including both
// write-only secret leaves -- to the controller at all.
func (snmpSection) isConfigured(m settingResourceModel) bool {
	return !m.Snmp.IsNull() && !m.Snmp.IsUnknown()
}
