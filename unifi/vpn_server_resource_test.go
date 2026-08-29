package unifi

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework-nettypes/cidrtypes"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	fwlist "github.com/hashicorp/terraform-plugin-framework/list"
	"github.com/hashicorp/terraform-plugin-framework/path"
	fwresource "github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/querycheck"
	"github.com/hashicorp/terraform-plugin-testing/tfversion"
	"github.com/ubiquiti-community/go-unifi/unifi"
)

func TestAccVPNServer_wireguard_basic(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccVPNServerConfig_wireguard_basic(),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"unifi_vpn_server.test",
						"name",
						"tfacc-wg-server",
					),
					resource.TestCheckResourceAttr("unifi_vpn_server.test", "enabled", "true"),
					resource.TestCheckResourceAttr(
						"unifi_vpn_server.test",
						"subnet",
						"10.100.0.1/24",
					),
					resource.TestCheckResourceAttr(
						"unifi_vpn_server.test",
						"wireguard.port",
						"51820",
					),
				),
			},
			{
				ResourceName:      "unifi_vpn_server.test",
				ImportState:       true,
				ImportStateVerify: true,
				ImportStateVerifyIgnore: []string{
					"wireguard.private_key",
				},
			},
		},
	})
}

func TestAccVPNServer_wireguard_update(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccVPNServerConfig_wireguard_update_before(),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"unifi_vpn_server.test",
						"name",
						"tfacc-wg-update",
					),
					resource.TestCheckResourceAttr(
						"unifi_vpn_server.test",
						"subnet",
						"10.101.0.1/24",
					),
					resource.TestCheckResourceAttr(
						"unifi_vpn_server.test",
						"wireguard.port",
						"51820",
					),
				),
			},
			{
				Config: testAccVPNServerConfig_wireguard_update_after(),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"unifi_vpn_server.test",
						"name",
						"tfacc-wg-update-renamed",
					),
					resource.TestCheckResourceAttr(
						"unifi_vpn_server.test",
						"subnet",
						"10.102.0.1/24",
					),
					resource.TestCheckResourceAttr(
						"unifi_vpn_server.test",
						"wireguard.port",
						"51821",
					),
				),
			},
		},
	})
}

func TestAccVPNServer_wireguard_with_dns(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccVPNServerConfig_wireguard_with_dns_servers(
					"8.8.8.8", "8.8.4.4", "1.1.1.1",
				),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("unifi_vpn_server.test", "name", "tfacc-wg-dns"),
					resource.TestCheckResourceAttr("unifi_vpn_server.test", "dns.enabled", "true"),
					resource.TestCheckResourceAttr("unifi_vpn_server.test", "dns.servers.#", "3"),
					resource.TestCheckResourceAttr(
						"unifi_vpn_server.test",
						"dns.servers.0",
						"8.8.8.8",
					),
					resource.TestCheckResourceAttr(
						"unifi_vpn_server.test",
						"dns.servers.1",
						"8.8.4.4",
					),
					resource.TestCheckResourceAttr(
						"unifi_vpn_server.test",
						"dns.servers.2",
						"1.1.1.1",
					),
				),
			},
			{
				ResourceName:      "unifi_vpn_server.test",
				ImportState:       true,
				ImportStateVerify: true,
				ImportStateVerifyIgnore: []string{
					"wireguard.private_key",
				},
			},
			{
				// A server the config drops must be cleared on the controller,
				// not merely left out of state: the two slots holding
				// "8.8.4.4" and "1.1.1.1" have to come back empty, or the read
				// below shows the stale, compacted leftovers instead of one.
				Config: testAccVPNServerConfig_wireguard_with_dns_servers("8.8.8.8"),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("unifi_vpn_server.test", "dns.servers.#", "1"),
					resource.TestCheckResourceAttr(
						"unifi_vpn_server.test",
						"dns.servers.0",
						"8.8.8.8",
					),
				),
			},
			{
				// servers = [] clears the last slot too. The framework's own
				// post-apply plan check is what proves this doesn't perpetually
				// re-plan: an empty list reading back as null (Optional, not
				// Computed) would show a diff on every following plan even
				// though nothing changed.
				Config: testAccVPNServerConfig_wireguard_with_dns_servers(),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("unifi_vpn_server.test", "dns.servers.#", "0"),
				),
			},
		},
	})
}

func TestAccVPNServer_wireguard_disabled(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccVPNServerConfig_wireguard_disabled(),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"unifi_vpn_server.test",
						"name",
						"tfacc-wg-disabled",
					),
					resource.TestCheckResourceAttr("unifi_vpn_server.test", "enabled", "false"),
				),
			},
		},
	})
}

func TestAccVPNServer_l2tp_basic(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccVPNServerConfig_l2tp_basic(),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"unifi_vpn_server.test",
						"name",
						"tfacc-l2tp-server",
					),
					resource.TestCheckResourceAttr("unifi_vpn_server.test", "enabled", "true"),
					resource.TestCheckResourceAttr(
						"unifi_vpn_server.test",
						"subnet",
						"10.110.0.1/24",
					),
					resource.TestCheckResourceAttr(
						"unifi_vpn_server.test",
						"l2tp.allow_weak_ciphers",
						"false",
					),
				),
			},
			{
				ResourceName:      "unifi_vpn_server.test",
				ImportState:       true,
				ImportStateVerify: true,
				ImportStateVerifyIgnore: []string{
					"l2tp.pre_shared_key",
					"radiusprofile_id",
				},
			},
		},
	})
}

func TestAccVPNServer_l2tp_update(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccVPNServerConfig_l2tp_update_before(),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"unifi_vpn_server.test",
						"name",
						"tfacc-l2tp-update",
					),
					resource.TestCheckResourceAttr(
						"unifi_vpn_server.test",
						"l2tp.allow_weak_ciphers",
						"false",
					),
				),
			},
			{
				Config: testAccVPNServerConfig_l2tp_update_after(),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"unifi_vpn_server.test",
						"name",
						"tfacc-l2tp-update-renamed",
					),
					resource.TestCheckResourceAttr(
						"unifi_vpn_server.test",
						"l2tp.allow_weak_ciphers",
						"true",
					),
				),
			},
		},
	})
}

func TestAccVPNServer_openvpn_basic(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccVPNServerConfig_openvpn_basic(),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"unifi_vpn_server.test",
						"name",
						"tfacc-ovpn-server",
					),
					resource.TestCheckResourceAttr("unifi_vpn_server.test", "enabled", "true"),
					resource.TestCheckResourceAttr(
						"unifi_vpn_server.test",
						"subnet",
						"10.120.0.1/24",
					),
					resource.TestCheckResourceAttr("unifi_vpn_server.test", "openvpn.port", "1194"),
					resource.TestCheckResourceAttr(
						"unifi_vpn_server.test",
						"openvpn.mode",
						"server",
					),
					resource.TestCheckResourceAttr(
						"unifi_vpn_server.test",
						"openvpn.encryption_cipher",
						"AES_256_CBC",
					),
				),
			},
			{
				ResourceName:      "unifi_vpn_server.test",
				ImportState:       true,
				ImportStateVerify: true,
				ImportStateVerifyIgnore: []string{
					"radiusprofile_id",
					"openvpn.server_crt",
					"openvpn.server_key",
					"openvpn.dh_key",
					"openvpn.shared_client_key",
					"openvpn.shared_client_crt",
					"openvpn.auth_key",
					"openvpn.ca_crt",
					"openvpn.ca_key",
				},
			},
		},
	})
}

func TestAccVPNServer_openvpn_update(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccVPNServerConfig_openvpn_update_before(),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"unifi_vpn_server.test",
						"name",
						"tfacc-ovpn-update",
					),
					resource.TestCheckResourceAttr("unifi_vpn_server.test", "openvpn.port", "1194"),
					resource.TestCheckResourceAttr(
						"unifi_vpn_server.test",
						"openvpn.encryption_cipher",
						"AES_256_CBC",
					),
				),
			},
			{
				Config: testAccVPNServerConfig_openvpn_update_after(),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"unifi_vpn_server.test",
						"name",
						"tfacc-ovpn-update-renamed",
					),
					resource.TestCheckResourceAttr("unifi_vpn_server.test", "openvpn.port", "1195"),
					resource.TestCheckResourceAttr(
						"unifi_vpn_server.test",
						"openvpn.encryption_cipher",
						"AES_256_CBC",
					),
				),
			},
		},
	})
}

func TestAccVPNServer_wireguard_custom_wan(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccVPNServerConfig_wireguard_custom_wan(),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("unifi_vpn_server.test", "name", "tfacc-wg-wan"),
					resource.TestCheckResourceAttr("unifi_vpn_server.test", "wan.ip", "any"),
					resource.TestCheckResourceAttr(
						"unifi_vpn_server.test",
						"wan.interface",
						"wan2",
					),
				),
			},
			{
				ResourceName:      "unifi_vpn_server.test",
				ImportState:       true,
				ImportStateVerify: true,
				ImportStateVerifyIgnore: []string{
					"wireguard.private_key",
				},
			},
		},
	})
}

// --- Config helper functions ---

func testAccVPNServerConfig_wireguard_basic() string {
	return `
resource "unifi_vpn_server" "test" {
  name   = "tfacc-wg-server"
  subnet = "10.100.0.1/24"

  wireguard = {
    private_key = "WPiBa/Ak1W+8Sp8L5yvbyhHeRO2o5kJvihq2VtJ+kFg="
  }
}
`
}

func testAccVPNServerConfig_wireguard_update_before() string {
	return `
resource "unifi_vpn_server" "test" {
  name   = "tfacc-wg-update"
  subnet = "10.101.0.1/24"

  wireguard = {
    private_key = "WPiBa/Ak1W+8Sp8L5yvbyhHeRO2o5kJvihq2VtJ+kFg="
    port        = 51820
  }
}
`
}

func testAccVPNServerConfig_wireguard_update_after() string {
	return `
resource "unifi_vpn_server" "test" {
  name   = "tfacc-wg-update-renamed"
  subnet = "10.102.0.1/24"

  wireguard = {
    private_key = "WPiBa/Ak1W+8Sp8L5yvbyhHeRO2o5kJvihq2VtJ+kFg="
    port        = 51821
  }
}
`
}

// testAccVPNServerConfig_wireguard_with_dns_servers parameterizes the DNS
// acceptance config on the server list, so the same config builds both the
// initial create and a later update that drops some of them.
func testAccVPNServerConfig_wireguard_with_dns_servers(servers ...string) string {
	quoted := make([]string, len(servers))
	for i, server := range servers {
		quoted[i] = fmt.Sprintf("%q", server)
	}
	return fmt.Sprintf(`
resource "unifi_vpn_server" "test" {
  name   = "tfacc-wg-dns"
  subnet = "10.103.0.1/24"

  dns = {
    servers = [%s]
  }

  wireguard = {
    private_key = "WPiBa/Ak1W+8Sp8L5yvbyhHeRO2o5kJvihq2VtJ+kFg="
  }
}
`, strings.Join(quoted, ", "))
}

func testAccVPNServerConfig_wireguard_disabled() string {
	return `
resource "unifi_vpn_server" "test" {
  name    = "tfacc-wg-disabled"
  subnet  = "10.104.0.1/24"
  enabled = false

  wireguard = {
    private_key = "WPiBa/Ak1W+8Sp8L5yvbyhHeRO2o5kJvihq2VtJ+kFg="
  }
}
`
}

func testAccVPNServerConfig_l2tp_basic() string {
	return `
resource "unifi_radius_profile" "test" {
  name = "tfacc-l2tp-radius"

  auth_server {
    ip       = "192.168.1.100"
    port     = 1812
    secret   = "radius-secret"
  }
}

resource "unifi_vpn_server" "test" {
  name             = "tfacc-l2tp-server"
  subnet           = "10.110.0.1/24"
  radiusprofile_id = unifi_radius_profile.test.id

  l2tp = {
    pre_shared_key = "tfacc-l2tp-psk-secret"
  }
}
`
}

func testAccVPNServerConfig_l2tp_update_before() string {
	return `
resource "unifi_radius_profile" "test" {
  name = "tfacc-l2tp-upd-radius"

  auth_server {
    ip       = "192.168.1.100"
    port     = 1812
    secret   = "radius-secret"
  }
}

resource "unifi_vpn_server" "test" {
  name             = "tfacc-l2tp-update"
  subnet           = "10.111.0.1/24"
  radiusprofile_id = unifi_radius_profile.test.id

  l2tp = {
    pre_shared_key     = "tfacc-l2tp-psk-secret"
    allow_weak_ciphers = false
  }
}
`
}

func testAccVPNServerConfig_l2tp_update_after() string {
	return `
resource "unifi_radius_profile" "test" {
  name = "tfacc-l2tp-upd-radius"

  auth_server {
    ip       = "192.168.1.100"
    port     = 1812
    secret   = "radius-secret"
  }
}

resource "unifi_vpn_server" "test" {
  name             = "tfacc-l2tp-update-renamed"
  subnet           = "10.111.0.1/24"
  radiusprofile_id = unifi_radius_profile.test.id

  l2tp = {
    pre_shared_key     = "tfacc-l2tp-psk-secret"
    allow_weak_ciphers = true
  }
}
`
}

func testAccVPNServerConfig_openvpn_basic() string {
	return `
resource "unifi_radius_profile" "test" {
  name = "tfacc-ovpn-radius"

  auth_server {
    ip       = "192.168.1.100"
    port     = 1812
    secret   = "radius-secret"
  }
}

resource "unifi_vpn_server" "test" {
  name              = "tfacc-ovpn-server"
  subnet            = "10.120.0.1/24"
  radiusprofile_id  = unifi_radius_profile.test.id

  openvpn = {}
}
`
}

func testAccVPNServerConfig_openvpn_update_before() string {
	return `
resource "unifi_radius_profile" "test" {
  name = "tfacc-ovpn-upd-radius"

  auth_server {
    ip       = "192.168.1.100"
    port     = 1812
    secret   = "radius-secret"
  }
}

resource "unifi_vpn_server" "test" {
  name              = "tfacc-ovpn-update"
  subnet            = "10.121.0.1/24"
  radiusprofile_id  = unifi_radius_profile.test.id

  openvpn = {
    port              = 1194
    encryption_cipher = "AES_256_CBC"
  }
}
`
}

func testAccVPNServerConfig_openvpn_update_after() string {
	return `
resource "unifi_radius_profile" "test" {
  name = "tfacc-ovpn-upd-radius"

  auth_server {
    ip       = "192.168.1.100"
    port     = 1812
    secret   = "radius-secret"
  }
}

resource "unifi_vpn_server" "test" {
  name              = "tfacc-ovpn-update-renamed"
  subnet            = "10.121.0.1/24"
  radiusprofile_id  = unifi_radius_profile.test.id

  openvpn = {
    port              = 1195
    encryption_cipher = "AES_256_CBC"
  }
}
`
}

func testAccVPNServerConfig_wireguard_custom_wan() string {
	return `
resource "unifi_vpn_server" "test" {
  name   = "tfacc-wg-wan"
  subnet = "10.105.0.1/24"

  wan = {
    interface = "wan2"
  }

  wireguard = {
    private_key = "WPiBa/Ak1W+8Sp8L5yvbyhHeRO2o5kJvihq2VtJ+kFg="
  }
}
`
}

// TestGenerateWireGuardPrivateKey verifies the provider generates a valid
// base64 32-byte Curve25519 private key when the user omits one.
func TestGenerateWireGuardPrivateKey(t *testing.T) {
	k1, err := generateWireGuardPrivateKey()
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	raw, err := base64.StdEncoding.DecodeString(k1)
	if err != nil {
		t.Fatalf("key is not valid base64: %v", err)
	}
	if len(raw) != 32 {
		t.Errorf("key length = %d bytes, want 32", len(raw))
	}
	// Curve25519 clamping must be applied.
	if raw[0]&7 != 0 || raw[31]&128 != 0 || raw[31]&64 == 0 {
		t.Errorf("key is not WireGuard-clamped: %x", raw)
	}
	// Keys must be unique per call.
	k2, _ := generateWireGuardPrivateKey()
	if k1 == k2 {
		t.Error("two generated keys are identical")
	}
}

func TestNewVPNServerResource(t *testing.T) {
	r := NewVPNServerResource()
	if r == nil {
		t.Fatal("NewVPNServerResource() returned nil")
	}
	if _, ok := r.(fwresource.ResourceWithConfigure); !ok {
		t.Error("expected ResourceWithConfigure interface")
	}
	if _, ok := r.(fwresource.ResourceWithImportState); !ok {
		t.Error("expected ResourceWithImportState interface")
	}
}

func TestNewVPNServerListResource(t *testing.T) {
	r := NewVPNServerListResource()
	if r == nil {
		t.Fatal("NewVPNServerListResource() returned nil")
	}
}

func Test_vpnServerDNSModel_AttributeTypes(t *testing.T) {
	m := vpnServerDNSModel{}
	got := m.AttributeTypes()
	for _, key := range []string{"enabled", "servers"} {
		if _, ok := got[key]; !ok {
			t.Errorf("AttributeTypes() missing key %q", key)
		}
	}
	if got["enabled"] != types.BoolType {
		t.Errorf("enabled type = %v, want BoolType", got["enabled"])
	}
}

func Test_vpnServerWANModel_AttributeTypes(t *testing.T) {
	m := vpnServerWANModel{}
	got := m.AttributeTypes()
	for _, key := range []string{"ip", "interface"} {
		if _, ok := got[key]; !ok {
			t.Errorf("AttributeTypes() missing key %q", key)
		}
	}
}

func Test_vpnServerWireguardModel_AttributeTypes(t *testing.T) {
	m := vpnServerWireguardModel{}
	got := m.AttributeTypes()
	for _, key := range []string{"private_key", "public_key", "port"} {
		if _, ok := got[key]; !ok {
			t.Errorf("AttributeTypes() missing key %q", key)
		}
	}
	if got["port"] != types.Int64Type {
		t.Errorf("port type = %v, want Int64Type", got["port"])
	}
}

func Test_vpnServerL2TPModel_AttributeTypes(t *testing.T) {
	m := vpnServerL2TPModel{}
	got := m.AttributeTypes()
	for _, key := range []string{"allow_weak_ciphers", "pre_shared_key"} {
		if _, ok := got[key]; !ok {
			t.Errorf("AttributeTypes() missing key %q", key)
		}
	}
}

func Test_vpnServerOpenVPNModel_AttributeTypes(t *testing.T) {
	m := vpnServerOpenVPNModel{}
	got := m.AttributeTypes()
	for _, key := range []string{"port", "mode", "encryption_cipher", "server_crt", "server_key", "dh_key", "ca_crt", "ca_key"} {
		if _, ok := got[key]; !ok {
			t.Errorf("AttributeTypes() missing key %q", key)
		}
	}
}

func Test_vpnServerResource_IdentitySchema(t *testing.T) {
	r := newVPNServerKitResource()
	resp := &fwresource.IdentitySchemaResponse{}
	r.IdentitySchema(context.Background(), fwresource.IdentitySchemaRequest{}, resp)
	if resp.Diagnostics.HasError() {
		t.Errorf("IdentitySchema() produced errors: %v", resp.Diagnostics)
	}
	if _, ok := resp.IdentitySchema.Attributes["id"]; !ok {
		t.Error("IdentitySchema missing 'id' attribute")
	}
}

func Test_vpnServerResource_modelToNetwork(t *testing.T) {
	ctx := context.Background()

	t.Run("missing vpn type returns error", func(t *testing.T) {
		spec := vpnServerKitSpec()
		model := &vpnServerKitModel{
			Name:      types.StringValue("test"),
			Enabled:   types.BoolValue(true),
			Subnet:    cidrtypes.NewIPv4PrefixValue("10.100.0.1/24"),
			Wireguard: types.ObjectNull(vpnServerWireguardModel{}.AttributeTypes()),
			L2TP:      types.ObjectNull(vpnServerL2TPModel{}.AttributeTypes()),
			OpenVPN:   types.ObjectNull(vpnServerOpenVPNModel{}.AttributeTypes()),
			DNS:       types.ObjectNull(vpnServerDNSModel{}.AttributeTypes()),
			WAN:       types.ObjectNull(vpnServerWANModel{}.AttributeTypes()),
		}
		got, diags := spec.ToSDK(ctx, model)
		if !diags.HasError() {
			// vpn_type is set by BeforeSend, not ToSDK, so BeforeSend must
			// run for the discriminator to appear.
			diags.Append(spec.BeforeSend(ctx, model, model, vpnServerKitModel{}, got, nil)...)
		}
		if !diags.HasError() {
			t.Error("expected error for missing VPN type")
		}
		// ToSDK always builds an object; BeforeSend's diagnostic is what
		// stops the apply on error, not a nil return.
		if got == nil {
			t.Fatal("ToSDK returned no object at all, which it should never do")
		}
		if got.VPNType != nil {
			t.Errorf("VPNType = %q, want unset when no block is configured", *got.VPNType)
		}
	})

	t.Run("wireguard model sets vpn type", func(t *testing.T) {
		spec := vpnServerKitSpec()
		port := int64(51820)
		privKey := "WPiBa/Ak1W+8Sp8L5yvbyhHeRO2o5kJvihq2VtJ+kFg="
		wgModel := vpnServerWireguardModel{
			PrivateKey: types.StringValue(privKey),
			PublicKey:  types.StringNull(),
			Port:       types.Int64Value(port),
		}
		wgObj, d := types.ObjectValueFrom(ctx, vpnServerWireguardModel{}.AttributeTypes(), wgModel)
		if d.HasError() {
			t.Fatalf("building wireguard object: %v", d)
		}
		model := &vpnServerKitModel{
			Name:      types.StringValue("wg-server"),
			Enabled:   types.BoolValue(true),
			Subnet:    cidrtypes.NewIPv4PrefixValue("10.100.0.1/24"),
			Wireguard: wgObj,
			L2TP:      types.ObjectNull(vpnServerL2TPModel{}.AttributeTypes()),
			OpenVPN:   types.ObjectNull(vpnServerOpenVPNModel{}.AttributeTypes()),
			DNS:       types.ObjectNull(vpnServerDNSModel{}.AttributeTypes()),
			WAN:       types.ObjectNull(vpnServerWANModel{}.AttributeTypes()),
		}
		got, diags := spec.ToSDK(ctx, model)
		if !diags.HasError() {
			// vpn_type is set by BeforeSend, not ToSDK, so BeforeSend must
			// run for the discriminator to appear.
			diags.Append(spec.BeforeSend(ctx, model, model, vpnServerKitModel{}, got, nil)...)
		}
		if diags.HasError() {
			t.Fatalf("unexpected diags: %v", diags)
		}
		if got == nil {
			t.Fatal("expected non-nil network")
		}
		if got.VPNType == nil || *got.VPNType != "wireguard-server" {
			t.Errorf("VPNType = %v, want wireguard-server", got.VPNType)
		}
		if got.WireguardPrivateKey == nil || *got.WireguardPrivateKey != privKey {
			t.Errorf("WireguardPrivateKey = %v, want %q", got.WireguardPrivateKey, privKey)
		}
		if got.LocalPort == nil || *got.LocalPort != port {
			t.Errorf("LocalPort = %v, want %d", got.LocalPort, port)
		}
	})

	t.Run("l2tp model sets vpn type", func(t *testing.T) {
		spec := vpnServerKitSpec()
		l2tpModel := vpnServerL2TPModel{
			AllowWeakCiphers: types.BoolValue(false),
			PreSharedKey:     types.StringValue("my-psk"),
		}
		l2tpObj, d := types.ObjectValueFrom(ctx, vpnServerL2TPModel{}.AttributeTypes(), l2tpModel)
		if d.HasError() {
			t.Fatalf("building l2tp object: %v", d)
		}
		model := &vpnServerKitModel{
			Name:      types.StringValue("l2tp-server"),
			Enabled:   types.BoolValue(true),
			Subnet:    cidrtypes.NewIPv4PrefixValue("10.110.0.1/24"),
			Wireguard: types.ObjectNull(vpnServerWireguardModel{}.AttributeTypes()),
			L2TP:      l2tpObj,
			OpenVPN:   types.ObjectNull(vpnServerOpenVPNModel{}.AttributeTypes()),
			DNS:       types.ObjectNull(vpnServerDNSModel{}.AttributeTypes()),
			WAN:       types.ObjectNull(vpnServerWANModel{}.AttributeTypes()),
		}
		got, diags := spec.ToSDK(ctx, model)
		if !diags.HasError() {
			// vpn_type is set by BeforeSend, not ToSDK, so BeforeSend must
			// run for the discriminator to appear.
			diags.Append(spec.BeforeSend(ctx, model, model, vpnServerKitModel{}, got, nil)...)
		}
		if diags.HasError() {
			t.Fatalf("unexpected diags: %v", diags)
		}
		if got.VPNType == nil || *got.VPNType != "l2tp-server" {
			t.Errorf("VPNType = %v, want l2tp-server", got.VPNType)
		}
		if got.IPSecPreSharedKey == nil || *got.IPSecPreSharedKey != "my-psk" {
			t.Errorf("IPSecPreSharedKey = %v, want my-psk", got.IPSecPreSharedKey)
		}
	})
}

func Test_vpnServerResource_networkToModel(t *testing.T) {
	ctx := context.Background()

	t.Run("wireguard network populates wireguard block", func(t *testing.T) {
		spec := vpnServerKitSpec()
		vpnType := "wireguard-server"
		name := "wg-test"
		subnet := "10.100.0.1/24"
		port := int64(51820)
		privKey := "WPiBa/Ak1W+8Sp8L5yvbyhHeRO2o5kJvihq2VtJ+kFg="
		network := &unifi.Network{
			ID:                  "net-123",
			Name:                &name,
			Enabled:             true,
			IPSubnet:            &subnet,
			VPNType:             &vpnType,
			WireguardPrivateKey: &privKey,
			LocalPort:           &port,
		}
		var model vpnServerKitModel
		diags := spec.ToModel(ctx, network, &model, "default")
		diags.Append(spec.AfterReceive(ctx, network, &model, deref(&vpnServerKitModel{}), nil)...)
		if diags.HasError() {
			t.Fatalf("unexpected diags: %v", diags)
		}
		if model.ID.ValueString() != "net-123" {
			t.Errorf("ID = %q, want net-123", model.ID.ValueString())
		}
		if model.Name.ValueString() != "wg-test" {
			t.Errorf("Name = %q, want wg-test", model.Name.ValueString())
		}
		if !model.Enabled.ValueBool() {
			t.Error("Enabled should be true")
		}
		if model.Wireguard.IsNull() {
			t.Fatal("Wireguard block should not be null")
		}
		var wg vpnServerWireguardModel
		if d := model.Wireguard.As(
			ctx,
			&wg,
			struct{ UnhandledNullAsEmpty, UnhandledUnknownAsEmpty bool }{},
		); d.HasError() {
			t.Fatalf("reading wireguard: %v", d)
		}
		if wg.PrivateKey.ValueString() != privKey {
			t.Errorf("PrivateKey = %q, want %q", wg.PrivateKey.ValueString(), privKey)
		}
		if wg.Port.ValueInt64() != port {
			t.Errorf("Port = %d, want %d", wg.Port.ValueInt64(), port)
		}
		// L2TP and OpenVPN should be null for a wireguard server
		if !model.L2TP.IsNull() {
			t.Error("L2TP should be null for wireguard server")
		}
		if !model.OpenVPN.IsNull() {
			t.Error("OpenVPN should be null for wireguard server")
		}
	})

	t.Run("l2tp network preserves psk from prior state", func(t *testing.T) {
		spec := vpnServerKitSpec()
		vpnType := "l2tp-server"
		name := "l2tp-test"
		subnet := "10.110.0.1/24"
		network := &unifi.Network{
			ID:       "net-456",
			Name:     &name,
			Enabled:  true,
			IPSubnet: &subnet,
			VPNType:  &vpnType,
			// IPSecPreSharedKey is nil (API does not return it on read)
		}

		priorL2TP := vpnServerL2TPModel{
			AllowWeakCiphers: types.BoolValue(false),
			PreSharedKey:     types.StringValue("stored-psk"),
		}
		priorL2TPObj, d := types.ObjectValueFrom(
			ctx,
			vpnServerL2TPModel{}.AttributeTypes(),
			priorL2TP,
		)
		if d.HasError() {
			t.Fatalf("building prior l2tp: %v", d)
		}
		priorState := &vpnServerKitModel{
			L2TP: priorL2TPObj,
		}

		var model vpnServerKitModel
		diags := spec.ToModel(ctx, network, &model, "default")
		diags.Append(spec.AfterReceive(ctx, network, &model, deref(priorState), nil)...)
		if diags.HasError() {
			t.Fatalf("unexpected diags: %v", diags)
		}
		if model.L2TP.IsNull() {
			t.Fatal("L2TP block should not be null")
		}
		var l2tp vpnServerL2TPModel
		if d := model.L2TP.As(
			ctx,
			&l2tp,
			struct{ UnhandledNullAsEmpty, UnhandledUnknownAsEmpty bool }{},
		); d.HasError() {
			t.Fatalf("reading l2tp: %v", d)
		}
		if l2tp.PreSharedKey.ValueString() != "stored-psk" {
			t.Errorf(
				"PreSharedKey = %q, want stored-psk (preserved from prior state)",
				l2tp.PreSharedKey.ValueString(),
			)
		}
	})
}

func Test_vpnServerResource_ListResourceConfigSchema(t *testing.T) {
	r := newVPNServerKitResource()
	resp := &fwlist.ListResourceSchemaResponse{}
	r.ListResourceConfigSchema(context.Background(), fwlist.ListResourceSchemaRequest{}, resp)
	if resp.Diagnostics.HasError() {
		t.Errorf("ListResourceConfigSchema() produced errors: %v", resp.Diagnostics)
	}
	if _, ok := resp.Schema.Attributes["site"]; !ok {
		t.Error("ListResourceConfigSchema missing 'site' attribute")
	}
}

func Test_generateWireGuardPrivateKey(t *testing.T) {
	got, err := generateWireGuardPrivateKey()
	if err != nil {
		t.Fatalf("generateWireGuardPrivateKey() error = %v", err)
	}
	if got == "" {
		t.Error("generateWireGuardPrivateKey() returned empty string")
	}
	raw, err := base64.StdEncoding.DecodeString(got)
	if err != nil {
		t.Fatalf("result is not valid base64: %v", err)
	}
	if len(raw) != 32 {
		t.Errorf("key length = %d bytes, want 32", len(raw))
	}
}

func TestAccVPNServerList_basic(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		TerraformVersionChecks: []tfversion.TerraformVersionCheck{
			tfversion.SkipBelow(tfversion.Version1_14_0),
		},
		Steps: []resource.TestStep{
			{
				Config: testAccVPNServerConfig_wireguard_basic(),
			},
			{
				Query: true,
				Config: `
					provider "unifi" {}
					list "unifi_vpn_server" "test" {
						provider = unifi
						config {
							filter {
								name  = "name"
								value = "tfacc-wg-server"
						  }
					  }
					}
				`,
				QueryResultChecks: []querycheck.QueryResultCheck{
					querycheck.ExpectLengthAtLeast("unifi_vpn_server.test", 1),
				},
			},
		},
	})
}

// deref adapts the old networkToModel prior-state pointer to AfterReceive's
// value parameter. A nil prior means no prior state, which is the zero model.
func deref(prior *vpnServerKitModel) vpnServerKitModel {
	if prior == nil {
		return vpnServerKitModel{}
	}
	return *prior
}

// TestVPNServerDNSServersFillFourSlotsInOrder pins the four-slot lift: the
// controller carries four dhcpd_dns_N wires, not two, and a config listing
// four servers must reach all of them, in order.
func TestVPNServerDNSServersFillFourSlotsInOrder(t *testing.T) {
	network := &unifi.Network{}
	vpnServerDNSServersToNetwork(
		[]string{"1.1.1.1", "2.2.2.2", "3.3.3.3", "4.4.4.4"},
		network,
	)

	for i, want := range []struct {
		got  *string
		name string
	}{
		{network.DHCPDDNS1, "DHCPDDNS1"},
		{network.DHCPDDNS2, "DHCPDDNS2"},
		{network.DHCPDDNS3, "DHCPDDNS3"},
		{network.DHCPDDNS4, "DHCPDDNS4"},
	} {
		wantValue := []string{"1.1.1.1", "2.2.2.2", "3.3.3.3", "4.4.4.4"}[i]
		if want.got == nil || *want.got != wantValue {
			t.Errorf("%s = %v, want %q", want.name, want.got, wantValue)
		}
	}
}

// TestVPNServerDNSServersClearTheSlotsAConfigDropped pins the clearing half:
// a slot the controller currently fills and the new list no longer reaches
// gets an explicit "" so the write clears it, not nil -- go-unifi's masked
// write synthesizes JSON null for a nil pointer named in the mask, and null
// is not "leave alone". A slot neither side ever filled stays nil and is
// reported to nobody, so the write says nothing about it.
func TestVPNServerDNSServersClearTheSlotsAConfigDropped(t *testing.T) {
	current := &unifi.Network{
		DHCPDDNS1: strPtr("1.1.1.1"),
		DHCPDDNS2: strPtr("2.2.2.2"),
		DHCPDDNS3: strPtr("3.3.3.3"),
		// DHCPDDNS4 was never configured; stays nil.
	}

	dropped := vpnServerDNSServersClearDropped([]string{"9.9.9.9"}, current)

	wantDropped := map[string]bool{"dhcpd_dns_2": true, "dhcpd_dns_3": true}
	if len(dropped) != len(wantDropped) {
		t.Fatalf("dropped wires = %v, want exactly %v", dropped, wantDropped)
	}
	for _, wire := range dropped {
		if !wantDropped[wire] {
			t.Errorf("unexpected wire %q in dropped", wire)
		}
	}

	if current.DHCPDDNS2 == nil || *current.DHCPDDNS2 != "" {
		t.Errorf("DHCPDDNS2 = %v, want a non-nil pointer to \"\"", current.DHCPDDNS2)
	}
	if current.DHCPDDNS3 == nil || *current.DHCPDDNS3 != "" {
		t.Errorf("DHCPDDNS3 = %v, want a non-nil pointer to \"\"", current.DHCPDDNS3)
	}
	if current.DHCPDDNS4 != nil {
		t.Errorf("DHCPDDNS4 = %v, want nil (neither side ever filled it)", current.DHCPDDNS4)
	}
	// The slot the new list still covers is left for Encode to write; this
	// helper only ever adds to what Encode already decided.
	if current.DHCPDDNS1 == nil || *current.DHCPDDNS1 != "1.1.1.1" {
		t.Errorf("DHCPDDNS1 = %v, want untouched at \"1.1.1.1\"", current.DHCPDDNS1)
	}
}

// TestVPNServerDNSServersFromNetworkReadsBackEmptyNotNullWhenConfigured pins
// the read-side half of "an explicit list, even an empty one, is
// authoritative": a controller reporting all four slots empty reads back as
// an empty, non-null list when the prior config named dns.servers at all
// (even as []), and as null when it never did. Without this, `servers = []`
// clears every slot (measured live) but state comes back null; since
// dns.servers is Optional, not Computed, the next plan disagrees with state
// forever.
func TestVPNServerDNSServersFromNetworkReadsBackEmptyNotNullWhenConfigured(t *testing.T) {
	ctx := context.Background()
	network := &unifi.Network{} // every slot nil: the controller reports none.

	emptyServers, d := types.ListValueFrom(ctx, types.StringType, []string{})
	if d.HasError() {
		t.Fatalf("building an empty servers list: %v", d)
	}

	t.Run("prior servers was an explicit empty list", func(t *testing.T) {
		prior, d := types.ObjectValueFrom(ctx, vpnServerDNSModel{}.AttributeTypes(), vpnServerDNSModel{
			Enabled: types.BoolValue(true),
			Servers: emptyServers,
		})
		if d.HasError() {
			t.Fatalf("building prior: %v", d)
		}

		var diags diag.Diagnostics
		got := vpnServerDNSServersFromNetwork(ctx, &diags, network, vpnServerDNSServersTouched(prior))
		if diags.HasError() {
			t.Fatalf("decoding: %v", diags)
		}
		if got.IsNull() {
			t.Error("servers = null, want a non-null empty list")
		}
		if len(got.Elements()) != 0 {
			t.Errorf("servers has %d elements, want 0", len(got.Elements()))
		}
	})

	t.Run("prior servers was never configured", func(t *testing.T) {
		prior := types.ObjectNull(vpnServerDNSModel{}.AttributeTypes())

		var diags diag.Diagnostics
		got := vpnServerDNSServersFromNetwork(ctx, &diags, network, vpnServerDNSServersTouched(prior))
		if diags.HasError() {
			t.Fatalf("decoding: %v", diags)
		}
		if !got.IsNull() {
			t.Errorf("servers = %v, want null", got)
		}
	})
}

// TestVPNServerDNSServersRejectsAFifthServerAtPlan pins the
// listvalidator.SizeAtMost(4) added alongside the four-slot lift: the
// controller carries exactly four dhcpd_dns_N slots, so a fifth server
// belongs in a plan-time error, not a silent truncation.
func TestVPNServerDNSServersRejectsAFifthServerAtPlan(t *testing.T) {
	ctx := context.Background()
	var response fwresource.SchemaResponse
	newVPNServerKitResource().Schema(ctx, fwresource.SchemaRequest{}, &response)
	if response.Diagnostics.HasError() {
		t.Fatalf("build the schema: %v", response.Diagnostics)
	}

	dnsAttr, ok := response.Schema.Attributes["dns"]
	if !ok {
		t.Fatal(`schema is missing attribute "dns"`)
	}
	dnsNested, ok := dnsAttr.(schema.SingleNestedAttribute)
	if !ok {
		t.Fatalf(`attribute "dns" is a %T, want schema.SingleNestedAttribute`, dnsAttr)
	}
	serversAttr, ok := dnsNested.Attributes["servers"]
	if !ok {
		t.Fatal(`dns is missing member "servers"`)
	}
	listAttr, ok := serversAttr.(schema.ListAttribute)
	if !ok {
		t.Fatalf(`dns.servers is a %T, want schema.ListAttribute`, serversAttr)
	}

	validateServers := func(t *testing.T, servers []string) diag.Diagnostics {
		t.Helper()
		list, d := types.ListValueFrom(ctx, types.StringType, servers)
		if d.HasError() {
			t.Fatalf("building the list value: %v", d)
		}
		var diags diag.Diagnostics
		for _, v := range listAttr.Validators {
			validateResp := &validator.ListResponse{}
			v.ValidateList(ctx, validator.ListRequest{
				Path:        path.Root("dns").AtName("servers"),
				ConfigValue: list,
			}, validateResp)
			diags.Append(validateResp.Diagnostics...)
		}
		return diags
	}

	if diags := validateServers(t, []string{
		"1.1.1.1", "2.2.2.2", "3.3.3.3", "4.4.4.4", "5.5.5.5",
	}); !diags.HasError() {
		t.Error("five servers passed config validation; want a plan-time error, since " +
			"the controller has only four dhcpd_dns_N slots to hold them")
	}
	// The control: four, the declared maximum, must still pass.
	if diags := validateServers(t, []string{
		"1.1.1.1", "2.2.2.2", "3.3.3.3", "4.4.4.4",
	}); diags.HasError() {
		t.Errorf("four servers failed config validation: %v", diags)
	}
}

// vpnServerDNSBlockAbsent describes a prior state with no dns block at all --
// the shape of a resource state before it ever recorded one, or an
// unpopulated import.
func vpnServerDNSBlockAbsent() types.Object {
	return types.ObjectNull(vpnServerDNSModel{}.AttributeTypes())
}

// vpnServerDNSBlockWithServers builds a dns block with servers set to
// exactly the given list, including an explicit empty one.
func vpnServerDNSBlockWithServers(t *testing.T, servers []string) types.Object {
	t.Helper()
	ctx := context.Background()
	list, d := types.ListValueFrom(ctx, types.StringType, servers)
	if d.HasError() {
		t.Fatalf("building servers list: %v", d)
	}
	object, d := types.ObjectValueFrom(ctx, vpnServerDNSModel{}.AttributeTypes(), vpnServerDNSModel{
		Enabled: types.BoolValue(true),
		Servers: list,
	})
	if d.HasError() {
		t.Fatalf("building dns object: %v", d)
	}
	return object
}

// vpnServerDNSBlockServersOmitted describes a dns block that is part of the
// apply (enabled is set) but doesn't mention servers at all -- distinct from
// vpnServerDNSBlockAbsent, whose whole block is null.
func vpnServerDNSBlockServersOmitted(t *testing.T) types.Object {
	t.Helper()
	object, d := types.ObjectValueFrom(context.Background(), vpnServerDNSModel{}.AttributeTypes(), vpnServerDNSModel{
		Enabled: types.BoolValue(true),
		Servers: types.ListNull(types.StringType),
	})
	if d.HasError() {
		t.Fatalf("building dns object: %v", d)
	}
	return object
}

// vpnServerMaskTestModel builds a full model around one dns block, with a
// wireguard block set (a private key) so every scenario has something else
// to keep the write mask non-empty regardless of what the dns scenario does
// -- matching final-review.md's Lens C setup ("wireguard, dns.enabled=true").
func vpnServerMaskTestModel(t *testing.T, dns types.Object) vpnServerKitModel {
	t.Helper()
	ctx := context.Background()
	wireguard, d := types.ObjectValueFrom(ctx, vpnServerWireguardModel{}.AttributeTypes(), vpnServerWireguardModel{
		PrivateKey: types.StringValue("WPiBa/Ak1W+8Sp8L5yvbyhHeRO2o5kJvihq2VtJ+kFg="),
		PublicKey:  types.StringNull(),
		Port:       types.Int64Null(),
	})
	if d.HasError() {
		t.Fatalf("building wireguard object: %v", d)
	}
	return vpnServerKitModel{
		Name:      types.StringValue("mask-scenario"),
		Enabled:   types.BoolValue(true),
		Subnet:    cidrtypes.NewIPv4PrefixValue("10.100.0.1/24"),
		DNS:       dns,
		WAN:       types.ObjectNull(vpnServerWANModel{}.AttributeTypes()),
		Wireguard: wireguard,
		L2TP:      types.ObjectNull(vpnServerL2TPModel{}.AttributeTypes()),
		OpenVPN:   types.ObjectNull(vpnServerOpenVPNModel{}.AttributeTypes()),
	}
}

// vpnServerRunDNSMaskScenario drives resourcekit's own Update() sequence for
// vpnServerKitSpec by hand -- ApplyPlanToState, WireFields from the raw
// plan, ToSDK from the merged state, BeforeSend, then UnwritableWires -- in
// the exact order internal/resourcekit.Resource.Update follows, without the
// tfprotov6 request/response plumbing around it. Returns the final wire
// mask as a set, and the SDK object BeforeSend/Encode left behind, so a
// caller can check exactly which DNS wires reached the write and what value
// each holds -- reproducing final-review.md's Lens C probe (a throwaway
// kit-level Update probe, deleted after the review) as a committed test.
func vpnServerRunDNSMaskScenario(
	t *testing.T, prior, plan vpnServerKitModel,
) (map[string]bool, *unifi.Network) {
	t.Helper()
	ctx := context.Background()
	spec := vpnServerKitSpec()

	// Captured before ApplyPlanToState overwrites state in place, exactly as
	// Update does -- this is the only point where the prior and the plan are
	// still two separate things.
	priorState := prior
	state := prior
	spec.ApplyPlanToState(&plan, &state)

	fields, err := spec.WireFields(&plan)
	if err != nil {
		t.Fatalf("WireFields: %v", err)
	}

	sdk, diags := spec.ToSDK(ctx, &state)
	if diags.HasError() {
		t.Fatalf("ToSDK: %v", diags)
	}

	diags = spec.BeforeSend(ctx, &plan, &state, priorState, sdk, nil)
	if diags.HasError() {
		t.Fatalf("BeforeSend: %v", diags)
	}

	if spec.UnwritableWires != nil {
		unwritable := make(map[string]struct{})
		for _, name := range spec.UnwritableWires(sdk) {
			unwritable[name] = struct{}{}
		}
		kept := fields[:0:0]
		for _, name := range fields {
			if _, drop := unwritable[name]; !drop {
				kept = append(kept, name)
			}
		}
		fields = kept
	}

	mask := make(map[string]bool, len(fields))
	for _, name := range fields {
		mask[name] = true
	}
	return mask, sdk
}

// vpnServerAssertDNSWire checks one dhcpd_dns_N wire against a scenario's
// mask and the SDK value left behind. wantValue nil means "must be absent
// from the mask" -- current's Go value is not checked in that case, since a
// wire outside the mask is never sent regardless of what it holds. A
// non-nil wantValue means "must be in the mask, with exactly this value",
// which also covers the deliberate "" clear.
func vpnServerAssertDNSWire(t *testing.T, mask map[string]bool, current *string, wire string, wantValue *string) {
	t.Helper()
	inMask := mask[wire]
	wantInMask := wantValue != nil
	if inMask != wantInMask {
		t.Errorf("%s in mask = %v, want %v", wire, inMask, wantInMask)
		return
	}
	if !wantInMask {
		return
	}
	if current == nil || *current != *wantValue {
		t.Errorf("%s = %v, want a pointer to %q", wire, current, *wantValue)
	}
}

// TestVPNServerDNSMaskAcrossPriorAndPlanShapes pins final-review.md's Lens C
// table: the joint decision of ConditionalWires (mask from the raw plan),
// vpnServerDNSServersClearDropped (the prior/plan comparison) and
// vpnServerUnwritableWires (the nil-narrowing) has to produce, for every
// prior/plan shape, exactly the kept slots with values, the dropped slots
// as a deliberate non-nil "", and nothing else. An acceptance test can only
// see the read-back result, which can't tell "wire absent from the mask"
// from "wire sent as an explicit JSON null" (both read back empty) -- this
// test inspects the mask directly instead.
func TestVPNServerDNSMaskAcrossPriorAndPlanShapes(t *testing.T) {
	strp := func(s string) *string { return &s }

	t.Run("prior 3, plan 1 -- drops the two the new list no longer covers", func(t *testing.T) {
		prior := vpnServerMaskTestModel(t, vpnServerDNSBlockWithServers(t,
			[]string{"1.1.1.1", "2.2.2.2", "3.3.3.3"}))
		plan := vpnServerMaskTestModel(t, vpnServerDNSBlockWithServers(t, []string{"1.1.1.1"}))

		mask, sdk := vpnServerRunDNSMaskScenario(t, prior, plan)

		vpnServerAssertDNSWire(t, mask, sdk.DHCPDDNS1, "dhcpd_dns_1", strp("1.1.1.1"))
		vpnServerAssertDNSWire(t, mask, sdk.DHCPDDNS2, "dhcpd_dns_2", strp(""))
		vpnServerAssertDNSWire(t, mask, sdk.DHCPDDNS3, "dhcpd_dns_3", strp(""))
		vpnServerAssertDNSWire(t, mask, sdk.DHCPDDNS4, "dhcpd_dns_4", nil)
	})

	t.Run("prior 3, plan omits servers -- DNS untouched, no dns wire at all", func(t *testing.T) {
		prior := vpnServerMaskTestModel(t, vpnServerDNSBlockWithServers(t,
			[]string{"1.1.1.1", "2.2.2.2", "3.3.3.3"}))
		plan := vpnServerMaskTestModel(t, vpnServerDNSBlockServersOmitted(t))

		mask, sdk := vpnServerRunDNSMaskScenario(t, prior, plan)

		vpnServerAssertDNSWire(t, mask, sdk.DHCPDDNS1, "dhcpd_dns_1", nil)
		vpnServerAssertDNSWire(t, mask, sdk.DHCPDDNS2, "dhcpd_dns_2", nil)
		vpnServerAssertDNSWire(t, mask, sdk.DHCPDDNS3, "dhcpd_dns_3", nil)
		vpnServerAssertDNSWire(t, mask, sdk.DHCPDDNS4, "dhcpd_dns_4", nil)
	})

	t.Run("prior 3, plan [] -- clears every slot prior held", func(t *testing.T) {
		prior := vpnServerMaskTestModel(t, vpnServerDNSBlockWithServers(t,
			[]string{"1.1.1.1", "2.2.2.2", "3.3.3.3"}))
		plan := vpnServerMaskTestModel(t, vpnServerDNSBlockWithServers(t, []string{}))

		mask, sdk := vpnServerRunDNSMaskScenario(t, prior, plan)

		vpnServerAssertDNSWire(t, mask, sdk.DHCPDDNS1, "dhcpd_dns_1", strp(""))
		vpnServerAssertDNSWire(t, mask, sdk.DHCPDDNS2, "dhcpd_dns_2", strp(""))
		vpnServerAssertDNSWire(t, mask, sdk.DHCPDDNS3, "dhcpd_dns_3", strp(""))
		vpnServerAssertDNSWire(t, mask, sdk.DHCPDDNS4, "dhcpd_dns_4", nil)
	})

	t.Run("prior 3, plan the same 3 -- nothing dropped", func(t *testing.T) {
		prior := vpnServerMaskTestModel(t, vpnServerDNSBlockWithServers(t,
			[]string{"1.1.1.1", "2.2.2.2", "3.3.3.3"}))
		plan := vpnServerMaskTestModel(t, vpnServerDNSBlockWithServers(t,
			[]string{"1.1.1.1", "2.2.2.2", "3.3.3.3"}))

		mask, sdk := vpnServerRunDNSMaskScenario(t, prior, plan)

		vpnServerAssertDNSWire(t, mask, sdk.DHCPDDNS1, "dhcpd_dns_1", strp("1.1.1.1"))
		vpnServerAssertDNSWire(t, mask, sdk.DHCPDDNS2, "dhcpd_dns_2", strp("2.2.2.2"))
		vpnServerAssertDNSWire(t, mask, sdk.DHCPDDNS3, "dhcpd_dns_3", strp("3.3.3.3"))
		vpnServerAssertDNSWire(t, mask, sdk.DHCPDDNS4, "dhcpd_dns_4", nil)
	})

	t.Run("prior 1, plan 3 -- growing needs no clear", func(t *testing.T) {
		prior := vpnServerMaskTestModel(t, vpnServerDNSBlockWithServers(t, []string{"1.1.1.1"}))
		plan := vpnServerMaskTestModel(t, vpnServerDNSBlockWithServers(t,
			[]string{"1.1.1.1", "2.2.2.2", "3.3.3.3"}))

		mask, sdk := vpnServerRunDNSMaskScenario(t, prior, plan)

		vpnServerAssertDNSWire(t, mask, sdk.DHCPDDNS1, "dhcpd_dns_1", strp("1.1.1.1"))
		vpnServerAssertDNSWire(t, mask, sdk.DHCPDDNS2, "dhcpd_dns_2", strp("2.2.2.2"))
		vpnServerAssertDNSWire(t, mask, sdk.DHCPDDNS3, "dhcpd_dns_3", strp("3.3.3.3"))
		vpnServerAssertDNSWire(t, mask, sdk.DHCPDDNS4, "dhcpd_dns_4", nil)
	})

	t.Run("prior 4, plan 2 -- clears both trailing slots", func(t *testing.T) {
		prior := vpnServerMaskTestModel(t, vpnServerDNSBlockWithServers(t,
			[]string{"1.1.1.1", "2.2.2.2", "3.3.3.3", "4.4.4.4"}))
		plan := vpnServerMaskTestModel(t, vpnServerDNSBlockWithServers(t,
			[]string{"1.1.1.1", "9.9.9.9"}))

		mask, sdk := vpnServerRunDNSMaskScenario(t, prior, plan)

		vpnServerAssertDNSWire(t, mask, sdk.DHCPDDNS1, "dhcpd_dns_1", strp("1.1.1.1"))
		vpnServerAssertDNSWire(t, mask, sdk.DHCPDDNS2, "dhcpd_dns_2", strp("9.9.9.9"))
		vpnServerAssertDNSWire(t, mask, sdk.DHCPDDNS3, "dhcpd_dns_3", strp(""))
		vpnServerAssertDNSWire(t, mask, sdk.DHCPDDNS4, "dhcpd_dns_4", strp(""))
	})

	t.Run("prior has no dns block at all (import), plan 1 -- nothing to clear", func(t *testing.T) {
		prior := vpnServerMaskTestModel(t, vpnServerDNSBlockAbsent())
		plan := vpnServerMaskTestModel(t, vpnServerDNSBlockWithServers(t, []string{"1.1.1.1"}))

		mask, sdk := vpnServerRunDNSMaskScenario(t, prior, plan)

		vpnServerAssertDNSWire(t, mask, sdk.DHCPDDNS1, "dhcpd_dns_1", strp("1.1.1.1"))
		vpnServerAssertDNSWire(t, mask, sdk.DHCPDDNS2, "dhcpd_dns_2", nil)
		vpnServerAssertDNSWire(t, mask, sdk.DHCPDDNS3, "dhcpd_dns_3", nil)
		vpnServerAssertDNSWire(t, mask, sdk.DHCPDDNS4, "dhcpd_dns_4", nil)
	})
}
