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

// networkOptimizationSection is the settingSection implementation for the
// "network_optimization" settings section: a flat SingleNestedAttribute
// with a single ownerManaged scalar leaf, no nested objects/lists and no
// secrets.
type networkOptimizationSection struct{}

func init() {
	registerSection(networkOptimizationSection{})
}

func (networkOptimizationSection) key() string      { return "network_optimization" }
func (networkOptimizationSection) attrName() string { return "network_optimization" }

// schemaAttribute is byte-identical to the inline "network_optimization"
// block in setting_resource.go's schema (Task 24a enforces this with a
// schema-equivalence golden).
func (networkOptimizationSection) schemaAttribute() schema.Attribute {
	return schema.SingleNestedAttribute{
		MarkdownDescription: "Automated network optimization settings.",
		Optional:            true,
		Computed:            true,
		PlanModifiers: []planmodifier.Object{
			objectplanmodifier.UseStateForUnknown(),
		},
		Attributes: map[string]schema.Attribute{
			"enabled": schema.BoolAttribute{
				MarkdownDescription: "Whether automated network optimization is enabled.",
				Optional:            true,
				Computed:            true,
				Default:             booldefault.StaticBool(false),
			},
		},
	}
}

func (networkOptimizationSection) ownership() map[string]ownershipClass {
	return map[string]ownershipClass{
		"enabled": ownerManaged,
	}
}

// decode populates model.NetworkOpt from snap's "network_optimization"
// section data, falling back to prior.NetworkOpt's matching leaf for any
// field whose ownership class does not read from the API (none, here - the
// only leaf is ownerManaged).
func (networkOptimizationSection) decode(ctx context.Context, snap rawSettings, prior settingResourceModel, model *settingResourceModel) diag.Diagnostics {
	var diags diag.Diagnostics

	own := networkOptimizationSection{}.ownership()

	var priorModel settingNetworkOptimizationModel
	if !prior.NetworkOpt.IsNull() && !prior.NetworkOpt.IsUnknown() {
		diags.Append(prior.NetworkOpt.As(ctx, &priorModel, basetypes.ObjectAsOptions{})...)
	}

	sec, _ := snap.section("network_optimization")
	data := sec.Data

	enabled, d := decodeBool(data, "enabled", own["enabled"], priorModel.Enabled)
	diags.Append(d...)
	if diags.HasError() {
		return diags
	}

	m := settingNetworkOptimizationModel{
		Enabled: enabled,
	}

	obj, objDiags := types.ObjectValueFrom(ctx, networkOptimizationAttrTypes, m)
	diags.Append(objDiags...)
	if diags.HasError() {
		return diags
	}

	model.NetworkOpt = obj
	return diags
}

// overlay computes the "network_optimization" PUT body from
// model.NetworkOpt, starting from a deep copy of the snapshot's current
// section data so any unmodeled key already present on the controller is
// preserved. Returns configured == false (no write) when the section is
// not configured (null/unknown) in model.
func (networkOptimizationSection) overlay(ctx context.Context, model, prior settingResourceModel, snap rawSettings) (settings.RawSetting, bool, diag.Diagnostics) {
	var diags diag.Diagnostics

	if model.NetworkOpt.IsNull() || model.NetworkOpt.IsUnknown() {
		return settings.RawSetting{}, false, diags
	}

	own := networkOptimizationSection{}.ownership()

	var m settingNetworkOptimizationModel
	diags.Append(model.NetworkOpt.As(ctx, &m, basetypes.ObjectAsOptions{})...)
	if diags.HasError() {
		return settings.RawSetting{}, false, diags
	}

	base := snap.dataCopy("network_optimization")
	overlayBool(base, "enabled", own["enabled"], m.Enabled)

	rs := settings.RawSetting{
		BaseSetting: settings.BaseSetting{Key: "network_optimization"},
		Data:        base,
	}
	return rs, true, diags
}

func (networkOptimizationSection) capability(snap rawSettings) capabilityState {
	return sectionCapability(snap, "network_optimization")
}

// carryBestEffort copies the plan's network_optimization value onto dst.
// This section holds no secret leaves, so it is a straight copy with no
// per-leaf plan/prior choice needed.
func (networkOptimizationSection) carryBestEffort(dst *settingResourceModel, plan, prior settingResourceModel) diag.Diagnostics {
	dst.NetworkOpt = plan.NetworkOpt
	return nil
}

func (networkOptimizationSection) isConfigured(m settingResourceModel) bool {
	return !m.NetworkOpt.IsNull() && !m.NetworkOpt.IsUnknown()
}
