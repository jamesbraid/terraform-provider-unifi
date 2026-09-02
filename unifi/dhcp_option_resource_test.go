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

func TestAccDHCPOptionList_emptyOrSeeded(t *testing.T) {
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
list "unifi_dhcp_option" "test" {
  provider = unifi
  config {}
}
`,
			QueryResultChecks: []querycheck.QueryResultCheck{
				querycheck.ExpectLengthAtLeast("unifi_dhcp_option.test", 0),
			},
		}},
	})
}

func TestAccDHCPOptionFramework_basic(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: `
resource "unifi_dhcp_option" "test" {
  name = "tf-acc-dhcp-option"
  code = "100"
  type = "text"
}
`,
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet("unifi_dhcp_option.test", "id"),
					resource.TestCheckResourceAttr("unifi_dhcp_option.test", "name", "tf-acc-dhcp-option"),
					resource.TestCheckResourceAttr("unifi_dhcp_option.test", "code", "100"),
					resource.TestCheckResourceAttr("unifi_dhcp_option.test", "type", "text"),
					resource.TestCheckResourceAttr("unifi_dhcp_option.test", "signed", "false"),
				),
			},
			{
				// The update flips the value type and adds the fields that only
				// mean something for an integer option.
				Config: `
resource "unifi_dhcp_option" "test" {
  name   = "tf-acc-dhcp-option"
  code   = "100"
  type   = "integer"
  signed = true
  width  = 32
}
`,
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("unifi_dhcp_option.test", "type", "integer"),
					resource.TestCheckResourceAttr("unifi_dhcp_option.test", "signed", "true"),
					resource.TestCheckResourceAttr("unifi_dhcp_option.test", "width", "32"),
				),
			},
			{
				ResourceName:      "unifi_dhcp_option.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

func TestDHCPOptionDescriptorRoundTripsEveryField(t *testing.T) {
	ctx := context.Background()
	spec := dhcpOptionKitSpec()

	model := dhcpOptionKitModel{
		ID:     types.StringValue("opt-1"),
		Code:   types.StringValue("100"),
		Name:   types.StringValue("lease-seconds"),
		Signed: types.BoolValue(true),
		Type:   types.StringValue("integer"),
		Width:  types.Int64Value(32),
	}

	var sdk ui.DHCPOption
	for _, field := range spec.Fields {
		if d := field.ToSDK(ctx, &model, &sdk); d.HasError() {
			t.Fatalf("ToSDK(%s): %v", field.WireName(), d)
		}
	}
	if sdk.Code != "100" || sdk.Name != "lease-seconds" || sdk.Type != "integer" {
		t.Errorf("scalars did not reach the SDK struct: %+v", sdk)
	}
	if !sdk.Signed {
		t.Error("signed did not reach the SDK struct")
	}
	if sdk.Width == nil || *sdk.Width != 32 {
		t.Errorf("width reached the SDK struct as %v", sdk.Width)
	}

	var back dhcpOptionKitModel
	for _, field := range spec.Fields {
		if d := field.ToModel(ctx, &sdk, &back); d.HasError() {
			t.Fatalf("ToModel(%s): %v", field.WireName(), d)
		}
	}
	for name, pair := range map[string][2]any{
		"code":   {back.Code, model.Code},
		"name":   {back.Name, model.Name},
		"signed": {back.Signed, model.Signed},
		"type":   {back.Type, model.Type},
		"width":  {back.Width, model.Width},
	} {
		if pair[0] != pair[1] {
			t.Errorf("%s round trip: %v want %v", name, pair[0], pair[1])
		}
	}
}

// TestDHCPOptionWidthAbsenceStaysNull pins the pointer distinction the
// bootstrap records: width is the struct's one pointer field, and a
// controller that says nothing must read back as null, not as a zero no
// validator would accept.
func TestDHCPOptionWidthAbsenceStaysNull(t *testing.T) {
	ctx := context.Background()
	spec := dhcpOptionKitSpec()

	sdk := ui.DHCPOption{Code: "100", Name: "opt", Type: "text"}
	var model dhcpOptionKitModel
	for _, field := range spec.Fields {
		if d := field.ToModel(ctx, &sdk, &model); d.HasError() {
			t.Fatalf("ToModel(%s): %v", field.WireName(), d)
		}
	}
	if !model.Width.IsNull() {
		t.Errorf("an absent width read back as %v", model.Width)
	}
}

// TestDHCPOptionDescriptorCoversEveryManagedField stops the round trip above
// passing because a field is absent from the descriptor entirely.
func TestDHCPOptionDescriptorCoversEveryManagedField(t *testing.T) {
	got := map[string]bool{}
	for _, f := range dhcpOptionKitSpec().Fields {
		got[f.WireName()] = true
	}
	for _, want := range []string{"code", "name", "signed", "type", "width"} {
		if !got[want] {
			t.Errorf("the descriptor does not carry managed field %q", want)
		}
	}
	if len(got) != 5 {
		t.Errorf("descriptor carries %d fields, want 5: %v", len(got), got)
	}
}

func TestDHCPOptionConstructorsServeBothSurfaces(t *testing.T) {
	if NewDHCPOptionResource() == nil {
		t.Error("NewDHCPOptionResource returned nil")
	}
	if NewDHCPOptionListResource() == nil {
		t.Error("NewDHCPOptionListResource returned nil")
	}
	if r := newDHCPOptionKitResource(); r.Spec.TypeName != "dhcp_option" {
		t.Errorf("spec TypeName = %q", r.Spec.TypeName)
	}
}
