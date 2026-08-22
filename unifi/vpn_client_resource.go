package unifi

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/list"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/ubiquiti-community/go-unifi/unifi"
	resource_vpn_client "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_vpn_client"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/resourcekit"
	"github.com/ubiquiti-community/terraform-provider-unifi/unifi/util"
)

// THE FOUR NESTED MODELS STAY IN THIS PACKAGE because they are the schema's
// shape, not the kit's: the descriptor's Encode and Decode read them, and so do
// the tests that pin what each nested object contains.

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

type vpnClientResource struct {
	resourcekit.Resource[vpnClientResourceModel, unifi.Network]
}

var (
	_ resource.Resource                   = &vpnClientResource{}
	_ resource.ResourceWithImportState    = &vpnClientResource{}
	_ resource.ResourceWithIdentity       = &vpnClientResource{}
	_ resource.ResourceWithValidateConfig = &vpnClientResource{}
	_ list.ListResource                   = &vpnClientResource{}
	_ list.ListResourceWithConfigure      = &vpnClientResource{}
)

func newVPNClientKitResource() *vpnClientResource {
	r := &vpnClientResource{}
	r.Spec = vpnClientKitSpec()
	r.SchemaSpec = vpnClientKitSchema()
	r.ListSurface = vpnClientKitList()
	return r
}

func NewVPNClientResource() resource.Resource { return newVPNClientKitResource() }

func NewVPNClientListResource() list.ListResource { return newVPNClientKitResource() }

// Schema STAYS IN THIS PACKAGE, HAND-WRITTEN, because internal/schemabehaviour
// derives what the provider applies by parsing unifi/*.go for a receiver whose
// Metadata names the surface and whose Schema it can follow, and promotion from
// an embedded type is invisible to a parser. Moving it into the kit would not
// fail; it would go quiet.
func (r *vpnClientResource) Schema(
	ctx context.Context,
	_ resource.SchemaRequest,
	resp *resource.SchemaResponse,
) {
	resp.Schema = resource_vpn_client.VpnClientResourceSchema(ctx)
	resp.Schema.Attributes["timeouts"] = timeouts.Attributes(
		ctx,
		timeouts.Opts{Create: true, Read: true, Update: true, Delete: true},
	)
}

// Metadata is here for the same reason: the deriver reads the surface name off
// this method, in this package.
func (r *vpnClientResource) Metadata(
	_ context.Context,
	req resource.MetadataRequest,
	resp *resource.MetadataResponse,
) {
	resp.TypeName = req.ProviderTypeName + "_vpn_client"
}

func (r *vpnClientResource) Configure(
	_ context.Context,
	req resource.ConfigureRequest,
	resp *resource.ConfigureResponse,
) {
	client, ok := resourceClient(req.ProviderData, &resp.Diagnostics)
	if !ok {
		return
	}
	r.Spec.Backend = vpnClientKitBackend(client.ApiClient)
	r.DefaultSite = client.Site
}

// ValidateConfig WARNS ABOUT VALUES THE ENCODER WILL NOT SEND, and it stays
// here because it is about this surface rather than about the kit. It now
// builds the object through Spec.ToSDK, which is the same conversion
// modelToNetwork was.
//
// THE ASSERTION ABOVE IS THE GUARD, not decoration. The framework calls
// ValidateConfig only if the type satisfies the interface, so a mistyped
// signature would mean the warning is never raised with nothing failing to say
// so.
func (r *vpnClientResource) ValidateConfig(
	ctx context.Context,
	req resource.ValidateConfigRequest,
	resp *resource.ValidateConfigResponse,
) {
	var model vpnClientResourceModel
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
	resp.Diagnostics.Append(droppedOnWrite("VPN client", network)...)
}

// File mode writes the same three fields from a parsed configuration rather
// than from this member, so it does not go through here: that is the
// configuration member's business, not the peer's.
// wireguardPeerToNetwork writes the peer block's FOUR observed fields, and the
// fourth is the one that reads like it belongs to the caller.
//
// wireguard_client_mode is "manual" exactly when the provider supplies peer
// details, so it is part of the peer relation rather than a sibling decision --
// and decodeVPNClientPeer already reads it back as the discriminator for
// whether there is a peer at all. It was assigned by the caller until the claim
// covering these fields had to name what this function writes.
func wireguardPeerToNetwork(peer wireguardPeerModel, network *unifi.Network) {
	network.WireguardClientMode = util.Ptr("manual")
	network.WireguardClientPeerIP = peer.IP.ValueStringPointer()
	network.WireguardClientPeerPort = peer.Port.ValueInt64Pointer()
	network.WireguardClientPeerPublicKey = peer.PublicKey.ValueStringPointer()
}

// wireguardDNSServersToNetwork distributes wireguard.dns_servers positionally
// into the two observed slots. It does not clear the slot it does not use, so
// a shorter list leaves whatever was there.
//
// A third server never reaches here: the schema carries
// listvalidator.SizeBetween(1, 2), so validation rejects it with a diagnostic.
//
// Both the configured list and the one parsed out of a configuration file
// arrive here, because the distribution is the same either way.
func wireguardDNSServersToNetwork(dnsServers []string, network *unifi.Network) {
	if len(dnsServers) > 0 {
		network.DHCPDDNS1 = dnsServers[0]
	}
	if len(dnsServers) > 1 {
		network.DHCPDDNS2 = dnsServers[1]
	}
}

// wireguardPeerFromNetwork reads wireguard.peer back from the three flat
// observed fields. Only manual mode has a peer; the caller decides that.
func wireguardPeerFromNetwork(
	ctx context.Context,
	diags *diag.Diagnostics,
	network *unifi.Network,
) types.Object {
	// The empty string is absent here, as it is everywhere the controller
	// reports one of these.
	absentAsNull := func(ptr *string) types.String {
		if ptr == nil || *ptr == "" {
			return types.StringNull()
		}
		return types.StringValue(*ptr)
	}
	peer := wireguardPeerModel{
		IP:        absentAsNull(network.WireguardClientPeerIP),
		Port:      types.Int64PointerValue(network.WireguardClientPeerPort),
		PublicKey: absentAsNull(network.WireguardClientPeerPublicKey),
	}
	object, d := types.ObjectValueFrom(ctx, peer.AttributeTypes(), peer)
	diags.Append(d...)
	return object
}

// wireguardDNSServersFromNetwork collects wireguard.dns_servers from the two
// observed slots, keeping only the non-empty ones. The write distributes
// positionally and this compacts, so a value in slot two with slot one empty
// reads back as the first element.
func wireguardDNSServersFromNetwork(
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
