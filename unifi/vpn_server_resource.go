package unifi

import (
	"context"
	"crypto/rand"
	"encoding/base64"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/list"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/identityschema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/ubiquiti-community/go-unifi/unifi"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_vpn_server"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/resourcekit"
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

// vpnServerResource defines the resource implementation.
// CRUD and both mappers are gone -- the kit does them. What stays is the
// schema, the identity schema, ValidateConfig, and the helpers the descriptor
// calls.
type vpnServerResource struct {
	resourcekit.Resource[vpnServerKitModel, unifi.Network]
}

func newVPNServerKitResource() *vpnServerResource {
	r := &vpnServerResource{}
	r.Spec = vpnServerKitSpec()
	r.SchemaSpec = vpnServerKitSchema()
	r.ListSurface = vpnServerKitList()
	return r
}

func NewVPNServerResource() resource.Resource { return newVPNServerKitResource() }

func NewVPNServerListResource() list.ListResource { return newVPNServerKitResource() }

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

	r.Spec.Backend = vpnServerKitBackend(client.ApiClient)
	r.DefaultSite = client.Site
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
	var model vpnServerKitModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &model)...)
	if resp.Diagnostics.HasError() {
		return
	}
	network, diags := r.Spec.ToSDK(ctx, &model)
	// A configuration this mapper cannot build is a problem the apply will
	// report properly; warning about its fields here would be noise on top of
	// a real error.
	if diags.HasError() || network == nil {
		return
	}
	// BeforeSend is what sets purpose and vpn_type, and droppedOnWrite reads
	// the purpose to choose an encoder. ToSDK alone would hand it a Network
	// with no purpose, which encodes to an error rather than a field list.
	if diags := r.Spec.BeforeSend(ctx, &model, &model, network, nil); diags.HasError() {
		return
	}
	resp.Diagnostics.Append(droppedOnWrite("remote-user VPN", network)...)
}

// THE ASSERTION IS THE GUARD, not decoration. The framework calls ValidateConfig
// only if the type satisfies this interface, so a mistyped signature would mean
// the warning above is simply never raised -- with nothing failing to say so.
// This makes that a compile error.
var _ resource.ResourceWithValidateConfig = &vpnServerResource{}
