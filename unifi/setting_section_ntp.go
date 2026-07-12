package unifi

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/objectplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
)

// ntpSection is the settingSection implementation for the "ntp" settings
// section: a flat SingleNestedAttribute with five managed scalar string
// leaves, no nested objects/lists and no secrets.
type ntpSection struct{}

func init() {
	registerSection(ntpSection{})
}

func (ntpSection) key() string      { return "ntp" }
func (ntpSection) attrName() string { return "ntp" }

// schemaAttribute is byte-identical to the inline "ntp" block in
// setting_resource.go's schema (Task 24a enforces this with a
// schema-equivalence golden).
func (ntpSection) schemaAttribute() schema.Attribute {
	return schema.SingleNestedAttribute{
		MarkdownDescription: "NTP (time server) settings.",
		Optional:            true,
		Computed:            true,
		PlanModifiers: []planmodifier.Object{
			objectplanmodifier.UseStateForUnknown(),
		},
		Attributes: map[string]schema.Attribute{
			"setting_preference": schema.StringAttribute{
				MarkdownDescription: "Configuration mode: `auto` or `manual`.",
				Optional:            true,
				Computed:            true,
				Validators: []validator.String{
					stringvalidator.OneOf("auto", "manual"),
				},
			},
			"ntp_server_1": schema.StringAttribute{
				MarkdownDescription: "Primary NTP server.",
				Optional:            true,
				Computed:            true,
			},
			"ntp_server_2": schema.StringAttribute{
				MarkdownDescription: "Second NTP server.",
				Optional:            true,
				Computed:            true,
			},
			"ntp_server_3": schema.StringAttribute{
				MarkdownDescription: "Third NTP server.",
				Optional:            true,
				Computed:            true,
			},
			"ntp_server_4": schema.StringAttribute{
				MarkdownDescription: "Fourth NTP server.",
				Optional:            true,
				Computed:            true,
			},
		},
	}
}

// decode populates model.Ntp from snap's "ntp" section data.
func (ntpSection) decode(ctx context.Context, snap rawSettings, prior settingResourceModel, model *settingResourceModel) diag.Diagnostics {
	var diags diag.Diagnostics

	var priorModel settingNtpModel
	if !prior.Ntp.IsNull() && !prior.Ntp.IsUnknown() {
		diags.Append(prior.Ntp.As(ctx, &priorModel, basetypes.ObjectAsOptions{})...)
	}

	sec, _ := snap.section("ntp")
	data := sec.Data

	settingPreference, d := decodeString(data, "setting_preference", priorModel.SettingPreference)
	diags.Append(d...)
	ntpServer1, d := decodeString(data, "ntp_server_1", priorModel.NtpServer1)
	diags.Append(d...)
	ntpServer2, d := decodeString(data, "ntp_server_2", priorModel.NtpServer2)
	diags.Append(d...)
	ntpServer3, d := decodeString(data, "ntp_server_3", priorModel.NtpServer3)
	diags.Append(d...)
	ntpServer4, d := decodeString(data, "ntp_server_4", priorModel.NtpServer4)
	diags.Append(d...)
	if diags.HasError() {
		return diags
	}

	m := settingNtpModel{
		NtpServer1:        ntpServer1,
		NtpServer2:        ntpServer2,
		NtpServer3:        ntpServer3,
		NtpServer4:        ntpServer4,
		SettingPreference: settingPreference,
	}

	obj, objDiags := types.ObjectValueFrom(ctx, ntpAttrTypes, m)
	diags.Append(objDiags...)
	if diags.HasError() {
		return diags
	}

	model.Ntp = obj
	return diags
}

// overlay computes the "ntp" PUT body from model.Ntp, starting from a deep
// copy of the snapshot's current section data so any unmodeled key already
// present on the controller is preserved. Returns configured == false (no
// write) when the section is not configured (null/unknown) in model.
func (ntpSection) overlay(ctx context.Context, model, prior settingResourceModel, snap rawSettings) (settings.RawSetting, bool, diag.Diagnostics) {
	var diags diag.Diagnostics

	if model.Ntp.IsNull() || model.Ntp.IsUnknown() {
		return settings.RawSetting{}, false, diags
	}

	var m settingNtpModel
	diags.Append(model.Ntp.As(ctx, &m, basetypes.ObjectAsOptions{})...)
	if diags.HasError() {
		return settings.RawSetting{}, false, diags
	}

	base := snap.dataCopy("ntp")
	overlayString(base, "setting_preference", m.SettingPreference)
	overlayString(base, "ntp_server_1", m.NtpServer1)
	overlayString(base, "ntp_server_2", m.NtpServer2)
	overlayString(base, "ntp_server_3", m.NtpServer3)
	overlayString(base, "ntp_server_4", m.NtpServer4)

	rs := settings.RawSetting{
		BaseSetting: settings.BaseSetting{Key: "ntp"},
		Data:        base,
	}
	return rs, true, diags
}

// carryBestEffort copies the plan's ntp value onto dst. This section holds
// no secret leaves, so it is a straight copy with no per-leaf plan/prior
// choice needed.
func (ntpSection) carryBestEffort(dst *settingResourceModel, plan settingResourceModel) diag.Diagnostics {
	dst.Ntp = plan.Ntp
	return nil
}

// isConfigured reports whether m.Ntp is set (non-null, non-unknown), gating
// whether Create/Update push this section to the controller at all.
func (ntpSection) isConfigured(m settingResourceModel) bool {
	return !m.Ntp.IsNull() && !m.Ntp.IsUnknown()
}
