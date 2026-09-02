package unifi

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/querycheck"
	"github.com/hashicorp/terraform-plugin-testing/tfversion"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
)

func TestAccHotspotOpList_emptyOrSeeded(t *testing.T) {
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
list "unifi_hotspot_op" "test" {
  provider = unifi
  config {}
}
`,
			QueryResultChecks: []querycheck.QueryResultCheck{
				querycheck.ExpectLengthAtLeast("unifi_hotspot_op.test", 0),
			},
		}},
	})
}

func TestAccHotspotOpFramework_basic(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: `
resource "unifi_hotspot_op" "test" {
  name     = "Hotspot Operator Test"
  password = "test-password"
  note     = "front desk"
}
`,
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet("unifi_hotspot_op.test", "id"),
					resource.TestCheckResourceAttr("unifi_hotspot_op.test", "name", "Hotspot Operator Test"),
					resource.TestCheckResourceAttr("unifi_hotspot_op.test", "note", "front desk"),
				),
			},
			{
				Config: `
resource "unifi_hotspot_op" "test" {
  name     = "Hotspot Operator Test Updated"
  password = "test-password"
  note     = "front desk"
}
`,
				Check: resource.TestCheckResourceAttr(
					"unifi_hotspot_op.test", "name", "Hotspot Operator Test Updated",
				),
			},
			{
				ResourceName:            "unifi_hotspot_op.test",
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"password"}, // Password is not returned by API
			},
		},
	})
}

func TestHotspotOpDescriptorRoundTripsEveryField(t *testing.T) {
	ctx := context.Background()
	spec := hotspotOpKitSpec()

	model := hotspotOpKitModel{
		ID:       types.StringValue("op-1"),
		Name:     types.StringValue("Front Desk"),
		Password: types.StringValue("s3cret"),
		Note:     types.StringValue("lobby kiosk"),
	}

	var sdk ui.HotspotOp
	for _, field := range spec.Fields {
		if d := field.ToSDK(ctx, &model, &sdk); d.HasError() {
			t.Fatalf("ToSDK(%s): %v", field.WireName(), d)
		}
	}
	if sdk.Name != "Front Desk" {
		t.Errorf("name did not reach the SDK struct: %q", sdk.Name)
	}
	if sdk.Password != "s3cret" {
		t.Errorf("password did not reach the SDK struct's x_password: %q", sdk.Password)
	}
	if sdk.Note != "lobby kiosk" {
		t.Errorf("note did not reach the SDK struct: %q", sdk.Note)
	}

	var back hotspotOpKitModel
	for _, field := range spec.Fields {
		if d := field.ToModel(ctx, &sdk, &back); d.HasError() {
			t.Fatalf("ToModel(%s): %v", field.WireName(), d)
		}
	}
	if back.Name != model.Name {
		t.Errorf("name round trip: %v want %v", back.Name, model.Name)
	}
	if back.Note != model.Note {
		t.Errorf("note round trip: %v want %v", back.Note, model.Note)
	}
}

// TestHotspotOpDescriptorCoversEveryManagedField stops the round trip above
// passing because a field is absent from the descriptor entirely. It also
// pins that the password maps to the controller's x_password, not password.
func TestHotspotOpDescriptorCoversEveryManagedField(t *testing.T) {
	got := map[string]bool{}
	for _, f := range hotspotOpKitSpec().Fields {
		got[f.WireName()] = true
	}
	for _, wire := range []string{"name", "x_password", "note"} {
		if !got[wire] {
			t.Errorf("the descriptor does not carry managed field %q", wire)
		}
	}
	if len(got) != 3 {
		t.Errorf("descriptor carries %d fields, want 3: %v", len(got), got)
	}
}

func TestHotspotOpConstructorsServeBothSurfaces(t *testing.T) {
	if NewHotspotOpResource() == nil {
		t.Error("NewHotspotOpResource returned nil")
	}
	if NewHotspotOpListResource() == nil {
		t.Error("NewHotspotOpListResource returned nil")
	}
	if r := newHotspotOpKitResource(); r.Spec.TypeName != "hotspot_op" {
		t.Errorf("spec TypeName = %q", r.Spec.TypeName)
	}
}
