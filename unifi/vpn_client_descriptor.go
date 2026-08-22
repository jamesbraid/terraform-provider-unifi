package unifi

// The vpn_client descriptor.
//
// THE SURFACE WHOSE BEHAVIOUR IS MOSTLY PRIOR-STATE CARRY-FORWARD. The
// practitioner supplies a wireguard config FILE, the provider parses it and
// writes manual mode because the controller's own file mode is not consistently
// supported, and the controller then reports manual mode forever. So the
// configuration block, the DNS servers and both write-only keys come from what
// was there before rather than from the wire -- five attributes that no Decode
// can produce, because Spec.ToModel has already overwritten them by the time any
// hook runs. AfterReceive's prior model exists for this.
//
// PURPOSE AND VPN_TYPE ARE CONSTANTS, seeded in New and carried by AlwaysWire.
// purpose selects which of go-unifi's seven alias structs serialises the object,
// so it is not decoration -- and because this surface never varies it, there is
// exactly one alias and it emits all fifteen declared wires. That is why
// NarrowMask is network's alone and not needed here: measured, 15 declared
// against 26 emitted, none missing.

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
// IT READS PRIOR AND NOT model, and the difference is the whole point.
// Spec.ToModel writes into the model the operation started with, so every
// attribute a Field owns -- and wireguard is one -- already holds the
// controller's answer by the time this runs. prior is the plan on a create, the
// state on a read, and the state with the plan applied on an update, which is
// exactly what the hand-written networkToModel took as its priorState argument
// at those same three call sites.
//
// FILE MODE IS THE ONLY BRANCH THAT CARRIES ANYTHING. A practitioner who wrote
// a peer block gets the controller's peer back, unchanged from what Decode
// produced; the hand-written read did the same, and a carry-forward on that
// path would freeze a peer the controller had moved.
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

	// THE CONFIGURATION REPLACES THE PEER RATHER THAN JOINING IT. They are the
	// two arms of one switch and the schema accepts only one, so a refresh that
	// returned both would not fit the state it is written into.
	after.Configuration = before.Configuration
	after.Peer = types.ObjectNull(wireguardPeerModel{}.AttributeTypes())
	// The DNS servers came from the file and were sent to the controller. It
	// reports them back, but from the file's ordering rather than the block's,
	// so prior is the one that matches what the practitioner wrote.
	after.DnsServers = before.DnsServers
	// Neither key is ever reported. Prior state is the only place they exist.
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

func vpnClientKitSpec() resourcekit.Spec[vpnClientResourceModel, ui.Network] {
	return resourcekit.Spec[vpnClientResourceModel, ui.Network]{
		TypeName: "vpn_client",
		Subject:  "VPN Client",
		// THE TWO CONSTANTS LIVE HERE. Every network this surface writes is a
		// wireguard client, and the hand-written mapper set both on every send.
		New: func() *ui.Network {
			return &ui.Network{
				Purpose: ui.PurposeVPNClient,
				VPNType: util.Ptr("wireguard-client"),
			}
		},
		ID:       func(m *vpnClientResourceModel) *types.String { return &m.ID },
		Site:     func(m *vpnClientResourceModel) *types.String { return &m.Site },
		Timeouts: func(m *vpnClientResourceModel) *timeouts.Value { return &m.Timeouts },
		// NOTHING IN THE PLAN CAN PUT THE CONSTANTS IN THE MASK, because no
		// attribute carries them. Without this the update sends an object whose
		// purpose the controller never sees, and purpose is what selects the
		// encoder.
		AlwaysWire:   []string{"purpose", "vpn_type"},
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
			// THE SCATTERED OBJECT IS DECLARED INLINE, and the tests read it back
			// out of this Spec rather than from a second copy. The mapping
			// reader parses a Fields entry as a composite literal, so a helper
			// returning one would hide every name it declares -- and a hidden
			// name is an attribute the practitioner sets and the apply drops.
			// Encode, Decode and the predicates stay as named functions beside
			// the object they convert.
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
		Backend: resourcekit.Backend[ui.Network]{
			GetID: func(s *ui.Network) string { return s.ID },
			SetID: func(s *ui.Network, id string) { s.ID = id },
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
		// THE DELETE BODY CARRIES THE NAME and Backend.Delete is handed only a
		// site and an id. A read answers it, as network's does: the object is
		// about to be destroyed, and one GET to learn what to call it is cheaper
		// than widening a signature every surface shares.
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
