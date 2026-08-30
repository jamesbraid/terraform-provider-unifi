package unifi

import (
	"context"
	"fmt"
	"os"
	"testing"

	fwresource "github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
)

func TestAccSettingResource_mgmt(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccSettingConfig_mgmt(),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"unifi_setting.test",
						"mgmt.auto_upgrade",
						"true",
					),
					resource.TestCheckResourceAttr(
						"unifi_setting.test",
						"mgmt.ssh_enabled",
						"false",
					),
				),
			},
			{
				ResourceName:      "unifi_setting.test",
				ImportState:       true,
				ImportStateVerify: true,
				ImportStateVerifyIgnore: []string{
					"mgmt.%",
					"mgmt.auto_upgrade",
					"mgmt.ssh_enabled",
				},
			},
		},
	})
}

// TestAccSettingResource_mgmtMaskedWriteLeavesSiblingsAlone is R2-B part 1's
// spike gate: mgmt is served through resourcekit.SpecSection, whose Write
// sends UpdateSettingFields with a mask naming only the fields the plan
// set. led_enabled is never named -- the config here never mentions it --
// so setting it out of band, then applying a config change to a DIFFERENT
// mgmt attribute, must leave it alone. Before the spike, writeMgmtSection's
// read-modify-write would have carried whatever it fetched (a fetch that
// races the out-of-band write): either way this pins the new mechanism, not
// a coincidence of timing.
func TestAccSettingResource_mgmtMaskedWriteLeavesSiblingsAlone(t *testing.T) {
	const ledOutOfBand = true

	setLedEnabledOutOfBand := func() {
		client, site := probeClient(t)
		ctx := context.Background()
		_, mgmt, err := ui.GetSetting[*settings.Mgmt](client, ctx, site)
		if err != nil {
			t.Fatalf("reading mgmt out of band: %v", err)
		}
		mgmt.LedEnabled = ledOutOfBand
		if err := client.UpdateSetting(ctx, site, mgmt); err != nil {
			t.Fatalf("setting led_enabled out of band: %v", err)
		}
		_, back, err := ui.GetSetting[*settings.Mgmt](client, ctx, site)
		if err != nil {
			t.Fatalf("reading mgmt back out of band: %v", err)
		}
		if back.LedEnabled != ledOutOfBand {
			t.Fatalf("the controller did not store led_enabled=%v (got %v), so there is "+
				"nothing for the next apply to clobber and this test would pass vacuously",
				ledOutOfBand, back.LedEnabled)
		}
		t.Logf("POSITIVE CONTROL: controller holds led_enabled=%v before the apply "+
			"that changes a different mgmt attribute", back.LedEnabled)
	}

	checkLedSurvived := func(*terraform.State) error {
		client, site := probeClient(t)
		_, mgmt, err := ui.GetSetting[*settings.Mgmt](client, context.Background(), site)
		if err != nil {
			return err
		}
		if mgmt.LedEnabled != ledOutOfBand {
			return fmt.Errorf(
				"led_enabled = %v after an apply that only changed auto_upgrade; the "+
					"masked write named auto_upgrade alone and must not have touched this",
				mgmt.LedEnabled)
		}
		t.Logf("led_enabled survived the masked write as %v", mgmt.LedEnabled)
		return nil
	}

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: `
resource "unifi_setting" "test" {
  mgmt = {
    ssh_enabled = true
  }
}
`,
			},
			{
				PreConfig: setLedEnabledOutOfBand,
				Config: `
resource "unifi_setting" "test" {
  mgmt = {
    ssh_enabled  = true
    auto_upgrade = true
  }
}
`,
				Check: checkLedSurvived,
			},
		},
	})
}

func TestAccSettingResource_radius(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccSettingConfig_radius(),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"unifi_setting.test",
						"radius.accounting_enabled",
						"true",
					),
					resource.TestCheckResourceAttr(
						"unifi_setting.test",
						"radius.auth_port",
						"1812",
					),
					resource.TestCheckResourceAttr(
						"unifi_setting.test",
						"radius.acct_port",
						"1813",
					),
				),
			},
			{
				ResourceName:      "unifi_setting.test",
				ImportState:       true,
				ImportStateVerify: true,
				ImportStateVerifyIgnore: []string{
					// SpecSection.Read only fetches a section when Configured(plan)
					// is true; import's state has just id set, so radius reads
					// back null instead of the applied values.
					"radius.secret", // Secret is sensitive and won't be in state after import
					"radius.%",
					"radius.accounting_enabled",
					"radius.enabled",
					"radius.acct_port",
					"radius.auth_port",
					"radius.interim_update_interval",
				},
			},
		},
	})
}

func TestAccSettingResource_usg(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccSettingConfig_usg(),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"unifi_setting.test",
						"usg.ftp_module",
						"true",
					),
				),
			},
			{
				ResourceName:      "unifi_setting.test",
				ImportState:       true,
				ImportStateVerify: true,
				ImportStateVerifyIgnore: []string{
					"usg.%",
					"usg.ftp_module",
				},
			},
		},
	})
}

func TestAccSettingResource_combined(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccSettingConfig_combined(),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"unifi_setting.test",
						"mgmt.auto_upgrade",
						"true",
					),
					resource.TestCheckResourceAttr(
						"unifi_setting.test",
						"mgmt.ssh_enabled",
						"true",
					),
					resource.TestCheckResourceAttr(
						"unifi_setting.test",
						"radius.accounting_enabled",
						"false",
					),
					resource.TestCheckResourceAttr(
						"unifi_setting.test",
						"usg.ftp_module",
						"false",
					),
				),
			},
			{
				Config: testAccSettingConfig_combinedUpdate(),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"unifi_setting.test",
						"mgmt.auto_upgrade",
						"false",
					),
					resource.TestCheckResourceAttr(
						"unifi_setting.test",
						"mgmt.ssh_enabled",
						"false",
					),
					resource.TestCheckResourceAttr(
						"unifi_setting.test",
						"radius.accounting_enabled",
						"true",
					),
					resource.TestCheckResourceAttr(
						"unifi_setting.test",
						"usg.ftp_module",
						"true",
					),
				),
			},
		},
	})
}

func TestAccSettingResource_sshKeys(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccSettingConfig_sshKeys(),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"unifi_setting.test",
						"mgmt.ssh_enabled",
						"true",
					),
					resource.TestCheckResourceAttr("unifi_setting.test", "mgmt.ssh_keys.#", "1"),
					resource.TestCheckResourceAttr(
						"unifi_setting.test",
						"mgmt.ssh_keys.0.name",
						"test-key",
					),
					resource.TestCheckResourceAttr(
						"unifi_setting.test",
						"mgmt.ssh_keys.0.type",
						"ssh-rsa",
					),
					resource.TestCheckResourceAttr(
						"unifi_setting.test",
						"mgmt.ssh_keys.0.comment",
						"Test SSH Key",
					),
				),
			},
			{
				Config: testAccSettingConfig_sshKeysUpdate(),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("unifi_setting.test", "mgmt.ssh_keys.#", "2"),
					resource.TestCheckResourceAttr(
						"unifi_setting.test",
						"mgmt.ssh_keys.0.name",
						"test-key",
					),
					resource.TestCheckResourceAttr(
						"unifi_setting.test",
						"mgmt.ssh_keys.1.name",
						"test-key-2",
					),
				),
			},
		},
	})
}

func testAccSettingConfig_mgmt() string {
	return `
resource "unifi_setting" "test" {
  mgmt = {
    auto_upgrade = true
    ssh_enabled  = false
  }
}
`
}

func testAccSettingConfig_radius() string {
	return `
resource "unifi_setting" "test" {
  radius = {
    accounting_enabled = true
    auth_port          = 1812
    acct_port          = 1813
    secret             = "test-secret-123"
  }
}
`
}

func testAccSettingConfig_usg() string {
	return `
resource "unifi_setting" "test" {
  usg = {
    ftp_module = true
  }
}
`
}

func testAccSettingConfig_combined() string {
	return `
resource "unifi_setting" "test" {
  mgmt = {
    auto_upgrade = true
    ssh_enabled  = true
  }

  radius = {
    accounting_enabled = false
  }

  usg = {
    ftp_module = false
  }
}
`
}

func testAccSettingConfig_combinedUpdate() string {
	return `
resource "unifi_setting" "test" {
  mgmt = {
    auto_upgrade = false
    ssh_enabled  = false
  }

  radius = {
    accounting_enabled = true
  }

  usg = {
    ftp_module = true
  }
}
`
}

func testAccSettingConfig_sshKeys() string {
	return `
resource "unifi_setting" "test" {
  mgmt = {
    ssh_enabled = true
    ssh_keys = [{
      name    = "test-key"
      type    = "ssh-rsa"
      key     = "ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABAQDTest123"
      comment = "Test SSH Key"
    }]
  }
}
`
}

func TestAccSettingResource_doh(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccSettingConfig_dohAuto(),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"unifi_setting.test",
						"doh.state",
						"auto",
					),
				),
			},
			{
				ResourceName:      "unifi_setting.test",
				ImportState:       true,
				ImportStateVerify: true,
				ImportStateVerifyIgnore: []string{
					"doh.%",
					"doh.state",
				},
			},
			{
				Config: testAccSettingConfig_dohOff(),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"unifi_setting.test",
						"doh.state",
						"off",
					),
				),
			},
		},
	})
}

func TestAccSettingResource_dohCustomServers(t *testing.T) {
	// custom_servers requires controller support beyond simulation/demo mode:
	// the simulation controller returns DohCustomServersUnsupported (400).
	if os.Getenv("UNIFI_SKIP_CONTAINER") == "" {
		t.Skip("custom DoH servers require a real controller; set UNIFI_SKIP_CONTAINER to run")
	}

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccSettingConfig_dohCustomServers(),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"unifi_setting.test",
						"doh.state",
						"custom",
					),
					resource.TestCheckResourceAttr(
						"unifi_setting.test",
						"doh.custom_servers.#",
						"1",
					),
					resource.TestCheckResourceAttr(
						"unifi_setting.test",
						"doh.custom_servers.0.server_name",
						"my-resolver",
					),
					resource.TestCheckResourceAttr(
						"unifi_setting.test",
						"doh.custom_servers.0.enabled",
						"true",
					),
				),
			},
			{
				ResourceName:      "unifi_setting.test",
				ImportState:       true,
				ImportStateVerify: true,
				ImportStateVerifyIgnore: []string{
					"doh.%",
					"doh.state",
					"doh.custom_servers.#",
					"doh.custom_servers.0.server_name",
					"doh.custom_servers.0.sdns_stamp",
					"doh.custom_servers.0.enabled",
				},
			},
		},
	})
}

func TestAccSettingResource_ips(t *testing.T) {
	// ips_mode ids/ips/ipsInline requires a real UniFi gateway (UDM/USG); the
	// simulation controller accepts the PUT but reverts ips_mode to
	// "disabled" on read-back.
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccSettingConfig_ips(),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"unifi_setting.test",
						"ips.ips_mode",
						"disabled",
					),
					resource.TestCheckResourceAttr(
						"unifi_setting.test",
						"ips.restrict_torrents",
						"true",
					),
				),
			},
			{
				ResourceName:      "unifi_setting.test",
				ImportState:       true,
				ImportStateVerify: true,
				ImportStateVerifyIgnore: []string{
					"ips.%",
					"ips.ips_mode",
					"ips.restrict_torrents",
				},
			},
			{
				Config: testAccSettingConfig_ipsDisabled(),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"unifi_setting.test",
						"ips.ips_mode",
						"disabled",
					),
					resource.TestCheckResourceAttr(
						"unifi_setting.test",
						"ips.restrict_torrents",
						"false",
					),
				),
			},
		},
	})
}

func TestAccSettingResource_ipsHoneypot(t *testing.T) {
	// Honeypot requires a UDM-class gateway; the simulation controller
	// presents as a USG, which returns HoneypotIsNotSupportedInUsg (400).
	if os.Getenv("UNIFI_SKIP_CONTAINER") == "" {
		t.Skip("honeypot requires a real UDM-class controller; set UNIFI_SKIP_CONTAINER to run")
	}

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccSettingConfig_ipsHoneypot(),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"unifi_setting.test",
						"ips.honeypot_enabled",
						"true",
					),
					resource.TestCheckResourceAttr(
						"unifi_setting.test",
						"ips.honeypot.#",
						"1",
					),
					resource.TestCheckResourceAttr(
						"unifi_setting.test",
						"ips.honeypot.0.ip_address",
						"10.1.10.254",
					),
					resource.TestCheckResourceAttr(
						"unifi_setting.test",
						"ips.honeypot.0.version",
						"v4",
					),
				),
			},
		},
	})
}

func testAccSettingConfig_dohAuto() string {
	return `
resource "unifi_setting" "test" {
  doh = {
    state = "auto"
  }
}
`
}

func testAccSettingConfig_dohOff() string {
	return `
resource "unifi_setting" "test" {
  doh = {
    state = "off"
  }
}
`
}

func testAccSettingConfig_dohCustomServers() string {
	return `
resource "unifi_setting" "test" {
  doh = {
    state = "custom"
    custom_servers = [{
      server_name = "my-resolver"
      sdns_stamp  = "sdns://AgcAAAAAAAAACTEyNy4wLjAuMQA"
      enabled     = true
    }]
  }
}
`
}

func testAccSettingConfig_ips() string {
	return `
resource "unifi_setting" "test" {
  ips = {
    ips_mode          = "disabled"
    restrict_torrents = true
  }
}
`
}

func testAccSettingConfig_ipsDisabled() string {
	return `
resource "unifi_setting" "test" {
  ips = {
    ips_mode          = "disabled"
    restrict_torrents = false
  }
}
`
}

func testAccSettingConfig_ipsHoneypot() string {
	return `
resource "unifi_network" "test" {
  name   = "test-honeypot-network"
  subnet = "10.1.10.1/24"
  vlan   = 10
}

resource "unifi_setting" "test" {
  ips = {
    honeypot_enabled = true
    honeypot = [{
      ip_address = "10.1.10.254"
      network_id = unifi_network.test.id
      version    = "v4"
    }]
  }
}
`
}

func testAccSettingConfig_sshKeysUpdate() string {
	return `
resource "unifi_setting" "test" {
  mgmt = {
    ssh_enabled = true
    ssh_keys = [
      {
        name    = "test-key"
        type    = "ssh-rsa"
        key     = "ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABAQDTest123"
        comment = "Test SSH Key"
      },
      {
        name    = "test-key-2"
        type    = "ssh-ed25519"
        key     = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAITest456"
        comment = "Second Test Key"
      }
    ]
  }
}
`
}

// TestAccSettingResource_autoSpeedtest, _country, _dpi, _lcm,
// _networkOptimization, _ntp, _syslog and _igmpSnooping are the RED/GREEN
// net for the R2-B part 2 migration: these eight sections had zero live
// coverage before this test, so their move to the resource kit would
// otherwise be judged by unit round-trips alone. Proved green against the
// legacy code first; a future regression in the migrated section fails one
// of these instead of shipping silently.

func TestAccSettingResource_autoSpeedtest(t *testing.T) {
	// auto_speedtest requires a real WAN uplink: the simulation controller has
	// none, so it answers api.err.SpeedTestNotSupported (400) on the write.
	if os.Getenv("UNIFI_SKIP_CONTAINER") == "" {
		t.Skip("auto speedtest requires a real controller with a WAN uplink; set UNIFI_SKIP_CONTAINER to run")
	}

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccSettingConfig_autoSpeedtest(),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"unifi_setting.test",
						"auto_speedtest.enabled",
						"true",
					),
					resource.TestCheckResourceAttr(
						"unifi_setting.test",
						"auto_speedtest.cron_expr",
						"0 3 * * *",
					),
				),
			},
			{
				ResourceName:      "unifi_setting.test",
				ImportState:       true,
				ImportStateVerify: true,
				ImportStateVerifyIgnore: []string{
					"auto_speedtest.%",
					"auto_speedtest.enabled",
					"auto_speedtest.cron_expr",
				},
			},
			{
				Config: testAccSettingConfig_autoSpeedtestUpdate(),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"unifi_setting.test",
						"auto_speedtest.enabled",
						"false",
					),
					resource.TestCheckResourceAttr(
						"unifi_setting.test",
						"auto_speedtest.cron_expr",
						"0 3 * * *",
					),
				),
			},
		},
	})
}

func testAccSettingConfig_autoSpeedtest() string {
	return `
resource "unifi_setting" "test" {
  auto_speedtest = {
    enabled   = true
    cron_expr = "0 3 * * *"
  }
}
`
}

func testAccSettingConfig_autoSpeedtestUpdate() string {
	return `
resource "unifi_setting" "test" {
  auto_speedtest = {
    enabled   = false
    cron_expr = "0 3 * * *"
  }
}
`
}

func TestAccSettingResource_country(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccSettingConfig_country(),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"unifi_setting.test",
						"country.code",
						"826",
					),
				),
			},
			{
				ResourceName:      "unifi_setting.test",
				ImportState:       true,
				ImportStateVerify: true,
				ImportStateVerifyIgnore: []string{
					"country.%",
					"country.code",
				},
			},
			{
				Config: testAccSettingConfig_countryUpdate(),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"unifi_setting.test",
						"country.code",
						"276",
					),
				),
			},
		},
	})
}

func testAccSettingConfig_country() string {
	return `
resource "unifi_setting" "test" {
  country = {
    code = 826
  }
}
`
}

func testAccSettingConfig_countryUpdate() string {
	return `
resource "unifi_setting" "test" {
  country = {
    code = 276
  }
}
`
}

func TestAccSettingResource_dpi(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccSettingConfig_dpi(),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"unifi_setting.test",
						"dpi.enabled",
						"true",
					),
					resource.TestCheckResourceAttr(
						"unifi_setting.test",
						"dpi.fingerprinting_enabled",
						"true",
					),
				),
			},
			{
				ResourceName:      "unifi_setting.test",
				ImportState:       true,
				ImportStateVerify: true,
				ImportStateVerifyIgnore: []string{
					"dpi.%",
					"dpi.enabled",
					"dpi.fingerprinting_enabled",
				},
			},
			{
				Config: testAccSettingConfig_dpiUpdate(),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"unifi_setting.test",
						"dpi.enabled",
						"true",
					),
					resource.TestCheckResourceAttr(
						"unifi_setting.test",
						"dpi.fingerprinting_enabled",
						"false",
					),
				),
			},
		},
	})
}

func testAccSettingConfig_dpi() string {
	return `
resource "unifi_setting" "test" {
  dpi = {
    enabled                = true
    fingerprinting_enabled = true
  }
}
`
}

func testAccSettingConfig_dpiUpdate() string {
	return `
resource "unifi_setting" "test" {
  dpi = {
    enabled                = true
    fingerprinting_enabled = false
  }
}
`
}

func TestAccSettingResource_lcm(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccSettingConfig_lcm(),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"unifi_setting.test",
						"lcm.enabled",
						"true",
					),
					resource.TestCheckResourceAttr(
						"unifi_setting.test",
						"lcm.brightness",
						"50",
					),
				),
			},
			{
				ResourceName:      "unifi_setting.test",
				ImportState:       true,
				ImportStateVerify: true,
				ImportStateVerifyIgnore: []string{
					"lcm.%",
					"lcm.brightness",
					"lcm.enabled",
					"lcm.idle_timeout",
					"lcm.sync",
					"lcm.touch_event",
				},
			},
			{
				Config: testAccSettingConfig_lcmUpdate(),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"unifi_setting.test",
						"lcm.enabled",
						"true",
					),
					resource.TestCheckResourceAttr(
						"unifi_setting.test",
						"lcm.brightness",
						"80",
					),
				),
			},
		},
	})
}

func testAccSettingConfig_lcm() string {
	return `
resource "unifi_setting" "test" {
  lcm = {
    enabled    = true
    brightness = 50
  }
}
`
}

func testAccSettingConfig_lcmUpdate() string {
	return `
resource "unifi_setting" "test" {
  lcm = {
    enabled    = true
    brightness = 80
  }
}
`
}

func TestAccSettingResource_networkOptimization(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccSettingConfig_networkOptimization(),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"unifi_setting.test",
						"network_optimization.enabled",
						"true",
					),
				),
			},
			{
				ResourceName:      "unifi_setting.test",
				ImportState:       true,
				ImportStateVerify: true,
				ImportStateVerifyIgnore: []string{
					"network_optimization.%",
					"network_optimization.enabled",
				},
			},
			{
				Config: testAccSettingConfig_networkOptimizationUpdate(),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"unifi_setting.test",
						"network_optimization.enabled",
						"false",
					),
				),
			},
		},
	})
}

func testAccSettingConfig_networkOptimization() string {
	return `
resource "unifi_setting" "test" {
  network_optimization = {
    enabled = true
  }
}
`
}

func testAccSettingConfig_networkOptimizationUpdate() string {
	return `
resource "unifi_setting" "test" {
  network_optimization = {
    enabled = false
  }
}
`
}

// TestAccSettingResource_ntp asserts what the controller actually does with
// ntp_server_1, ntp_server_2 and setting_preference = "manual": a
// live-controller probe against this branch's own masked write (not the
// legacy whole-object PUT the round 1 finding ran through, whose omitempty
// tag never let an explicit "" reach the wire) found that ntp_server_2
// clears to a literal "" both when configured that way from create and when
// updated back to "" from a previously-set value. Both are asserted here as
// exact values, not merely TestCheckResourceAttrSet.
func TestAccSettingResource_ntp(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccSettingConfig_ntp(),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"unifi_setting.test",
						"ntp.ntp_server_1",
						"time.example.com",
					),
					resource.TestCheckResourceAttr(
						"unifi_setting.test",
						"ntp.setting_preference",
						"manual",
					),
					resource.TestCheckResourceAttr(
						"unifi_setting.test",
						"ntp.ntp_server_2",
						"",
					),
				),
			},
			{
				ResourceName:      "unifi_setting.test",
				ImportState:       true,
				ImportStateVerify: true,
				ImportStateVerifyIgnore: []string{
					"ntp.%",
					"ntp.ntp_server_1",
					"ntp.ntp_server_2",
					"ntp.ntp_server_3",
					"ntp.ntp_server_4",
					"ntp.setting_preference",
				},
			},
			{
				Config: testAccSettingConfig_ntpUpdate(),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"unifi_setting.test",
						"ntp.ntp_server_1",
						"time.cloudflare.com",
					),
					resource.TestCheckResourceAttr(
						"unifi_setting.test",
						"ntp.ntp_server_2",
						"time.probe-configured.example.com",
					),
				),
			},
			{
				Config: testAccSettingConfig_ntpUpdateCleared(),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"unifi_setting.test",
						"ntp.ntp_server_1",
						"time.cloudflare.com",
					),
					resource.TestCheckResourceAttr(
						"unifi_setting.test",
						"ntp.ntp_server_2",
						"",
					),
				),
			},
		},
	})
}

func testAccSettingConfig_ntp() string {
	return `
resource "unifi_setting" "test" {
  ntp = {
    ntp_server_1       = "time.example.com"
    ntp_server_2       = ""
    setting_preference = "manual"
  }
}
`
}

func testAccSettingConfig_ntpUpdate() string {
	return `
resource "unifi_setting" "test" {
  ntp = {
    ntp_server_1       = "time.cloudflare.com"
    ntp_server_2       = "time.probe-configured.example.com"
    setting_preference = "manual"
  }
}
`
}

func testAccSettingConfig_ntpUpdateCleared() string {
	return `
resource "unifi_setting" "test" {
  ntp = {
    ntp_server_1       = "time.cloudflare.com"
    ntp_server_2       = ""
    setting_preference = "manual"
  }
}
`
}

func TestAccSettingResource_syslog(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccSettingConfig_syslog(),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"unifi_setting.test",
						"syslog.enabled",
						"true",
					),
					resource.TestCheckResourceAttr(
						"unifi_setting.test",
						"syslog.ip",
						"10.0.0.5",
					),
					resource.TestCheckResourceAttr(
						"unifi_setting.test",
						"syslog.port",
						"514",
					),
					resource.TestCheckResourceAttr(
						"unifi_setting.test",
						"syslog.contents.#",
						"2",
					),
					resource.TestCheckResourceAttr(
						"unifi_setting.test",
						"syslog.contents.0",
						"device",
					),
					resource.TestCheckResourceAttr(
						"unifi_setting.test",
						"syslog.contents.1",
						"client",
					),
				),
			},
			{
				ResourceName:      "unifi_setting.test",
				ImportState:       true,
				ImportStateVerify: true,
				ImportStateVerifyIgnore: []string{
					"syslog.%",
					"syslog.contents",
					"syslog.debug",
					"syslog.enabled",
					"syslog.ip",
					"syslog.log_all_contents",
					"syslog.netconsole_enabled",
					"syslog.netconsole_host",
					"syslog.netconsole_port",
					"syslog.port",
					"syslog.this_controller",
					"syslog.this_controller_encrypted_only",
				},
			},
			{
				Config: testAccSettingConfig_syslogUpdate(),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"unifi_setting.test",
						"syslog.enabled",
						"false",
					),
					resource.TestCheckResourceAttr(
						"unifi_setting.test",
						"syslog.ip",
						"10.0.0.5",
					),
					resource.TestCheckResourceAttr(
						"unifi_setting.test",
						"syslog.contents.#",
						"2",
					),
				),
			},
		},
	})
}

func testAccSettingConfig_syslog() string {
	return `
resource "unifi_setting" "test" {
  syslog = {
    enabled  = true
    ip       = "10.0.0.5"
    port     = 514
    contents = ["device", "client"]
  }
}
`
}

func testAccSettingConfig_syslogUpdate() string {
	return `
resource "unifi_setting" "test" {
  syslog = {
    enabled  = false
    ip       = "10.0.0.5"
    port     = 514
    contents = ["device", "client"]
  }
}
`
}

// TestAccSettingResource_igmpSnooping exercises the site-level
// igmp_snooping setting's two modelled fields (of the controller's 15; the
// other 13 -- querier mode, switches, flood options -- are advanced UI-only
// options preserved via read-modify-write, not schema attributes):
// enabled, and network_ids pointed at a real network so the round 1 finding
// (enabled read back false with no networks named) gets a fair test.
func TestAccSettingResource_igmpSnooping(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccSettingConfig_igmpSnooping(),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"unifi_setting.test",
						"igmp_snooping.enabled",
						"true",
					),
					resource.TestCheckResourceAttr(
						"unifi_setting.test",
						"igmp_snooping.network_ids.#",
						"1",
					),
					resource.TestCheckResourceAttrPair(
						"unifi_setting.test", "igmp_snooping.network_ids.0",
						"unifi_network.test", "id",
					),
				),
			},
			{
				ResourceName:      "unifi_setting.test",
				ImportState:       true,
				ImportStateVerify: true,
				ImportStateVerifyIgnore: []string{
					"igmp_snooping.%",
					"igmp_snooping.enabled",
					"igmp_snooping.network_ids",
				},
			},
			{
				Config: testAccSettingConfig_igmpSnoopingUpdate(),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"unifi_setting.test",
						"igmp_snooping.enabled",
						"false",
					),
					resource.TestCheckResourceAttr(
						"unifi_setting.test",
						"igmp_snooping.network_ids.#",
						"1",
					),
				),
			},
		},
	})
}

func testAccSettingConfig_igmpSnooping() string {
	return `
resource "unifi_network" "test" {
  name   = "test-igmp-network"
  subnet = "10.3.10.1/24"
  vlan   = 30
}

resource "unifi_setting" "test" {
  igmp_snooping = {
    enabled     = true
    network_ids = [unifi_network.test.id]
  }
}
`
}

func testAccSettingConfig_igmpSnoopingUpdate() string {
	return `
resource "unifi_network" "test" {
  name   = "test-igmp-network"
  subnet = "10.3.10.1/24"
  vlan   = 30
}

resource "unifi_setting" "test" {
  igmp_snooping = {
    enabled     = false
    network_ids = [unifi_network.test.id]
  }
}
`
}

func TestAccSettingResource_locale(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccSettingConfig_locale(),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"unifi_setting.test",
						"locale.timezone",
						"America/Los_Angeles",
					),
				),
			},
			{
				ResourceName:      "unifi_setting.test",
				ImportState:       true,
				ImportStateVerify: true,
				ImportStateVerifyIgnore: []string{
					"locale.%",
					"locale.timezone",
				},
			},
			{
				Config: testAccSettingConfig_localeUpdate(),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"unifi_setting.test",
						"locale.timezone",
						"America/New_York",
					),
				),
			},
		},
	})
}

func testAccSettingConfig_locale() string {
	return `
resource "unifi_setting" "test" {
  locale = {
    timezone = "America/Los_Angeles"
  }
}
`
}

func testAccSettingConfig_localeUpdate() string {
	return `
resource "unifi_setting" "test" {
  locale = {
    timezone = "America/New_York"
  }
}
`
}

func TestAccSettingResource_globalNat(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccSettingConfig_globalNat(),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"unifi_setting.test",
						"global_nat.mode",
						"auto",
					),
					resource.TestCheckResourceAttr(
						"unifi_setting.test",
						"global_nat.excluded_network_ids.#",
						"1",
					),
					resource.TestCheckResourceAttrPair(
						"unifi_setting.test", "global_nat.excluded_network_ids.0",
						"unifi_network.test", "id",
					),
				),
			},
			{
				ResourceName:      "unifi_setting.test",
				ImportState:       true,
				ImportStateVerify: true,
				ImportStateVerifyIgnore: []string{
					"global_nat.%",
					"global_nat.mode",
					"global_nat.excluded_network_ids",
				},
			},
			{
				Config: testAccSettingConfig_globalNatUpdate(),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"unifi_setting.test",
						"global_nat.mode",
						"off",
					),
					resource.TestCheckResourceAttr(
						"unifi_setting.test",
						"global_nat.excluded_network_ids.#",
						"1",
					),
				),
			},
		},
	})
}

func testAccSettingConfig_globalNat() string {
	return `
resource "unifi_network" "test" {
  name   = "test-global-nat-network"
  subnet = "10.3.11.1/24"
  vlan   = 31
}

resource "unifi_setting" "test" {
  global_nat = {
    mode                 = "auto"
    excluded_network_ids = [unifi_network.test.id]
  }
}
`
}

func testAccSettingConfig_globalNatUpdate() string {
	return `
resource "unifi_network" "test" {
  name   = "test-global-nat-network"
  subnet = "10.3.11.1/24"
  vlan   = 31
}

resource "unifi_setting" "test" {
  global_nat = {
    mode                 = "off"
    excluded_network_ids = [unifi_network.test.id]
  }
}
`
}

func TestAccSettingResource_sslInspection(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccSettingConfig_sslInspection(),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"unifi_setting.test",
						"ssl_inspection.state",
						"simple",
					),
				),
			},
			{
				ResourceName:      "unifi_setting.test",
				ImportState:       true,
				ImportStateVerify: true,
				ImportStateVerifyIgnore: []string{
					"ssl_inspection.%",
					"ssl_inspection.state",
				},
			},
			{
				Config: testAccSettingConfig_sslInspectionUpdate(),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"unifi_setting.test",
						"ssl_inspection.state",
						"off",
					),
				),
			},
		},
	})
}

func testAccSettingConfig_sslInspection() string {
	return `
resource "unifi_setting" "test" {
  ssl_inspection = {
    state = "simple"
  }
}
`
}

func testAccSettingConfig_sslInspectionUpdate() string {
	return `
resource "unifi_setting" "test" {
  ssl_inspection = {
    state = "off"
  }
}
`
}

// TestAccSettingResource_ipsec exercises ikev2_reauthentication_method with
// the one value the SDK's own comment records as observed
// (make-before-break) plus IKEv2's other named rekey strategy
// (break-before-make); the schema carries no validator, so the controller
// itself is what could refuse the second value, not the provider.
func TestAccSettingResource_ipsec(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccSettingConfig_ipsec(),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"unifi_setting.test",
						"ipsec.ikev2_reauthentication_method",
						"make-before-break",
					),
				),
			},
			{
				ResourceName:      "unifi_setting.test",
				ImportState:       true,
				ImportStateVerify: true,
				ImportStateVerifyIgnore: []string{
					"ipsec.%",
					"ipsec.ikev2_reauthentication_method",
				},
			},
			{
				Config: testAccSettingConfig_ipsecUpdate(),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"unifi_setting.test",
						"ipsec.ikev2_reauthentication_method",
						"break-before-make",
					),
				),
			},
		},
	})
}

func testAccSettingConfig_ipsec() string {
	return `
resource "unifi_setting" "test" {
  ipsec = {
    ikev2_reauthentication_method = "make-before-break"
  }
}
`
}

func testAccSettingConfig_ipsecUpdate() string {
	return `
resource "unifi_setting" "test" {
  ipsec = {
    ikev2_reauthentication_method = "break-before-make"
  }
}
`
}

func TestNewSettingResource(t *testing.T) {
	r := NewSettingResource()
	if r == nil {
		t.Fatal("NewSettingResource() returned nil")
	}
	if _, ok := r.(fwresource.ResourceWithConfigure); !ok {
		t.Error("expected ResourceWithConfigure interface")
	}
	if _, ok := r.(fwresource.ResourceWithImportState); !ok {
		t.Error("expected ResourceWithImportState interface")
	}
}

func Test_settingResource_UpgradeState(t *testing.T) {
	r := &settingResource{}
	ctx := context.Background()
	got := r.UpgradeState(ctx)
	if got == nil {
		t.Fatal("UpgradeState() returned nil")
	}
	if _, ok := got[0]; !ok {
		t.Error("UpgradeState() map should contain version key 0")
	}
}

// TestSettingNtpServersUseStateForUnknown checks that omitted
// Optional+Computed server fields retain their prior values instead of
// repeatedly planning as "known after apply" when the NTP block is configured.
func TestSettingNtpServersUseStateForUnknown(t *testing.T) {
	resp := &fwresource.SchemaResponse{}
	(&settingResource{}).Schema(context.Background(), fwresource.SchemaRequest{}, resp)

	ntp, ok := resp.Schema.Attributes["ntp"].(schema.SingleNestedAttribute)
	if !ok {
		t.Fatal("ntp is not a SingleNestedAttribute")
	}
	for _, key := range []string{"ntp_server_1", "ntp_server_2", "ntp_server_3", "ntp_server_4"} {
		server, ok := ntp.Attributes[key].(schema.StringAttribute)
		if !ok {
			t.Errorf("ntp.%s is not a StringAttribute", key)
			continue
		}
		if !server.Optional || !server.Computed {
			t.Errorf("ntp.%s must remain Optional+Computed", key)
		}
		if len(server.PlanModifiers) == 0 {
			t.Errorf("ntp.%s must use UseStateForUnknown", key)
			continue
		}

		req := planmodifier.StringRequest{
			ConfigValue: types.StringNull(),
			PlanValue:   types.StringUnknown(),
			State: tfsdk.State{
				Raw: tftypes.NewValue(tftypes.String, ""),
			},
			StateValue: types.StringValue(""),
		}
		modified := &planmodifier.StringResponse{PlanValue: req.PlanValue}
		server.PlanModifiers[0].PlanModifyString(context.Background(), req, modified)
		if modified.Diagnostics.HasError() {
			t.Errorf("ntp.%s plan modifier returned errors: %v", key, modified.Diagnostics)
		}
		if modified.PlanValue.IsNull() || modified.PlanValue.IsUnknown() ||
			modified.PlanValue.ValueString() != "" {
			t.Errorf("ntp.%s plan = %v, want prior known empty state", key, modified.PlanValue)
		}
	}
}

// Test_settingResource_mgmtModelToSetting and
// Test_settingResource_mgmtSettingToModel (deleted along with the mappers
// they exercised) pinned three behaviours, each with a surviving owner: a
// plan-null bool field leaves the SDK struct at its Go zero on write --
// resourcekit's own BoolField unit tests cover that generically, and it's
// not mgmt-specific; the read side's plan-conditioned null/reflect-remote
// split is TestMgmtAfterReceive's AutoUpgrade (configured) and
// WifimanEnabled (unconfigured) cases -- boolOrNull is one shared helper
// across all eight bools, so a second field exercising the same branch adds
// no coverage; and the wire name itself is TestMgmtKitSpecConformance's
// WireNameProblems.

// Test_settingResource_radiusModelToSetting and
// Test_settingResource_radiusSettingToModel (deleted along with the mappers
// they exercised) pinned two behaviours, each with a surviving owner: the
// null-plan-leaves-base-unchanged / non-null-overlay write-side behaviour is
// TestRadiusSettingRoundTrip (setting_radius_descriptor_test.go), driving
// radiusKitSpec's own ToSDK/ToModel instead of the deleted mappers; the
// secret plan-conditioned read is TestRadiusAfterReceiveKeepsThePlansSecretWhenNamed,
// which ports the two radiusSettingToModel subtests exactly.

// TestUsgGeoRoundTrip, TestUsgGeoPreservesUnmanagedFields, TestUsgGeoAbsentSetting
// and Test_settingResource_usgSettingToModel (deleted along with the mappers
// they exercised) moved to setting_usg_descriptor_test.go:
// TestUsgGeoRenamesBlockToActionOnTheWire and TestUsgGeoIsWrittenOnlyWhenConfigured
// (the wire-rename and write-only-when-configured behaviours),
// TestUsgGeoBackendPreservesUnmanagedSubFieldsOnAPartialWrite (the
// read-modify-write merge), TestUsgGeoBackendReadTreatsIPFilteringNilAsZero and
// TestUsgGeoDocumentReadNotFoundLeavesModelUntouched (the absent-usg_geo cases)
// and TestUsgAfterReceiveNullsWhatThePlanDidNotName (the null-plan read-side
// behaviour).

// TestIpsSuppressionAbsentSetting (deleted along with ipsSettingToModel/
// ipsSuppressionConfigured) moved to setting_ips_descriptor_test.go:
// TestIpsSuppressionIsWrittenOnlyWhenConfigured (the configured predicate)
// and TestIpsSuppressionDocumentReadNotFoundYieldsEmptyLists (the
// absent-document read, which now reads back an empty, not null, list --
// see that file's own top comment for why ips_suppression's own tolerance
// differs from usg_geo's).

// Test_settingResource_igmpSnoopingModelToSetting and
// Test_settingResource_igmpSnoopingSettingToModel (deleted along with the
// mappers they exercised) moved to
// setting_igmp_snooping_descriptor_test.go's TestIgmpSnoopingSettingRoundTrip
// and TestIgmpSnoopingSpecReadsEmptyNetworkIDs. The "overlaid onto base"/
// "advanced fields preserved" assertions did not port: see that file's own
// comment for what replaces them.

// Test_settingResource_dohModelToSetting and
// Test_settingResource_dohSettingToModel (deleted along with the mappers
// they exercised) moved to setting_doh_descriptor_test.go's
// TestDohSettingRoundTrip (the state/round-trip assertions),
// TestDohAfterReceiveNullsWhatThePlanDidNotName (the plan-conditioned-null
// case) and TestDohConfiguredEmptyCustomServersReadsBackAsEmptyList (a
// case neither deleted test covered).

// Test_settingResource_ipsSettingToModel (deleted along with
// ipsSettingToModel) is subsumed by TestIpsAfterReceiveNullsWhatThePlanDidNotName
// (setting_ips_descriptor_test.go, the null-plan case) and ordinary
// StringField.ToModel behavior (the non-null-reflects-remote-value case,
// which every kit Field already carries its own tests for).

// TestIgmpSnoopingModelMerge (deleted along with igmpSnoopingModelToSetting/
// igmpSnoopingSettingToModel) pinned the Go-level read-modify-write that
// preserved advanced querier/flood fields across an update. That merge is
// retired, not ported: setting_igmp_snooping_descriptor_test.go's
// TestIgmpSnoopingSpecMasksOnlyEnabled and
// TestIgmpSnoopingBackendUpdateFieldsSendsOnlyTheNamedWiresPlusKey pin its
// replacement -- UpdateSettingFields' field mask, which needs no base read
// at all to leave the other thirteen fields untouched.

// TestAutoSpeedtestSettingRoundTrip moved to
// setting_auto_speedtest_descriptor_test.go: it now drives
// autoSpeedtestKitSpec's own ToSDK/ToModel instead of the deleted
// autoSpeedtestModelToSetting/autoSpeedtestSettingToModel mappers.

// TestSettingBlocksRoundTrip moved to setting_syslog_descriptor_test.go's
// TestSyslogSettingRoundTrip: it now drives syslogKitSpec's own
// ToSDK/ToModel instead of the deleted
// syslogModelToSetting/syslogSettingToModel mappers.

// TestNtpSettingStateNormalization moved to
// setting_ntp_descriptor_test.go's TestNtpSettingRoundTripStateNormalization:
// it now drives ntpKitSpec's own ToSDK/ToModel instead of the deleted
// ntpModelToSetting/ntpSettingToModel mappers.

// mgmt's ssh_password/plan-conditioned-null behaviour, formerly
// TestMgmtNewFields against mgmtModelToSetting/mgmtSettingToModel, is now
// TestMgmtAfterReceive (setting_mgmt_descriptor_test.go) against
// mgmtAfterReceive directly. The one assertion with no equivalent there is
// "read-base field WifimanEnabled was clobbered": that pinned the old
// read-modify-write overlay, which the masked write retires outright --
// UpdateSettingFields' field mask is what now keeps an unmanaged field from
// being clobbered, not a Go-level struct merge, so there is nothing left in
// this package for that assertion to pin.

// TestSyslogOmitsUnsetPorts moved to setting_syslog_descriptor_test.go's
// TestSyslogSpecOmitsAnUnsetPort: it now drives syslogKitSpec's own ToSDK
// instead of the deleted syslogModelToSetting.

// TestLcmOmitsUnsetInts moved to setting_lcm_descriptor_test.go's
// TestLcmSpecOmitsAnUnsetBrightness: it now drives lcmKitSpec's own ToSDK
// instead of the deleted lcmModelToSetting.
