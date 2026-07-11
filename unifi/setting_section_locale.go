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

// settingLocaleModel is the nested locale block: the site timezone. The
// attribute name aligns with filipowm's unifi_setting_locale for config
// portability.
type settingLocaleModel struct {
	Timezone types.String `tfsdk:"timezone"`
}

var localeAttrTypes = map[string]attr.Type{
	"timezone": types.StringType,
}

type localeSection struct{}

func (localeSection) key() string { return "locale" }

func (localeSection) attrTypes() map[string]attr.Type { return localeAttrTypes }

func (localeSection) schemaAttribute() schema.SingleNestedAttribute {
	return schema.SingleNestedAttribute{
		MarkdownDescription: "Site locale settings.",
		Optional:            true,
		Computed:            true,
		PlanModifiers: []planmodifier.Object{
			objectplanmodifier.UseStateForUnknown(),
		},
		Attributes: map[string]schema.Attribute{
			"timezone": schema.StringAttribute{
				MarkdownDescription: "Site timezone as an IANA zone name (e.g. `America/Vancouver`).",
				Optional:            true,
				Computed:            true,
			},
		},
	}
}

func (localeSection) get(m *settingResourceModel) types.Object { return m.Locale }

func (localeSection) set(m *settingResourceModel, obj types.Object) { m.Locale = obj }

func (localeSection) overlay(
	ctx context.Context,
	obj types.Object,
	data map[string]any,
) diag.Diagnostics {
	var diags diag.Diagnostics
	var m settingLocaleModel
	diags.Append(obj.As(ctx, &m, basetypes.ObjectAsOptions{})...)
	if diags.HasError() {
		return diags
	}
	localeModelToData(&m, data)
	return diags
}

// localeModelToData writes only the user-set fields into the raw section
// document; unset fields keep their remote values.
func localeModelToData(m *settingLocaleModel, data map[string]any) {
	if !m.Timezone.IsNull() && !m.Timezone.IsUnknown() {
		data["timezone"] = m.Timezone.ValueString()
	}
}

func (localeSection) read(
	ctx context.Context,
	client *Client,
	site string,
) (types.Object, diag.Diagnostics) {
	var diags diag.Diagnostics
	_, setting, err := ui.GetSetting[*settings.Locale](client.ApiClient, ctx, site)
	if err != nil {
		var notFound *ui.NotFoundError
		if errors.As(err, &notFound) {
			return types.ObjectNull(localeAttrTypes), diags
		}
		diags.AddError("Error Reading Locale Setting", err.Error())
		return types.ObjectNull(localeAttrTypes), diags
	}
	return types.ObjectValueFrom(ctx, localeAttrTypes, localeSettingToModel(setting))
}

func localeSettingToModel(s *settings.Locale) settingLocaleModel {
	return settingLocaleModel{Timezone: util.StringValueOrNull(s.Timezone)}
}
