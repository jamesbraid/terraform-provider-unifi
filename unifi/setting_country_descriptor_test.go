package unifi

import (
	"context"
	"testing"

	resource_setting "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_setting"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/resourcekit"
)

// TestCountryKitSpecConformance runs the same conformance instruments every
// other kit descriptor's test applies (see setting_mgmt_descriptor_test.go's
// TestMgmtKitSpecConformance), scoped to country's own nested schema rather
// than a whole resource's, since country is one section of unifi_setting
// rather than a surface of its own.
func TestCountryKitSpecConformance(t *testing.T) {
	ctx := context.Background()
	spec := countryKitSpec()
	for _, problem := range resourcekit.WireNameProblems(spec) {
		t.Error(problem)
	}
	for _, problem := range resourcekit.NestedProblems(spec) {
		t.Error(problem)
	}
	built := countryNestedSchema(ctx)
	for _, problem := range resourcekit.ElideProblems(spec, built) {
		t.Error(problem)
	}
	for _, problem := range resourcekit.ZeroReadProblems(spec, built) {
		t.Error(problem)
	}
}

// TestCountryNestedSchemaHasExactlyItsAttributes guards countryNestedSchema's
// type assertion against a generator regression: "country" moving off
// SingleNestedAttribute would panic every conformance test above instead of
// naming the actual problem, so this pins the shape ahead of that.
func TestCountryNestedSchemaHasExactlyItsAttributes(t *testing.T) {
	ctx := context.Background()
	built := resource_setting.SettingResourceSchema(ctx)
	if _, ok := built.Attributes["country"]; !ok {
		t.Fatal(`the generated setting schema has no "country" attribute`)
	}
	nested := countryNestedSchema(ctx)
	if len(nested.Attributes) != 1 {
		t.Errorf("country has %d attribute(s), want 1; update countryKitSpec and this count together",
			len(nested.Attributes))
	}
}
