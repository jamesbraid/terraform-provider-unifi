package unifi

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	resource_setting "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_setting"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/resourcekit"
)

// TestAutoSpeedtestSettingRoundTrip ports the deleted
// TestAutoSpeedtestSettingRoundTrip (setting_resource_test.go), which
// exercised autoSpeedtestModelToSetting/autoSpeedtestSettingToModel
// directly: model -> go-unifi setting -> model preserves the fields. It now
// drives the Spec's own ToSDK/ToModel instead of the deleted mappers.
func TestAutoSpeedtestSettingRoundTrip(t *testing.T) {
	ctx := context.Background()
	spec := autoSpeedtestKitSpec()

	in := &settingAutoSpeedtestModel{
		Enabled:  types.BoolValue(true),
		CronExpr: types.StringValue("0 3 * * *"),
	}
	setting, diags := spec.ToSDK(ctx, in)
	if diags.HasError() {
		t.Fatalf("ToSDK: %v", diags)
	}
	if !setting.Enabled || setting.CronExpr != "0 3 * * *" {
		t.Fatalf("ToSDK = %+v, want enabled cron=0 3 * * *", setting)
	}

	var out settingAutoSpeedtestModel
	if diags := spec.ToModel(ctx, setting, &out, ""); diags.HasError() {
		t.Fatalf("ToModel: %v", diags)
	}
	if !out.Enabled.ValueBool() || out.CronExpr.ValueString() != "0 3 * * *" {
		t.Errorf("ToModel = %+v, want enabled cron preserved", out)
	}
}

// TestAutoSpeedtestKitSpecConformance runs the same conformance instruments
// every other kit descriptor's test applies (see setting_mgmt_descriptor_test.go's
// TestMgmtKitSpecConformance), scoped to auto_speedtest's own nested schema
// rather than a whole resource's, since auto_speedtest is one section of
// unifi_setting rather than a surface of its own.
func TestAutoSpeedtestKitSpecConformance(t *testing.T) {
	ctx := context.Background()
	spec := autoSpeedtestKitSpec()
	for _, problem := range resourcekit.WireNameProblems(spec) {
		t.Error(problem)
	}
	for _, problem := range resourcekit.NestedProblems(spec) {
		t.Error(problem)
	}
	built := autoSpeedtestNestedSchema(ctx)
	for _, problem := range resourcekit.ElideProblems(spec, built) {
		t.Error(problem)
	}
	for _, problem := range resourcekit.ZeroReadProblems(spec, built) {
		t.Error(problem)
	}
}

// TestAutoSpeedtestNestedSchemaHasExactlyItsAttributes guards
// autoSpeedtestNestedSchema's type assertion against a generator regression:
// "auto_speedtest" moving off SingleNestedAttribute would panic every
// conformance test above instead of naming the actual problem, so this pins
// the shape ahead of that.
func TestAutoSpeedtestNestedSchemaHasExactlyItsAttributes(t *testing.T) {
	ctx := context.Background()
	built := resource_setting.SettingResourceSchema(ctx)
	if _, ok := built.Attributes["auto_speedtest"]; !ok {
		t.Fatal(`the generated setting schema has no "auto_speedtest" attribute`)
	}
	nested := autoSpeedtestNestedSchema(ctx)
	if len(nested.Attributes) != 2 {
		t.Errorf("auto_speedtest has %d attribute(s), want 2; update autoSpeedtestKitSpec and this count together",
			len(nested.Attributes))
	}
}
