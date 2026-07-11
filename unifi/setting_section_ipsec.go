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

// settingIpsecModel is the nested ipsec block: site-wide IPsec/IKEv2
// behavior (currently the IKEv2 reauthentication method).
type settingIpsecModel struct {
	Ikev2ReauthenticationMethod types.String `tfsdk:"ikev2_reauthentication_method"`
}

var ipsecAttrTypes = map[string]attr.Type{
	"ikev2_reauthentication_method": types.StringType,
}

type ipsecSection struct{}

func (ipsecSection) key() string { return "ipsec" }

func (ipsecSection) attrTypes() map[string]attr.Type { return ipsecAttrTypes }

func (ipsecSection) schemaAttribute() schema.SingleNestedAttribute {
	return schema.SingleNestedAttribute{
		MarkdownDescription: "Site-wide IPsec settings.",
		Optional:            true,
		Computed:            true,
		PlanModifiers: []planmodifier.Object{
			objectplanmodifier.UseStateForUnknown(),
		},
		Attributes: map[string]schema.Attribute{
			"ikev2_reauthentication_method": schema.StringAttribute{
				MarkdownDescription: "IKEv2 reauthentication method (e.g. `make-before-break`).",
				Optional:            true,
				Computed:            true,
			},
		},
	}
}

func (ipsecSection) get(m *settingResourceModel) types.Object { return m.Ipsec }

func (ipsecSection) set(m *settingResourceModel, obj types.Object) { m.Ipsec = obj }

func (ipsecSection) overlay(
	ctx context.Context,
	obj types.Object,
	data map[string]any,
) diag.Diagnostics {
	var diags diag.Diagnostics
	var m settingIpsecModel
	diags.Append(obj.As(ctx, &m, basetypes.ObjectAsOptions{})...)
	if diags.HasError() {
		return diags
	}
	ipsecModelToData(&m, data)
	return diags
}

// ipsecModelToData writes only the user-set fields into the raw section
// document; unset fields keep their remote values.
func ipsecModelToData(m *settingIpsecModel, data map[string]any) {
	if !m.Ikev2ReauthenticationMethod.IsNull() && !m.Ikev2ReauthenticationMethod.IsUnknown() {
		data["ikev2_reauthentication_method"] = m.Ikev2ReauthenticationMethod.ValueString()
	}
}

func (ipsecSection) read(
	ctx context.Context,
	client *Client,
	site string,
) (types.Object, diag.Diagnostics) {
	var diags diag.Diagnostics
	_, setting, err := ui.GetSetting[*settings.Ipsec](client.ApiClient, ctx, site)
	if err != nil {
		var notFound *ui.NotFoundError
		if errors.As(err, &notFound) {
			return types.ObjectNull(ipsecAttrTypes), diags
		}
		diags.AddError("Error Reading IPsec Setting", err.Error())
		return types.ObjectNull(ipsecAttrTypes), diags
	}
	return types.ObjectValueFrom(ctx, ipsecAttrTypes, ipsecSettingToModel(setting))
}

func ipsecSettingToModel(s *settings.Ipsec) settingIpsecModel {
	return settingIpsecModel{
		Ikev2ReauthenticationMethod: util.StringValueOrNull(s.Ikev2ReauthenticationMethod),
	}
}
