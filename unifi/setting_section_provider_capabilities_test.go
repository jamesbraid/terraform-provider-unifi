package unifi

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	fwresource "github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
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
