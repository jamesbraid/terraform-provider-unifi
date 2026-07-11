package unifi

import (
	"context"
	"errors"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/objectplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/setplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
	"github.com/ubiquiti-community/terraform-provider-unifi/unifi/util"
)

// settingGlobalNatModel is the nested global_nat block: the site-wide NAT
// mode and per-network exclusions that pair with unifi_nat_rule.
type settingGlobalNatModel struct {
	Mode               types.String `tfsdk:"mode"`
	ExcludedNetworkIDs types.Set    `tfsdk:"excluded_network_ids"`
}

var globalNatAttrTypes = map[string]attr.Type{
	"mode":                 types.StringType,
	"excluded_network_ids": types.SetType{ElemType: types.StringType},
}

type globalNatSection struct{}

func (globalNatSection) key() string { return "global_nat" }

func (globalNatSection) attrTypes() map[string]attr.Type { return globalNatAttrTypes }

func (globalNatSection) schemaAttribute() schema.SingleNestedAttribute {
	return schema.SingleNestedAttribute{
		MarkdownDescription: "Site-wide NAT settings.",
		Optional:            true,
		Computed:            true,
		PlanModifiers: []planmodifier.Object{
			objectplanmodifier.UseStateForUnknown(),
		},
		Attributes: map[string]schema.Attribute{
			"mode": schema.StringAttribute{
				MarkdownDescription: "NAT mode: `auto`, `custom`, or `off`.",
				Optional:            true,
				Computed:            true,
				Validators: []validator.String{
					stringvalidator.OneOf("auto", "custom", "off"),
				},
			},
			"excluded_network_ids": schema.SetAttribute{
				MarkdownDescription: "Network IDs excluded from automatic NAT.",
				Optional:            true,
				Computed:            true,
				ElementType:         types.StringType,
				PlanModifiers: []planmodifier.Set{
					setplanmodifier.UseStateForUnknown(),
				},
			},
		},
	}
}

func (globalNatSection) get(m *settingResourceModel) types.Object { return m.GlobalNat }

func (globalNatSection) set(m *settingResourceModel, obj types.Object) { m.GlobalNat = obj }

func (globalNatSection) overlay(
	ctx context.Context,
	obj types.Object,
	data map[string]any,
) diag.Diagnostics {
	var diags diag.Diagnostics
	var m settingGlobalNatModel
	diags.Append(obj.As(ctx, &m, basetypes.ObjectAsOptions{})...)
	if diags.HasError() {
		return diags
	}
	globalNatModelToData(ctx, &m, data, &diags)
	return diags
}

// globalNatModelToData writes only the user-set fields into the raw section
// document; unset fields keep their remote values.
func globalNatModelToData(
	ctx context.Context,
	m *settingGlobalNatModel,
	data map[string]any,
	diags *diag.Diagnostics,
) {
	if !m.Mode.IsNull() && !m.Mode.IsUnknown() {
		data["mode"] = m.Mode.ValueString()
	}
	if !m.ExcludedNetworkIDs.IsNull() && !m.ExcludedNetworkIDs.IsUnknown() {
		var ids []string
		diags.Append(m.ExcludedNetworkIDs.ElementsAs(ctx, &ids, false)...)
		data["excluded_network_ids"] = ids
	}
}

func (globalNatSection) read(
	ctx context.Context,
	client *Client,
	site string,
) (types.Object, diag.Diagnostics) {
	var diags diag.Diagnostics
	_, setting, err := ui.GetSetting[*settings.GlobalNat](client.ApiClient, ctx, site)
	if err != nil {
		var notFound *ui.NotFoundError
		if errors.As(err, &notFound) {
			return types.ObjectNull(globalNatAttrTypes), diags
		}
		diags.AddError("Error Reading Global NAT Setting", err.Error())
		return types.ObjectNull(globalNatAttrTypes), diags
	}
	model := globalNatSettingToModel(ctx, setting, &diags)
	if diags.HasError() {
		return types.ObjectNull(globalNatAttrTypes), diags
	}
	return types.ObjectValueFrom(ctx, globalNatAttrTypes, model)
}

func globalNatSettingToModel(
	ctx context.Context,
	s *settings.GlobalNat,
	diags *diag.Diagnostics,
) settingGlobalNatModel {
	ids, d := types.SetValueFrom(ctx, types.StringType, s.ExcludedNetworkIDs)
	diags.Append(d...)
	return settingGlobalNatModel{
		Mode:               util.StringValueOrNull(s.Mode),
		ExcludedNetworkIDs: ids,
	}
}
