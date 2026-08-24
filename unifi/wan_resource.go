package unifi

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/list"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/identityschema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	"github.com/ubiquiti-community/go-unifi/unifi"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/listresource_wan"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_wan"
	"github.com/ubiquiti-community/terraform-provider-unifi/unifi/util"
)

// Ensure provider defined types fully satisfy framework interfaces.
var (
	_ resource.Resource                = &wanResource{}
	_ resource.ResourceWithImportState = &wanResource{}
	_ resource.ResourceWithIdentity    = &wanResource{}
)

// Ensure provider defined types fully satisfy list interfaces.
var (
	_ list.ListResource              = &wanResource{}
	_ list.ListResourceWithConfigure = &wanResource{}
)

func NewWANResource() resource.Resource {
	return &wanResource{}
}

func NewWANListResource() list.ListResource {
	return &wanResource{}
}

// wanResource defines the resource implementation.
type wanResource struct {
	client *Client
}

// wanResourceModel describes the resource data model.
type wanResourceModel struct {
	ID           types.String `tfsdk:"id"`
	Site         types.String `tfsdk:"site"`
	Name         types.String `tfsdk:"name"`
	NetworkGroup types.String `tfsdk:"networkgroup"`

	// WAN Type Settings
	Type   types.String `tfsdk:"type"`
	TypeV6 types.String `tfsdk:"type_v6"`

	// VLAN Settings
	Vlan types.Object `tfsdk:"vlan"`

	// QoS Settings
	EgressQoS types.Object `tfsdk:"egress_qos"`

	// DNS Settings
	DNS types.Object `tfsdk:"dns"`

	// DHCP Settings
	DHCP   types.Object `tfsdk:"dhcp"`
	DHCPv6 types.Object `tfsdk:"dhcpv6"`

	// Smart Queue Settings
	SmartQ types.Object `tfsdk:"smartq"`

	// UPnP Settings
	UPnP types.Object `tfsdk:"upnp"`

	// Load Balance Settings
	LoadBalance types.Object `tfsdk:"load_balance"`

	// IGMP Settings
	IGMPProxy types.Object `tfsdk:"igmp_proxy"`

	// Additional Settings
	ReportWANEvent        types.Bool   `tfsdk:"report_wan_event"`
	Enabled               types.Bool   `tfsdk:"enabled"`
	IPAliases             types.List   `tfsdk:"ip_aliases"`
	SettingPreference     types.String `tfsdk:"setting_preference"`
	IPv6SettingPreference types.String `tfsdk:"ipv6_setting_preference"`
	SingleNetworkLAN      types.String `tfsdk:"single_network_lan"`
	MACOverrideEnabled    types.Bool   `tfsdk:"mac_override_enabled"`
	DsliteRemoteHost      types.String `tfsdk:"wan_dslite_remote_host"`
	DsliteRemoteHostAuto  types.Bool   `tfsdk:"wan_dslite_remote_host_auto"`

	// Provider Capabilities
	ProviderCapabilities types.Object `tfsdk:"provider_capabilities"`

	Timeouts timeouts.Value `tfsdk:"timeouts"`
}

// wanListConfigModel describes the list configuration model.
type wanListConfigModel struct {
	Site   types.String `tfsdk:"site"`
	Filter types.List   `tfsdk:"filter"`
}

// wanListFilterModel represents a single name/value filter entry.
type wanListFilterModel struct {
	Name  types.String `tfsdk:"name"`
	Value types.String `tfsdk:"value"`
}

// vlanModel describes the VLAN configuration.
type vlanModel struct {
	Enabled types.Bool  `tfsdk:"enabled"`
	ID      types.Int64 `tfsdk:"id"`
}

func (m vlanModel) AttributeTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"enabled": types.BoolType,
		"id":      types.Int64Type,
	}
}

// egressQosModel describes the Egress QoS configuration.
type egressQosModel struct {
	Enabled  types.Bool  `tfsdk:"enabled"`
	Priority types.Int64 `tfsdk:"priority"`
}

func (m egressQosModel) AttributeTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"enabled":  types.BoolType,
		"priority": types.Int64Type,
	}
}

// smartqModel describes the Smart Queue configuration.
type smartqModel struct {
	Enabled  types.Bool  `tfsdk:"enabled"`
	UpRate   types.Int64 `tfsdk:"up_rate"`
	DownRate types.Int64 `tfsdk:"down_rate"`
}

func (m smartqModel) AttributeTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"enabled":   types.BoolType,
		"up_rate":   types.Int64Type,
		"down_rate": types.Int64Type,
	}
}

// providerCapabilitiesModel describes the provider capabilities nested object.
type providerCapabilitiesModel struct {
	DownloadKbps types.Int64 `tfsdk:"download_kilobits_per_second"`
	UploadKbps   types.Int64 `tfsdk:"upload_kilobits_per_second"`
}

func (m providerCapabilitiesModel) AttributeTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"download_kilobits_per_second": types.Int64Type,
		"upload_kilobits_per_second":   types.Int64Type,
	}
}

// dhcpOptionModel describes a DHCPv6 option.
type dhcpOptionModel struct {
	OptionNumber types.Int64  `tfsdk:"option_number"`
	Value        types.String `tfsdk:"value"`
}

func (m dhcpOptionModel) AttributeTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"option_number": types.Int64Type,
		"value":         types.StringType,
	}
}

// dnsModel describes the DNS configuration nested object.
type dnsModel struct {
	Primary        types.String `tfsdk:"primary"`
	Secondary      types.String `tfsdk:"secondary"`
	IPv6Primary    types.String `tfsdk:"ipv6_primary"`
	IPv6Secondary  types.String `tfsdk:"ipv6_secondary"`
	Preference     types.String `tfsdk:"preference"`
	IPv6Preference types.String `tfsdk:"ipv6_preference"`
}

func (m dnsModel) AttributeTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"primary":         types.StringType,
		"secondary":       types.StringType,
		"ipv6_primary":    types.StringType,
		"ipv6_secondary":  types.StringType,
		"preference":      types.StringType,
		"ipv6_preference": types.StringType,
	}
}

// upnpModel describes the UPnP configuration nested object.
type upnpModel struct {
	Enabled       types.Bool   `tfsdk:"enabled"`
	WANInterface  types.String `tfsdk:"wan_interface"`
	NatPMPEnabled types.Bool   `tfsdk:"nat_pmp_enabled"`
	SecureMode    types.Bool   `tfsdk:"secure_mode"`
}

func (m upnpModel) AttributeTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"enabled":         types.BoolType,
		"wan_interface":   types.StringType,
		"nat_pmp_enabled": types.BoolType,
		"secure_mode":     types.BoolType,
	}
}

// loadBalanceModel describes the load balance configuration nested object.
type loadBalanceModel struct {
	Type             types.String `tfsdk:"type"`
	Weight           types.Int64  `tfsdk:"weight"`
	FailoverPriority types.Int64  `tfsdk:"failover_priority"`
}

func (m loadBalanceModel) AttributeTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"type":              types.StringType,
		"weight":            types.Int64Type,
		"failover_priority": types.Int64Type,
	}
}

// igmpProxyModel describes the IGMP proxy configuration nested object.
type igmpProxyModel struct {
	Downstream types.String `tfsdk:"downstream"`
	Upstream   types.Bool   `tfsdk:"upstream"`
}

func (m igmpProxyModel) AttributeTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"downstream": types.StringType,
		"upstream":   types.BoolType,
	}
}

// dhcpv6WanModel describes the DHCPv6 WAN configuration nested object.
type dhcpv6WanModel struct {
	CoS            types.Int64  `tfsdk:"cos"`
	PDSize         types.Int64  `tfsdk:"pd_size"`
	PDSizeAuto     types.Bool   `tfsdk:"pd_size_auto"`
	Options        types.List   `tfsdk:"options"`
	DelegationType types.String `tfsdk:"wan_delegation_type"`
}

func (m dhcpv6WanModel) AttributeTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"cos":          types.Int64Type,
		"pd_size":      types.Int64Type,
		"pd_size_auto": types.BoolType,
		"options": types.ListType{
			ElemType: types.ObjectType{AttrTypes: dhcpOptionModel{}.AttributeTypes()},
		},
		"wan_delegation_type": types.StringType,
	}
}

// dhcpWanModel describes the DHCP WAN configuration nested object.
type dhcpWanModel struct {
	CoS     types.Int64 `tfsdk:"cos"`
	Options types.List  `tfsdk:"options"`
}

func (m dhcpWanModel) AttributeTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"cos": types.Int64Type,
		"options": types.ListType{
			ElemType: types.ObjectType{AttrTypes: dhcpOptionModel{}.AttributeTypes()},
		},
	}
}

func (r *wanResource) Metadata(
	ctx context.Context,
	req resource.MetadataRequest,
	resp *resource.MetadataResponse,
) {
	resp.TypeName = req.ProviderTypeName + "_wan"
}

// IdentitySchema implements [resource.ResourceWithIdentity].
func (r *wanResource) IdentitySchema(
	_ context.Context,
	_ resource.IdentitySchemaRequest,
	resp *resource.IdentitySchemaResponse,
) {
	resp.IdentitySchema = identityschema.Schema{
		Attributes: map[string]identityschema.Attribute{
			"id": identityschema.StringAttribute{
				RequiredForImport: true,
			},
		},
	}
}

func (r *wanResource) Schema(
	ctx context.Context,
	req resource.SchemaRequest,
	resp *resource.SchemaResponse,
) {
	resp.Schema = resource_wan.WanResourceSchema(ctx)
	// Grafted rather than generated, as everywhere else: timeouts.Attributes
	// is a call, not a literal, so the code specification cannot carry it.
	resp.Schema.Attributes["timeouts"] = timeouts.Attributes(
		ctx,
		timeouts.Opts{Create: true, Read: true, Update: true, Delete: true},
	)
}

func (r *wanResource) Configure(
	ctx context.Context,
	req resource.ConfigureRequest,
	resp *resource.ConfigureResponse,
) {
	client, ok := resourceClient(req.ProviderData, &resp.Diagnostics)
	if !ok {
		return
	}
	r.client = client
}

func (r *wanResource) Create(
	ctx context.Context,
	req resource.CreateRequest,
	resp *resource.CreateResponse,
) {
	var plan wanResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	createTimeout, timeoutDiags := plan.Timeouts.Create(ctx, 20*time.Minute)
	resp.Diagnostics.Append(timeoutDiags...)
	if resp.Diagnostics.HasError() {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, createTimeout)
	defer cancel()

	network, diags := r.modelToNetwork(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	site := plan.Site.ValueString()
	if site == "" {
		site = r.client.Site
	}

	createdNetwork, err := r.client.CreateNetwork(ctx, site, network)
	if err != nil {
		// An existing WAN in this network group is adopted and updated with the planned config.
		if strings.Contains(err.Error(), "WanConfigurationForNetworkGroupAlreadyExists") {
			createdNetwork, err = r.adoptExistingWAN(ctx, site, network)
			if err != nil {
				resp.Diagnostics.AddError(
					"Client Error",
					fmt.Sprintf("Unable to adopt existing WAN network, got error: %s", err),
				)
				return
			}

			// Reads the existing WAN's state, then overlays only what the user
			// explicitly configured -- from req.Config, which is null for unset HCL fields, unlike req.Plan which carries defaults.
			var config wanResourceModel
			resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
			if resp.Diagnostics.HasError() {
				return
			}

			var state wanResourceModel
			diags = r.networkToModel(ctx, createdNetwork, &state, site)
			resp.Diagnostics.Append(diags...)
			if resp.Diagnostics.HasError() {
				return
			}

			r.overlayConfig(&state, &config, &plan)
			state.Timeouts = plan.Timeouts

			resp.Diagnostics.Append(resp.Identity.SetAttribute(ctx, path.Root("id"), state.ID)...)
			resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
			return
		}

		resp.Diagnostics.AddError(
			"Client Error",
			fmt.Sprintf("Unable to create WAN network, got error: %s", err),
		)
		return
	}

	// For normal creation, read the API response into a fresh model
	// (not the plan, which may have unknown values for Computed fields).
	var config wanResourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var state wanResourceModel
	diags = r.networkToModel(ctx, createdNetwork, &state, site)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	r.overlayConfig(&state, &config, &plan)
	state.Timeouts = plan.Timeouts

	resp.Diagnostics.Append(resp.Identity.SetAttribute(ctx, path.Root("id"), state.ID)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// adoptExistingWAN finds the existing WAN network in the given network group and updates it.
func (r *wanResource) adoptExistingWAN(
	ctx context.Context,
	site string,
	network *unifi.Network,
) (*unifi.Network, error) {
	networks, err := r.client.ListNetwork(ctx, site)
	if err != nil {
		return nil, fmt.Errorf("listing networks: %w", err)
	}

	// Matches the WAN owning the same network group we're creating (WAN, WAN2, …),
	// not just the primary "WAN" -- otherwise a WAN2 conflict adopts the wrong interface (#334).
	wantGroup := "WAN"
	if network.WANNetworkGroup != nil && *network.WANNetworkGroup != "" {
		wantGroup = *network.WANNetworkGroup
	}
	var existing *unifi.Network
	for _, n := range networks {
		if n.Purpose == unifi.PurposeWAN && n.WANNetworkGroup != nil &&
			*n.WANNetworkGroup == wantGroup {
			existing = &n
			break
		}
	}
	if existing == nil {
		return nil, fmt.Errorf(
			"existing WAN network (group %s) not found despite creation conflict",
			wantGroup,
		)
	}

	network.ID = existing.ID
	// Masked (see wanWireFields): a whole-object PUT here blanked every field the
	// provider doesn't model, including PPPoE credentials, since this overwrites an existing controller-configured WAN.
	return r.client.UpdateNetworkFields(ctx, site, network, wanWireFields(network)...)
}

// overlayConfig applies only explicitly-configured values from config onto
// state: null config means keep state (from the API); non-null means use plan (validated/transformed).
func (r *wanResource) overlayConfig(
	state *wanResourceModel,
	config *wanResourceModel,
	plan *wanResourceModel,
) {
	if !config.Name.IsNull() {
		state.Name = plan.Name
	}
	if !config.Type.IsNull() {
		state.Type = plan.Type
	}
	if !config.TypeV6.IsNull() {
		state.TypeV6 = plan.TypeV6
	}
	// Nested objects (Vlan, EgressQoS, DNS, DHCP, DHCPv6, SmartQ, UPnP, LoadBalance,
	// IGMPProxy) are NOT overlaid: on initial create the plan may carry unknown computed
	// children with no prior state, while state's API response is already resolved.
	if !config.ReportWANEvent.IsNull() {
		state.ReportWANEvent = plan.ReportWANEvent
	}
	if !config.Enabled.IsNull() {
		state.Enabled = plan.Enabled
	}
	if !config.IPAliases.IsNull() {
		state.IPAliases = plan.IPAliases
	}
	if !config.ProviderCapabilities.IsNull() {
		state.ProviderCapabilities = plan.ProviderCapabilities
	}
	// setting_preference/ipv6_setting_preference are Computed: the controller may
	// echo back "auto" even when the user asked for "manual", so an explicit value is kept to match the plan (mirrors applyPlanToState on Update).
	if !config.SettingPreference.IsNull() {
		state.SettingPreference = plan.SettingPreference
	}
	if !config.IPv6SettingPreference.IsNull() {
		state.IPv6SettingPreference = plan.IPv6SettingPreference
	}
	// The controller can force wan_dslite_remote_host_auto back to true server-side;
	// keep the user's value so the create result matches the plan (#281).
	if !config.DsliteRemoteHostAuto.IsNull() {
		state.DsliteRemoteHostAuto = plan.DsliteRemoteHostAuto
	}
	if !config.DsliteRemoteHost.IsNull() {
		state.DsliteRemoteHost = plan.DsliteRemoteHost
	}
}

func (r *wanResource) Read(
	ctx context.Context,
	req resource.ReadRequest,
	resp *resource.ReadResponse,
) {
	var state wanResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	readTimeout, timeoutDiags := state.Timeouts.Read(ctx, 20*time.Minute)
	resp.Diagnostics.Append(timeoutDiags...)
	if resp.Diagnostics.HasError() {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, readTimeout)
	defer cancel()

	site := state.Site.ValueString()
	if site == "" {
		site = r.client.Site
	}

	var network *unifi.Network
	var err error

	if state.ID.IsNull() || state.ID.IsUnknown() {
		network, err = r.client.GetNetworkByName(ctx, site, state.Name.ValueString())
		if err != nil {
			resp.Diagnostics.AddError(
				"Client Error",
				fmt.Sprintf("Unable to read WAN network, got error: %s", err),
			)
			return
		}
	} else {
		network, err = r.client.GetNetwork(ctx, site, state.ID.ValueString())
		if err != nil {
			resp.Diagnostics.AddError(
				"Client Error",
				fmt.Sprintf("Unable to read WAN network, got error: %s", err),
			)
			return
		}
	}

	diags := r.networkToModel(ctx, network, &state, site)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.Identity.SetAttribute(ctx, path.Root("id"), state.ID)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *wanResource) Update(
	ctx context.Context,
	req resource.UpdateRequest,
	resp *resource.UpdateResponse,
) {
	var state wanResourceModel
	var plan wanResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	updateTimeout, timeoutDiags := plan.Timeouts.Update(ctx, 20*time.Minute)
	resp.Diagnostics.Append(timeoutDiags...)
	if resp.Diagnostics.HasError() {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, updateTimeout)
	defer cancel()

	r.applyPlanToState(ctx, &plan, &state)
	state.Timeouts = plan.Timeouts

	network, diags := r.modelToNetwork(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	site := state.Site.ValueString()
	if site == "" {
		site = r.client.Site
	}

	network.ID = state.ID.ValueString()
	// MASKED, and this is the frequent site: Update runs on every apply that
	// touches a WAN, where adoptExistingWAN runs once.
	updatedNetwork, err := r.client.UpdateNetworkFields(
		ctx, site, network, wanWireFields(network)...)
	if err != nil {
		resp.Diagnostics.AddError(
			"Client Error",
			fmt.Sprintf("Unable to update WAN network, got error: %s", err),
		)
		return
	}

	diags = r.networkToModel(ctx, updatedNetwork, &state, site)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Re-asserts the planned DS-Lite values: the controller forces
	// wan_dslite_remote_host_auto to true on AFTR auto-detection, so the read above
	// would otherwise contradict a plan that set false (#281) -- networkToModel runs first, so its value would win without this.
	if !plan.DsliteRemoteHostAuto.IsNull() && !plan.DsliteRemoteHostAuto.IsUnknown() {
		state.DsliteRemoteHostAuto = plan.DsliteRemoteHostAuto
	}
	if !plan.DsliteRemoteHost.IsNull() && !plan.DsliteRemoteHost.IsUnknown() {
		state.DsliteRemoteHost = plan.DsliteRemoteHost
	}

	resp.Diagnostics.Append(resp.Identity.SetAttribute(ctx, path.Root("id"), state.ID)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// applyPlanToState merges plan values into state, preserving state values where plan is null/unknown.
func (r *wanResource) applyPlanToState(
	_ context.Context,
	plan *wanResourceModel,
	state *wanResourceModel,
) {
	// Apply plan values to state, but only if plan value is not null/unknown
	if !plan.Name.IsNull() && !plan.Name.IsUnknown() {
		state.Name = plan.Name
	}
	if !plan.Type.IsNull() && !plan.Type.IsUnknown() {
		state.Type = plan.Type
	}
	if !plan.TypeV6.IsNull() && !plan.TypeV6.IsUnknown() {
		state.TypeV6 = plan.TypeV6
	}
	if !plan.Vlan.IsNull() && !plan.Vlan.IsUnknown() {
		state.Vlan = plan.Vlan
	}
	if !plan.EgressQoS.IsNull() && !plan.EgressQoS.IsUnknown() {
		state.EgressQoS = plan.EgressQoS
	}
	if !plan.DNS.IsNull() && !plan.DNS.IsUnknown() {
		state.DNS = plan.DNS
	}
	if !plan.DHCP.IsNull() && !plan.DHCP.IsUnknown() {
		state.DHCP = plan.DHCP
	}
	if !plan.DHCPv6.IsNull() && !plan.DHCPv6.IsUnknown() {
		state.DHCPv6 = plan.DHCPv6
	}
	if !plan.SmartQ.IsNull() && !plan.SmartQ.IsUnknown() {
		state.SmartQ = plan.SmartQ
	}
	if !plan.UPnP.IsNull() && !plan.UPnP.IsUnknown() {
		state.UPnP = plan.UPnP
	}
	if !plan.LoadBalance.IsNull() && !plan.LoadBalance.IsUnknown() {
		state.LoadBalance = plan.LoadBalance
	}
	if !plan.IGMPProxy.IsNull() && !plan.IGMPProxy.IsUnknown() {
		state.IGMPProxy = plan.IGMPProxy
	}
	if !plan.ReportWANEvent.IsNull() && !plan.ReportWANEvent.IsUnknown() {
		state.ReportWANEvent = plan.ReportWANEvent
	}
	if !plan.Enabled.IsNull() && !plan.Enabled.IsUnknown() {
		state.Enabled = plan.Enabled
	}
	if !plan.IPAliases.IsNull() && !plan.IPAliases.IsUnknown() {
		state.IPAliases = plan.IPAliases
	}
	if !plan.ProviderCapabilities.IsNull() && !plan.ProviderCapabilities.IsUnknown() {
		state.ProviderCapabilities = plan.ProviderCapabilities
	}
	if !plan.SettingPreference.IsNull() && !plan.SettingPreference.IsUnknown() {
		state.SettingPreference = plan.SettingPreference
	}
	if !plan.IPv6SettingPreference.IsNull() && !plan.IPv6SettingPreference.IsUnknown() {
		state.IPv6SettingPreference = plan.IPv6SettingPreference
	}
	if !plan.SingleNetworkLAN.IsNull() && !plan.SingleNetworkLAN.IsUnknown() {
		state.SingleNetworkLAN = plan.SingleNetworkLAN
	}
	if !plan.MACOverrideEnabled.IsNull() && !plan.MACOverrideEnabled.IsUnknown() {
		state.MACOverrideEnabled = plan.MACOverrideEnabled
	}
	if !plan.DsliteRemoteHost.IsNull() && !plan.DsliteRemoteHost.IsUnknown() {
		state.DsliteRemoteHost = plan.DsliteRemoteHost
	}
	if !plan.DsliteRemoteHostAuto.IsNull() && !plan.DsliteRemoteHostAuto.IsUnknown() {
		state.DsliteRemoteHostAuto = plan.DsliteRemoteHostAuto
	}
}

func (r *wanResource) Delete(
	ctx context.Context,
	req resource.DeleteRequest,
	resp *resource.DeleteResponse,
) {
	var state wanResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	deleteTimeout, timeoutDiags := state.Timeouts.Delete(ctx, 20*time.Minute)
	resp.Diagnostics.Append(timeoutDiags...)
	if resp.Diagnostics.HasError() {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, deleteTimeout)
	defer cancel()

	site := state.Site.ValueString()
	if site == "" {
		site = r.client.Site
	}

	networkName := state.Name.ValueString()
	networkID := state.ID.ValueString()
	err := r.client.DeleteNetwork(ctx, site, networkID, networkName)
	if err != nil {
		// WAN networks cannot be deleted from the controller; removing from state only.
		if strings.Contains(err.Error(), "NoDelete") {
			return
		}
		resp.Diagnostics.AddError(
			"Client Error",
			fmt.Sprintf("Unable to delete WAN network, got error: %s", err),
		)
		return
	}
}

func (r *wanResource) ImportState(
	ctx context.Context,
	req resource.ImportStateRequest,
	resp *resource.ImportStateResponse,
) {
	idParts := strings.Split(req.ID, ":")
	if len(idParts) == 2 {
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("site"), idParts[0])...)
		req.ID = idParts[1]
	}

	rootAttributeName := "name"
	if strings.HasPrefix(req.ID, "name=") {
		req.ID = strings.TrimPrefix(req.ID, "name=")
	} else if regexp.MustCompile(`^[0-9a-f]{24}$`).MatchString(req.ID) {
		rootAttributeName = "id"
	}

	resource.ImportStatePassthroughID(ctx, path.Root(rootAttributeName), req, resp)
}

// modelToNetwork converts from Terraform model to unifi.Network: only known
// values are set, so marshalWAN omits null/unknown fields via omitempty.
func (r *wanResource) modelToNetwork(
	ctx context.Context,
	model *wanResourceModel,
) (*unifi.Network, diag.Diagnostics) {
	var diags diag.Diagnostics

	// Preserves the interface's WAN network group (WAN, WAN2, …): hard-coding "WAN"
	// collides with a secondary uplink's PUT and the controller rejects it (#334); HiddenID mirrors the group, defaulting to "WAN".
	networkGroup := "WAN"
	if !model.NetworkGroup.IsNull() && !model.NetworkGroup.IsUnknown() &&
		model.NetworkGroup.ValueString() != "" {
		networkGroup = model.NetworkGroup.ValueString()
	}

	network := &unifi.Network{
		Name:            model.Name.ValueStringPointer(),
		Purpose:         unifi.PurposeWAN, // Statically set to "wan"
		WANNetworkGroup: util.Ptr(networkGroup),
		HiddenID:        networkGroup,
		Enabled:         model.Enabled.ValueBool(),
	}

	// Neither type is guaranteed known, so both guards carry weight: a Default made
	// this write unconditional before, silently stamping an existing static WAN back to dhcp when a config omitted type.
	if !model.Type.IsNull() && !model.Type.IsUnknown() {
		network.WANType = model.Type.ValueStringPointer()
	}
	if !model.TypeV6.IsNull() && !model.TypeV6.IsUnknown() {
		network.WANTypeV6 = model.TypeV6.ValueStringPointer()
	}

	// DNS Settings
	if !model.DNS.IsNull() && !model.DNS.IsUnknown() {
		var dns dnsModel
		d := model.DNS.As(ctx, &dns, basetypes.ObjectAsOptions{})
		diags.Append(d...)
		if !diags.HasError() {
			if !dns.Primary.IsNull() && !dns.Primary.IsUnknown() {
				network.WANDNS1 = dns.Primary.ValueStringPointer()
			}
			if !dns.Secondary.IsNull() && !dns.Secondary.IsUnknown() {
				network.WANDNS2 = dns.Secondary.ValueStringPointer()
			}
			if !dns.IPv6Primary.IsNull() && !dns.IPv6Primary.IsUnknown() {
				network.WANIPV6DNS1 = dns.IPv6Primary.ValueStringPointer()
			}
			if !dns.IPv6Secondary.IsNull() && !dns.IPv6Secondary.IsUnknown() {
				network.WANIPV6DNS2 = dns.IPv6Secondary.ValueStringPointer()
			}
			if !dns.Preference.IsNull() && !dns.Preference.IsUnknown() {
				network.WANDNSPreference = dns.Preference.ValueStringPointer()
			}
			if !dns.IPv6Preference.IsNull() && !dns.IPv6Preference.IsUnknown() {
				network.WANIPV6DNSPreference = dns.IPv6Preference.ValueStringPointer()
			}
		}
	}

	// Handle VLAN configuration
	if !model.Vlan.IsNull() && !model.Vlan.IsUnknown() {
		var vlan vlanModel
		d := model.Vlan.As(ctx, &vlan, basetypes.ObjectAsOptions{})
		diags.Append(d...)
		if !diags.HasError() {
			network.WANVLANEnabled = vlan.Enabled.ValueBool()
			network.WANVLAN = vlan.ID.ValueInt64Pointer()
		}
	}

	// Handle Egress QoS configuration
	if !model.EgressQoS.IsNull() && !model.EgressQoS.IsUnknown() {
		var egressQos egressQosModel
		d := model.EgressQoS.As(ctx, &egressQos, basetypes.ObjectAsOptions{})
		diags.Append(d...)
		if !diags.HasError() {
			network.WANEgressQOSEnabled = egressQos.Enabled.ValueBoolPointer()
			network.WANEgressQOS = egressQos.Priority.ValueInt64Pointer()
		}
	}

	// DHCP Settings
	if !model.DHCP.IsNull() && !model.DHCP.IsUnknown() {
		var dhcp dhcpWanModel
		d := model.DHCP.As(ctx, &dhcp, basetypes.ObjectAsOptions{})
		diags.Append(d...)
		if !diags.HasError() {
			if !dhcp.CoS.IsNull() && !dhcp.CoS.IsUnknown() {
				network.WANDHCPCos = dhcp.CoS.ValueInt64Pointer()
			}
			if !dhcp.Options.IsNull() && !dhcp.Options.IsUnknown() {
				var dhcpOptionsModel []dhcpOptionModel
				diags.Append(dhcp.Options.ElementsAs(ctx, &dhcpOptionsModel, false)...)
				if !diags.HasError() {
					dhcpOptions := make([]unifi.NetworkWANDHCPOptions, len(dhcpOptionsModel))
					for i, opt := range dhcpOptionsModel {
						dhcpOptions[i] = unifi.NetworkWANDHCPOptions{
							OptionNumber: opt.OptionNumber.ValueInt64Pointer(),
							Value:        opt.Value.ValueStringPointer(),
						}
					}
					network.WANDHCPOptions = dhcpOptions
				}
			}
		}
	}

	// DHCPv6 Settings
	if !model.DHCPv6.IsNull() && !model.DHCPv6.IsUnknown() {
		var dhcpv6 dhcpv6WanModel
		d := model.DHCPv6.As(ctx, &dhcpv6, basetypes.ObjectAsOptions{})
		diags.Append(d...)
		if !diags.HasError() {
			if !dhcpv6.CoS.IsNull() && !dhcpv6.CoS.IsUnknown() {
				network.WANDHCPv6Cos = dhcpv6.CoS.ValueInt64Pointer()
			}
			if !dhcpv6.PDSize.IsNull() && !dhcpv6.PDSize.IsUnknown() {
				network.WANDHCPv6PDSize = dhcpv6.PDSize.ValueInt64Pointer()
			}
			if !dhcpv6.PDSizeAuto.IsNull() && !dhcpv6.PDSizeAuto.IsUnknown() {
				network.WANDHCPv6PDSizeAuto = dhcpv6.PDSizeAuto.ValueBool()
			}
			if !dhcpv6.DelegationType.IsNull() && !dhcpv6.DelegationType.IsUnknown() {
				network.IPV6WANDelegationType = dhcpv6.DelegationType.ValueStringPointer()
			}
			if !dhcpv6.Options.IsNull() && !dhcpv6.Options.IsUnknown() {
				var dhcpV6Options []dhcpOptionModel
				diags.Append(dhcpv6.Options.ElementsAs(ctx, &dhcpV6Options, false)...)
				if !diags.HasError() {
					network.WANDHCPv6Options = make(
						[]unifi.NetworkWANDHCPv6Options,
						len(dhcpV6Options),
					)
					for i, opt := range dhcpV6Options {
						network.WANDHCPv6Options[i] = unifi.NetworkWANDHCPv6Options{
							OptionNumber: opt.OptionNumber.ValueInt64Pointer(),
							Value:        opt.Value.ValueStringPointer(),
						}
					}
				}
			}
		}
	}

	// Handle Smart Queue configuration
	if !model.SmartQ.IsNull() && !model.SmartQ.IsUnknown() {
		var smartq smartqModel
		d := model.SmartQ.As(ctx, &smartq, basetypes.ObjectAsOptions{})
		diags.Append(d...)
		if !diags.HasError() {
			network.WANSmartQEnabled = smartq.Enabled.ValueBool()
			network.WANSmartQUpRate = smartq.UpRate.ValueInt64Pointer()
			network.WANSmartQDownRate = smartq.DownRate.ValueInt64Pointer()
		}
	}

	// UPnP Settings
	if !model.UPnP.IsNull() && !model.UPnP.IsUnknown() {
		var upnp upnpModel
		d := model.UPnP.As(ctx, &upnp, basetypes.ObjectAsOptions{})
		diags.Append(d...)
		if !diags.HasError() {
			if !upnp.Enabled.IsNull() && !upnp.Enabled.IsUnknown() {
				network.UPnPEnabled = upnp.Enabled.ValueBoolPointer()
			}
			if !upnp.WANInterface.IsNull() && !upnp.WANInterface.IsUnknown() {
				network.UPnPWANInterface = upnp.WANInterface.ValueStringPointer()
			}
			if !upnp.NatPMPEnabled.IsNull() && !upnp.NatPMPEnabled.IsUnknown() {
				network.UPnPNatPMPEnabled = upnp.NatPMPEnabled.ValueBoolPointer()
			}
			if !upnp.SecureMode.IsNull() && !upnp.SecureMode.IsUnknown() {
				network.UPnPSecureMode = upnp.SecureMode.ValueBoolPointer()
			}
		}
	}

	// Load Balance Settings
	if !model.LoadBalance.IsNull() && !model.LoadBalance.IsUnknown() {
		var lb loadBalanceModel
		d := model.LoadBalance.As(ctx, &lb, basetypes.ObjectAsOptions{})
		diags.Append(d...)
		if !diags.HasError() {
			if !lb.Type.IsNull() && !lb.Type.IsUnknown() {
				network.WANLoadBalanceType = lb.Type.ValueStringPointer()
			}
			if !lb.Weight.IsNull() && !lb.Weight.IsUnknown() {
				network.WANLoadBalanceWeight = lb.Weight.ValueInt64Pointer()
			}
			if !lb.FailoverPriority.IsNull() && !lb.FailoverPriority.IsUnknown() {
				network.WANFailoverPriority = lb.FailoverPriority.ValueInt64Pointer()
			}
		}
	}

	// IGMP Settings
	if !model.IGMPProxy.IsNull() && !model.IGMPProxy.IsUnknown() {
		var igmp igmpProxyModel
		d := model.IGMPProxy.As(ctx, &igmp, basetypes.ObjectAsOptions{})
		diags.Append(d...)
		if !diags.HasError() {
			if !igmp.Downstream.IsNull() && !igmp.Downstream.IsUnknown() {
				network.IGMPProxyFor = igmp.Downstream.ValueStringPointer()
			}
			if !igmp.Upstream.IsNull() && !igmp.Upstream.IsUnknown() {
				network.IGMPProxyUpstream = igmp.Upstream.ValueBool()
			}
		}
	}

	// Additional Settings
	if !model.ReportWANEvent.IsNull() && !model.ReportWANEvent.IsUnknown() {
		network.ReportWANEvent = model.ReportWANEvent.ValueBool()
	}
	if !model.SettingPreference.IsNull() && !model.SettingPreference.IsUnknown() {
		network.SettingPreference = model.SettingPreference.ValueStringPointer()
	}
	if !model.IPv6SettingPreference.IsNull() && !model.IPv6SettingPreference.IsUnknown() {
		network.IPV6SettingPreference = model.IPv6SettingPreference.ValueStringPointer()
	}
	if !model.SingleNetworkLAN.IsNull() && !model.SingleNetworkLAN.IsUnknown() {
		network.SingleNetworkLan = model.SingleNetworkLAN.ValueStringPointer()
	}
	if !model.MACOverrideEnabled.IsNull() && !model.MACOverrideEnabled.IsUnknown() {
		network.MACOverrideEnabled = model.MACOverrideEnabled.ValueBool()
	}
	if !model.DsliteRemoteHost.IsNull() && !model.DsliteRemoteHost.IsUnknown() {
		network.WANDsliteRemoteHost = model.DsliteRemoteHost.ValueStringPointer()
	}
	if !model.DsliteRemoteHostAuto.IsNull() && !model.DsliteRemoteHostAuto.IsUnknown() {
		network.WANDsliteRemoteHostAuto = model.DsliteRemoteHostAuto.ValueBool()
	}

	if !model.IPAliases.IsNull() && !model.IPAliases.IsUnknown() {
		var ipAliases []string
		diags.Append(model.IPAliases.ElementsAs(ctx, &ipAliases, false)...)
		network.WANIPAliases = ipAliases
	}

	// Provider Capabilities
	if !model.ProviderCapabilities.IsNull() && !model.ProviderCapabilities.IsUnknown() {
		var providerCaps providerCapabilitiesModel
		diags.Append(
			model.ProviderCapabilities.As(ctx, &providerCaps, basetypes.ObjectAsOptions{})...)
		if !diags.HasError() {
			network.WANProviderCapabilities = &unifi.NetworkWANProviderCapabilities{
				DownloadKilobitsPerSecond: providerCaps.DownloadKbps.ValueInt64Pointer(),
				UploadKilobitsPerSecond:   providerCaps.UploadKbps.ValueInt64Pointer(),
			}
		}
	}

	return network, diags
}

// dnsAddrValue maps a controller WAN DNS address pointer to a Terraform value,
// treating a nil pointer and an empty string alike as null -- the controller
// returns "" for this unconfigured Optional field, which would otherwise conflict with a planned null (#333).
func dnsAddrValue(p *string) types.String {
	if p == nil || *p == "" {
		return types.StringNull()
	}
	return types.StringValue(*p)
}

// networkToModel converts from unifi.Network to Terraform model.
func (r *wanResource) networkToModel(
	ctx context.Context,
	network *unifi.Network,
	model *wanResourceModel,
	site string,
) diag.Diagnostics {
	var diags diag.Diagnostics

	model.ID = types.StringValue(network.ID)
	model.Site = types.StringValue(site)
	model.Name = types.StringPointerValue(network.Name)

	// Preserve the WAN network group (WAN, WAN2, …) so updates target the right
	// interface instead of always defaulting to "WAN" (#334).
	if network.WANNetworkGroup != nil && *network.WANNetworkGroup != "" {
		model.NetworkGroup = types.StringValue(*network.WANNetworkGroup)
	} else {
		model.NetworkGroup = types.StringValue("WAN")
	}

	// WAN Type Settings — only overwrite when API returns a value
	if network.WANType != nil {
		model.Type = types.StringValue(*network.WANType)
	}
	if network.WANTypeV6 != nil {
		model.TypeV6 = types.StringValue(*network.WANTypeV6)
	}

	// VLAN Settings: the controller omits the VLAN id when unset; map it to the
	// schema default (0) rather than null so an imported WAN plans clean without an apply (#262).
	vlanID := int64(0)
	if network.WANVLAN != nil {
		vlanID = *network.WANVLAN
	}
	vlanValue := vlanModel{
		Enabled: types.BoolValue(network.WANVLANEnabled),
		ID:      types.Int64Value(vlanID),
	}
	vlanObj, d := types.ObjectValueFrom(ctx, vlanValue.AttributeTypes(), vlanValue)
	diags.Append(d...)
	model.Vlan = vlanObj

	// Egress QoS Settings. Every nested object below follows the same rule: only
	// create/update it if the model already has it or the API returned data for it.
	hasEgressQosData := network.WANEgressQOSEnabled != nil || network.WANEgressQOS != nil
	if !model.EgressQoS.IsNull() || hasEgressQosData {
		var currentEgressQos egressQosModel
		if !model.EgressQoS.IsNull() && !model.EgressQoS.IsUnknown() {
			d := model.EgressQoS.As(ctx, &currentEgressQos, basetypes.ObjectAsOptions{})
			diags.Append(d...)
		}
		if network.WANEgressQOSEnabled != nil {
			currentEgressQos.Enabled = types.BoolValue(*network.WANEgressQOSEnabled)
		}
		if network.WANEgressQOS != nil {
			currentEgressQos.Priority = types.Int64Value(*network.WANEgressQOS)
		}
		egressQosObj, d := types.ObjectValueFrom(
			ctx,
			currentEgressQos.AttributeTypes(),
			currentEgressQos,
		)
		diags.Append(d...)
		model.EgressQoS = egressQosObj
	}

	// DNS Settings
	hasDNSData := network.WANDNS1 != nil || network.WANDNS2 != nil ||
		network.WANIPV6DNS1 != nil || network.WANIPV6DNS2 != nil ||
		network.WANDNSPreference != nil || network.WANIPV6DNSPreference != nil
	if !model.DNS.IsNull() || hasDNSData {
		var currentDNS dnsModel
		if !model.DNS.IsNull() && !model.DNS.IsUnknown() {
			d := model.DNS.As(ctx, &currentDNS, basetypes.ObjectAsOptions{})
			diags.Append(d...)
		}
		if network.WANDNS1 != nil {
			currentDNS.Primary = dnsAddrValue(network.WANDNS1)
		}
		if network.WANDNS2 != nil {
			currentDNS.Secondary = dnsAddrValue(network.WANDNS2)
		}
		if network.WANIPV6DNS1 != nil {
			currentDNS.IPv6Primary = dnsAddrValue(network.WANIPV6DNS1)
		}
		if network.WANIPV6DNS2 != nil {
			currentDNS.IPv6Secondary = dnsAddrValue(network.WANIPV6DNS2)
		}
		if network.WANDNSPreference != nil {
			currentDNS.Preference = types.StringValue(*network.WANDNSPreference)
		}
		if network.WANIPV6DNSPreference != nil {
			currentDNS.IPv6Preference = types.StringValue(*network.WANIPV6DNSPreference)
		}
		dnsObj, d := types.ObjectValueFrom(ctx, currentDNS.AttributeTypes(), currentDNS)
		diags.Append(d...)
		model.DNS = dnsObj
	}

	// DHCP Settings
	hasDHCPData := network.WANDHCPCos != nil || len(network.WANDHCPOptions) > 0
	if !model.DHCP.IsNull() || hasDHCPData {
		var currentDHCP dhcpWanModel
		if !model.DHCP.IsNull() && !model.DHCP.IsUnknown() {
			d := model.DHCP.As(ctx, &currentDHCP, basetypes.ObjectAsOptions{})
			diags.Append(d...)
		}
		if network.WANDHCPCos != nil {
			currentDHCP.CoS = types.Int64Value(*network.WANDHCPCos)
		}
		dhcpOptType := types.ObjectType{AttrTypes: dhcpOptionModel{}.AttributeTypes()}
		if len(network.WANDHCPOptions) > 0 {
			dhcpOptionsValues := make([]attr.Value, len(network.WANDHCPOptions))
			for i, opt := range network.WANDHCPOptions {
				dhcpOptionsValues[i], _ = types.ObjectValue(
					dhcpOptionModel{}.AttributeTypes(),
					map[string]attr.Value{
						"option_number": types.Int64PointerValue(opt.OptionNumber),
						"value":         types.StringPointerValue(opt.Value),
					},
				)
			}
			currentDHCP.Options, _ = types.ListValue(dhcpOptType, dhcpOptionsValues)
		} else if currentDHCP.Options.IsNull() || currentDHCP.Options.IsUnknown() {
			currentDHCP.Options = types.ListNull(dhcpOptType)
		}
		dhcpObj, d := types.ObjectValueFrom(ctx, currentDHCP.AttributeTypes(), currentDHCP)
		diags.Append(d...)
		model.DHCP = dhcpObj
	}

	// DHCPv6 Settings
	hasDHCPv6Data := network.WANDHCPv6Cos != nil || network.WANDHCPv6PDSize != nil ||
		network.IPV6WANDelegationType != nil || len(network.WANDHCPv6Options) > 0
	if !model.DHCPv6.IsNull() || hasDHCPv6Data {
		var currentDHCPv6 dhcpv6WanModel
		if !model.DHCPv6.IsNull() && !model.DHCPv6.IsUnknown() {
			d := model.DHCPv6.As(ctx, &currentDHCPv6, basetypes.ObjectAsOptions{})
			diags.Append(d...)
		}
		if network.WANDHCPv6Cos != nil {
			currentDHCPv6.CoS = types.Int64Value(*network.WANDHCPv6Cos)
		}
		if network.WANDHCPv6PDSize != nil {
			currentDHCPv6.PDSize = types.Int64Value(*network.WANDHCPv6PDSize)
		}
		currentDHCPv6.PDSizeAuto = types.BoolValue(network.WANDHCPv6PDSizeAuto)
		if network.IPV6WANDelegationType != nil {
			currentDHCPv6.DelegationType = types.StringValue(*network.IPV6WANDelegationType)
		}
		dhcpV6OptType := types.ObjectType{AttrTypes: dhcpOptionModel{}.AttributeTypes()}
		if len(network.WANDHCPv6Options) > 0 {
			dhcpV6OptionsValues := make([]attr.Value, len(network.WANDHCPv6Options))
			for i, opt := range network.WANDHCPv6Options {
				dhcpV6OptionsValues[i], _ = types.ObjectValue(
					dhcpOptionModel{}.AttributeTypes(),
					map[string]attr.Value{
						"option_number": types.Int64PointerValue(opt.OptionNumber),
						"value":         types.StringPointerValue(opt.Value),
					},
				)
			}
			currentDHCPv6.Options, _ = types.ListValue(dhcpV6OptType, dhcpV6OptionsValues)
		} else if currentDHCPv6.Options.IsNull() || currentDHCPv6.Options.IsUnknown() {
			currentDHCPv6.Options = types.ListNull(dhcpV6OptType)
		}
		dhcpv6Obj, d := types.ObjectValueFrom(ctx, currentDHCPv6.AttributeTypes(), currentDHCPv6)
		diags.Append(d...)
		model.DHCPv6 = dhcpv6Obj
	}

	// Smart Queue Settings
	hasSmartqData := network.WANSmartQEnabled || network.WANSmartQUpRate != nil ||
		network.WANSmartQDownRate != nil
	if !model.SmartQ.IsNull() || hasSmartqData {
		var currentSmartq smartqModel
		if !model.SmartQ.IsNull() && !model.SmartQ.IsUnknown() {
			d := model.SmartQ.As(ctx, &currentSmartq, basetypes.ObjectAsOptions{})
			diags.Append(d...)
		}
		if hasSmartqData {
			currentSmartq.Enabled = types.BoolValue(network.WANSmartQEnabled)
			if network.WANSmartQUpRate != nil {
				currentSmartq.UpRate = types.Int64Value(*network.WANSmartQUpRate)
			}
			if network.WANSmartQDownRate != nil {
				currentSmartq.DownRate = types.Int64Value(*network.WANSmartQDownRate)
			}
		}
		smartqObj, d := types.ObjectValueFrom(ctx, currentSmartq.AttributeTypes(), currentSmartq)
		diags.Append(d...)
		model.SmartQ = smartqObj
	}

	// UPnP Settings
	hasUPnPData := network.UPnPEnabled != nil || network.UPnPWANInterface != nil ||
		network.UPnPNatPMPEnabled != nil || network.UPnPSecureMode != nil
	if !model.UPnP.IsNull() || hasUPnPData {
		var currentUPnP upnpModel
		if !model.UPnP.IsNull() && !model.UPnP.IsUnknown() {
			d := model.UPnP.As(ctx, &currentUPnP, basetypes.ObjectAsOptions{})
			diags.Append(d...)
		}
		if network.UPnPEnabled != nil {
			currentUPnP.Enabled = types.BoolValue(*network.UPnPEnabled)
		}
		if network.UPnPWANInterface != nil {
			currentUPnP.WANInterface = types.StringValue(*network.UPnPWANInterface)
		}
		if network.UPnPNatPMPEnabled != nil {
			currentUPnP.NatPMPEnabled = types.BoolValue(*network.UPnPNatPMPEnabled)
		}
		if network.UPnPSecureMode != nil {
			currentUPnP.SecureMode = types.BoolValue(*network.UPnPSecureMode)
		}
		upnpObj, d := types.ObjectValueFrom(ctx, currentUPnP.AttributeTypes(), currentUPnP)
		diags.Append(d...)
		model.UPnP = upnpObj
	}

	// Load Balance Settings
	hasLBData := network.WANLoadBalanceType != nil || network.WANLoadBalanceWeight != nil ||
		network.WANFailoverPriority != nil
	if !model.LoadBalance.IsNull() || hasLBData {
		var currentLB loadBalanceModel
		if !model.LoadBalance.IsNull() && !model.LoadBalance.IsUnknown() {
			d := model.LoadBalance.As(ctx, &currentLB, basetypes.ObjectAsOptions{})
			diags.Append(d...)
		}
		if network.WANLoadBalanceType != nil {
			currentLB.Type = types.StringValue(*network.WANLoadBalanceType)
		}
		if network.WANLoadBalanceWeight != nil {
			currentLB.Weight = types.Int64Value(*network.WANLoadBalanceWeight)
		}
		if network.WANFailoverPriority != nil {
			currentLB.FailoverPriority = types.Int64Value(*network.WANFailoverPriority)
		}
		lbObj, d := types.ObjectValueFrom(ctx, currentLB.AttributeTypes(), currentLB)
		diags.Append(d...)
		model.LoadBalance = lbObj
	}

	// IGMP Settings
	hasIGMPData := network.IGMPProxyFor != nil || network.IGMPProxyUpstream
	if !model.IGMPProxy.IsNull() || hasIGMPData {
		var currentIGMP igmpProxyModel
		if !model.IGMPProxy.IsNull() && !model.IGMPProxy.IsUnknown() {
			d := model.IGMPProxy.As(ctx, &currentIGMP, basetypes.ObjectAsOptions{})
			diags.Append(d...)
		}
		if network.IGMPProxyFor != nil {
			currentIGMP.Downstream = types.StringValue(*network.IGMPProxyFor)
		}
		currentIGMP.Upstream = types.BoolValue(network.IGMPProxyUpstream)
		igmpObj, d := types.ObjectValueFrom(ctx, currentIGMP.AttributeTypes(), currentIGMP)
		diags.Append(d...)
		model.IGMPProxy = igmpObj
	}

	// Additional Settings
	model.ReportWANEvent = types.BoolValue(network.ReportWANEvent)
	model.Enabled = types.BoolValue(network.Enabled)
	model.SettingPreference = types.StringPointerValue(network.SettingPreference)
	model.IPv6SettingPreference = types.StringPointerValue(network.IPV6SettingPreference)
	model.SingleNetworkLAN = types.StringPointerValue(network.SingleNetworkLan)
	model.MACOverrideEnabled = types.BoolValue(network.MACOverrideEnabled)
	model.DsliteRemoteHost = types.StringPointerValue(network.WANDsliteRemoteHost)
	model.DsliteRemoteHostAuto = types.BoolValue(network.WANDsliteRemoteHostAuto)

	if len(network.WANIPAliases) > 0 {
		ipAliasesValues := make([]attr.Value, len(network.WANIPAliases))
		for i, alias := range network.WANIPAliases {
			ipAliasesValues[i] = types.StringValue(alias)
		}
		model.IPAliases, diags = types.ListValue(types.StringType, ipAliasesValues)
	} else {
		model.IPAliases = types.ListNull(types.StringType)
	}

	// Provider Capabilities — only overwrite when API returns data
	if network.WANProviderCapabilities != nil &&
		(network.WANProviderCapabilities.DownloadKilobitsPerSecond != nil ||
			network.WANProviderCapabilities.UploadKilobitsPerSecond != nil) {
		providerCapsAttrTypes := map[string]attr.Type{
			"download_kilobits_per_second": types.Int64Type,
			"upload_kilobits_per_second":   types.Int64Type,
		}
		providerCapsValues := map[string]attr.Value{
			"download_kilobits_per_second": types.Int64PointerValue(
				network.WANProviderCapabilities.DownloadKilobitsPerSecond,
			),
			"upload_kilobits_per_second": types.Int64PointerValue(
				network.WANProviderCapabilities.UploadKilobitsPerSecond,
			),
		}
		var d diag.Diagnostics
		model.ProviderCapabilities, d = types.ObjectValue(
			providerCapsAttrTypes,
			providerCapsValues,
		)
		diags.Append(d...)
	}
	// If API returns nil, preserve existing model.ProviderCapabilities

	applyWANDefaults(model)

	return diags
}

// applyWANDefaults ensures null/unknown fields have properly-typed values: typed
// nulls for objects/lists (distinguishing "not configured" from "empty"), and unknown scalars converted to null to avoid "unknown value after apply" errors.
func applyWANDefaults(model *wanResourceModel) {
	if model.Type.IsUnknown() {
		model.Type = types.StringNull()
	}
	if model.TypeV6.IsUnknown() {
		model.TypeV6 = types.StringNull()
	}
	if model.ReportWANEvent.IsUnknown() {
		model.ReportWANEvent = types.BoolNull()
	}
	if model.Enabled.IsUnknown() {
		model.Enabled = types.BoolNull()
	}
	if model.EgressQoS.IsNull() || model.EgressQoS.IsUnknown() {
		model.EgressQoS = types.ObjectNull(egressQosModel{}.AttributeTypes())
	}
	if model.SmartQ.IsNull() || model.SmartQ.IsUnknown() {
		model.SmartQ = types.ObjectNull(smartqModel{}.AttributeTypes())
	}
	if model.Vlan.IsNull() || model.Vlan.IsUnknown() {
		model.Vlan = types.ObjectNull(vlanModel{}.AttributeTypes())
	}
	if model.ProviderCapabilities.IsNull() || model.ProviderCapabilities.IsUnknown() {
		model.ProviderCapabilities = types.ObjectNull(providerCapabilitiesModel{}.AttributeTypes())
	}
	if model.DNS.IsNull() || model.DNS.IsUnknown() {
		model.DNS = types.ObjectNull(dnsModel{}.AttributeTypes())
	}
	if model.DHCP.IsNull() || model.DHCP.IsUnknown() {
		model.DHCP = types.ObjectNull(dhcpWanModel{}.AttributeTypes())
	}
	if model.DHCPv6.IsNull() || model.DHCPv6.IsUnknown() {
		model.DHCPv6 = types.ObjectNull(dhcpv6WanModel{}.AttributeTypes())
	}
	if model.UPnP.IsNull() || model.UPnP.IsUnknown() {
		model.UPnP = types.ObjectNull(upnpModel{}.AttributeTypes())
	}
	if model.LoadBalance.IsNull() || model.LoadBalance.IsUnknown() {
		model.LoadBalance = types.ObjectNull(loadBalanceModel{}.AttributeTypes())
	}
	if model.IGMPProxy.IsNull() || model.IGMPProxy.IsUnknown() {
		model.IGMPProxy = types.ObjectNull(igmpProxyModel{}.AttributeTypes())
	}
	if model.IPAliases.IsNull() || model.IPAliases.IsUnknown() {
		model.IPAliases = types.ListNull(types.StringType)
	}
}

// ListResourceConfigSchema implements [list.ListResource].
func (r *wanResource) ListResourceConfigSchema(
	ctx context.Context,
	_ list.ListResourceSchemaRequest,
	resp *list.ListResourceSchemaResponse,
) {
	resp.Schema = listresource_wan.WanListResourceSchema(ctx)
}

// List implements [list.ListResource].
func (r *wanResource) List(
	ctx context.Context,
	req list.ListRequest,
	stream *list.ListResultsStream,
) {
	var config wanListConfigModel

	diags := req.Config.Get(ctx, &config)
	if diags.HasError() {
		stream.Results = list.ListResultsStreamDiagnostics(diags)
		return
	}

	site := config.Site.ValueString()
	if site == "" {
		site = r.client.Site
	}

	var filters []wanListFilterModel
	if !config.Filter.IsNull() && !config.Filter.IsUnknown() {
		config.Filter.ElementsAs(ctx, &filters, false)
	}

	postFilters := make(map[string]string)
	for _, f := range filters {
		postFilters[f.Name.ValueString()] = f.Value.ValueString()
	}

	networks, err := r.client.ListNetwork(ctx, site)
	if err != nil {
		var d diag.Diagnostics
		d.AddError("Error Listing WAN Networks", "Could not list WAN networks: "+err.Error())
		stream.Results = list.ListResultsStreamDiagnostics(d)
		return
	}

	stream.Results = func(push func(list.ListResult) bool) {
		for _, network := range networks {
			if network.Purpose != unifi.PurposeWAN || network.WANNetworkGroup == nil {
				continue
			}

			if nameFilter, ok := postFilters["name"]; ok {
				if network.Name == nil || *network.Name != nameFilter {
					continue
				}
			}

			result := req.NewListResult(ctx)
			if network.Name != nil {
				result.DisplayName = *network.Name
			}

			result.Diagnostics.Append(
				result.Identity.SetAttribute(
					ctx,
					path.Root("id"),
					types.StringValue(network.ID),
				)...,
			)

			var model wanResourceModel
			result.Diagnostics.Append(r.networkToModel(ctx, &network, &model, site)...)
			if !result.Diagnostics.HasError() {
				model.Timeouts = timeoutsNullValue()
				result.Diagnostics.Append(result.Resource.Set(ctx, model)...)
			}

			if !push(result) {
				return
			}
		}
	}
}

// ValidateConfig warns when the configuration sets a value the controller will
// not receive for this kind of network: go-unifi serializes a Network through
// one of seven per-purpose structs, and any field the chosen one omits is
// discarded with no diagnostic at any layer -- 62 of this provider's attributes
// can be set and never arrive. At plan time, so a still-unknown attribute goes unreported (a miss, not a false alarm).
func (r *wanResource) ValidateConfig(
	ctx context.Context,
	req resource.ValidateConfigRequest,
	resp *resource.ValidateConfigResponse,
) {
	var model wanResourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &model)...)
	if resp.Diagnostics.HasError() {
		return
	}
	network, diags := r.modelToNetwork(ctx, &model)
	// A configuration this mapper can't build is a problem the apply will report
	// properly; warning about its fields here would just be noise on top of a real error.
	if diags.HasError() || network == nil {
		return
	}
	resp.Diagnostics.Append(droppedOnWrite("WAN", network)...)
}

// This assertion is the guard, not decoration: the framework only calls
// ValidateConfig if the type satisfies this interface, so a mistyped signature would otherwise silently drop the warning above.
var _ resource.ResourceWithValidateConfig = &wanResource{}
