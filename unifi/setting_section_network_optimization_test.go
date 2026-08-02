package unifi

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
)

// TestNetworkOptimizationSection_GoldenReproduction proves overlay()
// reproduces the golden PUT body (byte-identical after stripping the
// routing "key" field) for the representative model used to capture that
// golden.
func TestNetworkOptimizationSection_GoldenReproduction(t *testing.T) {
	ctx := context.Background()
	sec := networkOptimizationSection{}

	m := settingNetworkOptimizationModel{
		Enabled: types.BoolValue(true),
	}
	obj, diags := types.ObjectValueFrom(ctx, networkOptimizationAttrTypes, m)
	if diags.HasError() {
		t.Fatalf("building network_optimization object: %v", diags)
	}

	model := settingResourceModel{NetworkOpt: obj}
	prior := settingResourceModel{}
	snap := newRawSettings(nil) // empty snapshot: section absent

	rs, configured, oDiags := sec.overlay(ctx, model, prior, snap)
	if oDiags.HasError() {
		t.Fatalf("overlay diagnostics: %v", oDiags)
	}
	if !configured {
		t.Fatalf("overlay configured = false, want true")
	}
	if rs.Key != "network_optimization" {
		t.Errorf("rs.Key = %q, want %q", rs.Key, "network_optimization")
	}

	assertPUTBodyMatchesGolden(t, rs, goldenNetworkOptimization)
}

// TestNetworkOptimizationSection_DecodeRoundTrip proves decode() reads a
// snapshot section's fields into model.NetworkOpt.
func TestNetworkOptimizationSection_DecodeRoundTrip(t *testing.T) {
	ctx := context.Background()
	sec := networkOptimizationSection{}

	snap := newRawSettings([]settings.RawSetting{{
		BaseSetting: settings.BaseSetting{Key: "network_optimization"},
		Data: map[string]any{
			"enabled": true,
		},
	}})

	prior := settingResourceModel{}
	model := settingResourceModel{}

	diags := sec.decode(ctx, snap, prior, &model)
	if diags.HasError() {
		t.Fatalf("decode diagnostics: %v", diags)
	}

	if model.NetworkOpt.IsNull() || model.NetworkOpt.IsUnknown() {
		t.Fatalf("model.NetworkOpt is null/unknown after decode")
	}

	var got settingNetworkOptimizationModel
	if diags := model.NetworkOpt.As(ctx, &got, basetypes.ObjectAsOptions{}); diags.HasError() {
		t.Fatalf("extracting settingNetworkOptimizationModel: %v", diags)
	}

	if got.Enabled.ValueBool() != true {
		t.Errorf("Enabled = %v, want true", got.Enabled.ValueBool())
	}
}

// TestNetworkOptimizationSection_Preservation proves overlay() preserves an
// unmodeled key already present in the snapshot's section data.
func TestNetworkOptimizationSection_Preservation(t *testing.T) {
	ctx := context.Background()
	sec := networkOptimizationSection{}

	snap := newRawSettings([]settings.RawSetting{{
		BaseSetting: settings.BaseSetting{Key: "network_optimization"},
		Data: map[string]any{
			"enabled":     true,
			"x_unmanaged": "keep",
		},
	}})

	m := settingNetworkOptimizationModel{
		Enabled: types.BoolValue(true),
	}
	obj, diags := types.ObjectValueFrom(ctx, networkOptimizationAttrTypes, m)
	if diags.HasError() {
		t.Fatalf("building network_optimization object: %v", diags)
	}

	model := settingResourceModel{NetworkOpt: obj}
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

// TestNetworkOptimizationSection_NotConfigured proves overlay() returns
// configured == false and a zero-value RawSetting when the section is not
// configured (null object) in the model.
func TestNetworkOptimizationSection_NotConfigured(t *testing.T) {
	ctx := context.Background()
	sec := networkOptimizationSection{}

	model := settingResourceModel{NetworkOpt: types.ObjectNull(networkOptimizationAttrTypes)}
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

// TestNetworkOptimizationSection_InterfaceWiring is a light structural
// check that the section is registered and its key()/attrName() match the
// section name, matching the pattern the registry-level tests
// (TestRegistryKeysUnique, TestSectionOwnershipCoversSchema) already sweep
// over settingSections.
func TestNetworkOptimizationSection_InterfaceWiring(t *testing.T) {
	sec := networkOptimizationSection{}
	if sec.key() != "network_optimization" {
		t.Errorf("key() = %q, want %q", sec.key(), "network_optimization")
	}
	if sec.attrName() != "network_optimization" {
		t.Errorf("attrName() = %q, want %q", sec.attrName(), "network_optimization")
	}

	var found bool
	for _, s := range settingSections {
		if _, ok := s.(networkOptimizationSection); ok {
			found = true
		}
	}
	if !found {
		t.Errorf("networkOptimizationSection not found in settingSections registry")
	}
}
