package unifi

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
)

const goldenLocale = `{"key":"locale","timezone":"Etc/UTC"}`

// TestLocaleSection_GoldenReproduction proves overlay() reproduces the
// section's golden PUT body for the representative model.
func TestLocaleSection_GoldenReproduction(t *testing.T) {
	ctx := context.Background()
	sec := localeSection{}

	m := settingLocaleModel{Timezone: types.StringValue("Etc/UTC")}
	obj, diags := types.ObjectValueFrom(ctx, localeAttrTypes, m)
	if diags.HasError() {
		t.Fatalf("building locale object: %v", diags)
	}

	model := settingResourceModel{Locale: obj}
	prior := settingResourceModel{}
	snap := newRawSettings(nil)

	rs, configured, oDiags := sec.overlay(ctx, model, prior, snap)
	if oDiags.HasError() {
		t.Fatalf("overlay diagnostics: %v", oDiags)
	}
	if !configured {
		t.Fatalf("overlay configured = false, want true")
	}
	if rs.Key != "locale" {
		t.Errorf("rs.Key = %q, want %q", rs.Key, "locale")
	}

	assertPUTBodyMatchesGolden(t, rs, goldenLocale)
}

// TestLocaleSection_DecodeRoundTrip proves decode() reads a snapshot
// section's fields into model.Locale.
func TestLocaleSection_DecodeRoundTrip(t *testing.T) {
	ctx := context.Background()
	sec := localeSection{}

	snap := newRawSettings([]settings.RawSetting{{
		BaseSetting: settings.BaseSetting{Key: "locale"},
		Data: map[string]any{
			"timezone": "Etc/UTC",
		},
	}})

	prior := settingResourceModel{}
	model := settingResourceModel{}

	diags := sec.decode(ctx, snap, prior, &model)
	if diags.HasError() {
		t.Fatalf("decode diagnostics: %v", diags)
	}

	if model.Locale.IsNull() || model.Locale.IsUnknown() {
		t.Fatalf("model.Locale is null/unknown after decode")
	}

	var got settingLocaleModel
	if diags := model.Locale.As(ctx, &got, basetypes.ObjectAsOptions{}); diags.HasError() {
		t.Fatalf("extracting settingLocaleModel: %v", diags)
	}

	if got.Timezone.ValueString() != "Etc/UTC" {
		t.Errorf("Timezone = %q, want %q", got.Timezone.ValueString(), "Etc/UTC")
	}
}

// TestLocaleSection_Preservation proves overlay() preserves an unmodeled
// key already present in the snapshot's section data.
func TestLocaleSection_Preservation(t *testing.T) {
	ctx := context.Background()
	sec := localeSection{}

	snap := newRawSettings([]settings.RawSetting{{
		BaseSetting: settings.BaseSetting{Key: "locale"},
		Data: map[string]any{
			"timezone":    "Etc/UTC",
			"x_unmanaged": "keep",
		},
	}})

	m := settingLocaleModel{Timezone: types.StringValue("Etc/UTC")}
	obj, diags := types.ObjectValueFrom(ctx, localeAttrTypes, m)
	if diags.HasError() {
		t.Fatalf("building locale object: %v", diags)
	}

	model := settingResourceModel{Locale: obj}
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

// TestLocaleSection_NotConfigured proves overlay() returns configured ==
// false and a zero-value RawSetting when the section is not configured.
func TestLocaleSection_NotConfigured(t *testing.T) {
	ctx := context.Background()
	sec := localeSection{}

	model := settingResourceModel{Locale: types.ObjectNull(localeAttrTypes)}
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

// TestLocaleSection_InterfaceWiring is a light structural check that the
// section is registered and its key()/attrName() match the section name.
func TestLocaleSection_InterfaceWiring(t *testing.T) {
	sec := localeSection{}
	if sec.key() != "locale" {
		t.Errorf("key() = %q, want %q", sec.key(), "locale")
	}
	if sec.attrName() != "locale" {
		t.Errorf("attrName() = %q, want %q", sec.attrName(), "locale")
	}

	var found bool
	for _, s := range settingSections {
		if _, ok := s.(localeSection); ok {
			found = true
		}
	}
	if !found {
		t.Errorf("localeSection not found in settingSections registry")
	}
}
