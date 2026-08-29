package unifi

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/list"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/ubiquiti-community/go-unifi/unifi"
	resource_vpn_client "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_vpn_client"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/resourcekit"
	"github.com/ubiquiti-community/terraform-provider-unifi/unifi/util"
)

// The four nested models below stay in this package: they are the schema's
// shape, not the kit's — the descriptor's Encode/Decode and tests read them directly.

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
	PrivateKey types.String `tfsdk:"private_key"`
	// PrivateKeyWO/PrivateKeyWOVersion are grafted onto the schema below, not
	// generated, and have no Field in the descriptor -- vpnClientBeforeSend and decodeVPNClientWireguard carry them the same way site_to_site_vpn's pre_shared_key_wo does.
	PrivateKeyWO        types.String `tfsdk:"private_key_wo"`
	PrivateKeyWOVersion types.Int64  `tfsdk:"private_key_wo_version"`
	Configuration       types.Object `tfsdk:"configuration"`
	Peer                types.Object `tfsdk:"peer"`
	PresharedKeyEnabled types.Bool   `tfsdk:"preshared_key_enabled"`
	PresharedKey        types.String `tfsdk:"preshared_key"`
	Interface           types.String `tfsdk:"interface"`
	DnsServers          types.List   `tfsdk:"dns_servers"`
}

func (m wireguardModel) AttributeTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"private_key":            types.StringType,
		"private_key_wo":         types.StringType,
		"private_key_wo_version": types.Int64Type,
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

	// Grafted rather than generated, same reason site_to_site_vpn's pre_shared_key_wo
	// is: the code specification has no write_only member. private_key itself changes
	// from Required to Optional here, with AtLeastOneOf/ConflictsWith against
	// private_key_wo (#372) -- stricter than wlan's passphrase_wo or this surface's
	// own pre_shared_key_wo, since a WireGuard tunnel genuinely needs one of the two.
	wireguard, ok := resp.Schema.Attributes["wireguard"].(schema.SingleNestedAttribute)
	if !ok {
		return
	}
	privateKey, ok := wireguard.Attributes["private_key"].(schema.StringAttribute)
	if ok {
		privateKey.Required = false
		privateKey.Optional = true
		privateKey.MarkdownDescription = "WireGuard private key for this client. Stored in " +
			"state; use `private_key_wo` to avoid persisting the secret."
		privateKey.Validators = append(privateKey.Validators,
			stringvalidator.AtLeastOneOf(
				path.MatchRelative().AtParent().AtName("private_key"),
				path.MatchRelative().AtParent().AtName("private_key_wo"),
			),
			stringvalidator.ConflictsWith(
				path.MatchRelative().AtParent().AtName("private_key_wo"),
			),
		)
		wireguard.Attributes["private_key"] = privateKey
	}
	wireguard.Attributes["private_key_wo"] = schema.StringAttribute{
		MarkdownDescription: "Write-only equivalent of `private_key` (Terraform 1.11+). " +
			"Used at apply time but never written to state, so it can be sourced from " +
			"an ephemeral resource (e.g. a Vault secret). Mutually exclusive with " +
			"`private_key`; pair with `private_key_wo_version` to rotate it.",
		Optional:  true,
		Sensitive: true,
		WriteOnly: true,
		Validators: []validator.String{
			stringvalidator.AtLeastOneOf(
				path.MatchRelative().AtParent().AtName("private_key"),
				path.MatchRelative().AtParent().AtName("private_key_wo"),
			),
			stringvalidator.ConflictsWith(
				path.MatchRelative().AtParent().AtName("private_key"),
			),
		},
	}
	wireguard.Attributes["private_key_wo_version"] = schema.Int64Attribute{
		MarkdownDescription: "Version counter for `private_key_wo`. Terraform cannot " +
			"compare a write-only value between applies, so a rotation has nothing " +
			"else to notice; increment this to trigger one.",
		Optional: true,
		Validators: []validator.Int64{
			int64validator.AtLeast(1),
			int64validator.AlsoRequires(
				path.MatchRelative().AtParent().AtName("private_key_wo"),
			),
		},
	}
	resp.Schema.Attributes["wireguard"] = wireguard
}

// Metadata is here, not promoted from an embedded type: descriptor_policy_test.go's
// kitServedSurfaces resolves each surface's TypeName by parsing this method.
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

// ValidateConfig warns about values the encoder will not send.
//
// The assertion above is the guard, not decoration: the framework only calls
// ValidateConfig if the type satisfies the interface, so a mistyped signature would otherwise silently drop the warning.
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
	// A configuration this mapper can't build is a problem the apply will report
	// properly; warning about its fields here would just be noise on top of a real error.
	if diags.HasError() || network == nil {
		return
	}
	resp.Diagnostics.Append(droppedOnWrite("VPN client", network)...)
}

// wireguardPeerToNetwork writes the peer block's four observed fields,
// including wireguard_client_mode="manual" -- the discriminator decodeVPNClientPeer
// reads back to decide whether there's a peer at all (file mode sets its own three fields elsewhere).
func wireguardPeerToNetwork(peer wireguardPeerModel, network *unifi.Network) {
	network.WireguardClientMode = util.Ptr("manual")
	network.WireguardClientPeerIP = peer.IP.ValueStringPointer()
	network.WireguardClientPeerPort = peer.Port.ValueInt64Pointer()
	network.WireguardClientPeerPublicKey = peer.PublicKey.ValueStringPointer()
}

// wireguardDNSServersToNetwork distributes dns_servers positionally into the two
// observed slots without clearing the unused one; a third never reaches here
// (schema carries SizeBetween(1,2)). Both the configured list and one parsed from a file arrive here, since the distribution is the same either way.
func wireguardDNSServersToNetwork(dnsServers []string, network *unifi.Network) {
	if len(dnsServers) > 0 {
		network.DHCPDDNS1 = util.Ptr(dnsServers[0])
	}
	if len(dnsServers) > 1 {
		network.DHCPDDNS2 = util.Ptr(dnsServers[1])
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

// wireguardDNSServersFromNetwork collects dns_servers from the two observed
// slots, keeping only non-empty ones; pairing with the write above compacts, so a lone slot-two value reads back as the first element.
func wireguardDNSServersFromNetwork(
	ctx context.Context,
	diags *diag.Diagnostics,
	network *unifi.Network,
) types.List {
	servers := collectNonEmptyStringPointers(network.DHCPDDNS1, network.DHCPDDNS2)
	if len(servers) == 0 {
		return types.ListNull(types.StringType)
	}
	list, d := types.ListValueFrom(ctx, types.StringType, servers)
	diags.Append(d...)
	return list
}
