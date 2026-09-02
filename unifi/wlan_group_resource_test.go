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

func TestAccWLANGroupList_emptyOrSeeded(t *testing.T) {
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
list "unifi_wlan_group" "test" {
  provider = unifi
  config {}
}
`,
			QueryResultChecks: []querycheck.QueryResultCheck{
				querycheck.ExpectLengthAtLeast("unifi_wlan_group.test", 0),
			},
		}},
	})
}

func TestAccWLANGroupFramework_basic(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: `
resource "unifi_wlan_group" "test" {
  name = "WLAN Group Test"
}
`,
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet("unifi_wlan_group.test", "id"),
					resource.TestCheckResourceAttr("unifi_wlan_group.test", "name", "WLAN Group Test"),
				),
			},
			{
				Config: `
resource "unifi_wlan_group" "test" {
  name = "WLAN Group Test Updated"
}
`,
				Check: resource.TestCheckResourceAttr(
					"unifi_wlan_group.test", "name", "WLAN Group Test Updated",
				),
			},
			{
				ResourceName:      "unifi_wlan_group.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

func TestWLANGroupDescriptorRoundTripsEveryField(t *testing.T) {
	ctx := context.Background()
	spec := wlanGroupKitSpec()

	model := wlanGroupKitModel{
		ID:   types.StringValue("group-1"),
		Name: types.StringValue("Guests"),
	}

	var sdk ui.WLANGroup
	for _, field := range spec.Fields {
		if d := field.ToSDK(ctx, &model, &sdk); d.HasError() {
			t.Fatalf("ToSDK(%s): %v", field.WireName(), d)
		}
	}
	if sdk.Name != "Guests" {
		t.Errorf("name did not reach the SDK struct: %q", sdk.Name)
	}

	var back wlanGroupKitModel
	for _, field := range spec.Fields {
		if d := field.ToModel(ctx, &sdk, &back); d.HasError() {
			t.Fatalf("ToModel(%s): %v", field.WireName(), d)
		}
	}
	if back.Name != model.Name {
		t.Errorf("name round trip: %v want %v", back.Name, model.Name)
	}
}

// TestWLANGroupDescriptorCoversEveryManagedField stops the round trip above
// passing because a field is absent from the descriptor entirely.
func TestWLANGroupDescriptorCoversEveryManagedField(t *testing.T) {
	got := map[string]bool{}
	for _, f := range wlanGroupKitSpec().Fields {
		got[f.WireName()] = true
	}
	if !got["name"] {
		t.Error(`the descriptor does not carry managed field "name"`)
	}
	if len(got) != 1 {
		t.Errorf("descriptor carries %d fields, want 1: %v", len(got), got)
	}
}

func TestWLANGroupConstructorsServeBothSurfaces(t *testing.T) {
	if NewWLANGroupResource() == nil {
		t.Error("NewWLANGroupResource returned nil")
	}
	if NewWLANGroupListResource() == nil {
		t.Error("NewWLANGroupListResource returned nil")
	}
	if r := newWLANGroupKitResource(); r.Spec.TypeName != "wlan_group" {
		t.Errorf("spec TypeName = %q", r.Spec.TypeName)
	}
}
