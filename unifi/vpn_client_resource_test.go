package unifi

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	fwlist "github.com/hashicorp/terraform-plugin-framework/list"
	fwresource "github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/querycheck"
	"github.com/hashicorp/terraform-plugin-testing/tfversion"
)

func TestAccVPNClient_file_mode(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccVPNClientConfig_file_mode(),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"unifi_vpn_client.test",
						"name",
						"test-wireguard-vpn",
					),
					resource.TestCheckResourceAttr("unifi_vpn_client.test", "enabled", "true"),
					resource.TestCheckResourceAttr(
						"unifi_vpn_client.test",
						"default_route",
						"true",
					),
					resource.TestCheckResourceAttr("unifi_vpn_client.test", "pull_dns", "false"),
					resource.TestCheckResourceAttr(
						"unifi_vpn_client.test",
						"wireguard.interface",
						"wan",
					),
					resource.TestCheckResourceAttrSet(
						"unifi_vpn_client.test",
						"wireguard.configuration.content",
					),
					resource.TestCheckResourceAttrSet(
						"unifi_vpn_client.test",
						"wireguard.configuration.filename",
					),
				),
			},
			{
				ResourceName:      "unifi_vpn_client.test",
				ImportState:       true,
				ImportStateVerify: true,
				ImportStateVerifyIgnore: []string{
					"wireguard.private_key",
					"wireguard.configuration",
					"wireguard.configuration.content",
					"wireguard.configuration.filename",
					"wireguard.preshared_key",
					"wireguard.peer",
					"wireguard.peer.ip",
					"wireguard.peer.port",
					"wireguard.peer.public_key",
					"wireguard.dns_servers",
				},
			},
		},
	})
}

func TestAccVPNClient_manual_mode(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccVPNClientConfig_manual_mode(),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"unifi_vpn_client.test",
						"name",
						"test-wireguard-manual",
					),
					resource.TestCheckResourceAttr("unifi_vpn_client.test", "enabled", "true"),
					resource.TestCheckResourceAttr(
						"unifi_vpn_client.test",
						"default_route",
						"false",
					),
					resource.TestCheckResourceAttr("unifi_vpn_client.test", "pull_dns", "true"),
					resource.TestCheckResourceAttr(
						"unifi_vpn_client.test",
						"wireguard.peer.ip",
						"192.0.2.1",
					),
					resource.TestCheckResourceAttr(
						"unifi_vpn_client.test",
						"wireguard.peer.port",
						"51820",
					),
					resource.TestCheckResourceAttrSet(
						"unifi_vpn_client.test",
						"wireguard.peer.public_key",
					),
				),
			},
			{
				ResourceName:      "unifi_vpn_client.test",
				ImportState:       true,
				ImportStateVerify: true,
				ImportStateVerifyIgnore: []string{
					"wireguard.private_key",
					"wireguard.peer.public_key",
					"wireguard.preshared_key",
				},
			},
		},
	})
}

func TestAccVPNClient_with_preshared_key(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccVPNClientConfig_with_preshared_key(),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"unifi_vpn_client.test",
						"name",
						"test-wireguard-psk",
					),
					resource.TestCheckResourceAttr(
						"unifi_vpn_client.test",
						"wireguard.preshared_key_enabled",
						"true",
					),
					resource.TestCheckResourceAttrSet(
						"unifi_vpn_client.test",
						"wireguard.preshared_key",
					),
				),
			},
		},
	})
}

func testAccVPNClientConfig_file_mode() string {
	return `
resource "unifi_vpn_client" "test" {
  name          = "test-wireguard-vpn"
  enabled       = true
  subnet        = "10.0.0.2/24"
  default_route = true
  pull_dns      = false

  wireguard = {
    private_key = "WPiBa/Ak1W+8Sp8L5yvbyhHeRO2o5kJvihq2VtJ+kFg="
    interface   = "wan"

    configuration = {
      content  = "W0ludGVyZmFjZV0KUHJpdmF0ZUtleSA9IFdQaUJhL0FrMVcrOFNwOEw1eXZieWhIZVJPMm81a0p2aWhxMlZ0SitrRmc9CkFkZHJlc3MgPSAxMC4wLjAuMi8yNApETlMgPSA4LjguOC44LCA4LjguNC40CgpbUGVlcl0KUHVibGljS2V5ID0gN0IrMlozb2RQYkROc2ZWcitGOGludmo2L21CS0xWYW9sT0hYWm9DYUJBMD0KRW5kcG9pbnQgPSAxOTIuMC4yLjE6NTE4MjAKQWxsb3dlZElQcyA9IDAuMC4wLjAvMAo="
      filename = "wireguard.conf"
    }
  }
}
`
}

func testAccVPNClientConfig_manual_mode() string {
	return `
resource "unifi_vpn_client" "test" {
  name          = "test-wireguard-manual"
  enabled       = true
  subnet        = "10.0.1.2/24"
  default_route = false
  pull_dns      = true

  wireguard = {
    private_key = "WPiBa/Ak1W+8Sp8L5yvbyhHeRO2o5kJvihq2VtJ+kFg="
    interface   = "wan"
    dns_servers = ["8.8.8.8", "8.8.4.4"]

    peer = {
      ip         = "192.0.2.1"
      port       = 51820
      public_key = "7B+2Z3odPbDNsfVr+F8invj6/mBKLVaolOHXZoCaBA0="
    }
  }
}
`
}

func testAccVPNClientConfig_with_preshared_key() string {
	return `
resource "unifi_vpn_client" "test" {
  name          = "test-wireguard-psk"
  enabled       = true
  subnet        = "10.0.2.2/24"
  default_route = true
  pull_dns      = false

  wireguard = {
    private_key            = "WPiBa/Ak1W+8Sp8L5yvbyhHeRO2o5kJvihq2VtJ+kFg="
    preshared_key_enabled  = true
    preshared_key          = "F3JcsRyn9Hywwyhl4EznlV4ZThatbB5Hi4U9b3emM+g="
    interface              = "wan"
    dns_servers            = ["8.8.8.8", "8.8.4.4"]

    peer = {
      ip         = "192.0.2.1"
      port       = 51820
      public_key = "7B+2Z3odPbDNsfVr+F8invj6/mBKLVaolOHXZoCaBA0="
    }
  }
}
`
}

func TestNewVPNClientResource(t *testing.T) {
	r := NewVPNClientResource()
	if r == nil {
		t.Fatal("NewVPNClientResource() returned nil")
	}
	if _, ok := r.(fwresource.ResourceWithImportState); !ok {
		t.Error("expected ResourceWithImportState interface")
	}
}

func TestNewVPNClientListResource(t *testing.T) {
	r := NewVPNClientListResource()
	if r == nil {
		t.Fatal("NewVPNClientListResource() returned nil")
	}
	if _, ok := r.(fwlist.ListResourceWithConfigure); !ok {
		t.Error("expected ListResourceWithConfigure interface")
	}
}

func Test_wireguardConfigurationModel_AttributeTypes(t *testing.T) {
	m := wireguardConfigurationModel{}
	got := m.AttributeTypes()
	want := map[string]attr.Type{
		"content":  types.StringType,
		"filename": types.StringType,
	}
	if len(got) != len(want) {
		t.Errorf("AttributeTypes() returned %d entries, want %d", len(got), len(want))
	}
	for k, wantType := range want {
		if gotType, ok := got[k]; !ok {
			t.Errorf("missing key %q", k)
		} else if gotType != wantType {
			t.Errorf("key %q: got %v, want %v", k, gotType, wantType)
		}
	}
}

func Test_wireguardPeerModel_AttributeTypes(t *testing.T) {
	m := wireguardPeerModel{}
	got := m.AttributeTypes()
	want := map[string]attr.Type{
		"ip":         types.StringType,
		"port":       types.Int64Type,
		"public_key": types.StringType,
	}
	if len(got) != len(want) {
		t.Errorf("AttributeTypes() returned %d entries, want %d", len(got), len(want))
	}
	for k, wantType := range want {
		if gotType, ok := got[k]; !ok {
			t.Errorf("missing key %q", k)
		} else if gotType != wantType {
			t.Errorf("key %q: got %v, want %v", k, gotType, wantType)
		}
	}
}

func Test_wireguardModel_AttributeTypes(t *testing.T) {
	m := wireguardModel{}
	got := m.AttributeTypes()
	for _, key := range []string{
		"private_key", "configuration", "peer",
		"preshared_key_enabled", "preshared_key", "interface", "dns_servers",
	} {
		if _, ok := got[key]; !ok {
			t.Errorf("missing key %q in AttributeTypes()", key)
		}
	}
}

func Test_vpnClientResource_IdentitySchema(t *testing.T) {
	r := newVPNClientKitResource()
	resp := &fwresource.IdentitySchemaResponse{}
	r.IdentitySchema(context.Background(), fwresource.IdentitySchemaRequest{}, resp)
	if resp.Diagnostics.HasError() {
		t.Errorf("IdentitySchema() produced errors: %v", resp.Diagnostics)
	}
	if _, ok := resp.IdentitySchema.Attributes["id"]; !ok {
		t.Error("IdentitySchema missing 'id' attribute")
	}
}

func Test_vpnClientResource_ListResourceConfigSchema(t *testing.T) {
	r := newVPNClientKitResource()
	resp := &fwlist.ListResourceSchemaResponse{}
	r.ListResourceConfigSchema(context.Background(), fwlist.ListResourceSchemaRequest{}, resp)
	if resp.Diagnostics.HasError() {
		t.Errorf("ListResourceConfigSchema() produced errors: %v", resp.Diagnostics)
	}
	if _, ok := resp.Schema.Attributes["site"]; !ok {
		t.Error("ListResourceConfigSchema missing 'site' attribute")
	}
}

func TestAccVPNClientList_basic(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		TerraformVersionChecks: []tfversion.TerraformVersionCheck{
			tfversion.SkipBelow(tfversion.Version1_14_0),
		},
		Steps: []resource.TestStep{
			{
				Config: testAccVPNClientConfig_file_mode(),
			},
			{
				Query: true,
				Config: `
					provider "unifi" {}
					list "unifi_vpn_client" "test" {
						provider = unifi
						config {
							filter {
								name  = "name"
								value = "test-wireguard-vpn"
						  }
					  }
					}
				`,
				QueryResultChecks: []querycheck.QueryResultCheck{
					querycheck.ExpectLengthAtLeast("unifi_vpn_client.test", 1),
				},
			},
		},
	})
}
