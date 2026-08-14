package unifi

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-nettypes/cidrtypes"
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
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/listresource_vpn_client"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_vpn_client"
	"github.com/ubiquiti-community/terraform-provider-unifi/unifi/util"
)

// Ensure provider defined types fully satisfy framework interfaces.
var (
	_ resource.Resource                = &vpnClientResource{}
	_ resource.ResourceWithImportState = &vpnClientResource{}
	_ resource.ResourceWithIdentity    = &vpnClientResource{}
)

// Ensure provider defined types fully satisfy list interfaces.
var (
	_ list.ListResource              = &vpnClientResource{}
	_ list.ListResourceWithConfigure = &vpnClientResource{}
)

func NewVPNClientResource() resource.Resource {
	return &vpnClientResource{}
}

func NewVPNClientListResource() list.ListResource {
	return &vpnClientResource{}
}

// vpnClientResource defines the resource implementation.
type vpnClientResource struct {
	client *Client
}

type vpnClientIdentityModel struct {
	ID types.String `tfsdk:"id"`
}

// vpnClientListConfigModel describes the list configuration model.
type vpnClientListConfigModel struct {
	Site   types.String `tfsdk:"site"`
	Filter types.List   `tfsdk:"filter"`
}

// vpnClientListFilterModel represents a single name/value filter entry.
type vpnClientListFilterModel struct {
	Name  types.String `tfsdk:"name"`
	Value types.String `tfsdk:"value"`
}

// wireguardConfigurationModel describes the WireGuard configuration file upload.
type wireguardConfigurationModel struct {
	Content  types.String `tfsdk:"content"`
	Filename types.String `tfsdk:"filename"`
}

func (m wireguardConfigurationModel) AttributeTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"content":  types.StringType,
		"filename": types.StringType,
	}
}

// wireguardPeerModel describes the WireGuard peer configuration for manual mode.
type wireguardPeerModel struct {
	IP        types.String `tfsdk:"ip"`
	Port      types.Int64  `tfsdk:"port"`
	PublicKey types.String `tfsdk:"public_key"`
}

func (m wireguardPeerModel) AttributeTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"ip":         types.StringType,
		"port":       types.Int64Type,
		"public_key": types.StringType,
	}
}

// wireguardModel describes the WireGuard VPN configuration.
type wireguardModel struct {
	PrivateKey          types.String `tfsdk:"private_key"`
	Configuration       types.Object `tfsdk:"configuration"`
	Peer                types.Object `tfsdk:"peer"`
	PresharedKeyEnabled types.Bool   `tfsdk:"preshared_key_enabled"`
	PresharedKey        types.String `tfsdk:"preshared_key"`
	Interface           types.String `tfsdk:"interface"`
	DnsServers          types.List   `tfsdk:"dns_servers"`
}

func (m wireguardModel) AttributeTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"private_key": types.StringType,
		"configuration": types.ObjectType{
			AttrTypes: wireguardConfigurationModel{}.AttributeTypes(),
		},
		"peer":                  types.ObjectType{AttrTypes: wireguardPeerModel{}.AttributeTypes()},
		"preshared_key_enabled": types.BoolType,
		"preshared_key":         types.StringType,
		"interface":             types.StringType,
		"dns_servers":           types.ListType{ElemType: types.StringType},
	}
}

// vpnClientResourceModel describes the resource data model.
type vpnClientResourceModel struct {
	ID           types.String         `tfsdk:"id"`
	Site         types.String         `tfsdk:"site"`
	Name         types.String         `tfsdk:"name"`
	Enabled      types.Bool           `tfsdk:"enabled"`
	Subnet       cidrtypes.IPv4Prefix `tfsdk:"subnet"`
	DefaultRoute types.Bool           `tfsdk:"default_route"`
	PullDNS      types.Bool           `tfsdk:"pull_dns"`
	Wireguard    types.Object         `tfsdk:"wireguard"`
	Timeouts     timeouts.Value       `tfsdk:"timeouts"`
}

func (r *vpnClientResource) Metadata(
	ctx context.Context,
	req resource.MetadataRequest,
	resp *resource.MetadataResponse,
) {
	resp.TypeName = req.ProviderTypeName + "_vpn_client"
}

// IdentitySchema implements [resource.ResourceWithIdentity].
func (r *vpnClientResource) IdentitySchema(
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

func (r *vpnClientResource) Schema(
	ctx context.Context,
	req resource.SchemaRequest,
	resp *resource.SchemaResponse,
) {
	resp.Schema = resource_vpn_client.VpnClientResourceSchema(ctx)
	// Grafted rather than generated, as everywhere else: timeouts.Attributes
	// is a call, not a literal, so the code specification cannot carry it.
	resp.Schema.Attributes["timeouts"] = timeouts.Attributes(ctx, timeouts.Opts{
		Create: true,
		Read:   true,
		Update: true,
		Delete: true,
	})
}

func (r *vpnClientResource) Configure(
	ctx context.Context,
	req resource.ConfigureRequest,
	resp *resource.ConfigureResponse,
) {
	// Prevent panic if the provider has not been configured.
	if req.ProviderData == nil {
		return
	}

	client, ok := req.ProviderData.(*Client)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Resource Configure Type",
			fmt.Sprintf(
				"Expected *Client, got: %T. Please report this issue to the provider developers.",
				req.ProviderData,
			),
		)
		return
	}

	r.client = client
}

func (r *vpnClientResource) Create(
	ctx context.Context,
	req resource.CreateRequest,
	resp *resource.CreateResponse,
) {
	var data vpnClientResourceModel

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
			"Error Creating VPN Client",
			err.Error(),
		)
		return
	}

	// Convert back to model
	var planData vpnClientResourceModel
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
	idModel := vpnClientIdentityModel{ID: data.ID}
	resp.Diagnostics.Append(resp.Identity.Set(ctx, &idModel)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *vpnClientResource) Read(
	ctx context.Context,
	req resource.ReadRequest,
	resp *resource.ReadResponse,
) {
	var data vpnClientResourceModel

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
				"Error Reading VPN Client",
				"Could not read VPN client ID "+data.ID.ValueString()+": "+err.Error(),
			)
			return
		}
	} else {
		// Get the network by name
		network, err = r.client.GetNetworkByName(ctx, site, data.Name.ValueString())
		if err != nil {
			resp.Diagnostics.AddError(
				"Error Reading VPN Client",
				"Could not read VPN client name "+data.Name.ValueString()+": "+err.Error(),
			)
			return
		}
	}

	// Convert to model
	var priorState vpnClientResourceModel
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
	idModel := vpnClientIdentityModel{ID: data.ID}
	resp.Diagnostics.Append(resp.Identity.Set(ctx, &idModel)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *vpnClientResource) Update(
	ctx context.Context,
	req resource.UpdateRequest,
	resp *resource.UpdateResponse,
) {
	var data vpnClientResourceModel

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
	updatedNetwork, err := r.client.UpdateNetwork(ctx, site, network)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Updating VPN Client",
			err.Error(),
		)
		return
	}

	// Convert back to model
	var planData vpnClientResourceModel
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
	idModel := vpnClientIdentityModel{ID: data.ID}
	resp.Diagnostics.Append(resp.Identity.Set(ctx, &idModel)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *vpnClientResource) Delete(
	ctx context.Context,
	req resource.DeleteRequest,
	resp *resource.DeleteResponse,
) {
	var data vpnClientResourceModel

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
			"Error Deleting VPN Client",
			err.Error(),
		)
		return
	}
}

func (r *vpnClientResource) ImportState(
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
		idModel := vpnClientIdentityModel{ID: types.StringValue(req.ID)}
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
func (r *vpnClientResource) modelToNetwork(
	ctx context.Context,
	model *vpnClientResourceModel,
) (*unifi.Network, diag.Diagnostics) {
	var diags diag.Diagnostics

	network := &unifi.Network{
		Name:                  model.Name.ValueStringPointer(),
		Purpose:               unifi.PurposeVPNClient,
		Enabled:               model.Enabled.ValueBool(),
		IPSubnet:              model.Subnet.ValueStringPointer(),
		VPNType:               util.Ptr("wireguard-client"),
		VPNClientDefaultRoute: model.DefaultRoute.ValueBool(),
		VPNClientPullDNS:      model.PullDNS.ValueBool(),
	}

	// Handle WireGuard configuration
	if !model.Wireguard.IsNull() && !model.Wireguard.IsUnknown() {
		var wireguard wireguardModel
		d := model.Wireguard.As(ctx, &wireguard, basetypes.ObjectAsOptions{})
		diags.Append(d...)
		if !diags.HasError() {
			network.WireguardPrivateKey = wireguard.PrivateKey.ValueStringPointer()
			network.WireguardClientPresharedKeyEnabled = wireguard.PresharedKeyEnabled.ValueBool()
			network.WireguardInterface = wireguard.Interface.ValueStringPointer()

			// Handle DNS servers
			if !wireguard.DnsServers.IsNull() && !wireguard.DnsServers.IsUnknown() {
				var dnsServers []string
				d := wireguard.DnsServers.ElementsAs(ctx, &dnsServers, false)
				diags.Append(d...)
				if !diags.HasError() {
					if len(dnsServers) > 0 {
						network.DHCPDDNS1 = dnsServers[0]
					}
					if len(dnsServers) > 1 {
						network.DHCPDDNS2 = dnsServers[1]
					}
				}
			}

			// Check if configuration (file mode) is set
			// The UniFi API's file mode is not consistently supported across controller
			// versions and environments. Instead, we parse the WireGuard config file and
			// extract the peer parameters to use manual mode, which works universally.
			if !wireguard.Configuration.IsNull() && !wireguard.Configuration.IsUnknown() {
				var config wireguardConfigurationModel
				d := wireguard.Configuration.As(ctx, &config, basetypes.ObjectAsOptions{})
				diags.Append(d...)
				if !diags.HasError() {
					parsed, err := parseWireGuardBase64Config(config.Content.ValueString())
					if err != nil {
						diags.AddError(
							"Invalid WireGuard Configuration File",
							fmt.Sprintf("Failed to parse WireGuard configuration: %s", err),
						)
						return nil, diags
					}

					network.WireguardClientMode = util.Ptr("manual")
					network.WireguardClientPeerPublicKey = util.Ptr(parsed.PublicKey)
					network.WireguardClientPeerIP = util.Ptr(parsed.EndpointIP)
					network.WireguardClientPeerPort = util.Ptr(parsed.EndpointPort)

					// Use private key from config file if not set explicitly
					if parsed.PrivateKey != "" &&
						(wireguard.PrivateKey.IsNull() || wireguard.PrivateKey.IsUnknown()) {
						network.WireguardPrivateKey = util.Ptr(parsed.PrivateKey)
					}

					// Use preshared key from config file if present
					if parsed.PresharedKey != "" {
						network.WireguardClientPresharedKeyEnabled = true
						network.WireguardClientPresharedKey = util.Ptr(parsed.PresharedKey)
					}

					// Use DNS servers from config file if not set explicitly
					if len(parsed.DNS) > 0 && wireguard.DnsServers.IsNull() {
						if len(parsed.DNS) > 0 {
							network.DHCPDDNS1 = parsed.DNS[0]
						}
						if len(parsed.DNS) > 1 {
							network.DHCPDDNS2 = parsed.DNS[1]
						}
					}
				}
			} else if !wireguard.Peer.IsNull() && !wireguard.Peer.IsUnknown() {
				// Check if peer (manual mode) is set
				var peer wireguardPeerModel
				d := wireguard.Peer.As(ctx, &peer, basetypes.ObjectAsOptions{})
				diags.Append(d...)
				if !diags.HasError() {
					network.WireguardClientMode = util.Ptr("manual")
					network.WireguardClientPeerIP = peer.IP.ValueStringPointer()
					network.WireguardClientPeerPort = peer.Port.ValueInt64Pointer()
					network.WireguardClientPeerPublicKey = peer.PublicKey.ValueStringPointer()
				}
			}

			// Preshared key (optional for both modes)
			if wireguard.PresharedKeyEnabled.ValueBool() {
				network.WireguardClientPresharedKey = wireguard.PresharedKey.ValueStringPointer()
			}
		}
	}

	return network, diags
}

// networkToModel converts from unifi.Network to Terraform model.
func (r *vpnClientResource) networkToModel(
	ctx context.Context,
	network *unifi.Network,
	model *vpnClientResourceModel,
	site string,
	priorState *vpnClientResourceModel,
) diag.Diagnostics {
	var diags diag.Diagnostics

	model.ID = types.StringValue(network.ID)
	model.Site = types.StringValue(site)
	model.Name = types.StringPointerValue(network.Name)
	model.Enabled = types.BoolValue(network.Enabled)
	if network.IPSubnet != nil {
		model.Subnet = cidrtypes.NewIPv4PrefixValue(*network.IPSubnet)
	} else {
		model.Subnet = cidrtypes.NewIPv4PrefixNull()
	}
	model.DefaultRoute = types.BoolValue(network.VPNClientDefaultRoute)
	model.PullDNS = types.BoolValue(network.VPNClientPullDNS)

	// Helper function to convert empty strings to null
	strPtrToType := func(ptr *string) types.String {
		if ptr == nil || *ptr == "" {
			return types.StringNull()
		}
		return types.StringValue(*ptr)
	}

	// Check if priorState had a configuration block (file mode was used).
	// Since we convert file mode to manual mode for the API, the API always
	// returns "manual" mode. We need to preserve the configuration from the
	// prior state so Terraform doesn't see a diff.
	priorUsedFileMode := false
	if priorState != nil && !priorState.Wireguard.IsNull() && !priorState.Wireguard.IsUnknown() {
		var priorWG wireguardModel
		d := priorState.Wireguard.As(ctx, &priorWG, basetypes.ObjectAsOptions{})
		diags.Append(d...)
		if !diags.HasError() && !priorWG.Configuration.IsNull() &&
			!priorWG.Configuration.IsUnknown() {
			priorUsedFileMode = true
		}
	}

	// Build WireGuard configuration
	var configurationObj types.Object
	var peerObj types.Object

	if priorUsedFileMode {
		// Prior state used file mode: preserve the configuration block from
		// prior state and set peer to null (the API data came from parsing the file)
		var priorWG wireguardModel
		d := priorState.Wireguard.As(ctx, &priorWG, basetypes.ObjectAsOptions{})
		diags.Append(d...)
		configurationObj = priorWG.Configuration
		peerObj = types.ObjectNull(wireguardPeerModel{}.AttributeTypes())
	} else if network.WireguardClientMode != nil && *network.WireguardClientMode == "manual" {
		// Manual mode: populate peer from API response
		peerValue := wireguardPeerModel{
			IP:        strPtrToType(network.WireguardClientPeerIP),
			Port:      types.Int64PointerValue(network.WireguardClientPeerPort),
			PublicKey: strPtrToType(network.WireguardClientPeerPublicKey),
		}
		var d diag.Diagnostics
		peerObj, d = types.ObjectValueFrom(ctx, peerValue.AttributeTypes(), peerValue)
		diags.Append(d...)
		configurationObj = types.ObjectNull(wireguardConfigurationModel{}.AttributeTypes())
	} else {
		// No mode set or file mode returned by API - both null
		configurationObj = types.ObjectNull(wireguardConfigurationModel{}.AttributeTypes())
		peerObj = types.ObjectNull(wireguardPeerModel{}.AttributeTypes())
	}

	// Build DNS servers list
	var dnsServersList types.List
	if priorUsedFileMode {
		// When file mode is used, DNS servers came from the config file and were
		// passed to the API. Preserve the dns_servers from prior state to avoid diffs.
		var priorWG wireguardModel
		d := priorState.Wireguard.As(ctx, &priorWG, basetypes.ObjectAsOptions{})
		diags.Append(d...)
		dnsServersList = priorWG.DnsServers
	} else {
		var dnsServers []string
		if network.DHCPDDNS1 != "" {
			dnsServers = append(dnsServers, network.DHCPDDNS1)
		}
		if network.DHCPDDNS2 != "" {
			dnsServers = append(dnsServers, network.DHCPDDNS2)
		}
		if len(dnsServers) > 0 {
			var d diag.Diagnostics
			dnsServersList, d = types.ListValueFrom(ctx, types.StringType, dnsServers)
			diags.Append(d...)
		} else {
			dnsServersList = types.ListNull(types.StringType)
		}
	}

	// For private key and preshared key: when file mode was used, preserve the
	// values from prior state since the API may return different representations.
	privateKeyVal := strPtrToType(network.WireguardPrivateKey)
	presharedKeyVal := strPtrToType(network.WireguardClientPresharedKey)
	presharedKeyEnabled := types.BoolValue(network.WireguardClientPresharedKeyEnabled)

	if priorUsedFileMode {
		var priorWG wireguardModel
		d := priorState.Wireguard.As(ctx, &priorWG, basetypes.ObjectAsOptions{})
		diags.Append(d...)
		if !diags.HasError() {
			privateKeyVal = priorWG.PrivateKey
			presharedKeyVal = priorWG.PresharedKey
			presharedKeyEnabled = priorWG.PresharedKeyEnabled
		}
	}

	wireguardValue := wireguardModel{
		PrivateKey:          privateKeyVal,
		Configuration:       configurationObj,
		Peer:                peerObj,
		PresharedKeyEnabled: presharedKeyEnabled,
		PresharedKey:        presharedKeyVal,
		Interface:           types.StringPointerValue(network.WireguardInterface),
		DnsServers:          dnsServersList,
	}

	wireguardObj, d := types.ObjectValueFrom(
		ctx,
		wireguardValue.AttributeTypes(),
		wireguardValue,
	)
	diags.Append(d...)
	model.Wireguard = wireguardObj

	return diags
}

// ListResourceConfigSchema implements [list.ListResource].
func (r *vpnClientResource) ListResourceConfigSchema(
	ctx context.Context,
	req list.ListResourceSchemaRequest,
	resp *list.ListResourceSchemaResponse,
) {
	resp.Schema = listresource_vpn_client.VpnClientListResourceSchema(ctx)
}

// List implements [list.ListResource].
func (r *vpnClientResource) List(
	ctx context.Context,
	req list.ListRequest,
	stream *list.ListResultsStream,
) {
	var config vpnClientListConfigModel

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
	var filters []vpnClientListFilterModel
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
		d.AddError("Error Listing VPN Clients", "Could not list networks: "+err.Error())
		stream.Results = list.ListResultsStreamDiagnostics(d)
		return
	}

	stream.Results = func(push func(list.ListResult) bool) {
		for _, network := range networks {
			// Filter by purpose: only vpn-client networks.
			if network.Purpose != unifi.PurposeVPNClient {
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
			var model vpnClientResourceModel
			result.Diagnostics.Append(
				r.networkToModel(ctx, &network, &model, site, &vpnClientResourceModel{})...)
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
