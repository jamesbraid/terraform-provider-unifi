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

func TestAccClientQosRate_basic(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccClientQosRateConfig_basic(),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"unifi_client_qos_rate.test",
						"name",
						"tfacc-group",
					),
					resource.TestCheckResourceAttr(
						"unifi_client_qos_rate.test",
						"qos_rate_max_down",
						"-1",
					),
					resource.TestCheckResourceAttr(
						"unifi_client_qos_rate.test",
						"qos_rate_max_up",
						"-1",
					),
				),
			},
			{
				ResourceName:      "unifi_client_qos_rate.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

func TestAccClientQosRate_qos(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccClientQosRateConfig_qos(),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"unifi_client_qos_rate.test",
						"name",
						"tfacc-qos-group",
					),
					resource.TestCheckResourceAttr(
						"unifi_client_qos_rate.test",
						"qos_rate_max_down",
						"1000",
					),
					resource.TestCheckResourceAttr(
						"unifi_client_qos_rate.test",
						"qos_rate_max_up",
						"500",
					),
				),
			},
		},
	})
}

func TestAccClientQosRate_update(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccClientQosRateConfig_update_before(),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"unifi_client_qos_rate.test",
						"name",
						"tfacc-update-group",
					),
					resource.TestCheckResourceAttr(
						"unifi_client_qos_rate.test",
						"qos_rate_max_down",
						"100",
					),
				),
			},
			{
				Config: testAccClientQosRateConfig_update_after(),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"unifi_client_qos_rate.test",
						"name",
						"tfacc-update-group-renamed",
					),
					resource.TestCheckResourceAttr(
						"unifi_client_qos_rate.test",
						"qos_rate_max_down",
						"200",
					),
				),
			},
		},
	})
}

func TestAccClientQosRateList_basic(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		TerraformVersionChecks: []tfversion.TerraformVersionCheck{
			tfversion.SkipBelow(tfversion.Version1_14_0),
		},
		Steps: []resource.TestStep{
			{
				Config: testAccClientQosRateConfig_basic(),
			},
			{
				Query: true,
				Config: `
					provider "unifi" {}
					list "unifi_client_qos_rate" "test" {
						provider = unifi
						config {
							filter {
								name  = "name"
								value = "tfacc-group"
						  }
					  }
					}
				`,
				QueryResultChecks: []querycheck.QueryResultCheck{
					querycheck.ExpectLengthAtLeast("unifi_client_qos_rate.test", 1),
				},
			},
		},
	})
}

// planToClientQosRate and clientQosRateToModel are gone; the descriptor's
// Fields do that work now, and the behaviour they asserted is expressed
// here against the descriptor instead of against two deleted functions.
func TestClientQosRateDescriptorRoundTripsEveryField(t *testing.T) {
	ctx := context.Background()
	spec := clientQosRateKitSpec()

	model := clientQosRateKitModel{
		ID:             types.StringValue("group-id"),
		Name:           types.StringValue("test-group"),
		QOSRateMaxDown: types.Int64Value(1000),
		QOSRateMaxUp:   types.Int64Value(500),
	}

	sdk := ui.ClientGroup{}
	for _, field := range spec.Fields {
		if diags := field.ToSDK(ctx, &model, &sdk); diags.HasError() {
			t.Fatalf("ToSDK(%s): %v", field.WireName(), diags)
		}
	}
	if sdk.Name != "test-group" {
		t.Errorf("name did not reach the SDK struct: %q", sdk.Name)
	}
	if sdk.QOSRateMaxDown == nil || *sdk.QOSRateMaxDown != 1000 {
		t.Errorf("qos_rate_max_down did not reach the SDK struct: %v", sdk.QOSRateMaxDown)
	}
	if sdk.QOSRateMaxUp == nil || *sdk.QOSRateMaxUp != 500 {
		t.Errorf("qos_rate_max_up did not reach the SDK struct: %v", sdk.QOSRateMaxUp)
	}

	// And back, into a fresh model, so a field that writes but does not read
	// is caught rather than assumed symmetric.
	var back clientQosRateKitModel
	for _, field := range spec.Fields {
		if diags := field.ToModel(ctx, &sdk, &back); diags.HasError() {
			t.Fatalf("ToModel(%s): %v", field.WireName(), diags)
		}
	}
	if back.Name != model.Name {
		t.Errorf("name round trip: got %v want %v", back.Name, model.Name)
	}
	if back.QOSRateMaxDown != model.QOSRateMaxDown {
		t.Errorf("qos_rate_max_down round trip: got %v want %v", back.QOSRateMaxDown, model.QOSRateMaxDown)
	}
	if back.QOSRateMaxUp != model.QOSRateMaxUp {
		t.Errorf("qos_rate_max_up round trip: got %v want %v", back.QOSRateMaxUp, model.QOSRateMaxUp)
	}
}

// TestClientQosRateDescriptorCoversEveryManagedField stops the round-trip
// above passing because a field is missing from the descriptor entirely: it
// asserts the descriptor's field set against the mapping's managed fields.
func TestClientQosRateDescriptorCoversEveryManagedField(t *testing.T) {
	spec := clientQosRateKitSpec()
	got := map[string]bool{}
	for _, f := range spec.Fields {
		got[f.WireName()] = true
	}
	// name and the two rates; _id is the identity and is served by Spec.ID.
	for _, want := range []string{"name", "qos_rate_max_down", "qos_rate_max_up"} {
		if !got[want] {
			t.Errorf("the descriptor does not carry managed field %q", want)
		}
	}
	if len(got) != 3 {
		t.Errorf("descriptor carries %d fields, want 3: %v", len(got), got)
	}
}

// TestClientQosRateConstructorsServeBothSurfaces replaces the two constructor
// tests, which asserted the old concrete type.
func TestClientQosRateConstructorsServeBothSurfaces(t *testing.T) {
	if NewClientQosRateResource() == nil {
		t.Error("NewClientQosRateResource returned nil")
	}
	if NewClientQosRateListResource() == nil {
		t.Error("NewClientQosRateListResource returned nil")
	}
	r := newClientQosRateKitResource()
	if r.Spec.TypeName != "client_qos_rate" {
		t.Errorf("spec TypeName = %q", r.Spec.TypeName)
	}
	if r.ListSurface.DisplayName == nil {
		t.Error("the list surface has no display name")
	}
}

func testAccClientQosRateConfig_basic() string {
	return `
resource "unifi_client_qos_rate" "test" {
	name = "tfacc-group"
}
`
}

func testAccClientQosRateConfig_qos() string {
	return `
resource "unifi_client_qos_rate" "test" {
	name               = "tfacc-qos-group"
	qos_rate_max_down  = 1000
	qos_rate_max_up    = 500
}
`
}

func testAccClientQosRateConfig_update_before() string {
	return `
resource "unifi_client_qos_rate" "test" {
	name               = "tfacc-update-group"
	qos_rate_max_down  = 100
}
`
}

func testAccClientQosRateConfig_update_after() string {
	return `
resource "unifi_client_qos_rate" "test" {
	name               = "tfacc-update-group-renamed"
	qos_rate_max_down  = 200
}
`
}
