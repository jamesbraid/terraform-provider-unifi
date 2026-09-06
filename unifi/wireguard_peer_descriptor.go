package unifi

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/types"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/listresource_wireguard_peer"
	resource_wireguard_peer "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_wireguard_peer"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/resourcekit"
)

type wireguardPeerKitModel struct {
	ID          types.String   `tfsdk:"id"`
	Site        types.String   `tfsdk:"site"`
	NetworkID   types.String   `tfsdk:"network_id"`
	Name        types.String   `tfsdk:"name"`
	InterfaceIP types.String   `tfsdk:"interface_ip"`
	PublicKey   types.String   `tfsdk:"public_key"`
	AllowedIPs  types.List     `tfsdk:"allowed_ips"`
	Timeouts    timeouts.Value `tfsdk:"timeouts"`
}

func wireguardPeerKitSpec() resourcekit.Spec[wireguardPeerKitModel, ui.WireGuardPeer] {
	return resourcekit.Spec[wireguardPeerKitModel, ui.WireGuardPeer]{
		TypeName: "wireguard_peer",
		Subject:  "WireGuard Peer",
		IDWire:   "_id",
		New:      func() *ui.WireGuardPeer { return &ui.WireGuardPeer{} },
		ID:       func(m *wireguardPeerKitModel) *types.String { return &m.ID },
		Site:     func(m *wireguardPeerKitModel) *types.String { return &m.Site },
		Timeouts: func(m *wireguardPeerKitModel) *timeouts.Value { return &m.Timeouts },
		Fields: []resourcekit.Field[wireguardPeerKitModel, ui.WireGuardPeer]{
			// The parent key, doubling as a body field: the batch endpoints
			// carry the network in their URL, and Create clears it from the
			// body (see wireguardPeerKitBackend), but UpdateFields reads it
			// off the object to build that URL.
			resourcekit.StringField[wireguardPeerKitModel, ui.WireGuardPeer]{
				Wire:  "network_id",
				Model: func(m *wireguardPeerKitModel) *types.String { return &m.NetworkID },
				SDK:   func(s *ui.WireGuardPeer) *string { return &s.NetworkID },
				Elide: resourcekit.KeepZero,
			},
			resourcekit.StringField[wireguardPeerKitModel, ui.WireGuardPeer]{
				Wire:  "name",
				Model: func(m *wireguardPeerKitModel) *types.String { return &m.Name },
				SDK:   func(s *ui.WireGuardPeer) *string { return &s.Name },
				Elide: resourcekit.KeepZero,
			},
			resourcekit.StringField[wireguardPeerKitModel, ui.WireGuardPeer]{
				Wire:  "interface_ip",
				Model: func(m *wireguardPeerKitModel) *types.String { return &m.InterfaceIP },
				SDK:   func(s *ui.WireGuardPeer) *string { return &s.InterfaceIP },
				Elide: resourcekit.KeepZero,
			},
			resourcekit.StringField[wireguardPeerKitModel, ui.WireGuardPeer]{
				Wire:  "public_key",
				Model: func(m *wireguardPeerKitModel) *types.String { return &m.PublicKey },
				SDK:   func(s *ui.WireGuardPeer) *string { return &s.PublicKey },
				Elide: resourcekit.KeepZero,
			},
			resourcekit.StringListField[wireguardPeerKitModel, ui.WireGuardPeer]{
				Wire:  "allowed_ips",
				Model: func(m *wireguardPeerKitModel) *types.List { return &m.AllowedIPs },
				SDK:   func(s *ui.WireGuardPeer) *[]string { return &s.AllowedIPs },
				Elide: resourcekit.KeepZero,
			},
		},
		// Seeded here as well as in wireguardPeerKitBackend, so a unit test
		// calling ToModel on an unconfigured spec does not dereference nil.
		Backend: resourcekit.Backend[ui.WireGuardPeer]{
			GetID: func(s *ui.WireGuardPeer) string { return s.ID },
			SetID: func(s *ui.WireGuardPeer, id string) { s.ID = id },
		},
	}
}

func wireguardPeerKitSchema() resourcekit.SchemaSpec {
	return resourcekit.SchemaSpec{
		Resource: resource_wireguard_peer.WireguardPeerResourceSchema,
		Timeouts: timeouts.Opts{Create: true, Read: true, Update: true, Delete: true},
	}
}

// wireguardPeerKitList carries ONLY ConfigSchema, as client's does: the list
// config takes the parent network_id, which the kit's uniform ListConfig
// cannot decode, so this surface supplies its own List method and the kit's
// Filters and DisplayName would be configured and never read.
func wireguardPeerKitList() resourcekit.ListSpec[ui.WireGuardPeer] {
	return resourcekit.ListSpec[ui.WireGuardPeer]{
		ConfigSchema: listresource_wireguard_peer.WireguardPeerListResourceSchema,
	}
}

// wireguardPeerKitBackend binds the peer collection nested under one
// WireGuard server network. Create and UpdateFields read the parent key off
// the object itself; Read, Delete and List need it as an argument, which
// (site, id) closures cannot carry -- the resource's own Read, Delete and
// List resolve it and bind this backend scoped to it before delegating
// (see wireguard_peer_resource.go).
func wireguardPeerKitBackend(
	client *ui.ApiClient,
	networkID string,
) resourcekit.Backend[ui.WireGuardPeer] {
	return resourcekit.Backend[ui.WireGuardPeer]{
		Create: func(ctx context.Context, site string, in *ui.WireGuardPeer) (*ui.WireGuardPeer, error) {
			// The batch endpoint names the network in its URL; the
			// hand-written mapper never put network_id in the body, so clear
			// it and let omitempty drop it there too.
			parent := in.NetworkID
			in.NetworkID = ""
			return client.CreateWireGuardPeer(ctx, site, parent, in)
		},
		Read: func(ctx context.Context, site, id string) (*ui.WireGuardPeer, error) {
			return client.GetWireGuardPeer(ctx, site, networkID, id)
		},
		UpdateFields: func(
			ctx context.Context, site string, in *ui.WireGuardPeer, fields ...string,
		) (*ui.WireGuardPeer, error) {
			return client.UpdateWireGuardPeerFields(ctx, site, in.NetworkID, in, fields...)
		},
		Delete: func(ctx context.Context, site, id string) error {
			return client.DeleteWireGuardPeer(ctx, site, networkID, id)
		},
		List: func(ctx context.Context, site string) ([]ui.WireGuardPeer, error) {
			return client.ListWireGuardPeers(ctx, site, networkID)
		},
		GetID: func(s *ui.WireGuardPeer) string { return s.ID },
		SetID: func(s *ui.WireGuardPeer, id string) { s.ID = id },
	}
}
