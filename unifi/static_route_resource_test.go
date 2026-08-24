package unifi

import (
	"context"
	"fmt"
	"net/netip"
	"os"
	"reflect"
	"slices"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework-nettypes/iptypes"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	fwlist "github.com/hashicorp/terraform-plugin-framework/list"
	"github.com/hashicorp/terraform-plugin-framework/path"
	fwresource "github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/querycheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/hashicorp/terraform-plugin-testing/tfversion"
	"github.com/ubiquiti-community/go-unifi/unifi"
	"github.com/ubiquiti-community/terraform-provider-unifi/unifi/validators"
)

func testAccStaticRouteCheckDestroy(s *terraform.State) error {
	ctx := context.Background()
	apiURL := os.Getenv("UNIFI_API")
	if apiURL == "" {
		return nil
	}
	apiClient, err := unifi.New(ctx, &unifi.Config{
		BaseURL:       apiURL,
		Username:      os.Getenv("UNIFI_USERNAME"),
		Password:      os.Getenv("UNIFI_PASSWORD"),
		AllowInsecure: true,
	})
	if err != nil {
		return nil //nolint:nilerr // best-effort check; skip when no live client
	}
	c := &Client{ApiClient: apiClient, Site: "default"}
	for _, rs := range s.RootModule().Resources {
		if rs.Type != "unifi_static_route" {
			continue
		}
		site := rs.Primary.Attributes["site"]
		if site == "" {
			site = c.Site
		}
		_, err := c.GetRouting(ctx, site, rs.Primary.ID)
		if err == nil {
			return fmt.Errorf("unifi_static_route %s still exists", rs.Primary.ID)
		}
		if _, ok := err.(*unifi.NotFoundError); !ok {
			return err
		}
	}
	return nil
}

// TestUnitStaticRoute_nextHopValidation verifies the next_hop validator accepts IPv4 and IPv6
// and rejects non-IP values, without requiring a real UniFi controller.
func TestUnitStaticRoute_nextHopValidation(t *testing.T) {
	// Replicate the validator used in the schema.
	v := stringvalidator.Any(validators.IPv4Validator(), validators.IPv6Validator())

	tests := []struct {
		name      string
		nextHop   string
		wantError bool
	}{
		{name: "valid_ipv4", nextHop: "192.168.1.1", wantError: false},
		{
			name:      "valid_ipv6_full",
			nextHop:   "2001:0db8:0000:0000:0000:0000:0000:0001",
			wantError: false,
		},
		{name: "valid_ipv6_compressed", nextHop: "2001:db8::1", wantError: false},
		{name: "valid_ipv6_loopback", nextHop: "::1", wantError: false},
		{name: "invalid_hostname", nextHop: "not-an-ip", wantError: true},
		{name: "invalid_cidr", nextHop: "192.168.1.0/24", wantError: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := validator.StringRequest{
				Path:           path.Root("next_hop"),
				PathExpression: path.MatchRoot("next_hop"),
				ConfigValue:    types.StringValue(tc.nextHop),
				Config:         tfsdk.Config{}, // unused by these validators
			}
			resp := &validator.StringResponse{}
			v.ValidateString(context.Background(), req, resp)

			hasError := resp.Diagnostics.HasError()
			if hasError != tc.wantError {
				t.Errorf("next_hop=%q: got error=%v, want error=%v (diags: %v)",
					tc.nextHop, hasError, tc.wantError, resp.Diagnostics)
			}
		})
	}
}

func TestAccStaticRouteFramework_basic(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: providerFactories,
		CheckDestroy:             testAccStaticRouteCheckDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccStaticRouteFrameworkConfig_basic(),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("unifi_static_route.test", "name", "test-route"),
					resource.TestCheckResourceAttr(
						"unifi_static_route.test",
						"network",
						"192.168.100.0/24",
					),
					resource.TestCheckResourceAttr(
						"unifi_static_route.test",
						"type",
						"nexthop-route",
					),
					resource.TestCheckResourceAttr("unifi_static_route.test", "distance", "1"),
					resource.TestCheckResourceAttr(
						"unifi_static_route.test",
						"next_hop",
						"192.168.1.1",
					),
				),
			},
			{
				ResourceName:      "unifi_static_route.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

func TestAccStaticRouteFramework_ipv6NextHop(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: providerFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccStaticRouteFrameworkConfig_ipv6(),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"unifi_static_route.test",
						"name",
						"test-route-ipv6",
					),
					resource.TestCheckResourceAttr(
						"unifi_static_route.test",
						"next_hop",
						"2001:db8::1",
					),
				),
			},
			{
				ResourceName:      "unifi_static_route.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

func testAccStaticRouteFrameworkConfig_basic() string {
	return `
resource "unifi_static_route" "test" {
	name     = "test-route"
	network  = "192.168.100.0/24"
	type     = "nexthop-route"
	distance = 1
	next_hop = "192.168.1.1"
}
`
}

func TestAccStaticRouteFramework_enabledAndGateway(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: providerFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccStaticRouteFrameworkConfig_disabled(),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"unifi_static_route.disabled",
						"enabled",
						"false",
					),
					// gateway_type defaults to the controller value.
					resource.TestCheckResourceAttr(
						"unifi_static_route.disabled",
						"gateway_type",
						"default",
					),
				),
			},
			{
				ResourceName:      "unifi_static_route.disabled",
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

func testAccStaticRouteFrameworkConfig_disabled() string {
	return `
resource "unifi_static_route" "disabled" {
	name     = "test-route-disabled"
	network  = "192.168.101.0/24"
	type     = "nexthop-route"
	distance = 1
	next_hop = "192.168.1.1"
	enabled  = false
}
`
}

func testAccStaticRouteFrameworkConfig_ipv6() string {
	return `
resource "unifi_static_route" "test" {
	name     = "test-route-ipv6"
	network  = "2001:db8::/32"
	type     = "nexthop-route"
	distance = 1
	next_hop = "2001:db8::1"
}
`
}

func TestNewStaticRouteFrameworkResource(t *testing.T) {
	r := NewStaticRouteFrameworkResource()
	if r == nil {
		t.Fatal("returned nil")
	}
	if _, ok := r.(fwresource.ResourceWithConfigure); !ok {
		t.Error("expected ResourceWithConfigure")
	}
	if _, ok := r.(fwresource.ResourceWithImportState); !ok {
		t.Error("expected ResourceWithImportState")
	}
	if _, ok := r.(fwresource.ResourceWithIdentity); !ok {
		t.Error("expected ResourceWithIdentity")
	}
	if _, ok := r.(fwresource.ResourceWithConfigValidators); !ok {
		t.Error("expected ResourceWithConfigValidators")
	}
}

func TestNewStaticRouteListResource(t *testing.T) {
	r := NewStaticRouteListResource()
	if r == nil {
		t.Fatal("returned nil")
	}
	if _, ok := r.(fwlist.ListResourceWithConfigure); !ok {
		t.Error("expected ListResourceWithConfigure")
	}
}

func Test_staticRouteFrameworkResource_IdentitySchema(t *testing.T) {
	r := newStaticRouteKitResource()
	resp := &fwresource.IdentitySchemaResponse{}
	r.IdentitySchema(context.Background(), fwresource.IdentitySchemaRequest{}, resp)
	if _, ok := resp.IdentitySchema.Attributes["id"]; !ok {
		t.Error("expected identity schema to have 'id' attribute")
	}
}

func Test_staticRouteFrameworkResource_ConfigValidators(t *testing.T) {
	r := newStaticRouteKitResource()
	validators := r.ConfigValidators(context.Background())
	if len(validators) == 0 {
		t.Error("expected at least one config validator")
	}
}

func Test_staticRouteIPVersionValidator_Description(t *testing.T) {
	v := &staticRouteIPVersionValidator{}
	want := "network and next_hop must use the same IP version (both IPv4 or both IPv6)"
	if got := v.Description(context.Background()); got != want {
		t.Errorf("Description() = %q, want %q", got, want)
	}
}

func Test_staticRouteIPVersionValidator_MarkdownDescription(t *testing.T) {
	v := &staticRouteIPVersionValidator{}
	want := "network and next_hop must use the same IP version (both IPv4 or both IPv6)"
	if got := v.MarkdownDescription(context.Background()); got != want {
		t.Errorf("MarkdownDescription() = %q, want %q", got, want)
	}
}

func Test_ipVersionsMatch(t *testing.T) {
	tests := []struct {
		name string
		args struct {
			prefix netip.Prefix
			hop    netip.Addr
		}
		want bool
	}{
		{
			name: "both_ipv4",
			args: struct {
				prefix netip.Prefix
				hop    netip.Addr
			}{netip.MustParsePrefix("192.168.0.0/24"), netip.MustParseAddr("10.0.0.1")},
			want: true,
		},
		{
			name: "both_ipv6",
			args: struct {
				prefix netip.Prefix
				hop    netip.Addr
			}{netip.MustParsePrefix("2001:db8::/32"), netip.MustParseAddr("2001:db8::1")},
			want: true,
		},
		{
			name: "ipv4_prefix_ipv6_hop",
			args: struct {
				prefix netip.Prefix
				hop    netip.Addr
			}{netip.MustParsePrefix("192.168.0.0/24"), netip.MustParseAddr("2001:db8::1")},
			want: false,
		},
		{
			name: "ipv6_prefix_ipv4_hop",
			args: struct {
				prefix netip.Prefix
				hop    netip.Addr
			}{netip.MustParsePrefix("2001:db8::/32"), netip.MustParseAddr("10.0.0.1")},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ipVersionsMatch(tt.args.prefix, tt.args.hop); got != tt.want {
				t.Errorf("ipVersionsMatch() = %v, want %v", got, tt.want)
			}
		})
	}
}

// Test_staticRouteIPVersionValidator_ValidateResource covers the real
// validator end to end. A prior cycle deleted the dead validateIPVersionMatch
// this validator never called, along with TestUnitStaticRoute_ipVersionValidator
// -- a test whose name claimed to cover ValidateResource but whose body only
// ever exercised the dead function -- leaving ValidateResource itself with no
// direct test, only Test_ipVersionsMatch's indirect coverage of the predicate
// it calls. This restores that.
func Test_staticRouteIPVersionValidator_ValidateResource(t *testing.T) {
	ctx := context.Background()
	schemaResp := &fwresource.SchemaResponse{}
	newStaticRouteKitResource().Schema(ctx, fwresource.SchemaRequest{}, schemaResp)
	if schemaResp.Diagnostics.HasError() {
		t.Fatalf("build the schema: %v", schemaResp.Diagnostics)
	}

	configFor := func(t *testing.T, network, nextHop string) tfsdk.Config {
		t.Helper()
		model := staticRouteKitModel{
			ID:            types.StringNull(),
			Site:          types.StringNull(),
			Name:          types.StringValue("route1"),
			Network:       types.StringValue(network),
			Type:          types.StringValue("nexthop-route"),
			Distance:      types.Int64Null(),
			NextHop:       iptypes.NewIPAddressValue(nextHop),
			Interface:     types.StringNull(),
			Enabled:       types.BoolValue(true),
			GatewayDevice: types.StringNull(),
			GatewayType:   types.StringNull(),
			Timeouts:      timeoutsNullValue(),
		}
		staging := tfsdk.State{Schema: schemaResp.Schema}
		if diags := staging.Set(ctx, model); diags.HasError() {
			t.Fatalf("set the config: %v", diags)
		}
		return tfsdk.Config{Schema: schemaResp.Schema, Raw: staging.Raw}
	}

	tests := []struct {
		name      string
		network   string
		nextHop   string
		wantError bool
	}{
		{"both_ipv4", "192.168.0.0/24", "10.0.0.1", false},
		{"both_ipv6", "2001:db8::/32", "2001:db8::1", false},
		{"mixed_v4_network_v6_hop", "192.168.0.0/24", "2001:db8::1", true},
		{"mixed_v6_network_v4_hop", "2001:db8::/32", "10.0.0.1", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := &staticRouteIPVersionValidator{}
			resp := &fwresource.ValidateConfigResponse{}
			v.ValidateResource(ctx, fwresource.ValidateConfigRequest{
				Config: configFor(t, tt.network, tt.nextHop),
			}, resp)
			if got := resp.Diagnostics.HasError(); got != tt.wantError {
				t.Errorf("network=%q next_hop=%q: got error=%v, want %v (diags: %v)",
					tt.network, tt.nextHop, got, tt.wantError, resp.Diagnostics)
			}
		})
	}
}

// The three mapper tests below predate the kit cutover: their assertions are
// conformance against the hand-written mapper, with only the calls repointed
// at Spec.ApplyPlanToState/ToSDK/ToModel.

func Test_staticRouteFrameworkResource_applyPlanToState(t *testing.T) {
	spec := staticRouteKitSpec()
	plan := &staticRouteKitModel{
		Name:    types.StringValue("route1"),
		Network: types.StringValue("10.0.0.0/8"),
		Type:    types.StringValue("nexthop-route"),
	}
	state := &staticRouteKitModel{}
	spec.ApplyPlanToState(plan, state)
	if state.Name.ValueString() != "route1" {
		t.Error("expected Name to be copied from plan")
	}
	if state.Network.ValueString() != "10.0.0.0/8" {
		t.Error("expected Network to be copied from plan")
	}
}

func Test_staticRouteFrameworkResource_modelToRouting(t *testing.T) {
	dist := int64(1)
	base := func() *staticRouteKitModel {
		return &staticRouteKitModel{
			Name:          types.StringValue("route1"),
			Network:       types.StringValue("192.168.0.0/24"),
			Distance:      types.Int64Value(1),
			NextHop:       iptypes.NewIPAddressValue("192.168.1.1"),
			Interface:     types.StringValue("eth0"),
			Enabled:       types.BoolValue(true),
			GatewayDevice: types.StringNull(),
			GatewayType:   types.StringValue("default"),
		}
	}

	for _, testCase := range []struct {
		name      string
		routeType string
		want      *unifi.Routing
	}{
		{
			name:      "nexthop-route sends the hop and not the interface",
			routeType: "nexthop-route",
			want: &unifi.Routing{
				Type: "static-route", Name: "route1",
				StaticRouteNetwork: "192.168.0.0/24", StaticRouteType: "nexthop-route",
				StaticRouteDistance: &dist, StaticRouteNexthop: "192.168.1.1",
				Enabled: true, GatewayType: "default",
			},
		},
		{
			// A model holding BOTH values must still send only the one its
			// route type owns -- the reason the write predicate exists.
			name:      "interface-route sends the interface and not the hop",
			routeType: "interface-route",
			want: &unifi.Routing{
				Type: "static-route", Name: "route1",
				StaticRouteNetwork: "192.168.0.0/24", StaticRouteType: "interface-route",
				StaticRouteDistance: &dist, StaticRouteInterface: "eth0",
				Enabled: true, GatewayType: "default",
			},
		},
		{
			name:      "blackhole sends neither",
			routeType: "blackhole",
			want: &unifi.Routing{
				Type: "static-route", Name: "route1",
				StaticRouteNetwork: "192.168.0.0/24", StaticRouteType: "blackhole",
				StaticRouteDistance: &dist,
				Enabled:             true, GatewayType: "default",
			},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			model := base()
			model.Type = types.StringValue(testCase.routeType)
			got, diags := staticRouteKitSpec().ToSDK(context.Background(), model)
			if diags.HasError() {
				t.Fatalf("ToSDK: %v", diags)
			}
			if !reflect.DeepEqual(got, testCase.want) {
				t.Errorf("ToSDK() = %+v, want %+v", got, testCase.want)
			}
		})
	}
}

// The wire mask must agree with the write: a suppressed field still named in
// the update mask would put whatever the SDK struct held on the wire.
func Test_staticRouteFrameworkResource_wireMaskFollowsTheRouteType(t *testing.T) {
	for _, testCase := range []struct {
		routeType string
		absent    string
	}{
		{"nexthop-route", "static-route_interface"},
		{"interface-route", "static-route_nexthop"},
	} {
		t.Run(testCase.routeType, func(t *testing.T) {
			plan := &staticRouteKitModel{
				Name:      types.StringValue("route1"),
				Network:   types.StringValue("192.168.0.0/24"),
				Type:      types.StringValue(testCase.routeType),
				Distance:  types.Int64Value(1),
				NextHop:   iptypes.NewIPAddressValue("192.168.1.1"),
				Interface: types.StringValue("eth0"),
				Enabled:   types.BoolValue(true),
			}
			fields, err := staticRouteKitSpec().WireFields(plan)
			if err != nil {
				t.Fatalf("WireFields: %v", err)
			}
			if slices.Contains(fields, testCase.absent) {
				t.Errorf(
					"a %s names %q on the wire: %v",
					testCase.routeType,
					testCase.absent,
					fields,
				)
			}
		})
	}
}

func Test_staticRouteFrameworkResource_routingToModel(t *testing.T) {
	dist := int64(1)
	routing := &unifi.Routing{
		ID:                  "abc123",
		Name:                "route1",
		StaticRouteNetwork:  "192.168.0.0/24",
		StaticRouteType:     "nexthop-route",
		StaticRouteDistance: &dist,
		StaticRouteNexthop:  "192.168.1.1",
		Enabled:             true,
		GatewayType:         "default",
	}
	model := &staticRouteKitModel{}
	if diags := staticRouteKitSpec().ToModel(context.Background(), routing, model, "default"); diags.HasError() {
		t.Fatalf("ToModel: %v", diags)
	}
	if model.ID.ValueString() != "abc123" {
		t.Errorf("ID = %q, want %q", model.ID.ValueString(), "abc123")
	}
	if model.Site.ValueString() != "default" {
		t.Errorf("Site = %q, want %q", model.Site.ValueString(), "default")
	}
	if model.NextHop.ValueString() != "192.168.1.1" {
		t.Errorf("NextHop = %q, want %q", model.NextHop.ValueString(), "192.168.1.1")
	}
}

// A controller that reports no gateway_type must still leave the model
// holding "default", or the attribute reads as changed on every refresh.
func Test_staticRouteFrameworkResource_gatewayTypeDefaultsOnAnEmptyRead(t *testing.T) {
	for _, testCase := range []struct{ reported, want string }{
		{"", "default"},
		{"upstream", "upstream"},
	} {
		model := &staticRouteKitModel{}
		routing := &unifi.Routing{GatewayType: testCase.reported}
		if diags := staticRouteKitSpec().ToModel(context.Background(), routing, model, "default"); diags.HasError() {
			t.Fatalf("ToModel: %v", diags)
		}
		if got := model.GatewayType.ValueString(); got != testCase.want {
			t.Errorf("a controller reporting %q left gateway_type %q, want %q",
				testCase.reported, got, testCase.want)
		}
	}
}

func Test_staticRouteFrameworkResource_ListResourceConfigSchema(t *testing.T) {
	r := newStaticRouteKitResource()
	resp := &fwlist.ListResourceSchemaResponse{}
	r.ListResourceConfigSchema(context.Background(), fwlist.ListResourceSchemaRequest{}, resp)
	if len(resp.Schema.Attributes) == 0 {
		t.Error("expected non-empty list resource schema")
	}
}

func TestAccStaticRouteList_basic(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		TerraformVersionChecks: []tfversion.TerraformVersionCheck{
			tfversion.SkipBelow(tfversion.Version1_14_0),
		},
		Steps: []resource.TestStep{
			{
				Config: testAccStaticRouteFrameworkConfig_basic(),
			},
			{
				Query: true,
				Config: `
					provider "unifi" {}
					list "unifi_static_route" "test" {
						provider = unifi
						config {
							filter {
								name  = "name"
								value = "test-route"
						  }
					  }
					}
				`,
				QueryResultChecks: []querycheck.QueryResultCheck{
					querycheck.ExpectLengthAtLeast("unifi_static_route.test", 1),
				},
			},
		},
	})
}
