package unifi

import (
	"context"
	"errors"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/objectplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
)

// settingProviderCapabilitiesModel is the nested provider_capabilities
// block: the advertised ISP download/upload capacity (kbps) the controller
// uses for WAN utilization displays and Smart Queues sizing. Not exposed by
// any prior-art provider (filipowm has no equivalent); names follow the
// go-unifi/controller JSON keys directly.
type settingProviderCapabilitiesModel struct {
	Download types.Int64 `tfsdk:"download"`
	Upload   types.Int64 `tfsdk:"upload"`
}

var providerCapabilitiesAttrTypes = map[string]attr.Type{
	"download": types.Int64Type,
	"upload":   types.Int64Type,
}

type providerCapabilitiesSection struct{}

func (providerCapabilitiesSection) key() string { return "provider_capabilities" }

func (providerCapabilitiesSection) attrTypes() map[string]attr.Type {
	return providerCapabilitiesAttrTypes
}

func (providerCapabilitiesSection) schemaAttribute() schema.SingleNestedAttribute {
	return schema.SingleNestedAttribute{
		MarkdownDescription: "ISP capability settings: the advertised download/upload capacity of the " +
			"internet connection, in kbps. Used by the controller for WAN utilization displays and " +
			"Smart Queues sizing; not otherwise enforced.",
		Optional: true,
		Computed: true,
		PlanModifiers: []planmodifier.Object{
			objectplanmodifier.UseStateForUnknown(),
		},
		Attributes: map[string]schema.Attribute{
			"download": schema.Int64Attribute{
				MarkdownDescription: "Advertised download capacity in kbps (e.g. `1000000` for 1 Gbps).",
				Optional:            true,
				Computed:            true,
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.UseStateForUnknown(),
				},
			},
			"upload": schema.Int64Attribute{
				MarkdownDescription: "Advertised upload capacity in kbps.",
				Optional:            true,
				Computed:            true,
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.UseStateForUnknown(),
				},
			},
		},
	}
}

func (providerCapabilitiesSection) get(m *settingResourceModel) types.Object {
	return m.ProviderCapabilities
}

func (providerCapabilitiesSection) set(m *settingResourceModel, obj types.Object) {
	m.ProviderCapabilities = obj
}

func (providerCapabilitiesSection) overlay(
	ctx context.Context,
	obj types.Object,
	data map[string]any,
) diag.Diagnostics {
	var diags diag.Diagnostics
	var m settingProviderCapabilitiesModel
	diags.Append(obj.As(ctx, &m, basetypes.ObjectAsOptions{})...)
	if diags.HasError() {
		return diags
	}
	providerCapabilitiesModelToData(&m, data, &diags)
	return diags
}

// providerCapabilitiesModelToData writes only the user-set fields into the
// raw section document; unset fields keep their remote values.
func providerCapabilitiesModelToData(
	m *settingProviderCapabilitiesModel,
	data map[string]any,
	_ *diag.Diagnostics,
) {
	if !m.Download.IsNull() && !m.Download.IsUnknown() {
		data["download"] = m.Download.ValueInt64()
	}
	if !m.Upload.IsNull() && !m.Upload.IsUnknown() {
		data["upload"] = m.Upload.ValueInt64()
	}
}

func (providerCapabilitiesSection) read(
	ctx context.Context,
	client *Client,
	site string,
) (types.Object, diag.Diagnostics) {
	var diags diag.Diagnostics
	_, setting, err := ui.GetSetting[*settings.ProviderCapabilities](client.ApiClient, ctx, site)
	if err != nil {
		var notFound *ui.NotFoundError
		if errors.As(err, &notFound) {
			return types.ObjectNull(providerCapabilitiesAttrTypes), diags
		}
		diags.AddError("Error Reading Provider Capabilities Setting", err.Error())
		return types.ObjectNull(providerCapabilitiesAttrTypes), diags
	}
	model := providerCapabilitiesSettingToModel(setting)
	return types.ObjectValueFrom(ctx, providerCapabilitiesAttrTypes, model)
}

func providerCapabilitiesSettingToModel(s *settings.ProviderCapabilities) settingProviderCapabilitiesModel {
	m := settingProviderCapabilitiesModel{
		Download: types.Int64Null(),
		Upload:   types.Int64Null(),
	}
	if s.Download != 0 {
		m.Download = types.Int64Value(s.Download)
	}
	if s.Upload != 0 {
		m.Upload = types.Int64Value(s.Upload)
	}
	return m
}
