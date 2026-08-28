package unifi

import (
	"context"
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-nettypes/cidrtypes"
	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework-timetypes/timetypes"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/resourcekit"
	"github.com/ubiquiti-community/terraform-provider-unifi/unifi/util"
)

type networkKitModel struct {
	ID                          types.String         `tfsdk:"id"`
	Site                        types.String         `tfsdk:"site"`
	Enabled                     types.Bool           `tfsdk:"enabled"`
	Name                        types.String         `tfsdk:"name"`
	NatOutboundIPAddresses      types.List           `tfsdk:"nat_outbound_ip_addresses"`
	AutoScale                   types.Bool           `tfsdk:"auto_scale"`
	Subnet                      cidrtypes.IPv4Prefix `tfsdk:"subnet"`
	DomainName                  types.String         `tfsdk:"domain_name"`
	Vlan                        types.Int64          `tfsdk:"vlan"`
	NetworkIsolation            types.Bool           `tfsdk:"network_isolation"`
	SettingPreference           types.String         `tfsdk:"setting_preference"`
	InternetAccess              types.Bool           `tfsdk:"internet_access"`
	IgmpSnooping                types.Bool           `tfsdk:"igmp_snooping"`
	MulticastDNS                types.Bool           `tfsdk:"multicast_dns"`
	GatewayType                 types.String         `tfsdk:"gateway_type"`
	IPv6InterfaceType           types.String         `tfsdk:"ipv6_interface_type"`
	IPv6ClientAddressAssignment types.String         `tfsdk:"ipv6_client_address_assignment"`
	IPv6StaticSubnet            types.String         `tfsdk:"ipv6_static_subnet"`
	IPv6RA                      types.Bool           `tfsdk:"ipv6_ra"`
	IPv6RAPriority              types.String         `tfsdk:"ipv6_ra_priority"`
	IPv6RAPreferredLifetime     timetypes.GoDuration `tfsdk:"ipv6_ra_preferred_lifetime"`
	IPv6RAValidLifetime         timetypes.GoDuration `tfsdk:"ipv6_ra_valid_lifetime"`
	IPv6PDInterface             types.String         `tfsdk:"ipv6_pd_interface"`
	IPv6PDPrefixID              types.String         `tfsdk:"ipv6_pd_prefixid"`
	IPv6PDStart                 types.String         `tfsdk:"ipv6_pd_start"`
	IPv6PDStop                  types.String         `tfsdk:"ipv6_pd_stop"`
	IPv6PDAutoPrefixidEnabled   types.Bool           `tfsdk:"ipv6_pd_auto_prefixid_enabled"`
	LteLan                      types.Bool           `tfsdk:"lte_lan"`
	IPAliases                   types.List           `tfsdk:"ip_aliases"`
	IPv6Aliases                 types.List           `tfsdk:"ipv6_aliases"`
	ThirdPartyGateway           types.Bool           `tfsdk:"third_party_gateway"`
	Purpose                     types.String         `tfsdk:"purpose"`
	DhcpGuarding                types.Object         `tfsdk:"dhcp_guarding"`
	DhcpServer                  types.Object         `tfsdk:"dhcp_server"`
	DhcpV6Server                types.Object         `tfsdk:"dhcp_v6_server"`
	DhcpRelay                   types.Object         `tfsdk:"dhcp_relay"`
	Timeouts                    timeouts.Value       `tfsdk:"timeouts"`
}

type netModel = networkKitModel

// netPtr is the shape most of unifi.Network's scalars have: a *string the model
// carries as a plain types.String. Same helper, same reason, as s2sPtr.
func netPtr(
	wire string,
	model func(*netModel) *types.String,
	sdk func(*ui.Network) **string,
) resourcekit.StringLikePtrField[netModel, ui.Network, types.String] {
	// StringLikePtrField carries no Elide: a nil pointer is null and there is
	// nothing else it could mean, so there is no decision to record.
	return resourcekit.StringLikePtrField[netModel, ui.Network, types.String]{
		Wire: wire, Model: model, SDK: sdk,
		New: func(v basetypes.StringValue) types.String { return v },
	}
}

func netBool(
	wire string,
	model func(*netModel) *types.Bool,
	sdk func(*ui.Network) *bool,
) resourcekit.BoolField[netModel, ui.Network] {
	return resourcekit.BoolField[netModel, ui.Network]{Wire: wire, Model: model, SDK: sdk}
}

// emptyIfUnset sends "" rather than omitting, for a field whose omitempty would
// otherwise leave the controller's old value in place when clearing it.
func emptyIfUnset(v types.String) *string {
	if v.IsNull() || v.IsUnknown() {
		return util.Ptr("")
	}
	return v.ValueStringPointer()
}

// strPtrOrNull is stringOrNull for a pointer: nil and "" are both absence.
func strPtrOrNull(ptr *string) types.String {
	if ptr == nil || *ptr == "" {
		return types.StringNull()
	}
	return types.StringValue(*ptr)
}

func networkKitBackend(client *ui.ApiClient) resourcekit.Backend[ui.Network] {
	return resourcekit.Backend[ui.Network]{
		// A network IS created -- unlike a device, which is adopted -- so the
		// whole object goes on create and the mask governs updates only.
		Create: func(ctx context.Context, site string, in *ui.Network) (*ui.Network, error) {
			return client.CreateNetwork(ctx, site, in)
		},
		Read: func(ctx context.Context, site, id string) (*ui.Network, error) {
			return client.GetNetwork(ctx, site, id)
		},
		ReadByName: func(ctx context.Context, site, name string) (*ui.Network, error) {
			return client.GetNetworkByName(ctx, site, name)
		},
		UpdateFields: func(
			ctx context.Context, site string, in *ui.Network, fields ...string,
		) (*ui.Network, error) {
			return client.UpdateNetworkFields(ctx, site, in, fields...)
		},
		// Delete needs the name but Backend.Delete only gets a site and id; one
		// read answers it, and the object is about to be destroyed anyway.
		Delete: func(ctx context.Context, site, id string) error {
			existing, err := client.GetNetwork(ctx, site, id)
			if err != nil {
				return err
			}
			name := ""
			if existing.Name != nil {
				name = *existing.Name
			}
			return client.DeleteNetwork(ctx, site, id, name)
		},
		List: func(ctx context.Context, site string) ([]ui.Network, error) {
			return client.ListNetwork(ctx, site)
		},
		GetID: func(s *ui.Network) string { return s.ID },
		SetID: func(s *ui.Network, id string) { s.ID = id },
	}
}

// networkKitBeforeSend owns the three things no Field can express: purpose
// and third_party_gateway share one wire field, and vlan is one attribute
// over two -- none of that fits a Field's one-to-one mapping, so AlwaysWire
// carries the results.
//
// DHCP defaults are create-only: under a mask, an unconfigured dhcp_server
// puts none of its wires in the mask, so an update leaves the controller's
// own alone. A create still sends the whole object, so a new LAN with no
// dhcp_server block still gets one, as it always has.
func networkKitBeforeSend(
	ctx context.Context,
	_, effective *netModel,
	sdk *ui.Network,
	_ any,
) diag.Diagnostics {
	var diags diag.Diagnostics

	// networkgroup is not modelled and the controller requires it.
	sdk.NetworkGroup = util.Ptr("LAN")
	// mdns_enabled is deprecated in the 10.x schema and retained for backwards
	// compatibility. It is still the only wire multicast_dns has, and the
	// attribute is released, so it is written until the attribute is retired.
	sdk.MdnsEnabled = effective.MulticastDNS.ValueBool() //nolint:staticcheck // the only wire for a released attribute
	sdk.Purpose = ui.PurposeCorporate
	networkPurposeToNetwork(effective.Purpose, effective.ThirdPartyGateway, sdk)
	networkVLANToNetwork(effective.Vlan, sdk)

	if sdk.IPAliases == nil {
		sdk.IPAliases = []string{}
	}

	creating := sdk.ID == ""
	if !creating {
		return diags
	}

	relayEnabled := false
	if !effective.DhcpRelay.IsNull() && !effective.DhcpRelay.IsUnknown() {
		var relay dhcpRelayModel
		if d := effective.DhcpRelay.As(ctx, &relay, basetypes.ObjectAsOptions{}); !d.HasError() {
			relayEnabled = relay.Enabled.ValueBool()
		}
	}

	dhcpServerUnset := effective.DhcpServer.IsNull() || effective.DhcpServer.IsUnknown()
	if dhcpServerUnset && !relayEnabled {
		// A DHCP server and a DHCP relay cannot coexist: with relay on, saying
		// dhcpd_enabled=true makes the controller reject the request.
		sdk.DHCPDBootEnabled = false
		sdk.DHCPDBootServer = ""
		sdk.DHCPDBootFilename = util.Ptr("")
		sdk.DHCPDEnabled = true
		sdk.DHCPDGatewayEnabled = false
		sdk.DHCPDConflictChecking = true
		sdk.DHCPDNtpEnabled = false
		sdk.DHCPDTimeOffsetEnabled = false
		sdk.DHCPDDNSEnabled = false
		sdk.DHCPDLeaseTime = util.Ptr(int64(86400))
		sdk.DHCPDWinsEnabled = false
		sdk.DHCPDWins1 = util.Ptr("")
		sdk.DHCPDWins2 = util.Ptr("")
		sdk.DHCPDWPAdUrl = util.Ptr("")
		sdk.DHCPDTFTPServer = util.Ptr("")
		sdk.DHCPDUnifiController = util.Ptr("")
		sdk.DHCPDDNS1 = ""
		sdk.DHCPDDNS2 = ""
		sdk.DHCPDDNS3 = ""
		sdk.DHCPDDNS4 = ""
	}
	return diags
}

// networkKitAfterReceive computes the two attributes that share purpose.
func networkKitAfterReceive(
	_ context.Context, sdk *ui.Network, model *netModel, prior netModel, _ any,
) diag.Diagnostics {
	model.Purpose, model.ThirdPartyGateway = networkPurposeFromNetwork(sdk)

	// Some controllers force multicast_dns to false on a corporate network, so
	// a known value (one the practitioner set) survives the read; only an
	// unset one takes the controller's. This is why multicast_dns is not a
	// Field: a Field's ToModel would overwrite it unconditionally before this
	// hook ever saw the prior value.
	if prior.MulticastDNS.IsNull() || prior.MulticastDNS.IsUnknown() {
		model.MulticastDNS = types.BoolValue(sdk.MdnsEnabled) //nolint:staticcheck // as above
	} else {
		model.MulticastDNS = prior.MulticastDNS
	}
	model.Vlan = networkVLANFromNetwork(sdk)
	// ipv6_aliases stays null: the SDK does have an IPV6Aliases field, but the
	// provider has never written it, and adding that is a practitioner-visible
	// change for its own commit.
	model.IPv6Aliases = types.ListNull(types.StringType)

	// A VLAN-only network doesn't store most of this surface: the controller
	// accepts the write but omits these fields on every read, so a plain
	// refresh would replace subnet, auto_scale and gateway_type with zeros and
	// plan an update forever. Preserve the prior value (state on refresh, the
	// plan on create) and let the API answer only where prior is unknown.
	if sdk.Purpose == ui.PurposeVLANOnly {
		networkPreserveVLANOnly(model, prior, sdk)
	}
	return nil
}

func networkPreserveVLANOnly(model *netModel, prior netModel, sdk *ui.Network) {
	if !prior.Subnet.IsUnknown() {
		model.Subnet = prior.Subnet
	}
	if !prior.AutoScale.IsUnknown() {
		model.AutoScale = prior.AutoScale
	}
	if !prior.InternetAccess.IsUnknown() {
		model.InternetAccess = prior.InternetAccess
	}
	if !prior.GatewayType.IsUnknown() {
		model.GatewayType = prior.GatewayType
	}
	if !prior.IPv6InterfaceType.IsUnknown() {
		model.IPv6InterfaceType = prior.IPv6InterfaceType
	}
	if !prior.IPv6StaticSubnet.IsUnknown() {
		model.IPv6StaticSubnet = prior.IPv6StaticSubnet
	}
	if !prior.IPv6PDInterface.IsUnknown() {
		model.IPv6PDInterface = prior.IPv6PDInterface
	}
	if !prior.IPv6PDPrefixID.IsUnknown() {
		model.IPv6PDPrefixID = prior.IPv6PDPrefixID
	}
	if !prior.LteLan.IsUnknown() {
		model.LteLan = prior.LteLan
	}
	if !prior.SettingPreference.IsUnknown() {
		model.SettingPreference = prior.SettingPreference
	} else {
		model.SettingPreference = types.StringPointerValue(sdk.SettingPreference)
	}
	if !prior.IPv6ClientAddressAssignment.IsUnknown() {
		model.IPv6ClientAddressAssignment = prior.IPv6ClientAddressAssignment
	} else {
		model.IPv6ClientAddressAssignment = types.StringPointerValue(
			sdk.IPV6ClientAddressAssignment,
		)
	}
	if !prior.IPv6RA.IsUnknown() {
		model.IPv6RA = prior.IPv6RA
	} else {
		model.IPv6RA = types.BoolValue(sdk.IPV6RaEnabled)
	}
	if !prior.IPv6RAPriority.IsUnknown() {
		model.IPv6RAPriority = prior.IPv6RAPriority
	} else {
		model.IPv6RAPriority = types.StringPointerValue(sdk.IPV6RaPriority)
	}
	if !prior.IPv6RAPreferredLifetime.IsUnknown() {
		model.IPv6RAPreferredLifetime = prior.IPv6RAPreferredLifetime
	} else {
		model.IPv6RAPreferredLifetime = util.DurationPtrValue(
			sdk.IPV6RaPreferredLifetime, time.Second,
		)
	}
	if !prior.IPv6RAValidLifetime.IsUnknown() {
		model.IPv6RAValidLifetime = prior.IPv6RAValidLifetime
	} else {
		model.IPv6RAValidLifetime = util.DurationPtrValue(sdk.IPV6RaValidLifetime, time.Second)
	}
	if !prior.IPv6PDStart.IsUnknown() {
		model.IPv6PDStart = prior.IPv6PDStart
	} else {
		model.IPv6PDStart = types.StringPointerValue(sdk.IPV6PDStart)
	}
	if !prior.IPv6PDStop.IsUnknown() {
		model.IPv6PDStop = prior.IPv6PDStop
	} else {
		model.IPv6PDStop = types.StringPointerValue(sdk.IPV6PDStop)
	}
	if !prior.IPv6PDAutoPrefixidEnabled.IsUnknown() {
		model.IPv6PDAutoPrefixidEnabled = prior.IPv6PDAutoPrefixidEnabled
	} else {
		model.IPv6PDAutoPrefixidEnabled = types.BoolValue(sdk.IPV6PDAutoPrefixidEnabled)
	}
	if !prior.DomainName.IsUnknown() {
		model.DomainName = prior.DomainName
	} else {
		model.DomainName = types.StringNull()
	}
}

func networkKitSpec() resourcekit.Spec[netModel, ui.Network] {
	return resourcekit.Spec[netModel, ui.Network]{
		TypeName: "network",
		Subject:  "Network",
		New:      func() *ui.Network { return &ui.Network{} },
		ID:       func(m *netModel) *types.String { return &m.ID },
		Site:     func(m *netModel) *types.String { return &m.Site },
		Timeouts: func(m *netModel) *timeouts.Value { return &m.Timeouts },
		// The documented import handle is "name=Test VLAN"; see Spec.Name.
		Name: func(m *netModel) *types.String { return &m.Name },
		// Fields is one literal because an instrument parses this file rather
		// than running it; a list built via helper calls would be invisible to
		// it, and every field in it would read as missing.
		Fields: []resourcekit.Field[netModel, ui.Network]{
			netPtr("name", func(m *netModel) *types.String { return &m.Name },
				func(s *ui.Network) **string { return &s.Name }),
			netBool("enabled", func(m *netModel) *types.Bool { return &m.Enabled },
				func(s *ui.Network) *bool { return &s.Enabled }),
			netBool("auto_scale_enabled", func(m *netModel) *types.Bool { return &m.AutoScale },
				func(s *ui.Network) *bool { return &s.AutoScaleEnabled }),
			resourcekit.StringLikePtrField[netModel, ui.Network, cidrtypes.IPv4Prefix]{
				Wire:  "ip_subnet",
				Model: func(m *netModel) *cidrtypes.IPv4Prefix { return &m.Subnet },
				SDK:   func(s *ui.Network) **string { return &s.IPSubnet },
				New: func(v basetypes.StringValue) cidrtypes.IPv4Prefix {
					return cidrtypes.IPv4Prefix{StringValue: v}
				},
			},
			netPtr("domain_name", func(m *netModel) *types.String { return &m.DomainName },
				func(s *ui.Network) **string { return &s.DomainName }),
			netBool("network_isolation_enabled",
				func(m *netModel) *types.Bool { return &m.NetworkIsolation },
				func(s *ui.Network) *bool { return &s.NetworkIsolationEnabled }),
			netPtr("setting_preference",
				func(m *netModel) *types.String { return &m.SettingPreference },
				func(s *ui.Network) **string { return &s.SettingPreference }),
			netBool("internet_access_enabled",
				func(m *netModel) *types.Bool { return &m.InternetAccess },
				func(s *ui.Network) *bool { return &s.InternetAccessEnabled }),
			netBool("igmp_snooping", func(m *netModel) *types.Bool { return &m.IgmpSnooping },
				func(s *ui.Network) *bool { return &s.IGMPSnooping }),
			netPtr("gateway_type", func(m *netModel) *types.String { return &m.GatewayType },
				func(s *ui.Network) **string { return &s.GatewayType }),
			netPtr("ipv6_interface_type",
				func(m *netModel) *types.String { return &m.IPv6InterfaceType },
				func(s *ui.Network) **string { return &s.IPV6InterfaceType }),
			netPtr("ipv6_client_address_assignment",
				func(m *netModel) *types.String { return &m.IPv6ClientAddressAssignment },
				func(s *ui.Network) **string { return &s.IPV6ClientAddressAssignment }),
			netPtr("ipv6_subnet", func(m *netModel) *types.String { return &m.IPv6StaticSubnet },
				func(s *ui.Network) **string { return &s.IPV6Subnet }),
			netBool("ipv6_ra_enabled", func(m *netModel) *types.Bool { return &m.IPv6RA },
				func(s *ui.Network) *bool { return &s.IPV6RaEnabled }),
			netPtr("ipv6_ra_priority", func(m *netModel) *types.String { return &m.IPv6RAPriority },
				func(s *ui.Network) **string { return &s.IPV6RaPriority }),
			resourcekit.DurationPtrField[netModel, ui.Network]{
				Wire:  "ipv6_ra_preferred_lifetime",
				Model: func(m *netModel) *timetypes.GoDuration { return &m.IPv6RAPreferredLifetime },
				SDK:   func(s *ui.Network) **int64 { return &s.IPV6RaPreferredLifetime },
				Units: time.Second,
				Elide: resourcekit.KeepZero,
			},
			resourcekit.DurationPtrField[netModel, ui.Network]{
				Wire:  "ipv6_ra_valid_lifetime",
				Model: func(m *netModel) *timetypes.GoDuration { return &m.IPv6RAValidLifetime },
				SDK:   func(s *ui.Network) **int64 { return &s.IPV6RaValidLifetime },
				Units: time.Second,
				Elide: resourcekit.KeepZero,
			},
			netPtr("ipv6_pd_interface", func(m *netModel) *types.String { return &m.IPv6PDInterface },
				func(s *ui.Network) **string { return &s.IPV6PDInterface }),
			resourcekit.StringField[netModel, ui.Network]{
				// The one ipv6_pd_* field the SDK does not carry as a pointer.
				Wire:  "ipv6_pd_prefixid",
				Model: func(m *netModel) *types.String { return &m.IPv6PDPrefixID },
				SDK:   func(s *ui.Network) *string { return &s.IPV6PDPrefixid },
				Elide: resourcekit.KeepZero,
			},
			netPtr("ipv6_pd_start", func(m *netModel) *types.String { return &m.IPv6PDStart },
				func(s *ui.Network) **string { return &s.IPV6PDStart }),
			netPtr("ipv6_pd_stop", func(m *netModel) *types.String { return &m.IPv6PDStop },
				func(s *ui.Network) **string { return &s.IPV6PDStop }),
			netBool("ipv6_pd_auto_prefixid_enabled",
				func(m *netModel) *types.Bool { return &m.IPv6PDAutoPrefixidEnabled },
				func(s *ui.Network) *bool { return &s.IPV6PDAutoPrefixidEnabled }),
			netBool("lte_lan_enabled", func(m *netModel) *types.Bool { return &m.LteLan },
				func(s *ui.Network) *bool { return &s.LteLanEnabled }),
			resourcekit.StringListField[netModel, ui.Network]{
				// KeepZero, NOT NullZero. An empty membership must read back as an
				// empty list: nulling it means a config saying ip_aliases = []
				// never stops planning a change.
				Wire:  "ip_aliases",
				Model: func(m *netModel) *types.List { return &m.IPAliases },
				SDK:   func(s *ui.Network) *[]string { return &s.IPAliases },
				Elide: resourcekit.KeepZero,
			},
			resourcekit.ScatteredObjectField[netModel, ui.Network]{
				Wires: []string{
					"dhcpguard_enabled", "dhcpd_ip_1", "dhcpd_ip_2", "dhcpd_ip_3",
				},
				Model:     func(m *netModel) *types.Object { return &m.DhcpGuarding },
				AttrTypes: dhcpGuardingModel{}.AttributeTypes(),
				// The three slots are filled positionally; masking one the
				// encoder didn't write sends its zero, blanking whatever the
				// controller holds, so a two-server list must not put
				// dhcpd_ip_3 in the mask.
				ConditionalWires: map[string]func(types.Object) bool{
					"dhcpd_ip_1": func(o types.Object) bool {
						return dhcpGuardingServerCount(o) > 0
					},
					"dhcpd_ip_2": func(o types.Object) bool {
						return dhcpGuardingServerCount(o) > 1
					},
					"dhcpd_ip_3": func(o types.Object) bool {
						return dhcpGuardingServerCount(o) > 2
					},
				},
				Encode: func(
					ctx context.Context, object types.Object, sdk *ui.Network,
				) diag.Diagnostics {
					var guarding dhcpGuardingModel
					diags := object.As(ctx, &guarding, basetypes.ObjectAsOptions{})
					if diags.HasError() {
						return diags
					}
					sdk.DHCPguardEnabled = guarding.Enabled.ValueBool()
					networkDHCPGuardingServersToNetwork(ctx, &diags, guarding.Servers, sdk)
					return diags
				},
				Decode: func(
					ctx context.Context, sdk *ui.Network, _ types.Object,
				) (types.Object, diag.Diagnostics) {
					var diags diag.Diagnostics
					value := dhcpGuardingModel{
						Enabled: types.BoolValue(sdk.DHCPguardEnabled),
						Servers: networkDHCPGuardingServersFromNetwork(ctx, &diags, sdk),
					}
					object, d := types.ObjectValueFrom(ctx, value.AttributeTypes(), value)
					diags.Append(d...)
					return object, diags
				},
				Elide: resourcekit.KeepZero,
			},
			resourcekit.ScatteredObjectField[netModel, ui.Network]{
				Wires: []string{
					"dhcpd_enabled", "dhcpd_start", "dhcpd_stop", "dhcpd_gateway_enabled",
					"dhcpd_conflict_checking", "dhcpd_ntp_enabled", "dhcpd_time_offset_enabled",
					"dhcpd_dns_enabled", "dhcpd_leasetime", "dhcpd_wpad_url", "dhcpd_tftp_server",
					"dhcpd_unifi_controller", "dhcpd_dns_1", "dhcpd_dns_2", "dhcpd_dns_3",
					"dhcpd_dns_4", "dhcpd_boot_enabled", "dhcpd_boot_server", "dhcpd_boot_filename",
					"dhcpd_wins_enabled", "dhcpd_wins_1", "dhcpd_wins_2",
					"dhcpd_ntp_1", "dhcpd_ntp_2",
				},
				Model:     func(m *netModel) *types.Object { return &m.DhcpServer },
				AttrTypes: dhcpServerModel{}.AttributeTypes(),
				Encode: func(
					ctx context.Context, object types.Object, sdk *ui.Network,
				) diag.Diagnostics {
					var server dhcpServerModel
					diags := object.As(ctx, &server, basetypes.ObjectAsOptions{})
					if diags.HasError() {
						return diags
					}
					networkBootToNetwork(ctx, &diags, server.Boot, sdk)
					sdk.DHCPDEnabled = server.Enabled.ValueBool()
					sdk.DHCPDStart = server.Start.ValueStringPointer()
					sdk.DHCPDStop = server.Stop.ValueStringPointer()
					sdk.DHCPDGatewayEnabled = server.GatewayEnabled.ValueBool()
					sdk.DHCPDConflictChecking = server.ConflictChecking.ValueBool()
					sdk.DHCPDNtpEnabled = server.NtpEnabled.ValueBool()
					sdk.DHCPDTimeOffsetEnabled = server.TimeOffsetEnabled.ValueBool()
					sdk.DHCPDDNSEnabled = server.DnsEnabled.ValueBool()
					sdk.DHCPDLeaseTime = util.DurationUnitsPtr(server.Leasetime, time.Second)
					networkWINSToNetwork(ctx, &diags, server.Wins, sdk)
					// These three send an empty string rather than omitting: the
					// field carries omitempty, so a nil pointer would leave the
					// controller's old value in place instead of clearing it.
					sdk.DHCPDWPAdUrl = emptyIfUnset(server.WpadUrl)
					sdk.DHCPDTFTPServer = emptyIfUnset(server.TftpServer)
					sdk.DHCPDUnifiController = emptyIfUnset(server.UnifiController)
					networkDHCPServerDNSToNetwork(ctx, &diags, server.DnsServers, sdk)
					networkNTPServersToNetwork(ctx, &diags, server.NtpServers, sdk)
					return diags
				},
				Decode: func(
					ctx context.Context, sdk *ui.Network, _ types.Object,
				) (types.Object, diag.Diagnostics) {
					var diags diag.Diagnostics
					value := dhcpServerModel{
						Boot:              networkBootFromNetwork(ctx, &diags, sdk),
						Enabled:           types.BoolValue(sdk.DHCPDEnabled),
						GatewayEnabled:    types.BoolValue(sdk.DHCPDGatewayEnabled),
						ConflictChecking:  types.BoolValue(sdk.DHCPDConflictChecking),
						NtpEnabled:        types.BoolValue(sdk.DHCPDNtpEnabled),
						TimeOffsetEnabled: types.BoolValue(sdk.DHCPDTimeOffsetEnabled),
						DnsEnabled:        types.BoolValue(sdk.DHCPDDNSEnabled),
						Leasetime:         util.DurationPtrValue(sdk.DHCPDLeaseTime, time.Second),
						Wins:              networkWINSFromNetwork(ctx, &diags, sdk),
						WpadUrl:           strPtrOrNull(sdk.DHCPDWPAdUrl),
						Start:             types.StringPointerValue(sdk.DHCPDStart),
						Stop:              types.StringPointerValue(sdk.DHCPDStop),
						TftpServer:        strPtrOrNull(sdk.DHCPDTFTPServer),
						UnifiController:   strPtrOrNull(sdk.DHCPDUnifiController),
						DnsServers:        networkDHCPServerDNSFromNetwork(ctx, &diags, sdk),
						NtpServers:        networkNTPServersFromNetwork(ctx, &diags, sdk),
					}
					object, d := types.ObjectValueFrom(ctx, value.AttributeTypes(), value)
					diags.Append(d...)
					return object, diags
				},
				Elide: resourcekit.KeepZero,
			},
			resourcekit.ScatteredObjectField[netModel, ui.Network]{
				Wires: []string{
					"dhcpdv6_enabled", "dhcpdv6_dns_auto", "dhcpdv6_start", "dhcpdv6_stop",
					"dhcpdv6_leasetime", "dhcpdv6_dns_1", "dhcpdv6_dns_2", "dhcpdv6_dns_3",
					"dhcpdv6_dns_4",
				},
				Model:     func(m *netModel) *types.Object { return &m.DhcpV6Server },
				AttrTypes: dhcpV6ServerModel{}.AttributeTypes(),
				Encode: func(
					ctx context.Context, object types.Object, sdk *ui.Network,
				) diag.Diagnostics {
					var v6 dhcpV6ServerModel
					diags := object.As(ctx, &v6, basetypes.ObjectAsOptions{})
					if diags.HasError() {
						return diags
					}
					sdk.DHCPDV6Enabled = v6.Enabled.ValueBool()
					sdk.DHCPDV6DNSAuto = v6.DNSAuto.ValueBool()
					sdk.DHCPDV6Start = v6.Start.ValueStringPointer()
					sdk.DHCPDV6Stop = v6.Stop.ValueStringPointer()
					sdk.DHCPDV6LeaseTime = v6.Lease.ValueInt64Pointer()
					networkDHCPV6ServerDNSToNetwork(ctx, &diags, v6.DNSServers, sdk)
					return diags
				},
				Decode: func(
					ctx context.Context, sdk *ui.Network, _ types.Object,
				) (types.Object, diag.Diagnostics) {
					var diags diag.Diagnostics
					value := dhcpV6ServerModel{
						Enabled:    types.BoolValue(sdk.DHCPDV6Enabled),
						DNSAuto:    types.BoolValue(sdk.DHCPDV6DNSAuto),
						DNSServers: networkDHCPV6ServerDNSFromNetwork(ctx, &diags, sdk),
						Lease:      types.Int64PointerValue(sdk.DHCPDV6LeaseTime),
						Start:      types.StringPointerValue(sdk.DHCPDV6Start),
						Stop:       types.StringPointerValue(sdk.DHCPDV6Stop),
					}
					object, d := types.ObjectValueFrom(ctx, value.AttributeTypes(), value)
					diags.Append(d...)
					return object, diags
				},
				Elide: resourcekit.KeepZero,
			},
			resourcekit.ScatteredObjectField[netModel, ui.Network]{
				Wires: []string{"dhcp_relay_enabled", "dhcp_relay_servers"},
				Model: func(m *netModel) *types.Object { return &m.DhcpRelay },
				// The server list is written only when the practitioner
				// supplied one; masking it otherwise sends [] and clears the
				// controller's.
				ConditionalWires: map[string]func(types.Object) bool{
					"dhcp_relay_servers": func(o types.Object) bool {
						return !objectListMember(o, "servers").IsNull()
					},
				},
				AttrTypes: dhcpRelayModel{}.AttributeTypes(),
				Encode: func(
					ctx context.Context, object types.Object, sdk *ui.Network,
				) diag.Diagnostics {
					var relay dhcpRelayModel
					diags := object.As(ctx, &relay, basetypes.ObjectAsOptions{})
					if diags.HasError() {
						return diags
					}
					sdk.DHCPRelayEnabled = relay.Enabled.ValueBool()
					if !relay.Servers.IsNull() && !relay.Servers.IsUnknown() {
						var servers []string
						diags.Append(relay.Servers.ElementsAs(ctx, &servers, false)...)
						if !diags.HasError() {
							sdk.DHCPRelayServers = servers
						}
					}
					return diags
				},
				Decode: func(
					ctx context.Context, sdk *ui.Network, _ types.Object,
				) (types.Object, diag.Diagnostics) {
					var diags diag.Diagnostics
					servers := types.ListNull(types.StringType)
					if len(sdk.DHCPRelayServers) > 0 {
						var d diag.Diagnostics
						servers, d = types.ListValueFrom(ctx, types.StringType, sdk.DHCPRelayServers)
						diags.Append(d...)
					}
					value := dhcpRelayModel{
						Enabled: types.BoolValue(sdk.DHCPRelayEnabled),
						Servers: servers,
					}
					object, d := types.ObjectValueFrom(ctx, value.AttributeTypes(), value)
					diags.Append(d...)
					return object, diags
				},
				Elide: resourcekit.KeepZero,
			},
			resourcekit.ObjectListField[netModel, ui.Network, ui.NetworkNATOutboundIPAddresses]{
				Wire:       "nat_outbound_ip_addresses",
				Model:      func(m *netModel) *types.List { return &m.NatOutboundIPAddresses },
				SDK:        func(s *ui.Network) *[]ui.NetworkNATOutboundIPAddresses { return &s.NATOutboundIPAddresses },
				AttrTypes:  natOutboundIPAddresses(),
				Unmodelled: []string{"ip_address_pool"},
				Encode: func(
					ctx context.Context, object types.Object,
				) (ui.NetworkNATOutboundIPAddresses, diag.Diagnostics) {
					var entry natOutboundIPAddressesModel
					diags := object.As(ctx, &entry, basetypes.ObjectAsOptions{})
					if diags.HasError() {
						return ui.NetworkNATOutboundIPAddresses{}, diags
					}
					return ui.NetworkNATOutboundIPAddresses{
						IPAddress:       entry.IPAddress.ValueString(),
						Mode:            entry.Mode.ValueStringPointer(),
						WANNetworkGroup: entry.WANNetworkGroup.ValueStringPointer(),
					}, diags
				},
				Decode: func(
					ctx context.Context, entry ui.NetworkNATOutboundIPAddresses,
				) (types.Object, diag.Diagnostics) {
					var diags diag.Diagnostics
					pool := types.ListNull(types.StringType)
					if entry.IPAddressPool != nil {
						value, d := types.ListValueFrom(ctx, types.StringType, entry.IPAddressPool)
						diags.Append(d...)
						pool = value
					}
					object, d := types.ObjectValue(natOutboundIPAddresses(), map[string]attr.Value{
						"ip_address":        types.StringValue(entry.IPAddress),
						"ip_address_pool":   pool,
						"mode":              types.StringPointerValue(entry.Mode),
						"wan_network_group": types.StringPointerValue(entry.WANNetworkGroup),
					})
					diags.Append(d...)
					return object, diags
				},
				Elide: resourcekit.KeepZero,
			},
		},

		Backend: resourcekit.Backend[ui.Network]{
			// Seeded so ToModel doesn't nil-dereference: Configure replaces the
			// whole Backend, so a test binary that never calls it would panic
			// here otherwise.
			GetID: func(s *ui.Network) string { return s.ID },
			SetID: func(s *ui.Network, id string) { s.ID = id },
		},

		BeforeSend:   networkKitBeforeSend,
		AfterReceive: networkKitAfterReceive,

		// did-emit, not would-emit: would-emit reopens value-clobbering on
		// every ConditionalWires-guarded wire.
		UnwritableWires: func(sdk *ui.Network) []string {
			emitted := make(map[string]struct{})
			for _, name := range networkMaskFor(networkAllWireNames(), sdk) {
				emitted[name] = struct{}{}
			}
			var unwritable []string
			for _, name := range networkAllWireNames() {
				if _, ok := emitted[name]; !ok {
					unwritable = append(unwritable, name)
				}
			}
			return unwritable
		},

		// purpose and vlan are derived by BeforeSend from attributes that are
		// not Fields, so nothing in the plan can put them in the mask.
		// networkgroup is not modelled at all and the controller requires it.
		AlwaysWire: []string{"purpose", "vlan", "vlan_enabled", "networkgroup", "mdns_enabled"},
	}
}

// dhcpGuardingServerCount reports how many guarding servers the object carries,
// which is what decides how many of the three positional slots Encode writes.
func dhcpGuardingServerCount(object types.Object) int {
	list := objectListMember(object, "servers")
	if list.IsNull() || list.IsUnknown() {
		return 0
	}
	return len(list.Elements())
}

// objectListMember reads a list-typed member, answering a null list when the
// member is absent or of another type -- which means the same thing to the
// callers above: nothing to write.
func objectListMember(object types.Object, name string) types.List {
	value, ok := object.Attributes()[name]
	if !ok {
		return types.ListNull(types.StringType)
	}
	list, ok := value.(types.List)
	if !ok {
		return types.ListNull(types.StringType)
	}
	return list
}

// networkAllWireNames is every top-level json tag on unifi.Network. This
// answers a question about the encoder, not this descriptor, so it must
// report every name the object cannot write; deriving it from this
// descriptor's own declarations would make the answer depend on who's asking.
func networkAllWireNames() []string {
	networkWireNamesOnce.Do(func() {
		typ := reflect.TypeOf(ui.Network{})
		for i := range typ.NumField() {
			tag := typ.Field(i).Tag.Get("json")
			if tag == "" || tag == "-" {
				continue
			}
			name, _, _ := strings.Cut(tag, ",")
			if name != "" {
				networkWireNames = append(networkWireNames, name)
			}
		}
		sort.Strings(networkWireNames)
	})
	return networkWireNames
}

var (
	networkWireNamesOnce sync.Once
	networkWireNames     []string
)
