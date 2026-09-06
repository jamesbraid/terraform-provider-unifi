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
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/querycheck"
	"github.com/hashicorp/terraform-plugin-testing/tfversion"
	"github.com/ubiquiti-community/go-unifi/unifi"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/resourcekit"
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

// wanEncodedForWrite builds the SDK object the way the write path does: the
// spec's ToSDK for the Fields, then BeforeSend for the hook-derived wires.
func wanEncodedForWrite(t *testing.T, model *wanKitModel) *unifi.Network {
	t.Helper()
	ctx := context.Background()
	spec := wanKitSpec()
	sdk, diags := spec.ToSDK(ctx, model)
	if diags.HasError() {
		t.Fatalf("ToSDK: %v", diags)
	}
	var config wanKitModel
	var prior wanKitModel
	if d := spec.BeforeSend(ctx, &config, model, prior, sdk, nil); d.HasError() {
		t.Fatalf("BeforeSend: %v", d)
	}
	return sdk
}

// Test_wanNetworkGroup checks the WAN network group is preserved on every
// write (and HiddenID mirrors it) instead of being hard-coded to "WAN" --
// otherwise a secondary uplink (WAN2) collides with the primary and the
// controller rejects it (#334).
func Test_wanNetworkGroup(t *testing.T) {
	base := wanKitModel{
		Name:    types.StringValue("CC Internet SFP"),
		Enabled: types.BoolValue(true),
		Type:    types.StringValue("dhcp"),
	}

	t.Run("WAN2 is preserved and mirrored to hidden id", func(t *testing.T) {
		m := base
		m.NetworkGroup = types.StringValue("WAN2")
		n := wanEncodedForWrite(t, &m)
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
		n := wanEncodedForWrite(t, &m)
		if n.WANNetworkGroup == nil || *n.WANNetworkGroup != "WAN" {
			t.Errorf("WANNetworkGroup = %v, want WAN", n.WANNetworkGroup)
		}
		if n.HiddenID != "WAN" {
			t.Errorf("HiddenID = %q, want WAN", n.HiddenID)
		}
	})
}

// Test_wanDslitePlanOutranksTheEcho pins the #281 behaviour through the
// kit's plan-over-response rule: the controller forces
// wan_dslite_remote_host_auto back to true on AFTR auto-detection, so a
// planned false must survive the read-back, while an unset plan keeps the
// controller's answer.
func Test_wanDslitePlanOutranksTheEcho(t *testing.T) {
	spec := wanKitSpec()

	t.Run("configured false overrides controller true", func(t *testing.T) {
		state := wanKitModel{DsliteRemoteHostAuto: types.BoolValue(true)}
		plan := wanKitModel{DsliteRemoteHostAuto: types.BoolValue(false)}
		spec.ApplyPlanToState(&plan, &state)
		if state.DsliteRemoteHostAuto.ValueBool() {
			t.Errorf("DsliteRemoteHostAuto = true, want false (planned value)")
		}
	})

	t.Run("unset keeps controller value", func(t *testing.T) {
		state := wanKitModel{DsliteRemoteHostAuto: types.BoolValue(true)}
		plan := wanKitModel{DsliteRemoteHostAuto: types.BoolNull()}
		spec.ApplyPlanToState(&plan, &state)
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
		r := newWANKitResource()
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
	r := newWANKitResource()
	schemaResp := &fwresource.SchemaResponse{}
	r.Schema(ctx, fwresource.SchemaRequest{}, schemaResp)
	if schemaResp.Diagnostics.HasError() {
		t.Fatalf("build the schema: %v", schemaResp.Diagnostics)
	}

	configFor := func(t *testing.T, wanType string) tfsdk.Config {
		t.Helper()
		model := nullWANKitModel()
		model.Name = types.StringValue("wan1")
		model.Type = types.StringValue(wanType)
		model.Enabled = types.BoolValue(true)
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

// nullWANKitModel is a model with every collection-shaped attribute at its
// typed null, which is what an empty configuration decodes to.
func nullWANKitModel() *wanKitModel {
	return &wanKitModel{
		Vlan:                 types.ObjectNull(vlanModel{}.AttributeTypes()),
		EgressQoS:            types.ObjectNull(egressQosModel{}.AttributeTypes()),
		DNS:                  types.ObjectNull(dnsModel{}.AttributeTypes()),
		DHCP:                 types.ObjectNull(dhcpWanModel{}.AttributeTypes()),
		DHCPv6:               types.ObjectNull(dhcpv6WanModel{}.AttributeTypes()),
		SmartQ:               types.ObjectNull(smartqModel{}.AttributeTypes()),
		UPnP:                 types.ObjectNull(upnpModel{}.AttributeTypes()),
		LoadBalance:          types.ObjectNull(loadBalanceModel{}.AttributeTypes()),
		IGMPProxy:            types.ObjectNull(igmpProxyModel{}.AttributeTypes()),
		ProviderCapabilities: types.ObjectNull(providerCapabilitiesModel{}.AttributeTypes()),
		IPAliases:            types.ListNull(types.StringType),
		Timeouts:             timeoutsNullValue(),
	}
}

func Test_wanToSDK(t *testing.T) {
	t.Run("minimal model converts correctly", func(t *testing.T) {
		model := nullWANKitModel()
		model.Name = types.StringValue("test")
		model.Type = types.StringValue("dhcp")
		model.Enabled = types.BoolValue(true)
		got := wanEncodedForWrite(t, model)
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

func Test_wanToModel(t *testing.T) {
	t.Run("converts API network back to model", func(t *testing.T) {
		spec := wanKitSpec()
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
		model := nullWANKitModel()
		diags := spec.ToModel(ctx, network, model, "default")
		if diags.HasError() {
			t.Fatalf("ToModel() returned errors: %v", diags)
		}
		var prior wanKitModel
		diags = spec.AfterReceive(ctx, network, model, prior, nil)
		if diags.HasError() {
			t.Fatalf("AfterReceive() returned errors: %v", diags)
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
		// The group is defaulted the way the hand read did (#334).
		if model.NetworkGroup.ValueString() != "WAN" {
			t.Errorf("expected NetworkGroup=WAN, got %v", model.NetworkGroup.ValueString())
		}
	})
}

// Test_wanDecodeAbsence pins the read shape for an unconfigured WAN: vlan is
// always materialized (the controller omits the id when unset, and the object
// carries the schema default instead, #262), while every other nested object
// and the alias list stay typed nulls until the API reports data.
func Test_wanDecodeAbsence(t *testing.T) {
	spec := wanKitSpec()
	ctx := context.Background()
	model := nullWANKitModel()
	diags := spec.ToModel(ctx, &unifi.Network{Purpose: "wan"}, model, "default")
	if diags.HasError() {
		t.Fatalf("ToModel() returned errors: %v", diags)
	}
	if model.Vlan.IsNull() {
		t.Error("expected Vlan to be materialized with defaults")
	}
	if !model.EgressQoS.IsNull() {
		t.Error("expected EgressQoS to stay null")
	}
	if !model.SmartQ.IsNull() {
		t.Error("expected SmartQ to stay null")
	}
	if !model.DNS.IsNull() {
		t.Error("expected DNS to stay null")
	}
	if !model.IPAliases.IsNull() {
		t.Error("expected IPAliases to stay null")
	}
}

func Test_wanResource_ListResourceConfigSchema(t *testing.T) {
	t.Run("does not panic", func(t *testing.T) {
		r := newWANKitResource()
		resp := &fwlist.ListResourceSchemaResponse{}
		r.ListResourceConfigSchema(context.Background(), fwlist.ListResourceSchemaRequest{}, resp)
		if resp.Diagnostics.HasError() {
			t.Fatalf("ListResourceConfigSchema() returned errors: %v", resp.Diagnostics)
		}
	})
}

// unknownWANAttr builds the unknown value of one member type, for the probe
// below. The switch covers every member type wan's objects declare.
func unknownWANAttr(t *testing.T, typ attr.Type) attr.Value {
	t.Helper()
	switch concrete := typ.(type) {
	case basetypes.BoolType:
		return types.BoolUnknown()
	case basetypes.Int64Type:
		return types.Int64Unknown()
	case basetypes.StringType:
		return types.StringUnknown()
	case types.ListType:
		return types.ListUnknown(concrete.ElemType)
	}
	t.Fatalf("no unknown value for member type %T; teach unknownWANAttr about it", typ)
	return nil
}

// Test_wanDecodeResolvesUnknownPriorMembers hands every scattered Decode a
// prior whose members are all unknown -- the shape a create hands it when
// the configuration sets the object but omits its Optional+Computed
// members -- against a response carrying none of the wires. Every member
// must come back known (a value or null): Terraform refuses an unknown
// after apply, which is how dns.ipv6_preference, load_balance.type and
// load_balance.weight failed TestAccWANFramework_withNestedObjects.
func Test_wanDecodeResolvesUnknownPriorMembers(t *testing.T) {
	ctx := context.Background()
	checked := 0
	for _, field := range wanKitSpec().Fields {
		scattered, ok := field.(resourcekit.ScatteredObjectField[wanKitModel, unifi.Network])
		if !ok {
			continue
		}
		members := make(map[string]attr.Value, len(scattered.AttrTypes))
		for name, typ := range scattered.AttrTypes {
			members[name] = unknownWANAttr(t, typ)
		}
		prior, d := types.ObjectValue(scattered.AttrTypes, members)
		if d.HasError() {
			t.Fatalf("%s: building the all-unknown prior: %v", scattered.Wires[0], d)
		}
		object, d := scattered.Decode(ctx, &unifi.Network{Purpose: unifi.PurposeWAN}, prior)
		if d.HasError() {
			t.Fatalf("%s: Decode: %v", scattered.Wires[0], d)
		}
		checked++
		if object.IsUnknown() {
			t.Errorf("%s: Decode returned an unknown object", scattered.Wires[0])
			continue
		}
		if object.IsNull() {
			continue
		}
		for name, value := range object.Attributes() {
			if value.IsUnknown() {
				t.Errorf("%s: member %q is still unknown after Decode; Terraform refuses "+
					"an unknown after apply", scattered.Wires[0], name)
			}
		}
	}
	if checked != 10 {
		t.Errorf("checked %d scattered field(s), want 10; a field the walk missed is one "+
			"whose Decode nothing here holds to the known-members rule", checked)
	}
}
