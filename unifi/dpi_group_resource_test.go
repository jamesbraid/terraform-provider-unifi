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

func TestAccDpiGroupList_emptyOrSeeded(t *testing.T) {
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
list "unifi_dpi_group" "test" {
  provider = unifi
  config {}
}
`,
			QueryResultChecks: []querycheck.QueryResultCheck{
				querycheck.ExpectLengthAtLeast("unifi_dpi_group.test", 0),
			},
		}},
	})
}

func TestAccDpiGroupFramework_basic(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: `
resource "unifi_dpi_group" "test" {
  name    = "DPI Group Test"
  enabled = true
}
`,
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet("unifi_dpi_group.test", "id"),
					resource.TestCheckResourceAttr("unifi_dpi_group.test", "name", "DPI Group Test"),
				),
			},
			{
				Config: `
resource "unifi_dpi_group" "test" {
  name    = "DPI Group Test Updated"
  enabled = true
}
`,
				Check: resource.TestCheckResourceAttr(
					"unifi_dpi_group.test", "name", "DPI Group Test Updated",
				),
			},
			{
				ResourceName:      "unifi_dpi_group.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

func TestDpiGroupDescriptorRoundTripsEveryField(t *testing.T) {
	ctx := context.Background()
	spec := dpiGroupKitSpec()

	model := dpiGroupKitModel{
		ID:        types.StringValue("group-1"),
		Name:      types.StringValue("Streaming"),
		DpiappIDs: types.ListValueMust(types.StringType, []attr.Value{types.StringValue("app-1")}),
		Enabled:   types.BoolValue(true),
	}

	var sdk ui.DpiGroup
	for _, field := range spec.Fields {
		if d := field.ToSDK(ctx, &model, &sdk); d.HasError() {
			t.Fatalf("ToSDK(%s): %v", field.WireName(), d)
		}
	}
	if sdk.Name != "Streaming" {
		t.Errorf("name did not reach the SDK struct: %q", sdk.Name)
	}
	if !sdk.Enabled {
		t.Errorf("enabled did not reach the SDK struct: %+v", sdk)
	}
	if len(sdk.DPIappIDs) != 1 || sdk.DPIappIDs[0] != "app-1" {
		t.Errorf("dpiapp_ids did not reach the SDK struct: %v", sdk.DPIappIDs)
	}

	var back dpiGroupKitModel
	for _, field := range spec.Fields {
		if d := field.ToModel(ctx, &sdk, &back); d.HasError() {
			t.Fatalf("ToModel(%s): %v", field.WireName(), d)
		}
	}
	if back.Name != model.Name {
		t.Errorf("name round trip: %v want %v", back.Name, model.Name)
	}
	if !back.DpiappIDs.Equal(model.DpiappIDs) {
		t.Errorf("dpiapp_ids round trip: %v want %v", back.DpiappIDs, model.DpiappIDs)
	}
}

// TestDpiGroupDescriptorCoversEveryManagedField stops the round trip above
// passing because a field is absent from the descriptor entirely.
func TestDpiGroupDescriptorCoversEveryManagedField(t *testing.T) {
	got := map[string]bool{}
	for _, f := range dpiGroupKitSpec().Fields {
		got[f.WireName()] = true
	}
	for _, wire := range []string{"name", "dpiapp_ids", "enabled"} {
		if !got[wire] {
			t.Errorf("the descriptor does not carry managed field %q", wire)
		}
	}
	if len(got) != 3 {
		t.Errorf("descriptor carries %d fields, want 3: %v", len(got), got)
	}
}

func TestDpiGroupConstructorsServeBothSurfaces(t *testing.T) {
	if NewDpiGroupResource() == nil {
		t.Error("NewDpiGroupResource returned nil")
	}
	if NewDpiGroupListResource() == nil {
		t.Error("NewDpiGroupListResource returned nil")
	}
	if r := newDpiGroupKitResource(); r.Spec.TypeName != "dpi_group" {
		t.Errorf("spec TypeName = %q", r.Spec.TypeName)
	}
}
