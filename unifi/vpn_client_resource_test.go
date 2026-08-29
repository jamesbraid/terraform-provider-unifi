package unifi

import (
	"context"
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	fwlist "github.com/hashicorp/terraform-plugin-framework/list"
	fwresource "github.com/hashicorp/terraform-plugin-framework/resource"
	rschema "github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/querycheck"
	"github.com/hashicorp/terraform-plugin-testing/tfversion"
	"github.com/ubiquiti-community/go-unifi/unifi"
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

// TestAccVPNClient_write_only_private_key creates with private_key_wo,
// confirms neither key attribute is in state, then rotates to a different
// key by bumping private_key_wo_version and confirms state again.
func TestAccVPNClient_write_only_private_key(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccVPNClientConfig_write_only_private_key(
					1,
					"WPiBa/Ak1W+8Sp8L5yvbyhHeRO2o5kJvihq2VtJ+kFg=",
				),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("unifi_vpn_client.test", "enabled", "false"),
					resource.TestCheckNoResourceAttr(
						"unifi_vpn_client.test",
						"wireguard.private_key",
					),
					resource.TestCheckNoResourceAttr(
						"unifi_vpn_client.test",
						"wireguard.private_key_wo",
					),
					resource.TestCheckResourceAttr(
						"unifi_vpn_client.test",
						"wireguard.private_key_wo_version",
						"1",
					),
				),
			},
			{
				Config: testAccVPNClientConfig_write_only_private_key(
					2,
					"uGEwDKZ2Hf2s2Dg59c9K+qYzJEBN5s8fNWVTxZx9kUo=",
				),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckNoResourceAttr(
						"unifi_vpn_client.test",
						"wireguard.private_key",
					),
					resource.TestCheckNoResourceAttr(
						"unifi_vpn_client.test",
						"wireguard.private_key_wo",
					),
					resource.TestCheckResourceAttr(
						"unifi_vpn_client.test",
						"wireguard.private_key_wo_version",
						"2",
					),
				),
			},
		},
	})
}

func testAccVPNClientConfig_write_only_private_key(version int, privateKey string) string {
	return fmt.Sprintf(`
resource "unifi_vpn_client" "test" {
  name          = "test-wireguard-write-only"
  enabled       = false
  subnet        = "10.0.3.2/24"
  default_route = false
  pull_dns      = false

  wireguard = {
    private_key_wo         = %q
    private_key_wo_version = %d
    interface              = "wan"
    dns_servers            = ["8.8.8.8"]

    peer = {
      ip         = "192.0.2.1"
      port       = 51820
      public_key = "7B+2Z3odPbDNsfVr+F8invj6/mBKLVaolOHXZoCaBA0="
    }
  }
}
`, privateKey, version)
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
		"private_key", "private_key_wo", "private_key_wo_version", "configuration", "peer",
		"preshared_key_enabled", "preshared_key", "interface", "dns_servers",
	} {
		if _, ok := got[key]; !ok {
			t.Errorf("missing key %q in AttributeTypes()", key)
		}
	}
}

// Test_vpnClientResource_privateKeyWriteOnlySchema pins the schema graft:
// private_key_wo is write-only, sensitive and optional; private_key_wo_version
// is an optional int64; and private_key itself is Optional (not Required),
// since private_key_wo is now a second way to supply it.
func Test_vpnClientResource_privateKeyWriteOnlySchema(t *testing.T) {
	ctx := context.Background()
	r := &vpnClientResource{}
	resp := &fwresource.SchemaResponse{}
	r.Schema(ctx, fwresource.SchemaRequest{}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("schema diagnostics: %v", resp.Diagnostics)
	}

	wireguard, ok := resp.Schema.Attributes["wireguard"].(rschema.SingleNestedAttribute)
	if !ok {
		t.Fatal("wireguard is not a single nested attribute")
	}
	privateKeyWO, ok := wireguard.Attributes["private_key_wo"].(rschema.StringAttribute)
	if !ok {
		t.Fatal("wireguard.private_key_wo is not a string attribute")
	}
	if !privateKeyWO.WriteOnly || !privateKeyWO.Sensitive || !privateKeyWO.Optional {
		t.Fatalf("private_key_wo flags = write-only:%t sensitive:%t optional:%t",
			privateKeyWO.WriteOnly, privateKeyWO.Sensitive, privateKeyWO.Optional)
	}
	privateKeyWOVersion, ok := wireguard.Attributes["private_key_wo_version"].(rschema.Int64Attribute)
	if !ok || !privateKeyWOVersion.Optional {
		t.Fatal("wireguard.private_key_wo_version is not an optional int64 attribute")
	}
	privateKey, ok := wireguard.Attributes["private_key"].(rschema.StringAttribute)
	if !ok {
		t.Fatal("wireguard.private_key is not a string attribute")
	}
	if privateKey.Required || !privateKey.Optional {
		t.Error("wireguard.private_key is still Required; private_key_wo has no other way in " +
			"to supply the key")
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

// TestWireguardDNSServersFillTwoSlotsInOrder pins the two-slot fill: the
// controller carries two dhcpd_dns_N wires for wireguard.dns_servers, and a
// config listing two servers must reach both, in order.
func TestWireguardDNSServersFillTwoSlotsInOrder(t *testing.T) {
	network := &unifi.Network{}
	wireguardDNSServersToNetwork([]string{"1.1.1.1", "2.2.2.2"}, network)

	if network.DHCPDDNS1 == nil || *network.DHCPDDNS1 != "1.1.1.1" {
		t.Errorf("DHCPDDNS1 = %v, want %q", network.DHCPDDNS1, "1.1.1.1")
	}
	if network.DHCPDDNS2 == nil || *network.DHCPDDNS2 != "2.2.2.2" {
		t.Errorf("DHCPDDNS2 = %v, want %q", network.DHCPDDNS2, "2.2.2.2")
	}
}

// TestWireguardDNSServersClearTheSlotDropped pins the clearing half: a slot
// the controller currently fills and the new list no longer reaches gets an
// explicit "" so the write clears it, not nil -- go-unifi's masked write
// synthesizes JSON null for a nil pointer named in the mask, and null is not
// "leave alone". The slot the new list still covers is left for Encode to
// write.
func TestWireguardDNSServersClearTheSlotDropped(t *testing.T) {
	current := &unifi.Network{
		DHCPDDNS1: strPtr("1.1.1.1"),
		DHCPDDNS2: strPtr("2.2.2.2"),
	}

	dropped := wireguardDNSServersClearDropped([]string{"9.9.9.9"}, current)

	if len(dropped) != 1 || dropped[0] != "dhcpd_dns_2" {
		t.Fatalf("dropped wires = %v, want exactly [dhcpd_dns_2]", dropped)
	}
	if current.DHCPDDNS2 == nil || *current.DHCPDDNS2 != "" {
		t.Errorf("DHCPDDNS2 = %v, want a non-nil pointer to \"\"", current.DHCPDDNS2)
	}
	// The slot the new list still covers is left for Encode to write; this
	// helper only ever adds to what Encode already decided.
	if current.DHCPDDNS1 == nil || *current.DHCPDDNS1 != "1.1.1.1" {
		t.Errorf("DHCPDDNS1 = %v, want untouched at \"1.1.1.1\"", current.DHCPDDNS1)
	}
}

// TestWireguardDNSServersClearDroppedTouchesNothingWhenNothingDropped is the
// control: a plan that still covers every slot prior filled must leave the
// mask unchanged, or the drop check above is not proof of anything.
func TestWireguardDNSServersClearDroppedTouchesNothingWhenNothingDropped(t *testing.T) {
	current := &unifi.Network{
		DHCPDDNS1: strPtr("1.1.1.1"),
		DHCPDDNS2: strPtr("2.2.2.2"),
	}

	dropped := wireguardDNSServersClearDropped([]string{"1.1.1.1", "2.2.2.2"}, current)

	if len(dropped) != 0 {
		t.Errorf("dropped wires = %v, want none; the new list still covers both slots", dropped)
	}
}
