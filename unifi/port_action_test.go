package unifi

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	fwaction "github.com/hashicorp/terraform-plugin-framework/action"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/tfversion"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/acctestenv"
)

// TestAccPortAction_persistsPoeOverride exercises the action through Terraform,
// then reads the controller rather than relying on action state (actions do not
// have any). The switch starts pending, so the managed device adopts it before
// the after-create trigger invokes the action.
func TestAccPortAction_persistsPoeOverride(t *testing.T) {
	mac := os.Getenv(acctestenv.EnvAccDeviceMAC)
	if mac == "" {
		t.Skipf("%s not set; skipping port action acceptance test", acctestenv.EnvAccDeviceMAC)
	}

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		TerraformVersionChecks: []tfversion.TerraformVersionCheck{
			tfversion.SkipBelow(tfversion.Version1_14_0),
		},
		Steps: []resource.TestStep{{
			Config: testAccPortActionConfig(mac, 1, "off"),
			PostApplyFunc: func() {
				assertPortPoeMode(t, mac, 1, "off")
			},
		}},
	})
}

func testAccPortActionConfig(mac string, port int64, poeMode string) string {
	return fmt.Sprintf(`
resource "unifi_device" "port_action_target" {
  mac               = %q
  name              = "Port Action Target"
  allow_adoption    = true
  forget_on_destroy = false

  lifecycle {
    action_trigger {
      events  = [after_create]
      actions = [action.unifi_port.test]
    }
  }
}

action "unifi_port" "test" {
  config {
    device_mac  = %q
    port_number = %d
    poe_mode    = %q
  }
}
`, mac, mac, port, poeMode)
}

func assertPortPoeMode(t *testing.T, mac string, port int64, wantMode string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	client, err := ui.New(ctx, &ui.Config{
		BaseURL:       os.Getenv("UNIFI_API"),
		Username:      os.Getenv("UNIFI_USERNAME"),
		Password:      os.Getenv("UNIFI_PASSWORD"),
		AllowInsecure: true,
	})
	if err != nil {
		t.Fatalf("connect to controller after action: %v", err)
	}

	for {
		device, getErr := client.GetDeviceByMAC(ctx, "default", mac)
		if getErr == nil {
			for _, override := range device.PortOverrides {
				if override.PortIDX != nil && *override.PortIDX == port && override.PoeMode == wantMode {
					return
				}
			}
			err = fmt.Errorf("port %d has no persisted poe_mode %q override", port, wantMode)
		} else {
			err = getErr
		}
		if ctx.Err() != nil {
			t.Fatalf("read port override after action: %v", err)
		}
		time.Sleep(250 * time.Millisecond)
	}
}

func TestNewPortAction(t *testing.T) {
	got := NewPortAction()
	if got == nil {
		t.Fatal("NewPortAction() returned nil")
	}
	if _, ok := got.(fwaction.ActionWithConfigure); !ok {
		t.Error("expected ActionWithConfigure interface")
	}
}

func Test_portAction_Schema(t *testing.T) {
	tests := []struct {
		name      string
		wantAttrs []string
	}{
		{
			name:      "has_required_attributes",
			wantAttrs: []string{"device_mac", "port_number", "poe_mode", "timeouts"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := &portAction{}
			resp := &fwaction.SchemaResponse{}
			a.Schema(context.Background(), fwaction.SchemaRequest{}, resp)
			for _, attr := range tt.wantAttrs {
				if _, ok := resp.Schema.Attributes[attr]; !ok {
					t.Errorf("expected attribute %q in schema", attr)
				}
			}
		})
	}
}
