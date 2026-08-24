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

func TestAccFirewallZoneList_emptyOrSeeded(t *testing.T) {
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
list "unifi_firewall_zone" "test" {
  provider = unifi
  config {}
}
`,
			QueryResultChecks: []querycheck.QueryResultCheck{
				querycheck.ExpectLengthAtLeast("unifi_firewall_zone.test", 0),
			},
		}},
	})
}

func TestAccFirewallZoneFramework_basic(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: `
resource "unifi_network" "firewall_zone_test" {
  name   = "Firewall Zone Test Network"
  subnet = "192.168.250.1/24"
  vlan   = 250

  # A zone claiming this network flips it to manual controller-side, while the
  # schema default asks for auto. Declare the end state so the post-apply
  # refresh plan stays empty.
  setting_preference = "manual"
}

resource "unifi_firewall_zone" "test" {
  name        = "Firewall Zone Test"
  network_ids = [unifi_network.firewall_zone_test.id]
}
`,
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet("unifi_firewall_zone.test", "id"),
					resource.TestCheckResourceAttr("unifi_firewall_zone.test", "name", "Firewall Zone Test"),
				),
			},
			{
				Config: `
resource "unifi_network" "firewall_zone_test" {
  name   = "Firewall Zone Test Network"
  subnet = "192.168.250.1/24"
  vlan   = 250

  # A zone claiming this network flips it to manual controller-side, while the
  # schema default asks for auto. Declare the end state so the post-apply
  # refresh plan stays empty.
  setting_preference = "manual"
}

resource "unifi_firewall_zone" "test" {
  name        = "Firewall Zone Test Updated"
  network_ids = [unifi_network.firewall_zone_test.id]
}
`,
				Check: resource.TestCheckResourceAttr(
					"unifi_firewall_zone.test", "name", "Firewall Zone Test Updated",
				),
			},
			{
				ResourceName:      "unifi_firewall_zone.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
			{
				// "name=<zone name>" resolves the controller-assigned id via
				// firewallZoneKitBackend's ReadByName, the same route the
				// built-in zones (Hotspot, Internal, ...) need since their id
				// is never chosen by the practitioner.
				ResourceName:      "unifi_firewall_zone.test",
				ImportState:       true,
				ImportStateVerify: true,
				ImportStateId:     "name=Firewall Zone Test Updated",
			},
		},
	})
}

// The mapping and body coverage that survived the cutover:
// modelToFirewallZone and firewallZoneToModel are gone; the descriptor's
// Fields do that work. Their assertions are re-expressed against the descriptor
// rather than deleted. The generic Schema, IdentitySchema and
// ListResourceConfigSchema cases are not re-expressed: the kit serves all three
// now, and resourcekit's own tests cover them once for every surface instead of
// once per surface.
func TestFirewallZoneDescriptorRoundTripsEveryField(t *testing.T) {
	ctx := context.Background()
	spec := firewallZoneKitSpec()

	ids, diags := types.ListValueFrom(ctx, types.StringType, []string{"net-a", "net-b"})
	if diags.HasError() {
		t.Fatal(diags)
	}
	model := firewallZoneKitModel{
		ID:         types.StringValue("zone-1"),
		Name:       types.StringValue("Trusted"),
		NetworkIDs: ids,
	}

	var sdk ui.FirewallZone
	for _, field := range spec.Fields {
		if d := field.ToSDK(ctx, &model, &sdk); d.HasError() {
			t.Fatalf("ToSDK(%s): %v", field.WireName(), d)
		}
	}
	if sdk.Name != "Trusted" {
		t.Errorf("name did not reach the SDK struct: %q", sdk.Name)
	}
	if len(sdk.NetworkIDs) != 2 {
		t.Errorf("network_ids reached the SDK struct as %v", sdk.NetworkIDs)
	}

	var back firewallZoneKitModel
	for _, field := range spec.Fields {
		if d := field.ToModel(ctx, &sdk, &back); d.HasError() {
			t.Fatalf("ToModel(%s): %v", field.WireName(), d)
		}
	}
	if back.Name != model.Name {
		t.Errorf("name round trip: %v want %v", back.Name, model.Name)
	}
	if !back.NetworkIDs.Equal(model.NetworkIDs) {
		t.Errorf("network_ids round trip: %v want %v", back.NetworkIDs, model.NetworkIDs)
	}
}

// TestFirewallZoneAlwaysSendsNetworkIDs keeps the assertion that
// TestFirewallZoneCreateBodyAlwaysSendsNetworkIDs made.
//
// network_ids is the SDK's only field without omitempty, so a nil slice
// serialises as null and an empty one as [] -- and the controller reads those
// as different requests. StringListField empties the slice rather than leaving
// it nil for exactly this reason; the assertion moves here so the property is
// still defended after the hand-written body builder was deleted.
func TestFirewallZoneAlwaysSendsNetworkIDs(t *testing.T) {
	ctx := context.Background()
	spec := firewallZoneKitSpec()

	model := firewallZoneKitModel{
		Name:       types.StringValue("Trusted"),
		NetworkIDs: types.ListNull(types.StringType), // the practitioner said nothing
	}
	var sdk ui.FirewallZone
	for _, field := range spec.Fields {
		if d := field.ToSDK(ctx, &model, &sdk); d.HasError() {
			t.Fatalf("ToSDK(%s): %v", field.WireName(), d)
		}
	}
	if sdk.NetworkIDs == nil {
		t.Error("network_ids is nil, so it serialises as null rather than []; " +
			"the controller reads those as different requests")
	}
	if len(sdk.NetworkIDs) != 0 {
		t.Errorf("a null list produced %v", sdk.NetworkIDs)
	}
}

// TestFirewallZoneDescriptorCoversEveryManagedField stops the round trip above
// passing because a field is absent from the descriptor entirely.
func TestFirewallZoneDescriptorCoversEveryManagedField(t *testing.T) {
	got := map[string]bool{}
	for _, f := range firewallZoneKitSpec().Fields {
		got[f.WireName()] = true
	}
	for _, want := range []string{"name", "network_ids", "zone_key", "default_zone"} {
		if !got[want] {
			t.Errorf("the descriptor does not carry managed field %q", want)
		}
	}
	if len(got) != 4 {
		t.Errorf("descriptor carries %d fields, want 4: %v", len(got), got)
	}
}

// TestFirewallZoneConstructorsServeBothSurfaces replaces the two constructor
// tests, which asserted the old concrete type.
func TestFirewallZoneConstructorsServeBothSurfaces(t *testing.T) {
	if NewFirewallZoneResource() == nil {
		t.Error("NewFirewallZoneResource returned nil")
	}
	if NewFirewallZoneListResource() == nil {
		t.Error("NewFirewallZoneListResource returned nil")
	}
	if r := newFirewallZoneKitResource(); r.Spec.TypeName != "firewall_zone" {
		t.Errorf("spec TypeName = %q", r.Spec.TypeName)
	}
}
