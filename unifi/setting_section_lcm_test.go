package unifi

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
)

// TestLcmSection_GoldenReproduction proves overlay() reproduces the Task-9
// golden PUT body (byte-identical after stripping the routing "key" field)
// for the representative model used to capture that golden.
func TestLcmSection_GoldenReproduction(t *testing.T) {
	ctx := context.Background()
	sec := lcmSection{}

	m := settingLcmModel{
		Enabled:     types.BoolValue(true),
		Brightness:  types.Int64Value(50),
		IdleTimeout: types.Int64Value(300),
		Sync:        types.BoolValue(true),
		TouchEvent:  types.BoolValue(false),
	}
	obj, diags := types.ObjectValueFrom(ctx, lcmAttrTypes, m)
	if diags.HasError() {
		t.Fatalf("building lcm object: %v", diags)
	}

	model := settingResourceModel{Lcm: obj}
	prior := settingResourceModel{}
	snap := newRawSettings(nil) // empty snapshot: section absent

	rs, configured, oDiags := sec.overlay(ctx, model, prior, snap)
	if oDiags.HasError() {
		t.Fatalf("overlay diagnostics: %v", oDiags)
	}
	if !configured {
		t.Fatalf("overlay configured = false, want true")
	}
	if rs.Key != "lcm" {
		t.Errorf("rs.Key = %q, want %q", rs.Key, "lcm")
	}

	assertPUTBodyMatchesGolden(t, rs, goldenLcm)
}

// TestLcmSection_DecodeRoundTrip proves decode() reads a snapshot section's
// fields into model.Lcm.
func TestLcmSection_DecodeRoundTrip(t *testing.T) {
	ctx := context.Background()
	sec := lcmSection{}

	snap := newRawSettings([]settings.RawSetting{{
		BaseSetting: settings.BaseSetting{Key: "lcm"},
		Data: map[string]any{
			"enabled":      true,
			"brightness":   float64(50),
			"idle_timeout": float64(300),
			"sync":         true,
			"touch_event":  false,
		},
	}})

	prior := settingResourceModel{}
	model := settingResourceModel{}

	diags := sec.decode(ctx, snap, prior, &model)
	if diags.HasError() {
		t.Fatalf("decode diagnostics: %v", diags)
	}

	if model.Lcm.IsNull() || model.Lcm.IsUnknown() {
		t.Fatalf("model.Lcm is null/unknown after decode")
	}

	var got settingLcmModel
	if diags := model.Lcm.As(ctx, &got, basetypes.ObjectAsOptions{}); diags.HasError() {
		t.Fatalf("extracting settingLcmModel: %v", diags)
	}

	if !got.Enabled.ValueBool() {
		t.Errorf("Enabled = %v, want true", got.Enabled)
	}
	if got.Brightness.ValueInt64() != 50 {
		t.Errorf("Brightness = %v, want 50", got.Brightness.ValueInt64())
	}
	if got.IdleTimeout.ValueInt64() != 300 {
		t.Errorf("IdleTimeout = %v, want 300", got.IdleTimeout.ValueInt64())
	}
	if !got.Sync.ValueBool() {
		t.Errorf("Sync = %v, want true", got.Sync)
	}
	if got.TouchEvent.ValueBool() {
		t.Errorf("TouchEvent = %v, want false", got.TouchEvent)
	}
}

// TestLcmSection_Preservation proves overlay() preserves an unmodeled key
// already present in the snapshot's section data.
func TestLcmSection_Preservation(t *testing.T) {
	ctx := context.Background()
	sec := lcmSection{}

	snap := newRawSettings([]settings.RawSetting{{
		BaseSetting: settings.BaseSetting{Key: "lcm"},
		Data: map[string]any{
			"enabled":      true,
			"brightness":   float64(50),
			"idle_timeout": float64(300),
			"sync":         true,
			"touch_event":  false,
			"x_unmanaged":  "keep",
		},
	}})

	m := settingLcmModel{
		Enabled:     types.BoolValue(true),
		Brightness:  types.Int64Value(50),
		IdleTimeout: types.Int64Value(300),
		Sync:        types.BoolValue(true),
		TouchEvent:  types.BoolValue(false),
	}
	obj, diags := types.ObjectValueFrom(ctx, lcmAttrTypes, m)
	if diags.HasError() {
		t.Fatalf("building lcm object: %v", diags)
	}

	model := settingResourceModel{Lcm: obj}
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

// TestLcmSection_NotConfigured proves overlay() returns configured == false
// and a zero-value RawSetting when the section is not configured (null
// object) in the model.
func TestLcmSection_NotConfigured(t *testing.T) {
	ctx := context.Background()
	sec := lcmSection{}

	model := settingResourceModel{Lcm: types.ObjectNull(lcmAttrTypes)}
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

// TestLcmSection_InterfaceWiring is a light structural check that the
// section is registered and its key()/attrName() match the section name,
// matching the pattern the registry-level tests (TestRegistryKeysUnique,
// TestSectionOwnershipCoversSchema) already sweep over settingSections.
func TestLcmSection_InterfaceWiring(t *testing.T) {
	sec := lcmSection{}
	if sec.key() != "lcm" {
		t.Errorf("key() = %q, want %q", sec.key(), "lcm")
	}
	if sec.attrName() != "lcm" {
		t.Errorf("attrName() = %q, want %q", sec.attrName(), "lcm")
	}

	var found bool
	for _, s := range settingSections {
		if _, ok := s.(lcmSection); ok {
			found = true
		}
	}
	if !found {
		t.Errorf("lcmSection not found in settingSections registry")
	}
}
