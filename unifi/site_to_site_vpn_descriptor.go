package unifi

import (
	"context"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-nettypes/iptypes"
	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework-timetypes/timetypes"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/listresource_site_to_site_vpn"
	resource_site_to_site_vpn "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_site_to_site_vpn"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/resourcekit"
)

type siteToSiteVPNKitModel struct {
	ID             types.String         `tfsdk:"id"`
	Site           types.String         `tfsdk:"site"`
	Name           types.String         `tfsdk:"name"`
	Enabled        types.Bool           `tfsdk:"enabled"`
	Interface      types.String         `tfsdk:"interface"`
	PeerIP         iptypes.IPv4Address  `tfsdk:"peer_ip"`
	LocalIP        iptypes.IPv4Address  `tfsdk:"local_ip"`
	KeyExchange    types.String         `tfsdk:"key_exchange"`
	PreSharedKey   types.String         `tfsdk:"pre_shared_key"`
	PreSharedKeyWO types.String         `tfsdk:"pre_shared_key_wo"`
	RemoteSubnets  types.List           `tfsdk:"remote_subnets"`
	Profile        types.String         `tfsdk:"profile"`
	IKEEncryption  types.String         `tfsdk:"ike_encryption"`
	IKEHash        types.String         `tfsdk:"ike_hash"`
	IKEDhGroup     types.Int64          `tfsdk:"ike_dh_group"`
	IKELifetime    timetypes.GoDuration `tfsdk:"ike_lifetime"`
	ESPEncryption  types.String         `tfsdk:"esp_encryption"`
	ESPHash        types.String         `tfsdk:"esp_hash"`
	ESPDhGroup     types.Int64          `tfsdk:"esp_dh_group"`
	ESPLifetime    timetypes.GoDuration `tfsdk:"esp_lifetime"`
	PFS            types.Bool           `tfsdk:"pfs"`
	DynamicRouting types.Bool           `tfsdk:"dynamic_routing"`
	RouteDistance  types.Int64          `tfsdk:"route_distance"`
	Timeouts       timeouts.Value       `tfsdk:"timeouts"`
}

type s2sModel = siteToSiteVPNKitModel

func s2sPtr(
	wire string,
	model func(*s2sModel) *types.String,
	sdk func(*ui.Network) **string,
) resourcekit.StringLikePtrField[s2sModel, ui.Network, types.String] {
	return resourcekit.StringLikePtrField[s2sModel, ui.Network, types.String]{
		Wire: wire, Model: model, SDK: sdk,
		New: func(v basetypes.StringValue) types.String { return v },
	}
}

func s2sInt(
	wire string,
	model func(*s2sModel) *types.Int64,
	sdk func(*ui.Network) **int64,
) resourcekit.Int64PtrField[s2sModel, ui.Network] {
	// OmitZero: the controller rejects 0 for the DH-group and route-distance
	// fields.
	return resourcekit.Int64PtrField[s2sModel, ui.Network]{
		Wire: wire, Model: model, SDK: sdk,
		// KeepZero on the read and OmitZero on the write is not a
		// contradiction: we never send a zero, but one the controller reports
		// is recorded faithfully.
		Elide: resourcekit.KeepZero, OmitZero: true,
	}
}

func siteToSiteVPNKitSpec() resourcekit.Spec[s2sModel, ui.Network] {
	return resourcekit.Spec[s2sModel, ui.Network]{
		TypeName: "site_to_site_vpn",
		Subject:  "Site-to-Site VPN",
		New: func() *ui.Network {
			vpnType := "ipsec-vpn"
			return &ui.Network{Purpose: ui.PurposeSiteVPN, VPNType: &vpnType}
		},
		ID:       func(m *s2sModel) *types.String { return &m.ID },
		Site:     func(m *s2sModel) *types.String { return &m.Site },
		Timeouts: func(m *s2sModel) *timeouts.Value { return &m.Timeouts },

		BeforeSend: siteToSiteVPNPreSharedKey,

		// The pre-shared key is set by BeforeSend from whichever of the two
		// attributes the practitioner used, and neither is a Field, so nothing
		// in the plan can put it in the mask.
		AlwaysWire: []string{"x_ipsec_pre_shared_key"},

		Fields: []resourcekit.Field[s2sModel, ui.Network]{
			s2sPtr("name", func(m *s2sModel) *types.String { return &m.Name },
				func(s *ui.Network) **string { return &s.Name }),
			resourcekit.BoolField[s2sModel, ui.Network]{
				Wire:  "enabled",
				Model: func(m *s2sModel) *types.Bool { return &m.Enabled },
				SDK:   func(s *ui.Network) *bool { return &s.Enabled },
			},
			s2sPtr("ipsec_interface", func(m *s2sModel) *types.String { return &m.Interface },
				func(s *ui.Network) **string { return &s.IPSecInterface }),
			resourcekit.StringLikePtrField[s2sModel, ui.Network, iptypes.IPv4Address]{
				Wire:  "ipsec_peer_ip",
				Model: func(m *s2sModel) *iptypes.IPv4Address { return &m.PeerIP },
				SDK:   func(s *ui.Network) **string { return &s.IPSecPeerIP },
				New: func(v basetypes.StringValue) iptypes.IPv4Address {
					return iptypes.IPv4Address{StringValue: v}
				},
			},
			resourcekit.StringLikePtrField[s2sModel, ui.Network, iptypes.IPv4Address]{
				Wire:  "ipsec_local_ip",
				Model: func(m *s2sModel) *iptypes.IPv4Address { return &m.LocalIP },
				SDK:   func(s *ui.Network) **string { return &s.IPSecLocalIP },
				New: func(v basetypes.StringValue) iptypes.IPv4Address {
					return iptypes.IPv4Address{StringValue: v}
				},
			},
			s2sPtr("ipsec_key_exchange", func(m *s2sModel) *types.String { return &m.KeyExchange },
				func(s *ui.Network) **string { return &s.IPSecKeyExchange }),
			resourcekit.StringListField[s2sModel, ui.Network]{
				Wire:  "remote_vpn_subnets",
				Model: func(m *s2sModel) *types.List { return &m.RemoteSubnets },
				SDK:   func(s *ui.Network) *[]string { return &s.RemoteVPNSubnets },
				Elide: resourcekit.KeepZero,
			},
			s2sPtr("ipsec_profile", func(m *s2sModel) *types.String { return &m.Profile },
				func(s *ui.Network) **string { return &s.IPSecProfile }),
			// NOT IPSecIkeEncryption. Both exist; only this one is emitted for
			// a site-vpn, and a mask naming the other is refused by the SDK.
			s2sPtr("ipsec_encryption", func(m *s2sModel) *types.String { return &m.IKEEncryption },
				func(s *ui.Network) **string { return &s.IPSecEncryption }),
			s2sPtr("ipsec_hash", func(m *s2sModel) *types.String { return &m.IKEHash },
				func(s *ui.Network) **string { return &s.IPSecHash }),
			s2sInt("ipsec_dh_group", func(m *s2sModel) *types.Int64 { return &m.IKEDhGroup },
				func(s *ui.Network) **int64 { return &s.IPSecDhGroup }),
			resourcekit.DurationPtrField[s2sModel, ui.Network]{
				Wire:  "ipsec_ike_lifetime",
				Model: func(m *s2sModel) *timetypes.GoDuration { return &m.IKELifetime },
				SDK:   func(s *ui.Network) **int64 { return &s.IPSecIkeLifetime },
				Units: time.Second,
				// A pointer to zero reads back as "0s", which is what
				// util.DurationPtrValue produced.
				Elide: resourcekit.KeepZero,
			},
			s2sPtr(
				"ipsec_esp_encryption",
				func(m *s2sModel) *types.String { return &m.ESPEncryption },
				func(s *ui.Network) **string { return &s.IPSecEspEncryption },
			),
			s2sPtr("ipsec_esp_hash", func(m *s2sModel) *types.String { return &m.ESPHash },
				func(s *ui.Network) **string { return &s.IPSecEspHash }),
			s2sInt("ipsec_esp_dh_group", func(m *s2sModel) *types.Int64 { return &m.ESPDhGroup },
				func(s *ui.Network) **int64 { return &s.IPSecEspDhGroup }),
			resourcekit.DurationPtrField[s2sModel, ui.Network]{
				Wire:  "ipsec_esp_lifetime",
				Model: func(m *s2sModel) *timetypes.GoDuration { return &m.ESPLifetime },
				SDK:   func(s *ui.Network) **int64 { return &s.IPSecEspLifetime },
				Units: time.Second,
				// A pointer to zero reads back as "0s", which is what
				// util.DurationPtrValue produced.
				Elide: resourcekit.KeepZero,
			},
			// ipsec_pfs and ipsec_dynamic_routing are emitted unconditionally by
			// the encoder (go-unifi v1.105.0); no suppression predicate needed
			// here. See TestPFSAndDynamicRoutingAreEmittedUnconditionally.
			resourcekit.BoolField[s2sModel, ui.Network]{
				Wire:  "ipsec_pfs",
				Model: func(m *s2sModel) *types.Bool { return &m.PFS },
				SDK:   func(s *ui.Network) *bool { return &s.IPSecPfs },
			},
			resourcekit.BoolField[s2sModel, ui.Network]{
				Wire:  "ipsec_dynamic_routing",
				Model: func(m *s2sModel) *types.Bool { return &m.DynamicRouting },
				SDK:   func(s *ui.Network) *bool { return &s.IPSecDynamicRouting },
			},
			s2sInt("route_distance", func(m *s2sModel) *types.Int64 { return &m.RouteDistance },
				func(s *ui.Network) **int64 { return &s.RouteDistance }),
		},
		// Seeded here as well as in siteToSiteVPNKitBackend, because Configure binds
		// the real Backend and a unit test calling ToModel on an unconfigured
		// spec would otherwise dereference nil.
		Backend: resourcekit.Backend[ui.Network]{
			GetID: func(s *ui.Network) string { return s.ID },
			SetID: func(s *ui.Network, id string) { s.ID = id },
		},
	}
}

func siteToSiteVPNKitSchema() resourcekit.SchemaSpec {
	return resourcekit.SchemaSpec{
		Resource: resource_site_to_site_vpn.SiteToSiteVpnResourceSchema,
		Timeouts: timeouts.Opts{Create: true, Read: true, Update: true, Delete: true},
		Version:  1,
	}
}

func siteToSiteVPNKitList() resourcekit.ListSpec[ui.Network] {
	return resourcekit.ListSpec[ui.Network]{
		ConfigSchema: listresource_site_to_site_vpn.SiteToSiteVpnListResourceSchema,
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

func siteToSiteVPNKitBackend(client *ui.ApiClient) resourcekit.Backend[ui.Network] {
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
		// Delete's body carries the name, which the kit's Delete signature
		// doesn't pass, so this reads the object for it. Whether the
		// controller truly requires the name isn't verifiable from code, so
		// it isn't dropped to save a round trip on the one operation with no
		// way back.
		Delete: func(ctx context.Context, site, id string) error {
			existing, err := client.GetNetwork(ctx, site, id)
			if err != nil {
				return err
			}
			var name string
			if existing.Name != nil {
				name = *existing.Name
			}
			return client.DeleteNetwork(ctx, site, id, name)
		},
		// Lists every network and narrows to this purpose: the controller has
		// one networkconf endpoint for all seven kinds, so without the filter
		// a site-to-site VPN list would return corporate LANs as well.
		List: func(ctx context.Context, site string) ([]ui.Network, error) {
			all, err := client.ListNetwork(ctx, site)
			if err != nil {
				return nil, err
			}
			out := make([]ui.Network, 0, len(all))
			for _, n := range all {
				if n.Purpose == ui.PurposeSiteVPN {
					out = append(out, n)
				}
			}
			return out, nil
		},
		GetID: func(s *ui.Network) string { return s.ID },
		SetID: func(s *ui.Network, id string) { s.ID = id },
	}
}

// siteToSiteVPNPreSharedKey sets the IPsec secret from whichever attribute
// the practitioner used. Neither is a Field: pre_shared_key_wo is write-only
// and never enters state, and pre_shared_key's read deliberately doesn't
// repopulate from the controller's echo (that would give write-only users a
// perpetual diff) -- a Field's ToModel always writes, so neither can be one.
// The write-only value wins when both are set.
func siteToSiteVPNPreSharedKey(
	_ context.Context,
	config, effective *s2sModel,
	_ s2sModel,
	sdk *ui.Network,
	_ any,
) diag.Diagnostics {
	// The write-only attribute is only ever in the CONFIG -- that is what
	// write-only means -- so this one reads config where the stored variant
	// reads the effective model.
	if wo := config.PreSharedKeyWO; !wo.IsNull() && !wo.IsUnknown() && wo.ValueString() != "" {
		key := wo.ValueString()
		sdk.IPSecPreSharedKey = &key
		return nil
	}
	if psk := effective.PreSharedKey; !psk.IsNull() && !psk.IsUnknown() && psk.ValueString() != "" {
		key := psk.ValueString()
		sdk.IPSecPreSharedKey = &key
	}
	return nil
}
