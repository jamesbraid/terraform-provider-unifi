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
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
	"github.com/ubiquiti-community/terraform-provider-unifi/unifi/util"
)

// settingSslInspectionModel is the nested ssl_inspection block. The state
// attribute name and values align with filipowm's unifi_setting_ssl_inspection
// for config portability. The controller's identity-certificate scoping
// fields are not modeled; the raw merge preserves them across updates.
type settingSslInspectionModel struct {
	State types.String `tfsdk:"state"`
}

var sslInspectionAttrTypes = map[string]attr.Type{
	"state": types.StringType,
}

type sslInspectionSection struct{}

func (sslInspectionSection) key() string { return "ssl_inspection" }

func (sslInspectionSection) attrTypes() map[string]attr.Type { return sslInspectionAttrTypes }

func (sslInspectionSection) schemaAttribute() schema.SingleNestedAttribute {
	return schema.SingleNestedAttribute{
		MarkdownDescription: "SSL/TLS inspection settings. Controller-managed identity-certificate " +
			"scoping fields are preserved across updates.",
		Optional: true,
		Computed: true,
		PlanModifiers: []planmodifier.Object{
			objectplanmodifier.UseStateForUnknown(),
		},
		Attributes: map[string]schema.Attribute{
			"state": schema.StringAttribute{
				MarkdownDescription: "SSL inspection mode: `off`, `simple`, or `advanced`.",
				Optional:            true,
				Computed:            true,
				Validators: []validator.String{
					stringvalidator.OneOf("off", "simple", "advanced"),
				},
			},
		},
	}
}

func (sslInspectionSection) get(m *settingResourceModel) types.Object { return m.SslInspection }

func (sslInspectionSection) set(m *settingResourceModel, obj types.Object) { m.SslInspection = obj }

func (sslInspectionSection) overlay(
	ctx context.Context,
	obj types.Object,
	data map[string]any,
) diag.Diagnostics {
	var diags diag.Diagnostics
	var m settingSslInspectionModel
	diags.Append(obj.As(ctx, &m, basetypes.ObjectAsOptions{})...)
	if diags.HasError() {
		return diags
	}
	sslInspectionModelToData(&m, data)
	return diags
}

// sslInspectionModelToData writes only the user-set fields into the raw
// section document; unset fields — including the controller's unmodeled
// identity_certificate_* fields — keep their remote values.
func sslInspectionModelToData(m *settingSslInspectionModel, data map[string]any) {
	if !m.State.IsNull() && !m.State.IsUnknown() {
		data["state"] = m.State.ValueString()
	}
}

func (sslInspectionSection) read(
	ctx context.Context,
	client *Client,
	site string,
) (types.Object, diag.Diagnostics) {
	var diags diag.Diagnostics
	_, setting, err := ui.GetSetting[*settings.SslInspection](client.ApiClient, ctx, site)
	if err != nil {
		var notFound *ui.NotFoundError
		if errors.As(err, &notFound) {
			return types.ObjectNull(sslInspectionAttrTypes), diags
		}
		diags.AddError("Error Reading SSL Inspection Setting", err.Error())
		return types.ObjectNull(sslInspectionAttrTypes), diags
	}
	return types.ObjectValueFrom(ctx, sslInspectionAttrTypes, sslInspectionSettingToModel(setting))
}

func sslInspectionSettingToModel(s *settings.SslInspection) settingSslInspectionModel {
	return settingSslInspectionModel{State: util.StringValueOrNull(s.State)}
}
