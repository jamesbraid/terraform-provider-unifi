package unifi

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
)

// assertPUTBodyMatchesGolden compares a section's overlay RawSetting body to
// its Task-9 golden, EXCLUDING the "key" field: the legacy typed converter
// left key="" while the new RawSetting sets key=<section> for endpoint
// routing. key() is verified separately by the registry test; the "key" wire
// value is a benign routing artifact.
//
// Shared by every section's migration test (Tasks 10-22); Task 10 introduces
// it as the scalar template.
func assertPUTBodyMatchesGolden(t *testing.T, rs settings.RawSetting, golden string) {
	t.Helper()

	gotBytes, err := json.Marshal(&rs)
	if err != nil {
		t.Fatalf("json.Marshal(&rs): %v", err)
	}
	var gotMap map[string]any
	if err := json.Unmarshal(gotBytes, &gotMap); err != nil {
		t.Fatalf("unmarshal got: %v (input: %s)", err, string(gotBytes))
	}
	delete(gotMap, "key")

	var wantMap map[string]any
	if err := json.Unmarshal([]byte(golden), &wantMap); err != nil {
		t.Fatalf("unmarshal golden: %v (input: %s)", err, golden)
	}
	delete(wantMap, "key")

	gotNorm, err := json.Marshal(gotMap)
	if err != nil {
		t.Fatalf("re-marshal got: %v", err)
	}
	wantNorm, err := json.Marshal(wantMap)
	if err != nil {
		t.Fatalf("re-marshal want: %v", err)
	}

	if string(gotNorm) != string(wantNorm) {
		t.Errorf("PUT body (key-stripped) mismatch:\n got:  %s\n want: %s", gotNorm, wantNorm)
	}
}

// TestAutoSpeedtestSection_GoldenReproduction proves overlay() reproduces
// the Task-9 golden PUT body (byte-identical after stripping the routing
// "key" field) for the representative model used to capture that golden.
func TestAutoSpeedtestSection_GoldenReproduction(t *testing.T) {
	ctx := context.Background()
	sec := autoSpeedtestSection{}

	m := settingAutoSpeedtestModel{
		Enabled:  types.BoolValue(true),
		CronExpr: types.StringValue("0 0 * * *"),
	}
	obj, diags := types.ObjectValueFrom(ctx, autoSpeedtestAttrTypes, m)
	if diags.HasError() {
		t.Fatalf("building auto_speedtest object: %v", diags)
	}

	model := settingResourceModel{AutoSpeedtest: obj}
	prior := settingResourceModel{}
	snap := newRawSettings(nil) // empty snapshot: section absent

	rs, configured, oDiags := sec.overlay(ctx, model, prior, snap)
	if oDiags.HasError() {
		t.Fatalf("overlay diagnostics: %v", oDiags)
	}
	if !configured {
		t.Fatalf("overlay configured = false, want true")
	}
	if rs.Key != "auto_speedtest" {
		t.Errorf("rs.Key = %q, want %q", rs.Key, "auto_speedtest")
	}

	assertPUTBodyMatchesGolden(t, rs, goldenAutoSpeedtest)
}

// TestAutoSpeedtestSection_DecodeRoundTrip proves decode() reads a snapshot
// section's fields into model.AutoSpeedtest.
func TestAutoSpeedtestSection_DecodeRoundTrip(t *testing.T) {
	ctx := context.Background()
	sec := autoSpeedtestSection{}

	snap := newRawSettings([]settings.RawSetting{{
		BaseSetting: settings.BaseSetting{Key: "auto_speedtest"},
		Data: map[string]any{
			"enabled":   true,
			"cron_expr": "0 0 * * *",
		},
	}})

	prior := settingResourceModel{}
	model := settingResourceModel{}

	diags := sec.decode(ctx, snap, prior, &model)
	if diags.HasError() {
		t.Fatalf("decode diagnostics: %v", diags)
	}

	if model.AutoSpeedtest.IsNull() || model.AutoSpeedtest.IsUnknown() {
		t.Fatalf("model.AutoSpeedtest is null/unknown after decode")
	}

	var got settingAutoSpeedtestModel
	if diags := model.AutoSpeedtest.As(ctx, &got, basetypes.ObjectAsOptions{}); diags.HasError() {
		t.Fatalf("extracting settingAutoSpeedtestModel: %v", diags)
	}

	if !got.Enabled.ValueBool() {
		t.Errorf("Enabled = %v, want true", got.Enabled)
	}
	if got.CronExpr.ValueString() != "0 0 * * *" {
		t.Errorf("CronExpr = %q, want %q", got.CronExpr.ValueString(), "0 0 * * *")
	}
}

// TestAutoSpeedtestSection_PresentEmpty proves the present-empty codec
// contract (permitted delta 1): a snapshot with cron_expr:"" decodes to
// StringValue(""), never collapsed to null.
func TestAutoSpeedtestSection_PresentEmpty(t *testing.T) {
	ctx := context.Background()
	sec := autoSpeedtestSection{}

	snap := newRawSettings([]settings.RawSetting{{
		BaseSetting: settings.BaseSetting{Key: "auto_speedtest"},
		Data: map[string]any{
			"enabled":   false,
			"cron_expr": "",
		},
	}})

	prior := settingResourceModel{}
	model := settingResourceModel{}

	diags := sec.decode(ctx, snap, prior, &model)
	if diags.HasError() {
		t.Fatalf("decode diagnostics: %v", diags)
	}

	var got settingAutoSpeedtestModel
	if diags := model.AutoSpeedtest.As(ctx, &got, basetypes.ObjectAsOptions{}); diags.HasError() {
		t.Fatalf("extracting settingAutoSpeedtestModel: %v", diags)
	}

	if got.CronExpr.IsNull() {
		t.Errorf("CronExpr is null, want present-empty StringValue(\"\")")
	}
	if got.CronExpr.ValueString() != "" {
		t.Errorf("CronExpr = %q, want empty string", got.CronExpr.ValueString())
	}
}

// TestAutoSpeedtestSection_Preservation proves overlay() preserves an
// unmodeled key already present in the snapshot's section data.
func TestAutoSpeedtestSection_Preservation(t *testing.T) {
	ctx := context.Background()
	sec := autoSpeedtestSection{}

	snap := newRawSettings([]settings.RawSetting{{
		BaseSetting: settings.BaseSetting{Key: "auto_speedtest"},
		Data: map[string]any{
			"enabled":     true,
			"cron_expr":   "0 0 * * *",
			"x_unmanaged": "keep",
		},
	}})

	m := settingAutoSpeedtestModel{
		Enabled:  types.BoolValue(true),
		CronExpr: types.StringValue("0 0 * * *"),
	}
	obj, diags := types.ObjectValueFrom(ctx, autoSpeedtestAttrTypes, m)
	if diags.HasError() {
		t.Fatalf("building auto_speedtest object: %v", diags)
	}

	model := settingResourceModel{AutoSpeedtest: obj}
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

// TestAutoSpeedtestSection_NotConfigured proves overlay() returns
// configured == false and a zero-value RawSetting when the section is not
// configured (null object) in the model.
func TestAutoSpeedtestSection_NotConfigured(t *testing.T) {
	ctx := context.Background()
	sec := autoSpeedtestSection{}

	model := settingResourceModel{AutoSpeedtest: types.ObjectNull(autoSpeedtestAttrTypes)}
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

// TestAutoSpeedtestSection_InterfaceWiring is a light structural check that
// the section is registered and its key()/attrName() match the section
// name, matching the pattern the registry-level tests
// (TestRegistryKeysUnique, TestSectionOwnershipCoversSchema) already sweep
// over settingSections.
func TestAutoSpeedtestSection_InterfaceWiring(t *testing.T) {
	sec := autoSpeedtestSection{}
	if sec.key() != "auto_speedtest" {
		t.Errorf("key() = %q, want %q", sec.key(), "auto_speedtest")
	}
	if sec.attrName() != "auto_speedtest" {
		t.Errorf("attrName() = %q, want %q", sec.attrName(), "auto_speedtest")
	}

	var found bool
	for _, s := range settingSections {
		if _, ok := s.(autoSpeedtestSection); ok {
			found = true
		}
	}
	if !found {
		t.Errorf("autoSpeedtestSection not found in settingSections registry")
	}
}
