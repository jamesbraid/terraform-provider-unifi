package unifi

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/objectplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
)

// lcmSection is the settingSection implementation for the "lcm" settings
// section: a flat SingleNestedAttribute with only ownerManaged scalar
// leaves, no nested objects/lists and no secrets.
type lcmSection struct{}

func init() {
	registerSection(lcmSection{})
}

func (lcmSection) key() string      { return "lcm" }
func (lcmSection) attrName() string { return "lcm" }

// schemaAttribute is byte-identical to the inline "lcm" block in
// setting_resource.go's schema (Task 24a enforces this with a
// schema-equivalence golden).
func (lcmSection) schemaAttribute() schema.Attribute {
	return schema.SingleNestedAttribute{
		MarkdownDescription: "LCD/display (LCM) settings for devices with a screen.",
		Optional:            true,
		Computed:            true,
		PlanModifiers: []planmodifier.Object{
			objectplanmodifier.UseStateForUnknown(),
		},
		Attributes: map[string]schema.Attribute{
			"enabled": schema.BoolAttribute{
				MarkdownDescription: "Whether the device display is enabled.",
				Optional:            true,
				Computed:            true,
				Default:             booldefault.StaticBool(true),
			},
			"brightness": schema.Int64Attribute{
				MarkdownDescription: "Display brightness (1-100).",
				Optional:            true,
				Computed:            true,
				Validators:          []validator.Int64{int64validator.Between(1, 100)},
			},
			"idle_timeout": schema.Int64Attribute{
				MarkdownDescription: "Seconds of inactivity before the display turns off (10-3600).",
				Optional:            true,
				Computed:            true,
				Validators:          []validator.Int64{int64validator.Between(10, 3600)},
			},
			"sync": schema.BoolAttribute{
				MarkdownDescription: "Sync display settings across devices.",
				Optional:            true,
				Computed:            true,
				Default:             booldefault.StaticBool(false),
			},
			"touch_event": schema.BoolAttribute{
				MarkdownDescription: "Whether touch events on the display are enabled.",
				Optional:            true,
				Computed:            true,
				Default:             booldefault.StaticBool(true),
			},
		},
	}
}

func (lcmSection) ownership() map[string]ownershipClass {
	return map[string]ownershipClass{
		"enabled":      ownerManaged,
		"brightness":   ownerManaged,
		"idle_timeout": ownerManaged,
		"sync":         ownerManaged,
		"touch_event":  ownerManaged,
	}
}

// decode populates model.Lcm from snap's "lcm" section data, falling back
// to prior.Lcm's matching leaf for any field whose ownership class does
// not read from the API (none, here - all five leaves are ownerManaged).
func (lcmSection) decode(ctx context.Context, snap rawSettings, prior settingResourceModel, model *settingResourceModel) diag.Diagnostics {
	var diags diag.Diagnostics

	own := lcmSection{}.ownership()

	var priorModel settingLcmModel
	if !prior.Lcm.IsNull() && !prior.Lcm.IsUnknown() {
		diags.Append(prior.Lcm.As(ctx, &priorModel, basetypes.ObjectAsOptions{})...)
	}

	sec, _ := snap.section("lcm")
	data := sec.Data

	enabled, d := decodeBool(data, "enabled", own["enabled"], priorModel.Enabled)
	diags.Append(d...)
	brightness, d := decodeInt64(data, "brightness", own["brightness"], priorModel.Brightness)
	diags.Append(d...)
	idleTimeout, d := decodeInt64(data, "idle_timeout", own["idle_timeout"], priorModel.IdleTimeout)
	diags.Append(d...)
	sync, d := decodeBool(data, "sync", own["sync"], priorModel.Sync)
	diags.Append(d...)
	touchEvent, d := decodeBool(data, "touch_event", own["touch_event"], priorModel.TouchEvent)
	diags.Append(d...)
	if diags.HasError() {
		return diags
	}

	m := settingLcmModel{
		Enabled:     enabled,
		Brightness:  brightness,
		IdleTimeout: idleTimeout,
		Sync:        sync,
		TouchEvent:  touchEvent,
	}

	obj, objDiags := types.ObjectValueFrom(ctx, lcmAttrTypes, m)
	diags.Append(objDiags...)
	if diags.HasError() {
		return diags
	}

	model.Lcm = obj
	return diags
}

// overlay computes the "lcm" PUT body from model.Lcm, starting from a deep
// copy of the snapshot's current section data so any unmodeled key already
// present on the controller is preserved. Returns configured == false (no
// write) when the section is not configured (null/unknown) in model.
func (lcmSection) overlay(ctx context.Context, model, prior settingResourceModel, snap rawSettings) (settings.RawSetting, bool, diag.Diagnostics) {
	var diags diag.Diagnostics

	if model.Lcm.IsNull() || model.Lcm.IsUnknown() {
		return settings.RawSetting{}, false, diags
	}

	own := lcmSection{}.ownership()

	var m settingLcmModel
	diags.Append(model.Lcm.As(ctx, &m, basetypes.ObjectAsOptions{})...)
	if diags.HasError() {
		return settings.RawSetting{}, false, diags
	}

	base := snap.dataCopy("lcm")
	overlayBool(base, "enabled", own["enabled"], m.Enabled)
	overlayInt64(base, "brightness", own["brightness"], m.Brightness)
	overlayInt64(base, "idle_timeout", own["idle_timeout"], m.IdleTimeout)
	overlayBool(base, "sync", own["sync"], m.Sync)
	overlayBool(base, "touch_event", own["touch_event"], m.TouchEvent)

	rs := settings.RawSetting{
		BaseSetting: settings.BaseSetting{Key: "lcm"},
		Data:        base,
	}
	return rs, true, diags
}

func (lcmSection) capability(snap rawSettings) capabilityState {
	return sectionCapability(snap, "lcm")
}

// carryBestEffort copies the plan's lcm value onto dst. This section holds
// no secret leaves, so it is a straight copy with no per-leaf plan/prior
// choice needed.
func (lcmSection) carryBestEffort(dst *settingResourceModel, plan, prior settingResourceModel) diag.Diagnostics {
	dst.Lcm = plan.Lcm
	return nil
}
