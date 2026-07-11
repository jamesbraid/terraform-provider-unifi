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

func Test_ipsecModelToData(t *testing.T) {
	m := &settingIpsecModel{
		Ikev2ReauthenticationMethod: types.StringValue("make-before-break"),
	}
	// Pre-existing raw fields must be preserved, not clobbered.
	data := map[string]any{"unmodeled_field": "keep"}

	ipsecModelToData(m, data)

	if data["ikev2_reauthentication_method"] != "make-before-break" {
		t.Fatalf("ikev2_reauthentication_method = %v", data["ikev2_reauthentication_method"])
	}
	if data["unmodeled_field"] != "keep" {
		t.Fatal("unmodeled_field was clobbered")
	}
}

func Test_ipsecModelToData_nullLeavesRemoteValue(t *testing.T) {
	m := &settingIpsecModel{Ikev2ReauthenticationMethod: types.StringNull()}
	data := map[string]any{"ikev2_reauthentication_method": "make-before-break"}

	ipsecModelToData(m, data)

	if data["ikev2_reauthentication_method"] != "make-before-break" {
		t.Fatal("null method overwrote remote value")
	}
}

func Test_ipsecSettingToModel(t *testing.T) {
	m := ipsecSettingToModel(&settings.Ipsec{
		Ikev2ReauthenticationMethod: "make-before-break",
	})
	if m.Ikev2ReauthenticationMethod.ValueString() != "make-before-break" {
		t.Fatalf("method = %v", m.Ikev2ReauthenticationMethod)
	}
	empty := ipsecSettingToModel(&settings.Ipsec{})
	if !empty.Ikev2ReauthenticationMethod.IsNull() {
		t.Fatalf("empty method should map to null, got %v", empty.Ikev2ReauthenticationMethod)
	}
}

func Test_settingResource_Schema_ipsec(t *testing.T) {
	r := &settingResource{}
	var resp fwresource.SchemaResponse
	r.Schema(context.Background(), fwresource.SchemaRequest{}, &resp)
	if _, ok := resp.Schema.Attributes["ipsec"]; !ok {
		t.Fatal("schema is missing the ipsec section attribute")
	}
}

func TestAccSettingResource_ipsec(t *testing.T) {
	// ipsec requires a real gateway-class controller; the demo/simulation
	// controller rejects the section. Verified directly: GET returns an
	// empty {} document (unlike e.g. netflow, which is fully populated),
	// and PUT with a minimal payload (with or without "key") fails
	// identically with api.err.Invalid (400) — not a payload-shape bug in
	// this test's config.
	if os.Getenv("UNIFI_SKIP_CONTAINER") == "" {
		t.Skip("ipsec requires a real controller; set UNIFI_SKIP_CONTAINER to run")
	}

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: `
resource "unifi_setting" "test" {
  ipsec = {
    ikev2_reauthentication_method = "make-before-break"
  }
}
`,
				Check: resource.TestCheckResourceAttr(
					"unifi_setting.test", "ipsec.ikev2_reauthentication_method", "make-before-break",
				),
			},
		},
	})
}
