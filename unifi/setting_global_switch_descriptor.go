package unifi

// The global_switch section descriptor: an unconditional-mirror hydration
// with no specials, shaped like setting_mgmt_descriptor.go, plus one
// ObjectListField (acl_l3_isolation, the same kind ether_lighting's two
// override lists use) and one StringListField (switch_exclusions).
//
// acl_device_isolation is settings.GlobalSwitch's other []string field and
// is deliberately NOT modeled: unlike acl_l3_isolation's sibling (whose
// destination_networks/source_network shape reads unambiguously as network
// FKs) it carries no wire comment, no FieldConstraints pattern, and no
// sibling field to infer its element shape from -- it could be MAC
// addresses, client IDs, or something else. Omitted rather than guessed,
// the same call this dispatch's radio_ai section makes for `default` and
// `useXY`.
//
// dot1x_fallback_networkconf_id, poe_staging_delay_msec and stp_version all
// carry a controller-published FieldConstraints pattern, but GlobalSwitch is
// a top-level settings document: the bootstrap capture looks up a
// document's own fields under its bare Go type name ("GlobalSwitch"), and
// settings.FieldConstraints keys every entry as "Setting"+TypeName
// ("SettingGlobalSwitch") -- confirmed empirically by running sdk-bootstrap
// against GlobalSwitch alone and finding stp_version's bootstrap entry
// carries no constraint at all. So, like every other top-level field in
// this batch (ssl_inspection.state, global_nat.mode, mdns.mode,
// teleport.subnet_cidr), these three validators are hand-transcribed in
// provider-codegen/policy/setting.json rather than compiler-derived; the
// compiler DID derive them for the two nested list_nested types this batch
// adds elsewhere (radio_ai's channels_blacklist/radios_configuration),
// whose Go type names already carry the "Setting" prefix and match
// FieldConstraints exactly.
import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
	resource_setting "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_setting"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/resourcekit"
)

// settingGlobalSwitchAclL3IsolationModel is one element of global_switch's
// acl_l3_isolation list.
type settingGlobalSwitchAclL3IsolationModel struct {
	DestinationNetworks types.List   `tfsdk:"destination_networks"`
	SourceNetwork       types.String `tfsdk:"source_network"`
}

// settingGlobalSwitchModel is global_switch's own section model, decoded out
// of settingResourceModel.GlobalSwitch.
type settingGlobalSwitchModel struct {
	AclL3Isolation                 types.List   `tfsdk:"acl_l3_isolation"`
	AutoStpEdgeDetectionEnabled    types.Bool   `tfsdk:"auto_stp_edge_detection_enabled"`
	DHCPSnoop                      types.Bool   `tfsdk:"dhcp_snoop"`
	Dot1XFallbackNetworkconfID     types.String `tfsdk:"dot1x_fallback_networkconf_id"`
	Dot1XPortctrlEnabled           types.Bool   `tfsdk:"dot1x_portctrl_enabled"`
	FloodKnownProtocols            types.Bool   `tfsdk:"flood_known_protocols"`
	FlowctrlEnabled                types.Bool   `tfsdk:"flowctrl_enabled"`
	ForwardUnknownMcastRouterPorts types.Bool   `tfsdk:"forward_unknown_mcast_router_ports"`
	JumboframeEnabled              types.Bool   `tfsdk:"jumboframe_enabled"`
	LinkDebounce                   types.Int64  `tfsdk:"link_debounce"`
	PoeStagingDelayMsec            types.Int64  `tfsdk:"poe_staging_delay_msec"`
	RADIUSProfileID                types.String `tfsdk:"radiusprofile_id"`
	StpVersion                     types.String `tfsdk:"stp_version"`
	SwitchExclusions               types.List   `tfsdk:"switch_exclusions"`
}

// globalSwitchAclL3IsolationAttrTypes and globalSwitchAttrTypes type
// global_switch's acl_l3_isolation elements and global_switch's own object
// in state; both must match the generated schema exactly.
var (
	globalSwitchAclL3IsolationAttrTypes = map[string]attr.Type{
		"destination_networks": types.ListType{ElemType: types.StringType},
		"source_network":       types.StringType,
	}
	globalSwitchAttrTypes = map[string]attr.Type{
		"acl_l3_isolation": types.ListType{
			ElemType: types.ObjectType{AttrTypes: globalSwitchAclL3IsolationAttrTypes},
		},
		"auto_stp_edge_detection_enabled":    types.BoolType,
		"dhcp_snoop":                         types.BoolType,
		"dot1x_fallback_networkconf_id":      types.StringType,
		"dot1x_portctrl_enabled":             types.BoolType,
		"flood_known_protocols":              types.BoolType,
		"flowctrl_enabled":                   types.BoolType,
		"forward_unknown_mcast_router_ports": types.BoolType,
		"jumboframe_enabled":                 types.BoolType,
		"link_debounce":                      types.Int64Type,
		"poe_staging_delay_msec":             types.Int64Type,
		"radiusprofile_id":                   types.StringType,
		"stp_version":                        types.StringType,
		"switch_exclusions":                  types.ListType{ElemType: types.StringType},
	}
)

// globalSwitchKitSpec maps every attribute of the generated global_switch
// schema (resource_setting/setting_resource_gen.go's "global_switch"
// SingleNestedAttribute) onto settings.GlobalSwitch. link_debounce and
// poe_staging_delay_msec are the only Int64PtrFields; both patterns admit a
// literal 0 (link_debounce's own "0|[1-9]00|..." and poe_staging_delay_msec's
// Int64Values set, which starts at 0), so neither needs OmitZero -- the one
// field in this dispatch's three sections where the pattern check comes back
// negative. dot1x_fallback_networkconf_id's RegexMatches accepts "" via its
// own trailing `|` alternative, so it wants KeepZero, same reasoning as
// teleport.subnet_cidr; stp_version's OneOf rejects "", so it wants
// NullZero, same as ssl_inspection.state/mdns.mode.
func globalSwitchKitSpec() resourcekit.Spec[settingGlobalSwitchModel, settings.GlobalSwitch] {
	return resourcekit.Spec[settingGlobalSwitchModel, settings.GlobalSwitch]{
		TypeName: "setting_global_switch",
		Subject:  "Global Switch Setting",
		New:      func() *settings.GlobalSwitch { return &settings.GlobalSwitch{} },
		Fields: []resourcekit.Field[settingGlobalSwitchModel, settings.GlobalSwitch]{
			resourcekit.ObjectListField[
				settingGlobalSwitchModel, settings.GlobalSwitch, settings.SettingGlobalSwitchAclL3Isolation,
			]{
				Wire:  "acl_l3_isolation",
				Model: func(m *settingGlobalSwitchModel) *types.List { return &m.AclL3Isolation },
				SDK: func(s *settings.GlobalSwitch) *[]settings.SettingGlobalSwitchAclL3Isolation {
					return &s.AclL3Isolation
				},
				AttrTypes: globalSwitchAclL3IsolationAttrTypes,
				Encode:    globalSwitchAclL3IsolationEncode,
				Decode:    globalSwitchAclL3IsolationDecode,
				Elide:     resourcekit.KeepZero,
			},
			resourcekit.BoolField[settingGlobalSwitchModel, settings.GlobalSwitch]{
				Wire:  "auto_stp_edge_detection_enabled",
				Model: func(m *settingGlobalSwitchModel) *types.Bool { return &m.AutoStpEdgeDetectionEnabled },
				SDK:   func(s *settings.GlobalSwitch) *bool { return &s.AutoStpEdgeDetectionEnabled },
			},
			resourcekit.BoolField[settingGlobalSwitchModel, settings.GlobalSwitch]{
				Wire:  "dhcp_snoop",
				Model: func(m *settingGlobalSwitchModel) *types.Bool { return &m.DHCPSnoop },
				SDK:   func(s *settings.GlobalSwitch) *bool { return &s.DHCPSnoop },
			},
			resourcekit.StringField[settingGlobalSwitchModel, settings.GlobalSwitch]{
				Wire:  "dot1x_fallback_networkconf_id",
				Model: func(m *settingGlobalSwitchModel) *types.String { return &m.Dot1XFallbackNetworkconfID },
				SDK:   func(s *settings.GlobalSwitch) *string { return &s.Dot1XFallbackNetworkID },
				Elide: resourcekit.KeepZero,
			},
			resourcekit.BoolField[settingGlobalSwitchModel, settings.GlobalSwitch]{
				Wire:  "dot1x_portctrl_enabled",
				Model: func(m *settingGlobalSwitchModel) *types.Bool { return &m.Dot1XPortctrlEnabled },
				SDK:   func(s *settings.GlobalSwitch) *bool { return &s.Dot1XPortctrlEnabled },
			},
			resourcekit.BoolField[settingGlobalSwitchModel, settings.GlobalSwitch]{
				Wire:  "flood_known_protocols",
				Model: func(m *settingGlobalSwitchModel) *types.Bool { return &m.FloodKnownProtocols },
				SDK:   func(s *settings.GlobalSwitch) *bool { return &s.FloodKnownProtocols },
			},
			resourcekit.BoolField[settingGlobalSwitchModel, settings.GlobalSwitch]{
				Wire:  "flowctrl_enabled",
				Model: func(m *settingGlobalSwitchModel) *types.Bool { return &m.FlowctrlEnabled },
				SDK:   func(s *settings.GlobalSwitch) *bool { return &s.FlowctrlEnabled },
			},
			resourcekit.BoolField[settingGlobalSwitchModel, settings.GlobalSwitch]{
				Wire:  "forward_unknown_mcast_router_ports",
				Model: func(m *settingGlobalSwitchModel) *types.Bool { return &m.ForwardUnknownMcastRouterPorts },
				SDK:   func(s *settings.GlobalSwitch) *bool { return &s.ForwardUnknownMcastRouterPorts },
			},
			resourcekit.BoolField[settingGlobalSwitchModel, settings.GlobalSwitch]{
				Wire:  "jumboframe_enabled",
				Model: func(m *settingGlobalSwitchModel) *types.Bool { return &m.JumboframeEnabled },
				SDK:   func(s *settings.GlobalSwitch) *bool { return &s.JumboframeEnabled },
			},
			resourcekit.Int64PtrField[settingGlobalSwitchModel, settings.GlobalSwitch]{
				Wire:  "link_debounce",
				Model: func(m *settingGlobalSwitchModel) *types.Int64 { return &m.LinkDebounce },
				SDK:   func(s *settings.GlobalSwitch) **int64 { return &s.LinkDebounce },
				Elide: resourcekit.KeepZero,
			},
			resourcekit.Int64PtrField[settingGlobalSwitchModel, settings.GlobalSwitch]{
				Wire:  "poe_staging_delay_msec",
				Model: func(m *settingGlobalSwitchModel) *types.Int64 { return &m.PoeStagingDelayMsec },
				SDK:   func(s *settings.GlobalSwitch) **int64 { return &s.PoeStagingDelayMsec },
				Elide: resourcekit.KeepZero,
			},
			resourcekit.StringField[settingGlobalSwitchModel, settings.GlobalSwitch]{
				Wire:  "radiusprofile_id",
				Model: func(m *settingGlobalSwitchModel) *types.String { return &m.RADIUSProfileID },
				SDK:   func(s *settings.GlobalSwitch) *string { return &s.RADIUSProfileID },
				Elide: resourcekit.KeepZero,
			},
			resourcekit.StringField[settingGlobalSwitchModel, settings.GlobalSwitch]{
				Wire:  "stp_version",
				Model: func(m *settingGlobalSwitchModel) *types.String { return &m.StpVersion },
				SDK:   func(s *settings.GlobalSwitch) *string { return &s.StpVersion },
				Elide: resourcekit.NullZero,
			},
			resourcekit.StringListField[settingGlobalSwitchModel, settings.GlobalSwitch]{
				Wire:  "switch_exclusions",
				Model: func(m *settingGlobalSwitchModel) *types.List { return &m.SwitchExclusions },
				SDK:   func(s *settings.GlobalSwitch) *[]string { return &s.SwitchExclusions },
				Elide: resourcekit.KeepZero,
			},
		},
	}
}

func globalSwitchAclL3IsolationEncode(
	ctx context.Context, object types.Object,
) (settings.SettingGlobalSwitchAclL3Isolation, diag.Diagnostics) {
	var model settingGlobalSwitchAclL3IsolationModel
	diags := object.As(ctx, &model, basetypes.ObjectAsOptions{})
	var destinationNetworks []string
	diags.Append(model.DestinationNetworks.ElementsAs(ctx, &destinationNetworks, false)...)
	return settings.SettingGlobalSwitchAclL3Isolation{
		DestinationNetworks: destinationNetworks,
		SourceNetwork:       model.SourceNetwork.ValueString(),
	}, diags
}

func globalSwitchAclL3IsolationDecode(
	ctx context.Context, element settings.SettingGlobalSwitchAclL3Isolation,
) (types.Object, diag.Diagnostics) {
	destinationNetworks := element.DestinationNetworks
	if destinationNetworks == nil {
		destinationNetworks = []string{}
	}
	destinationNetworksList, diags := types.ListValueFrom(ctx, types.StringType, destinationNetworks)
	object, d := types.ObjectValueFrom(ctx, globalSwitchAclL3IsolationAttrTypes, settingGlobalSwitchAclL3IsolationModel{
		DestinationNetworks: destinationNetworksList,
		SourceNetwork:       types.StringValue(element.SourceNetwork),
	})
	diags.Append(d...)
	return object, diags
}

// globalSwitchNestedSchema is the global_switch SingleNestedAttribute's own
// Attributes, wrapped as a schema.Schema so resourcekit's conformance
// checks -- built for a whole resource's top-level schema -- can run
// against one section of unifi_setting instead.
func globalSwitchNestedSchema(ctx context.Context) schema.Schema {
	built := resource_setting.SettingResourceSchema(ctx)
	globalSwitch := built.Attributes["global_switch"].(schema.SingleNestedAttribute) //nolint:forcetypeassert // global_switch is declared as SingleNestedAttribute in the generated schema; a mismatch here is a generator regression this is meant to catch loudly.
	return schema.Schema{Attributes: globalSwitch.Attributes}
}

// globalSwitchKitBackend binds globalSwitchKitSpec to a client: Read is
// GetSetting[*GlobalSwitch], UpdateFields is the masked UpdateSettingFields
// -- naming only the fields the plan set.
func globalSwitchKitBackend(client *ui.ApiClient) resourcekit.Backend[settings.GlobalSwitch] {
	return resourcekit.Backend[settings.GlobalSwitch]{
		Read: func(ctx context.Context, site, _ string) (*settings.GlobalSwitch, error) {
			_, globalSwitch, err := ui.GetSetting[*settings.GlobalSwitch](client, ctx, site)
			if err != nil {
				return nil, err
			}
			return globalSwitch, nil
		},
		UpdateFields: func(
			ctx context.Context, site string, in *settings.GlobalSwitch, fields ...string,
		) (*settings.GlobalSwitch, error) {
			if err := client.UpdateSettingFields(ctx, site, in, fields...); err != nil {
				return nil, err
			}
			return in, nil
		},
	}
}

// globalSwitchKitSection builds the global_switch entry for
// settingResource's Sections, bound to client via settingKitSections, which
// calls it with r.client.ApiClient.
func globalSwitchKitSection(client *ui.ApiClient) resourcekit.Section[settingResourceModel] {
	spec := globalSwitchKitSpec()
	spec.Backend = globalSwitchKitBackend(client)
	return resourcekit.SpecSection[settingResourceModel, settingGlobalSwitchModel, settings.GlobalSwitch]{
		SectionName: "global_switch",
		Get:         func(m *settingResourceModel) *types.Object { return &m.GlobalSwitch },
		Set:         func(m *settingResourceModel, o types.Object) { m.GlobalSwitch = o },
		AttrTypes:   globalSwitchAttrTypes,
		Spec:        spec,
	}
}
