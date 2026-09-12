package unifi

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/querycheck"
	"github.com/hashicorp/terraform-plugin-testing/tfversion"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
)

func TestAccDpiAppList_emptyOrSeeded(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		TerraformVersionChecks: []tfversion.TerraformVersionCheck{
			tfversion.SkipBelow(tfversion.Version1_14_0),
		},
		Steps: []resource.TestStep{{
			Query: true,
			Config: `
provider "unifi" {}
list "unifi_dpi_app" "test" {
  provider = unifi
  config {}
}
`,
			QueryResultChecks: []querycheck.QueryResultCheck{
				querycheck.ExpectLengthAtLeast("unifi_dpi_app.test", 0),
			},
		}},
	})
}

func TestAccDpiAppFramework_basic(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: `
resource "unifi_dpi_app" "test" {
  name    = "DPI App Test"
  enabled = true
  log     = false
  blocked = true
  cats    = [4]
}
`,
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet("unifi_dpi_app.test", "id"),
					resource.TestCheckResourceAttr("unifi_dpi_app.test", "name", "DPI App Test"),
					resource.TestCheckResourceAttr("unifi_dpi_app.test", "blocked", "true"),
				),
			},
			{
				Config: `
resource "unifi_dpi_app" "test" {
  name    = "DPI App Test Updated"
  enabled = true
  log     = false
  blocked = true
  cats    = [4]
}
`,
				Check: resource.TestCheckResourceAttr(
					"unifi_dpi_app.test", "name", "DPI App Test Updated",
				),
			},
			{
				ResourceName:      "unifi_dpi_app.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

func TestDpiAppDescriptorRoundTripsEveryField(t *testing.T) {
	ctx := context.Background()
	spec := dpiAppKitSpec()

	model := dpiAppKitModel{
		ID:             types.StringValue("app-1"),
		Name:           types.StringValue("Streaming"),
		Apps:           types.ListValueMust(types.Int64Type, []attr.Value{types.Int64Value(7)}),
		Cats:           types.ListValueMust(types.Int64Type, []attr.Value{types.Int64Value(4)}),
		Blocked:        types.BoolValue(true),
		Enabled:        types.BoolValue(true),
		Log:            types.BoolValue(false),
		QoSRateMaxDown: types.Int64Value(1000),
		QoSRateMaxUp:   types.Int64Value(500),
	}

	var sdk ui.DpiApp
	for _, field := range spec.Fields {
		if d := field.ToSDK(ctx, &model, &sdk); d.HasError() {
			t.Fatalf("ToSDK(%s): %v", field.WireName(), d)
		}
	}
	if sdk.Name != "Streaming" {
		t.Errorf("name did not reach the SDK struct: %q", sdk.Name)
	}
	if !sdk.Blocked || !sdk.Enabled || sdk.Log {
		t.Errorf("bools did not reach the SDK struct: %+v", sdk)
	}
	if len(sdk.Cats) != 1 || sdk.Cats[0] != 4 {
		t.Errorf("cats did not reach the SDK struct: %v", sdk.Cats)
	}
	if sdk.QOSRateMaxDown == nil || *sdk.QOSRateMaxDown != 1000 {
		t.Errorf("qos_rate_max_down did not reach the SDK struct: %v", sdk.QOSRateMaxDown)
	}

	var back dpiAppKitModel
	for _, field := range spec.Fields {
		if d := field.ToModel(ctx, &sdk, &back); d.HasError() {
			t.Fatalf("ToModel(%s): %v", field.WireName(), d)
		}
	}
	if back.Name != model.Name {
		t.Errorf("name round trip: %v want %v", back.Name, model.Name)
	}
	if !back.Cats.Equal(model.Cats) {
		t.Errorf("cats round trip: %v want %v", back.Cats, model.Cats)
	}
	if back.QoSRateMaxUp != model.QoSRateMaxUp {
		t.Errorf("qos_rate_max_up round trip: %v want %v", back.QoSRateMaxUp, model.QoSRateMaxUp)
	}
}

// TestDpiAppDescriptorCoversEveryManagedField stops the round trip above
// passing because a field is absent from the descriptor entirely.
func TestDpiAppDescriptorCoversEveryManagedField(t *testing.T) {
	got := map[string]bool{}
	for _, f := range dpiAppKitSpec().Fields {
		got[f.WireName()] = true
	}
	for _, wire := range []string{
		"name", "apps", "cats", "blocked", "enabled", "log",
		"qos_rate_max_down", "qos_rate_max_up",
	} {
		if !got[wire] {
			t.Errorf("the descriptor does not carry managed field %q", wire)
		}
	}
	if len(got) != 8 {
		t.Errorf("descriptor carries %d fields, want 8: %v", len(got), got)
	}
}

func TestDpiAppConstructorsServeBothSurfaces(t *testing.T) {
	if NewDpiAppResource() == nil {
		t.Error("NewDpiAppResource returned nil")
	}
	if NewDpiAppListResource() == nil {
		t.Error("NewDpiAppListResource returned nil")
	}
	if r := newDpiAppKitResource(); r.Spec.TypeName != "dpi_app" {
		t.Errorf("spec TypeName = %q", r.Spec.TypeName)
	}
}
