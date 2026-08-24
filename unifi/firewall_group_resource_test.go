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

func TestAccFirewallGroupFramework_basic(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccFirewallGroupFrameworkConfig_basic(),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet("unifi_firewall_group.test", "id"),
					resource.TestCheckResourceAttr(
						"unifi_firewall_group.test",
						"name",
						"Test Address Group",
					),
					resource.TestCheckResourceAttr(
						"unifi_firewall_group.test",
						"type",
						"address-group",
					),
					resource.TestCheckResourceAttr("unifi_firewall_group.test", "members.#", "2"),
				),
			},
			{
				ResourceName:      "unifi_firewall_group.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

func TestAccFirewallGroupFramework_portGroup(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccFirewallGroupFrameworkConfig_portGroup(),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet("unifi_firewall_group.test", "id"),
					resource.TestCheckResourceAttr(
						"unifi_firewall_group.test",
						"name",
						"Test Port Group",
					),
					resource.TestCheckResourceAttr(
						"unifi_firewall_group.test",
						"type",
						"port-group",
					),
					resource.TestCheckResourceAttr("unifi_firewall_group.test", "members.#", "3"),
				),
			},
		},
	})
}

func testAccFirewallGroupFrameworkConfig_basic() string {
	return `
resource "unifi_firewall_group" "test" {
	name = "Test Address Group"
	type = "address-group"
	members = [
		"192.168.1.10",
		"192.168.1.20"
	]
}
`
}

func testAccFirewallGroupFrameworkConfig_portGroup() string {
	return `
resource "unifi_firewall_group" "test" {
	name = "Test Port Group"
	type = "port-group"
	members = [
		"80",
		"443",
		"8080"
	]
}
`
}

func TestAccFirewallGroupList_basic(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		TerraformVersionChecks: []tfversion.TerraformVersionCheck{
			tfversion.SkipBelow(tfversion.Version1_14_0),
		},
		Steps: []resource.TestStep{
			{
				Config: testAccFirewallGroupFrameworkConfig_basic(),
			},
			{
				Query: true,
				Config: `
					provider "unifi" {}
					list "unifi_firewall_group" "test" {
						provider = unifi
						config {
							filter {
								name  = "name"
								value = "Test Address Group"
						  }
					  }
					}
				`,
				QueryResultChecks: []querycheck.QueryResultCheck{
					querycheck.ExpectLengthAtLeast("unifi_firewall_group.test", 1),
				},
			},
		},
	})
}

// modelToAPIFirewallGroup, setResourceData and firewallGroupToModel are
// gone; the descriptor's Fields do that work, and their assertions are
// re-expressed here against the descriptor rather than deleted.
//
// One case changed on purpose: the old firewallGroupToModel nulled
// `members` when the API returned none, but members is Required in the
// generated schema, so a null state against a config holding a value is an
// inconsistent-result-after-apply -- and `members = []` is a legal
// configuration, so the trigger is reachable. The descriptor keeps the
// empty set instead; TestFirewallGroupEmptyMembersStaysEmpty asserts the
// new behaviour.
func TestFirewallGroupDescriptorRoundTripsEveryField(t *testing.T) {
	ctx := context.Background()
	spec := firewallGroupKitSpec()

	members, diags := types.SetValueFrom(ctx, types.StringType, []string{"10.0.0.1", "10.0.0.2"})
	if diags.HasError() {
		t.Fatal(diags)
	}
	model := firewallGroupKitModel{
		ID:      types.StringValue("fg1"),
		Name:    types.StringValue("Test"),
		Type:    types.StringValue("address-group"),
		Members: members,
	}

	var sdk ui.FirewallGroup
	for _, field := range spec.Fields {
		if d := field.ToSDK(ctx, &model, &sdk); d.HasError() {
			t.Fatalf("ToSDK(%s): %v", field.WireName(), d)
		}
	}
	if sdk.Name != "Test" {
		t.Errorf("name did not reach the SDK struct: %q", sdk.Name)
	}
	if sdk.GroupType != "address-group" {
		t.Errorf("group_type did not reach the SDK struct: %q", sdk.GroupType)
	}
	if len(sdk.GroupMembers) != 2 {
		t.Errorf("group_members reached the SDK struct as %v", sdk.GroupMembers)
	}

	var back firewallGroupKitModel
	for _, field := range spec.Fields {
		if d := field.ToModel(ctx, &sdk, &back); d.HasError() {
			t.Fatalf("ToModel(%s): %v", field.WireName(), d)
		}
	}
	if back.Name != model.Name || back.Type != model.Type {
		t.Errorf("scalar round trip: %v/%v want %v/%v", back.Name, back.Type, model.Name, model.Type)
	}
	if !back.Members.Equal(model.Members) {
		t.Errorf("members round trip: %v want %v", back.Members, model.Members)
	}
}

// TestFirewallGroupEmptyMembersStaysEmpty replaces "empty members produces null
// set". members is Required, so its zero must survive rather than becoming
// null: the practitioner may write `members = []`, and nulling that makes state
// disagree with config.
func TestFirewallGroupEmptyMembersStaysEmpty(t *testing.T) {
	ctx := context.Background()
	spec := firewallGroupKitSpec()
	api := ui.FirewallGroup{ID: "fg2", Name: "Empty", GroupType: "address-group", GroupMembers: []string{}}

	var model firewallGroupKitModel
	for _, field := range spec.Fields {
		if d := field.ToModel(ctx, &api, &model); d.HasError() {
			t.Fatalf("ToModel(%s): %v", field.WireName(), d)
		}
	}
	if model.Members.IsNull() {
		t.Error("members went null on an empty API list; it is Required, so the empty must survive")
	}
	if n := len(model.Members.Elements()); n != 0 {
		t.Errorf("members holds %d element(s), want an empty set", n)
	}
}

// TestFirewallGroupDescriptorCoversEveryManagedField stops the round trip above
// passing because a field is absent from the descriptor entirely.
func TestFirewallGroupDescriptorCoversEveryManagedField(t *testing.T) {
	got := map[string]bool{}
	for _, f := range firewallGroupKitSpec().Fields {
		got[f.WireName()] = true
	}
	for _, want := range []string{"name", "group_type", "group_members"} {
		if !got[want] {
			t.Errorf("the descriptor does not carry managed field %q", want)
		}
	}
	if len(got) != 3 {
		t.Errorf("descriptor carries %d fields, want 3: %v", len(got), got)
	}
}

// TestFirewallGroupConstructorsServeBothSurfaces replaces the two constructor
// tests, which asserted the old concrete type.
func TestFirewallGroupConstructorsServeBothSurfaces(t *testing.T) {
	if NewFirewallGroupFrameworkResource() == nil {
		t.Error("NewFirewallGroupFrameworkResource returned nil")
	}
	if NewFirewallGroupListResource() == nil {
		t.Error("NewFirewallGroupListResource returned nil")
	}
	r := newFirewallGroupKitResource()
	if r.Spec.TypeName != "firewall_group" {
		t.Errorf("spec TypeName = %q", r.Spec.TypeName)
	}
}
