package unifi

import (
	"context"
	"regexp"

	"github.com/hashicorp/terraform-plugin-framework-validators/listvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
)

// globalSwitchMACValidator matches go-unifi's own GlobalSwitch.
// SwitchExclusions doc-comment regex: a colon-separated MAC address.
var globalSwitchMACValidator = listvalidator.ValueStringsAre(
	stringvalidator.RegexMatches(
		regexp.MustCompile(`^([0-9A-Fa-f]{2}:){5}([0-9A-Fa-f]{2})$`),
		"must be a MAC address (e.g. \"AA:BB:CC:00:11:22\")",
	),
)

// globalSwitchSection is the settingSection implementation for the
// "global_switch" settings section: site-wide switch port/network
// behavior defaults. Models the full 12-leaf wire struct plus the nested
// acl_l3_isolation object list — nothing deferred.
//
// switch_exclusions ([]string of switch MAC addresses to exclude from
// global switch behavior) IS modeled, as a List[String] with a per-element
// MAC regex validator: it is a user-configured policy control with a
// documented, controller-validated shape, the same class as every other
// leaf in this section — not controller-assigned metadata analogous to
// mgmt.ssh_keys' per-element fingerprint/date.
type globalSwitchSection struct{}

func init() {
	registerSection(globalSwitchSection{})
}

func (globalSwitchSection) key() string      { return "global_switch" }
func (globalSwitchSection) attrName() string { return "global_switch" }

func (globalSwitchSection) schemaAttribute() schema.Attribute {
	return schema.SingleNestedAttribute{
		MarkdownDescription: "Global switch settings.",
		Optional:            true,
		Computed:            true,
		Attributes: map[string]schema.Attribute{
			"acl_device_isolation": schema.ListAttribute{
				MarkdownDescription: "IDs of networks isolated from each other at Layer 2.",
				ElementType:         types.StringType,
				Optional:            true,
				Computed:            true,
				PlanModifiers: []planmodifier.List{
					listplanmodifier.UseStateForUnknown(),
				},
			},
			"acl_l3_isolation": schema.ListNestedAttribute{
				MarkdownDescription: "Layer 3 isolation rules: a source network isolated from a set of destination networks.",
				Optional:            true,
				Computed:            true,
				PlanModifiers: []planmodifier.List{
					listplanmodifier.UseStateForUnknown(),
				},
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"source_network": schema.StringAttribute{
							MarkdownDescription: "Source network ID.",
							Optional:            true,
							Computed:            true,
						},
						"destination_networks": schema.ListAttribute{
							MarkdownDescription: "Destination network IDs isolated from the source network.",
							ElementType:         types.StringType,
							Optional:            true,
							Computed:            true,
						},
					},
				},
			},
			"dhcp_snoop": schema.BoolAttribute{
				MarkdownDescription: "Whether DHCP snooping is enabled.",
				Optional:            true,
				Computed:            true,
				Default:             booldefault.StaticBool(false),
			},
			"dot1x_fallback_networkconf_id": schema.StringAttribute{
				MarkdownDescription: "Network ID to fall back to when 802.1X authentication fails.",
				Optional:            true,
				Computed:            true,
			},
			"dot1x_portctrl_enabled": schema.BoolAttribute{
				MarkdownDescription: "Whether 802.1X port control is enabled.",
				Optional:            true,
				Computed:            true,
				Default:             booldefault.StaticBool(false),
			},
			"flood_known_protocols": schema.BoolAttribute{
				MarkdownDescription: "Whether known-protocol multicast is flooded.",
				Optional:            true,
				Computed:            true,
				Default:             booldefault.StaticBool(false),
			},
			"flowctrl_enabled": schema.BoolAttribute{
				MarkdownDescription: "Whether switch flow control is enabled.",
				Optional:            true,
				Computed:            true,
				Default:             booldefault.StaticBool(false),
			},
			"forward_unknown_mcast_router_ports": schema.BoolAttribute{
				MarkdownDescription: "Whether unknown multicast is forwarded to router ports.",
				Optional:            true,
				Computed:            true,
				Default:             booldefault.StaticBool(false),
			},
			"jumboframe_enabled": schema.BoolAttribute{
				MarkdownDescription: "Whether jumbo frames are enabled.",
				Optional:            true,
				Computed:            true,
				Default:             booldefault.StaticBool(false),
			},
			"radiusprofile_id": schema.StringAttribute{
				MarkdownDescription: "RADIUS profile ID used for 802.1X port authentication.",
				Optional:            true,
				Computed:            true,
			},
			"stp_version": schema.StringAttribute{
				MarkdownDescription: "Spanning tree protocol version.",
				Optional:            true,
				Computed:            true,
				Validators: []validator.String{
					stringvalidator.OneOf("stp", "rstp", "disabled"),
				},
			},
			"switch_exclusions": schema.ListAttribute{
				MarkdownDescription: "MAC addresses of switches excluded from these global switch settings.",
				ElementType:         types.StringType,
				Optional:            true,
				Computed:            true,
				PlanModifiers: []planmodifier.List{
					listplanmodifier.UseStateForUnknown(),
				},
				Validators: []validator.List{globalSwitchMACValidator},
			},
		},
	}
}

func (s globalSwitchSection) decode(ctx context.Context, snap rawSettings, prior settingResourceModel, model *settingResourceModel) diag.Diagnostics {
	var diags diag.Diagnostics

	var priorModel settingGlobalSwitchModel
	if !prior.GlobalSwitch.IsNull() && !prior.GlobalSwitch.IsUnknown() {
		diags.Append(prior.GlobalSwitch.As(ctx, &priorModel, basetypes.ObjectAsOptions{})...)
	}

	sec, _ := snap.section(s.key())
	data := sec.Data

	aclDeviceIsolation, d := decodeStringList(ctx, data, "acl_device_isolation", priorModel.AclDeviceIsolation)
	diags.Append(d...)
	aclL3Isolation, d := decodeObjectList(ctx, data, "acl_l3_isolation", priorModel.AclL3Isolation, types.ObjectType{AttrTypes: globalSwitchAclL3IsolationAttrTypes})
	diags.Append(d...)
	dhcpSnoop, d := decodeBool(data, "dhcp_snoop", priorModel.DHCPSnoop)
	diags.Append(d...)
	dot1xFallbackNetworkID, d := decodeString(data, "dot1x_fallback_networkconf_id", priorModel.Dot1XFallbackNetworkID)
	diags.Append(d...)
	dot1xPortctrlEnabled, d := decodeBool(data, "dot1x_portctrl_enabled", priorModel.Dot1XPortctrlEnabled)
	diags.Append(d...)
	floodKnownProtocols, d := decodeBool(data, "flood_known_protocols", priorModel.FloodKnownProtocols)
	diags.Append(d...)
	flowctrlEnabled, d := decodeBool(data, "flowctrl_enabled", priorModel.FlowctrlEnabled)
	diags.Append(d...)
	forwardUnknownMcastRouterPorts, d := decodeBool(data, "forward_unknown_mcast_router_ports", priorModel.ForwardUnknownMcastRouterPorts)
	diags.Append(d...)
	jumboframeEnabled, d := decodeBool(data, "jumboframe_enabled", priorModel.JumboframeEnabled)
	diags.Append(d...)
	radiusProfileID, d := decodeString(data, "radiusprofile_id", priorModel.RADIUSProfileID)
	diags.Append(d...)
	stpVersion, d := decodeString(data, "stp_version", priorModel.StpVersion)
	diags.Append(d...)
	switchExclusions, d := decodeStringList(ctx, data, "switch_exclusions", priorModel.SwitchExclusions)
	diags.Append(d...)
	if diags.HasError() {
		return diags
	}

	m := settingGlobalSwitchModel{
		AclDeviceIsolation:             aclDeviceIsolation,
		AclL3Isolation:                 aclL3Isolation,
		DHCPSnoop:                      dhcpSnoop,
		Dot1XFallbackNetworkID:         dot1xFallbackNetworkID,
		Dot1XPortctrlEnabled:           dot1xPortctrlEnabled,
		FloodKnownProtocols:            floodKnownProtocols,
		FlowctrlEnabled:                flowctrlEnabled,
		ForwardUnknownMcastRouterPorts: forwardUnknownMcastRouterPorts,
		JumboframeEnabled:              jumboframeEnabled,
		RADIUSProfileID:                radiusProfileID,
		StpVersion:                     stpVersion,
		SwitchExclusions:               switchExclusions,
	}

	obj, objDiags := types.ObjectValueFrom(ctx, globalSwitchAttrTypes, m)
	diags.Append(objDiags...)
	if diags.HasError() {
		return diags
	}

	model.GlobalSwitch = obj
	return diags
}

func (s globalSwitchSection) overlay(ctx context.Context, model, prior settingResourceModel, snap rawSettings) (settings.RawSetting, bool, diag.Diagnostics) {
	var diags diag.Diagnostics

	if model.GlobalSwitch.IsNull() || model.GlobalSwitch.IsUnknown() {
		return settings.RawSetting{}, false, diags
	}

	var m settingGlobalSwitchModel
	diags.Append(model.GlobalSwitch.As(ctx, &m, basetypes.ObjectAsOptions{})...)
	if diags.HasError() {
		return settings.RawSetting{}, false, diags
	}

	base := snap.dataCopy(s.key())
	diags.Append(overlayStringList(ctx, base, "acl_device_isolation", m.AclDeviceIsolation)...)
	diags.Append(overlayObjectList(ctx, base, "acl_l3_isolation", m.AclL3Isolation)...)
	overlayBool(base, "dhcp_snoop", m.DHCPSnoop)
	overlayString(base, "dot1x_fallback_networkconf_id", m.Dot1XFallbackNetworkID)
	overlayBool(base, "dot1x_portctrl_enabled", m.Dot1XPortctrlEnabled)
	overlayBool(base, "flood_known_protocols", m.FloodKnownProtocols)
	overlayBool(base, "flowctrl_enabled", m.FlowctrlEnabled)
	overlayBool(base, "forward_unknown_mcast_router_ports", m.ForwardUnknownMcastRouterPorts)
	overlayBool(base, "jumboframe_enabled", m.JumboframeEnabled)
	overlayString(base, "radiusprofile_id", m.RADIUSProfileID)
	overlayString(base, "stp_version", m.StpVersion)
	diags.Append(overlayStringList(ctx, base, "switch_exclusions", m.SwitchExclusions)...)
	if diags.HasError() {
		return settings.RawSetting{}, false, diags
	}

	rs := settings.RawSetting{
		BaseSetting: settings.BaseSetting{Key: s.key()},
		Data:        base,
	}
	return rs, true, diags
}

func (globalSwitchSection) carryBestEffort(dst *settingResourceModel, plan settingResourceModel) diag.Diagnostics {
	dst.GlobalSwitch = plan.GlobalSwitch
	return nil
}

func (globalSwitchSection) isConfigured(m settingResourceModel) bool {
	return !m.GlobalSwitch.IsNull() && !m.GlobalSwitch.IsUnknown()
}
