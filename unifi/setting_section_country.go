package unifi

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/objectplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
)

// countrySection is the settingSection implementation for the "country"
// settings section: a flat SingleNestedAttribute with a single
// managed scalar leaf, no nested objects/lists and no secrets.
type countrySection struct{}

func init() {
	registerSection(countrySection{})
}

func (countrySection) key() string      { return "country" }
func (countrySection) attrName() string { return "country" }

// schemaAttribute is byte-identical to the inline "country" block in
// setting_resource.go's schema (Task 24a enforces this with a
// schema-equivalence golden).
func (countrySection) schemaAttribute() schema.Attribute {
	return schema.SingleNestedAttribute{
		MarkdownDescription: "Regulatory country settings.",
		Optional:            true,
		Computed:            true,
		PlanModifiers: []planmodifier.Object{
			objectplanmodifier.UseStateForUnknown(),
		},
		Attributes: map[string]schema.Attribute{
			"code": schema.Int64Attribute{
				MarkdownDescription: "Regulatory country code (ISO 3166-1 numeric).",
				Required:            true,
			},
		},
	}
}

// decode populates model.Country from snap's "country" section data.
func (countrySection) decode(ctx context.Context, snap rawSettings, prior settingResourceModel, model *settingResourceModel) diag.Diagnostics {
	var diags diag.Diagnostics

	var priorModel settingCountryModel
	if !prior.Country.IsNull() && !prior.Country.IsUnknown() {
		diags.Append(prior.Country.As(ctx, &priorModel, basetypes.ObjectAsOptions{})...)
	}

	sec, _ := snap.section("country")
	data := sec.Data

	code, d := decodeInt64(data, "code", priorModel.Code)
	diags.Append(d...)
	if diags.HasError() {
		return diags
	}

	m := settingCountryModel{
		Code: code,
	}

	obj, objDiags := types.ObjectValueFrom(ctx, countryAttrTypes, m)
	diags.Append(objDiags...)
	if diags.HasError() {
		return diags
	}

	model.Country = obj
	return diags
}

// overlay computes the "country" PUT body from model.Country, starting
// from a deep copy of the snapshot's current section data so any unmodeled
// key already present on the controller is preserved. Returns configured
// == false (no write) when the section is not configured (null/unknown)
// in model.
func (countrySection) overlay(ctx context.Context, model, prior settingResourceModel, snap rawSettings) (settings.RawSetting, bool, diag.Diagnostics) {
	var diags diag.Diagnostics

	if model.Country.IsNull() || model.Country.IsUnknown() {
		return settings.RawSetting{}, false, diags
	}

	var m settingCountryModel
	diags.Append(model.Country.As(ctx, &m, basetypes.ObjectAsOptions{})...)
	if diags.HasError() {
		return settings.RawSetting{}, false, diags
	}

	base := snap.dataCopy("country")
	overlayInt64(base, "code", m.Code)

	rs := settings.RawSetting{
		BaseSetting: settings.BaseSetting{Key: "country"},
		Data:        base,
	}
	return rs, true, diags
}

// carryBestEffort copies the plan's country value onto dst. This section
// holds no secret leaves, so it is a straight copy with no per-leaf
// plan/prior choice needed.
func (countrySection) carryBestEffort(dst *settingResourceModel, plan settingResourceModel) diag.Diagnostics {
	dst.Country = plan.Country
	return nil
}

// isConfigured reports whether m.Country is set (non-null, non-unknown),
// gating whether Create/Update push this section to the controller at all.
func (countrySection) isConfigured(m settingResourceModel) bool {
	return !m.Country.IsNull() && !m.Country.IsUnknown()
}
