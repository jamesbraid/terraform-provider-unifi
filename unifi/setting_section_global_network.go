package unifi

import (
	"context"
	"errors"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/objectplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
	"github.com/ubiquiti-community/terraform-provider-unifi/unifi/util"
)

// settingGlobalNetworkModel is the nested global_network block: site-wide
// network defaults (currently the default security posture).
type settingGlobalNetworkModel struct {
	DefaultSecurityPosture types.String `tfsdk:"default_security_posture"`
}

var globalNetworkAttrTypes = map[string]attr.Type{
	"default_security_posture": types.StringType,
}

type globalNetworkSection struct{}

func (globalNetworkSection) key() string { return "global_network" }

func (globalNetworkSection) attrTypes() map[string]attr.Type { return globalNetworkAttrTypes }

func (globalNetworkSection) schemaAttribute() schema.SingleNestedAttribute {
	return schema.SingleNestedAttribute{
		MarkdownDescription: "Site-wide network defaults.",
		Optional:            true,
		Computed:            true,
		PlanModifiers: []planmodifier.Object{
			objectplanmodifier.UseStateForUnknown(),
		},
		Attributes: map[string]schema.Attribute{
			"default_security_posture": schema.StringAttribute{
				MarkdownDescription: "Default security posture for new networks (e.g. `ALLOW_ALL`).",
				Optional:            true,
				Computed:            true,
			},
		},
	}
}

func (globalNetworkSection) get(m *settingResourceModel) types.Object { return m.GlobalNetwork }

func (globalNetworkSection) set(m *settingResourceModel, obj types.Object) { m.GlobalNetwork = obj }

func (globalNetworkSection) overlay(
	ctx context.Context,
	obj types.Object,
	data map[string]any,
) diag.Diagnostics {
	var diags diag.Diagnostics
	var m settingGlobalNetworkModel
	diags.Append(obj.As(ctx, &m, basetypes.ObjectAsOptions{})...)
	if diags.HasError() {
		return diags
	}
	globalNetworkModelToData(&m, data)
	return diags
}

// globalNetworkModelToData writes only the user-set fields into the raw
// section document; unset fields keep their remote values.
func globalNetworkModelToData(m *settingGlobalNetworkModel, data map[string]any) {
	if !m.DefaultSecurityPosture.IsNull() && !m.DefaultSecurityPosture.IsUnknown() {
		data["default_security_posture"] = m.DefaultSecurityPosture.ValueString()
	}
}

func (globalNetworkSection) read(
	ctx context.Context,
	client *Client,
	site string,
) (types.Object, diag.Diagnostics) {
	var diags diag.Diagnostics
	_, setting, err := ui.GetSetting[*settings.GlobalNetwork](client.ApiClient, ctx, site)
	if err != nil {
		var notFound *ui.NotFoundError
		if errors.As(err, &notFound) {
			return types.ObjectNull(globalNetworkAttrTypes), diags
		}
		diags.AddError("Error Reading Global Network Setting", err.Error())
		return types.ObjectNull(globalNetworkAttrTypes), diags
	}
	return types.ObjectValueFrom(ctx, globalNetworkAttrTypes, globalNetworkSettingToModel(setting))
}

func globalNetworkSettingToModel(s *settings.GlobalNetwork) settingGlobalNetworkModel {
	return settingGlobalNetworkModel{
		DefaultSecurityPosture: util.StringValueOrNull(s.DefaultSecurityPosture),
	}
}
