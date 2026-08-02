package unifi

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
)

// TestDpiSection_GoldenReproduction proves overlay() reproduces the Task-9
// golden PUT body (byte-identical after stripping the routing "key" field)
// for the representative model used to capture that golden.
func TestDpiSection_GoldenReproduction(t *testing.T) {
	ctx := context.Background()
	sec := dpiSection{}

	m := settingDpiModel{
		Enabled:               types.BoolValue(true),
		FingerprintingEnabled: types.BoolValue(false),
	}
	obj, diags := types.ObjectValueFrom(ctx, dpiAttrTypes, m)
	if diags.HasError() {
		t.Fatalf("building dpi object: %v", diags)
	}

	model := settingResourceModel{Dpi: obj}
	prior := settingResourceModel{}
	snap := newRawSettings(nil) // empty snapshot: section absent

	rs, configured, oDiags := sec.overlay(ctx, model, prior, snap)
	if oDiags.HasError() {
		t.Fatalf("overlay diagnostics: %v", oDiags)
	}
	if !configured {
		t.Fatalf("overlay configured = false, want true")
	}
	if rs.Key != "dpi" {
		t.Errorf("rs.Key = %q, want %q", rs.Key, "dpi")
	}

	assertPUTBodyMatchesGolden(t, rs, goldenDpi)
}

// TestDpiSection_DecodeRoundTrip proves decode() reads a snapshot section's
// fields into model.Dpi. The snapshot fixture uses the RAW controller wire
// key "fingerprintingEnabled" (camelCase) - not the schema/tfsdk leaf name
// "fingerprinting_enabled".
func TestDpiSection_DecodeRoundTrip(t *testing.T) {
	ctx := context.Background()
	sec := dpiSection{}

	snap := newRawSettings([]settings.RawSetting{{
		BaseSetting: settings.BaseSetting{Key: "dpi"},
		Data: map[string]any{
			"enabled":               true,
			"fingerprintingEnabled": false,
		},
	}})

	prior := settingResourceModel{}
	model := settingResourceModel{}

	diags := sec.decode(ctx, snap, prior, &model)
	if diags.HasError() {
		t.Fatalf("decode diagnostics: %v", diags)
	}

	if model.Dpi.IsNull() || model.Dpi.IsUnknown() {
		t.Fatalf("model.Dpi is null/unknown after decode")
	}

	var got settingDpiModel
	if diags := model.Dpi.As(ctx, &got, basetypes.ObjectAsOptions{}); diags.HasError() {
		t.Fatalf("extracting settingDpiModel: %v", diags)
	}

	if !got.Enabled.ValueBool() {
		t.Errorf("Enabled = %v, want true", got.Enabled)
	}
	if got.FingerprintingEnabled.ValueBool() {
		t.Errorf("FingerprintingEnabled = %v, want false", got.FingerprintingEnabled)
	}
}

// TestDpiSection_Preservation proves overlay() preserves an unmodeled key
// already present in the snapshot's section data.
func TestDpiSection_Preservation(t *testing.T) {
	ctx := context.Background()
	sec := dpiSection{}

	snap := newRawSettings([]settings.RawSetting{{
		BaseSetting: settings.BaseSetting{Key: "dpi"},
		Data: map[string]any{
			"enabled":               true,
			"fingerprintingEnabled": false,
			"x_unmanaged":           "keep",
		},
	}})

	m := settingDpiModel{
		Enabled:               types.BoolValue(true),
		FingerprintingEnabled: types.BoolValue(false),
	}
	obj, diags := types.ObjectValueFrom(ctx, dpiAttrTypes, m)
	if diags.HasError() {
		t.Fatalf("building dpi object: %v", diags)
	}

	model := settingResourceModel{Dpi: obj}
	prior := settingResourceModel{}

	rs, configured, oDiags := sec.overlay(ctx, model, prior, snap)
	if oDiags.HasError() {
		t.Fatalf("overlay diagnostics: %v", oDiags)
	}
	if !configured {
		t.Fatalf("overlay configured = false, want true")
	}

	if got, ok := rs.Data["x_unmanaged"]; !ok || got != "keep" {
		t.Errorf("rs.Data[%q] = %v (ok=%v), want %q", "x_unmanaged", got, ok, "keep")
	}
}

// TestDpiSection_NotConfigured proves overlay() returns configured == false
// and a zero-value RawSetting when the section is not configured (null
// object) in the model.
func TestDpiSection_NotConfigured(t *testing.T) {
	ctx := context.Background()
	sec := dpiSection{}

	model := settingResourceModel{Dpi: types.ObjectNull(dpiAttrTypes)}
	prior := settingResourceModel{}
	snap := newRawSettings(nil)

	rs, configured, oDiags := sec.overlay(ctx, model, prior, snap)
	if oDiags.HasError() {
		t.Fatalf("overlay diagnostics: %v", oDiags)
	}
	if configured {
		t.Fatalf("overlay configured = true, want false")
	}
	if rs.Key != "" || len(rs.Data) != 0 {
		t.Errorf("overlay returned non-zero RawSetting when not configured: %+v", rs)
	}
}

// TestDpiSection_InterfaceWiring is a light structural check that the
// section is registered and its key()/attrName() match the section name,
// matching the pattern the registry-level tests (TestRegistryKeysUnique,
// TestSectionOwnershipCoversSchema) already sweep over settingSections.
func TestDpiSection_InterfaceWiring(t *testing.T) {
	sec := dpiSection{}
	if sec.key() != "dpi" {
		t.Errorf("key() = %q, want %q", sec.key(), "dpi")
	}
	if sec.attrName() != "dpi" {
		t.Errorf("attrName() = %q, want %q", sec.attrName(), "dpi")
	}

	var found bool
	for _, s := range settingSections {
		if _, ok := s.(dpiSection); ok {
			found = true
		}
	}
	if !found {
		t.Errorf("dpiSection not found in settingSections registry")
	}
}
