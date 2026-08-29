package unifi

import (
	"context"
	"testing"

	resource_setting "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_setting"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/resourcekit"
)

// TestDpiKitSpecConformance runs the same conformance instruments every
// other kit descriptor's test applies (see setting_mgmt_descriptor_test.go's
// TestMgmtKitSpecConformance), scoped to dpi's own nested schema rather than
// a whole resource's, since dpi is one section of unifi_setting rather than
// a surface of its own.
func TestDpiKitSpecConformance(t *testing.T) {
	ctx := context.Background()
	spec := dpiKitSpec()
	for _, problem := range resourcekit.WireNameProblems(spec) {
		t.Error(problem)
	}
	for _, problem := range resourcekit.NestedProblems(spec) {
		t.Error(problem)
	}
	built := dpiNestedSchema(ctx)
	for _, problem := range resourcekit.ElideProblems(spec, built) {
		t.Error(problem)
	}
	for _, problem := range resourcekit.ZeroReadProblems(spec, built) {
		t.Error(problem)
	}
}

// TestDpiNestedSchemaHasExactlyItsAttributes guards dpiNestedSchema's type
// assertion against a generator regression: "dpi" moving off
// SingleNestedAttribute would panic every conformance test above instead of
// naming the actual problem, so this pins the shape ahead of that.
func TestDpiNestedSchemaHasExactlyItsAttributes(t *testing.T) {
	ctx := context.Background()
	built := resource_setting.SettingResourceSchema(ctx)
	if _, ok := built.Attributes["dpi"]; !ok {
		t.Fatal(`the generated setting schema has no "dpi" attribute`)
	}
	nested := dpiNestedSchema(ctx)
	if len(nested.Attributes) != 2 {
		t.Errorf("dpi has %d attribute(s), want 2; update dpiKitSpec and this count together",
			len(nested.Attributes))
	}
}
