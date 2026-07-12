package unifi

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
)

// TestCountrySection_GoldenReproduction proves overlay() reproduces the
// Task-9 golden PUT body (byte-identical after stripping the routing "key"
// field) for the representative model used to capture that golden.
func TestCountrySection_GoldenReproduction(t *testing.T) {
	ctx := context.Background()
	sec := countrySection{}

	m := settingCountryModel{
		Code: types.Int64Value(840),
	}
	obj, diags := types.ObjectValueFrom(ctx, countryAttrTypes, m)
	if diags.HasError() {
		t.Fatalf("building country object: %v", diags)
	}

	model := settingResourceModel{Country: obj}
	prior := settingResourceModel{}
	snap := newRawSettings(nil) // empty snapshot: section absent

	rs, configured, oDiags := sec.overlay(ctx, model, prior, snap)
	if oDiags.HasError() {
		t.Fatalf("overlay diagnostics: %v", oDiags)
	}
	if !configured {
		t.Fatalf("overlay configured = false, want true")
	}
	if rs.Key != "country" {
		t.Errorf("rs.Key = %q, want %q", rs.Key, "country")
	}

	assertPUTBodyMatchesGolden(t, rs, goldenCountry)
}

// TestCountrySection_DecodeRoundTrip proves decode() reads a snapshot
// section's fields into model.Country.
func TestCountrySection_DecodeRoundTrip(t *testing.T) {
	ctx := context.Background()
	sec := countrySection{}

	snap := newRawSettings([]settings.RawSetting{{
		BaseSetting: settings.BaseSetting{Key: "country"},
		Data: map[string]any{
			"code": float64(840),
		},
	}})

	prior := settingResourceModel{}
	model := settingResourceModel{}

	diags := sec.decode(ctx, snap, prior, &model)
	if diags.HasError() {
		t.Fatalf("decode diagnostics: %v", diags)
	}

	if model.Country.IsNull() || model.Country.IsUnknown() {
		t.Fatalf("model.Country is null/unknown after decode")
	}

	var got settingCountryModel
	if diags := model.Country.As(ctx, &got, basetypes.ObjectAsOptions{}); diags.HasError() {
		t.Fatalf("extracting settingCountryModel: %v", diags)
	}

	if got.Code.ValueInt64() != 840 {
		t.Errorf("Code = %v, want 840", got.Code.ValueInt64())
	}
}

// TestCountrySection_Preservation proves overlay() preserves an unmodeled
// key already present in the snapshot's section data.
func TestCountrySection_Preservation(t *testing.T) {
	ctx := context.Background()
	sec := countrySection{}

	snap := newRawSettings([]settings.RawSetting{{
		BaseSetting: settings.BaseSetting{Key: "country"},
		Data: map[string]any{
			"code":        float64(840),
			"x_unmanaged": "keep",
		},
	}})

	m := settingCountryModel{
		Code: types.Int64Value(840),
	}
	obj, diags := types.ObjectValueFrom(ctx, countryAttrTypes, m)
	if diags.HasError() {
		t.Fatalf("building country object: %v", diags)
	}

	model := settingResourceModel{Country: obj}
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

// TestCountrySection_NotConfigured proves overlay() returns configured ==
// false and a zero-value RawSetting when the section is not configured
// (null object) in the model.
func TestCountrySection_NotConfigured(t *testing.T) {
	ctx := context.Background()
	sec := countrySection{}

	model := settingResourceModel{Country: types.ObjectNull(countryAttrTypes)}
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

// TestCountrySection_InterfaceWiring is a light structural check that the
// section is registered and its key()/attrName() match the section name,
// matching the pattern the registry-level tests (TestRegistryKeysUnique,
// TestSectionOwnershipCoversSchema) already sweep over settingSections.
func TestCountrySection_InterfaceWiring(t *testing.T) {
	sec := countrySection{}
	if sec.key() != "country" {
		t.Errorf("key() = %q, want %q", sec.key(), "country")
	}
	if sec.attrName() != "country" {
		t.Errorf("attrName() = %q, want %q", sec.attrName(), "country")
	}

	var found bool
	for _, s := range settingSections {
		if _, ok := s.(countrySection); ok {
			found = true
		}
	}
	if !found {
		t.Errorf("countrySection not found in settingSections registry")
	}
}
