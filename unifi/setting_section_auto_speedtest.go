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

// autoSpeedtestSection is the settingSection implementation for the
// "auto_speedtest" settings section. It is the scalar template that Tasks
// 11-15 copy: a flat SingleNestedAttribute with only ownerManaged scalar
// leaves, no nested objects/lists and no secrets.
type autoSpeedtestSection struct{}

func init() {
	registerSection(autoSpeedtestSection{})
}

func (autoSpeedtestSection) key() string      { return "auto_speedtest" }
func (autoSpeedtestSection) attrName() string { return "auto_speedtest" }

// schemaAttribute is byte-identical to the inline "auto_speedtest" block in
// setting_resource.go's schema (Task 24a enforces this with a
// schema-equivalence golden).
func (autoSpeedtestSection) schemaAttribute() schema.Attribute {
	return schema.SingleNestedAttribute{
		MarkdownDescription: "Periodic automated internet speed test settings.",
		Optional:            true,
		Computed:            true,
		PlanModifiers: []planmodifier.Object{
			objectplanmodifier.UseStateForUnknown(),
		},
		Attributes: map[string]schema.Attribute{
			"enabled": schema.BoolAttribute{
				MarkdownDescription: "Whether periodic automated speed tests are enabled.",
				Optional:            true,
				Computed:            true,
				Default:             booldefault.StaticBool(false),
			},
			"cron_expr": schema.StringAttribute{
				MarkdownDescription: "Cron expression controlling when the speed test runs (e.g. `0 * * * *`).",
				Optional:            true,
				Computed:            true,
			},
		},
	}
}

func (autoSpeedtestSection) ownership() map[string]ownershipClass {
	return map[string]ownershipClass{
		"enabled":   ownerManaged,
		"cron_expr": ownerManaged,
	}
}

// decode populates model.AutoSpeedtest from snap's "auto_speedtest" section
// data, falling back to prior.AutoSpeedtest's matching leaf for any field
// whose ownership class does not read from the API (none, here - both
// leaves are ownerManaged).
func (autoSpeedtestSection) decode(ctx context.Context, snap rawSettings, prior settingResourceModel, model *settingResourceModel) diag.Diagnostics {
	var diags diag.Diagnostics

	own := autoSpeedtestSection{}.ownership()

	var priorModel settingAutoSpeedtestModel
	if !prior.AutoSpeedtest.IsNull() && !prior.AutoSpeedtest.IsUnknown() {
		diags.Append(prior.AutoSpeedtest.As(ctx, &priorModel, basetypes.ObjectAsOptions{})...)
	}

	sec, _ := snap.section("auto_speedtest")
	data := sec.Data

	enabled, d := decodeBool(data, "enabled", own["enabled"], priorModel.Enabled)
	diags.Append(d...)
	cronExpr, d := decodeString(data, "cron_expr", own["cron_expr"], priorModel.CronExpr)
	diags.Append(d...)
	if diags.HasError() {
		return diags
	}

	m := settingAutoSpeedtestModel{
		Enabled:  enabled,
		CronExpr: cronExpr,
	}

	obj, objDiags := types.ObjectValueFrom(ctx, autoSpeedtestAttrTypes, m)
	diags.Append(objDiags...)
	if diags.HasError() {
		return diags
	}

	model.AutoSpeedtest = obj
	return diags
}

// overlay computes the "auto_speedtest" PUT body from model.AutoSpeedtest,
// starting from a deep copy of the snapshot's current section data so any
// unmodeled key already present on the controller is preserved. Returns
// configured == false (no write) when the section is not configured
// (null/unknown) in model.
func (autoSpeedtestSection) overlay(ctx context.Context, model, prior settingResourceModel, snap rawSettings) (settings.RawSetting, bool, diag.Diagnostics) {
	var diags diag.Diagnostics

	if model.AutoSpeedtest.IsNull() || model.AutoSpeedtest.IsUnknown() {
		return settings.RawSetting{}, false, diags
	}

	own := autoSpeedtestSection{}.ownership()

	var m settingAutoSpeedtestModel
	diags.Append(model.AutoSpeedtest.As(ctx, &m, basetypes.ObjectAsOptions{})...)
	if diags.HasError() {
		return settings.RawSetting{}, false, diags
	}

	base := snap.dataCopy("auto_speedtest")
	overlayBool(base, "enabled", own["enabled"], m.Enabled)
	overlayString(base, "cron_expr", own["cron_expr"], m.CronExpr)

	rs := settings.RawSetting{
		BaseSetting: settings.BaseSetting{Key: "auto_speedtest"},
		Data:        base,
	}
	return rs, true, diags
}

func (autoSpeedtestSection) capability(snap rawSettings) capabilityState {
	return sectionCapability(snap, "auto_speedtest")
}

// carryBestEffort copies the plan's auto_speedtest value onto dst. This
// section holds no secret leaves, so it is a straight copy with no
// per-leaf plan/prior choice needed.
func (autoSpeedtestSection) carryBestEffort(dst *settingResourceModel, plan, prior settingResourceModel) diag.Diagnostics {
	dst.AutoSpeedtest = plan.AutoSpeedtest
	return nil
}

func (autoSpeedtestSection) isConfigured(m settingResourceModel) bool {
	return !m.AutoSpeedtest.IsNull() && !m.AutoSpeedtest.IsUnknown()
}
