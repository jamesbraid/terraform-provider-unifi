package unifi

import (
	"context"
	"regexp"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-timetypes/timetypes"
	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
)

// radiusAttrTypes is the object attribute-type map for settingRadiusModel.
// There is no pre-existing package-level var for this section (unlike
// earlier sections): this map matches the inline one built in
// setting_resource.go's Update path (radiusModelToSetting/radiusSettingToModel
// callers).
var radiusAttrTypes = map[string]attr.Type{
	"accounting_enabled":      types.BoolType,
	"acct_port":               types.Int64Type,
	"auth_port":               types.Int64Type,
	"interim_update_interval": timetypes.GoDurationType{},
	"secret":                  types.StringType,
}

// radiusSection is the settingSection implementation for the "radius"
// settings section. It combines several features seen individually in
// earlier sections plus one new one: a GoDuration leaf
// (interim_update_interval, Task 19b codec), read-modify-write preservation
// of unmodeled controller fields (configure_whole_network, tunneled_reply,
// enabled — like igmp_snooping), and a write-only secret leaf (secret): the
// model's tfsdk name is "secret" but the controller's wire key for it is
// "x_secret". decode reads secret inline from priorModel.Secret — never from
// the API, since the controller never returns secret values, only a mask —
// and overlay deletes x_secret from the PUT body entirely when the config
// value is null/unknown rather than ever re-sending a masked value; a
// configured value (including an explicit empty string) is written
// verbatim.
type radiusSection struct{}

func init() {
	registerSection(radiusSection{})
}

func (radiusSection) key() string      { return "radius" }
func (radiusSection) attrName() string { return "radius" }

// schemaAttribute is byte-identical to the inline "radius" block in
// setting_resource.go's schema (setting_resource.go:970-1017): the parent
// SingleNestedAttribute is Optional+Computed with NO PlanModifiers.
func (radiusSection) schemaAttribute() schema.Attribute {
	return schema.SingleNestedAttribute{
		MarkdownDescription: "RADIUS settings.",
		Optional:            true,
		Computed:            true,
		Attributes: map[string]schema.Attribute{
			"accounting_enabled": schema.BoolAttribute{
				MarkdownDescription: "Enable RADIUS accounting.",
				Optional:            true,
				Computed:            true,
			},
			"acct_port": schema.Int64Attribute{
				MarkdownDescription: "RADIUS accounting port.",
				Optional:            true,
				Computed:            true,
				Validators: []validator.Int64{
					int64validator.Between(1, 65535),
				},
			},
			"auth_port": schema.Int64Attribute{
				MarkdownDescription: "RADIUS authentication port.",
				Optional:            true,
				Computed:            true,
				Validators: []validator.Int64{
					int64validator.Between(1, 65535),
				},
			},
			"interim_update_interval": schema.StringAttribute{
				MarkdownDescription: "Interim update interval, as a Go duration string " +
					"(e.g. `1h`, `3600s`).",
				CustomType: timetypes.GoDurationType{},
				Optional:   true,
				Computed:   true,
			},
			"secret": schema.StringAttribute{
				MarkdownDescription: "RADIUS shared secret.",
				Optional:            true,
				Computed:            true,
				Sensitive:           true,
				Validators: []validator.String{
					stringvalidator.LengthBetween(1, 48),
					stringvalidator.RegexMatches(
						regexp.MustCompile(`^[^\\\ "']+$`),
						"must not contain backslashes, spaces, single quotes, or double quotes",
					),
				},
			},
		},
	}
}

// decode populates model.Radius from snap's "radius" section data. The
// secret leaf reads from priorModel.Secret unconditionally — the controller
// never returns secret values (only a mask), so "x_secret" in data is never
// inspected.
//
// TODO(go-unifi): "x_secret" is read/written as a raw map key rather than
// through settings.Radius.Secret (go-unifi already tags it
// `json:"x_secret,omitempty"` correctly). PERMANENT: the "x_" prefix is the
// controller's own wire naming, not a go-unifi modeling gap, and this
// section's raw map access is required regardless (see dataCopy's TODO in
// setting_snapshot.go) for read-modify-write over unmodeled fields.
func (s radiusSection) decode(ctx context.Context, snap rawSettings, prior settingResourceModel, model *settingResourceModel) diag.Diagnostics {
	var diags diag.Diagnostics

	var priorModel settingRadiusModel
	if !prior.Radius.IsNull() && !prior.Radius.IsUnknown() {
		diags.Append(prior.Radius.As(ctx, &priorModel, basetypes.ObjectAsOptions{})...)
	}

	sec, _ := snap.section(s.key())
	data := sec.Data

	accountingEnabled, d := decodeBool(data, "accounting_enabled", priorModel.AccountingEnabled)
	diags.Append(d...)
	acctPort, d := decodeInt64(data, "acct_port", priorModel.AcctPort)
	diags.Append(d...)
	authPort, d := decodeInt64(data, "auth_port", priorModel.AuthPort)
	diags.Append(d...)
	interimUpdateInterval, d := decodeGoDuration(data, "interim_update_interval", priorModel.InterimUpdateInterval, time.Second)
	diags.Append(d...)
	// secret (wire x_secret) is write-only: the controller never returns it,
	// so decode always preserves prior instead of reading data.
	secret := priorModel.Secret
	if diags.HasError() {
		return diags
	}

	m := settingRadiusModel{
		AccountingEnabled:     accountingEnabled,
		AcctPort:              acctPort,
		AuthPort:              authPort,
		InterimUpdateInterval: interimUpdateInterval,
		Secret:                secret,
	}

	obj, objDiags := types.ObjectValueFrom(ctx, radiusAttrTypes, m)
	diags.Append(objDiags...)
	if diags.HasError() {
		return diags
	}

	model.Radius = obj
	return diags
}

// overlay computes the "radius" PUT body from model.Radius, starting from a
// deep copy of the snapshot's current section data so any unmodeled key
// already present on the controller (configure_whole_network,
// tunneled_reply, enabled) is preserved (RMW). The secret leaf writes to
// wire key "x_secret" inline: a null/unknown config value deletes x_secret
// from base (never re-sends a masked value); a configured value, including
// an explicit empty string, is written verbatim. Returns configured ==
// false (no write) when the section is not configured (null/unknown) in
// model.
func (s radiusSection) overlay(ctx context.Context, model, prior settingResourceModel, snap rawSettings) (settings.RawSetting, bool, diag.Diagnostics) {
	var diags diag.Diagnostics

	if model.Radius.IsNull() || model.Radius.IsUnknown() {
		return settings.RawSetting{}, false, diags
	}

	var m settingRadiusModel
	diags.Append(model.Radius.As(ctx, &m, basetypes.ObjectAsOptions{})...)
	if diags.HasError() {
		return settings.RawSetting{}, false, diags
	}

	base := snap.dataCopy(s.key())
	overlayBool(base, "accounting_enabled", m.AccountingEnabled)
	overlayInt64(base, "acct_port", m.AcctPort)
	overlayInt64(base, "auth_port", m.AuthPort)
	overlayGoDuration(base, "interim_update_interval", m.InterimUpdateInterval, time.Second)
	if !m.Secret.IsNull() && !m.Secret.IsUnknown() {
		base["x_secret"] = m.Secret.ValueString()
	} else {
		delete(base, "x_secret") // never replay a read-back mask
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

// carryBestEffort copies the plan's radius value onto dst via
// carrySecretObject: this section holds a write-only secret leaf (secret),
// so a straight plan copy would be wrong when a C2.4 second-failure recovery
// needs to fall back to prior's secret for a null/unknown plan secret.
// carrySecretObject copies every other (non-secret) leaf from plan verbatim
// and, for secret specifically, keeps prior's value (read off dst, which
// bestEffortState seeds as prior) when plan's is null/unknown, and keeps
// plan's value (including an explicit empty string) when set.
func (radiusSection) carryBestEffort(dst *settingResourceModel, plan settingResourceModel) diag.Diagnostics {
	obj, diags := carrySecretObject(plan.Radius, dst.Radius, "secret")
	dst.Radius = obj
	return diags
}

func (radiusSection) isConfigured(m settingResourceModel) bool {
	return !m.Radius.IsNull() && !m.Radius.IsUnknown()
}
