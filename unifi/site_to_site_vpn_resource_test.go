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
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
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
// conversion. A unit test because the dockerized acceptance controller has
// no WAN/peer to establish an IPsec tunnel.
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

// TestEveryWireNameIsEmittedBySiteVPNEncoding: go-unifi's Network encodes a
// different field set per purpose, and both ipsec_encryption and
// ipsec_ike_encryption are real json tags -- so a Wire pointing at the wrong
// one passes the wire-name check and only fails here, where the encoder is
// asked directly. maskedBody refuses a mask naming a field the encoder drops.
func TestEveryWireNameIsEmittedBySiteVPNEncoding(t *testing.T) {
	// Fully populated because nearly every field is tagged omitempty; an
	// empty network emits almost nothing.
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

	// The mask, not every field: what must be emitted is what a write would
	// actually name.
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

// Since go-unifi v1.105.0 the encoder emits ipsec_pfs and
// ipsec_dynamic_routing unconditionally, so the mask must name them
// unconditionally too -- omitting them when false silently drops the value,
// making the setting one that can be turned on but never off.
func TestPFSAndDynamicRoutingAreEmittedUnconditionally(t *testing.T) {
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
			if !inMask {
				t.Errorf("with the value %v, %q is not in the mask; "+
					"the encoder emits it unconditionally now, so the mask must name it "+
					"unconditionally too or a false is silently dropped by this provider", on, name)
			}
			if !isEmitted {
				t.Errorf("with the value %v, %q was not emitted by the encoder; "+
					"go-unifi v1.105.0 was supposed to have fixed that", on, name)
			}
			if inMask != isEmitted {
				t.Errorf("with the value %v, %q is in the mask=%v but emitted=%v; "+
					"the mask and the encoder must agree or the write is refused",
					on, name, inMask, isEmitted)
			}
		}
	}
}

func Test_siteToSiteVPNKitResource_ConfigValidators(t *testing.T) {
	r := newSiteToSiteVPNKitResource()
	validators := r.ConfigValidators(context.Background())
	if len(validators) == 0 {
		t.Error("expected at least one config validator")
	}
}

func Test_siteToSiteVPNRemoteSubnetsConfigValidator_Description(t *testing.T) {
	v := &siteToSiteVPNRemoteSubnetsConfigValidator{}
	want := "remote_subnets must hold at least one subnet unless dynamic_routing is true"
	if got := v.Description(context.Background()); got != want {
		t.Errorf("Description() = %q, want %q", got, want)
	}
}

func Test_siteToSiteVPNRemoteSubnetsConfigValidator_MarkdownDescription(t *testing.T) {
	v := &siteToSiteVPNRemoteSubnetsConfigValidator{}
	want := "remote_subnets must hold at least one subnet unless dynamic_routing is true"
	if got := v.MarkdownDescription(context.Background()); got != want {
		t.Errorf("MarkdownDescription() = %q, want %q", got, want)
	}
}

// Test_siteToSiteVPNRemoteSubnetsConfigValidator_ValidateResource builds a
// real schema-backed config, the same shape
// Test_staticRouteIPVersionValidator_ValidateResource uses, and exercises
// both sides of upstream's #433 fix: empty remote_subnets is rejected with
// dynamic_routing off, and allowed once it's on.
func Test_siteToSiteVPNRemoteSubnetsConfigValidator_ValidateResource(t *testing.T) {
	ctx := context.Background()
	schemaResp := &fwresource.SchemaResponse{}
	newSiteToSiteVPNKitResource().Schema(ctx, fwresource.SchemaRequest{}, schemaResp)
	if schemaResp.Diagnostics.HasError() {
		t.Fatalf("build the schema: %v", schemaResp.Diagnostics)
	}

	configFor := func(t *testing.T, dynamicRouting types.Bool, subnets []string) tfsdk.Config {
		t.Helper()
		remoteSubnets := types.ListNull(types.StringType)
		if subnets != nil {
			var diags diag.Diagnostics
			remoteSubnets, diags = types.ListValueFrom(ctx, types.StringType, subnets)
			if diags.HasError() {
				t.Fatalf("building remote_subnets: %v", diags)
			}
		}
		model := siteToSiteVPNKitModel{
			ID:             types.StringNull(),
			Site:           types.StringNull(),
			Name:           types.StringValue("HQ-to-Branch"),
			Enabled:        types.BoolValue(true),
			Interface:      types.StringNull(),
			PeerIP:         iptypes.NewIPv4AddressValue("203.0.113.9"),
			LocalIP:        iptypes.NewIPv4AddressNull(),
			KeyExchange:    types.StringNull(),
			PreSharedKey:   types.StringNull(),
			PreSharedKeyWO: types.StringNull(),
			RemoteSubnets:  remoteSubnets,
			Profile:        types.StringNull(),
			IKEEncryption:  types.StringNull(),
			IKEHash:        types.StringNull(),
			IKEDhGroup:     types.Int64Null(),
			IKELifetime:    timetypes.NewGoDurationNull(),
			ESPEncryption:  types.StringNull(),
			ESPHash:        types.StringNull(),
			ESPDhGroup:     types.Int64Null(),
			ESPLifetime:    timetypes.NewGoDurationNull(),
			PFS:            types.BoolNull(),
			DynamicRouting: dynamicRouting,
			RouteDistance:  types.Int64Null(),
			Timeouts:       timeoutsNullValue(),
		}
		staging := tfsdk.State{Schema: schemaResp.Schema}
		if diags := staging.Set(ctx, model); diags.HasError() {
			t.Fatalf("set the config: %v", diags)
		}
		return tfsdk.Config{Schema: schemaResp.Schema, Raw: staging.Raw}
	}

	tests := []struct {
		name           string
		dynamicRouting types.Bool
		subnets        []string
		wantError      bool
	}{
		{"static_with_subnets", types.BoolValue(false), []string{"192.0.2.0/24"}, false},
		{"static_with_no_subnets", types.BoolValue(false), []string{}, true},
		{"static_unset_with_no_subnets", types.BoolNull(), []string{}, true},
		{"dynamic_with_no_subnets", types.BoolValue(true), []string{}, false},
		{"dynamic_with_subnets", types.BoolValue(true), []string{"192.0.2.0/24"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := &siteToSiteVPNRemoteSubnetsConfigValidator{}
			resp := &fwresource.ValidateConfigResponse{}
			v.ValidateResource(ctx, fwresource.ValidateConfigRequest{
				Config: configFor(t, tt.dynamicRouting, tt.subnets),
			}, resp)
			if got := resp.Diagnostics.HasError(); got != tt.wantError {
				t.Errorf("dynamic_routing=%v subnets=%v: got error=%v, want %v (diags: %v)",
					tt.dynamicRouting, tt.subnets, got, tt.wantError, resp.Diagnostics)
			}
		})
	}
}
