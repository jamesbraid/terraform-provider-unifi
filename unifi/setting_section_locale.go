package unifi

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
)

// localeSection is the settingSection implementation for the "locale"
// settings section: a flat SingleNestedAttribute with a single managed
// scalar leaf (timezone), no nested objects/lists and no secrets.
type localeSection struct{}

func init() {
	registerSection(localeSection{})
}

func (localeSection) key() string      { return "locale" }
func (localeSection) attrName() string { return "locale" }

// schemaAttribute defines the "locale" block.
func (localeSection) schemaAttribute() schema.Attribute {
	return schema.SingleNestedAttribute{
		MarkdownDescription: "Site locale settings.",
		Optional:            true,
		Computed:            true,
		Attributes: map[string]schema.Attribute{
			"timezone": schema.StringAttribute{
				MarkdownDescription: "The site's configured timezone (IANA timezone name, e.g. \"Etc/UTC\").",
				Optional:            true,
				Computed:            true,
			},
		},
	}
}

// decode populates model.Locale from snap's "locale" section data.
func (s localeSection) decode(ctx context.Context, snap rawSettings, prior settingResourceModel, model *settingResourceModel) diag.Diagnostics {
	var diags diag.Diagnostics

	var priorModel settingLocaleModel
	if !prior.Locale.IsNull() && !prior.Locale.IsUnknown() {
		diags.Append(prior.Locale.As(ctx, &priorModel, basetypes.ObjectAsOptions{})...)
	}

	sec, _ := snap.section(s.key())
	data := sec.Data

	timezone, d := decodeString(data, "timezone", priorModel.Timezone)
	diags.Append(d...)
	if diags.HasError() {
		return diags
	}

	m := settingLocaleModel{Timezone: timezone}

	obj, objDiags := types.ObjectValueFrom(ctx, localeAttrTypes, m)
	diags.Append(objDiags...)
	if diags.HasError() {
		return diags
	}

	model.Locale = obj
	return diags
}

// overlay computes the "locale" PUT body from model.Locale, starting from
// a deep copy of the snapshot's current section data so any unmodeled key
// already present on the controller is preserved. Returns configured ==
// false (no write) when the section is not configured (null/unknown) in
// model.
func (s localeSection) overlay(ctx context.Context, model, prior settingResourceModel, snap rawSettings) (settings.RawSetting, bool, diag.Diagnostics) {
	var diags diag.Diagnostics

	if model.Locale.IsNull() || model.Locale.IsUnknown() {
		return settings.RawSetting{}, false, diags
	}

	var m settingLocaleModel
	diags.Append(model.Locale.As(ctx, &m, basetypes.ObjectAsOptions{})...)
	if diags.HasError() {
		return settings.RawSetting{}, false, diags
	}

	base := snap.dataCopy(s.key())
	overlayString(base, "timezone", m.Timezone)

	rs := settings.RawSetting{
		BaseSetting: settings.BaseSetting{Key: s.key()},
		Data:        base,
	}
	return rs, true, diags
}

// carryBestEffort copies the plan's locale value onto dst. This section
// holds no secret leaves, so it is a straight copy.
func (localeSection) carryBestEffort(dst *settingResourceModel, plan settingResourceModel) diag.Diagnostics {
	dst.Locale = plan.Locale
	return nil
}

// isConfigured reports whether m.Locale is set (non-null, non-unknown).
func (localeSection) isConfigured(m settingResourceModel) bool {
	return !m.Locale.IsNull() && !m.Locale.IsUnknown()
}
