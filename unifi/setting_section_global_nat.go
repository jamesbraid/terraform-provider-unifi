package unifi

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
)

// globalNatSection is the settingSection implementation for the
// "global_nat" settings section: site-wide NAT mode plus a list of
// networks excluded from it.
type globalNatSection struct{}

func init() {
	registerSection(globalNatSection{})
}

func (globalNatSection) key() string      { return "global_nat" }
func (globalNatSection) attrName() string { return "global_nat" }

func (globalNatSection) schemaAttribute() schema.Attribute {
	return schema.SingleNestedAttribute{
		MarkdownDescription: "Global NAT settings.",
		Optional:            true,
		Computed:            true,
		Attributes: map[string]schema.Attribute{
			"excluded_network_ids": schema.ListAttribute{
				MarkdownDescription: "IDs of networks excluded from the site-wide NAT mode.",
				ElementType:         types.StringType,
				Optional:            true,
				Computed:            true,
				PlanModifiers: []planmodifier.List{
					listplanmodifier.UseStateForUnknown(),
				},
			},
			"mode": schema.StringAttribute{
				MarkdownDescription: "Site-wide NAT mode.",
				Optional:            true,
				Computed:            true,
				Validators: []validator.String{
					stringvalidator.OneOf("auto", "custom", "off"),
				},
			},
		},
	}
}

func (s globalNatSection) decode(ctx context.Context, snap rawSettings, prior settingResourceModel, model *settingResourceModel) diag.Diagnostics {
	var diags diag.Diagnostics

	var priorModel settingGlobalNatModel
	if !prior.GlobalNat.IsNull() && !prior.GlobalNat.IsUnknown() {
		diags.Append(prior.GlobalNat.As(ctx, &priorModel, basetypes.ObjectAsOptions{})...)
	}

	sec, _ := snap.section(s.key())
	data := sec.Data

	excludedNetworkIDs, d := decodeStringList(ctx, data, "excluded_network_ids", priorModel.ExcludedNetworkIDs)
	diags.Append(d...)
	mode, d := decodeString(data, "mode", priorModel.Mode)
	diags.Append(d...)
	if diags.HasError() {
		return diags
	}

	m := settingGlobalNatModel{
		ExcludedNetworkIDs: excludedNetworkIDs,
		Mode:               mode,
	}

	obj, objDiags := types.ObjectValueFrom(ctx, globalNatAttrTypes, m)
	diags.Append(objDiags...)
	if diags.HasError() {
		return diags
	}

	model.GlobalNat = obj
	return diags
}

func (s globalNatSection) overlay(ctx context.Context, model, prior settingResourceModel, snap rawSettings) (settings.RawSetting, bool, diag.Diagnostics) {
	var diags diag.Diagnostics

	if model.GlobalNat.IsNull() || model.GlobalNat.IsUnknown() {
		return settings.RawSetting{}, false, diags
	}

	var m settingGlobalNatModel
	diags.Append(model.GlobalNat.As(ctx, &m, basetypes.ObjectAsOptions{})...)
	if diags.HasError() {
		return settings.RawSetting{}, false, diags
	}

	base := snap.dataCopy(s.key())
	diags.Append(overlayStringList(ctx, base, "excluded_network_ids", m.ExcludedNetworkIDs)...)
	overlayString(base, "mode", m.Mode)
	if diags.HasError() {
		return settings.RawSetting{}, false, diags
	}

	rs := settings.RawSetting{
		BaseSetting: settings.BaseSetting{Key: s.key()},
		Data:        base,
	}
	return rs, true, diags
}

func (globalNatSection) carryBestEffort(dst *settingResourceModel, plan settingResourceModel) diag.Diagnostics {
	dst.GlobalNat = plan.GlobalNat
	return nil
}

func (globalNatSection) isConfigured(m settingResourceModel) bool {
	return !m.GlobalNat.IsNull() && !m.GlobalNat.IsUnknown()
}
