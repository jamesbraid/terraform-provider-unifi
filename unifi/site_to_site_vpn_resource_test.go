package unifi

import (
	"context"
	"encoding/json"
	"slices"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework-nettypes/iptypes"
	"github.com/hashicorp/terraform-plugin-framework-timetypes/timetypes"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	fwlist "github.com/hashicorp/terraform-plugin-framework/list"
	fwresource "github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/querycheck"
	"github.com/hashicorp/terraform-plugin-testing/tfversion"
	"github.com/ubiquiti-community/go-unifi/unifi"
)

func TestAccSiteToSiteVPNList_empty(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		TerraformVersionChecks: []tfversion.TerraformVersionCheck{
			tfversion.SkipBelow(tfversion.Version1_14_0),
		},
		Steps: []resource.TestStep{{
			Query: true,
			Config: `
provider "unifi" {}
list "unifi_site_to_site_vpn" "test" {
  provider = unifi
  config {}
}
`,
			QueryResultChecks: []querycheck.QueryResultCheck{
				querycheck.ExpectLength("unifi_site_to_site_vpn.test", 0),
			},
		}},
	})
}

// TestSiteToSiteVPNModelRoundTrip validates the model <-> go-unifi Network
// conversion for the unifi_site_to_site_vpn resource (#78). It is a unit test
// rather than an acceptance test because the dockerized acceptance controller
// has no WAN/peer to establish an IPsec tunnel; the live round-trip is exercised
// against a real controller during development.
func TestSiteToSiteVPNModelRoundTrip(t *testing.T) {
	ctx := context.Background()

	subnets, d := types.ListValueFrom(
		ctx,
		types.StringType,
		[]string{"192.0.2.0/24", "198.51.100.0/24"},
	)
	if d.HasError() {
		t.Fatalf("building remote_subnets: %v", d)
	}
	model := &siteToSiteVPNKitModel{
		Name:          types.StringValue("HQ-to-Branch"),
		Enabled:       types.BoolValue(true),
		Interface:     types.StringValue("wan"),
		PeerIP:        iptypes.NewIPv4AddressValue("203.0.113.9"),
		KeyExchange:   types.StringValue("ikev2"),
		PreSharedKey:  types.StringValue("s3cret-psk"),
		RemoteSubnets: subnets,
		Profile:       types.StringValue("customized"),
		IKEEncryption: types.StringValue("aes256"),
		IKEDhGroup:    types.Int64Value(14),
		PFS:           types.BoolValue(true),
	}

	network, diags := siteToSiteVPNToSDKWithHooks(ctx, model)
	if diags.HasError() {
		t.Fatalf("modelToNetwork: %v", diags)
	}
	if network.Purpose != unifi.PurposeSiteVPN {
		t.Errorf("Purpose = %q, want %q", network.Purpose, unifi.PurposeSiteVPN)
	}
	if network.VPNType == nil || *network.VPNType != "ipsec-vpn" {
		t.Errorf("VPNType = %v, want ipsec-vpn", network.VPNType)
	}
	if network.IPSecPeerIP == nil || *network.IPSecPeerIP != "203.0.113.9" {
		t.Errorf("IPSecPeerIP = %v", network.IPSecPeerIP)
	}
	if network.IPSecPreSharedKey == nil || *network.IPSecPreSharedKey != "s3cret-psk" {
		t.Errorf("IPSecPreSharedKey not set")
	}
	if network.IPSecDhGroup == nil || *network.IPSecDhGroup != 14 {
		t.Errorf("IPSecDhGroup = %v, want 14", network.IPSecDhGroup)
	}
	if !network.IPSecPfs {
		t.Error("IPSecPfs = false, want true")
	}
	if len(network.RemoteVPNSubnets) != 2 {
		t.Errorf("RemoteVPNSubnets = %v, want 2 entries", network.RemoteVPNSubnets)
	}

	// API -> model: secret is preserved (not re-read), other fields map back.
	apiNetwork := &unifi.Network{
		ID:                "net-1",
		Name:              unifi.Ptr("HQ-to-Branch"),
		Purpose:           unifi.PurposeSiteVPN,
		Enabled:           true,
		VPNType:           unifi.Ptr("ipsec-vpn"),
		IPSecInterface:    unifi.Ptr("wan"),
		IPSecPeerIP:       unifi.Ptr("203.0.113.9"),
		IPSecKeyExchange:  unifi.Ptr("ikev2"),
		IPSecPreSharedKey: unifi.Ptr("echoed-by-controller"),
		IPSecPfs:          true,
		RemoteVPNSubnets:  []string{"192.0.2.0/24", "198.51.100.0/24"},
	}
	out := &siteToSiteVPNKitModel{
		PreSharedKey: types.StringValue("s3cret-psk"), // prior state value
	}
	if diags := siteToSiteVPNKitSpec().ToModel(ctx, apiNetwork, out, "default"); diags.HasError() {
		t.Fatalf("networkToModel: %v", diags)
	}
	if out.ID.ValueString() != "net-1" {
		t.Errorf("ID = %q, want net-1", out.ID.ValueString())
	}
	if out.PeerIP.ValueString() != "203.0.113.9" {
		t.Errorf("PeerIP = %q", out.PeerIP.ValueString())
	}
	// The controller echoes the PSK on read, but networkToModel must preserve the
	// configured/state value to avoid perpetual diffs.
	if out.PreSharedKey.ValueString() != "s3cret-psk" {
		t.Errorf(
			"PreSharedKey = %q, want preserved s3cret-psk (not the API echo)",
			out.PreSharedKey.ValueString(),
		)
	}
	if l := len(out.RemoteSubnets.Elements()); l != 2 {
		t.Errorf("RemoteSubnets length = %d, want 2", l)
	}
}

func TestNewSiteToSiteVPNResource(t *testing.T) {
	r := NewSiteToSiteVPNResource()
	if r == nil {
		t.Fatal("NewSiteToSiteVPNResource() returned nil")
	}
	if _, ok := r.(fwresource.ResourceWithImportState); !ok {
		t.Error("expected ResourceWithImportState interface")
	}
}

func TestNewSiteToSiteVPNListResource(t *testing.T) {
	r := NewSiteToSiteVPNListResource()
	if r == nil {
		t.Fatal("NewSiteToSiteVPNListResource() returned nil")
	}
	if _, ok := r.(fwlist.ListResourceWithConfigure); !ok {
		t.Error("expected ListResourceWithConfigure interface")
	}
}

func Test_siteToSiteVPNResource_IdentitySchema(t *testing.T) {
	r := newSiteToSiteVPNKitResource()
	resp := &fwresource.IdentitySchemaResponse{}
	r.IdentitySchema(context.Background(), fwresource.IdentitySchemaRequest{}, resp)
	if resp.Diagnostics.HasError() {
		t.Errorf("IdentitySchema() produced errors: %v", resp.Diagnostics)
	}
	if _, ok := resp.IdentitySchema.Attributes["id"]; !ok {
		t.Error("IdentitySchema missing 'id' attribute")
	}
}

func Test_siteToSiteVPNResource_UpgradeState(t *testing.T) {
	r := newSiteToSiteVPNKitResource()
	upgraders := r.UpgradeState(context.Background())
	if _, ok := upgraders[0]; !ok {
		t.Error("expected state upgrader for version 0")
	}
}

func Test_siteToSiteVPNResource_siteOrDefault(t *testing.T) {
	t.Run("non-empty site is returned as-is", func(t *testing.T) {
		r := newSiteToSiteVPNKitResource()
		r.DefaultSite = "fallback"
		got := r.Site(&siteToSiteVPNKitModel{Site: types.StringValue("custom")})
		if got != "custom" {
			t.Errorf("got %q, want %q", got, "custom")
		}
	})
	t.Run("empty site falls back to client site", func(t *testing.T) {
		r := newSiteToSiteVPNKitResource()
		r.DefaultSite = "default"
		got := r.Site(&siteToSiteVPNKitModel{Site: types.StringValue("")})
		if got != "default" {
			t.Errorf("got %q, want %q", got, "default")
		}
	})
}

func Test_siteToSiteVPNResource_modelToNetwork(t *testing.T) {
	ctx := context.Background()

	t.Run("basic fields are set", func(t *testing.T) {
		subnets, d := types.ListValueFrom(ctx, types.StringType, []string{"10.0.0.0/24"})
		if d.HasError() {
			t.Fatalf("building subnets: %v", d)
		}
		model := &siteToSiteVPNKitModel{
			Name:          types.StringValue("test-vpn"),
			Enabled:       types.BoolValue(true),
			Interface:     types.StringValue("wan"),
			PeerIP:        iptypes.NewIPv4AddressValue("1.2.3.4"),
			KeyExchange:   types.StringValue("ikev2"),
			PreSharedKey:  types.StringValue("psk"),
			RemoteSubnets: subnets,
			PFS:           types.BoolValue(true),
		}
		network, diags := siteToSiteVPNToSDKWithHooks(ctx, model)
		if diags.HasError() {
			t.Fatalf("unexpected diags: %v", diags)
		}
		if network.Purpose != unifi.PurposeSiteVPN {
			t.Errorf("Purpose = %q, want site-vpn", network.Purpose)
		}
		if network.VPNType == nil || *network.VPNType != "ipsec-vpn" {
			t.Errorf("VPNType = %v, want ipsec-vpn", network.VPNType)
		}
		if !network.Enabled {
			t.Error("Enabled should be true")
		}
		if !network.IPSecPfs {
			t.Error("IPSecPfs should be true")
		}
		if len(network.RemoteVPNSubnets) != 1 {
			t.Errorf("RemoteVPNSubnets length = %d, want 1", len(network.RemoteVPNSubnets))
		}
	})

	t.Run("null optional fields produce nil pointers", func(t *testing.T) {
		subnets, _ := types.ListValueFrom(ctx, types.StringType, []string{"10.0.0.0/24"})
		model := &siteToSiteVPNKitModel{
			Name:          types.StringValue("vpn"),
			Interface:     types.StringNull(),
			PeerIP:        iptypes.NewIPv4AddressNull(),
			IKEEncryption: types.StringNull(),
			RemoteSubnets: subnets,
		}
		network, diags := siteToSiteVPNToSDKWithHooks(ctx, model)
		if diags.HasError() {
			t.Fatalf("unexpected diags: %v", diags)
		}
		if network.IPSecInterface != nil {
			t.Errorf("IPSecInterface should be nil for null input, got %v", network.IPSecInterface)
		}
		if network.IPSecEncryption != nil {
			t.Errorf(
				"IPSecEncryption should be nil for null input, got %v",
				network.IPSecEncryption,
			)
		}
	})
}

func Test_siteToSiteVPNResource_networkToModel(t *testing.T) {
	ctx := context.Background()

	t.Run("basic fields are populated", func(t *testing.T) {
		name := "my-vpn"
		iface := "wan"
		network := &unifi.Network{
			ID:             "net-42",
			Name:           &name,
			Purpose:        unifi.PurposeSiteVPN,
			Enabled:        true,
			IPSecInterface: &iface,
			RemoteVPNSubnets: []string{
				"192.168.10.0/24",
			},
		}
		model := &siteToSiteVPNKitModel{}
		diags := siteToSiteVPNKitSpec().ToModel(ctx, network, model, "site1")
		if diags.HasError() {
			t.Fatalf("unexpected diags: %v", diags)
		}
		if model.ID.ValueString() != "net-42" {
			t.Errorf("ID = %q, want net-42", model.ID.ValueString())
		}
		if model.Site.ValueString() != "site1" {
			t.Errorf("Site = %q, want site1", model.Site.ValueString())
		}
		if model.Name.ValueString() != "my-vpn" {
			t.Errorf("Name = %q, want my-vpn", model.Name.ValueString())
		}
		if !model.Enabled.ValueBool() {
			t.Error("Enabled should be true")
		}
		if l := len(model.RemoteSubnets.Elements()); l != 1 {
			t.Errorf("RemoteSubnets length = %d, want 1", l)
		}
	})

	t.Run("nil pointer fields produce null values", func(t *testing.T) {
		network := &unifi.Network{
			ID:              "net-99",
			IPSecInterface:  nil,
			IPSecEncryption: nil,
		}
		model := &siteToSiteVPNKitModel{}
		diags := siteToSiteVPNKitSpec().ToModel(ctx, network, model, "default")
		if diags.HasError() {
			t.Fatalf("unexpected diags: %v", diags)
		}
		if !model.Interface.IsNull() {
			t.Errorf(
				"Interface should be null for nil pointer, got %q",
				model.Interface.ValueString(),
			)
		}
		if !model.IKEEncryption.IsNull() {
			t.Errorf(
				"IKEEncryption should be null for nil pointer, got %q",
				model.IKEEncryption.ValueString(),
			)
		}
	})
}

func Test_optStr(t *testing.T) {
	t.Run("null returns nil", func(t *testing.T) {
		if got := optStr(types.StringNull()); got != nil {
			t.Errorf("optStr(null) = %v, want nil", got)
		}
	})
	t.Run("unknown returns nil", func(t *testing.T) {
		if got := optStr(types.StringUnknown()); got != nil {
			t.Errorf("optStr(unknown) = %v, want nil", got)
		}
	})
	t.Run("empty string returns nil", func(t *testing.T) {
		if got := optStr(types.StringValue("")); got != nil {
			t.Errorf("optStr(\"\") = %v, want nil", got)
		}
	})
	t.Run("non-empty string returns pointer", func(t *testing.T) {
		got := optStr(types.StringValue("ikev2"))
		if got == nil {
			t.Fatal("optStr(\"ikev2\") returned nil")
		}
		if *got != "ikev2" {
			t.Errorf("*got = %q, want ikev2", *got)
		}
	})
}

func Test_siteToSiteVPNResource_ListResourceConfigSchema(t *testing.T) {
	r := newSiteToSiteVPNKitResource()
	resp := &fwlist.ListResourceSchemaResponse{}
	r.ListResourceConfigSchema(context.Background(), fwlist.ListResourceSchemaRequest{}, resp)
	if resp.Diagnostics.HasError() {
		t.Errorf("ListResourceConfigSchema() produced errors: %v", resp.Diagnostics)
	}
	if _, ok := resp.Schema.Attributes["site"]; !ok {
		t.Error("ListResourceConfigSchema missing 'site' attribute")
	}
}

// siteToSiteVPNToSDKWithHooks is what modelToNetwork became: the field mapping
// plus BeforeSend, which is where the pre-shared key is set. Calling only
// Spec.ToSDK would test an object the provider never sends.
func siteToSiteVPNToSDKWithHooks(
	ctx context.Context,
	model *siteToSiteVPNKitModel,
) (*unifi.Network, diag.Diagnostics) {
	sdk, diags := siteToSiteVPNKitSpec().ToSDK(ctx, model)
	if diags.HasError() {
		return sdk, diags
	}
	diags.Append(siteToSiteVPNPreSharedKey(ctx, model, model, sdk, nil)...)
	return sdk, diags
}

// EVERY WIRE NAME MUST BE ONE THE SITE-VPN ENCODER ACTUALLY EMITS, which the
// wire-name check cannot tell you.
//
// go-unifi's Network encodes a different field set per purpose. The wire-name
// check confirms a field's Wire matches the json tag on the struct member it
// reaches -- and both ipsec_encryption and ipsec_ike_encryption are real tags,
// so pointing ike_encryption at the wrong one satisfies it completely. Only
// this test notices: marshalSiteVPN emits the first and not the second, and
// maskedBody refuses a mask naming a field the encoder drops, so the mistake
// surfaces as a failing update against a live controller and nowhere earlier.
//
// Asking the encoder is the whole method. A hand-kept list of the site-vpn
// field set would be a second copy of something go-unifi already knows.
func TestEveryWireNameIsEmittedBySiteVPNEncoding(t *testing.T) {
	// A FULLY POPULATED OBJECT, because nearly every field is tagged omitempty:
	// encoding an empty network emits almost nothing and would report every
	// wire name as missing, which is what the first version of this test did.
	ctx := context.Background()
	subnets, d := types.ListValueFrom(ctx, types.StringType, []string{"10.0.0.0/24"})
	if d.HasError() {
		t.Fatalf("building subnets: %v", d)
	}
	populated := &siteToSiteVPNKitModel{
		Name:           types.StringValue("vpn"),
		Enabled:        types.BoolValue(true),
		Interface:      types.StringValue("wan"),
		PeerIP:         iptypes.NewIPv4AddressValue("192.0.2.1"),
		LocalIP:        iptypes.NewIPv4AddressValue("192.0.2.2"),
		KeyExchange:    types.StringValue("ikev2"),
		PreSharedKey:   types.StringValue("secret"),
		RemoteSubnets:  subnets,
		Profile:        types.StringValue("customized"),
		IKEEncryption:  types.StringValue("aes256"),
		IKEHash:        types.StringValue("sha256"),
		IKEDhGroup:     types.Int64Value(14),
		IKELifetime:    timetypes.NewGoDurationValueFromStringMust("8h"),
		ESPEncryption:  types.StringValue("aes256"),
		ESPHash:        types.StringValue("sha256"),
		ESPDhGroup:     types.Int64Value(14),
		ESPLifetime:    timetypes.NewGoDurationValueFromStringMust("1h"),
		PFS:            types.BoolValue(true),
		DynamicRouting: types.BoolValue(false),
		RouteDistance:  types.Int64Value(30),
	}
	built, diags := siteToSiteVPNToSDKWithHooks(ctx, populated)
	if diags.HasError() {
		t.Fatalf("building the object: %v", diags)
	}
	encoded, err := json.Marshal(built)
	if err != nil {
		t.Fatalf("encoding an empty site-vpn network: %v", err)
	}
	var emitted map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &emitted); err != nil {
		t.Fatalf("reading back the encoded network: %v", err)
	}
	if len(emitted) == 0 {
		t.Fatal(
			"the encoder emitted no fields at all, so every assertion below would pass vacuously",
		)
	}

	// THE MASK, NOT EVERY FIELD. What must be emitted is what a write would
	// actually name, and two fields are deliberately suppressed when false --
	// see the descriptor's note on the go-unifi omitempty defect.
	spec := siteToSiteVPNKitSpec()
	mask, err := spec.WireFields(populated)
	if err != nil {
		t.Fatalf("WireFields: %v", err)
	}
	mask = append(mask, spec.AlwaysWire...)
	if len(mask) < 15 {
		t.Fatalf("the mask names only %d field(s) for a fully populated model, "+
			"so this test is checking almost nothing: %v", len(mask), mask)
	}
	for _, name := range mask {
		if _, ok := emitted[name]; !ok {
			t.Errorf("the descriptor writes %q, which the site-vpn encoder does not emit; "+
				"a masked update naming it is refused by go-unifi", name)
		}
	}
}

// THE TWO SUPPRESSED BOOLEANS, asserted from both sides.
//
// go-unifi's marshalSiteVPN tags ipsec_pfs and ipsec_dynamic_routing omitempty
// while the Network struct does not, so a false is dropped and the settings can
// be turned on but never off. The descriptor keeps them out of the mask when
// false to preserve that behaviour rather than turn it into a failed apply.
//
// WHEN go-unifi IS FIXED THIS TEST FAILS, which is the intended reminder to
// delete both predicates: the false case will start being emitted.
func TestPFSAndDynamicRoutingAreSuppressedExactlyWhereTheEncoderDropsThem(t *testing.T) {
	ctx := context.Background()
	for _, on := range []bool{true, false} {
		model := &siteToSiteVPNKitModel{
			Name:           types.StringValue("vpn"),
			PFS:            types.BoolValue(on),
			DynamicRouting: types.BoolValue(on),
		}
		mask, err := siteToSiteVPNKitSpec().WireFields(model)
		if err != nil {
			t.Fatalf("WireFields: %v", err)
		}
		built, diags := siteToSiteVPNKitSpec().ToSDK(ctx, model)
		if diags.HasError() {
			t.Fatalf("ToSDK: %v", diags)
		}
		encoded, err := json.Marshal(built)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		var emitted map[string]json.RawMessage
		if err := json.Unmarshal(encoded, &emitted); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		for _, name := range []string{"ipsec_pfs", "ipsec_dynamic_routing"} {
			inMask := slices.Contains(mask, name)
			_, isEmitted := emitted[name]
			if inMask != isEmitted {
				t.Errorf("with the value %v, %q is in the mask=%v but emitted=%v; "+
					"the mask and the encoder must agree or the write is refused",
					on, name, inMask, isEmitted)
			}
			if inMask != on {
				t.Errorf("with the value %v, %q in mask=%v; want it masked only when true",
					on, name, inMask)
			}
		}
	}
}
