package unifi

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
)

// sslInspectionSection is the settingSection implementation for the
// "ssl_inspection" settings section: a flat SingleNestedAttribute with a
// single closed-enum scalar leaf, no nested objects/lists and no secrets.
type sslInspectionSection struct{}

func init() {
	registerSection(sslInspectionSection{})
}

func (sslInspectionSection) key() string      { return "ssl_inspection" }
func (sslInspectionSection) attrName() string { return "ssl_inspection" }

func (sslInspectionSection) schemaAttribute() schema.Attribute {
	return schema.SingleNestedAttribute{
		MarkdownDescription: "SSL/TLS inspection settings.",
		Optional:            true,
		Computed:            true,
		Attributes: map[string]schema.Attribute{
			"state": schema.StringAttribute{
				MarkdownDescription: "SSL inspection mode.",
				Optional:            true,
				Computed:            true,
				Validators: []validator.String{
					stringvalidator.OneOf("off", "simple", "advanced"),
				},
			},
		},
	}
}

func (s sslInspectionSection) decode(ctx context.Context, snap rawSettings, prior settingResourceModel, model *settingResourceModel) diag.Diagnostics {
	var diags diag.Diagnostics

	var priorModel settingSslInspectionModel
	if !prior.SslInspection.IsNull() && !prior.SslInspection.IsUnknown() {
		diags.Append(prior.SslInspection.As(ctx, &priorModel, basetypes.ObjectAsOptions{})...)
	}

	sec, _ := snap.section(s.key())
	data := sec.Data

	state, d := decodeString(data, "state", priorModel.State)
	diags.Append(d...)
	if diags.HasError() {
		return diags
	}

	m := settingSslInspectionModel{State: state}

	obj, objDiags := types.ObjectValueFrom(ctx, sslInspectionAttrTypes, m)
	diags.Append(objDiags...)
	if diags.HasError() {
		return diags
	}

	model.SslInspection = obj
	return diags
}

func (s sslInspectionSection) overlay(ctx context.Context, model, prior settingResourceModel, snap rawSettings) (settings.RawSetting, bool, diag.Diagnostics) {
	var diags diag.Diagnostics

	if model.SslInspection.IsNull() || model.SslInspection.IsUnknown() {
		return settings.RawSetting{}, false, diags
	}

	var m settingSslInspectionModel
	diags.Append(model.SslInspection.As(ctx, &m, basetypes.ObjectAsOptions{})...)
	if diags.HasError() {
		return settings.RawSetting{}, false, diags
	}

	base := snap.dataCopy(s.key())
	overlayString(base, "state", m.State)

	rs := settings.RawSetting{
		BaseSetting: settings.BaseSetting{Key: s.key()},
		Data:        base,
	}
	return rs, true, diags
}

func (sslInspectionSection) carryBestEffort(dst *settingResourceModel, plan settingResourceModel) diag.Diagnostics {
	dst.SslInspection = plan.SslInspection
	return nil
}

func (sslInspectionSection) isConfigured(m settingResourceModel) bool {
	return !m.SslInspection.IsNull() && !m.SslInspection.IsUnknown()
}
