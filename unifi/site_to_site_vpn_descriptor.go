package unifi

// The site_to_site_vpn descriptor.
//
// A NETWORK WEARING A PURPOSE, which is what makes it different from the other
// four. The SDK type is Network and the controller distinguishes the seven
// kinds by a purpose discriminator, so New seeds Purpose and VPNType the way
// static_route seeds its Type.
//
// THE MASKED UPDATE DEPENDS ON THAT SEED. go-unifi's Network encodes a
// different field set per purpose, and maskedBody marshals the object BEFORE
// filtering -- so a masked write with no Purpose cannot encode at all, and one
// naming a field the site-vpn encoder drops is refused. Both are loud rather
// than silent, which is why this surface is safe to mask, but neither is
// obvious from the struct.
//
// AND THE OBVIOUS SDK FIELD IS THE WRONG ONE, twice over. ike_encryption maps
// to IPSecEncryption (ipsec_encryption), not IPSecIkeEncryption
// (ipsec_ike_encryption); the same for ike_hash and ike_dh_group. Both pairs
// exist on the struct and only the first is emitted for site-vpn.

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
	// fields, which is what the hand-written optInt64 helper existed to honour.
	return resourcekit.Int64PtrField[s2sModel, ui.Network]{
		Wire: wire, Model: model, SDK: sdk,
		// KeepZero on the READ and OmitZero on the WRITE, which is not a
		// contradiction: we never send a zero, but a zero the controller
		// reports is recorded faithfully -- which is what
		// types.Int64PointerValue did in the mapper this replaces.
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
			// A MEASURED go-unifi DEFECT FORCES THE PREDICATE ON THESE TWO,
			// and it is not this cutover's doing.
			//
			// Network tags ipsec_pfs and ipsec_dynamic_routing WITHOUT
			// omitempty, but marshalSiteVPN's alias struct adds it. Measured:
			// a site-vpn network with pfs=true emits ipsec_pfs and one with
			// pfs=false does not. So these two settings can be turned on and
			// never off, through this provider or any other caller -- the
			// controller simply never receives the false.
			//
			// That is already true on the hand-written resource, which sends
			// the whole object. What the field mask changes is the FAILURE
			// MODE: a mask naming a field the encoder dropped is refused by
			// go-unifi, so without this predicate `pfs = false` would turn a
			// silent no-op into a failed apply. Louder, but a regression for
			// a configuration that plans clean today.
			//
			// So the write is suppressed exactly where the encoder would drop
			// it, preserving current behaviour rather than shipping an error.
			// REMOVE BOTH PREDICATES once go-unifi's alias tags match the
			// struct's; the test below will start failing when it is fixed,
			// which is the reminder.
			resourcekit.BoolField[s2sModel, ui.Network]{
				Wire:      "ipsec_pfs",
				Model:     func(m *s2sModel) *types.Bool { return &m.PFS },
				SDK:       func(s *ui.Network) *bool { return &s.IPSecPfs },
				WriteWhen: func(m *s2sModel) bool { return m.PFS.ValueBool() },
			},
			resourcekit.BoolField[s2sModel, ui.Network]{
				Wire:      "ipsec_dynamic_routing",
				Model:     func(m *s2sModel) *types.Bool { return &m.DynamicRouting },
				SDK:       func(s *ui.Network) *bool { return &s.IPSecDynamicRouting },
				WriteWhen: func(m *s2sModel) bool { return m.DynamicRouting.ValueBool() },
			},
			s2sInt("route_distance", func(m *s2sModel) *types.Int64 { return &m.RouteDistance },
				func(s *ui.Network) **int64 { return &s.RouteDistance }),
		},
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
		// DELETE CARRIES THE NAME IN ITS BODY, which the kit's Delete
		// signature does not pass, so the object is read for it.
		//
		// The extra request is deliberate. Whether the controller actually
		// requires that name is not knowable here -- it is a field in the
		// DELETE body, not the path -- and dropping it to save a round trip
		// would be changing a request shape on a guess, on the one operation
		// with no way back.
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
		// LISTS EVERY NETWORK AND NARROWS TO THIS PURPOSE. The controller has
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

// siteToSiteVPNPreSharedKey sets the IPsec secret from whichever attribute the
// practitioner used.
//
// NEITHER IS A FIELD, and that is not an oversight. pre_shared_key_wo is a
// write-only argument: it never enters state, so it has no model value to read
// on the way back. pre_shared_key is stored, but the read mapper deliberately
// does not repopulate it -- the controller echoes the secret, and writing it
// back would give write-only users a perpetual diff. A Field's ToModel always
// writes, so neither can be one.
//
// THE WRITE-ONLY VALUE WINS when both are set, matching the hand-written
// resource: a practitioner who supplies the ephemeral form means it.
func siteToSiteVPNPreSharedKey(
	_ context.Context,
	config, effective *s2sModel,
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
