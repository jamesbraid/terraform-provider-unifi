package unifi

import (
	"context"
	"reflect"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	fwlist "github.com/hashicorp/terraform-plugin-framework/list"
	fwresource "github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/querycheck"
	"github.com/hashicorp/terraform-plugin-testing/tfversion"
	"github.com/ubiquiti-community/go-unifi/unifi"
)

func TestAccWANList_basic(t *testing.T) {
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
list "unifi_wan" "test" {
  provider = unifi
  config {}
}
`,
			QueryResultChecks: []querycheck.QueryResultCheck{
				querycheck.ExpectLengthAtLeast("unifi_wan.test", 1),
			},
		}},
	})
}

func TestAccWANFramework_basic(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccWANFrameworkConfig_basic(),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet("unifi_wan.test", "id"),
					resource.TestCheckResourceAttr("unifi_wan.test", "name", "test-wan"),
					resource.TestCheckResourceAttr("unifi_wan.test", "type", "dhcp"),
					resource.TestCheckResourceAttr("unifi_wan.test", "vlan.enabled", "true"),
					resource.TestCheckResourceAttr("unifi_wan.test", "vlan.id", "10"),
					resource.TestCheckResourceAttr("unifi_wan.test", "enabled", "true"),
				),
			},
			{
				ResourceName:      "unifi_wan.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

// TestAccWANFramework_minimal verifies that a WAN with no optional nested objects
// can be created and imported without "was null, but now..." errors from API defaults.
func TestAccWANFramework_minimal(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccWANFrameworkConfig_minimal(),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet("unifi_wan.minimal", "id"),
					resource.TestCheckResourceAttr("unifi_wan.minimal", "name", "test-wan-minimal"),
					resource.TestCheckResourceAttr("unifi_wan.minimal", "type", "dhcp"),
					resource.TestCheckResourceAttr("unifi_wan.minimal", "enabled", "true"),
				),
			},
			{
				ResourceName:      "unifi_wan.minimal",
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

// TestAccWANFramework_withNestedObjects verifies that explicitly configured nested
// objects are preserved through create, read, and import.
func TestAccWANFramework_withNestedObjects(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccWANFrameworkConfig_withNestedObjects(),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet("unifi_wan.nested", "id"),
					resource.TestCheckResourceAttr("unifi_wan.nested", "name", "test-wan-nested"),
					resource.TestCheckResourceAttr("unifi_wan.nested", "type", "dhcp"),
					resource.TestCheckResourceAttr("unifi_wan.nested", "enabled", "true"),
					// VLAN
					resource.TestCheckResourceAttr("unifi_wan.nested", "vlan.enabled", "true"),
					resource.TestCheckResourceAttr("unifi_wan.nested", "vlan.id", "20"),
					// DNS
					resource.TestCheckResourceAttr("unifi_wan.nested", "dns.preference", "manual"),
					resource.TestCheckResourceAttr("unifi_wan.nested", "dns.primary", "8.8.8.8"),
					resource.TestCheckResourceAttr("unifi_wan.nested", "dns.secondary", "8.8.4.4"),
					// Load Balance
					resource.TestCheckResourceAttrSet(
						"unifi_wan.nested",
						"load_balance.failover_priority",
					),
				),
			},
			{
				ResourceName:      "unifi_wan.nested",
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

func testAccWANFrameworkConfig_basic() string {
	return `
resource "unifi_wan" "test" {
	name    = "test-wan"
	type    = "dhcp"
	enabled = true

	vlan = {
		enabled = true
		id      = 10
	}
}
`
}

func testAccWANFrameworkConfig_minimal() string {
	return `
resource "unifi_wan" "minimal" {
	name    = "test-wan-minimal"
	type    = "dhcp"
	enabled = true
}
`
}

func testAccWANFrameworkConfig_withNestedObjects() string {
	return `
resource "unifi_wan" "nested" {
	name    = "test-wan-nested"
	type    = "dhcp"
	enabled = true

	vlan = {
		enabled = true
		id      = 20
	}

	dns = {
		preference = "manual"
		primary    = "8.8.8.8"
		secondary  = "8.8.4.4"
	}

	load_balance = {
		failover_priority = 1
	}
}
`
}

// TestAccWANFramework_additionalFields verifies the newly exposed top-level
// fields round-trip through create, read, and import without spurious diffs.
func TestAccWANFramework_additionalFields(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccWANFrameworkConfig_additionalFields(),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet("unifi_wan.extra", "id"),
					resource.TestCheckResourceAttr("unifi_wan.extra", "name", "test-wan-extra"),
					// Computed fields populated from the controller.
					resource.TestCheckResourceAttrSet(
						"unifi_wan.extra",
						"mac_override_enabled",
					),
					resource.TestCheckResourceAttrSet(
						"unifi_wan.extra",
						"wan_dslite_remote_host_auto",
					),
				),
			},
			{
				ResourceName:      "unifi_wan.extra",
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

func testAccWANFrameworkConfig_additionalFields() string {
	// setting_preference is intentionally not pinned: the controller reverts
	// it to "auto" for a dhcp WAN regardless of what's sent, causing
	// perpetual auto->manual plan drift if asserted here.
	return `
resource "unifi_wan" "extra" {
	name    = "test-wan-extra"
	type    = "dhcp"
	enabled = true
}
`
}

func TestNewWANResource(t *testing.T) {
	got := NewWANResource()
	if got == nil {
		t.Fatal("NewWANResource() returned nil")
	}
	if _, ok := got.(fwresource.ResourceWithImportState); !ok {
		t.Errorf("NewWANResource() does not implement fwresource.ResourceWithImportState")
	}
	if _, ok := got.(fwresource.ResourceWithIdentity); !ok {
		t.Errorf("NewWANResource() does not implement fwresource.ResourceWithIdentity")
	}
}

func TestNewWANListResource(t *testing.T) {
	got := NewWANListResource()
	if got == nil {
		t.Fatal("NewWANListResource() returned nil")
	}
	if _, ok := got.(fwlist.ListResourceWithConfigure); !ok {
		t.Errorf("NewWANListResource() does not implement fwlist.ListResourceWithConfigure")
	}
}

func Test_vlanModel_AttributeTypes(t *testing.T) {
	tests := []struct {
		name string
		m    vlanModel
		want map[string]attr.Type
	}{
		{
			name: "returns correct types",
			want: map[string]attr.Type{
				"enabled": types.BoolType,
				"id":      types.Int64Type,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.m.AttributeTypes(); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("vlanModel.AttributeTypes() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_egressQosModel_AttributeTypes(t *testing.T) {
	tests := []struct {
		name string
		m    egressQosModel
		want map[string]attr.Type
	}{
		{
			name: "returns correct types",
			want: map[string]attr.Type{
				"enabled":  types.BoolType,
				"priority": types.Int64Type,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.m.AttributeTypes(); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("egressQosModel.AttributeTypes() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_smartqModel_AttributeTypes(t *testing.T) {
	tests := []struct {
		name string
		m    smartqModel
		want map[string]attr.Type
	}{
		{
			name: "returns correct types",
			want: map[string]attr.Type{
				"enabled":   types.BoolType,
				"up_rate":   types.Int64Type,
				"down_rate": types.Int64Type,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.m.AttributeTypes(); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("smartqModel.AttributeTypes() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_providerCapabilitiesModel_AttributeTypes(t *testing.T) {
	tests := []struct {
		name string
		m    providerCapabilitiesModel
		want map[string]attr.Type
	}{
		{
			name: "returns correct types",
			want: map[string]attr.Type{
				"download_kilobits_per_second": types.Int64Type,
				"upload_kilobits_per_second":   types.Int64Type,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.m.AttributeTypes(); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("providerCapabilitiesModel.AttributeTypes() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_dhcpOptionModel_AttributeTypes(t *testing.T) {
	tests := []struct {
		name string
		m    dhcpOptionModel
		want map[string]attr.Type
	}{
		{
			name: "returns correct types",
			want: map[string]attr.Type{
				"option_number": types.Int64Type,
				"value":         types.StringType,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.m.AttributeTypes(); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("dhcpOptionModel.AttributeTypes() = %v, want %v", got, tt.want)
			}
		})
	}
}

// Test_wanResource_networkGroup checks the WAN network group is preserved in
// the update PUT (and HiddenID mirrors it) instead of being hard-coded to
// "WAN" -- otherwise a secondary uplink (WAN2) collides with the primary and
// the controller rejects it.
func Test_wanResource_networkGroup(t *testing.T) {
	r := &wanResource{}
	ctx := context.Background()

	base := wanResourceModel{
		Name:    types.StringValue("CC Internet SFP"),
		Enabled: types.BoolValue(true),
		Type:    types.StringValue("dhcp"),
	}

	t.Run("WAN2 is preserved and mirrored to hidden id", func(t *testing.T) {
		m := base
		m.NetworkGroup = types.StringValue("WAN2")
		n, d := r.modelToNetwork(ctx, &m)
		if d.HasError() {
			t.Fatalf("modelToNetwork: %v", d)
		}
		if n.WANNetworkGroup == nil || *n.WANNetworkGroup != "WAN2" {
			t.Errorf("WANNetworkGroup = %v, want WAN2", n.WANNetworkGroup)
		}
		if n.HiddenID != "WAN2" {
			t.Errorf("HiddenID = %q, want WAN2", n.HiddenID)
		}
	})

	t.Run("unset defaults to WAN", func(t *testing.T) {
		m := base
		m.NetworkGroup = types.StringNull()
		n, d := r.modelToNetwork(ctx, &m)
		if d.HasError() {
			t.Fatalf("modelToNetwork: %v", d)
		}
		if n.WANNetworkGroup == nil || *n.WANNetworkGroup != "WAN" {
			t.Errorf("WANNetworkGroup = %v, want WAN", n.WANNetworkGroup)
		}
		if n.HiddenID != "WAN" {
			t.Errorf("HiddenID = %q, want WAN", n.HiddenID)
		}
	})
}

// Test_wanResource_overlayConfig_dslite checks that overlayConfig keeps the
// user's planned value for wan_dslite_remote_host_auto when it was set in
// config, and the controller's value when it wasn't -- the controller
// otherwise forces this back to true server-side.
func Test_wanResource_overlayConfig_dslite(t *testing.T) {
	r := &wanResource{}

	t.Run("configured false overrides controller true", func(t *testing.T) {
		state := wanResourceModel{DsliteRemoteHostAuto: types.BoolValue(true)}
		config := wanResourceModel{DsliteRemoteHostAuto: types.BoolValue(false)}
		plan := wanResourceModel{DsliteRemoteHostAuto: types.BoolValue(false)}
		r.overlayConfig(&state, &config, &plan)
		if state.DsliteRemoteHostAuto.ValueBool() {
			t.Errorf("DsliteRemoteHostAuto = true, want false (planned value)")
		}
	})

	t.Run("unset keeps controller value", func(t *testing.T) {
		state := wanResourceModel{DsliteRemoteHostAuto: types.BoolValue(true)}
		config := wanResourceModel{DsliteRemoteHostAuto: types.BoolNull()}
		plan := wanResourceModel{DsliteRemoteHostAuto: types.BoolValue(false)}
		r.overlayConfig(&state, &config, &plan)
		if !state.DsliteRemoteHostAuto.ValueBool() {
			t.Errorf("DsliteRemoteHostAuto = false, want true (controller value kept)")
		}
	})
}

// Test_dnsAddrValue checks that "" and a nil pointer both map to null, since
// the controller persists an unset WAN DNS address as "" while the Optional
// address fields plan as null -- a real address must still round-trip.
func Test_dnsAddrValue(t *testing.T) {
	empty := ""
	addr := "2001:4860:4860::8888"
	v4 := "8.8.8.8"

	cases := []struct {
		name     string
		in       *string
		wantNull bool
		wantStr  string
	}{
		{"nil pointer -> null", nil, true, ""},
		{"empty string -> null", &empty, true, ""},
		{"ipv6 address survives", &addr, false, addr},
		{"ipv4 address survives", &v4, false, v4},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := dnsAddrValue(c.in)
			if got.IsNull() != c.wantNull {
				t.Errorf("IsNull = %v, want %v", got.IsNull(), c.wantNull)
			}
			if !c.wantNull && got.ValueString() != c.wantStr {
				t.Errorf("ValueString = %q, want %q", got.ValueString(), c.wantStr)
			}
		})
	}
}

func Test_dnsModel_AttributeTypes(t *testing.T) {
	tests := []struct {
		name string
		m    dnsModel
		want map[string]attr.Type
	}{
		{
			name: "returns correct types",
			want: map[string]attr.Type{
				"primary":         types.StringType,
				"secondary":       types.StringType,
				"ipv6_primary":    types.StringType,
				"ipv6_secondary":  types.StringType,
				"preference":      types.StringType,
				"ipv6_preference": types.StringType,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.m.AttributeTypes(); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("dnsModel.AttributeTypes() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_upnpModel_AttributeTypes(t *testing.T) {
	tests := []struct {
		name string
		m    upnpModel
		want map[string]attr.Type
	}{
		{
			name: "returns correct types",
			want: map[string]attr.Type{
				"enabled":         types.BoolType,
				"wan_interface":   types.StringType,
				"nat_pmp_enabled": types.BoolType,
				"secure_mode":     types.BoolType,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.m.AttributeTypes(); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("upnpModel.AttributeTypes() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_loadBalanceModel_AttributeTypes(t *testing.T) {
	tests := []struct {
		name string
		m    loadBalanceModel
		want map[string]attr.Type
	}{
		{
			name: "returns correct types",
			want: map[string]attr.Type{
				"type":              types.StringType,
				"weight":            types.Int64Type,
				"failover_priority": types.Int64Type,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.m.AttributeTypes(); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("loadBalanceModel.AttributeTypes() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_igmpProxyModel_AttributeTypes(t *testing.T) {
	tests := []struct {
		name string
		m    igmpProxyModel
		want map[string]attr.Type
	}{
		{
			name: "returns correct types",
			want: map[string]attr.Type{
				"downstream": types.StringType,
				"upstream":   types.BoolType,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.m.AttributeTypes(); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("igmpProxyModel.AttributeTypes() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_dhcpv6WanModel_AttributeTypes(t *testing.T) {
	tests := []struct {
		name string
		m    dhcpv6WanModel
		want map[string]attr.Type
	}{
		{
			name: "returns correct types",
			want: map[string]attr.Type{
				"cos":          types.Int64Type,
				"pd_size":      types.Int64Type,
				"pd_size_auto": types.BoolType,
				"options": types.ListType{
					ElemType: types.ObjectType{AttrTypes: dhcpOptionModel{}.AttributeTypes()},
				},
				"wan_delegation_type": types.StringType,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.m.AttributeTypes(); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("dhcpv6WanModel.AttributeTypes() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_dhcpWanModel_AttributeTypes(t *testing.T) {
	tests := []struct {
		name string
		m    dhcpWanModel
		want map[string]attr.Type
	}{
		{
			name: "returns correct types",
			want: map[string]attr.Type{
				"cos": types.Int64Type,
				"options": types.ListType{
					ElemType: types.ObjectType{AttrTypes: dhcpOptionModel{}.AttributeTypes()},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.m.AttributeTypes(); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("dhcpWanModel.AttributeTypes() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_wanResource_IdentitySchema(t *testing.T) {
	t.Run("does not panic and returns identity attributes", func(t *testing.T) {
		r := &wanResource{}
		resp := &fwresource.IdentitySchemaResponse{}
		r.IdentitySchema(context.Background(), fwresource.IdentitySchemaRequest{}, resp)
		if resp.Diagnostics.HasError() {
			t.Fatalf("IdentitySchema() returned errors: %v", resp.Diagnostics)
		}
		if len(resp.IdentitySchema.Attributes) == 0 {
			t.Error("IdentitySchema() returned no attributes")
		}
	})
}

// TestWANConfigWithStaticTypeAndNoAddressIsRefused pins the stopgap in
// ValidateConfig: the model has no ip/netmask/gateway attributes yet, so
// type = "static" must be refused rather than plan clean and write an
// unaddressable WAN. dhcp is the control and must not error.
func TestWANConfigWithStaticTypeAndNoAddressIsRefused(t *testing.T) {
	ctx := context.Background()
	r := &wanResource{}
	schemaResp := &fwresource.SchemaResponse{}
	r.Schema(ctx, fwresource.SchemaRequest{}, schemaResp)
	if schemaResp.Diagnostics.HasError() {
		t.Fatalf("build the schema: %v", schemaResp.Diagnostics)
	}

	configFor := func(t *testing.T, wanType string) tfsdk.Config {
		t.Helper()
		model := &wanResourceModel{
			Name:     types.StringValue("wan1"),
			Type:     types.StringValue(wanType),
			Enabled:  types.BoolValue(true),
			Timeouts: timeoutsNullValue(),
		}
		applyWANDefaults(model)
		staging := tfsdk.State{Schema: schemaResp.Schema}
		if diags := staging.Set(ctx, model); diags.HasError() {
			t.Fatalf("set the config: %v", diags)
		}
		return tfsdk.Config{Schema: schemaResp.Schema, Raw: staging.Raw}
	}

	tests := []struct {
		name      string
		wanType   string
		wantError bool
	}{
		{"static_type_is_refused", "static", true},
		{"dhcp_type_is_allowed", "dhcp", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := &fwresource.ValidateConfigResponse{}
			r.ValidateConfig(ctx, fwresource.ValidateConfigRequest{
				Config: configFor(t, tt.wanType),
			}, resp)
			if got := resp.Diagnostics.HasError(); got != tt.wantError {
				t.Errorf("type=%q: got error=%v, want %v (diags: %v)",
					tt.wanType, got, tt.wantError, resp.Diagnostics)
			}
		})
	}
}

func Test_wanResource_modelToNetwork(t *testing.T) {
	t.Run("minimal model converts correctly", func(t *testing.T) {
		r := &wanResource{}
		ctx := context.Background()
		model := &wanResourceModel{
			Name:                  types.StringValue("test"),
			Type:                  types.StringValue("dhcp"),
			TypeV6:                types.StringNull(),
			Enabled:               types.BoolValue(true),
			Vlan:                  types.ObjectNull(vlanModel{}.AttributeTypes()),
			EgressQoS:             types.ObjectNull(egressQosModel{}.AttributeTypes()),
			DNS:                   types.ObjectNull(dnsModel{}.AttributeTypes()),
			DHCP:                  types.ObjectNull(dhcpWanModel{}.AttributeTypes()),
			DHCPv6:                types.ObjectNull(dhcpv6WanModel{}.AttributeTypes()),
			SmartQ:                types.ObjectNull(smartqModel{}.AttributeTypes()),
			UPnP:                  types.ObjectNull(upnpModel{}.AttributeTypes()),
			LoadBalance:           types.ObjectNull(loadBalanceModel{}.AttributeTypes()),
			IGMPProxy:             types.ObjectNull(igmpProxyModel{}.AttributeTypes()),
			ProviderCapabilities:  types.ObjectNull(providerCapabilitiesModel{}.AttributeTypes()),
			ReportWANEvent:        types.BoolNull(),
			IPAliases:             types.ListNull(types.StringType),
			SettingPreference:     types.StringNull(),
			IPv6SettingPreference: types.StringNull(),
			SingleNetworkLAN:      types.StringNull(),
			MACOverrideEnabled:    types.BoolNull(),
			DsliteRemoteHost:      types.StringNull(),
			DsliteRemoteHostAuto:  types.BoolNull(),
		}
		got, diags := r.modelToNetwork(ctx, model)
		if diags.HasError() {
			t.Fatalf("modelToNetwork() returned errors: %v", diags)
		}
		if got == nil {
			t.Fatal("modelToNetwork() returned nil network")
		}
		if got.Name == nil || *got.Name != "test" {
			t.Errorf("expected Name=test, got %v", got.Name)
		}
		if got.WANType == nil || *got.WANType != "dhcp" {
			t.Errorf("expected WANType=dhcp, got %v", got.WANType)
		}
		if got.Purpose != "wan" {
			t.Errorf("expected Purpose=wan, got %v", got.Purpose)
		}
		if !got.Enabled {
			t.Error("expected Enabled=true")
		}
	})
}

func Test_wanResource_networkToModel(t *testing.T) {
	t.Run("converts API network back to model", func(t *testing.T) {
		r := &wanResource{}
		ctx := context.Background()
		wanType := "dhcp"
		name := "test-wan"
		network := &unifi.Network{
			ID:      "abc123",
			Name:    &name,
			Purpose: "wan",
			WANType: &wanType,
			Enabled: true,
		}
		model := &wanResourceModel{}
		applyWANDefaults(model)
		diags := r.networkToModel(ctx, network, model, "default")
		if diags.HasError() {
			t.Fatalf("networkToModel() returned errors: %v", diags)
		}
		if model.ID.ValueString() != "abc123" {
			t.Errorf("expected ID=abc123, got %v", model.ID.ValueString())
		}
		if model.Site.ValueString() != "default" {
			t.Errorf("expected Site=default, got %v", model.Site.ValueString())
		}
		if model.Name.ValueString() != "test-wan" {
			t.Errorf("expected Name=test-wan, got %v", model.Name.ValueString())
		}
		if model.Type.ValueString() != "dhcp" {
			t.Errorf("expected Type=dhcp, got %v", model.Type.ValueString())
		}
	})
}

func Test_applyWANDefaults(t *testing.T) {
	t.Run("applies defaults to empty model", func(t *testing.T) {
		model := &wanResourceModel{}
		applyWANDefaults(model)
		if !model.Vlan.IsNull() {
			t.Error("expected Vlan to be null after defaults")
		}
		if !model.EgressQoS.IsNull() {
			t.Error("expected EgressQoS to be null after defaults")
		}
		if !model.SmartQ.IsNull() {
			t.Error("expected SmartQ to be null after defaults")
		}
		if !model.DNS.IsNull() {
			t.Error("expected DNS to be null after defaults")
		}
		if !model.IPAliases.IsNull() {
			t.Error("expected IPAliases to be null after defaults")
		}
	})
}

func Test_wanResource_ListResourceConfigSchema(t *testing.T) {
	t.Run("does not panic", func(t *testing.T) {
		r := &wanResource{}
		resp := &fwlist.ListResourceSchemaResponse{}
		r.ListResourceConfigSchema(context.Background(), fwlist.ListResourceSchemaRequest{}, resp)
		if resp.Diagnostics.HasError() {
			t.Fatalf("ListResourceConfigSchema() returned errors: %v", resp.Diagnostics)
		}
	})
}
