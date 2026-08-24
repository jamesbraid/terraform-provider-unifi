package unifi

import (
	"context"
	"fmt"
	"reflect"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	fwlist "github.com/hashicorp/terraform-plugin-framework/list"
	fwresource "github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/querycheck"
	"github.com/hashicorp/terraform-plugin-testing/tfversion"
)

func TestAccPortForward_basic(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccPortForwardConfig_basic(),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet("unifi_port_forward.test", "id"),
					resource.TestCheckResourceAttr(
						"unifi_port_forward.test",
						"name",
						"tfacc-port-forward",
					),
					resource.TestCheckResourceAttr(
						"unifi_port_forward.test",
						"forward.ip",
						"192.168.1.100",
					),
					resource.TestCheckResourceAttr(
						"unifi_port_forward.test",
						"forward.port",
						"8080",
					),
					resource.TestCheckResourceAttr(
						"unifi_port_forward.test",
						"wan.port",
						"80",
					),
					resource.TestCheckResourceAttr(
						"unifi_port_forward.test",
						"protocol",
						"tcp_udp",
					),
					resource.TestCheckResourceAttr(
						"unifi_port_forward.test",
						"logging",
						"false",
					),
				),
			},
			{
				ResourceName:      "unifi_port_forward.test",
				ImportState:       true,
				ImportStateVerify: true,
				ImportStateVerifyIgnore: []string{
					"enabled",
				},
			},
		},
	})
}

func TestAccPortForward_wan(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccPortForwardConfig_wan(),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet("unifi_port_forward.test_wan", "id"),
					resource.TestCheckResourceAttr(
						"unifi_port_forward.test_wan",
						"name",
						"tfacc-wan-forward",
					),
					resource.TestCheckResourceAttr(
						"unifi_port_forward.test_wan",
						"wan.interface",
						"wan",
					),
					resource.TestCheckResourceAttr(
						"unifi_port_forward.test_wan",
						"wan.port",
						"443",
					),
					resource.TestCheckResourceAttr(
						"unifi_port_forward.test_wan",
						"forward.ip",
						"192.168.1.50",
					),
					resource.TestCheckResourceAttr(
						"unifi_port_forward.test_wan",
						"forward.port",
						"443",
					),
					resource.TestCheckResourceAttr(
						"unifi_port_forward.test_wan",
						"protocol",
						"tcp",
					),
				),
			},
			{
				ResourceName:      "unifi_port_forward.test_wan",
				ImportState:       true,
				ImportStateVerify: true,
				ImportStateVerifyIgnore: []string{
					"enabled",
				},
			},
		},
	})
}

func TestAccPortForward_sourceLimitingIP(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccPortForwardConfig_sourceLimitingIP(),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet("unifi_port_forward.test_src", "id"),
					resource.TestCheckResourceAttr(
						"unifi_port_forward.test_src",
						"name",
						"tfacc-src-limited",
					),
					resource.TestCheckResourceAttr(
						"unifi_port_forward.test_src",
						"source_limiting.ip",
						"10.0.0.0/24",
					),
					resource.TestCheckResourceAttr(
						"unifi_port_forward.test_src",
						"source_limiting.enabled",
						"true",
					),
					resource.TestCheckResourceAttr(
						"unifi_port_forward.test_src",
						"source_limiting.type",
						"ip",
					),
				),
			},
			{
				ResourceName:      "unifi_port_forward.test_src",
				ImportState:       true,
				ImportStateVerify: true,
				ImportStateVerifyIgnore: []string{
					"enabled",
				},
			},
		},
	})
}

func TestAccPortForward_sourceLimitingFirewallGroup(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccPortForwardConfig_sourceLimitingFirewallGroup(),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet("unifi_port_forward.test_fwg", "id"),
					resource.TestCheckResourceAttr(
						"unifi_port_forward.test_fwg",
						"name",
						"tfacc-fwg-limited",
					),
					resource.TestCheckResourceAttr(
						"unifi_port_forward.test_fwg",
						"source_limiting.enabled",
						"true",
					),
					resource.TestCheckResourceAttr(
						"unifi_port_forward.test_fwg",
						"source_limiting.type",
						"firewall_group",
					),
					resource.TestCheckResourceAttrSet(
						"unifi_port_forward.test_fwg",
						"source_limiting.firewall_group_id",
					),
				),
			},
			{
				ResourceName:      "unifi_port_forward.test_fwg",
				ImportState:       true,
				ImportStateVerify: true,
				ImportStateVerifyIgnore: []string{
					"enabled",
				},
			},
		},
	})
}

func TestAccPortForward_update(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccPortForwardConfig_update_before(),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"unifi_port_forward.test_update",
						"name",
						"tfacc-update-forward",
					),
					resource.TestCheckResourceAttr(
						"unifi_port_forward.test_update",
						"forward.ip",
						"192.168.1.10",
					),
					resource.TestCheckResourceAttr(
						"unifi_port_forward.test_update",
						"forward.port",
						"80",
					),
					resource.TestCheckResourceAttr(
						"unifi_port_forward.test_update",
						"wan.port",
						"8080",
					),
					resource.TestCheckResourceAttr(
						"unifi_port_forward.test_update",
						"protocol",
						"tcp",
					),
				),
			},
			{
				Config: testAccPortForwardConfig_update_after(),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"unifi_port_forward.test_update",
						"name",
						"tfacc-update-forward-renamed",
					),
					resource.TestCheckResourceAttr(
						"unifi_port_forward.test_update",
						"forward.ip",
						"192.168.1.20",
					),
					resource.TestCheckResourceAttr(
						"unifi_port_forward.test_update",
						"forward.port",
						"443",
					),
					resource.TestCheckResourceAttr(
						"unifi_port_forward.test_update",
						"wan.port",
						"9443",
					),
					resource.TestCheckResourceAttr(
						"unifi_port_forward.test_update",
						"protocol",
						"tcp_udp",
					),
					resource.TestCheckResourceAttr(
						"unifi_port_forward.test_update",
						"logging",
						"true",
					),
				),
			},
		},
	})
}

func TestAccPortForward_syslogLogging(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccPortForwardConfig_syslogLogging(),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet("unifi_port_forward.test_log", "id"),
					resource.TestCheckResourceAttr(
						"unifi_port_forward.test_log",
						"name",
						"tfacc-logging",
					),
					resource.TestCheckResourceAttr(
						"unifi_port_forward.test_log",
						"logging",
						"true",
					),
				),
			},
		},
	})
}

func TestAccPortForward_protocols(t *testing.T) {
	for _, proto := range []string{"tcp", "udp", "tcp_udp"} {
		t.Run(proto, func(t *testing.T) {
			resource.Test(t, resource.TestCase{
				PreCheck:                 func() { preCheck(t) },
				ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
				Steps: []resource.TestStep{
					{
						Config: testAccPortForwardConfig_protocol(proto),
						Check: resource.ComposeTestCheckFunc(
							resource.TestCheckResourceAttr(
								"unifi_port_forward.test_proto",
								"protocol",
								proto,
							),
						),
					},
				},
			})
		})
	}
}

func TestAccPortForward_destinationIPs(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccPortForwardConfig_destinationIPs(),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet("unifi_port_forward.test_dst", "id"),
					resource.TestCheckResourceAttr(
						"unifi_port_forward.test_dst",
						"destination_ips.#",
						"1",
					),
					resource.TestCheckResourceAttr(
						"unifi_port_forward.test_dst",
						"destination_ips.0.destination_ip",
						"192.0.2.10",
					),
					resource.TestCheckResourceAttr(
						"unifi_port_forward.test_dst",
						"destination_ips.0.interface",
						"wan",
					),
				),
			},
			{
				ResourceName:            "unifi_port_forward.test_dst",
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"enabled"},
			},
		},
	})
}

func testAccPortForwardConfig_destinationIPs() string {
	return `
resource "unifi_port_forward" "test_dst" {
  name     = "tfacc-dst-ips"
  protocol = "tcp"

  wan = {
    port = "8443"
  }

  forward = {
    ip   = "192.168.1.20"
    port = "8443"
  }

  destination_ips = [
    {
      destination_ip = "192.0.2.10"
      interface      = "wan"
    },
  ]
}
`
}

// TestAccPortForward_anyIP verifies that wan.ip_address accepts "any" -- the
// controller's own value for a rule with no destination IP filter.
func TestAccPortForward_anyIP(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccPortForwardConfig_anyIP(),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet("unifi_port_forward.any", "id"),
					resource.TestCheckResourceAttr(
						"unifi_port_forward.any",
						"wan.ip_address",
						"any",
					),
				),
			},
			{
				ResourceName:            "unifi_port_forward.any",
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"enabled"},
			},
		},
	})
}

func testAccPortForwardConfig_anyIP() string {
	return `
resource "unifi_port_forward" "any" {
  name     = "tfacc-any-ip"
  protocol = "tcp"

  wan = {
    ip_address = "any"
    port       = "8081"
  }

  forward = {
    ip   = "192.168.1.30"
    port = "8081"
  }
}
`
}

func testAccPortForwardConfig_basic() string {
	return `
resource "unifi_port_forward" "test" {
  name = "tfacc-port-forward"

  wan = {
    port = "80"
  }

  forward = {
    ip   = "192.168.1.100"
    port = "8080"
  }
}
`
}

func testAccPortForwardConfig_wan() string {
	return `
resource "unifi_port_forward" "test_wan" {
  name     = "tfacc-wan-forward"
  protocol = "tcp"

  wan = {
    interface = "wan"
    port      = "443"
  }

  forward = {
    ip   = "192.168.1.50"
    port = "443"
  }
}
`
}

func testAccPortForwardConfig_sourceLimitingIP() string {
	return `
resource "unifi_port_forward" "test_src" {
  name = "tfacc-src-limited"

  wan = {
    port = "22"
  }

  forward = {
    ip   = "192.168.1.10"
    port = "22"
  }

  source_limiting = {
    ip      = "10.0.0.0/24"
    enabled = true
  }
}
`
}

func testAccPortForwardConfig_sourceLimitingFirewallGroup() string {
	return `
resource "unifi_firewall_group" "test_src_group" {
  name = "tfacc-pf-src-group"
  type = "address-group"
  members = [
    "10.0.0.1",
    "10.0.0.2",
  ]
}

resource "unifi_port_forward" "test_fwg" {
  name = "tfacc-fwg-limited"

  wan = {
    port = "2222"
  }

  forward = {
    ip   = "192.168.1.10"
    port = "22"
  }

  source_limiting = {
    firewall_group_id = unifi_firewall_group.test_src_group.id
    enabled           = true
  }
}
`
}

func testAccPortForwardConfig_update_before() string {
	return `
resource "unifi_port_forward" "test_update" {
  name     = "tfacc-update-forward"
  protocol = "tcp"

  wan = {
    port = "8080"
  }

  forward = {
    ip   = "192.168.1.10"
    port = "80"
  }
}
`
}

func testAccPortForwardConfig_update_after() string {
	return `
resource "unifi_port_forward" "test_update" {
  name          = "tfacc-update-forward-renamed"
  protocol      = "tcp_udp"
  logging = true

  wan = {
    port = "9443"
  }

  forward = {
    ip   = "192.168.1.20"
    port = "443"
  }
}
`
}

func testAccPortForwardConfig_syslogLogging() string {
	return `
resource "unifi_port_forward" "test_log" {
  name           = "tfacc-logging"
  logging = true

  wan = {
    port = "3000"
  }

  forward = {
    ip   = "192.168.1.100"
    port = "3000"
  }
}
`
}

func testAccPortForwardConfig_protocol(proto string) string {
	return fmt.Sprintf(`
resource "unifi_port_forward" "test_proto" {
  name     = "tfacc-proto-%s"
  protocol = "%s"

  wan = {
    port = "5000"
  }

  forward = {
    ip   = "192.168.1.100"
    port = "5000"
  }
}
`, proto, proto)
}

func TestNewPortForwardResource(t *testing.T) {
	got := NewPortForwardResource()
	if got == nil {
		t.Fatal("NewPortForwardResource() returned nil")
	}
	if _, ok := got.(fwresource.ResourceWithImportState); !ok {
		t.Errorf("NewPortForwardResource() does not implement resource.ResourceWithImportState")
	}
	if _, ok := got.(fwresource.ResourceWithIdentity); !ok {
		t.Errorf("NewPortForwardResource() does not implement resource.ResourceWithIdentity")
	}
}

func TestNewPortForwardListResource(t *testing.T) {
	got := NewPortForwardListResource()
	if got == nil {
		t.Fatal("NewPortForwardListResource() returned nil")
	}
	if _, ok := got.(fwlist.ListResourceWithConfigure); !ok {
		t.Errorf("NewPortForwardListResource() does not implement list.ListResourceWithConfigure")
	}
}

func Test_portForwardWanModel_AttributeTypes(t *testing.T) {
	tests := []struct {
		name string
		m    portForwardWanModel
		want map[string]attr.Type
	}{
		{
			name: "returns correct attribute types",
			m:    portForwardWanModel{},
			want: map[string]attr.Type{
				"interface":  types.StringType,
				"ip_address": types.StringType,
				"port":       types.StringType,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.m.AttributeTypes(); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("portForwardWanModel.AttributeTypes() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_portForwardForwardModel_AttributeTypes(t *testing.T) {
	tests := []struct {
		name string
		m    portForwardForwardModel
		want map[string]attr.Type
	}{
		{
			name: "returns correct attribute types",
			m:    portForwardForwardModel{},
			want: map[string]attr.Type{
				"ip":   types.StringType,
				"port": types.StringType,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.m.AttributeTypes(); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("portForwardForwardModel.AttributeTypes() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_portForwardSourceLimitingModel_AttributeTypes(t *testing.T) {
	tests := []struct {
		name string
		m    portForwardSourceLimitingModel
		want map[string]attr.Type
	}{
		{
			name: "returns correct attribute types",
			m:    portForwardSourceLimitingModel{},
			want: map[string]attr.Type{
				"ip":                types.StringType,
				"firewall_group_id": types.StringType,
				"enabled":           types.BoolType,
				"type":              types.StringType,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.m.AttributeTypes(); !reflect.DeepEqual(got, tt.want) {
				t.Errorf(
					"portForwardSourceLimitingModel.AttributeTypes() = %v, want %v",
					got,
					tt.want,
				)
			}
		})
	}
}

func Test_portForwardDestinationIPModel_AttributeTypes(t *testing.T) {
	tests := []struct {
		name string
		m    portForwardDestinationIPModel
		want map[string]attr.Type
	}{
		{
			name: "returns correct attribute types",
			m:    portForwardDestinationIPModel{},
			want: map[string]attr.Type{
				"destination_ip": types.StringType,
				"interface":      types.StringType,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.m.AttributeTypes(); !reflect.DeepEqual(got, tt.want) {
				t.Errorf(
					"portForwardDestinationIPModel.AttributeTypes() = %v, want %v",
					got,
					tt.want,
				)
			}
		})
	}
}

func Test_portForwardResource_IdentitySchema(t *testing.T) {
	type args struct {
		in0  context.Context
		in1  fwresource.IdentitySchemaRequest
		resp *fwresource.IdentitySchemaResponse
	}
	tests := []struct {
		name string
		r    *portForwardResource
		args args
	}{
		{
			name: "does not panic",
			r:    newPortForwardKitResource(),
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
			if tt.args.resp.Diagnostics.HasError() {
				t.Fatalf("IdentitySchema() diagnostics: %v", tt.args.resp.Diagnostics.Errors())
			}
			if len(tt.args.resp.IdentitySchema.Attributes) == 0 {
				t.Fatal("IdentitySchema() returned no attributes")
			}
		})
	}
}

func Test_portForwardResource_ListResourceConfigSchema(t *testing.T) {
	type args struct {
		in0  context.Context
		in1  fwlist.ListResourceSchemaRequest
		resp *fwlist.ListResourceSchemaResponse
	}
	tests := []struct {
		name string
		r    *portForwardResource
		args args
	}{
		{
			name: "does not panic",
			r:    newPortForwardKitResource(),
			args: args{
				in0:  context.Background(),
				in1:  fwlist.ListResourceSchemaRequest{},
				resp: &fwlist.ListResourceSchemaResponse{},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.r.ListResourceConfigSchema(tt.args.in0, tt.args.in1, tt.args.resp)
			if tt.args.resp.Diagnostics.HasError() {
				t.Fatalf("ListResourceConfigSchema() diagnostics: %v", tt.args.resp.Diagnostics.Errors())
			}
			if len(tt.args.resp.Schema.Attributes) == 0 {
				t.Fatal("ListResourceConfigSchema() returned no attributes")
			}
		})
	}
}

func TestAccPortForwardList_basic(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		TerraformVersionChecks: []tfversion.TerraformVersionCheck{
			tfversion.SkipBelow(tfversion.Version1_14_0),
		},
		Steps: []resource.TestStep{
			{
				Config: testAccPortForwardConfig_basic(),
			},
			{
				Query: true,
				Config: `
					provider "unifi" {}
					list "unifi_port_forward" "test" {
						provider = unifi
						config {
							filter {
								name  = "name"
								value = "tfacc-port-forward"
						  }
					  }
					}
				`,
				QueryResultChecks: []querycheck.QueryResultCheck{
					querycheck.ExpectLengthAtLeast("unifi_port_forward.test", 1),
				},
			},
		},
	})
}
