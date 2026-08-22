package unifi

import (
	"context"
	"crypto/rand"
	"encoding/base64"
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
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/listresource_vpn_server"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_vpn_server"
	"github.com/ubiquiti-community/terraform-provider-unifi/unifi/util"
)

// Ensure provider defined types fully satisfy framework interfaces.
var (
	_ resource.Resource                = &vpnServerResource{}
	_ resource.ResourceWithImportState = &vpnServerResource{}
	_ resource.ResourceWithIdentity    = &vpnServerResource{}
)

// Ensure provider defined types fully satisfy list interfaces.
var (
	_ list.ListResource              = &vpnServerResource{}
	_ list.ListResourceWithConfigure = &vpnServerResource{}
)

func NewVPNServerResource() resource.Resource {
	return &vpnServerResource{}
}

func NewVPNServerListResource() list.ListResource {
	return &vpnServerResource{}
}

// vpnServerResource defines the resource implementation.
type vpnServerResource struct {
	client *Client
}

type vpnServerIdentityModel struct {
	ID types.String `tfsdk:"id"`
}

// vpnServerListConfigModel describes the list configuration model.
type vpnServerListConfigModel struct {
	Site   types.String `tfsdk:"site"`
	Filter types.List   `tfsdk:"filter"`
}

// vpnServerListFilterModel represents a single name/value filter entry.
type vpnServerListFilterModel struct {
	Name  types.String `tfsdk:"name"`
	Value types.String `tfsdk:"value"`
}

// vpnServerDNSModel describes the DNS configuration for VPN clients.
type vpnServerDNSModel struct {
	Enabled types.Bool `tfsdk:"enabled"`
	Servers types.List `tfsdk:"servers"`
}

func (m vpnServerDNSModel) AttributeTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"enabled": types.BoolType,
		"servers": types.ListType{ElemType: types.StringType},
	}
}

// vpnServerWANModel describes the WAN binding configuration shared across VPN types.
type vpnServerWANModel struct {
	IP        types.String `tfsdk:"ip"`
	Interface types.String `tfsdk:"interface"`
}

func (m vpnServerWANModel) AttributeTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"ip":        types.StringType,
		"interface": types.StringType,
	}
}

// vpnServerWireguardModel describes the WireGuard-specific server configuration.
type vpnServerWireguardModel struct {
	PrivateKey types.String `tfsdk:"private_key"`
	PublicKey  types.String `tfsdk:"public_key"`
	Port       types.Int64  `tfsdk:"port"`
}

func (m vpnServerWireguardModel) AttributeTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"private_key": types.StringType,
		"public_key":  types.StringType,
		"port":        types.Int64Type,
	}
}

// vpnServerL2TPModel describes the L2TP-specific server configuration.
type vpnServerL2TPModel struct {
	AllowWeakCiphers types.Bool   `tfsdk:"allow_weak_ciphers"`
	PreSharedKey     types.String `tfsdk:"pre_shared_key"`
}

func (m vpnServerL2TPModel) AttributeTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"allow_weak_ciphers": types.BoolType,
		"pre_shared_key":     types.StringType,
	}
}

// vpnServerOpenVPNModel describes the OpenVPN-specific server configuration.
type vpnServerOpenVPNModel struct {
	Port             types.Int64  `tfsdk:"port"`
	Mode             types.String `tfsdk:"mode"`
	EncryptionCipher types.String `tfsdk:"encryption_cipher"`
	ServerCrt        types.String `tfsdk:"server_crt"`
	ServerKey        types.String `tfsdk:"server_key"`
	DhKey            types.String `tfsdk:"dh_key"`
	SharedClientKey  types.String `tfsdk:"shared_client_key"`
	SharedClientCrt  types.String `tfsdk:"shared_client_crt"`
	AuthKey          types.String `tfsdk:"auth_key"`
	CaCrt            types.String `tfsdk:"ca_crt"`
	CaKey            types.String `tfsdk:"ca_key"`
}

func (m vpnServerOpenVPNModel) AttributeTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"port":              types.Int64Type,
		"mode":              types.StringType,
		"encryption_cipher": types.StringType,
		"server_crt":        types.StringType,
		"server_key":        types.StringType,
		"dh_key":            types.StringType,
		"shared_client_key": types.StringType,
		"shared_client_crt": types.StringType,
		"auth_key":          types.StringType,
		"ca_crt":            types.StringType,
		"ca_key":            types.StringType,
	}
}

// vpnServerResourceModel describes the resource data model.
type vpnServerResourceModel struct {
	ID              types.String         `tfsdk:"id"`
	Site            types.String         `tfsdk:"site"`
	Name            types.String         `tfsdk:"name"`
	Enabled         types.Bool           `tfsdk:"enabled"`
	Subnet          cidrtypes.IPv4Prefix `tfsdk:"subnet"`
	DNS             types.Object         `tfsdk:"dns"`
	WAN             types.Object         `tfsdk:"wan"`
	RADIUSProfileID types.String         `tfsdk:"radiusprofile_id"`
	Wireguard       types.Object         `tfsdk:"wireguard"`
	L2TP            types.Object         `tfsdk:"l2tp"`
	OpenVPN         types.Object         `tfsdk:"openvpn"`
	Timeouts        timeouts.Value       `tfsdk:"timeouts"`
}

func (r *vpnServerResource) Metadata(
	ctx context.Context,
	req resource.MetadataRequest,
	resp *resource.MetadataResponse,
) {
	resp.TypeName = req.ProviderTypeName + "_vpn_server"
}

// IdentitySchema implements [resource.ResourceWithIdentity].
func (r *vpnServerResource) IdentitySchema(
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

func (r *vpnServerResource) Schema(
	ctx context.Context,
	req resource.SchemaRequest,
	resp *resource.SchemaResponse,
) {
	resp.Schema = resource_vpn_server.VpnServerResourceSchema(ctx)
	// Grafted rather than generated, as everywhere else: timeouts.Attributes
	// is a call, not a literal, so the code specification cannot carry it.
	resp.Schema.Attributes["timeouts"] = timeouts.Attributes(
		ctx,
		timeouts.Opts{Create: true, Read: true, Update: true, Delete: true},
	)
}

func (r *vpnServerResource) Configure(
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

func (r *vpnServerResource) Create(
	ctx context.Context,
	req resource.CreateRequest,
	resp *resource.CreateResponse,
) {
	var data vpnServerResourceModel

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

	network, diags := r.modelToNetwork(ctx, &data)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	site := data.Site.ValueString()
	if site == "" {
		site = r.client.Site
	}

	createdNetwork, err := r.client.CreateNetwork(ctx, site, network)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Creating VPN Server",
			err.Error(),
		)
		return
	}

	var planData vpnServerResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &planData)...)
	if resp.Diagnostics.HasError() {
		return
	}

	diags = r.networkToModel(ctx, createdNetwork, &data, site, &planData)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	idModel := vpnServerIdentityModel{ID: data.ID}
	resp.Diagnostics.Append(resp.Identity.Set(ctx, &idModel)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *vpnServerResource) Read(
	ctx context.Context,
	req resource.ReadRequest,
	resp *resource.ReadResponse,
) {
	var data vpnServerResourceModel

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
		network, err = r.client.GetNetwork(ctx, site, data.ID.ValueString())
		if err != nil {
			resp.Diagnostics.AddError(
				"Error Reading VPN Server",
				"Could not read VPN server ID "+data.ID.ValueString()+": "+err.Error(),
			)
			return
		}
	} else {
		network, err = r.client.GetNetworkByName(ctx, site, data.Name.ValueString())
		if err != nil {
			resp.Diagnostics.AddError(
				"Error Reading VPN Server",
				"Could not read VPN server name "+data.Name.ValueString()+": "+err.Error(),
			)
			return
		}
	}

	var priorState vpnServerResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &priorState)...)
	if resp.Diagnostics.HasError() {
		return
	}

	diags := r.networkToModel(ctx, network, &data, site, &priorState)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	idModel := vpnServerIdentityModel{ID: data.ID}
	resp.Diagnostics.Append(resp.Identity.Set(ctx, &idModel)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *vpnServerResource) Update(
	ctx context.Context,
	req resource.UpdateRequest,
	resp *resource.UpdateResponse,
) {
	var data vpnServerResourceModel

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

	// MASKED, NOT WHOLE-OBJECT. See vpnServerWireFields: the object is built
	// from the plan alone, so a whole-object write sent every unmodelled field
	// as its Go zero.
	//
	// AND NARROWED TO WHAT THIS OBJECT ENCODES, which vpn_server needs and was
	// documented as not needing. go-unifi refuses a mask naming a field the
	// purpose encoder drops, and a VPN server encodes only its own protocol's
	// fields -- so a wireguard server named twelve openvpn and l2tp keys it
	// never emits and every update failed with a 400 from the SDK before the
	// request was built.
	updatedNetwork, err := r.client.UpdateNetworkFields(
		ctx, site, network, networkMaskFor(vpnServerWireFields(), network)...)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Updating VPN Server",
			err.Error(),
		)
		return
	}

	var planData vpnServerResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &planData)...)
	if resp.Diagnostics.HasError() {
		return
	}

	diags = r.networkToModel(ctx, updatedNetwork, &data, site, &planData)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	idModel := vpnServerIdentityModel{ID: data.ID}
	resp.Diagnostics.Append(resp.Identity.Set(ctx, &idModel)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *vpnServerResource) Delete(
	ctx context.Context,
	req resource.DeleteRequest,
	resp *resource.DeleteResponse,
) {
	var data vpnServerResourceModel

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

	name := data.Name.ValueString()
	err := r.client.DeleteNetwork(ctx, site, data.ID.ValueString(), name)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Deleting VPN Server",
			err.Error(),
		)
		return
	}
}

func (r *vpnServerResource) ImportState(
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
		idModel := vpnServerIdentityModel{ID: types.StringValue(req.ID)}
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
func (r *vpnServerResource) modelToNetwork(
	ctx context.Context,
	model *vpnServerResourceModel,
) (*unifi.Network, diag.Diagnostics) {
	var diags diag.Diagnostics

	network := &unifi.Network{
		Name:              model.Name.ValueStringPointer(),
		Purpose:           unifi.PurposeUserVPN,
		Enabled:           model.Enabled.ValueBool(),
		IPSubnet:          model.Subnet.ValueStringPointer(),
		SettingPreference: util.Ptr("manual"),
	}

	// Determine VPN type from which nested block is configured
	hasWireguard := !model.Wireguard.IsNull() && !model.Wireguard.IsUnknown()
	hasL2TP := !model.L2TP.IsNull() && !model.L2TP.IsUnknown()
	hasOpenVPN := !model.OpenVPN.IsNull() && !model.OpenVPN.IsUnknown()

	switch {
	case hasWireguard:
		network.VPNType = util.Ptr("wireguard-server")
	case hasL2TP:
		network.VPNType = util.Ptr("l2tp-server")
	case hasOpenVPN:
		network.VPNType = util.Ptr("openvpn-server")
	default:
		diags.AddError(
			"Missing VPN Type Configuration",
			"Exactly one of `wireguard`, `l2tp`, or `openvpn` must be specified.",
		)
		return nil, diags
	}

	// Handle DNS configuration (shared across all VPN types)
	if !model.DNS.IsNull() && !model.DNS.IsUnknown() {
		var dns vpnServerDNSModel
		d := model.DNS.As(ctx, &dns, basetypes.ObjectAsOptions{})
		diags.Append(d...)
		if !diags.HasError() {
			if !dns.Enabled.IsNull() && !dns.Enabled.IsUnknown() {
				network.DHCPDDNSEnabled = dns.Enabled.ValueBool()
			}

			if !dns.Servers.IsNull() && !dns.Servers.IsUnknown() {
				var dnsServers []string
				d := dns.Servers.ElementsAs(ctx, &dnsServers, false)
				diags.Append(d...)
				if !diags.HasError() {
					vpnServerDNSServersToNetwork(dnsServers, network)
					// Default enabled to true when servers are specified
					if len(dnsServers) > 0 &&
						(dns.Enabled.IsNull() || dns.Enabled.IsUnknown()) {
						network.DHCPDDNSEnabled = true
					}
				}
			}
		}
	}

	// Handle WAN configuration (shared, but mapped to VPN-type-specific API fields)
	if !model.WAN.IsNull() && !model.WAN.IsUnknown() {
		var wan vpnServerWANModel
		d := model.WAN.As(ctx, &wan, basetypes.ObjectAsOptions{})
		diags.Append(d...)
		if !diags.HasError() {
			vpnServerWANIPToNetwork(wan.IP, network)
			vpnServerWANInterfaceToNetwork(wan.Interface, network)
		}
	}

	// RADIUS profile ID (applicable to L2TP and OpenVPN)
	if !model.RADIUSProfileID.IsNull() && !model.RADIUSProfileID.IsUnknown() {
		network.RADIUSProfileID = model.RADIUSProfileID.ValueStringPointer()
	}

	// Handle WireGuard-specific configuration
	if hasWireguard {
		var wireguard vpnServerWireguardModel
		d := model.Wireguard.As(ctx, &wireguard, basetypes.ObjectAsOptions{})
		diags.Append(d...)
		if !diags.HasError() {
			if !wireguard.PrivateKey.IsNull() && !wireguard.PrivateKey.IsUnknown() {
				network.WireguardPrivateKey = wireguard.PrivateKey.ValueStringPointer()
			} else {
				// The controller does not generate a key on create (it rejects
				// with api.err.WireguardMissingPrivateKey), so generate one
				// provider-side. On update the key is resolved from state via
				// UseStateForUnknown, so this branch only runs at create.
				key, err := generateWireGuardPrivateKey()
				if err != nil {
					diags.AddError("Unable to generate WireGuard private key", err.Error())
				} else {
					network.WireguardPrivateKey = &key
				}
			}
			vpnServerLocalPortToNetwork(wireguard.Port, network)
		}
	}

	// Handle L2TP-specific configuration
	if hasL2TP {
		var l2tp vpnServerL2TPModel
		d := model.L2TP.As(ctx, &l2tp, basetypes.ObjectAsOptions{})
		diags.Append(d...)
		if !diags.HasError() {
			network.L2TpAllowWeakCiphers = l2tp.AllowWeakCiphers.ValueBool()
			network.IPSecPreSharedKey = l2tp.PreSharedKey.ValueStringPointer()
		}
	}

	// Handle OpenVPN-specific configuration
	if hasOpenVPN {
		var openvpn vpnServerOpenVPNModel
		d := model.OpenVPN.As(ctx, &openvpn, basetypes.ObjectAsOptions{})
		diags.Append(d...)
		if !diags.HasError() {
			vpnServerLocalPortToNetwork(openvpn.Port, network)
			network.OpenVPNMode = openvpn.Mode.ValueStringPointer()
			network.OpenVPNEncryptionCipher = openvpn.EncryptionCipher.ValueStringPointer()

			// Send the controller-generated certificate and key material back on
			// update, and only then. On create these are unknown, and an unknown
			// yields a pointer to "" rather than nil, which puts empty x_ca_crt,
			// x_ca_key, x_dh_key and x_server_crt on the wire. The controller
			// issues that material itself, so a create must not assert it.
			network.ServerCrt = knownNonEmpty(openvpn.ServerCrt)
			network.ServerKey = knownNonEmpty(openvpn.ServerKey)
			network.DhKey = knownNonEmpty(openvpn.DhKey)
			network.SharedClientKey = knownNonEmpty(openvpn.SharedClientKey)
			network.SharedClientCrt = knownNonEmpty(openvpn.SharedClientCrt)
			network.AuthKey = knownNonEmpty(openvpn.AuthKey)
			network.CaCrt = knownNonEmpty(openvpn.CaCrt)
			network.CaKey = knownNonEmpty(openvpn.CaKey)
		}
	}

	return network, diags
}

// knownNonEmpty returns a pointer to v's value, or nil when it is null,
// unknown, or the empty string. Fields the controller generates must be absent
// from the payload rather than present and empty.
func knownNonEmpty(v types.String) *string {
	if v.IsNull() || v.IsUnknown() || v.ValueString() == "" {
		return nil
	}
	return v.ValueStringPointer()
}

// networkToModel converts from unifi.Network to Terraform model.
func (r *vpnServerResource) networkToModel(
	ctx context.Context,
	network *unifi.Network,
	model *vpnServerResourceModel,
	site string,
	priorState *vpnServerResourceModel,
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

	// Build DNS nested object
	{
		dnsServersList := vpnServerDNSServersFromNetwork(ctx, &diags, network)

		dnsValue := vpnServerDNSModel{
			Enabled: types.BoolValue(network.DHCPDDNSEnabled),
			Servers: dnsServersList,
		}
		var d diag.Diagnostics
		model.DNS, d = types.ObjectValueFrom(ctx, vpnServerDNSModel{}.AttributeTypes(), dnsValue)
		diags.Append(d...)
	}

	// Determine VPN type from the API response
	vpnType := ""
	if network.VPNType != nil {
		vpnType = *network.VPNType
	}

	// Build WAN nested object with VPN-type-specific API field mapping
	{
		wanValue := vpnServerWANModel{
			IP:        vpnServerWANIPFromNetwork(network),
			Interface: vpnServerWANInterfaceFromNetwork(network),
		}
		var d diag.Diagnostics
		model.WAN, d = types.ObjectValueFrom(ctx, vpnServerWANModel{}.AttributeTypes(), wanValue)
		diags.Append(d...)
	}

	// RADIUS profile ID
	if network.RADIUSProfileID != nil && *network.RADIUSProfileID != "" {
		model.RADIUSProfileID = types.StringPointerValue(network.RADIUSProfileID)
	} else {
		model.RADIUSProfileID = types.StringNull()
	}

	// Build VPN-type-specific nested objects
	switch vpnType {
	case "wireguard-server":
		// Preserve private key from prior state if the API doesn't return it
		privateKeyVal := types.StringPointerValue(network.WireguardPrivateKey)
		if (privateKeyVal.IsNull() || privateKeyVal.ValueString() == "") &&
			priorState != nil && !priorState.Wireguard.IsNull() && !priorState.Wireguard.IsUnknown() {
			var priorWG vpnServerWireguardModel
			d := priorState.Wireguard.As(ctx, &priorWG, basetypes.ObjectAsOptions{})
			diags.Append(d...)
			if !diags.HasError() {
				privateKeyVal = priorWG.PrivateKey
			}
		}

		strPtrToType := func(ptr *string) types.String {
			if ptr == nil || *ptr == "" {
				return types.StringNull()
			}
			return types.StringValue(*ptr)
		}

		// DERIVED WHEN THE CONTROLLER DOES NOT SEND ONE, which on 10.4.57 is
		// always: wireguard_public_key is absent on create, absent after every
		// update, and absent forever, so this attribute was null for its whole
		// life. Nothing errored, because Computed plus UseStateForUnknown makes
		// a null plan agree with a null read -- it was consistently empty.
		//
		// A practitioner reading it into a peer configuration or an output got
		// an empty string and no diagnostic, against a description promising a
		// value computed from the private key. That is now true.
		publicKeyVal := strPtrToType(network.WireguardPublicKey)
		if publicKeyVal.IsNull() && !privateKeyVal.IsNull() && privateKeyVal.ValueString() != "" {
			derived, err := wireguardPublicKey(privateKeyVal.ValueString())
			if err != nil {
				// REPORTED, NOT SWALLOWED. Falling back to null here would
				// restore the behaviour this replaces, and silently: the
				// practitioner would see the same empty string and have no way
				// to learn the key was malformed.
				diags.AddError(
					"Cannot derive the WireGuard public key",
					"The controller does not return wireguard_public_key, so the provider "+
						"derives it from the private key. That failed: "+err.Error(),
				)
			} else {
				publicKeyVal = types.StringValue(derived)
			}
		}

		wireguardValue := vpnServerWireguardModel{
			PrivateKey: privateKeyVal,
			PublicKey:  publicKeyVal,
			Port:       vpnServerLocalPortFromNetwork(network),
		}
		var d diag.Diagnostics
		model.Wireguard, d = types.ObjectValueFrom(
			ctx,
			vpnServerWireguardModel{}.AttributeTypes(),
			wireguardValue,
		)
		diags.Append(d...)
		model.L2TP = types.ObjectNull(vpnServerL2TPModel{}.AttributeTypes())
		model.OpenVPN = types.ObjectNull(vpnServerOpenVPNModel{}.AttributeTypes())

	case "l2tp-server":
		// Preserve pre-shared key from prior state since the API does not return it
		pskVal := types.StringPointerValue(network.IPSecPreSharedKey)
		if (pskVal.IsNull() || pskVal.ValueString() == "") &&
			priorState != nil && !priorState.L2TP.IsNull() && !priorState.L2TP.IsUnknown() {
			var priorL2TP vpnServerL2TPModel
			d := priorState.L2TP.As(ctx, &priorL2TP, basetypes.ObjectAsOptions{})
			diags.Append(d...)
			if !diags.HasError() {
				pskVal = priorL2TP.PreSharedKey
			}
		}

		l2tpValue := vpnServerL2TPModel{
			AllowWeakCiphers: types.BoolValue(network.L2TpAllowWeakCiphers),
			PreSharedKey:     pskVal,
		}
		var d diag.Diagnostics
		model.L2TP, d = types.ObjectValueFrom(ctx, vpnServerL2TPModel{}.AttributeTypes(), l2tpValue)
		diags.Append(d...)
		model.Wireguard = types.ObjectNull(vpnServerWireguardModel{}.AttributeTypes())
		model.OpenVPN = types.ObjectNull(vpnServerOpenVPNModel{}.AttributeTypes())

	case "openvpn-server":
		openvpnValue := vpnServerOpenVPNModel{
			Port:             vpnServerLocalPortFromNetwork(network),
			Mode:             types.StringPointerValue(network.OpenVPNMode),
			EncryptionCipher: types.StringPointerValue(network.OpenVPNEncryptionCipher),
			ServerCrt:        types.StringPointerValue(network.ServerCrt),
			ServerKey:        types.StringPointerValue(network.ServerKey),
			DhKey:            types.StringPointerValue(network.DhKey),
			SharedClientKey:  types.StringPointerValue(network.SharedClientKey),
			SharedClientCrt:  types.StringPointerValue(network.SharedClientCrt),
			AuthKey:          types.StringPointerValue(network.AuthKey),
			CaCrt:            types.StringPointerValue(network.CaCrt),
			CaKey:            types.StringPointerValue(network.CaKey),
		}
		var d diag.Diagnostics
		model.OpenVPN, d = types.ObjectValueFrom(
			ctx,
			vpnServerOpenVPNModel{}.AttributeTypes(),
			openvpnValue,
		)
		diags.Append(d...)
		model.Wireguard = types.ObjectNull(vpnServerWireguardModel{}.AttributeTypes())
		model.L2TP = types.ObjectNull(vpnServerL2TPModel{}.AttributeTypes())

	default:
		// Unknown VPN type — null out all type-specific blocks
		model.Wireguard = types.ObjectNull(vpnServerWireguardModel{}.AttributeTypes())
		model.L2TP = types.ObjectNull(vpnServerL2TPModel{}.AttributeTypes())
		model.OpenVPN = types.ObjectNull(vpnServerOpenVPNModel{}.AttributeTypes())
	}

	return diags
}

// ListResourceConfigSchema implements [list.ListResource].
func (r *vpnServerResource) ListResourceConfigSchema(
	ctx context.Context,
	req list.ListResourceSchemaRequest,
	resp *list.ListResourceSchemaResponse,
) {
	resp.Schema = listresource_vpn_server.VpnServerListResourceSchema(ctx)
}

// List implements [list.ListResource].
func (r *vpnServerResource) List(
	ctx context.Context,
	req list.ListRequest,
	stream *list.ListResultsStream,
) {
	var config vpnServerListConfigModel

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
	var filters []vpnServerListFilterModel
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
		d.AddError("Error Listing VPN Servers", "Could not list networks: "+err.Error())
		stream.Results = list.ListResultsStreamDiagnostics(d)
		return
	}

	stream.Results = func(push func(list.ListResult) bool) {
		for _, network := range networks {
			// Filter by purpose: only remote-user-vpn networks.
			if network.Purpose != unifi.PurposeUserVPN {
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
			var model vpnServerResourceModel
			result.Diagnostics.Append(
				r.networkToModel(ctx, &network, &model, site, &vpnServerResourceModel{})...)
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

// generateWireGuardPrivateKey returns a fresh base64-encoded Curve25519 private
// key in the WireGuard format. The UniFi controller does not generate a key for
// a WireGuard VPN server (it rejects creation with api.err.WireguardMissingPrivateKey),
// so the provider generates one when the user does not supply it.
func generateWireGuardPrivateKey() (string, error) {
	var key [32]byte
	if _, err := rand.Read(key[:]); err != nil {
		return "", err
	}
	// Curve25519 clamping, per the WireGuard key format.
	key[0] &= 248
	key[31] &= 127
	key[31] |= 64
	return base64.StdEncoding.EncodeToString(key[:]), nil
}

// The eight functions below are the halves this resource's policy claims name.
// Each relates one Terraform member to several observed fields, which is the
// one thing the compiler cannot check, so the policy names a function and a
// reader opens it. They were inline in modelToNetwork and networkToModel.
//
// The three type-specific relations all switch on network.VPNType, which
// modelToNetwork sets from which block is configured before any of them run,
// and which networkToModel reads back off the wire. One vocabulary, both
// directions.

// vpnServerLocalPortToNetwork writes whichever type's port is configured into
// the one observed local_port. The released schema exposes it under each type's
// own block, so two members relate to the one field and only one of them is
// ever set.
func vpnServerLocalPortToNetwork(port types.Int64, network *unifi.Network) {
	network.LocalPort = port.ValueInt64Pointer()
}

// vpnServerLocalPortFromNetwork reads local_port back. The caller places it
// under the block belonging to the type the controller reports.
func vpnServerLocalPortFromNetwork(network *unifi.Network) types.Int64 {
	return types.Int64PointerValue(network.LocalPort)
}

// vpnServerWANIPToNetwork writes wan.ip into the field belonging to the
// configured VPN type. A type the controller does not name writes nothing,
// which is what the switch did before.
func vpnServerWANIPToNetwork(ip types.String, network *unifi.Network) {
	switch vpnServerType(network) {
	case "wireguard-server":
		network.WireguardLocalWANIP = ip.ValueStringPointer()
	case "l2tp-server":
		network.L2TpLocalWANIP = ip.ValueStringPointer()
	case "openvpn-server":
		network.OpenVPNLocalWANIP = ip.ValueStringPointer()
	}
}

// vpnServerWANIPFromNetwork reads wan.ip from whichever of the three is set.
func vpnServerWANIPFromNetwork(network *unifi.Network) types.String {
	switch vpnServerType(network) {
	case "wireguard-server":
		return types.StringPointerValue(network.WireguardLocalWANIP)
	case "l2tp-server":
		return types.StringPointerValue(network.L2TpLocalWANIP)
	case "openvpn-server":
		return types.StringPointerValue(network.OpenVPNLocalWANIP)
	}
	return types.StringPointerValue(nil)
}

// vpnServerWANInterfaceToNetwork writes wan.interface into the field belonging
// to the configured VPN type.
func vpnServerWANInterfaceToNetwork(iface types.String, network *unifi.Network) {
	switch vpnServerType(network) {
	case "wireguard-server":
		network.WireguardInterface = iface.ValueStringPointer()
	case "l2tp-server":
		network.L2TpInterface = iface.ValueStringPointer()
	case "openvpn-server":
		network.OpenVPNInterface = iface.ValueStringPointer()
	}
}

// vpnServerWANInterfaceFromNetwork reads wan.interface from whichever of the
// three is set.
func vpnServerWANInterfaceFromNetwork(network *unifi.Network) types.String {
	switch vpnServerType(network) {
	case "wireguard-server":
		return types.StringPointerValue(network.WireguardInterface)
	case "l2tp-server":
		return types.StringPointerValue(network.L2TpInterface)
	case "openvpn-server":
		return types.StringPointerValue(network.OpenVPNInterface)
	}
	return types.StringPointerValue(nil)
}

// vpnServerDNSServersToNetwork distributes dns.servers positionally into the
// two observed slots. It does not clear the slot it does not use, so a shorter
// list leaves whatever was there.
//
// A THIRD SERVER IS SILENTLY DROPPED, and this is the only one of the three
// that does it: network's dhcp_server.dns_servers carries
// listvalidator.SizeAtMost(4) and vpn_client's wireguard.dns_servers carries
// SizeBetween(1, 2), so both refuse an over-long list with a diagnostic. This
// attribute carries no size validator at all, so a third is accepted, applied,
// and never written. Reported rather than fixed here: adding the bound is a
// public schema change, not a refactor.
func vpnServerDNSServersToNetwork(dnsServers []string, network *unifi.Network) {
	if len(dnsServers) > 0 {
		network.DHCPDDNS1 = dnsServers[0]
	}
	if len(dnsServers) > 1 {
		network.DHCPDDNS2 = dnsServers[1]
	}
}

// vpnServerDNSServersFromNetwork collects dns.servers from the two observed
// slots, keeping only the non-empty ones. The write distributes positionally
// and this compacts, so a value in slot two with slot one empty reads back as
// the first element.
func vpnServerDNSServersFromNetwork(
	ctx context.Context,
	diags *diag.Diagnostics,
	network *unifi.Network,
) types.List {
	var servers []string
	if network.DHCPDDNS1 != "" {
		servers = append(servers, network.DHCPDDNS1)
	}
	if network.DHCPDDNS2 != "" {
		servers = append(servers, network.DHCPDDNS2)
	}
	if len(servers) == 0 {
		return types.ListNull(types.StringType)
	}
	list, d := types.ListValueFrom(ctx, types.StringType, servers)
	diags.Append(d...)
	return list
}

// vpnServerType is the controller's own name for which VPN a network serves,
// empty when it names none.
func vpnServerType(network *unifi.Network) string {
	if network.VPNType == nil {
		return ""
	}
	return *network.VPNType
}

// ValidateConfig warns when the configuration sets a value the controller will
// not receive for this kind of network.
//
// go-unifi serialises a Network through one of seven per-purpose structs, and
// any field the chosen one omits is discarded with no diagnostic at any layer:
// the plan is clean, the apply succeeds, and the controller keeps what it had.
// Measured across the provider's attributes, 62 can be set and never arrive.
//
// AT PLAN TIME, so a practitioner sees it before applying rather than after.
// The cost is that an attribute still unknown at plan time reads as unset here
// and goes unreported -- a miss rather than a false alarm, which is the right
// way round for a warning.
func (r *vpnServerResource) ValidateConfig(
	ctx context.Context,
	req resource.ValidateConfigRequest,
	resp *resource.ValidateConfigResponse,
) {
	var model vpnServerResourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &model)...)
	if resp.Diagnostics.HasError() {
		return
	}
	network, diags := r.modelToNetwork(ctx, &model)
	// A configuration this mapper cannot build is a problem the apply will
	// report properly; warning about its fields here would be noise on top of
	// a real error.
	if diags.HasError() || network == nil {
		return
	}
	resp.Diagnostics.Append(droppedOnWrite("remote-user VPN", network)...)
}

// THE ASSERTION IS THE GUARD, not decoration. The framework calls ValidateConfig
// only if the type satisfies this interface, so a mistyped signature would mean
// the warning above is simply never raised -- with nothing failing to say so.
// This makes that a compile error.
var _ resource.ResourceWithValidateConfig = &vpnServerResource{}
