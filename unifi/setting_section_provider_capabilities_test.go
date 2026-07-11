package unifi

import (
	"context"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	fwresource "github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
)

func Test_providerCapabilitiesModelToData(t *testing.T) {
	var diags diag.Diagnostics
	m := &settingProviderCapabilitiesModel{
		Download: types.Int64Value(1000000),
		Upload:   types.Int64Null(),
	}
	// Raw fields go-unifi does not model (there are none known today, but the
	// overlay must still preserve whatever it is given) must round-trip.
	data := map[string]any{"unmodeled_field": "keep", "upload": float64(500000)}

	providerCapabilitiesModelToData(m, data, &diags)
	if diags.HasError() {
		t.Fatal(diags)
	}

	if data["download"] != int64(1000000) {
		t.Fatalf("download = %v", data["download"])
	}
	if data["upload"] != float64(500000) {
		t.Fatal("null upload overwrote remote value")
	}
	if data["unmodeled_field"] != "keep" {
		t.Fatal("unmodeled_field was clobbered")
	}
}

func Test_providerCapabilitiesSettingToModel(t *testing.T) {
	m := providerCapabilitiesSettingToModel(&settings.ProviderCapabilities{
		Download: 1000000,
		Upload:   1000000,
	})
	if m.Download.ValueInt64() != 1000000 || m.Upload.ValueInt64() != 1000000 {
		t.Fatalf("download/upload = %v/%v", m.Download, m.Upload)
	}

	empty := providerCapabilitiesSettingToModel(&settings.ProviderCapabilities{})
	if !empty.Download.IsNull() || !empty.Upload.IsNull() {
		t.Fatalf("zero-value download/upload should map to null, got %v/%v", empty.Download, empty.Upload)
	}
}

func Test_settingResource_Schema_providerCapabilities(t *testing.T) {
	r := &settingResource{}
	var resp fwresource.SchemaResponse
	r.Schema(context.Background(), fwresource.SchemaRequest{}, &resp)
	if _, ok := resp.Schema.Attributes["provider_capabilities"]; !ok {
		t.Fatal("schema is missing the provider_capabilities section attribute")
	}
}

func TestAccSettingResource_providerCapabilities(t *testing.T) {
	// provider_capabilities requires a real ISP-gateway/UDM-class controller;
	// the demo/simulation controller doesn't carry this setting key at all
	// (absent from GET /rest/setting) and rejects any PUT to it — even a
	// field-free {"key":"provider_capabilities"} payload — with
	// api.err.Invalid (400). Verified with a throwaway probe against the
	// demo container: GET returned 29 settings, none keyed
	// "provider_capabilities"; PUT with no fields and PUT with only
	// "download" both got the same 400, ruling out a field-validation issue.
	if os.Getenv("UNIFI_SKIP_CONTAINER") == "" {
		t.Skip("provider_capabilities requires a real controller; set UNIFI_SKIP_CONTAINER to run")
	}

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: `
resource "unifi_setting" "test" {
  provider_capabilities = {
    download = 1000000
    upload   = 500000
  }
}
`,
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"unifi_setting.test", "provider_capabilities.download", "1000000",
					),
					resource.TestCheckResourceAttr(
						"unifi_setting.test", "provider_capabilities.upload", "500000",
					),
				),
			},
		},
	})
}
