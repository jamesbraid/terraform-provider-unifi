package unifi

import (
	"context"
	"testing"

	fwlist "github.com/hashicorp/terraform-plugin-framework/list"
	fwresource "github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/querycheck"
	"github.com/hashicorp/terraform-plugin-testing/tfversion"
	"github.com/ubiquiti-community/go-unifi/unifi"
)

func TestAccSiteFramework_basic(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccSiteFrameworkConfig_basic(),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("unifi_site.test", "description", "tfacc-test"),
					resource.TestCheckResourceAttrSet("unifi_site.test", "name"),
				),
				ResourceName:  "unifi_site.test",
				ImportState:   true,
				ImportStateId: "default",
			},
		},
	})
}

func testAccSiteFrameworkConfig_basic() string {
	return `
resource "unifi_site" "test" {
	name        = "default"
	description = "tfacc-test"
}
`
}

func TestNewSiteFrameworkResource(t *testing.T) {
	r := NewSiteFrameworkResource()
	if r == nil {
		t.Fatal("returned nil")
	}
	if _, ok := r.(fwresource.ResourceWithConfigure); !ok {
		t.Error("expected ResourceWithConfigure")
	}
	if _, ok := r.(fwresource.ResourceWithImportState); !ok {
		t.Error("expected ResourceWithImportState")
	}
	if _, ok := r.(fwresource.ResourceWithIdentity); !ok {
		t.Error("expected ResourceWithIdentity")
	}
}

func TestNewSiteListResource(t *testing.T) {
	r := NewSiteListResource()
	if r == nil {
		t.Fatal("returned nil")
	}
	if _, ok := r.(fwlist.ListResourceWithConfigure); !ok {
		t.Error("expected ListResourceWithConfigure")
	}
}

func Test_siteFrameworkResource_IdentitySchema(t *testing.T) {
	r := newSiteKitResource()
	resp := &fwresource.IdentitySchemaResponse{}
	r.IdentitySchema(context.Background(), fwresource.IdentitySchemaRequest{}, resp)
	if _, ok := resp.IdentitySchema.Attributes["id"]; !ok {
		t.Error("expected identity schema to have 'id' attribute")
	}
}

// TestSiteDescriptorAppliesThePlansDescription pins what the hand
// applyPlanToState did: an update's plan value for description lands on the
// state, and the name (which cannot change after creation) is not the plan's
// to assert.
func TestSiteDescriptorAppliesThePlansDescription(t *testing.T) {
	spec := siteKitSpec()
	plan := siteKitModel{
		Description: types.StringValue("new-desc"),
	}
	state := siteKitModel{
		Name:        types.StringValue("default"),
		Description: types.StringValue("old-desc"),
	}
	spec.ApplyPlanToState(&plan, &state)
	if state.Description.ValueString() != "new-desc" {
		t.Error("expected Description to be copied from plan")
	}
	if state.Name.ValueString() != "default" {
		t.Error("expected Name to keep its state value")
	}
}

// TestSiteDescriptorRoundTripsTheSite pins the mapping in both directions:
// only the description reaches the write object through the field list (the
// controller derives everything else), the state's name is carried onto the
// object by BeforeSend because the update-site command is addressed by it,
// and a read maps id, name and description back.
func TestSiteDescriptorRoundTripsTheSite(t *testing.T) {
	ctx := context.Background()
	spec := siteKitSpec()

	model := siteKitModel{
		Name:        types.StringValue("default"),
		Description: types.StringValue("Default site"),
	}
	sdk, diags := spec.ToSDK(ctx, &model)
	if diags.HasError() {
		t.Fatalf("ToSDK: %v", diags)
	}
	if sdk.Description != "Default site" {
		t.Errorf("Description = %q, want the plan's", sdk.Description)
	}
	if sdk.Name != "" {
		t.Errorf("ToSDK wrote name %q; the name is read-only and only BeforeSend may set it", sdk.Name)
	}
	var prior siteKitModel
	if d := spec.BeforeSend(ctx, &model, &model, prior, sdk, nil); d.HasError() {
		t.Fatalf("BeforeSend: %v", d)
	}
	if sdk.Name != "default" {
		t.Errorf("Name = %q after BeforeSend, want the state's name", sdk.Name)
	}

	site := &unifi.Site{
		ID:          "abc123",
		Name:        "default",
		Description: "Default site",
	}
	var back siteKitModel
	if d := spec.ToModel(ctx, site, &back, ""); d.HasError() {
		t.Fatalf("ToModel: %v", d)
	}
	if back.ID.ValueString() != "abc123" {
		t.Errorf("ID = %q, want abc123", back.ID.ValueString())
	}
	if back.Name.ValueString() != "default" {
		t.Errorf("Name = %q, want default", back.Name.ValueString())
	}
	if back.Description.ValueString() != "Default site" {
		t.Errorf("Description = %q, want Default site", back.Description.ValueString())
	}
}

func Test_siteFrameworkResource_ListResourceConfigSchema(t *testing.T) {
	r := newSiteKitResource()
	resp := &fwlist.ListResourceSchemaResponse{}
	r.ListResourceConfigSchema(context.Background(), fwlist.ListResourceSchemaRequest{}, resp)
	if len(resp.Schema.Attributes) == 0 && len(resp.Schema.Blocks) == 0 {
		t.Error("expected non-empty list resource schema")
	}
}

func TestAccSiteList_basic(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		TerraformVersionChecks: []tfversion.TerraformVersionCheck{
			tfversion.SkipBelow(tfversion.Version1_14_0),
		},
		Steps: []resource.TestStep{
			{
				Query: true,
				Config: `
					provider "unifi" {}
					list "unifi_site" "test" {
						provider = unifi
						config {}
					}
				`,
				QueryResultChecks: []querycheck.QueryResultCheck{
					querycheck.ExpectLengthAtLeast("unifi_site.test", 1),
				},
			},
		},
	})
}
