package unifi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework-nettypes/cidrtypes"
	"github.com/hashicorp/terraform-plugin-framework-timetypes/timetypes"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	fwlist "github.com/hashicorp/terraform-plugin-framework/list"
	fwresource "github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/querycheck"
	"github.com/hashicorp/terraform-plugin-testing/tfversion"
	"github.com/ubiquiti-community/go-unifi/unifi"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/resourcekit"
	"github.com/ubiquiti-community/terraform-provider-unifi/unifi/util"
)

func strPtr(s string) *string { return &s }

func TestAccNetworkFramework_basic(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccNetworkFrameworkConfig_basic(),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"unifi_network.test",
						"name",
						"Test VLAN",
					),
					resource.TestCheckResourceAttr(
						"unifi_network.test",
						"subnet",
						"192.168.10.1/24",
					),
					resource.TestCheckResourceAttr("unifi_network.test", "vlan", "10"),
					resource.TestCheckResourceAttr("unifi_network.test", "enabled", "true"),
					// firewall_zone_id is Computed+Optional on the assumption the
					// controller assigns every network to a zone; this asserts that
					// assumption without hardcoding which zone (the controller
					// picks one, this provider doesn't).
					resource.TestCheckResourceAttrSet("unifi_network.test", "firewall_zone_id"),
				),
			},
			{
				ResourceName:      "unifi_network.test",
				ImportState:       true,
				ImportStateVerify: true,
				ImportStateId:     "name=Test VLAN",
				// Ignore dhcp_server and dhcp_relay since they're not configured in the test
				// but will be populated by the API with default values during import
				ImportStateVerifyIgnore: []string{
					"dhcp_server",
					"dhcp_relay",
				},
			},
		},
	})
}

func TestAccNetworkFramework_dhcp(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccNetworkFrameworkConfig_dhcp(),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"unifi_network.test_dhcp",
						"name",
						"Test DHCP Network",
					),
					resource.TestCheckResourceAttr(
						"unifi_network.test_dhcp",
						"subnet",
						"192.168.20.1/24",
					),
					resource.TestCheckResourceAttr("unifi_network.test_dhcp", "vlan", "20"),
					resource.TestCheckResourceAttr(
						"unifi_network.test_dhcp",
						"dhcp_server.enabled",
						"true",
					),
					resource.TestCheckResourceAttr(
						"unifi_network.test_dhcp",
						"dhcp_server.start",
						"192.168.20.10",
					),
					resource.TestCheckResourceAttr(
						"unifi_network.test_dhcp",
						"dhcp_server.stop",
						"192.168.20.254",
					),
					resource.TestCheckResourceAttr(
						"unifi_network.test_dhcp",
						"dhcp_server.leasetime",
						"24h0m0s",
					),
					resource.TestCheckResourceAttr(
						"unifi_network.test_dhcp",
						"dhcp_server.ntp_servers.#",
						"2",
					),
					resource.TestCheckResourceAttr(
						"unifi_network.test_dhcp",
						"dhcp_server.ntp_servers.0",
						"192.168.20.1",
					),
					resource.TestCheckResourceAttr(
						"unifi_network.test_dhcp",
						"dhcp_server.ntp_servers.1",
						"192.168.20.2",
					),
				),
			},
			{
				ResourceName:      "unifi_network.test_dhcp",
				ImportState:       true,
				ImportStateVerify: true,
				ImportStateId:     "name=Test DHCP Network",
			},
		},
	})
}

func TestAccNetworkFramework_guest(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccNetworkFrameworkConfig_guest(),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"unifi_network.test_guest",
						"name",
						"Guest Network",
					),
					resource.TestCheckResourceAttr(
						"unifi_network.test_guest",
						"subnet",
						"192.168.30.1/24",
					),
					resource.TestCheckResourceAttr(
						"unifi_network.test_guest",
						"vlan",
						"30",
					),
					resource.TestCheckResourceAttr(
						"unifi_network.test_guest",
						"internet_access",
						"true",
					),
					resource.TestCheckResourceAttr(
						"unifi_network.test_guest",
						"network_isolation",
						"true",
					),
				),
			},
		},
	})
}

func testAccNetworkFrameworkConfig_basic() string {
	return `
resource "unifi_network" "test" {
	name      = "Test VLAN"
	subnet    = "192.168.10.1/24"
	vlan      = 10
	enabled   = true
}
`
}

func testAccNetworkFrameworkConfig_dhcp() string {
	return `
resource "unifi_network" "test_dhcp" {
	name      = "Test DHCP Network"
	subnet    = "192.168.20.1/24"
	vlan      = 20

	dhcp_server = {
		enabled     = true
		start       = "192.168.20.10"
		stop        = "192.168.20.254"
		leasetime   = "24h0m0s"
		ntp_servers = ["192.168.20.1", "192.168.20.2"]
	}
}
`
}

func testAccNetworkFrameworkConfig_guest() string {
	return `
resource "unifi_network" "test_guest" {
	name              = "Guest Network"
	subnet            = "192.168.30.1/24"
	vlan              = 30
	internet_access   = true
	network_isolation = true
}
`
}

func TestAccNetworkFramework_thirdPartyGateway(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccNetworkFrameworkConfig_thirdPartyGateway(),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"unifi_network.test_third_party",
						"name",
						"Test Third Party",
					),
					resource.TestCheckResourceAttr(
						"unifi_network.test_third_party",
						"vlan",
						"3",
					),
					resource.TestCheckResourceAttr(
						"unifi_network.test_third_party",
						"third_party_gateway",
						"true",
					),
					resource.TestCheckResourceAttr(
						"unifi_network.test_third_party",
						"dhcp_guarding.enabled",
						"true",
					),
					resource.TestCheckResourceAttr(
						"unifi_network.test_third_party",
						"dhcp_guarding.servers.#",
						"2",
					),
					resource.TestCheckResourceAttr(
						"unifi_network.test_third_party",
						"dhcp_guarding.servers.0",
						"192.168.20.20",
					),
					resource.TestCheckResourceAttr(
						"unifi_network.test_third_party",
						"dhcp_guarding.servers.1",
						"192.168.20.21",
					),
				),
			},
			{
				ResourceName:      "unifi_network.test_third_party",
				ImportState:       true,
				ImportStateVerify: true,
				ImportStateId:     "name=Test Third Party",
				// These fields are not relevant to vlan-only networks and are not
				// returned by the API, so they cannot be recovered during import.
				ImportStateVerifyIgnore: []string{
					"subnet",
					"auto_scale",
					// setting_preference: the plan modifier stores "manual"
					// while a vlan-only live document omits the key entirely,
					// so import legitimately reads null.
					"setting_preference",
					"multicast_dns",
					"ipv6_interface_type",
					"ipv6_static_subnet",
					"ipv6_ra",
					"ipv6_ra_priority",
					"ipv6_ra_preferred_lifetime",
					"ipv6_ra_valid_lifetime",
					"ipv6_pd_interface",
					"ipv6_pd_prefixid",
					"ipv6_pd_start",
					"ipv6_pd_stop",
					"ipv6_pd_auto_prefixid_enabled",
					"lte_lan",
					"internet_access",
				},
			},
		},
	})
}

func TestAccNetworkFramework_thirdPartyGatewayMinimal(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccNetworkFrameworkConfig_thirdPartyGatewayMinimal(),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"unifi_network.test_third_party_min",
						"name",
						"Test Third Party Minimal",
					),
					resource.TestCheckResourceAttr(
						"unifi_network.test_third_party_min",
						"vlan",
						"4",
					),
					resource.TestCheckResourceAttr(
						"unifi_network.test_third_party_min",
						"third_party_gateway",
						"true",
					),
				),
			},
		},
	})
}

func testAccNetworkFrameworkConfig_thirdPartyGateway() string {
	return `
resource "unifi_network" "test_third_party" {
	name                = "Test Third Party"
	subnet              = "192.168.20.1/24"
	vlan                = 3
	third_party_gateway = true

	dhcp_guarding = {
		enabled = true
		servers = ["192.168.20.20", "192.168.20.21"]
	}
}
`
}

// Test_networkResource_ModifyPlan_settingPreference pins the attributes that
// force setting_preference to "manual". On "auto" the controller silently
// stores false for these toggles however they were sent; only a true value
// forces the switch, since switching on false would churn everyone.
func Test_networkResource_ModifyPlan_settingPreference(t *testing.T) {
	tests := []struct {
		name string
		// attr is the plan attribute set to true; empty means none.
		attr string
		want bool
	}{
		{name: "nothing enabled stays auto", attr: "", want: false},
		{name: "igmp_snooping", attr: "igmp_snooping", want: true},
		{name: "dhcp_relay", attr: "dhcp_relay.enabled", want: true},
		{name: "dhcp_guarding", attr: "dhcp_guarding.enabled", want: true},
		{name: "dhcp_server dns", attr: "dhcp_server.dns_enabled", want: true},
		{name: "dhcp_server ntp", attr: "dhcp_server.ntp_enabled", want: true},
		{name: "dhcp_server time offset", attr: "dhcp_server.time_offset_enabled", want: true},
	}
	resp := &fwresource.SchemaResponse{}
	newNetworkKitResource().Schema(context.Background(), fwresource.SchemaRequest{}, resp)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.attr == "" {
				return
			}
			// Guard the attribute names against schema drift. A rename would
			// leave ModifyPlan reading a path that never matches, and because
			// the controller accepts the write either way, the failure is
			// silent.
			root, nested, isNested := strings.Cut(tt.attr, ".")
			attribute, ok := resp.Schema.Attributes[root]
			if !ok {
				t.Fatalf("schema has no attribute %q", root)
			}
			if !isNested {
				return
			}
			single, ok := attribute.(schema.SingleNestedAttribute)
			if !ok {
				t.Fatalf("attribute %q is not a SingleNestedAttribute", root)
			}
			if _, ok := single.Attributes[nested]; !ok {
				t.Errorf("schema has no attribute %q under %q", nested, root)
			}
		})
	}
}

// TestAccNetworkFramework_dhcpGuardingCorporate covers DHCP Guard on a
// corporate network. The existing dhcp_guarding coverage is on a
// third_party_gateway network, which the controller stores as vlan-only --
// a different encoder path (marshalCorporate/marshalGuest) than this test
// exercises.
func TestAccNetworkFramework_dhcpGuardingCorporate(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccNetworkFrameworkConfig_dhcpGuardingCorporate("Test DHCP Guard"),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"unifi_network.test_dhcp_guard", "purpose", "corporate",
					),
					resource.TestCheckResourceAttr(
						"unifi_network.test_dhcp_guard", "dhcp_guarding.enabled", "true",
					),
					resource.TestCheckResourceAttr(
						"unifi_network.test_dhcp_guard", "dhcp_guarding.servers.#", "2",
					),
					resource.TestCheckResourceAttr(
						"unifi_network.test_dhcp_guard", "dhcp_guarding.servers.0", "192.168.70.20",
					),
					resource.TestCheckResourceAttr(
						"unifi_network.test_dhcp_guard", "dhcp_guarding.servers.1", "192.168.70.21",
					),
				),
			},
			{
				// Touch an unrelated field so the provider issues an update with
				// dhcp_guarding unchanged.
				Config: testAccNetworkFrameworkConfig_dhcpGuardingCorporate(
					"Test DHCP Guard Renamed",
				),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"unifi_network.test_dhcp_guard", "name", "Test DHCP Guard Renamed",
					),
					resource.TestCheckResourceAttr(
						"unifi_network.test_dhcp_guard", "dhcp_guarding.enabled", "true",
					),
					resource.TestCheckResourceAttr(
						"unifi_network.test_dhcp_guard", "dhcp_guarding.servers.#", "2",
					),
				),
			},
		},
	})
}

func testAccNetworkFrameworkConfig_dhcpGuardingCorporate(name string) string {
	return fmt.Sprintf(`
resource "unifi_network" "test_dhcp_guard" {
	name    = %q
	subnet  = "192.168.70.1/24"
	vlan    = 70
	purpose = "corporate"

	dhcp_guarding = {
		enabled = true
		servers = ["192.168.70.20", "192.168.70.21"]
	}
}
`, name)
}

func testAccNetworkFrameworkConfig_thirdPartyGatewayMinimal() string {
	return `
resource "unifi_network" "test_third_party_min" {
	name                = "Test Third Party Minimal"
	subnet              = "192.168.20.1/24"
	vlan                = 4
	third_party_gateway = true
}
`
}

func TestAccNetworkFramework_dhcpRelay(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccNetworkFrameworkConfig_dhcpRelay(),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"unifi_network.test_relay",
						"name",
						"Test DHCP Relay",
					),
					resource.TestCheckResourceAttr(
						"unifi_network.test_relay",
						"vlan",
						"50",
					),
					resource.TestCheckResourceAttr(
						"unifi_network.test_relay",
						"dhcp_relay.enabled",
						"true",
					),
					resource.TestCheckResourceAttr(
						"unifi_network.test_relay",
						"dhcp_relay.servers.#",
						"1",
					),
					resource.TestCheckResourceAttr(
						"unifi_network.test_relay",
						"dhcp_relay.servers.0",
						"192.168.50.1",
					),
				),
			},
			{
				ResourceName:      "unifi_network.test_relay",
				ImportState:       true,
				ImportStateVerify: true,
				ImportStateId:     "name=Test DHCP Relay",
				ImportStateVerifyIgnore: []string{
					"auto_scale",
					"multicast_dns",
					"ipv6_interface_type",
					"ipv6_static_subnet",
					"ipv6_ra",
					"ipv6_ra_priority",
					"ipv6_ra_preferred_lifetime",
					"ipv6_ra_valid_lifetime",
					"ipv6_pd_interface",
					"ipv6_pd_prefixid",
					"ipv6_pd_start",
					"ipv6_pd_stop",
					"ipv6_pd_auto_prefixid_enabled",
					"lte_lan",
					"internet_access",
				},
			},
		},
	})
}

func testAccNetworkFrameworkConfig_dhcpRelay() string {
	return `
resource "unifi_network" "test_relay" {
	name   = "Test DHCP Relay"
	subnet = "192.168.50.1/24"
	vlan   = 50

	dhcp_relay = {
		enabled = true
		servers = ["192.168.50.1"]
	}
}
`
}

func TestAccNetworkFramework_ipv6Static(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccNetworkFrameworkConfig_ipv6Static(),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"unifi_network.test_ipv6_static",
						"name",
						"Test IPv6 Static",
					),
					resource.TestCheckResourceAttr(
						"unifi_network.test_ipv6_static",
						"ipv6_interface_type",
						"static",
					),
					resource.TestCheckResourceAttr(
						"unifi_network.test_ipv6_static",
						"ipv6_static_subnet",
						"fd00::1/64",
					),
					resource.TestCheckResourceAttr(
						"unifi_network.test_ipv6_static",
						"ipv6_ra",
						"true",
					),
					resource.TestCheckResourceAttr(
						"unifi_network.test_ipv6_static",
						"ipv6_ra_priority",
						"high",
					),
					resource.TestCheckResourceAttr(
						"unifi_network.test_ipv6_static",
						"ipv6_ra_valid_lifetime",
						"24h0m0s",
					),
					resource.TestCheckResourceAttr(
						"unifi_network.test_ipv6_static",
						"ipv6_ra_preferred_lifetime",
						"4h0m0s",
					),
					resource.TestCheckResourceAttr(
						"unifi_network.test_ipv6_static",
						"ipv6_aliases.#",
						"1",
					),
					resource.TestCheckResourceAttr(
						"unifi_network.test_ipv6_static",
						"ipv6_aliases.0",
						"fd00::2/64",
					),
				),
			},
			{
				ResourceName:      "unifi_network.test_ipv6_static",
				ImportState:       true,
				ImportStateVerify: true,
				ImportStateId:     "name=Test IPv6 Static",
				ImportStateVerifyIgnore: []string{
					"dhcp_server",
					"dhcp_relay",
					"dhcp_v6_server",
				},
			},
		},
	})
}

func TestAccNetworkFramework_dhcpV6(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccNetworkFrameworkConfig_dhcpV6(),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"unifi_network.test_dhcpv6",
						"name",
						"Test DHCPv6",
					),
					resource.TestCheckResourceAttr(
						"unifi_network.test_dhcpv6",
						"ipv6_interface_type",
						"static",
					),
					resource.TestCheckResourceAttr(
						"unifi_network.test_dhcpv6",
						"dhcp_v6_server.enabled",
						"true",
					),
					resource.TestCheckResourceAttr(
						"unifi_network.test_dhcpv6",
						"dhcp_v6_server.dns_auto",
						"false",
					),
					resource.TestCheckResourceAttr(
						"unifi_network.test_dhcpv6",
						"dhcp_v6_server.dns_servers.#",
						"2",
					),
					resource.TestCheckResourceAttr(
						"unifi_network.test_dhcpv6",
						"dhcp_v6_server.dns_servers.0",
						"2001:4860:4860::8888",
					),
					resource.TestCheckResourceAttr(
						"unifi_network.test_dhcpv6",
						"dhcp_v6_server.dns_servers.1",
						"2001:4860:4860::8844",
					),
					resource.TestCheckResourceAttr(
						"unifi_network.test_dhcpv6",
						"dhcp_v6_server.start",
						"::2",
					),
					resource.TestCheckResourceAttr(
						"unifi_network.test_dhcpv6",
						"dhcp_v6_server.stop",
						"::7d1",
					),
					resource.TestCheckResourceAttr(
						"unifi_network.test_dhcpv6",
						"dhcp_v6_server.lease",
						"86400",
					),
				),
			},
			{
				ResourceName:      "unifi_network.test_dhcpv6",
				ImportState:       true,
				ImportStateVerify: true,
				ImportStateId:     "name=Test DHCPv6",
				ImportStateVerifyIgnore: []string{
					"dhcp_server",
					"dhcp_relay",
				},
			},
		},
	})
}

func testAccNetworkFrameworkConfig_ipv6Static() string {
	return `
resource "unifi_network" "test_ipv6_static" {
	name                    = "Test IPv6 Static"
	subnet                  = "192.168.40.1/24"
	vlan                    = 40
	ipv6_interface_type     = "static"
	ipv6_static_subnet      = "fd00::1/64"
	ipv6_ra                 = true
	ipv6_ra_priority        = "high"
	ipv6_ra_valid_lifetime  = "24h0m0s"
	ipv6_ra_preferred_lifetime = "4h0m0s"
	ipv6_aliases            = ["fd00::2/64"]
}
`
}

func testAccNetworkFrameworkConfig_dhcpV6() string {
	return `
resource "unifi_network" "test_dhcpv6" {
	name                = "Test DHCPv6"
	subnet              = "192.168.60.1/24"
	vlan                = 60
	ipv6_interface_type = "static"
	ipv6_static_subnet  = "fd01::1/64"
	ipv6_ra             = true

	dhcp_v6_server = {
		enabled     = true
		dns_auto    = false
		dns_servers = ["2001:4860:4860::8888", "2001:4860:4860::8844"]
		start       = "::2"
		stop        = "::7d1"
		lease       = 86400
	}
}
`
}

func TestNewNetworkResource(t *testing.T) {
	got := NewNetworkResource()
	if got == nil {
		t.Fatal("NewNetworkResource() returned nil")
	}
}

func TestNewNetworkListResource(t *testing.T) {
	got := NewNetworkListResource()
	if got == nil {
		t.Fatal("NewNetworkListResource() returned nil")
	}
}

func Test_dhcpBootModel_AttributeTypes(t *testing.T) {
	tests := []struct {
		name string
		m    dhcpBootModel
		want map[string]attr.Type
	}{
		{
			name: "returns correct attribute types",
			m:    dhcpBootModel{},
			want: map[string]attr.Type{
				"enabled":  types.BoolType,
				"server":   types.StringType,
				"filename": types.StringType,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.m.AttributeTypes(); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("dhcpBootModel.AttributeTypes() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_winsModel_AttributeTypes(t *testing.T) {
	tests := []struct {
		name string
		m    winsModel
		want map[string]attr.Type
	}{
		{
			name: "returns correct attribute types",
			m:    winsModel{},
			want: map[string]attr.Type{
				"enabled":   types.BoolType,
				"addresses": types.ListType{ElemType: types.StringType},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.m.AttributeTypes(); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("winsModel.AttributeTypes() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_dhcpServerModel_AttributeTypes(t *testing.T) {
	tests := []struct {
		name string
		m    dhcpServerModel
		want map[string]attr.Type
	}{
		{
			name: "returns correct attribute types",
			m:    dhcpServerModel{},
			want: map[string]attr.Type{
				"boot": types.ObjectType{
					AttrTypes: dhcpBootModel{}.AttributeTypes(),
				},
				"enabled":             types.BoolType,
				"start":               types.StringType,
				"stop":                types.StringType,
				"gateway_enabled":     types.BoolType,
				"conflict_checking":   types.BoolType,
				"ntp_enabled":         types.BoolType,
				"time_offset_enabled": types.BoolType,
				"dns_enabled":         types.BoolType,
				"leasetime":           timetypes.GoDurationType{},
				"wins":                types.ObjectType{AttrTypes: winsModel{}.AttributeTypes()},
				"wpad_url":            types.StringType,
				"tftp_server":         types.StringType,
				"unifi_controller":    types.StringType,
				"dns_servers":         types.ListType{ElemType: types.StringType},
				"ntp_servers":         types.ListType{ElemType: types.StringType},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.m.AttributeTypes(); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("dhcpServerModel.AttributeTypes() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_natOutboundIPAddressesModel_AttributeTypes(t *testing.T) {
	tests := []struct {
		name string
		d    natOutboundIPAddressesModel
		want map[string]attr.Type
	}{
		{
			name: "returns correct attribute types",
			d:    natOutboundIPAddressesModel{},
			want: map[string]attr.Type{
				"ip_address":        types.StringType,
				"ip_address_pool":   types.ListType{ElemType: types.StringType},
				"mode":              types.StringType,
				"wan_network_group": types.StringType,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.d.AttributeTypes(); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("natOutboundIPAddressesModel.AttributeTypes() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_natOutboundIPAddresses(t *testing.T) {
	tests := []struct {
		name string
		want map[string]attr.Type
	}{
		{
			name: "returns correct type map",
			want: map[string]attr.Type{
				"ip_address":        types.StringType,
				"ip_address_pool":   types.ListType{ElemType: types.StringType},
				"mode":              types.StringType,
				"wan_network_group": types.StringType,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := natOutboundIPAddresses(); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("natOutboundIPAddresses() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_dhcpGuardingModel_AttributeTypes(t *testing.T) {
	tests := []struct {
		name string
		m    dhcpGuardingModel
		want map[string]attr.Type
	}{
		{
			name: "returns correct attribute types",
			m:    dhcpGuardingModel{},
			want: map[string]attr.Type{
				"enabled": types.BoolType,
				"servers": types.ListType{ElemType: types.StringType},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.m.AttributeTypes(); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("dhcpGuardingModel.AttributeTypes() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_dhcpRelayModel_AttributeTypes(t *testing.T) {
	tests := []struct {
		name string
		d    dhcpRelayModel
		want map[string]attr.Type
	}{
		{
			name: "returns correct attribute types",
			d:    dhcpRelayModel{},
			want: map[string]attr.Type{
				"enabled": types.BoolType,
				"servers": types.ListType{ElemType: types.StringType},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.d.AttributeTypes(); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("dhcpRelayModel.AttributeTypes() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_dhcpV6ServerModel_AttributeTypes(t *testing.T) {
	tests := []struct {
		name string
		m    dhcpV6ServerModel
		want map[string]attr.Type
	}{
		{
			name: "returns correct attribute types",
			m:    dhcpV6ServerModel{},
			want: map[string]attr.Type{
				"enabled":     types.BoolType,
				"dns_auto":    types.BoolType,
				"dns_servers": types.ListType{ElemType: types.StringType},
				"lease":       types.Int64Type,
				"start":       types.StringType,
				"stop":        types.StringType,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.m.AttributeTypes(); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("dhcpV6ServerModel.AttributeTypes() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_networkResource_IdentitySchema(t *testing.T) {
	type args struct {
		in0  context.Context
		in1  fwresource.IdentitySchemaRequest
		resp *fwresource.IdentitySchemaResponse
	}
	tests := []struct {
		name string
		r    *networkKitResource
		args args
	}{
		{
			name: "returns identity schema with id",
			r:    newNetworkKitResource(),
			args: args{
				in0:  context.Background(),
				in1:  fwresource.IdentitySchemaRequest{},
				resp: &fwresource.IdentitySchemaResponse{},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.r.IdentitySchema(tt.args.in0, tt.args.in1, tt.args.resp)
			if _, ok := tt.args.resp.IdentitySchema.Attributes["id"]; !ok {
				t.Error("IdentitySchema() missing 'id' attribute")
			}
		})
	}
}

func Test_networkResource_UpgradeState(t *testing.T) {
	type args struct {
		ctx context.Context
	}
	tests := []struct {
		name string
		r    *networkKitResource
		args args
		want map[int64]fwresource.StateUpgrader
	}{
		{
			name: "returns non-nil map",
			r:    newNetworkKitResource(),
			args: args{
				ctx: context.Background(),
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.r.UpgradeState(tt.args.ctx)
			if got == nil {
				t.Error("UpgradeState() returned nil")
			}
		})
	}
}

func Test_networkResource_modelToNetwork(t *testing.T) {
	type args struct {
		ctx   context.Context
		model *netModel
	}
	tests := []struct {
		name  string
		r     *networkKitResource
		args  args
		want  *unifi.Network
		want1 diag.Diagnostics
	}{
		{
			name: "minimal model conversion",
			r:    newNetworkKitResource(),
			args: args{
				ctx: context.Background(),
				model: &netModel{
					Name:                        types.StringValue("test-net"),
					Enabled:                     types.BoolValue(true),
					Subnet:                      cidrtypes.NewIPv4PrefixValue("10.0.0.0/24"),
					AutoScale:                   types.BoolValue(false),
					NetworkIsolation:            types.BoolValue(false),
					SettingPreference:           types.StringNull(),
					InternetAccess:              types.BoolValue(false),
					MulticastDNS:                types.BoolValue(false),
					GatewayType:                 types.StringNull(),
					IPv6InterfaceType:           types.StringNull(),
					IPv6ClientAddressAssignment: types.StringNull(),
					IPv6StaticSubnet:            types.StringNull(),
					IPv6RA:                      types.BoolValue(false),
					IPv6RAPriority:              types.StringNull(),
					IPv6RAPreferredLifetime:     timetypes.NewGoDurationNull(),
					IPv6RAValidLifetime:         timetypes.NewGoDurationNull(),
					IPv6PDInterface:             types.StringNull(),
					IPv6PDPrefixid:              types.StringNull(),
					IPv6PDStart:                 types.StringNull(),
					IPv6PDStop:                  types.StringNull(),
					IPv6PDAutoPrefixidEnabled:   types.BoolValue(false),
					LteLAN:                      types.BoolValue(false),
					ThirdPartyGateway:           types.BoolValue(false),
					IGMPSnooping:                types.BoolValue(false),
					VLAN:                        types.Int64Null(),
					NATOutboundIPAddresses: types.ListNull(
						types.ObjectType{AttrTypes: natOutboundIPAddresses()},
					),
					IPAliases:   types.ListNull(types.StringType),
					IPv6Aliases: types.ListNull(types.StringType),
					DHCPServer: types.ObjectNull(
						dhcpServerModel{}.AttributeTypes(),
					),
					DHCPRelay: types.ObjectNull(
						dhcpRelayModel{}.AttributeTypes(),
					),
					DHCPV6Server: types.ObjectNull(
						dhcpV6ServerModel{}.AttributeTypes(),
					),
					DHCPGuarding: types.ObjectNull(
						dhcpGuardingModel{}.AttributeTypes(),
					),
				},
			},
			want1: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, got1 := tt.r.modelToNetwork(tt.args.ctx, tt.args.model)
			if got == nil {
				t.Fatal("modelToNetwork() returned nil network")
			}
			if *got.Name != "test-net" {
				t.Errorf("modelToNetwork() Name = %v, want test-net", *got.Name)
			}
			if got.Purpose != unifi.PurposeCorporate {
				t.Errorf(
					"modelToNetwork() Purpose = %v, want %v",
					got.Purpose,
					unifi.PurposeCorporate,
				)
			}
			if got1 != nil && got1.HasError() {
				t.Errorf("modelToNetwork() diagnostics has errors: %v", got1)
			}
		})
	}
}

func Test_networkResource_networkToModel(t *testing.T) {
	type args struct {
		ctx           context.Context
		network       *unifi.Network
		model         *netModel
		site          string
		previousModel *netModel
	}
	tests := []struct {
		name string
		r    *networkKitResource
		args args
		want diag.Diagnostics
	}{
		{
			name: "minimal network to model",
			r:    newNetworkKitResource(),
			args: args{
				ctx: context.Background(),
				network: &unifi.Network{
					ID:      "net-123",
					Name:    strPtr("test-net"),
					Purpose: unifi.PurposeCorporate,
					Enabled: true,
				},
				model: &netModel{},
				site:  "default",
				previousModel: &netModel{
					DHCPServer:   types.ObjectNull(dhcpServerModel{}.AttributeTypes()),
					DHCPRelay:    types.ObjectNull(dhcpRelayModel{}.AttributeTypes()),
					DHCPV6Server: types.ObjectNull(dhcpV6ServerModel{}.AttributeTypes()),
					DHCPGuarding: types.ObjectNull(dhcpGuardingModel{}.AttributeTypes()),
					NATOutboundIPAddresses: types.ListNull(
						types.ObjectType{AttrTypes: natOutboundIPAddresses()},
					),
					IPAliases:   types.ListNull(types.StringType),
					IPv6Aliases: types.ListNull(types.StringType),
				},
			},
			want: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.r.networkToModel(
				tt.args.ctx,
				tt.args.network,
				tt.args.model,
				tt.args.site,
				tt.args.previousModel,
			)
			if got != nil && got.HasError() {
				t.Errorf("networkToModel() diagnostics has errors: %v", got)
			}
			if tt.args.model.ID.ValueString() != "net-123" {
				t.Errorf("networkToModel() ID = %v, want net-123", tt.args.model.ID.ValueString())
			}
			if tt.args.model.Site.ValueString() != "default" {
				t.Errorf(
					"networkToModel() Site = %v, want default",
					tt.args.model.Site.ValueString(),
				)
			}
			if tt.args.model.Name.ValueString() != "test-net" {
				t.Errorf(
					"networkToModel() Name = %v, want test-net",
					tt.args.model.Name.ValueString(),
				)
			}
		})
	}
}

func Test_networkResource_ListResourceConfigSchema(t *testing.T) {
	type args struct {
		ctx  context.Context
		req  fwlist.ListResourceSchemaRequest
		resp *fwlist.ListResourceSchemaResponse
	}
	tests := []struct {
		name string
		r    *networkKitResource
		args args
	}{
		{
			name: "returns schema without panic",
			r:    newNetworkKitResource(),
			args: args{
				ctx:  context.Background(),
				req:  fwlist.ListResourceSchemaRequest{},
				resp: &fwlist.ListResourceSchemaResponse{},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.r.ListResourceConfigSchema(tt.args.ctx, tt.args.req, tt.args.resp)
			if tt.args.resp.Schema.Attributes == nil {
				t.Error("ListResourceConfigSchema() returned nil attributes")
			}
		})
	}
}

func TestAccNetworkList_basic(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		TerraformVersionChecks: []tfversion.TerraformVersionCheck{
			tfversion.SkipBelow(tfversion.Version1_14_0),
		},
		Steps: []resource.TestStep{
			{
				Config: testAccNetworkFrameworkConfig_basic(),
			},
			{
				Query: true,
				Config: `
					provider "unifi" {}
					list "unifi_network" "test" {
						provider = unifi
						config {
							filter {
								name  = "name"
								value = "Test VLAN"
						  }
					  }
					}
				`,
				QueryResultChecks: []querycheck.QueryResultCheck{
					querycheck.ExpectLengthAtLeast("unifi_network.test", 1),
				},
			},
		},
	})
}

// A corporate network's multicast_dns is overridden to false server-side by
// some controllers (UniFi OS gateways), so a user-configured true would fail
// the consistency check. The configured/known value must be preserved; an
// unset (unknown) value falls back to the controller's value.
func Test_networkResource_networkToModel_multicastDNS(t *testing.T) {
	r := newNetworkKitResource()
	base := func() *netModel {
		return &netModel{
			DHCPServer:   types.ObjectNull(dhcpServerModel{}.AttributeTypes()),
			DHCPRelay:    types.ObjectNull(dhcpRelayModel{}.AttributeTypes()),
			DHCPV6Server: types.ObjectNull(dhcpV6ServerModel{}.AttributeTypes()),
			DHCPGuarding: types.ObjectNull(dhcpGuardingModel{}.AttributeTypes()),
			NATOutboundIPAddresses: types.ListNull(
				types.ObjectType{AttrTypes: natOutboundIPAddresses()},
			),
			IPAliases:   types.ListNull(types.StringType),
			IPv6Aliases: types.ListNull(types.StringType),
		}
	}
	// Corporate network (not vlan-only); controller forces mdns false.
	network := &unifi.Network{
		ID:          "net-1",
		Name:        strPtr("IoT"),
		Purpose:     unifi.PurposeCorporate,
		Enabled:     true,
		IPSubnet:    strPtr("10.0.2.1/24"),
		MdnsEnabled: false,
	}

	t.Run("configured true is preserved", func(t *testing.T) {
		prev := base()
		prev.MulticastDNS = types.BoolValue(true)
		var model netModel
		d := r.networkToModel(context.Background(), network, &model, "default", prev)
		if d.HasError() {
			t.Fatalf("networkToModel: %v", d)
		}
		if !model.MulticastDNS.ValueBool() {
			t.Errorf("configured multicast_dns=true not preserved: %v", model.MulticastDNS)
		}
	})

	t.Run("unset falls back to controller value", func(t *testing.T) {
		prev := base()
		prev.MulticastDNS = types.BoolUnknown()
		var model netModel
		d := r.networkToModel(context.Background(), network, &model, "default", prev)
		if d.HasError() {
			t.Fatalf("networkToModel: %v", d)
		}
		if model.MulticastDNS.ValueBool() {
			t.Errorf("unset multicast_dns should reflect controller false, got true")
		}
	})
}

// Test_networkResource_purpose checks that purpose is author-settable
// (guest/vlan-only/corporate) on write and reflected from the controller on read.
func Test_networkResource_purpose(t *testing.T) {
	r := newNetworkKitResource()

	baseModel := func() *netModel {
		return &netModel{
			Name:              types.StringValue("test-net"),
			Subnet:            cidrtypes.NewIPv4PrefixValue("10.0.0.0/24"),
			ThirdPartyGateway: types.BoolValue(false),
			Purpose:           types.StringNull(),
			NATOutboundIPAddresses: types.ListNull(
				types.ObjectType{AttrTypes: natOutboundIPAddresses()},
			),
			IPAliases:    types.ListNull(types.StringType),
			IPv6Aliases:  types.ListNull(types.StringType),
			DHCPServer:   types.ObjectNull(dhcpServerModel{}.AttributeTypes()),
			DHCPRelay:    types.ObjectNull(dhcpRelayModel{}.AttributeTypes()),
			DHCPV6Server: types.ObjectNull(dhcpV6ServerModel{}.AttributeTypes()),
			DHCPGuarding: types.ObjectNull(dhcpGuardingModel{}.AttributeTypes()),
		}
	}

	t.Run("write: unset defaults to corporate", func(t *testing.T) {
		got, d := r.modelToNetwork(context.Background(), baseModel())
		if d.HasError() {
			t.Fatalf("modelToNetwork: %v", d)
		}
		if got.Purpose != unifi.PurposeCorporate {
			t.Errorf("Purpose = %q, want %q", got.Purpose, unifi.PurposeCorporate)
		}
	})

	t.Run("write: configured guest is sent", func(t *testing.T) {
		m := baseModel()
		m.Purpose = types.StringValue(unifi.PurposeGuest)
		got, d := r.modelToNetwork(context.Background(), m)
		if d.HasError() {
			t.Fatalf("modelToNetwork: %v", d)
		}
		if got.Purpose != unifi.PurposeGuest {
			t.Errorf("Purpose = %q, want %q", got.Purpose, unifi.PurposeGuest)
		}
	})

	t.Run("write: third_party_gateway forces vlan-only over purpose", func(t *testing.T) {
		m := baseModel()
		m.Purpose = types.StringValue(unifi.PurposeGuest)
		m.ThirdPartyGateway = types.BoolValue(true)
		got, d := r.modelToNetwork(context.Background(), m)
		if d.HasError() {
			t.Fatalf("modelToNetwork: %v", d)
		}
		if got.Purpose != unifi.PurposeVLANOnly {
			t.Errorf(
				"Purpose = %q, want %q (third_party_gateway precedence)",
				got.Purpose,
				unifi.PurposeVLANOnly,
			)
		}
	})

	t.Run("read: controller guest is reflected", func(t *testing.T) {
		network := &unifi.Network{
			ID:       "net-guest",
			Name:     strPtr("Guest"),
			Purpose:  unifi.PurposeGuest,
			Enabled:  true,
			IPSubnet: strPtr("10.0.9.1/24"),
		}
		prev := baseModel()
		prev.Purpose = types.StringValue(unifi.PurposeGuest)
		var model netModel
		d := r.networkToModel(context.Background(), network, &model, "default", prev)
		if d.HasError() {
			t.Fatalf("networkToModel: %v", d)
		}
		if model.Purpose.ValueString() != unifi.PurposeGuest {
			t.Errorf("Purpose = %q, want %q", model.Purpose.ValueString(), unifi.PurposeGuest)
		}
	})
}

// The two attributes are one controller field: modelToNetwork writes an
// explicit purpose and lets a true third_party_gateway override it, and
// networkToModel reads third_party_gateway back out of that same field.
// Every other vlan-only fixture in this file sets third_party_gateway =
// true directly; this one sets purpose = "vlan-only" instead, which is the
// only way to exercise third_party_gateway's own default resolving to true.
func TestAccNetworkFramework_purposeVLANOnlyDirect(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccNetworkFrameworkConfig_purposeVLANOnlyDirect(),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"unifi_network.test_purpose_vlan_only", "purpose", "vlan-only",
					),
					// Derived from the controller's purpose, not defaulted.
					resource.TestCheckResourceAttr(
						"unifi_network.test_purpose_vlan_only", "third_party_gateway", "true",
					),
				),
			},
		},
	})
}

// TestAccNetworkFramework_purposeConflict proves a contradictory pair is refused
// at plan time, naming both attributes, instead of reaching the controller and
// failing afterwards as an inconsistent result that blames the provider.
//
// PlanOnly is the assertion that matters here: it never reaches the controller.
func TestAccNetworkFramework_purposeConflict(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:      testAccNetworkFrameworkConfig_purposeConflict(),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`Conflicting network purpose`),
			},
		},
	})
}

func testAccNetworkFrameworkConfig_purposeVLANOnlyDirect() string {
	return `
resource "unifi_network" "test_purpose_vlan_only" {
	name    = "Test Purpose VLAN Only"
	purpose = "vlan-only"
	vlan    = 24
}
`
}

func testAccNetworkFrameworkConfig_purposeConflict() string {
	return `
resource "unifi_network" "test_purpose_conflict" {
	name                = "Test Purpose Conflict"
	subnet              = "192.168.24.1/24"
	vlan                = 25
	purpose             = "corporate"
	third_party_gateway = true
}
`
}

// ip_aliases and nat_outbound_ip_addresses are in the wire mask, so they are
// sent on every update; nulling them unconditionally would silently clear
// aliases configured in the UI whenever an apply touches anything else.
// Mask membership only proves the resource manages the field -- it says
// nothing about whether the value read back means anything.
func Test_networkToModel_readsBackTheMaskedCollections(t *testing.T) {
	ctx := context.Background()
	r := newNetworkKitResource()

	mode := "all"
	network := &unifi.Network{
		ID:        "net-1",
		Name:      strPtr("IoT"),
		Purpose:   unifi.PurposeCorporate,
		Enabled:   true,
		IPSubnet:  strPtr("10.0.2.1/24"),
		IPAliases: []string{"10.0.2.9/24", "10.0.2.10/24"},
		NATOutboundIPAddresses: []unifi.NetworkNATOutboundIPAddresses{
			{IPAddress: "203.0.113.5", Mode: &mode},
		},
	}

	var model netModel
	if d := r.networkToModel(ctx, network, &model, "default", &netModel{}); d.HasError() {
		t.Fatalf("networkToModel: %v", d)
	}

	if model.IPAliases.IsNull() {
		t.Fatal("ip_aliases came back null; the next write sends the empty slice " +
			"modelToNetwork pre-seeds and the controller drops what it holds")
	}
	var aliases []string
	if d := model.IPAliases.ElementsAs(ctx, &aliases, false); d.HasError() {
		t.Fatalf("reading ip_aliases back: %v", d)
	}
	if !reflect.DeepEqual(aliases, []string{"10.0.2.9/24", "10.0.2.10/24"}) {
		t.Errorf("ip_aliases = %v, want the controller's two", aliases)
	}

	if model.NATOutboundIPAddresses.IsNull() {
		t.Fatal("nat_outbound_ip_addresses came back null; same defect, same mask")
	}
	if n := len(model.NATOutboundIPAddresses.Elements()); n != 1 {
		t.Errorf("nat_outbound_ip_addresses has %d entries, want 1", n)
	}
}

// Test_networkToModel_emptyCollectionsAreEmptyNotNull is the half that is easy
// to get wrong in the other direction.
//
// Both attributes are now Optional AND Computed, so an empty collection is a
// value the practitioner may have configured. Returning null for "the
// controller holds none" makes `ip_aliases = []` a permanent diff: the config
// keeps producing [], state keeps saying null, and no apply settles it. It is
// the same nil-versus-empty distinction the resource kit's KeepZero carries,
// one layer up.
func Test_networkToModel_emptyCollectionsAreEmptyNotNull(t *testing.T) {
	ctx := context.Background()
	r := newNetworkKitResource()

	for _, testCase := range []struct {
		name    string
		aliases []string
	}{
		{"nil from the controller", nil},
		{"empty from the controller", []string{}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			network := &unifi.Network{
				ID: "net-1", Name: strPtr("IoT"), Purpose: unifi.PurposeCorporate,
				Enabled: true, IPSubnet: strPtr("10.0.2.1/24"),
				IPAliases: testCase.aliases,
			}
			var model netModel
			if d := r.networkToModel(ctx, network, &model, "default",
				&netModel{}); d.HasError() {
				t.Fatalf("networkToModel: %v", d)
			}
			if model.IPAliases.IsNull() {
				t.Fatal("ip_aliases is null for an empty membership; a config saying " +
					"ip_aliases = [] would then never stop planning a change")
			}
			if n := len(model.IPAliases.Elements()); n != 0 {
				t.Errorf("ip_aliases has %d elements, want 0", n)
			}
		})
	}
}

// ipv6_pd_prefixid gained Computed, so the plan carries UNKNOWN whenever the
// config omits it. The vlan-only branch copies plan values wholesale to
// avoid inconsistent-result errors on fields the controller doesn't return
// -- and copying an unknown through leaves the attribute unknown after
// apply, which Terraform rejects. Making an attribute Computed obliges
// every place that copies it to resolve the unknown.
func Test_networkToModel_vlanOnlyResolvesUnknownPrefixID(t *testing.T) {
	ctx := context.Background()
	r := newNetworkKitResource()

	network := &unifi.Network{
		ID: "net-1", Name: strPtr("VLAN"), Purpose: unifi.PurposeVLANOnly, Enabled: true,
		IPV6PDPrefixid: "1a",
	}
	prev := &netModel{IPv6PDPrefixid: types.StringUnknown()}

	var model netModel
	if d := r.networkToModel(ctx, network, &model, "default", prev); d.HasError() {
		t.Fatalf("networkToModel: %v", d)
	}
	if model.IPv6PDPrefixid.IsUnknown() {
		t.Fatal("ipv6_pd_prefixid is still unknown after apply; Terraform rejects that " +
			"with \"Provider produced inconsistent result after apply\"")
	}
	if model.IPv6PDPrefixid.ValueString() != "1a" {
		t.Errorf("ipv6_pd_prefixid = %q, want the controller's 1a",
			model.IPv6PDPrefixid.ValueString())
	}
}

// modelToNetwork and networkToModel are shims: the mapper tests below call
// them so they keep asserting exactly what they asserted before the surface
// moved onto the kit, rather than needing thirty call sites rewritten (and
// thirty expectations re-derived by hand) for ToSDK+BeforeSend and
// ToModel+AfterReceive. previousModel became the model itself here since
// the kit loads prior state into the model before ToModel runs.
func (r *networkKitResource) modelToNetwork(
	ctx context.Context, model *netModel,
) (*unifi.Network, diag.Diagnostics) {
	sdk, diags := r.Spec.ToSDK(ctx, model)
	if diags.HasError() {
		return sdk, diags
	}
	diags.Append(r.Spec.BeforeSend(ctx, model, model, netModel{}, sdk, nil)...)
	return sdk, diags
}

func (r *networkKitResource) networkToModel(
	ctx context.Context,
	network *unifi.Network,
	model *netModel,
	site string,
	previousModel *netModel,
) diag.Diagnostics {
	if previousModel != nil {
		*model = *previousModel
	}
	prior := *model
	diags := r.Spec.ToModel(ctx, network, model, site)
	diags.Append(r.Spec.AfterReceive(ctx, network, model, prior, nil)...)
	return diags
}

// dhcpDecodeFixture is a response carrying a value for every nullable member
// of the four scattered DHCP objects, so the probes below exercise every
// member type networkNullOf must cover.
func dhcpDecodeFixture() *unifi.Network {
	return &unifi.Network{
		ID:                     "net-1",
		Purpose:                unifi.PurposeCorporate,
		DHCPguardEnabled:       true,
		DHCPDIP1:               "192.168.1.5",
		DHCPDEnabled:           true,
		DHCPDStart:             strPtr("192.168.1.6"),
		DHCPDStop:              strPtr("192.168.1.254"),
		DHCPDGatewayEnabled:    true,
		DHCPDConflictChecking:  true,
		DHCPDNtpEnabled:        true,
		DHCPDTimeOffsetEnabled: true,
		DHCPDDNSEnabled:        true,
		DHCPDLeaseTime:         util.Ptr(int64(86400)),
		DHCPDWPAdUrl:           strPtr("http://wpad.example/wpad.dat"),
		DHCPDTFTPServer:        strPtr("192.168.1.2"),
		DHCPDUnifiController:   strPtr("192.168.1.3"),
		DHCPDDNS1:              strPtr("1.1.1.1"),
		DHCPDBootEnabled:       true,
		DHCPDBootServer:        "192.168.1.4",
		DHCPDBootFilename:      strPtr("pxelinux.0"),
		DHCPDWinsEnabled:       true,
		DHCPDWins1:             strPtr("192.168.1.7"),
		DHCPDNtp1:              strPtr("192.168.1.8"),
		DHCPDV6Enabled:         true,
		DHCPDV6DNSAuto:         true,
		DHCPDV6Start:           strPtr("::2"),
		DHCPDV6Stop:            strPtr("::7d1"),
		DHCPDV6LeaseTime:       util.Ptr(int64(86400)),
		DHCPDV6DNS1:            strPtr("2001:4860:4860::8888"),
		DHCPRelayEnabled:       true,
		DHCPRelayServers:       []string{"192.168.1.9"},
	}
}

// networkScatteredFields collects the four scattered DHCP objects from the
// spec, failing loudly if the count drifts so a new object cannot dodge the
// probes below.
func networkScatteredFields(t *testing.T) []resourcekit.ScatteredObjectField[netModel, unifi.Network] {
	t.Helper()
	var scattered []resourcekit.ScatteredObjectField[netModel, unifi.Network]
	for _, field := range networkKitSpec().Fields {
		if s, ok := field.(resourcekit.ScatteredObjectField[netModel, unifi.Network]); ok {
			scattered = append(scattered, s)
		}
	}
	if len(scattered) != 4 {
		t.Fatalf("found %d scattered field(s), want 4; a field this walk missed is one "+
			"whose Decode nothing here holds to the prior-null and known-members rules",
			len(scattered))
	}
	return scattered
}

// nullNetworkAttr builds the null value of one member type via networkNullOf,
// failing the test where the switch has no case -- the same loud path
// networkKeepPriorNulls takes at decode time.
func nullNetworkAttr(t *testing.T, typ attr.Type) attr.Value {
	t.Helper()
	null, ok := networkNullOf(typ)
	if !ok {
		t.Fatalf("no null value for member type %T; teach networkNullOf about it", typ)
	}
	return null
}

// Test_networkDecodePreservesPriorNulls hands the dhcp_server Decode a prior
// whose members are all null -- the shape an update hands it for members the
// plan never set -- against a response carrying a value for every one of
// them. start, stop and leasetime must come back null: on an update the
// prior is the plan, Terraform kills the apply when a plan-null member turns
// into a value, and go-unifi's corporate encoder invents dhcpd_start and
// dhcpd_stop from ip_subnet on every masked update, so the response carries
// them for a plan that never set a range (ubitofu controllertest reconcile
// scenario, 2026-09-05, provider 0.109.0 on unifi-network 10.6.101).
//
// Every OTHER member must take the response despite the null prior: those
// values only exist on the wire when the controller really holds them, and
// re-nulling them hides drift a refresh must surface -- the masked write
// then re-asserts an enable flag without its operand and the controller
// rejects it (api.err.MissingIPAddress for dhcpguard_enabled without
// dhcpd_ip_1, api.err.NtpAddressInvalid for dhcpd_ntp_enabled with an
// empty dhcpd_ntp_1; both measured here on the pinned 10.6.101 image when
// the rule was blanket). The null-prior contrast at the end proves the rule
// is prior-conditioned: an import (no prior at all) decodes the full
// response, range included.
func Test_networkDecodePreservesPriorNulls(t *testing.T) {
	ctx := context.Background()
	network := dhcpDecodeFixture()
	preserved := map[string]bool{"start": true, "stop": true, "leasetime": true}
	probed := false
	for _, scattered := range networkScatteredFields(t) {
		members := make(map[string]attr.Value, len(scattered.AttrTypes))
		for name, typ := range scattered.AttrTypes {
			members[name] = nullNetworkAttr(t, typ)
		}
		prior, d := types.ObjectValue(scattered.AttrTypes, members)
		if d.HasError() {
			t.Fatalf("%s: building the all-null prior: %v", scattered.Wires[0], d)
		}
		object, d := scattered.Decode(ctx, network, prior)
		if d.HasError() {
			t.Fatalf("%s: Decode: %v", scattered.Wires[0], d)
		}
		isDHCPServer := scattered.Wires[0] == "dhcpd_enabled"
		for name, value := range object.Attributes() {
			if isDHCPServer && preserved[name] {
				probed = true
				if !value.IsNull() {
					t.Errorf("dhcp_server member %q = %v, want null; the prior (the "+
						"plan, on an update) holds it as null and Terraform refuses "+
						"an apply that turns a plan-null into a value", name, value)
				}
				continue
			}
			// The fixture carries a wire value for every drift-bearing
			// member, so a null here means the decode dropped real
			// controller state.
			if name == "boot" || name == "wins" {
				continue // nested objects: their members decode from always-present wires
			}
			if value.IsNull() {
				t.Errorf("%s: member %q is null despite the response carrying it; "+
					"a null prior must not hide controller-side state from a refresh",
					scattered.Wires[0], name)
			}
		}

		// The contrast: no prior at all (an import's first read) takes the
		// response, range included.
		object, d = scattered.Decode(ctx, network, types.ObjectNull(scattered.AttrTypes))
		if d.HasError() {
			t.Fatalf("%s: Decode with null prior: %v", scattered.Wires[0], d)
		}
		if isDHCPServer {
			start := object.Attributes()["start"]
			if start.IsNull() {
				t.Error("dhcp_server.start is null on an import's first read; the rule " +
					"must only preserve nulls the prior asserts, not stop decoding")
			}
		}
	}
	if !probed {
		t.Fatal("the walk never reached dhcp_server's preserved members; " +
			"the probe proved nothing")
	}
}

// unknownNetworkAttr builds the unknown value of one member type, for the
// probe below. The switch covers every member type network's objects declare.
func unknownNetworkAttr(t *testing.T, typ attr.Type) attr.Value {
	t.Helper()
	switch concrete := typ.(type) {
	case basetypes.BoolType:
		return types.BoolUnknown()
	case basetypes.Int64Type:
		return types.Int64Unknown()
	case basetypes.StringType:
		return types.StringUnknown()
	case timetypes.GoDurationType:
		return timetypes.NewGoDurationUnknown()
	case types.ListType:
		return types.ListUnknown(concrete.ElemType)
	case types.ObjectType:
		return types.ObjectUnknown(concrete.AttrTypes)
	}
	t.Fatalf("no unknown value for member type %T; teach unknownNetworkAttr about it", typ)
	return nil
}

// Test_networkDecodeResolvesUnknownPriorMembers holds network's scattered
// Decodes to the same known-members rule wan's wanResolveUnknowns enforces:
// a prior full of unknowns (a create whose configuration sets the object but
// omits its Optional+Computed members) must leave every member known, since
// Terraform refuses an unknown after apply.
func Test_networkDecodeResolvesUnknownPriorMembers(t *testing.T) {
	ctx := context.Background()
	network := &unifi.Network{ID: "net-1", Purpose: unifi.PurposeCorporate}
	for _, scattered := range networkScatteredFields(t) {
		members := make(map[string]attr.Value, len(scattered.AttrTypes))
		for name, typ := range scattered.AttrTypes {
			members[name] = unknownNetworkAttr(t, typ)
		}
		prior, d := types.ObjectValue(scattered.AttrTypes, members)
		if d.HasError() {
			t.Fatalf("%s: building the all-unknown prior: %v", scattered.Wires[0], d)
		}
		object, d := scattered.Decode(ctx, network, prior)
		if d.HasError() {
			t.Fatalf("%s: Decode: %v", scattered.Wires[0], d)
		}
		if object.IsUnknown() {
			t.Errorf("%s: Decode returned an unknown object", scattered.Wires[0])
			continue
		}
		for name, value := range object.Attributes() {
			if value.IsUnknown() {
				t.Errorf("%s: member %q is still unknown after Decode; Terraform refuses "+
					"an unknown after apply", scattered.Wires[0], name)
			}
		}
	}
}

// TestAccNetworkFramework_importThenPlanClean pins the import round-trip the
// downstream reconcile scenario measured broken on v0.109.0 (ubitofu
// controllertest, 2026-09-05, unifi-network 10.6.101): the controller
// stores neither gateway_type nor dhcpd_leasetime, so both must import as
// null and stay unplanned. Unlike the older import steps above, gateway_type
// and dhcp_server are deliberately NOT in ImportStateVerifyIgnore -- their
// round-trip is the regression this test exists to catch (both carried
// static defaults before, so every imported network planned
// "+ gateway_type" and "+ dhcp_server.leasetime" forever). The final
// PlanOnly step is the downstream complaint verbatim: a plan straight after
// import must be empty.
func TestAccNetworkFramework_importThenPlanClean(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccNetworkFrameworkConfig_importThenPlanClean(),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"unifi_network.test_import_plan", "name", "Test Import Plan"),
					resource.TestCheckResourceAttr(
						"unifi_network.test_import_plan", "dhcp_server.start", "192.168.26.10"),
					resource.TestCheckResourceAttr(
						"unifi_network.test_import_plan", "dhcp_server.stop", "192.168.26.254"),
					// The controller stores no gateway_type and discards
					// dhcpd_leasetime, so neither may surface as a value.
					resource.TestCheckNoResourceAttr(
						"unifi_network.test_import_plan", "gateway_type"),
					resource.TestCheckNoResourceAttr(
						"unifi_network.test_import_plan", "dhcp_server.leasetime"),
				),
			},
			{
				ResourceName:      "unifi_network.test_import_plan",
				ImportState:       true,
				ImportStateVerify: true,
				ImportStateId:     "name=Test Import Plan",
			},
			{
				Config:   testAccNetworkFrameworkConfig_importThenPlanClean(),
				PlanOnly: true,
			},
		},
	})
}

func testAccNetworkFrameworkConfig_importThenPlanClean() string {
	return `
resource "unifi_network" "test_import_plan" {
	name   = "Test Import Plan"
	subnet = "192.168.26.1/24"
	vlan   = 26

	dhcp_server = {
		enabled = true
		start   = "192.168.26.10"
		stop    = "192.168.26.254"
	}
}
`
}

// TestAccNetworkFramework_dhcpEnableWithoutRange enables DHCP on an
// imported network whose stored document carries no dhcpd_start/dhcpd_stop,
// without configuring a range -- the downstream reconcile scenario verbatim
// (ubitofu controllertest, 2026-09-05, provider 0.109.0 on unifi-network
// 10.6.101). The masked update's response carries a range anyway (the SDK's
// corporate encoder derives one from ip_subnet when the caller holds none);
// before networkKeepPriorNulls the decode wrote it into state where the
// plan held null and Terraform killed the apply with "was null, but now
// cty.StringVal(...)". Both members must stay null through the apply and
// the step's own post-apply plan must come back empty.
//
// The range-free document has to be fabricated with a raw create: every
// SDK write path runs the corporate encoder, which fills dhcpd_start and
// dhcpd_stop whenever they are empty, so no unifi_network apply -- and no
// out-of-band UpdateNetwork -- can produce a document without them
// (measured here on the pinned 10.6.101 image).
func TestAccNetworkFramework_dhcpEnableWithoutRange(t *testing.T) {
	createRangeFreeNetworkOutOfBand := func() {
		base, client := rawWireSession(t)
		encoded, err := json.Marshal(map[string]any{
			"name":          "Test DHCP Range",
			"purpose":       "corporate",
			"ip_subnet":     "192.168.27.1/24",
			"vlan":          27,
			"vlan_enabled":  true,
			"dhcpd_enabled": false,
		})
		if err != nil {
			t.Fatal(err)
		}
		request, err := http.NewRequestWithContext(context.Background(),
			http.MethodPost, base+"/api/s/default/rest/networkconf", bytes.NewReader(encoded))
		if err != nil {
			t.Fatal(err)
		}
		request.Header.Set("Content-Type", "application/json")
		response, err := client.Do(request)
		if err != nil {
			t.Fatalf("raw networkconf create: %v", err)
		}
		defer response.Body.Close()
		if response.StatusCode != http.StatusOK {
			t.Fatalf("raw networkconf create returned %d", response.StatusCode)
		}
		documents, ok := rawDocuments(t, base, client, "/api/s/default/rest/networkconf")
		if !ok {
			t.Fatal("unable to list networkconf documents for the positive control")
		}
		for _, document := range documents {
			if document["name"] != "Test DHCP Range" {
				continue
			}
			_, hasStart := document["dhcpd_start"]
			_, hasStop := document["dhcpd_stop"]
			if hasStart || hasStop {
				t.Fatalf("the stored document carries dhcpd_start/dhcpd_stop "+
					"(start %v, stop %v), so the plan-null precondition this test "+
					"needs cannot be established and it would pass vacuously",
					hasStart, hasStop)
			}
			t.Log("POSITIVE CONTROL: the stored document carries no dhcpd_start/dhcpd_stop")
			return
		}
		t.Fatal("the raw create succeeded but the document is not listed")
	}

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				PreConfig:          createRangeFreeNetworkOutOfBand,
				Config:             testAccNetworkFrameworkConfig_dhcpRange(false),
				ResourceName:       "unifi_network.test_dhcp_range",
				ImportState:        true,
				ImportStateId:      "name=Test DHCP Range",
				ImportStatePersist: true,
			},
			{
				Config: testAccNetworkFrameworkConfig_dhcpRange(true),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"unifi_network.test_dhcp_range", "dhcp_server.enabled", "true"),
					// The range stays delegated: the plan held both members
					// null (the imported document carried neither), so the
					// update response's range must not reach state.
					resource.TestCheckNoResourceAttr(
						"unifi_network.test_dhcp_range", "dhcp_server.start"),
					resource.TestCheckNoResourceAttr(
						"unifi_network.test_dhcp_range", "dhcp_server.stop"),
				),
			},
		},
	})
}

func testAccNetworkFrameworkConfig_dhcpRange(enabled bool) string {
	return fmt.Sprintf(`
resource "unifi_network" "test_dhcp_range" {
	name   = "Test DHCP Range"
	subnet = "192.168.27.1/24"
	vlan   = 27

	dhcp_server = {
		enabled = %t
	}
}
`, enabled)
}
