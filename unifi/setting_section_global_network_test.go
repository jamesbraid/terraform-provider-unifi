package unifi

import (
	"context"
	"os"
	"testing"

	fwresource "github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
)

func Test_globalNetworkModelToData(t *testing.T) {
	m := &settingGlobalNetworkModel{
		DefaultSecurityPosture: types.StringValue("ALLOW_ALL"),
	}
	// Pre-existing raw fields must be preserved, not clobbered.
	data := map[string]any{"unmodeled_field": "keep"}

	globalNetworkModelToData(m, data)

	if data["default_security_posture"] != "ALLOW_ALL" {
		t.Fatalf("default_security_posture = %v", data["default_security_posture"])
	}
	if data["unmodeled_field"] != "keep" {
		t.Fatal("unmodeled_field was clobbered")
	}
}

func Test_globalNetworkModelToData_nullLeavesRemoteValue(t *testing.T) {
	m := &settingGlobalNetworkModel{DefaultSecurityPosture: types.StringNull()}
	data := map[string]any{"default_security_posture": "ALLOW_ALL"}

	globalNetworkModelToData(m, data)

	if data["default_security_posture"] != "ALLOW_ALL" {
		t.Fatal("null posture overwrote remote value")
	}
}

func Test_globalNetworkSettingToModel(t *testing.T) {
	m := globalNetworkSettingToModel(&settings.GlobalNetwork{
		DefaultSecurityPosture: "ALLOW_ALL",
	})
	if m.DefaultSecurityPosture.ValueString() != "ALLOW_ALL" {
		t.Fatalf("default_security_posture = %v", m.DefaultSecurityPosture)
	}
	empty := globalNetworkSettingToModel(&settings.GlobalNetwork{})
	if !empty.DefaultSecurityPosture.IsNull() {
		t.Fatalf("empty posture should map to null, got %v", empty.DefaultSecurityPosture)
	}
}

func Test_settingResource_Schema_globalNetwork(t *testing.T) {
	r := &settingResource{}
	var resp fwresource.SchemaResponse
	r.Schema(context.Background(), fwresource.SchemaRequest{}, &resp)
	if _, ok := resp.Schema.Attributes["global_network"]; !ok {
		t.Fatal("schema is missing the global_network section attribute")
	}
}

func TestAccSettingResource_globalNetwork(t *testing.T) {
	// global_network requires a real gateway-class controller; the demo/
	// simulation controller rejects the section. Verified directly: GET
	// returns an empty {} document (unlike e.g. netflow, which is fully
	// populated), and PUT with a minimal payload (with or without "key")
	// fails identically with api.err.Invalid (400) — not a payload-shape
	// bug in this test's config.
	if os.Getenv("UNIFI_SKIP_CONTAINER") == "" {
		t.Skip("global_network requires a real controller; set UNIFI_SKIP_CONTAINER to run")
	}

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: `
resource "unifi_setting" "test" {
  global_network = {
    default_security_posture = "ALLOW_ALL"
  }
}
`,
				Check: resource.TestCheckResourceAttr(
					"unifi_setting.test", "global_network.default_security_posture", "ALLOW_ALL",
				),
			},
		},
	})
}
