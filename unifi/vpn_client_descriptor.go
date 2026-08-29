package unifi

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-nettypes/cidrtypes"
	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/listresource_vpn_client"
	resource_vpn_client "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_vpn_client"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/resourcekit"
	"github.com/ubiquiti-community/terraform-provider-unifi/unifi/util"
)

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

func vpnClientPtr(
	wire string,
	model func(*vpnClientResourceModel) *types.String,
	sdk func(*ui.Network) **string,
) resourcekit.StringLikePtrField[vpnClientResourceModel, ui.Network, types.String] {
	return resourcekit.StringLikePtrField[vpnClientResourceModel, ui.Network, types.String]{
		Wire: wire, Model: model, SDK: sdk,
		New: func(v basetypes.StringValue) types.String { return v },
	}
}

func vpnClientBool(
	wire string,
	model func(*vpnClientResourceModel) *types.Bool,
	sdk func(*ui.Network) *bool,
) resourcekit.BoolField[vpnClientResourceModel, ui.Network] {
	return resourcekit.BoolField[vpnClientResourceModel, ui.Network]{Wire: wire, Model: model, SDK: sdk}
}

// vpnClientAfterReceive carries forward what the controller cannot report.
//
// prior is the plan on create, the state on read, and the state with the
// plan applied on update.
func vpnClientAfterReceive(
	ctx context.Context,
	_ *ui.Network,
	model *vpnClientResourceModel,
	prior vpnClientResourceModel,
	_ any,
) diag.Diagnostics {
	var diags diag.Diagnostics
	if prior.Wireguard.IsNull() || prior.Wireguard.IsUnknown() {
		return diags
	}
	var before wireguardModel
	diags.Append(prior.Wireguard.As(ctx, &before, basetypes.ObjectAsOptions{})...)
	if diags.HasError() {
		return diags
	}
	if before.Configuration.IsNull() || before.Configuration.IsUnknown() {
		return diags
	}
	if model.Wireguard.IsNull() || model.Wireguard.IsUnknown() {
		return diags
	}
	var after wireguardModel
	diags.Append(model.Wireguard.As(ctx, &after, basetypes.ObjectAsOptions{})...)
	if diags.HasError() {
		return diags
	}

	// configuration and peer are mutually exclusive; the schema accepts only one.
	after.Configuration = before.Configuration
	after.Peer = types.ObjectNull(wireguardPeerModel{}.AttributeTypes())
	// The controller reports DNS servers back in the file's order, not the
	// block's, so prior is what matches what the practitioner wrote.
	after.DnsServers = before.DnsServers
	// File mode only: the controller's stored key parsed from a file need not
	// match the file's own bytes, so prior is authoritative here.
	after.PrivateKey = before.PrivateKey
	after.PresharedKey = before.PresharedKey
	after.PresharedKeyEnabled = before.PresharedKeyEnabled

	object, d := types.ObjectValueFrom(ctx, after.AttributeTypes(), after)
	diags.Append(d...)
	if diags.HasError() {
		return diags
	}
	model.Wireguard = object
	return diags
}

// vpnClientBeforeSend sets the WireGuard private key from the write-only
// wireguard.private_key_wo, reading it from config since Terraform nulls
// write-only attributes in every persisted plan.
func vpnClientBeforeSend(
	ctx context.Context,
	config, _ *vpnClientResourceModel,
	_ vpnClientResourceModel,
	sdk *ui.Network,
	_ any,
) diag.Diagnostics {
	var diags diag.Diagnostics
	if config.Wireguard.IsNull() || config.Wireguard.IsUnknown() {
		return diags
	}
	var wireguard wireguardModel
	diags.Append(config.Wireguard.As(ctx, &wireguard, basetypes.ObjectAsOptions{})...)
	if diags.HasError() {
		return diags
	}
	if wo := wireguard.PrivateKeyWO; !wo.IsNull() && !wo.IsUnknown() && wo.ValueString() != "" {
		sdk.WireguardPrivateKey = wo.ValueStringPointer()
	}
	return diags
}

func vpnClientKitSpec() resourcekit.Spec[vpnClientResourceModel, ui.Network] {
	return resourcekit.Spec[vpnClientResourceModel, ui.Network]{
		TypeName: "vpn_client",
		Subject:  "VPN Client",
		New: func() *ui.Network {
			return &ui.Network{
				Purpose: ui.PurposeVPNClient,
				VPNType: util.Ptr("wireguard-client"),
			}
		},
		ID:       func(m *vpnClientResourceModel) *types.String { return &m.ID },
		Site:     func(m *vpnClientResourceModel) *types.String { return &m.Site },
		Timeouts: func(m *vpnClientResourceModel) *timeouts.Value { return &m.Timeouts },
		// AlwaysWire is required: no attribute carries purpose/vpn_type, so
		// without it an update omits them and the controller picks the wrong encoder.
		AlwaysWire:   []string{"purpose", "vpn_type"},
		BeforeSend:   vpnClientBeforeSend,
		AfterReceive: vpnClientAfterReceive,
		Fields: []resourcekit.Field[vpnClientResourceModel, ui.Network]{
			vpnClientPtr("name", func(m *vpnClientResourceModel) *types.String { return &m.Name },
				func(s *ui.Network) **string { return &s.Name }),
			vpnClientBool("enabled", func(m *vpnClientResourceModel) *types.Bool { return &m.Enabled },
				func(s *ui.Network) *bool { return &s.Enabled }),
			resourcekit.StringLikePtrField[vpnClientResourceModel, ui.Network, cidrtypes.IPv4Prefix]{
				Wire:  "ip_subnet",
				Model: func(m *vpnClientResourceModel) *cidrtypes.IPv4Prefix { return &m.Subnet },
				SDK:   func(s *ui.Network) **string { return &s.IPSubnet },
				New: func(v basetypes.StringValue) cidrtypes.IPv4Prefix {
					return cidrtypes.IPv4Prefix{StringValue: v}
				},
			},
			vpnClientBool("vpn_client_default_route",
				func(m *vpnClientResourceModel) *types.Bool { return &m.DefaultRoute },
				func(s *ui.Network) *bool { return &s.VPNClientDefaultRoute }),
			vpnClientBool("vpn_client_pull_dns",
				func(m *vpnClientResourceModel) *types.Bool { return &m.PullDNS },
				func(s *ui.Network) *bool { return &s.VPNClientPullDNS }),
			// Declared inline, not via a helper: the mapping reader parses a Fields
			// entry as a composite literal, so indirection would hide these wire
			// names and their attributes would silently stop applying.
			resourcekit.ScatteredObjectField[vpnClientResourceModel, ui.Network]{
				Wires: []string{
					"x_wireguard_private_key",
					"wireguard_interface",
					"wireguard_client_preshared_key_enabled",
					"wireguard_client_preshared_key",
					"wireguard_client_mode",
					"wireguard_client_peer_public_key",
					"wireguard_client_peer_ip",
					"wireguard_client_peer_port",
					"dhcpd_dns_1",
					"dhcpd_dns_2",
				},
				Model:     func(m *vpnClientResourceModel) *types.Object { return &m.Wireguard },
				AttrTypes: wireguardModel{}.AttributeTypes(),
				ConditionalWires: map[string]func(types.Object) bool{
					"dhcpd_dns_1":                      vpnClientWireguardWritesDNS(1),
					"dhcpd_dns_2":                      vpnClientWireguardWritesDNS(2),
					"wireguard_client_mode":            vpnClientWireguardWritesPeer,
					"wireguard_client_peer_public_key": vpnClientWireguardWritesPeer,
					"wireguard_client_peer_ip":         vpnClientWireguardWritesPeer,
					"wireguard_client_peer_port":       vpnClientWireguardWritesPeer,
					"wireguard_client_preshared_key":   vpnClientWireguardWritesPresharedKey,
				},
				Encode: encodeVPNClientWireguard,
				Decode: decodeVPNClientWireguard,
			},
		},
	}
}

func vpnClientKitSchema() resourcekit.SchemaSpec {
	return resourcekit.SchemaSpec{
		Resource: resource_vpn_client.VpnClientResourceSchema,
		Timeouts: timeouts.Opts{Create: true, Read: true, Update: true, Delete: true},
	}
}

func vpnClientKitList() resourcekit.ListSpec[ui.Network] {
	return resourcekit.ListSpec[ui.Network]{
		ConfigSchema: listresource_vpn_client.VpnClientListResourceSchema,
		DisplayName: func(s *ui.Network) string {
			if s.Name != nil && *s.Name != "" {
				return *s.Name
			}
			return s.ID
		},
		Filters: map[string]func(*ui.Network) string{
			"name": func(s *ui.Network) string {
				if s.Name == nil {
					return ""
				}
				return *s.Name
			},
		},
	}
}

func vpnClientKitBackend(client *ui.ApiClient) resourcekit.Backend[ui.Network] {
	return resourcekit.Backend[ui.Network]{
		Create: func(ctx context.Context, site string, in *ui.Network) (*ui.Network, error) {
			return client.CreateNetwork(ctx, site, in)
		},
		Read: func(ctx context.Context, site, id string) (*ui.Network, error) {
			return client.GetNetwork(ctx, site, id)
		},
		UpdateFields: func(ctx context.Context, site string, in *ui.Network, fields ...string) (*ui.Network, error) {
			return client.UpdateNetworkFields(ctx, site, in, fields...)
		},
		// The controller's delete call requires the network's name, which
		// Backend.Delete's site/id signature doesn't carry, so this reads it first.
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
