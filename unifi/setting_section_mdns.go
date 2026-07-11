package unifi

import (
	"context"
	"errors"
	"regexp"

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

var mdnsCustomServiceAddressRegexp = regexp.MustCompile(
	`^_[a-zA-Z0-9._-]+\._(tcp|udp)(\.local)?$`)

// settingMdnsModel is the nested mdns block: the multicast-DNS repeater
// service selection. Naming follows the controller's JSON (no filipowm
// equivalent exists). predefined_services is a set of service codes on the
// Terraform side; the raw JSON shape [{"code": "..."}] is wrapped/unwrapped
// in the converters. The controller's network scoping fields (enabled_for,
// enabled_for_network_ids) are not modeled by go-unifi yet; the raw merge
// preserves them. TODO(go-unifi): expose them once the structs gain them.
type settingMdnsModel struct {
	Mode               types.String `tfsdk:"mode"`
	CustomServices     types.Set    `tfsdk:"custom_services"`
	PredefinedServices types.Set    `tfsdk:"predefined_services"`
}

type settingMdnsCustomServiceModel struct {
	Name    types.String `tfsdk:"name"`
	Address types.String `tfsdk:"address"`
}

var (
	mdnsCustomServiceAttrTypes = map[string]attr.Type{
		"name":    types.StringType,
		"address": types.StringType,
	}
	mdnsAttrTypes = map[string]attr.Type{
		"mode": types.StringType,
		"custom_services": types.SetType{
			ElemType: types.ObjectType{AttrTypes: mdnsCustomServiceAttrTypes},
		},
		"predefined_services": types.SetType{ElemType: types.StringType},
	}
)

type mdnsSection struct{}

func (mdnsSection) key() string { return "mdns" }

func (mdnsSection) attrTypes() map[string]attr.Type { return mdnsAttrTypes }

func (mdnsSection) schemaAttribute() schema.SingleNestedAttribute {
	return schema.SingleNestedAttribute{
		MarkdownDescription: "Multicast DNS (mDNS) repeater settings: which " +
			"services are forwarded between networks. Controller-managed " +
			"network scoping fields not exposed here are preserved across " +
			"updates.",
		Optional: true,
		Computed: true,
		PlanModifiers: []planmodifier.Object{
			objectplanmodifier.UseStateForUnknown(),
		},
		Attributes: map[string]schema.Attribute{
			"mode": schema.StringAttribute{
				MarkdownDescription: "Service selection mode: `all`, `auto`, or `custom`.",
				Optional:            true,
				Computed:            true,
				Validators: []validator.String{
					stringvalidator.OneOf("all", "auto", "custom"),
				},
			},
			"custom_services": schema.SetNestedAttribute{
				MarkdownDescription: "Custom mDNS service definitions (used with `mode = \"custom\"`).",
				Optional:            true,
				Computed:            true,
				PlanModifiers: []planmodifier.Set{
					setplanmodifier.UseStateForUnknown(),
				},
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"name": schema.StringAttribute{
							MarkdownDescription: "Display name of the service.",
							Required:            true,
						},
						"address": schema.StringAttribute{
							MarkdownDescription: "mDNS service address, e.g. `_home-assistant._tcp`.",
							Required:            true,
							Validators: []validator.String{
								stringvalidator.RegexMatches(
									mdnsCustomServiceAddressRegexp,
									"must look like `_name._tcp`, `_name._udp`, or with a `.local` suffix",
								),
							},
						},
					},
				},
			},
			"predefined_services": schema.SetAttribute{
				MarkdownDescription: "Predefined service codes to forward (used with " +
					"`mode = \"custom\"`), e.g. `apple_airPlay`, `google_chromecast`, " +
					"`printers`, `sonos`, `homeKit`, `matter_network`.",
				Optional:    true,
				Computed:    true,
				ElementType: types.StringType,
				PlanModifiers: []planmodifier.Set{
					setplanmodifier.UseStateForUnknown(),
				},
			},
		},
	}
}

func (mdnsSection) get(m *settingResourceModel) types.Object { return m.Mdns }

func (mdnsSection) set(m *settingResourceModel, obj types.Object) { m.Mdns = obj }

func (mdnsSection) overlay(
	ctx context.Context,
	obj types.Object,
	data map[string]any,
) diag.Diagnostics {
	var diags diag.Diagnostics
	var m settingMdnsModel
	diags.Append(obj.As(ctx, &m, basetypes.ObjectAsOptions{})...)
	if diags.HasError() {
		return diags
	}
	mdnsModelToData(ctx, &m, data, &diags)
	return diags
}

// mdnsModelToData writes only the user-set fields into the raw section
// document; unset fields — including controller fields go-unifi does not
// model, like enabled_for — keep their remote values.
func mdnsModelToData(
	ctx context.Context,
	m *settingMdnsModel,
	data map[string]any,
	diags *diag.Diagnostics,
) {
	if !m.Mode.IsNull() && !m.Mode.IsUnknown() {
		data["mode"] = m.Mode.ValueString()
	}
	if !m.CustomServices.IsNull() && !m.CustomServices.IsUnknown() {
		var svcs []settingMdnsCustomServiceModel
		diags.Append(m.CustomServices.ElementsAs(ctx, &svcs, false)...)
		out := make([]map[string]any, 0, len(svcs))
		for _, svc := range svcs {
			out = append(out, map[string]any{
				"name":    svc.Name.ValueString(),
				"address": svc.Address.ValueString(),
			})
		}
		data["custom_services"] = out
	}
	if !m.PredefinedServices.IsNull() && !m.PredefinedServices.IsUnknown() {
		var codes []string
		diags.Append(m.PredefinedServices.ElementsAs(ctx, &codes, false)...)
		out := make([]map[string]any, 0, len(codes))
		for _, code := range codes {
			out = append(out, map[string]any{"code": code})
		}
		data["predefined_services"] = out
	}
}

func (mdnsSection) read(
	ctx context.Context,
	client *Client,
	site string,
) (types.Object, diag.Diagnostics) {
	var diags diag.Diagnostics
	_, setting, err := ui.GetSetting[*settings.Mdns](client.ApiClient, ctx, site)
	if err != nil {
		var notFound *ui.NotFoundError
		if errors.As(err, &notFound) {
			return types.ObjectNull(mdnsAttrTypes), diags
		}
		diags.AddError("Error Reading mDNS Setting", err.Error())
		return types.ObjectNull(mdnsAttrTypes), diags
	}
	model := mdnsSettingToModel(ctx, setting, &diags)
	if diags.HasError() {
		return types.ObjectNull(mdnsAttrTypes), diags
	}
	return types.ObjectValueFrom(ctx, mdnsAttrTypes, model)
}

func mdnsSettingToModel(
	ctx context.Context,
	s *settings.Mdns,
	diags *diag.Diagnostics,
) settingMdnsModel {
	svcs := make([]settingMdnsCustomServiceModel, 0, len(s.CustomServices))
	for _, svc := range s.CustomServices {
		svcs = append(svcs, settingMdnsCustomServiceModel{
			Name:    util.StringValueOrNull(svc.Name),
			Address: util.StringValueOrNull(svc.Address),
		})
	}
	custom, d := types.SetValueFrom(ctx,
		types.ObjectType{AttrTypes: mdnsCustomServiceAttrTypes}, svcs)
	diags.Append(d...)

	codes := make([]string, 0, len(s.PredefinedServices))
	for _, svc := range s.PredefinedServices {
		codes = append(codes, svc.Code)
	}
	predefined, d := types.SetValueFrom(ctx, types.StringType, codes)
	diags.Append(d...)

	return settingMdnsModel{
		Mode:               util.StringValueOrNull(s.Mode),
		CustomServices:     custom,
		PredefinedServices: predefined,
	}
}
