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

// settingGlobalSwitchModel is the nested global_switch block: site-wide
// switch behavior. acl_device_isolation, acl_l3_isolation, and
// switch_exclusions align with filipowm's unifi_setting_global_switch; the
// remaining fields are this provider's superset.
type settingGlobalSwitchModel struct {
	ACLDeviceIsolation             types.Set    `tfsdk:"acl_device_isolation"`
	ACLL3Isolation                 types.Set    `tfsdk:"acl_l3_isolation"`
	SwitchExclusions               types.Set    `tfsdk:"switch_exclusions"`
	DHCPSnoop                      types.Bool   `tfsdk:"dhcp_snoop"`
	Dot1XFallbackNetworkID         types.String `tfsdk:"dot1x_fallback_network_id"`
	Dot1XPortctrlEnabled           types.Bool   `tfsdk:"dot1x_portctrl_enabled"`
	FloodKnownProtocols            types.Bool   `tfsdk:"flood_known_protocols"`
	FlowctrlEnabled                types.Bool   `tfsdk:"flowctrl_enabled"`
	ForwardUnknownMcastRouterPorts types.Bool   `tfsdk:"forward_unknown_mcast_router_ports"`
	JumboframeEnabled              types.Bool   `tfsdk:"jumboframe_enabled"`
	RADIUSProfileID                types.String `tfsdk:"radius_profile_id"`
	StpVersion                     types.String `tfsdk:"stp_version"`
}

type settingGlobalSwitchACLL3IsolationModel struct {
	SourceNetwork       types.String `tfsdk:"source_network"`
	DestinationNetworks types.Set    `tfsdk:"destination_networks"`
}

var (
	globalSwitchACLL3IsolationAttrTypes = map[string]attr.Type{
		"source_network":       types.StringType,
		"destination_networks": types.SetType{ElemType: types.StringType},
	}
	globalSwitchAttrTypes = map[string]attr.Type{
		"acl_device_isolation": types.SetType{ElemType: types.StringType},
		"acl_l3_isolation": types.SetType{
			ElemType: types.ObjectType{AttrTypes: globalSwitchACLL3IsolationAttrTypes},
		},
		"switch_exclusions":                  types.SetType{ElemType: types.StringType},
		"dhcp_snoop":                         types.BoolType,
		"dot1x_fallback_network_id":          types.StringType,
		"dot1x_portctrl_enabled":             types.BoolType,
		"flood_known_protocols":              types.BoolType,
		"flowctrl_enabled":                   types.BoolType,
		"forward_unknown_mcast_router_ports": types.BoolType,
		"jumboframe_enabled":                 types.BoolType,
		"radius_profile_id":                  types.StringType,
		"stp_version":                        types.StringType,
	}
)

type globalSwitchSection struct{}

func (globalSwitchSection) key() string { return "global_switch" }

func (globalSwitchSection) attrTypes() map[string]attr.Type { return globalSwitchAttrTypes }

func (globalSwitchSection) schemaAttribute() schema.SingleNestedAttribute {
	return schema.SingleNestedAttribute{
		MarkdownDescription: "Site-wide switch settings. Controller-managed fields not exposed here (e.g. link debounce) are preserved across updates.",
		Optional:            true,
		Computed:            true,
		PlanModifiers: []planmodifier.Object{
			objectplanmodifier.UseStateForUnknown(),
		},
		Attributes: map[string]schema.Attribute{
			"acl_device_isolation": schema.SetAttribute{
				MarkdownDescription: "Device identifiers isolated by the controller's Device Isolation control.",
				Optional:            true,
				Computed:            true,
				ElementType:         types.StringType,
				PlanModifiers: []planmodifier.Set{
					setplanmodifier.UseStateForUnknown(),
				},
			},
			"acl_l3_isolation": schema.SetNestedAttribute{
				MarkdownDescription: "Layer-3 (network-to-network) isolation rules.",
				Optional:            true,
				Computed:            true,
				PlanModifiers: []planmodifier.Set{
					setplanmodifier.UseStateForUnknown(),
				},
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"source_network": schema.StringAttribute{
							MarkdownDescription: "UniFi network ID the rule applies to.",
							Required:            true,
						},
						"destination_networks": schema.SetAttribute{
							MarkdownDescription: "UniFi network IDs the source network is isolated from.",
							Required:            true,
							ElementType:         types.StringType,
						},
					},
				},
			},
			"switch_exclusions": schema.SetAttribute{
				MarkdownDescription: "Switch MAC addresses excluded from isolation enforcement.",
				Optional:            true,
				Computed:            true,
				ElementType:         types.StringType,
				PlanModifiers: []planmodifier.Set{
					setplanmodifier.UseStateForUnknown(),
				},
			},
			"dhcp_snoop": schema.BoolAttribute{
				MarkdownDescription: "Enable DHCP snooping.",
				Optional:            true,
				Computed:            true,
			},
			"dot1x_fallback_network_id": schema.StringAttribute{
				MarkdownDescription: "Fallback network ID for 802.1X (empty for none).",
				Optional:            true,
				Computed:            true,
			},
			"dot1x_portctrl_enabled": schema.BoolAttribute{
				MarkdownDescription: "Enable 802.1X port control.",
				Optional:            true,
				Computed:            true,
			},
			"flood_known_protocols": schema.BoolAttribute{
				MarkdownDescription: "Flood known protocols.",
				Optional:            true,
				Computed:            true,
			},
			"flowctrl_enabled": schema.BoolAttribute{
				MarkdownDescription: "Enable flow control.",
				Optional:            true,
				Computed:            true,
			},
			"forward_unknown_mcast_router_ports": schema.BoolAttribute{
				MarkdownDescription: "Forward unknown multicast to router ports.",
				Optional:            true,
				Computed:            true,
			},
			"jumboframe_enabled": schema.BoolAttribute{
				MarkdownDescription: "Enable jumbo frames.",
				Optional:            true,
				Computed:            true,
			},
			"radius_profile_id": schema.StringAttribute{
				MarkdownDescription: "RADIUS profile ID used for 802.1X.",
				Optional:            true,
				Computed:            true,
			},
			"stp_version": schema.StringAttribute{
				MarkdownDescription: "Spanning tree mode: `stp`, `rstp`, or `disabled`.",
				Optional:            true,
				Computed:            true,
				Validators: []validator.String{
					stringvalidator.OneOf("stp", "rstp", "disabled"),
				},
			},
		},
	}
}

func (globalSwitchSection) get(m *settingResourceModel) types.Object { return m.GlobalSwitch }

func (globalSwitchSection) set(m *settingResourceModel, obj types.Object) { m.GlobalSwitch = obj }

func (globalSwitchSection) overlay(
	ctx context.Context,
	obj types.Object,
	data map[string]any,
) diag.Diagnostics {
	var diags diag.Diagnostics
	var m settingGlobalSwitchModel
	diags.Append(obj.As(ctx, &m, basetypes.ObjectAsOptions{})...)
	if diags.HasError() {
		return diags
	}
	globalSwitchModelToData(ctx, &m, data, &diags)
	return diags
}

// globalSwitchModelToData writes only the user-set fields into the raw
// section document; unset fields — including controller fields go-unifi
// does not model, like link_debounce — keep their remote values.
func globalSwitchModelToData(
	ctx context.Context,
	m *settingGlobalSwitchModel,
	data map[string]any,
	diags *diag.Diagnostics,
) {
	if !m.ACLDeviceIsolation.IsNull() && !m.ACLDeviceIsolation.IsUnknown() {
		var ids []string
		diags.Append(m.ACLDeviceIsolation.ElementsAs(ctx, &ids, false)...)
		data["acl_device_isolation"] = ids
	}
	if !m.ACLL3Isolation.IsNull() && !m.ACLL3Isolation.IsUnknown() {
		var rules []settingGlobalSwitchACLL3IsolationModel
		diags.Append(m.ACLL3Isolation.ElementsAs(ctx, &rules, false)...)
		out := make([]map[string]any, 0, len(rules))
		for _, rule := range rules {
			var dst []string
			diags.Append(rule.DestinationNetworks.ElementsAs(ctx, &dst, false)...)
			out = append(out, map[string]any{
				"source_network":       rule.SourceNetwork.ValueString(),
				"destination_networks": dst,
			})
		}
		data["acl_l3_isolation"] = out
	}
	if !m.SwitchExclusions.IsNull() && !m.SwitchExclusions.IsUnknown() {
		var macs []string
		diags.Append(m.SwitchExclusions.ElementsAs(ctx, &macs, false)...)
		data["switch_exclusions"] = macs
	}
	if !m.DHCPSnoop.IsNull() && !m.DHCPSnoop.IsUnknown() {
		data["dhcp_snoop"] = m.DHCPSnoop.ValueBool()
	}
	if !m.Dot1XFallbackNetworkID.IsNull() && !m.Dot1XFallbackNetworkID.IsUnknown() {
		data["dot1x_fallback_networkconf_id"] = m.Dot1XFallbackNetworkID.ValueString()
	}
	if !m.Dot1XPortctrlEnabled.IsNull() && !m.Dot1XPortctrlEnabled.IsUnknown() {
		data["dot1x_portctrl_enabled"] = m.Dot1XPortctrlEnabled.ValueBool()
	}
	if !m.FloodKnownProtocols.IsNull() && !m.FloodKnownProtocols.IsUnknown() {
		data["flood_known_protocols"] = m.FloodKnownProtocols.ValueBool()
	}
	if !m.FlowctrlEnabled.IsNull() && !m.FlowctrlEnabled.IsUnknown() {
		data["flowctrl_enabled"] = m.FlowctrlEnabled.ValueBool()
	}
	if !m.ForwardUnknownMcastRouterPorts.IsNull() && !m.ForwardUnknownMcastRouterPorts.IsUnknown() {
		data["forward_unknown_mcast_router_ports"] = m.ForwardUnknownMcastRouterPorts.ValueBool()
	}
	if !m.JumboframeEnabled.IsNull() && !m.JumboframeEnabled.IsUnknown() {
		data["jumboframe_enabled"] = m.JumboframeEnabled.ValueBool()
	}
	if !m.RADIUSProfileID.IsNull() && !m.RADIUSProfileID.IsUnknown() {
		data["radiusprofile_id"] = m.RADIUSProfileID.ValueString()
	}
	if !m.StpVersion.IsNull() && !m.StpVersion.IsUnknown() {
		data["stp_version"] = m.StpVersion.ValueString()
	}
}

func (globalSwitchSection) read(
	ctx context.Context,
	client *Client,
	site string,
) (types.Object, diag.Diagnostics) {
	var diags diag.Diagnostics
	_, setting, err := ui.GetSetting[*settings.GlobalSwitch](client.ApiClient, ctx, site)
	if err != nil {
		var notFound *ui.NotFoundError
		if errors.As(err, &notFound) {
			return types.ObjectNull(globalSwitchAttrTypes), diags
		}
		diags.AddError("Error Reading Global Switch Setting", err.Error())
		return types.ObjectNull(globalSwitchAttrTypes), diags
	}
	model := globalSwitchSettingToModel(ctx, setting, &diags)
	if diags.HasError() {
		return types.ObjectNull(globalSwitchAttrTypes), diags
	}
	return types.ObjectValueFrom(ctx, globalSwitchAttrTypes, model)
}

func globalSwitchSettingToModel(
	ctx context.Context,
	s *settings.GlobalSwitch,
	diags *diag.Diagnostics,
) settingGlobalSwitchModel {
	aclDevice, d := types.SetValueFrom(ctx, types.StringType, s.AclDeviceIsolation)
	diags.Append(d...)

	rules := make([]settingGlobalSwitchACLL3IsolationModel, 0, len(s.AclL3Isolation))
	for _, rule := range s.AclL3Isolation {
		dst, d := types.SetValueFrom(ctx, types.StringType, rule.DestinationNetworks)
		diags.Append(d...)
		rules = append(rules, settingGlobalSwitchACLL3IsolationModel{
			SourceNetwork:       types.StringValue(rule.SourceNetwork),
			DestinationNetworks: dst,
		})
	}
	aclL3, d := types.SetValueFrom(ctx,
		types.ObjectType{AttrTypes: globalSwitchACLL3IsolationAttrTypes}, rules)
	diags.Append(d...)

	exclusions, d := types.SetValueFrom(ctx, types.StringType, s.SwitchExclusions)
	diags.Append(d...)

	return settingGlobalSwitchModel{
		ACLDeviceIsolation:             aclDevice,
		ACLL3Isolation:                 aclL3,
		SwitchExclusions:               exclusions,
		DHCPSnoop:                      types.BoolValue(s.DHCPSnoop),
		Dot1XFallbackNetworkID:         util.StringValueOrNull(s.Dot1XFallbackNetworkID),
		Dot1XPortctrlEnabled:           types.BoolValue(s.Dot1XPortctrlEnabled),
		FloodKnownProtocols:            types.BoolValue(s.FloodKnownProtocols),
		FlowctrlEnabled:                types.BoolValue(s.FlowctrlEnabled),
		ForwardUnknownMcastRouterPorts: types.BoolValue(s.ForwardUnknownMcastRouterPorts),
		JumboframeEnabled:              types.BoolValue(s.JumboframeEnabled),
		RADIUSProfileID:                util.StringValueOrNull(s.RADIUSProfileID),
		StpVersion:                     util.StringValueOrNull(s.StpVersion),
	}
}
