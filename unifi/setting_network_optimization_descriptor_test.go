package unifi

import (
	"context"
	"testing"

	resource_setting "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_setting"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/resourcekit"
)

// TestNetworkOptimizationKitSpecConformance runs the same conformance
// instruments every other kit descriptor's test applies (see
// setting_mgmt_descriptor_test.go's TestMgmtKitSpecConformance), scoped to
// network_optimization's own nested schema rather than a whole resource's,
// since network_optimization is one section of unifi_setting rather than a
// surface of its own.
func TestNetworkOptimizationKitSpecConformance(t *testing.T) {
	ctx := context.Background()
	spec := networkOptimizationKitSpec()
	for _, problem := range resourcekit.WireNameProblems(spec) {
		t.Error(problem)
	}
	for _, problem := range resourcekit.NestedProblems(spec) {
		t.Error(problem)
	}
	built := networkOptimizationNestedSchema(ctx)
	for _, problem := range resourcekit.ElideProblems(spec, built) {
		t.Error(problem)
	}
	for _, problem := range resourcekit.ZeroReadProblems(spec, built) {
		t.Error(problem)
	}
}

// TestNetworkOptimizationNestedSchemaHasExactlyItsAttributes guards
// networkOptimizationNestedSchema's type assertion against a generator
// regression: "network_optimization" moving off SingleNestedAttribute would
// panic every conformance test above instead of naming the actual problem,
// so this pins the shape ahead of that.
func TestNetworkOptimizationNestedSchemaHasExactlyItsAttributes(t *testing.T) {
	ctx := context.Background()
	built := resource_setting.SettingResourceSchema(ctx)
	if _, ok := built.Attributes["network_optimization"]; !ok {
		t.Fatal(`the generated setting schema has no "network_optimization" attribute`)
	}
	nested := networkOptimizationNestedSchema(ctx)
	if len(nested.Attributes) != 1 {
		t.Errorf("network_optimization has %d attribute(s), want 1; update "+
			"networkOptimizationKitSpec and this count together", len(nested.Attributes))
	}
}
