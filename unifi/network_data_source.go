package unifi

import (
	"context"
	"fmt"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/datasource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework-timetypes/timetypes"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/ubiquiti-community/go-unifi/unifi"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/datasource_network"
	"github.com/ubiquiti-community/terraform-provider-unifi/unifi/util"
)

// Ensure provider defined types fully satisfy framework interfaces.
var _ datasource.DataSource = &networkDataSource{}

func NewNetworkDataSource() datasource.DataSource {
	return &networkDataSource{}
}

// networkDataSource defines the data source implementation.
type networkDataSource struct {
	dataSourceWithClient
}

// networkDataSourceModel describes the data source data model.
type networkDataSourceModel struct {
	// Lookup keys
	ID   types.String `tfsdk:"id"`
	Site types.String `tfsdk:"site"`
	Name types.String `tfsdk:"name"`

	// Fields shared with resource (all Computed)
	Enabled                types.Bool   `tfsdk:"enabled"`
	AutoScale              types.Bool   `tfsdk:"auto_scale"`
	Subnet                 types.String `tfsdk:"subnet"`
	DomainName             types.String `tfsdk:"domain_name"`
	Vlan                   types.Int64  `tfsdk:"vlan"`
	NetworkIsolation       types.Bool   `tfsdk:"network_isolation"`
	SettingPreference      types.String `tfsdk:"setting_preference"`
	InternetAccess         types.Bool   `tfsdk:"internet_access"`
	IgmpSnooping           types.Bool   `tfsdk:"igmp_snooping"`
	MulticastDNS           types.Bool   `tfsdk:"multicast_dns"`
	GatewayType            types.String `tfsdk:"gateway_type"`
	IPv6InterfaceType      types.String `tfsdk:"ipv6_interface_type"`
	LteLan                 types.Bool   `tfsdk:"lte_lan"`
	IPAliases              types.List   `tfsdk:"ip_aliases"`
	IPv6Aliases            types.List   `tfsdk:"ipv6_aliases"`
	ThirdPartyGateway      types.Bool   `tfsdk:"third_party_gateway"`
	NatOutboundIPAddresses types.List   `tfsdk:"nat_outbound_ip_addresses"`
	DhcpGuarding           types.Object `tfsdk:"dhcp_guarding"`
	DhcpServer             types.Object `tfsdk:"dhcp_server"`
	DhcpRelay              types.Object `tfsdk:"dhcp_relay"`

	// Data-source-only informational fields
	Purpose      types.String `tfsdk:"purpose"`
	NetworkGroup types.String `tfsdk:"network_group"`

	// IPv6 detail fields (DS-only)
	IPv6StaticSubnet        types.String         `tfsdk:"ipv6_static_subnet"`
	IPv6PDInterface         types.String         `tfsdk:"ipv6_pd_interface"`
	IPv6PDPrefixID          types.String         `tfsdk:"ipv6_pd_prefixid"`
	IPv6PDStart             types.String         `tfsdk:"ipv6_pd_start"`
	IPv6PDStop              types.String         `tfsdk:"ipv6_pd_stop"`
	IPv6RA                  types.Bool           `tfsdk:"ipv6_ra"`
	IPv6RAPreferredLifetime timetypes.GoDuration `tfsdk:"ipv6_ra_preferred_lifetime"`
	IPv6RAPriority          types.String         `tfsdk:"ipv6_ra_priority"`
	IPv6RAValidLifetime     timetypes.GoDuration `tfsdk:"ipv6_ra_valid_lifetime"`

	// DHCPv6 server (DS-only)
	DhcpV6Server types.Object `tfsdk:"dhcp_v6_server"`

	// WAN fields (DS-only)
	WanDNS          types.List   `tfsdk:"wan_dns"`
	WanEgressQOS    types.Int64  `tfsdk:"wan_egress_qos"`
	WanGateway      types.String `tfsdk:"wan_gateway"`
	WanGatewayV6    types.String `tfsdk:"wan_gateway_v6"`
	WanIP           types.String `tfsdk:"wan_ip"`
	WanNetmask      types.String `tfsdk:"wan_netmask"`
	WanNetworkGroup types.String `tfsdk:"wan_network_group"`
	WanType         types.String `tfsdk:"wan_type"`
	WanTypeV6       types.String `tfsdk:"wan_type_v6"`
	WanUsername     types.String `tfsdk:"wan_username"`

	Timeouts timeouts.Value `tfsdk:"timeouts"`
}

func (d *networkDataSource) Metadata(
	ctx context.Context,
	req datasource.MetadataRequest,
	resp *datasource.MetadataResponse,
) {
	resp.TypeName = req.ProviderTypeName + "_network"
}

func (d *networkDataSource) Schema(
	ctx context.Context,
	req datasource.SchemaRequest,
	resp *datasource.SchemaResponse,
) {
	resp.Schema = datasource_network.NetworkDsDataSourceSchema(ctx)
	// Grafted rather than generated, as everywhere else: timeouts.Attributes
	// is a call, not a literal, so the code specification cannot carry it.
	resp.Schema.Attributes["timeouts"] = timeouts.Attributes(ctx)
}

func (d *networkDataSource) Read(
	ctx context.Context,
	req datasource.ReadRequest,
	resp *datasource.ReadResponse,
) {
	var config networkDataSourceModel
	diags := req.Config.Get(ctx, &config)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	readTimeout, timeoutDiags := config.Timeouts.Read(ctx, 20*time.Minute)
	resp.Diagnostics.Append(timeoutDiags...)
	if resp.Diagnostics.HasError() {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, readTimeout)
	defer cancel()

	site := config.Site.ValueString()
	if site == "" {
		site = d.client.Site
	}

	var network *unifi.Network
	var err error

	if !config.ID.IsNull() && !config.ID.IsUnknown() {
		id := config.ID.ValueString()
		network, err = d.client.GetNetwork(ctx, site, id)
		if err != nil {
			if _, ok := err.(*unifi.NotFoundError); ok {
				resp.Diagnostics.AddError(
					"Network Not Found",
					fmt.Sprintf("Network with ID %s not found: %s", id, err),
				)
				return
			}
			resp.Diagnostics.AddError(
				"Error Reading Network",
				fmt.Sprintf("Could not read network with ID %s: %s", id, err),
			)
			return
		}
	} else if !config.Name.IsNull() && !config.Name.IsUnknown() {
		name := config.Name.ValueString()
		network, err = d.client.GetNetworkByName(ctx, site, name)
		if err != nil {
			resp.Diagnostics.AddError(
				"Network Not Found",
				fmt.Sprintf("Network with name %s not found", name),
			)
			return
		}
	} else {
		resp.Diagnostics.AddError(
			"Missing Required Attribute",
			"Either 'id' or 'name' must be specified",
		)
		return
	}

	if err != nil {
		if _, ok := err.(*unifi.NotFoundError); ok {
			resp.Diagnostics.AddError(
				"Network Not Found",
				fmt.Sprintf("Network not found: %s", err),
			)
			return
		}
		resp.Diagnostics.AddError(
			"Error Reading Network",
			fmt.Sprintf("Could not read network: %s", err),
		)
		return
	}

	d.setDataSourceData(ctx, &resp.Diagnostics, network, &config, site)
	if resp.Diagnostics.HasError() {
		return
	}

	diags = resp.State.Set(ctx, config)
	resp.Diagnostics.Append(diags...)
}

func (d *networkDataSource) setDataSourceData(
	ctx context.Context,
	diags *diag.Diagnostics,
	network *unifi.Network,
	model *networkDataSourceModel,
	site string,
) {
	model.ID = types.StringValue(network.ID)
	model.Site = types.StringValue(site)
	model.Name = types.StringPointerValue(network.Name)
	model.Purpose, model.ThirdPartyGateway = networkDataSourcePurposeFromNetwork(network)
	model.NetworkGroup = types.StringPointerValue(network.NetworkGroup)

	// Shared with resource fields
	model.Enabled = types.BoolValue(network.Enabled)
	model.AutoScale = types.BoolValue(network.AutoScaleEnabled)
	model.Subnet = types.StringPointerValue(network.IPSubnet)
	model.DomainName = types.StringPointerValue(network.DomainName)
	model.Vlan = types.Int64PointerValue(network.VLAN)
	model.NetworkIsolation = types.BoolValue(network.NetworkIsolationEnabled)
	model.SettingPreference = types.StringPointerValue(network.SettingPreference)
	model.InternetAccess = types.BoolValue(network.InternetAccessEnabled)
	model.IgmpSnooping = types.BoolValue(network.IGMPSnooping)
	model.MulticastDNS = types.BoolValue(network.MdnsEnabled) //nolint:staticcheck // the only wire for a released attribute
	model.GatewayType = types.StringPointerValue(network.GatewayType)
	model.IPv6InterfaceType = types.StringPointerValue(network.IPV6InterfaceType)
	model.LteLan = types.BoolValue(network.LteLanEnabled)

	if len(network.IPAliases) > 0 {
		ipAliasesList, d := types.ListValueFrom(ctx, types.StringType, network.IPAliases)
		diags.Append(d...)
		model.IPAliases = ipAliasesList
	} else {
		model.IPAliases = types.ListNull(types.StringType)
	}

	// ipv6_aliases — not available in current API
	model.IPv6Aliases = types.ListNull(types.StringType)

	// nat_outbound_ip_addresses — not populated by API read
	model.NatOutboundIPAddresses = types.ListNull(
		types.ObjectType{AttrTypes: natOutboundIPAddresses()},
	)

	{
		dhcpGuardingValue := dhcpGuardingModel{
			Enabled: types.BoolValue(network.DHCPguardEnabled),
			Servers: networkDataSourceDHCPGuardingServersFromNetwork(ctx, diags, network),
		}
		dhcpGuardingObj, d := types.ObjectValueFrom(
			ctx,
			dhcpGuardingValue.AttributeTypes(),
			dhcpGuardingValue,
		)
		diags.Append(d...)
		model.DhcpGuarding = dhcpGuardingObj
	}

	{
		dhcpBootObj := networkDataSourceBootFromNetwork(ctx, diags, network)
		dnsServersList := networkDataSourceDHCPServerDNSFromNetwork(ctx, diags, network)
		winsObj := networkDataSourceWINSFromNetwork(ctx, diags, network)

		dhcpServerValue := dhcpServerModel{
			Boot:              dhcpBootObj,
			Enabled:           types.BoolValue(network.DHCPDEnabled),
			Start:             types.StringPointerValue(network.DHCPDStart),
			Stop:              types.StringPointerValue(network.DHCPDStop),
			GatewayEnabled:    types.BoolValue(network.DHCPDGatewayEnabled),
			ConflictChecking:  types.BoolValue(network.DHCPDConflictChecking),
			NtpEnabled:        types.BoolValue(network.DHCPDNtpEnabled),
			TimeOffsetEnabled: types.BoolValue(network.DHCPDTimeOffsetEnabled),
			DnsEnabled:        types.BoolValue(network.DHCPDDNSEnabled),
			Leasetime:         util.DurationPtrValue(network.DHCPDLeaseTime, time.Second),
			Wins:              winsObj,
			WpadUrl:           strPtrOrNull(network.DHCPDWPAdUrl),
			TftpServer:        strPtrOrNull(network.DHCPDTFTPServer),
			UnifiController:   strPtrOrNull(network.DHCPDUnifiController),
			DnsServers:        dnsServersList,
			NtpServers:        networkDataSourceNTPServersFromNetwork(ctx, diags, network),
		}
		dhcpServerObj, d := types.ObjectValueFrom(
			ctx,
			dhcpServerValue.AttributeTypes(),
			dhcpServerValue,
		)
		diags.Append(d...)
		model.DhcpServer = dhcpServerObj
	}

	{
		var relayServersVal types.List
		if len(network.DHCPRelayServers) > 0 {
			l, d := types.ListValueFrom(ctx, types.StringType, network.DHCPRelayServers)
			diags.Append(d...)
			relayServersVal = l
		} else {
			relayServersVal = types.ListNull(types.StringType)
		}
		dhcpRelayValue := dhcpRelayModel{
			Enabled: types.BoolValue(network.DHCPRelayEnabled),
			Servers: relayServersVal,
		}
		dhcpRelayObj, d := types.ObjectValueFrom(
			ctx,
			dhcpRelayValue.AttributeTypes(),
			dhcpRelayValue,
		)
		diags.Append(d...)
		model.DhcpRelay = dhcpRelayObj
	}

	// DS-only IPv6 fields
	model.IPv6StaticSubnet = types.StringPointerValue(network.IPV6Subnet)
	model.IPv6PDInterface = types.StringPointerValue(network.IPV6PDInterface)
	if network.IPV6PDPrefixid == "" {
		model.IPv6PDPrefixID = types.StringNull()
	} else {
		model.IPv6PDPrefixID = types.StringValue(network.IPV6PDPrefixid)
	}
	model.IPv6PDStart = types.StringPointerValue(network.IPV6PDStart)
	model.IPv6PDStop = types.StringPointerValue(network.IPV6PDStop)
	model.IPv6RA = types.BoolValue(network.IPV6RaEnabled)
	model.IPv6RAPreferredLifetime = util.DurationPtrValue(
		network.IPV6RaPreferredLifetime,
		time.Second,
	)
	model.IPv6RAPriority = types.StringPointerValue(network.IPV6RaPriority)
	model.IPv6RAValidLifetime = util.DurationPtrValue(network.IPV6RaValidLifetime, time.Second)

	{
		dhcpV6ServerValue := dhcpV6ServerModel{
			Enabled:    types.BoolValue(network.DHCPDV6Enabled),
			DNSAuto:    types.BoolValue(network.DHCPDV6DNSAuto),
			DNSServers: networkDataSourceDHCPV6ServerDNSFromNetwork(ctx, diags, network),
			Lease:      types.Int64PointerValue(network.DHCPDV6LeaseTime),
			Start:      types.StringPointerValue(network.DHCPDV6Start),
			Stop:       types.StringPointerValue(network.DHCPDV6Stop),
		}
		dhcpV6ServerObj, d := types.ObjectValueFrom(
			ctx,
			dhcpV6ServerValue.AttributeTypes(),
			dhcpV6ServerValue,
		)
		diags.Append(d...)
		model.DhcpV6Server = dhcpV6ServerObj
	}

	model.WanDNS = networkDataSourceWANDNSFromNetwork(ctx, diags, network)

	model.WanEgressQOS = types.Int64PointerValue(network.WANEgressQOS)
	model.WanGateway = types.StringPointerValue(network.WANGateway)
	if network.WANGatewayV6 == "" {
		model.WanGatewayV6 = types.StringNull()
	} else {
		model.WanGatewayV6 = types.StringValue(network.WANGatewayV6)
	}
	model.WanIP = types.StringPointerValue(network.WANIP)
	model.WanNetmask = types.StringPointerValue(network.WANNetmask)
	model.WanNetworkGroup = types.StringPointerValue(network.WANNetworkGroup)
	model.WanType = types.StringPointerValue(network.WANType)
	model.WanTypeV6 = types.StringPointerValue(network.WANTypeV6)
	if network.WANUsername == "" {
		model.WanUsername = types.StringNull()
	} else {
		model.WanUsername = types.StringValue(network.WANUsername)
	}
}

// collectNonEmptyStrings returns a slice of non-empty strings from the provided values.
func collectNonEmptyStrings(vals ...string) []string {
	var result []string
	for _, v := range vals {
		if v != "" {
			result = append(result, v)
		}
	}
	return result
}

// collectNonEmptyStringPointers returns a slice of non-nil, non-empty strings from the provided pointers.
func collectNonEmptyStringPointers(ptrs ...*string) []string {
	var result []string
	for _, p := range ptrs {
		if p != nil && *p != "" {
			result = append(result, *p)
		}
	}
	return result
}

// Each function below is the read half of a per-Terraform-member mapping this
// data source's fact-coverage policy names by function; being read-only, there's no to_api half.

// stringListOrNull renders collected addresses as a list, or null when there
// are none -- every collection below ends this way, since a sparse none-set means absent, not an empty list.
func stringListOrNull(
	ctx context.Context,
	diags *diag.Diagnostics,
	values []string,
) types.List {
	if len(values) == 0 {
		return types.ListNull(types.StringType)
	}
	list, d := types.ListValueFrom(ctx, types.StringType, values)
	diags.Append(d...)
	return list
}

// networkDataSourcePurposeFromNetwork reads purpose and third_party_gateway from
// the one observed purpose field: third_party_gateway is whether that value is vlan-only.
func networkDataSourcePurposeFromNetwork(network *unifi.Network) (types.String, types.Bool) {
	return types.StringValue(network.Purpose),
		types.BoolValue(network.Purpose == unifi.PurposeVLANOnly)
}

// networkDataSourceDHCPGuardingServersFromNetwork collects dhcp_guarding.servers
// from the three observed slots, keeping only the non-empty ones.
func networkDataSourceDHCPGuardingServersFromNetwork(
	ctx context.Context,
	diags *diag.Diagnostics,
	network *unifi.Network,
) types.List {
	return stringListOrNull(ctx, diags, collectNonEmptyStrings(
		network.DHCPDIP1, network.DHCPDIP2, network.DHCPDIP3,
	))
}

// networkDataSourceDHCPServerDNSFromNetwork collects dhcp_server.dns_servers
// from the four observed slots, keeping only the non-empty ones.
func networkDataSourceDHCPServerDNSFromNetwork(
	ctx context.Context,
	diags *diag.Diagnostics,
	network *unifi.Network,
) types.List {
	return stringListOrNull(ctx, diags, collectNonEmptyStringPointers(
		network.DHCPDDNS1, network.DHCPDDNS2, network.DHCPDDNS3, network.DHCPDDNS4,
	))
}

// networkDataSourceDHCPV6ServerDNSFromNetwork collects dhcp_v6_server.dns_servers
// from the four observed slots, keeping only the non-empty ones.
func networkDataSourceDHCPV6ServerDNSFromNetwork(
	ctx context.Context,
	diags *diag.Diagnostics,
	network *unifi.Network,
) types.List {
	return stringListOrNull(ctx, diags, collectNonEmptyStringPointers(
		network.DHCPDV6DNS1, network.DHCPDV6DNS2,
		network.DHCPDV6DNS3, network.DHCPDV6DNS4,
	))
}

// networkDataSourceWINSFromNetwork reads dhcp_server.wins from an enable flag
// and two address slots, the addresses collected non-empty.
func networkDataSourceWINSFromNetwork(
	ctx context.Context,
	diags *diag.Diagnostics,
	network *unifi.Network,
) types.Object {
	value := winsModel{
		Enabled: types.BoolValue(network.DHCPDWinsEnabled),
		Addresses: stringListOrNull(ctx, diags, collectNonEmptyStringPointers(
			network.DHCPDWins1, network.DHCPDWins2,
		)),
	}
	object, d := types.ObjectValueFrom(ctx, value.AttributeTypes(), value)
	diags.Append(d...)
	return object
}

// networkDataSourceNTPServersFromNetwork reads dhcp_server.ntp_servers from
// the two observed slots, collected non-empty.
func networkDataSourceNTPServersFromNetwork(
	ctx context.Context,
	diags *diag.Diagnostics,
	network *unifi.Network,
) types.List {
	return stringListOrNull(ctx, diags, collectNonEmptyStringPointers(
		network.DHCPDNtp1, network.DHCPDNtp2,
	))
}

// networkDataSourceBootFromNetwork reads dhcp_server.boot, which groups three
// flat observed fields the wire keeps apart.
func networkDataSourceBootFromNetwork(
	ctx context.Context,
	diags *diag.Diagnostics,
	network *unifi.Network,
) types.Object {
	filename := types.StringNull()
	if network.DHCPDBootFilename != nil && *network.DHCPDBootFilename != "" {
		filename = types.StringValue(*network.DHCPDBootFilename)
	}
	value := dhcpBootModel{
		Enabled:  types.BoolValue(network.DHCPDBootEnabled),
		Server:   types.StringValue(network.DHCPDBootServer),
		Filename: filename,
	}
	object, d := types.ObjectValueFrom(ctx, value.AttributeTypes(), value)
	diags.Append(d...)
	return object
}

// networkDataSourceWANDNSFromNetwork collects wan_dns from four observed slots
// (first two pointers, last two strings), the pointer pair first so order is by slot, not representation.
func networkDataSourceWANDNSFromNetwork(
	ctx context.Context,
	diags *diag.Diagnostics,
	network *unifi.Network,
) types.List {
	servers := collectNonEmptyStringPointers(network.WANDNS1, network.WANDNS2)
	servers = append(servers, collectNonEmptyStrings(network.WANDNS3, network.WANDNS4)...)
	return stringListOrNull(ctx, diags, servers)
}
