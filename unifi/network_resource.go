package unifi

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework-timetypes/timetypes"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
	"github.com/ubiquiti-community/terraform-provider-unifi/unifi/util"
)

// dhcpBootModel describes the DHCP boot configuration.
type dhcpBootModel struct {
	Enabled  types.Bool   `tfsdk:"enabled"`
	Server   types.String `tfsdk:"server"`
	Filename types.String `tfsdk:"filename"`
}

func (m dhcpBootModel) AttributeTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"enabled":  types.BoolType,
		"server":   types.StringType,
		"filename": types.StringType,
	}
}

// winsModel describes the WINS configuration.
type winsModel struct {
	Enabled   types.Bool `tfsdk:"enabled"`
	Addresses types.List `tfsdk:"addresses"`
}

func (m winsModel) AttributeTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"enabled":   types.BoolType,
		"addresses": types.ListType{ElemType: types.StringType},
	}
}

// dhcpServerModel describes the DHCP server configuration.
type dhcpServerModel struct {
	Boot              types.Object         `tfsdk:"boot"`
	Enabled           types.Bool           `tfsdk:"enabled"`
	Start             types.String         `tfsdk:"start"`
	Stop              types.String         `tfsdk:"stop"`
	GatewayEnabled    types.Bool           `tfsdk:"gateway_enabled"`
	ConflictChecking  types.Bool           `tfsdk:"conflict_checking"`
	NtpEnabled        types.Bool           `tfsdk:"ntp_enabled"`
	TimeOffsetEnabled types.Bool           `tfsdk:"time_offset_enabled"`
	DnsEnabled        types.Bool           `tfsdk:"dns_enabled"`
	Leasetime         timetypes.GoDuration `tfsdk:"leasetime"`
	Wins              types.Object         `tfsdk:"wins"`
	WpadUrl           types.String         `tfsdk:"wpad_url"`
	TftpServer        types.String         `tfsdk:"tftp_server"`
	UnifiController   types.String         `tfsdk:"unifi_controller"`
	DnsServers        types.List           `tfsdk:"dns_servers"`
	NtpServers        types.List           `tfsdk:"ntp_servers"`
}

func (m dhcpServerModel) AttributeTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"boot":                types.ObjectType{AttrTypes: dhcpBootModel{}.AttributeTypes()},
		"enabled":             types.BoolType,
		"start":               types.StringType,
		"stop":                types.StringType,
		"gateway_enabled":     types.BoolType,
		"conflict_checking":   types.BoolType,
		"ntp_enabled":         types.BoolType,
		"time_offset_enabled": types.BoolType,
		"dns_enabled":         types.BoolType,
		"leasetime":           timetypes.GoDurationType{},
		"wins":                types.ObjectType{AttrTypes: winsModel{}.AttributeTypes()},
		"wpad_url":            types.StringType,
		"tftp_server":         types.StringType,
		"unifi_controller":    types.StringType,
		"dns_servers":         types.ListType{ElemType: types.StringType},
		"ntp_servers":         types.ListType{ElemType: types.StringType},
	}
}

type natOutboundIPAddressesModel struct {
	IPAddress       types.String `tfsdk:"ip_address"`                  // ^(([0-9]|[1-9][0-9]|1[0-9]{2}|2[0-4][0-9]|25[0-5])\.){3}([0-9]|[1-9][0-9]|1[0-9]{2}|2[0-4][0-9]|25[0-5])$|^$
	IPAddressPool   types.List   `tfsdk:"ip_address_pool,omitempty"`   // ^(([0-9]|[1-9][0-9]|1[0-9]{2}|2[0-4][0-9]|25[0-5])\.){3}([0-9]|[1-9][0-9]|1[0-9]{2}|2[0-4][0-9]|25[0-5])$|^(([0-9]|[1-9][0-9]|1[0-9]{2}|2[0-4][0-9]|25[0-5])\.){3}([0-9]|[1-9][0-9]|1[0-9]{2}|2[0-4][0-9]|25[0-5])-(([0-9]|[1-9][0-9]|1[0-9]{2}|2[0-4][0-9]|25[0-5])\.){3}([0-9]|[1-9][0-9]|1[0-9]{2}|2[0-4][0-9]|25[0-5])$
	Mode            types.String `tfsdk:"mode,omitempty"`              // all|ip_address|ip_address_pool
	WANNetworkGroup types.String `tfsdk:"wan_network_group,omitempty"` // WAN[2-9]?
}

func (d natOutboundIPAddressesModel) AttributeTypes() map[string]attr.Type {
	return natOutboundIPAddresses()
}

func natOutboundIPAddresses() map[string]attr.Type {
	return map[string]attr.Type{
		"ip_address":        types.StringType,
		"ip_address_pool":   types.ListType{ElemType: types.StringType},
		"mode":              types.StringType,
		"wan_network_group": types.StringType,
	}
}

// dhcpGuardingModel describes the DHCP guarding configuration.
type dhcpGuardingModel struct {
	Enabled types.Bool `tfsdk:"enabled"`
	Servers types.List `tfsdk:"servers"`
}

func (m dhcpGuardingModel) AttributeTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"enabled": types.BoolType,
		"servers": types.ListType{ElemType: types.StringType},
	}
}

// dhcpRelayModel describes the DHCP relay configuration.
type dhcpRelayModel struct {
	Enabled types.Bool `tfsdk:"enabled"`
	Servers types.List `tfsdk:"servers"`
}

func (d dhcpRelayModel) AttributeTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"enabled": types.BoolType,
		"servers": types.ListType{ElemType: types.StringType},
	}
}

// dhcpV6ServerModel describes the DHCPv6 server configuration.
type dhcpV6ServerModel struct {
	Enabled    types.Bool   `tfsdk:"enabled"`
	DNSAuto    types.Bool   `tfsdk:"dns_auto"`
	DNSServers types.List   `tfsdk:"dns_servers"`
	Lease      types.Int64  `tfsdk:"lease"`
	Start      types.String `tfsdk:"start"`
	Stop       types.String `tfsdk:"stop"`
}

func (m dhcpV6ServerModel) AttributeTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"enabled":     types.BoolType,
		"dns_auto":    types.BoolType,
		"dns_servers": types.ListType{ElemType: types.StringType},
		"lease":       types.Int64Type,
		"start":       types.StringType,
		"stop":        types.StringType,
	}
}

// planBoolAt reads a bool from the plan, treating null and unknown as false.
func planBoolAt(
	ctx context.Context,
	plan tfsdk.Plan,
	p path.Path,
	diags *diag.Diagnostics,
) bool {
	var v types.Bool
	diags.Append(plan.GetAttribute(ctx, p, &v)...)
	return v.ValueBool()
}

func (r *networkKitResource) ConfigValidators(
	_ context.Context,
) []resource.ConfigValidator {
	return []resource.ConfigValidator{&networkPurposeAliasConfigValidator{}}
}

// networkPurposeAliasConfigValidator refuses a config that sets third_party_gateway
// and purpose to disagree: both write the controller's single Purpose field, so a
// disagreeing pair can't be satisfied. Leaving either side unset is not a conflict: the unset one is derived.
type networkPurposeAliasConfigValidator struct{}

func (v *networkPurposeAliasConfigValidator) Description(_ context.Context) string {
	return "third_party_gateway and purpose must agree: a third-party gateway network is always vlan-only"
}

func (v *networkPurposeAliasConfigValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v *networkPurposeAliasConfigValidator) ValidateResource(
	ctx context.Context,
	req resource.ValidateConfigRequest,
	resp *resource.ValidateConfigResponse,
) {
	var thirdParty types.Bool
	var purpose types.String
	resp.Diagnostics.Append(
		req.Config.GetAttribute(ctx, path.Root("third_party_gateway"), &thirdParty)...)
	resp.Diagnostics.Append(
		req.Config.GetAttribute(ctx, path.Root("purpose"), &purpose)...)
	if resp.Diagnostics.HasError() {
		return
	}
	// Unknown means an expression this validator can't resolve; null means it's
	// left to be derived -- neither is treated as a conflict.
	if thirdParty.IsNull() || thirdParty.IsUnknown() ||
		purpose.IsNull() || purpose.IsUnknown() {
		return
	}
	if thirdParty.ValueBool() == (purpose.ValueString() == ui.PurposeVLANOnly) {
		return
	}
	resp.Diagnostics.AddError(
		"Conflicting network purpose",
		fmt.Sprintf(
			"third_party_gateway = %t and purpose = %q cannot both hold: the "+
				"controller stores one purpose per network, and a third-party "+
				"gateway network is always %q.\n\n"+
				"Set third_party_gateway = %t, or change purpose to %q, or drop "+
				"one of them and let it be derived from the other.",
			thirdParty.ValueBool(), purpose.ValueString(), ui.PurposeVLANOnly,
			purpose.ValueString() == ui.PurposeVLANOnly, ui.PurposeVLANOnly,
		),
	)
}

// ModifyPlan forces setting_preference to "manual" when the plan enables a
// field the controller only honors under "manual" -- on "auto" it silently
// discards dhcpguard_enabled, igmp_snooping and the dhcpd dns/ntp/time-offset
// toggles (and disables dhcp_relay by re-enabling its own DHCP server), so the
// post-apply read would otherwise contradict the plan. Only a true value forces
// the switch; an explicit setting_preference is always respected.
func (r *networkKitResource) ModifyPlan(
	ctx context.Context,
	req resource.ModifyPlanRequest,
	resp *resource.ModifyPlanResponse,
) {
	if req.Plan.Raw.IsNull() {
		return // resource is being destroyed
	}

	var configPref types.String
	resp.Diagnostics.Append(
		req.Config.GetAttribute(ctx, path.Root("setting_preference"), &configPref)...)
	if resp.Diagnostics.HasError() || !configPref.IsNull() {
		return // user set it explicitly: respect their choice
	}

	needsManual := planBoolAt(ctx, req.Plan, path.Root("igmp_snooping"), &resp.Diagnostics) ||
		planBoolAt(ctx, req.Plan,
			path.Root("dhcp_relay").AtName("enabled"), &resp.Diagnostics) ||
		planBoolAt(ctx, req.Plan,
			path.Root("dhcp_guarding").AtName("enabled"), &resp.Diagnostics) ||
		planBoolAt(ctx, req.Plan,
			path.Root("dhcp_server").AtName("dns_enabled"), &resp.Diagnostics) ||
		planBoolAt(ctx, req.Plan,
			path.Root("dhcp_server").AtName("ntp_enabled"), &resp.Diagnostics) ||
		planBoolAt(ctx, req.Plan,
			path.Root("dhcp_server").AtName("time_offset_enabled"), &resp.Diagnostics)

	if resp.Diagnostics.HasError() || !needsManual {
		return
	}

	resp.Diagnostics.Append(
		resp.Plan.SetAttribute(
			ctx,
			path.Root("setting_preference"),
			types.StringValue("manual"),
		)...)
}

// networkVLANToNetwork writes vlan into both the observed number and its enable
// flag -- vlan_enabled derives from whether vlan is configured, so neither field alone is the attribute's source.
func networkVLANToNetwork(vlan types.Int64, network *ui.Network) {
	network.VLAN = vlan.ValueInt64Pointer()
	network.VLANEnabled = !vlan.IsNull() && !vlan.IsUnknown()
}

// networkVLANFromNetwork reads the vlan number back. vlan_enabled is not read:
// a network with no vlan reports a null number, which is the same thing.
func networkVLANFromNetwork(network *ui.Network) types.Int64 {
	return types.Int64PointerValue(network.VLAN)
}

// networkPurposeToNetwork writes two released attributes onto one observed
// field: purpose is honored when configured, then overwritten to vlan-only when third_party_gateway is true (the legacy way to ask for it).
func networkPurposeToNetwork(
	purpose types.String,
	thirdPartyGateway types.Bool,
	network *ui.Network,
) {
	if !purpose.IsNull() && !purpose.IsUnknown() && purpose.ValueString() != "" {
		network.Purpose = purpose.ValueString()
	}
	if thirdPartyGateway.ValueBool() {
		network.Purpose = ui.PurposeVLANOnly
	}
}

// networkPurposeFromNetwork computes both released attributes from the one
// observed field; an empty controller purpose reads back as corporate, which is what it means.
func networkPurposeFromNetwork(network *ui.Network) (types.String, types.Bool) {
	purpose := types.StringValue(ui.PurposeCorporate)
	if network.Purpose != "" {
		purpose = types.StringValue(network.Purpose)
	}
	return purpose, types.BoolValue(network.Purpose == ui.PurposeVLANOnly)
}

// networkDHCPGuardingServersToNetwork distributes servers positionally into the
// three observed slots but does NOT clear unused ones (unlike the DNS write below), so a shorter list leaves whatever was there.
func networkDHCPGuardingServersToNetwork(
	ctx context.Context,
	diags *diag.Diagnostics,
	servers types.List,
	network *ui.Network,
) {
	if servers.IsNull() || servers.IsUnknown() {
		return
	}
	var values []string
	diags.Append(servers.ElementsAs(ctx, &values, false)...)
	if diags.HasError() {
		return
	}
	if len(values) > 0 {
		network.DHCPDIP1 = values[0]
	}
	if len(values) > 1 {
		network.DHCPDIP2 = values[1]
	}
	if len(values) > 2 {
		network.DHCPDIP3 = values[2]
	}
}

// networkDHCPGuardingServersFromNetwork collects the three observed slots back
// into the one released list, keeping only the non-empty ones.
func networkDHCPGuardingServersFromNetwork(
	ctx context.Context,
	diags *diag.Diagnostics,
	network *ui.Network,
) types.List {
	return stringListOrNull(ctx, diags, collectNonEmptyStrings(
		network.DHCPDIP1, network.DHCPDIP2, network.DHCPDIP3,
	))
}

// networkDHCPServerDNSToNetwork distributes dns_servers positionally into the
// four observed slots, clearing unused trailing ones. A fifth server never
// reaches here (schema validation already rejects it); pairing with the From
// half below compacts, so a gap arriving from elsewhere shifts a value's slot on a round trip.
func networkDHCPServerDNSToNetwork(
	ctx context.Context,
	diags *diag.Diagnostics,
	dnsServers types.List,
	network *ui.Network,
) {
	slots := []**string{
		&network.DHCPDDNS1, &network.DHCPDDNS2, &network.DHCPDDNS3, &network.DHCPDDNS4,
	}
	if dnsServers.IsNull() || dnsServers.IsUnknown() {
		for _, slot := range slots {
			*slot = util.Ptr("")
		}
		return
	}
	var values []string
	diags.Append(dnsServers.ElementsAs(ctx, &values, false)...)
	if diags.HasError() {
		return
	}
	for i, slot := range slots {
		if i < len(values) {
			*slot = util.Ptr(values[i])
			continue
		}
		*slot = util.Ptr("")
	}
}

// networkDHCPServerDNSFromNetwork collects the four observed slots back into
// the one released list, keeping only the non-empty ones (compacted -- see the write half).
func networkDHCPServerDNSFromNetwork(
	ctx context.Context,
	diags *diag.Diagnostics,
	network *ui.Network,
) types.List {
	return stringListOrNull(ctx, diags, collectNonEmptyStringPointers(
		network.DHCPDDNS1, network.DHCPDDNS2, network.DHCPDDNS3, network.DHCPDDNS4,
	))
}

// networkDHCPV6ServerDNSToNetwork distributes dns_servers positionally into the
// four observed slots, clearing the trailing ones; the same fifth-server truncation as the v4 slots applies.
func networkDHCPV6ServerDNSToNetwork(
	ctx context.Context,
	diags *diag.Diagnostics,
	dnsServers types.List,
	network *ui.Network,
) {
	slots := []**string{
		&network.DHCPDV6DNS1, &network.DHCPDV6DNS2,
		&network.DHCPDV6DNS3, &network.DHCPDV6DNS4,
	}
	if dnsServers.IsNull() || dnsServers.IsUnknown() {
		for _, slot := range slots {
			*slot = util.Ptr("")
		}
		return
	}
	var values []string
	diags.Append(dnsServers.ElementsAs(ctx, &values, false)...)
	if diags.HasError() {
		return
	}
	for i, slot := range slots {
		if i < len(values) {
			*slot = util.Ptr(values[i])
			continue
		}
		*slot = util.Ptr("")
	}
}

// networkDHCPV6ServerDNSFromNetwork collects the four observed slots back into
// the one released list, keeping only the non-empty ones.
func networkDHCPV6ServerDNSFromNetwork(
	ctx context.Context,
	diags *diag.Diagnostics,
	network *ui.Network,
) types.List {
	return stringListOrNull(ctx, diags, collectNonEmptyStringPointers(
		network.DHCPDV6DNS1, network.DHCPDV6DNS2,
		network.DHCPDV6DNS3, network.DHCPDV6DNS4,
	))
}

// networkNTPServersToNetwork distributes ntp_servers positionally into the two
// observed slots, clearing the trailing one it does not use; SDK wire defect
// noted at the call site -- nilIfEmpty in the encoder still squashes an
// explicit clear to omitted, same class as dns_servers (#448/7150ba56).
func networkNTPServersToNetwork(
	ctx context.Context,
	diags *diag.Diagnostics,
	ntpServers types.List,
	network *ui.Network,
) {
	slots := []**string{&network.DHCPDNtp1, &network.DHCPDNtp2}
	if ntpServers.IsNull() || ntpServers.IsUnknown() {
		for _, slot := range slots {
			*slot = util.Ptr("")
		}
		return
	}
	var values []string
	diags.Append(ntpServers.ElementsAs(ctx, &values, false)...)
	if diags.HasError() {
		return
	}
	for i, slot := range slots {
		if i < len(values) {
			*slot = util.Ptr(values[i])
			continue
		}
		*slot = util.Ptr("")
	}
}

// networkNTPServersFromNetwork collects the two observed slots back into the
// one released list, keeping only the non-empty ones (compacted).
func networkNTPServersFromNetwork(
	ctx context.Context,
	diags *diag.Diagnostics,
	network *ui.Network,
) types.List {
	return stringListOrNull(ctx, diags, collectNonEmptyStringPointers(
		network.DHCPDNtp1, network.DHCPDNtp2,
	))
}

// networkWINSToNetwork writes wins over an enable flag and two address slots,
// positionally, trailing one cleared; an absent block disables it and clears both slots.
func networkWINSToNetwork(
	ctx context.Context,
	diags *diag.Diagnostics,
	winsObject types.Object,
	network *ui.Network,
) {
	if winsObject.IsNull() || winsObject.IsUnknown() {
		network.DHCPDWinsEnabled = false
		network.DHCPDWins1 = util.Ptr("")
		network.DHCPDWins2 = util.Ptr("")
		return
	}

	var wins winsModel
	diags.Append(winsObject.As(ctx, &wins, basetypes.ObjectAsOptions{})...)
	if diags.HasError() {
		return
	}
	network.DHCPDWinsEnabled = wins.Enabled.ValueBool()

	slots := []**string{&network.DHCPDWins1, &network.DHCPDWins2}
	if wins.Addresses.IsNull() || wins.Addresses.IsUnknown() {
		for _, slot := range slots {
			*slot = util.Ptr("")
		}
		return
	}
	var addresses []string
	diags.Append(wins.Addresses.ElementsAs(ctx, &addresses, false)...)
	if diags.HasError() {
		return
	}
	for i, slot := range slots {
		if i < len(addresses) {
			*slot = util.Ptr(addresses[i])
			continue
		}
		*slot = util.Ptr("")
	}
}

// networkWINSFromNetwork reads dhcp_server.wins back from the enable flag and
// the two address slots, the addresses compacted.
func networkWINSFromNetwork(
	ctx context.Context,
	diags *diag.Diagnostics,
	network *ui.Network,
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

// networkBootToNetwork writes boot over the three flat observed fields the wire
// keeps apart; an absent block disables it and empties both strings.
func networkBootToNetwork(
	ctx context.Context,
	diags *diag.Diagnostics,
	bootObject types.Object,
	network *ui.Network,
) {
	if bootObject.IsNull() || bootObject.IsUnknown() {
		network.DHCPDBootEnabled = false
		network.DHCPDBootServer = ""
		network.DHCPDBootFilename = util.Ptr("")
		return
	}

	var boot dhcpBootModel
	diags.Append(bootObject.As(ctx, &boot, basetypes.ObjectAsOptions{})...)
	if diags.HasError() {
		return
	}
	network.DHCPDBootEnabled = boot.Enabled.ValueBool()
	if boot.Server.IsNull() || boot.Server.IsUnknown() {
		network.DHCPDBootServer = ""
	} else {
		network.DHCPDBootServer = boot.Server.ValueString()
	}
	if boot.Filename.IsNull() || boot.Filename.IsUnknown() {
		network.DHCPDBootFilename = util.Ptr("")
	} else {
		network.DHCPDBootFilename = boot.Filename.ValueStringPointer()
	}
}

// networkBootFromNetwork groups the three flat observed fields back into
// dhcp_server.boot, the empty string reading as absent for both strings.
func networkBootFromNetwork(
	ctx context.Context,
	diags *diag.Diagnostics,
	network *ui.Network,
) types.Object {
	server := types.StringNull()
	if network.DHCPDBootServer != "" {
		server = types.StringValue(network.DHCPDBootServer)
	}
	filename := types.StringNull()
	if network.DHCPDBootFilename != nil && *network.DHCPDBootFilename != "" {
		filename = types.StringValue(*network.DHCPDBootFilename)
	}
	value := dhcpBootModel{
		Enabled:  types.BoolValue(network.DHCPDBootEnabled),
		Server:   server,
		Filename: filename,
	}
	object, d := types.ObjectValueFrom(ctx, value.AttributeTypes(), value)
	diags.Append(d...)
	return object
}

// ValidateConfig warns when the configuration sets a value the controller will
// not receive for this network's purpose: go-unifi serializes a Network through
// one of seven per-purpose structs, and a vlan-only network silently discards 44
// of the 51 attributes this resource exposes (corporate and guest drop 4 each).
// At plan time, so a still-unknown attribute goes unreported -- a miss, not a false alarm.
func (r *networkKitResource) ValidateConfig(
	ctx context.Context,
	req resource.ValidateConfigRequest,
	resp *resource.ValidateConfigResponse,
) {
	var model netModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &model)...)
	if resp.Diagnostics.HasError() {
		return
	}
	network, diags := r.Spec.ToSDK(ctx, &model)
	if diags.HasError() || network == nil {
		return
	}
	// ToSDK doesn't derive purpose (BeforeSend does, and hasn't run yet), so the
	// same derivation happens here -- otherwise the warning names " network" and hides which purpose dropped what.
	network.Purpose = ui.PurposeCorporate
	networkPurposeToNetwork(model.Purpose, model.ThirdPartyGateway, network)
	resp.Diagnostics.Append(droppedOnWrite(network.Purpose+" network", network)...)
}
