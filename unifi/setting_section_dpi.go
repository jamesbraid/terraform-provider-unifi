package unifi

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/objectplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
)

// dpiSection is the settingSection implementation for the "dpi" settings
// section: a flat SingleNestedAttribute with only ownerManaged scalar
// leaves, no nested objects/lists and no secrets.
//
// Note: the FingerprintingEnabled leaf's raw controller wire key is
// "fingerprintingEnabled" (camelCase, per go-unifi's settings.Dpi json tag),
// while its schema/tfsdk leaf name is "fingerprinting_enabled" (snake_case).
// ownership() is keyed by schema leaf names; decode/overlay operate on the
// raw data map and must use the camelCase wire key.
type dpiSection struct{}

func init() {
	registerSection(dpiSection{})
}

func (dpiSection) key() string      { return "dpi" }
func (dpiSection) attrName() string { return "dpi" }

// schemaAttribute is byte-identical to the inline "dpi" block in
// setting_resource.go's schema (Task 24a enforces this with a
// schema-equivalence golden).
func (dpiSection) schemaAttribute() schema.Attribute {
	return schema.SingleNestedAttribute{
		MarkdownDescription: "Deep Packet Inspection (DPI) settings.",
		Optional:            true,
		Computed:            true,
		PlanModifiers: []planmodifier.Object{
			objectplanmodifier.UseStateForUnknown(),
		},
		Attributes: map[string]schema.Attribute{
			"enabled": schema.BoolAttribute{
				MarkdownDescription: "Whether DPI is enabled.",
				Optional:            true,
				Computed:            true,
				Default:             booldefault.StaticBool(false),
			},
			"fingerprinting_enabled": schema.BoolAttribute{
				MarkdownDescription: "Whether device fingerprinting is enabled.",
				Optional:            true,
				Computed:            true,
				Default:             booldefault.StaticBool(false),
			},
		},
	}
}

func (dpiSection) ownership() map[string]ownershipClass {
	return map[string]ownershipClass{
		"enabled":                ownerManaged,
		"fingerprinting_enabled": ownerManaged,
	}
}

// decode populates model.Dpi from snap's "dpi" section data, falling back to
// prior.Dpi's matching leaf for any field whose ownership class does not
// read from the API (none, here - both leaves are ownerManaged). The raw
// data map is keyed by go-unifi's json tags, so FingerprintingEnabled is
// read from "fingerprintingEnabled" (camelCase), not the schema leaf name.
func (dpiSection) decode(ctx context.Context, snap rawSettings, prior settingResourceModel, model *settingResourceModel) diag.Diagnostics {
	var diags diag.Diagnostics

	own := dpiSection{}.ownership()

	var priorModel settingDpiModel
	if !prior.Dpi.IsNull() && !prior.Dpi.IsUnknown() {
		diags.Append(prior.Dpi.As(ctx, &priorModel, basetypes.ObjectAsOptions{})...)
	}

	sec, _ := snap.section("dpi")
	data := sec.Data

	enabled, d := decodeBool(data, "enabled", own["enabled"], priorModel.Enabled)
	diags.Append(d...)
	fingerprintingEnabled, d := decodeBool(data, "fingerprintingEnabled", own["fingerprinting_enabled"], priorModel.FingerprintingEnabled)
	diags.Append(d...)
	if diags.HasError() {
		return diags
	}

	m := settingDpiModel{
		Enabled:               enabled,
		FingerprintingEnabled: fingerprintingEnabled,
	}

	obj, objDiags := types.ObjectValueFrom(ctx, dpiAttrTypes, m)
	diags.Append(objDiags...)
	if diags.HasError() {
		return diags
	}

	model.Dpi = obj
	return diags
}

// overlay computes the "dpi" PUT body from model.Dpi, starting from a deep
// copy of the snapshot's current section data so any unmodeled key already
// present on the controller is preserved. Returns configured == false (no
// write) when the section is not configured (null/unknown) in model. The
// FingerprintingEnabled leaf is written to the raw "fingerprintingEnabled"
// (camelCase) wire key, matching go-unifi's settings.Dpi json tag.
func (dpiSection) overlay(ctx context.Context, model, prior settingResourceModel, snap rawSettings) (settings.RawSetting, bool, diag.Diagnostics) {
	var diags diag.Diagnostics

	if model.Dpi.IsNull() || model.Dpi.IsUnknown() {
		return settings.RawSetting{}, false, diags
	}

	own := dpiSection{}.ownership()

	var m settingDpiModel
	diags.Append(model.Dpi.As(ctx, &m, basetypes.ObjectAsOptions{})...)
	if diags.HasError() {
		return settings.RawSetting{}, false, diags
	}

	base := snap.dataCopy("dpi")
	overlayBool(base, "enabled", own["enabled"], m.Enabled)
	overlayBool(base, "fingerprintingEnabled", own["fingerprinting_enabled"], m.FingerprintingEnabled)

	rs := settings.RawSetting{
		BaseSetting: settings.BaseSetting{Key: "dpi"},
		Data:        base,
	}
	return rs, true, diags
}

func (dpiSection) capability(snap rawSettings) capabilityState {
	return sectionCapability(snap, "dpi")
}

// carryBestEffort copies the plan's dpi value onto dst. This section holds
// no secret leaves, so it is a straight copy with no per-leaf plan/prior
// choice needed.
func (dpiSection) carryBestEffort(dst *settingResourceModel, plan, prior settingResourceModel) diag.Diagnostics {
	dst.Dpi = plan.Dpi
	return nil
}
