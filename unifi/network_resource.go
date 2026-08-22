package unifi

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-nettypes/cidrtypes"
	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework-timetypes/timetypes"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/list"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/identityschema"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	"github.com/ubiquiti-community/go-unifi/unifi"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/listresource_network"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_network"
	"github.com/ubiquiti-community/terraform-provider-unifi/unifi/util"
)

// Ensure provider defined types fully satisfy framework interfaces.
var (
	_ resource.Resource                     = &networkResource{}
	_ resource.ResourceWithImportState      = &networkResource{}
	_ resource.ResourceWithIdentity         = &networkResource{}
	_ resource.ResourceWithModifyPlan       = &networkResource{}
	_ resource.ResourceWithUpgradeState     = &networkResource{}
	_ resource.ResourceWithConfigValidators = &networkResource{}
)

// Ensure provider defined types fully satisfy list interfaces.
var (
	_ list.ListResource              = &networkResource{}
	_ list.ListResourceWithConfigure = &networkResource{}
)

func NewNetworkResource() resource.Resource {
	return &networkResource{}
}

func NewNetworkListResource() list.ListResource {
	return &networkResource{}
}

// networkResource defines the resource implementation.
type networkResource struct {
	client *Client
}

type networkIdentityModel struct {
	ID types.String `tfsdk:"id"`
}

// networkListConfigModel describes the list configuration model.
type networkListConfigModel struct {
	Site   types.String `tfsdk:"site"`
	Filter types.List   `tfsdk:"filter"`
}

// networkListFilterModel represents a single name/value filter entry.
type networkListFilterModel struct {
	Name  types.String `tfsdk:"name"`
	Value types.String `tfsdk:"value"`
}

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

// stringsToList converts a controller string slice to a list value, mapping an
// absent collection to an EMPTY list rather than a null one. See the note in
// networkToModel: for an Optional+Computed attribute, null and [] are different
// values and only one of them round-trips.
func stringsToList(ctx context.Context, values []string, diags *diag.Diagnostics) types.List {
	if values == nil {
		values = []string{}
	}
	list, d := types.ListValueFrom(ctx, types.StringType, values)
	diags.Append(d...)
	return list
}

// natOutboundToList converts the controller's NAT outbound entries to a list of
// objects.
//
// ip_address_pool is carried through even though modelToNetwork does not write
// it. Reading a field the write drops is not symmetrical, and it is the right
// way round: state shows what the controller holds instead of claiming the pool
// is empty. The write-side gap is real and separate.
func natOutboundToList(
	ctx context.Context,
	entries []unifi.NetworkNATOutboundIPAddresses,
	diags *diag.Diagnostics,
) types.List {
	objectType := types.ObjectType{AttrTypes: natOutboundIPAddresses()}
	elements := make([]attr.Value, 0, len(entries))
	for _, entry := range entries {
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
		elements = append(elements, object)
	}
	list, d := types.ListValue(objectType, elements)
	diags.Append(d...)
	return list
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

// networkResourceModel describes the resource data model.
type networkResourceModel struct {
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

func (r *networkResource) Metadata(
	ctx context.Context,
	req resource.MetadataRequest,
	resp *resource.MetadataResponse,
) {
	resp.TypeName = req.ProviderTypeName + "_network"
}

// IdentitySchema implements [resource.ResourceWithIdentity].
func (r *networkResource) IdentitySchema(
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

func (r *networkResource) Schema(
	ctx context.Context,
	req resource.SchemaRequest,
	resp *resource.SchemaResponse,
) {
	resp.Schema = resource_network.NetworkResourceSchema(ctx)
	// v1: dhcp_server.leasetime, ipv6_ra_preferred_lifetime and
	// ipv6_ra_valid_lifetime changed from Int64 (seconds) to GoDuration
	// strings, which UpgradeState migrates. The specification cannot carry a
	// schema version, so it is re-set here as site_to_site_vpn does.
	resp.Schema.Version = 1
	// Grafted rather than generated, as everywhere else: timeouts.Attributes
	// is a call, not a literal, so the code specification cannot carry it.
	resp.Schema.Attributes["timeouts"] = timeouts.Attributes(
		ctx,
		timeouts.Opts{Create: true, Read: true, Update: true, Delete: true},
	)
}

// UpgradeState migrates v0 state to v1: leasetime (nested in dhcp_server),
// ipv6_ra_preferred_lifetime and ipv6_ra_valid_lifetime changed from integer
// seconds to GoDuration strings.
func (r *networkResource) UpgradeState(
	ctx context.Context,
) map[int64]resource.StateUpgrader {
	var schemaResp resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &schemaResp)
	schemaType := schemaResp.Schema.Type().TerraformType(ctx)

	return map[int64]resource.StateUpgrader{
		0: {
			StateUpgrader: func(
				ctx context.Context,
				req resource.UpgradeStateRequest,
				resp *resource.UpgradeStateResponse,
			) {
				if req.RawState == nil {
					return
				}
				dv, err := util.UpgradeDurationRawState(
					schemaType,
					req.RawState.JSON,
					func(state map[string]any) {
						util.SetDurationField(state, "ipv6_ra_preferred_lifetime", time.Second)
						util.SetDurationField(state, "ipv6_ra_valid_lifetime", time.Second)
						if dhcp, ok := state["dhcp_server"].(map[string]any); ok {
							util.SetDurationField(dhcp, "leasetime", time.Second)
						}
					},
				)
				if err != nil {
					resp.Diagnostics.AddError("Failed to upgrade network state", err.Error())
					return
				}
				resp.DynamicValue = dv
			},
		},
	}
}

func (r *networkResource) Configure(
	ctx context.Context,
	req resource.ConfigureRequest,
	resp *resource.ConfigureResponse,
) {
	// Prevent panic if the provider has not been configured.
	client, ok := resourceClient(req.ProviderData, &resp.Diagnostics)
	if !ok {
		return
	}

	r.client = client
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

func (r *networkResource) ConfigValidators(
	_ context.Context,
) []resource.ConfigValidator {
	return []resource.ConfigValidator{&networkPurposeAliasConfigValidator{}}
}

// networkPurposeAliasConfigValidator refuses a configuration that sets
// third_party_gateway and purpose to disagree.
//
// The two are not independent attributes. Both write the controller's single
// Purpose field -- an explicit purpose is applied first, then a true
// third_party_gateway overrides it to vlan-only -- and third_party_gateway is
// read back out of that same field rather than one of its own. So a
// disagreeing pair cannot be satisfied: whichever side loses the write is
// rewritten on the read, and the apply fails with "inconsistent result after
// apply" naming an attribute the practitioner set to exactly the value they
// asked for. That error blames the provider for the user's contradiction and
// says nothing about the other half of it.
//
// Refusing it here says which two lines conflict, before anything is created.
// Leaving either side unset is not a conflict: the unset one is derived.
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
	// An unknown value comes from an expression this validator cannot resolve,
	// so it cannot judge the pair. Null means the practitioner left it to be
	// derived, which is the whole point of the fix and never a conflict.
	if thirdParty.IsNull() || thirdParty.IsUnknown() ||
		purpose.IsNull() || purpose.IsUnknown() {
		return
	}
	if thirdParty.ValueBool() == (purpose.ValueString() == unifi.PurposeVLANOnly) {
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
			thirdParty.ValueBool(), purpose.ValueString(), unifi.PurposeVLANOnly,
			purpose.ValueString() == unifi.PurposeVLANOnly, unifi.PurposeVLANOnly,
		),
	)
}

// ModifyPlan forces setting_preference to "manual" when the plan enables a
// field the controller only honors under "manual".
//
// On "auto" the controller manages the advanced block itself and silently
// discards fields sent in the same payload — dhcpguard_enabled, igmp_snooping,
// and the dhcpd dns/ntp/time-offset toggles are stored as false however they
// were sent. It also re-enables its built-in DHCP server, which turns
// dhcp_relay off, since the two cannot coexist. The write succeeds either way,
// so without this the setting simply never takes effect and the post-apply read
// contradicts the plan.
//
// Only a true value forces the switch: "auto" storing false for a field the
// practitioner also set to false is the same outcome, so leave those alone.
// An explicit user-provided setting_preference is always respected.
func (r *networkResource) ModifyPlan(
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

func (r *networkResource) Create(
	ctx context.Context,
	req resource.CreateRequest,
	resp *resource.CreateResponse,
) {
	var data networkResourceModel

	// Read Terraform plan data into the model
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	createTimeout, timeoutDiags := data.Timeouts.Create(ctx, 20*time.Minute)
	resp.Diagnostics.Append(timeoutDiags...)
	if resp.Diagnostics.HasError() {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, createTimeout)
	defer cancel()

	// Convert to unifi.Network
	network, diags := r.modelToNetwork(ctx, &data)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	site := data.Site.ValueString()
	if site == "" {
		site = r.client.Site
	}

	// Create the network
	createdNetwork, err := r.client.CreateNetwork(ctx, site, network)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Creating network",
			err.Error(),
		)
		return
	}

	// Convert back to model, passing the plan data to preserve null values
	var planData networkResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &planData)...)
	if resp.Diagnostics.HasError() {
		return
	}

	diags = r.networkToModel(ctx, createdNetwork, &data, site, &planData)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Save data into Terraform state
	idModel := networkIdentityModel{ID: data.ID}
	resp.Diagnostics.Append(resp.Identity.Set(ctx, &idModel)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *networkResource) Read(
	ctx context.Context,
	req resource.ReadRequest,
	resp *resource.ReadResponse,
) {
	var data networkResourceModel

	// Read Terraform prior state data into the model
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	readTimeout, timeoutDiags := data.Timeouts.Read(ctx, 20*time.Minute)
	resp.Diagnostics.Append(timeoutDiags...)
	if resp.Diagnostics.HasError() {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, readTimeout)
	defer cancel()

	site := data.Site.ValueString()
	if site == "" {
		site = r.client.Site
	}

	var err error
	var network *unifi.Network

	if !data.ID.IsNull() && !data.ID.IsUnknown() {
		// Get the network by ID
		network, err = r.client.GetNetwork(ctx, site, data.ID.ValueString())
		if err != nil {
			resp.Diagnostics.AddError(
				"Error Reading network",
				"Could not read network ID "+data.ID.ValueString()+": "+err.Error(),
			)
			return
		}
	} else {
		// Get the network by name
		network, err = r.client.GetNetworkByName(ctx, site, data.Name.ValueString())
		if err != nil {
			resp.Diagnostics.AddError(
				"Error Reading network",
				"Could not read network name "+data.Name.ValueString()+": "+err.Error(),
			)
			return
		}
	}

	// Convert to model, passing the current state to preserve null values
	var priorState networkResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &priorState)...)
	if resp.Diagnostics.HasError() {
		return
	}

	diags := r.networkToModel(ctx, network, &data, site, &priorState)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Save updated data into Terraform state
	idModel := networkIdentityModel{ID: data.ID}
	resp.Diagnostics.Append(resp.Identity.Set(ctx, &idModel)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *networkResource) Update(
	ctx context.Context,
	req resource.UpdateRequest,
	resp *resource.UpdateResponse,
) {
	var data networkResourceModel

	// Read Terraform plan data into the model
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	updateTimeout, timeoutDiags := data.Timeouts.Update(ctx, 20*time.Minute)
	resp.Diagnostics.Append(timeoutDiags...)
	if resp.Diagnostics.HasError() {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, updateTimeout)
	defer cancel()

	// Convert to unifi.Network
	network, diags := r.modelToNetwork(ctx, &data)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	site := data.Site.ValueString()
	if site == "" {
		site = r.client.Site
	}

	network.ID = data.ID.ValueString()

	// Update the network
	// MASKED, NOT WHOLE-OBJECT, and the mask is narrowed to this purpose. See
	// networkWireFields: the object is built from the plan alone, so a
	// whole-object write sent every unmodelled field as its Go zero -- nine of
	// them on a corporate or guest network, three on vlan-only.
	updatedNetwork, err := r.client.UpdateNetworkFields(
		ctx, site, network, networkWireFields(network)...)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Updating network",
			err.Error(),
		)
		return
	}

	// Convert back to model, passing the plan data to preserve null values
	var planData networkResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &planData)...)
	if resp.Diagnostics.HasError() {
		return
	}

	diags = r.networkToModel(ctx, updatedNetwork, &data, site, &planData)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Save updated data into Terraform state
	idModel := networkIdentityModel{ID: data.ID}
	resp.Diagnostics.Append(resp.Identity.Set(ctx, &idModel)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *networkResource) Delete(
	ctx context.Context,
	req resource.DeleteRequest,
	resp *resource.DeleteResponse,
) {
	var data networkResourceModel

	// Read Terraform prior state data into the model
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	deleteTimeout, timeoutDiags := data.Timeouts.Delete(ctx, 20*time.Minute)
	resp.Diagnostics.Append(timeoutDiags...)
	if resp.Diagnostics.HasError() {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, deleteTimeout)
	defer cancel()

	site := data.Site.ValueString()
	if site == "" {
		site = r.client.Site
	}

	// Delete the network
	name := data.Name.ValueString()
	err := r.client.DeleteNetwork(ctx, site, data.ID.ValueString(), name)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Deleting network",
			err.Error(),
		)
		return
	}
}

func (r *networkResource) ImportState(
	ctx context.Context,
	req resource.ImportStateRequest,
	resp *resource.ImportStateResponse,
) {
	idParts := strings.Split(req.ID, ":")
	if len(idParts) == 2 {
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("site"), idParts[0])...)
		req.ID = idParts[1]
	}

	if strings.HasPrefix(req.ID, "name=") {
		req.ID = strings.TrimPrefix(req.ID, "name=")
		resource.ImportStatePassthroughID(ctx, path.Root("name"), req, resp)
	} else if regexp.MustCompile(`^[0-9a-f]{24}$`).MatchString(req.ID) {
		idModel := networkIdentityModel{ID: types.StringValue(req.ID)}
		resp.Diagnostics.Append(resp.Identity.Set(ctx, &idModel)...)
		if resp.Diagnostics.HasError() {
			return
		}
		resource.ImportStatePassthroughWithIdentity(
			ctx,
			path.Root("id"),
			path.Root("id"),
			req,
			resp,
		)
	} else {
		resource.ImportStatePassthroughID(ctx, path.Root("name"), req, resp)
	}
}

// modelToNetwork converts from Terraform model to unifi.Network.
func (r *networkResource) modelToNetwork(
	ctx context.Context,
	model *networkResourceModel,
) (*unifi.Network, diag.Diagnostics) {
	var diags diag.Diagnostics

	network := &unifi.Network{
		Name:                        model.Name.ValueStringPointer(),
		Purpose:                     unifi.PurposeCorporate,
		NetworkGroup:                util.Ptr("LAN"),
		AutoScaleEnabled:            model.AutoScale.ValueBool(),
		IPSubnet:                    model.Subnet.ValueStringPointer(),
		NetworkIsolationEnabled:     model.NetworkIsolation.ValueBool(),
		SettingPreference:           optStr(model.SettingPreference),
		InternetAccessEnabled:       model.InternetAccess.ValueBool(),
		MdnsEnabled:                 model.MulticastDNS.ValueBool(),
		GatewayType:                 model.GatewayType.ValueStringPointer(),
		IPV6InterfaceType:           model.IPv6InterfaceType.ValueStringPointer(),
		IPV6ClientAddressAssignment: optStr(model.IPv6ClientAddressAssignment),
		IPV6Subnet:                  model.IPv6StaticSubnet.ValueStringPointer(),
		IPV6RaEnabled:               model.IPv6RA.ValueBool(),
		IPV6RaPriority:              optStr(model.IPv6RAPriority),
		IPV6RaPreferredLifetime: util.DurationUnitsPtr(
			model.IPv6RAPreferredLifetime,
			time.Second,
		),
		IPV6RaValidLifetime:       util.DurationUnitsPtr(model.IPv6RAValidLifetime, time.Second),
		IPV6PDInterface:           optStr(model.IPv6PDInterface),
		IPV6PDPrefixid:            model.IPv6PDPrefixID.ValueString(),
		IPV6PDStart:               optStr(model.IPv6PDStart),
		IPV6PDStop:                optStr(model.IPv6PDStop),
		IPV6PDAutoPrefixidEnabled: model.IPv6PDAutoPrefixidEnabled.ValueBool(),
		LteLanEnabled:             model.LteLan.ValueBool(),
		Enabled:                   model.Enabled.ValueBool(),
		IGMPSnooping:              model.IgmpSnooping.ValueBool(),
		IPAliases:                 []string{},
	}

	// Purpose: default corporate, honor an explicitly configured value (guest,
	// vlan-only, corporate). third_party_gateway is the legacy way to request
	// vlan-only and takes precedence so existing configs keep working.
	networkPurposeToNetwork(model.Purpose, model.ThirdPartyGateway, network)

	// Handle DHCP guarding configuration
	if !model.DhcpGuarding.IsNull() && !model.DhcpGuarding.IsUnknown() {
		var dhcpGuarding dhcpGuardingModel
		d := model.DhcpGuarding.As(ctx, &dhcpGuarding, basetypes.ObjectAsOptions{})
		diags.Append(d...)
		if !diags.HasError() {
			network.DHCPguardEnabled = dhcpGuarding.Enabled.ValueBool()

			// Map servers to dhcpd_ip_1..3
			networkDHCPGuardingServersToNetwork(ctx, &diags, dhcpGuarding.Servers, network)
		}
	}

	// Handle domain name - set to empty string if null
	network.DomainName = model.DomainName.ValueStringPointer()

	// Handle optional int64 pointer fields
	networkVLANToNetwork(model.Vlan, network)

	// Handle NAT outbound IP addresses
	if !model.NatOutboundIPAddresses.IsNull() && !model.NatOutboundIPAddresses.IsUnknown() {
		var natIPs []natOutboundIPAddressesModel
		d := model.NatOutboundIPAddresses.ElementsAs(ctx, &natIPs, true)
		diags.Append(d...)
		if !diags.HasError() {
			for _, natIP := range natIPs {
				v := unifi.NetworkNATOutboundIPAddresses{
					IPAddress:       natIP.IPAddress.ValueString(),
					Mode:            natIP.Mode.ValueStringPointer(),
					WANNetworkGroup: natIP.WANNetworkGroup.ValueStringPointer(),
				}
				network.NATOutboundIPAddresses = append(network.NATOutboundIPAddresses, v)
			}
		}
	}

	// Handle IP aliases
	if !model.IPAliases.IsNull() && !model.IPAliases.IsUnknown() {
		var ipAliases []string
		d := model.IPAliases.ElementsAs(ctx, &ipAliases, false)
		diags.Append(d...)
		if !diags.HasError() {
			network.IPAliases = ipAliases
		}
	}

	// Handle IPv6 aliases
	if !model.IPv6Aliases.IsNull() && !model.IPv6Aliases.IsUnknown() {
		var ipv6Aliases []string
		d := model.IPv6Aliases.ElementsAs(ctx, &ipv6Aliases, false)
		diags.Append(d...)
		// if !diags.HasError() {
		// 	// IPv6Aliases field not available in API
		// }
	}

	// A DHCP server and DHCP relay cannot coexist on a network: with relay on,
	// emitting DHCPDEnabled=true (as the default branch below would) makes the
	// controller reject the request. We therefore skip the DHCP-server defaults
	// when relay is enabled. ModifyPlan additionally pins setting_preference to
	// "manual" so the controller honors the relay instead of auto-managing it.
	relayEnabled := false
	if !model.DhcpRelay.IsNull() && !model.DhcpRelay.IsUnknown() {
		var dr dhcpRelayModel
		if d := model.DhcpRelay.As(ctx, &dr, basetypes.ObjectAsOptions{}); !d.HasError() {
			relayEnabled = dr.Enabled.ValueBool()
		}
	}

	// Handle DHCP server configuration
	if !model.DhcpServer.IsNull() && !model.DhcpServer.IsUnknown() {
		var dhcpServer dhcpServerModel
		d := model.DhcpServer.As(ctx, &dhcpServer, basetypes.ObjectAsOptions{})
		diags.Append(d...)
		if !diags.HasError() {
			// Handle DHCP boot configuration
			networkBootToNetwork(ctx, &diags, dhcpServer.Boot, network)
			network.DHCPDEnabled = dhcpServer.Enabled.ValueBool()
			network.DHCPDStart = dhcpServer.Start.ValueStringPointer()
			network.DHCPDStop = dhcpServer.Stop.ValueStringPointer()
			network.DHCPDGatewayEnabled = dhcpServer.GatewayEnabled.ValueBool()
			network.DHCPDConflictChecking = dhcpServer.ConflictChecking.ValueBool()
			network.DHCPDNtpEnabled = dhcpServer.NtpEnabled.ValueBool()
			network.DHCPDTimeOffsetEnabled = dhcpServer.TimeOffsetEnabled.ValueBool()
			network.DHCPDDNSEnabled = dhcpServer.DnsEnabled.ValueBool()
			network.DHCPDLeaseTime = util.DurationUnitsPtr(dhcpServer.Leasetime, time.Second)

			// Handle WINS configuration
			networkWINSToNetwork(ctx, &diags, dhcpServer.Wins, network)

			if dhcpServer.WpadUrl.IsNull() || dhcpServer.WpadUrl.IsUnknown() {
				network.DHCPDWPAdUrl = util.Ptr("")
			} else {
				network.DHCPDWPAdUrl = dhcpServer.WpadUrl.ValueStringPointer()
			}

			if dhcpServer.TftpServer.IsNull() || dhcpServer.TftpServer.IsUnknown() {
				network.DHCPDTFTPServer = util.Ptr("")
			} else {
				network.DHCPDTFTPServer = dhcpServer.TftpServer.ValueStringPointer()
			}

			if dhcpServer.UnifiController.IsNull() || dhcpServer.UnifiController.IsUnknown() {
				network.DHCPDUnifiController = util.Ptr("")
			} else {
				network.DHCPDUnifiController = dhcpServer.UnifiController.ValueStringPointer()
			}

			// Handle DNS servers
			networkDHCPServerDNSToNetwork(ctx, &diags, dhcpServer.DnsServers, network)
		}
	} else if !relayEnabled {
		// Set defaults when DHCP server is not configured (and relay is off).
		network.DHCPDBootEnabled = false
		network.DHCPDBootServer = ""
		network.DHCPDBootFilename = util.Ptr("")
		network.DHCPDEnabled = true
		network.DHCPDGatewayEnabled = false
		network.DHCPDConflictChecking = true
		network.DHCPDNtpEnabled = false
		network.DHCPDTimeOffsetEnabled = false
		network.DHCPDDNSEnabled = false
		network.DHCPDLeaseTime = util.Ptr(int64(86400))
		network.DHCPDWinsEnabled = false
		network.DHCPDWins1 = util.Ptr("")
		network.DHCPDWins2 = util.Ptr("")
		network.DHCPDWPAdUrl = util.Ptr("")
		network.DHCPDTFTPServer = util.Ptr("")
		network.DHCPDUnifiController = util.Ptr("")
		network.DHCPDDNS1 = ""
		network.DHCPDDNS2 = ""
		network.DHCPDDNS3 = ""
		network.DHCPDDNS4 = ""
	}

	// Handle DHCPv6 server configuration
	if !model.DhcpV6Server.IsNull() && !model.DhcpV6Server.IsUnknown() {
		var dhcpV6Server dhcpV6ServerModel
		d := model.DhcpV6Server.As(ctx, &dhcpV6Server, basetypes.ObjectAsOptions{})
		diags.Append(d...)
		if !diags.HasError() {
			network.DHCPDV6Enabled = dhcpV6Server.Enabled.ValueBool()
			network.DHCPDV6DNSAuto = dhcpV6Server.DNSAuto.ValueBool()
			network.DHCPDV6Start = dhcpV6Server.Start.ValueStringPointer()
			network.DHCPDV6Stop = dhcpV6Server.Stop.ValueStringPointer()
			network.DHCPDV6LeaseTime = dhcpV6Server.Lease.ValueInt64Pointer()

			// Handle DHCPv6 DNS servers
			networkDHCPV6ServerDNSToNetwork(ctx, &diags, dhcpV6Server.DNSServers, network)
		}
	}

	// Handle DHCP relay configuration
	if !model.DhcpRelay.IsNull() && !model.DhcpRelay.IsUnknown() {
		var dhcpRelay dhcpRelayModel
		d := model.DhcpRelay.As(ctx, &dhcpRelay, basetypes.ObjectAsOptions{})
		diags.Append(d...)
		if !diags.HasError() {
			network.DHCPRelayEnabled = dhcpRelay.Enabled.ValueBool()
			if !dhcpRelay.Servers.IsNull() && !dhcpRelay.Servers.IsUnknown() {
				var servers []string
				d := dhcpRelay.Servers.ElementsAs(ctx, &servers, false)
				diags.Append(d...)
				if !diags.HasError() {
					network.DHCPRelayServers = servers
				}
			}
		}
	} else {
		// Set defaults when DHCP relay is not configured
		network.DHCPRelayEnabled = false
	}

	return network, diags
}

// networkToModel converts from unifi.Network to Terraform model.
// previousModel is the model from the plan or previous state, used to preserve null values.
func (r *networkResource) networkToModel(
	ctx context.Context,
	network *unifi.Network,
	model *networkResourceModel,
	site string,
	previousModel *networkResourceModel,
) diag.Diagnostics {
	var diags diag.Diagnostics

	model.ID = types.StringValue(network.ID)
	model.Site = types.StringValue(site)
	model.Name = types.StringPointerValue(network.Name)
	model.Enabled = types.BoolValue(network.Enabled)
	model.IgmpSnooping = types.BoolValue(network.IGMPSnooping)
	model.NetworkIsolation = types.BoolValue(network.NetworkIsolationEnabled)

	// Set third_party_gateway based on API purpose
	isVLANOnly := network.Purpose == unifi.PurposeVLANOnly
	model.Purpose, model.ThirdPartyGateway = networkPurposeFromNetwork(network)

	// Reflect the controller's actual purpose. On ZBF controllers the purpose is
	// driven by the network's firewall zone (e.g. guest ⇄ Hotspot zone), so we
	// read it back rather than assume the configured value: an unset purpose
	// (Computed) resolves to whatever the controller reports, and a configured
	// value that the controller rejects surfaces as an inconsistent-result error
	// instead of silently drifting.

	// For vlan-only networks, the API does not return fields like subnet, gateway_type,
	// setting_preference, etc. Preserve the plan/state values for these irrelevant fields
	// to avoid "inconsistent result after apply" errors.
	if isVLANOnly && previousModel != nil {
		model.Subnet = previousModel.Subnet
		model.AutoScale = previousModel.AutoScale
		// setting_preference carries no default and uses UseStateForUnknown, so
		// it is unknown during Create. Preserving that leaves it unknown after
		// apply, which Terraform rejects. Resolve it from the API value instead
		// -- null when the controller reports nothing, which is what a vlan-only
		// network gets and is a known value.
		if previousModel.SettingPreference.IsUnknown() {
			model.SettingPreference = types.StringPointerValue(network.SettingPreference)
		} else {
			model.SettingPreference = previousModel.SettingPreference
		}
		model.InternetAccess = previousModel.InternetAccess
		// multicast_dns uses UseStateForUnknown, so it may be unknown during
		// Create. Resolve it from the API value (the controller does not honor
		// mDNS for vlan-only networks, so this is effectively false).
		if previousModel.MulticastDNS.IsUnknown() {
			model.MulticastDNS = types.BoolValue(network.MdnsEnabled)
		} else {
			model.MulticastDNS = previousModel.MulticastDNS
		}
		model.GatewayType = previousModel.GatewayType
		// ipv6_interface_type uses UseStateForUnknown and carries no default, so
		// it is unknown during Create. Resolve it from the API value: the
		// controller owns it, and a static "none" default is what made an apply
		// that omitted the attribute switch IPv6 off and take the whole
		// dhcp_v6_server block down with it.
		if previousModel.IPv6InterfaceType.IsUnknown() {
			model.IPv6InterfaceType = types.StringPointerValue(network.IPV6InterfaceType)
		} else {
			model.IPv6InterfaceType = previousModel.IPv6InterfaceType
		}
		// ipv6_static_subnet became Computed for the same reason as
		// ipv6_interface_type: the controller assigns it, and a null plan
		// against a populated read aborts the apply. Computed means the plan
		// carries unknown on Create, so resolve it from the API rather than
		// copying the unknown through.
		if previousModel.IPv6StaticSubnet.IsUnknown() {
			model.IPv6StaticSubnet = types.StringPointerValue(network.IPV6Subnet)
		} else {
			model.IPv6StaticSubnet = previousModel.IPv6StaticSubnet
		}
		model.IPv6PDInterface = previousModel.IPv6PDInterface
		// ipv6_pd_prefixid gained Computed and UseStateForUnknown, so the plan
		// carries unknown whenever the config omits it. Copying that through
		// leaves the attribute unknown after apply, which Terraform rejects --
		// the same trap setting_preference documents a few lines up, and the
		// reason a schema change here is not free. Resolve it from the API
		// instead, mapping the controller's empty string to null exactly as the
		// non-vlan-only branch does.
		if previousModel.IPv6PDPrefixID.IsUnknown() {
			if network.IPV6PDPrefixid == "" {
				model.IPv6PDPrefixID = types.StringNull()
			} else {
				model.IPv6PDPrefixID = types.StringValue(network.IPV6PDPrefixid)
			}
		} else {
			model.IPv6PDPrefixID = previousModel.IPv6PDPrefixID
		}
		// lte_lan uses UseStateForUnknown, so it may be unknown during Create.
		// Resolve it from the API value: the controller assigns this flag
		// itself, which is why it must not carry a static default.
		if previousModel.LteLan.IsUnknown() {
			model.LteLan = types.BoolValue(network.LteLanEnabled)
		} else {
			model.LteLan = previousModel.LteLan
		}
		// The IPv6 attributes below are Computed + UseStateForUnknown. On Create
		// there is no prior state, so the plan carries them as unknown; copying
		// the plan value verbatim would leave them unknown in the result and
		// trip "invalid result object after apply". Resolve unknowns from the
		// API value (vlan-only networks have no meaningful IPv6 config, so this
		// is effectively the controller's zero value).
		if previousModel.IPv6ClientAddressAssignment.IsUnknown() {
			model.IPv6ClientAddressAssignment = types.StringPointerValue(
				network.IPV6ClientAddressAssignment,
			)
		} else {
			model.IPv6ClientAddressAssignment = previousModel.IPv6ClientAddressAssignment
		}
		if previousModel.IPv6RA.IsUnknown() {
			model.IPv6RA = types.BoolValue(network.IPV6RaEnabled)
		} else {
			model.IPv6RA = previousModel.IPv6RA
		}
		if previousModel.IPv6RAPriority.IsUnknown() {
			model.IPv6RAPriority = types.StringPointerValue(network.IPV6RaPriority)
		} else {
			model.IPv6RAPriority = previousModel.IPv6RAPriority
		}
		if previousModel.IPv6RAPreferredLifetime.IsUnknown() {
			model.IPv6RAPreferredLifetime = util.DurationPtrValue(
				network.IPV6RaPreferredLifetime,
				time.Second,
			)
		} else {
			model.IPv6RAPreferredLifetime = previousModel.IPv6RAPreferredLifetime
		}
		if previousModel.IPv6RAValidLifetime.IsUnknown() {
			model.IPv6RAValidLifetime = util.DurationPtrValue(
				network.IPV6RaValidLifetime,
				time.Second,
			)
		} else {
			model.IPv6RAValidLifetime = previousModel.IPv6RAValidLifetime
		}
		if previousModel.IPv6PDStart.IsUnknown() {
			model.IPv6PDStart = types.StringPointerValue(network.IPV6PDStart)
		} else {
			model.IPv6PDStart = previousModel.IPv6PDStart
		}
		if previousModel.IPv6PDStop.IsUnknown() {
			model.IPv6PDStop = types.StringPointerValue(network.IPV6PDStop)
		} else {
			model.IPv6PDStop = previousModel.IPv6PDStop
		}
		if previousModel.IPv6PDAutoPrefixidEnabled.IsUnknown() {
			model.IPv6PDAutoPrefixidEnabled = types.BoolValue(network.IPV6PDAutoPrefixidEnabled)
		} else {
			model.IPv6PDAutoPrefixidEnabled = previousModel.IPv6PDAutoPrefixidEnabled
		}
		// domain_name uses UseStateForUnknown, so it may be unknown during Create.
		// Resolve unknown to null since the API doesn't return it for vlan-only.
		if previousModel.DomainName.IsUnknown() {
			model.DomainName = types.StringNull()
		} else {
			model.DomainName = previousModel.DomainName
		}
	} else {
		model.AutoScale = types.BoolValue(network.AutoScaleEnabled)
		if network.IPSubnet != nil {
			model.Subnet = cidrtypes.NewIPv4PrefixValue(*network.IPSubnet)
		} else {
			model.Subnet = cidrtypes.NewIPv4PrefixNull()
		}
		model.SettingPreference = types.StringPointerValue(network.SettingPreference)
		model.InternetAccess = types.BoolValue(network.InternetAccessEnabled)
		// Some controllers (notably UniFi OS gateways) ignore mdns_enabled
		// per-network and always store false, so a configured `true` would fail
		// the consistency check (#282; the vlan-only branch above already does
		// this). Preserve the configured/known value; fall back to the
		// controller's value only when it wasn't set by the user (unknown/null,
		// e.g. on Read or List).
		if previousModel != nil && !previousModel.MulticastDNS.IsNull() &&
			!previousModel.MulticastDNS.IsUnknown() {
			model.MulticastDNS = previousModel.MulticastDNS
		} else {
			model.MulticastDNS = types.BoolValue(network.MdnsEnabled)
		}
		model.GatewayType = types.StringPointerValue(network.GatewayType)
		model.IPv6InterfaceType = types.StringPointerValue(network.IPV6InterfaceType)
		model.IPv6ClientAddressAssignment = types.StringPointerValue(
			network.IPV6ClientAddressAssignment,
		)
		model.IPv6StaticSubnet = types.StringPointerValue(network.IPV6Subnet)
		model.IPv6RA = types.BoolValue(network.IPV6RaEnabled)
		model.IPv6RAPriority = types.StringPointerValue(network.IPV6RaPriority)
		model.IPv6RAPreferredLifetime = util.DurationPtrValue(
			network.IPV6RaPreferredLifetime,
			time.Second,
		)
		model.IPv6RAValidLifetime = util.DurationPtrValue(network.IPV6RaValidLifetime, time.Second)
		model.IPv6PDInterface = types.StringPointerValue(network.IPV6PDInterface)
		if network.IPV6PDPrefixid == "" {
			model.IPv6PDPrefixID = types.StringNull()
		} else {
			model.IPv6PDPrefixID = types.StringValue(network.IPV6PDPrefixid)
		}
		model.IPv6PDStart = types.StringPointerValue(network.IPV6PDStart)
		model.IPv6PDStop = types.StringPointerValue(network.IPV6PDStop)
		model.IPv6PDAutoPrefixidEnabled = types.BoolValue(network.IPV6PDAutoPrefixidEnabled)
		model.LteLan = types.BoolValue(network.LteLanEnabled)
		model.DomainName = types.StringPointerValue(network.DomainName)
	}

	// The four DHCP blocks below are read back UNCONDITIONALLY. Each used to be
	// populated only when the previous model already held it -- or, on import,
	// when the corresponding enable flag was set -- and nulled otherwise. A
	// practitioner who never wrote the block therefore kept a null in state, the
	// controller's values were never read in, and the next apply sent the zeros
	// modelToNetwork pre-seeds: dhcp_guarding lost its server IPs, dhcp_server
	// lost DNS advertisement, time offset and WINS while dhcpd_enabled stayed
	// true, dhcp_relay lost both fields, and dhcp_v6_server lost dns_auto.
	//
	// The read alone does not fix that. Reading the block into the MODEL is only
	// half of it, because the write reads the PLAN: each block is Computed with
	// UseStateForUnknown in the generated schema, which is what carries state
	// into the plan when the configuration says nothing. Change either half
	// alone and the zeros still go out.
	//
	// isImport went with the guards. It had exactly these four consumers.

	// Build dhcp_guarding from API fields
	serversList := networkDHCPGuardingServersFromNetwork(ctx, &diags, network)

	dhcpGuardingValue := dhcpGuardingModel{
		Enabled: types.BoolValue(network.DHCPguardEnabled),
		Servers: serversList,
	}
	dhcpGuardingObj, d := types.ObjectValueFrom(
		ctx,
		dhcpGuardingValue.AttributeTypes(),
		dhcpGuardingValue,
	)
	diags.Append(d...)
	model.DhcpGuarding = dhcpGuardingObj

	model.Vlan = networkVLANFromNetwork(network)

	// READ THESE BACK. They used to be nulled unconditionally, with the comment
	// "for now set to null", and both are in the wire mask -- so the write sent
	// the empty slice modelToNetwork pre-seeds and the controller, which treats
	// the array as authoritative, dropped whatever it held. An apply that
	// touched only the vlan cleared aliases configured through the UI, every
	// time.
	//
	// Nulling a value the resource SENDS is the trap: mask membership answers
	// "does this resource manage the field", not "does the value it sends mean
	// anything". Ownership was right and the value was empty.
	//
	// EMPTY BECOMES AN EMPTY LIST, NOT NULL. Both attributes are now Optional
	// AND Computed, so an empty collection is a value the practitioner may have
	// asked for -- `ip_aliases = []` against a null state is a diff no apply
	// settles, because the config keeps producing []. Same nil-versus-empty
	// distinction the resource kit's KeepZero carries, one layer up.
	model.IPAliases = stringsToList(ctx, network.IPAliases, &diags)
	model.NatOutboundIPAddresses = natOutboundToList(ctx, network.NATOutboundIPAddresses, &diags)

	// ipv6_aliases stays null: it is NOT in the wire mask and the SDK has no
	// field for it (see the commented-out branch in modelToNetwork), so nothing
	// is sent and there is nothing to read back.
	model.IPv6Aliases = types.ListNull(types.StringType)

	// Only populate dhcp_server if:
	// 1. It was configured in the previous state (not null), OR
	// 2. This is an import and DHCP is enabled (populate everything during import)
	// Helper function to convert empty strings to null
	strPtrToType := func(ptr *string) types.String {
		if ptr == nil || *ptr == "" {
			return types.StringNull()
		}
		return types.StringValue(*ptr)
	}

	dhcpBootObj := networkBootFromNetwork(ctx, &diags, network)

	// Build DNS servers list from DHCPDDNS1-4
	dnsServersList := networkDHCPServerDNSFromNetwork(ctx, &diags, network)

	// Build WINS from the enable flag and DHCPDWins1-2
	winsObj := networkWINSFromNetwork(ctx, &diags, network)

	dhcpServerValue := dhcpServerModel{
		Boot:              dhcpBootObj,
		Enabled:           types.BoolValue(network.DHCPDEnabled),
		GatewayEnabled:    types.BoolValue(network.DHCPDGatewayEnabled),
		ConflictChecking:  types.BoolValue(network.DHCPDConflictChecking),
		NtpEnabled:        types.BoolValue(network.DHCPDNtpEnabled),
		TimeOffsetEnabled: types.BoolValue(network.DHCPDTimeOffsetEnabled),
		DnsEnabled:        types.BoolValue(network.DHCPDDNSEnabled),
		Leasetime:         util.DurationPtrValue(network.DHCPDLeaseTime, time.Second),
		Wins:              winsObj,
		WpadUrl:           strPtrToType(network.DHCPDWPAdUrl),
		Start:             types.StringPointerValue(network.DHCPDStart),
		Stop:              types.StringPointerValue(network.DHCPDStop),
		TftpServer:        strPtrToType(network.DHCPDTFTPServer),
		UnifiController:   strPtrToType(network.DHCPDUnifiController),
		DnsServers:        dnsServersList,
	}

	dhcpServerObj, d := types.ObjectValueFrom(
		ctx,
		dhcpServerValue.AttributeTypes(),
		dhcpServerValue,
	)
	diags.Append(d...)
	model.DhcpServer = dhcpServerObj

	// Only populate dhcp_v6_server if:
	// 1. It was configured in the previous state (not null), OR
	// 2. This is an import and DHCPv6 is enabled
	dhcpv6DNSList := networkDHCPV6ServerDNSFromNetwork(ctx, &diags, network)

	dhcpV6ServerValue := dhcpV6ServerModel{
		Enabled:    types.BoolValue(network.DHCPDV6Enabled),
		DNSAuto:    types.BoolValue(network.DHCPDV6DNSAuto),
		DNSServers: dhcpv6DNSList,
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

	// Only populate dhcp_relay if:
	// 1. It was configured in the previous state (not null), OR
	// 2. This is an import and DHCP relay is enabled (populate everything during import)
	var relayServersVal types.List
	if len(network.DHCPRelayServers) > 0 {
		var d diag.Diagnostics
		relayServersVal, d = types.ListValueFrom(
			ctx,
			types.StringType,
			network.DHCPRelayServers,
		)
		diags.Append(d...)
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

	return diags
}

// ListResourceConfigSchema implements [list.ListResource].
func (r *networkResource) ListResourceConfigSchema(
	ctx context.Context,
	req list.ListResourceSchemaRequest,
	resp *list.ListResourceSchemaResponse,
) {
	resp.Schema = listresource_network.NetworkListResourceSchema(ctx)
}

// List implements [list.ListResource].
func (r *networkResource) List(
	ctx context.Context,
	req list.ListRequest,
	stream *list.ListResultsStream,
) {
	var config networkListConfigModel

	diags := req.Config.Get(ctx, &config)
	if diags.HasError() {
		stream.Results = list.ListResultsStreamDiagnostics(diags)
		return
	}

	site := config.Site.ValueString()
	if site == "" {
		site = r.client.Site
	}

	// Process filter blocks.
	var filters []networkListFilterModel
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
		d.AddError("Error Listing Networks", "Could not list networks: "+err.Error())
		stream.Results = list.ListResultsStreamDiagnostics(d)
		return
	}

	stream.Results = func(push func(list.ListResult) bool) {
		for _, network := range networks {
			// Filter by purpose: only corporate, guest and vlan-only networks.
			if network.Purpose != unifi.PurposeCorporate &&
				network.Purpose != unifi.PurposeGuest &&
				network.Purpose != unifi.PurposeVLANOnly {
				continue
			}

			// Apply name filter if specified.
			if nameFilter, ok := postFilters["name"]; ok {
				if network.Name == nil || *network.Name != nameFilter {
					continue
				}
			}

			result := req.NewListResult(ctx)
			if network.Name != nil {
				result.DisplayName = *network.Name
			}

			// Set identity.
			result.Diagnostics.Append(
				result.Identity.SetAttribute(
					ctx,
					path.Root("id"),
					types.StringValue(network.ID),
				)...,
			)

			// Convert to model.
			var model networkResourceModel
			result.Diagnostics.Append(
				r.networkToModel(ctx, &network, &model, site, &networkResourceModel{})...)
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

// The fourteen functions below are the halves this resource's policy claims
// name. Each relates one Terraform member to several observed fields, which is
// the one thing the compiler cannot check, so the policy names a function and a
// reader opens it. They were inline in modelToNetwork and networkToModel, which
// are long enough that "the relation is in there" was not an answer.
//
// Absence is part of each relation, so a member that is null or unknown is
// handled here rather than by the caller. The one exception is the block that
// resets every dhcp_server field when there is no dhcp_server at all: that is
// the absence of the whole grouping, not of any one member.

// networkVLANToNetwork writes the one released vlan number into the observed
// number AND its enable flag: vlan_enabled is set from whether vlan is
// configured at all, so neither field alone is the attribute's source.
func networkVLANToNetwork(vlan types.Int64, network *unifi.Network) {
	network.VLAN = vlan.ValueInt64Pointer()
	network.VLANEnabled = !vlan.IsNull() && !vlan.IsUnknown()
}

// networkVLANFromNetwork reads the vlan number back. vlan_enabled is not read:
// a network with no vlan reports a null number, which is the same thing.
func networkVLANFromNetwork(network *unifi.Network) types.Int64 {
	return types.Int64PointerValue(network.VLAN)
}

// networkPurposeToNetwork writes two released attributes onto one observed
// field. purpose is honoured when configured and then OVERWRITTEN to vlan-only
// when third_party_gateway is true, which is the legacy way to ask for it.
func networkPurposeToNetwork(
	purpose types.String,
	thirdPartyGateway types.Bool,
	network *unifi.Network,
) {
	if !purpose.IsNull() && !purpose.IsUnknown() && purpose.ValueString() != "" {
		network.Purpose = purpose.ValueString()
	}
	if thirdPartyGateway.ValueBool() {
		network.Purpose = unifi.PurposeVLANOnly
	}
}

// networkPurposeFromNetwork computes both released attributes from the one
// observed field: purpose as the controller reports it, and
// third_party_gateway as whether that value is vlan-only. A controller that
// reports no purpose is reported as corporate, which is what it means.
func networkPurposeFromNetwork(network *unifi.Network) (types.String, types.Bool) {
	purpose := types.StringValue(unifi.PurposeCorporate)
	if network.Purpose != "" {
		purpose = types.StringValue(network.Purpose)
	}
	return purpose, types.BoolValue(network.Purpose == unifi.PurposeVLANOnly)
}

// networkDHCPGuardingServersToNetwork distributes dhcp_guarding.servers
// positionally into the three observed slots. It does NOT clear the slots it
// does not use -- unlike the dhcp_server DNS write below -- so a shorter list
// leaves whatever was there.
func networkDHCPGuardingServersToNetwork(
	ctx context.Context,
	diags *diag.Diagnostics,
	servers types.List,
	network *unifi.Network,
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
	network *unifi.Network,
) types.List {
	return stringListOrNull(ctx, diags, collectNonEmptyStrings(
		network.DHCPDIP1, network.DHCPDIP2, network.DHCPDIP3,
	))
}

// networkDHCPServerDNSToNetwork distributes dhcp_server.dns_servers positionally
// into the four observed slots, clearing the trailing ones it does not use.
//
// A fifth server never reaches here: the schema carries
// listvalidator.SizeAtMost(4), so validation rejects it with a diagnostic and
// the loop below cannot truncate. The bound is defence in depth, not the
// behaviour.
//
// What IS behaviour is the pairing with networkDHCPServerDNSFromNetwork, which
// compacts. The write never leaves a gap, but a gap arriving from anywhere else
// reads back compacted and writes back one slot earlier, so a value moves slot
// on a read-write round trip.
func networkDHCPServerDNSToNetwork(
	ctx context.Context,
	diags *diag.Diagnostics,
	dnsServers types.List,
	network *unifi.Network,
) {
	slots := []*string{
		&network.DHCPDDNS1, &network.DHCPDDNS2, &network.DHCPDDNS3, &network.DHCPDDNS4,
	}
	if dnsServers.IsNull() || dnsServers.IsUnknown() {
		for _, slot := range slots {
			*slot = ""
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
			*slot = values[i]
			continue
		}
		*slot = ""
	}
}

// networkDHCPServerDNSFromNetwork collects the four observed slots back into the
// one released list, keeping only the non-empty ones. See the write half for why
// compacting matters.
func networkDHCPServerDNSFromNetwork(
	ctx context.Context,
	diags *diag.Diagnostics,
	network *unifi.Network,
) types.List {
	return stringListOrNull(ctx, diags, collectNonEmptyStrings(
		network.DHCPDDNS1, network.DHCPDDNS2, network.DHCPDDNS3, network.DHCPDDNS4,
	))
}

// networkDHCPV6ServerDNSToNetwork distributes dhcp_v6_server.dns_servers
// positionally into the four observed slots, clearing the trailing ones. The
// same fifth-server truncation applies as for the v4 slots.
func networkDHCPV6ServerDNSToNetwork(
	ctx context.Context,
	diags *diag.Diagnostics,
	dnsServers types.List,
	network *unifi.Network,
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
	network *unifi.Network,
) types.List {
	return stringListOrNull(ctx, diags, collectNonEmptyStringPointers(
		network.DHCPDV6DNS1, network.DHCPDV6DNS2,
		network.DHCPDV6DNS3, network.DHCPDV6DNS4,
	))
}

// networkWINSToNetwork writes dhcp_server.wins over an enable flag and two
// address slots, the addresses distributed positionally and the trailing one
// cleared. An absent wins block disables it and clears both slots.
func networkWINSToNetwork(
	ctx context.Context,
	diags *diag.Diagnostics,
	winsObject types.Object,
	network *unifi.Network,
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

// networkBootToNetwork writes dhcp_server.boot over the three flat observed
// fields the wire keeps apart. An absent boot block disables it and empties
// both strings.
func networkBootToNetwork(
	ctx context.Context,
	diags *diag.Diagnostics,
	bootObject types.Object,
	network *unifi.Network,
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
	network *unifi.Network,
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
// not receive for this network's purpose.
//
// THIS IS THE SURFACE THE MEASUREMENT IS ABOUT. go-unifi serialises a Network
// through one of seven per-purpose structs, and a vlan-only network discards 44
// of the 51 attributes this resource exposes -- silently, with a clean plan and
// a successful apply. Corporate and guest drop 4 each.
//
// The subject comes from the built object's own Purpose rather than a constant,
// because this resource writes three different ones and a warning that said
// only "network" would not tell a practitioner which rule they had hit.
//
// AT PLAN TIME, so it arrives before the apply. An attribute still unknown then
// reads as unset and goes unreported, which is a miss rather than a false
// alarm.
func (r *networkResource) ValidateConfig(
	ctx context.Context,
	req resource.ValidateConfigRequest,
	resp *resource.ValidateConfigResponse,
) {
	var model networkResourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &model)...)
	if resp.Diagnostics.HasError() {
		return
	}
	network, diags := r.modelToNetwork(ctx, &model)
	if diags.HasError() || network == nil {
		return
	}
	resp.Diagnostics.Append(droppedOnWrite(network.Purpose+" network", network)...)
}

// THE ASSERTION IS THE GUARD, not decoration. The framework calls ValidateConfig
// only if the type satisfies this interface, so a mistyped signature would mean
// the warning above is simply never raised -- with nothing failing to say so.
// This makes that a compile error.
var _ resource.ResourceWithValidateConfig = &networkResource{}
