package unifi

import (
	"context"
	"fmt"
	"testing"

	fwresource "github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func Test_magicSiteToSiteVpnModelToData(t *testing.T) {
	m := &settingMagicSiteToSiteVpnModel{
		Enabled:    types.BoolValue(true),
		PublicKey:  types.StringValue("attacker-controlled-should-be-ignored"),
		PrivateKey: types.StringValue("synthetic-private-key"),
	}
	// The controller-generated key pair lives in the raw document; the
	// overlay must preserve public_key verbatim (it is derived, never
	// written) and any other unmodeled fields.
	data := map[string]any{
		"public_key":      "synthetic-public-key",
		"unmodeled_field": "keep",
	}

	magicSiteToSiteVpnModelToData(m, data)

	if data["enabled"] != true {
		t.Fatalf("enabled = %v", data["enabled"])
	}
	if data["x_private_key"] != "synthetic-private-key" {
		t.Fatalf("x_private_key = %v", data["x_private_key"])
	}
	if data["public_key"] != "synthetic-public-key" {
		t.Fatal("computed public_key must never be overwritten by the model")
	}
	if data["unmodeled_field"] != "keep" {
		t.Fatal("unmodeled_field was clobbered")
	}
}

func Test_magicSiteToSiteVpnModelToData_nullsLeaveRemoteValues(t *testing.T) {
	m := &settingMagicSiteToSiteVpnModel{
		Enabled:    types.BoolNull(),
		PublicKey:  types.StringNull(),
		PrivateKey: types.StringNull(),
	}
	data := map[string]any{
		"enabled":       true,
		"public_key":    "synthetic-public-key",
		"x_private_key": "synthetic-private-key",
	}

	magicSiteToSiteVpnModelToData(m, data)

	if data["enabled"] != true {
		t.Fatalf("null enabled overwrote remote value: %v", data["enabled"])
	}
	if data["x_private_key"] != "synthetic-private-key" {
		t.Fatal("null private_key must not touch the controller-generated key")
	}
	if data["public_key"] != "synthetic-public-key" {
		t.Fatal("public_key was clobbered")
	}
}

func Test_magicSiteToSiteVpnDataToModel(t *testing.T) {
	m := magicSiteToSiteVpnDataToModel(map[string]any{
		"enabled":       true,
		"public_key":    "synthetic-public-key",
		"x_private_key": "synthetic-private-key",
	})
	if !m.Enabled.ValueBool() {
		t.Fatalf("enabled = %v", m.Enabled)
	}
	if m.PublicKey.ValueString() != "synthetic-public-key" {
		t.Fatalf("public_key = %v", m.PublicKey)
	}
	if m.PrivateKey.ValueString() != "synthetic-private-key" {
		t.Fatalf("private_key = %v", m.PrivateKey)
	}

	empty := magicSiteToSiteVpnDataToModel(map[string]any{})
	if empty.Enabled.ValueBool() {
		t.Fatalf("missing enabled should be false, got %v", empty.Enabled)
	}
	if !empty.PublicKey.IsNull() || !empty.PrivateKey.IsNull() {
		t.Fatalf("missing keys should map to null, got %v / %v",
			empty.PublicKey, empty.PrivateKey)
	}
}

func Test_settingResource_Schema_magicSiteToSiteVpn(t *testing.T) {
	r := &settingResource{}
	var resp fwresource.SchemaResponse
	r.Schema(context.Background(), fwresource.SchemaRequest{}, &resp)
	sect, ok := resp.Schema.Attributes["magic_site_to_site_vpn"].(schema.SingleNestedAttribute)
	if !ok {
		t.Fatal("schema is missing the magic_site_to_site_vpn section attribute")
	}
	pk, ok := sect.Attributes["private_key"].(schema.StringAttribute)
	if !ok {
		t.Fatal("magic_site_to_site_vpn is missing private_key")
	}
	if !pk.Sensitive {
		t.Fatal("private_key must be Sensitive")
	}
	if pk.Required {
		t.Fatal("private_key must never be required")
	}
	pub, ok := sect.Attributes["public_key"].(schema.StringAttribute)
	if !ok {
		t.Fatal("magic_site_to_site_vpn is missing public_key")
	}
	if pub.Optional || pub.Required || !pub.Computed {
		t.Fatal("public_key must be Computed-only")
	}
}

func TestAccSettingResource_magicSiteToSiteVpn(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccSettingConfig_magicSiteToSiteVpn(true),
				Check: resource.TestCheckResourceAttr(
					"unifi_setting.test", "magic_site_to_site_vpn.enabled", "true",
				),
			},
			{
				Config: testAccSettingConfig_magicSiteToSiteVpn(false),
				Check: resource.TestCheckResourceAttr(
					"unifi_setting.test", "magic_site_to_site_vpn.enabled", "false",
				),
			},
		},
	})
}

func testAccSettingConfig_magicSiteToSiteVpn(enabled bool) string {
	return fmt.Sprintf(`
resource "unifi_setting" "test" {
  magic_site_to_site_vpn = {
    enabled = %t
  }
}
`, enabled)
}
