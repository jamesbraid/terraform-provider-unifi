package unifi

import (
	"context"
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	fwresource "github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
)

func TestAccOSPFRouter_basic(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				// The two fields the controller refuses a create without:
				// router_id and a non-empty areas list (probed live, both
				// rejected as NotEmpty violations).
				Config: testAccOSPFRouterConfig("10.255.0.1", false),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"unifi_ospf_router.test", "router_id", "10.255.0.1"),
					resource.TestCheckResourceAttr(
						"unifi_ospf_router.test", "areas.#", "1"),
					resource.TestCheckResourceAttr(
						"unifi_ospf_router.test", "areas.0.area_id", "0.0.0.0"),
					resource.TestCheckResourceAttr(
						"unifi_ospf_router.test", "areas.0.name", "backbone"),
					resource.TestCheckResourceAttr(
						"unifi_ospf_router.test", "enabled", "true"),
					resource.TestCheckResourceAttr(
						"unifi_ospf_router.test", "announce_default_route", "false"),
					resource.TestCheckResourceAttrSet(
						"unifi_ospf_router.test", "id"),
				),
			},
			{
				Config: testAccOSPFRouterConfig("10.255.0.1", true),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"unifi_ospf_router.test", "announce_default_route", "true"),
					resource.TestCheckResourceAttr(
						"unifi_ospf_router.test", "router_id", "10.255.0.1"),
				),
			},
			{
				ResourceName:    "unifi_ospf_router.test",
				ImportState:     true,
				ImportStateKind: resource.ImportBlockWithResourceIdentity,
			},
		},
	})
}

func testAccOSPFRouterConfig(routerID string, announceDefaultRoute bool) string {
	return fmt.Sprintf(`
resource "unifi_network" "ospf" {
	name    = "OSPF Test"
	purpose = "corporate"
	subnet  = "192.168.77.1/24"
	vlan    = 77
}

resource "unifi_ospf_router" "test" {
	router_id              = %q
	announce_default_route = %t

	areas = [{
		area_id     = "0.0.0.0"
		name        = "backbone"
		area_type   = "normal"
		network_ids = [unifi_network.ospf.id]
	}]
}
`, routerID, announceDefaultRoute)
}

func TestNewOSPFRouterResource(t *testing.T) {
	r := NewOSPFRouterResource()
	if r == nil {
		t.Fatal("NewOSPFRouterResource() returned nil")
	}
	if _, ok := r.(fwresource.ResourceWithConfigure); !ok {
		t.Error("expected ResourceWithConfigure interface")
	}
	if _, ok := r.(fwresource.ResourceWithImportState); !ok {
		t.Error("expected ResourceWithImportState interface")
	}
	kit := newOSPFRouterKitResource()
	if kit.Spec.TypeName != "ospf_router" {
		t.Errorf("spec TypeName = %q", kit.Spec.TypeName)
	}
}

// TestOSPFRouterDescriptorCoversEveryManagedField stops the round trip below
// passing because a field is missing from the descriptor entirely: it asserts
// the descriptor's field set against the mapping's managed fields.
func TestOSPFRouterDescriptorCoversEveryManagedField(t *testing.T) {
	spec := ospfRouterKitSpec()
	got := map[string]bool{}
	for _, f := range spec.Fields {
		got[f.WireName()] = true
	}
	// _id is the identity and is served by Spec.ID.
	want := []string{
		"announce_default_route",
		"areas",
		"enabled",
		"interfaces",
		"redistribute_bgp_routes",
		"redistribute_connected_routes",
		"redistribute_connected_routes_metric_type",
		"redistribute_static_routes",
		"redistribute_static_routes_metric_type",
		"router_id",
	}
	for _, wire := range want {
		if !got[wire] {
			t.Errorf("the descriptor does not carry managed field %q", wire)
		}
	}
	if len(got) != len(want) {
		t.Errorf("descriptor carries %d fields, want %d: %v", len(got), len(want), got)
	}
}

func TestOSPFRouterDescriptorRoundTripsEveryField(t *testing.T) {
	ctx := context.Background()
	spec := ospfRouterKitSpec()

	area, diags := types.ObjectValue(ospfAreaModel{}.AttributeTypes(), map[string]attr.Value{
		"area_id":   types.StringValue("0.0.0.0"),
		"area_type": types.StringValue("normal"),
		"name":      types.StringValue("backbone"),
		"network_ids": types.ListValueMust(types.StringType, []attr.Value{
			types.StringValue("networkid1"),
		}),
	})
	if diags.HasError() {
		t.Fatalf("building the area object: %v", diags)
	}
	iface, diags := types.ObjectValue(ospfInterfaceModel{}.AttributeTypes(), map[string]attr.Value{
		"authentication_type": types.StringNull(),
		"cost":                types.StringValue("10"),
		"dead_interval":       types.StringValue("40"),
		"hello_interval":      types.StringValue("10"),
		"network_id":          types.StringValue("networkid1"),
		"passive_interface":   types.BoolValue(true),
		"priority":            types.StringNull(),
	})
	if diags.HasError() {
		t.Fatalf("building the interface object: %v", diags)
	}

	model := ospfRouterKitModel{
		ID:                   types.StringValue("router-id"),
		AnnounceDefaultRoute: types.BoolValue(true),
		Areas: types.ListValueMust(
			types.ObjectType{AttrTypes: ospfAreaModel{}.AttributeTypes()},
			[]attr.Value{area},
		),
		Enabled: types.BoolValue(true),
		Interfaces: types.ListValueMust(
			types.ObjectType{AttrTypes: ospfInterfaceModel{}.AttributeTypes()},
			[]attr.Value{iface},
		),
		RedistributeBgpRoutes:                 types.BoolValue(true),
		RedistributeConnectedRoutes:           types.BoolValue(true),
		RedistributeConnectedRoutesMetricType: types.StringValue("type-2"),
		RedistributeStaticRoutes:              types.BoolValue(false),
		RedistributeStaticRoutesMetricType:    types.StringNull(),
		RouterID:                              types.StringValue("10.255.0.1"),
	}

	sdk := ui.OSPFRouter{}
	for _, field := range spec.Fields {
		if diags := field.ToSDK(ctx, &model, &sdk); diags.HasError() {
			t.Fatalf("ToSDK(%s): %v", field.WireName(), diags)
		}
	}
	if !sdk.AnnounceDefaultRoute {
		t.Error("announce_default_route did not reach the SDK struct")
	}
	if sdk.RouterID != "10.255.0.1" {
		t.Errorf("router_id did not reach the SDK struct: %q", sdk.RouterID)
	}
	if len(sdk.Areas) != 1 {
		t.Fatalf("areas did not reach the SDK struct: %v", sdk.Areas)
	}
	wantArea := ui.OSPFRouterAreas{
		AreaID:     "0.0.0.0",
		AreaType:   "normal",
		Name:       "backbone",
		NetworkIDs: []string{"networkid1"},
	}
	if sdk.Areas[0].AreaID != wantArea.AreaID || sdk.Areas[0].AreaType != wantArea.AreaType ||
		sdk.Areas[0].Name != wantArea.Name || len(sdk.Areas[0].NetworkIDs) != 1 ||
		sdk.Areas[0].NetworkIDs[0] != "networkid1" {
		t.Errorf("area encoded as %+v, want %+v", sdk.Areas[0], wantArea)
	}
	if len(sdk.Interfaces) != 1 {
		t.Fatalf("interfaces did not reach the SDK struct: %v", sdk.Interfaces)
	}
	if sdk.Interfaces[0].NetworkID != "networkid1" || !sdk.Interfaces[0].PassiveInterface ||
		sdk.Interfaces[0].Cost != "10" || sdk.Interfaces[0].AuthenticationType != "" {
		t.Errorf("interface encoded as %+v", sdk.Interfaces[0])
	}
	if sdk.RedistributeConnectedRoutesMetricType != "type-2" {
		t.Errorf("redistribute_connected_routes_metric_type did not reach the SDK struct: %q",
			sdk.RedistributeConnectedRoutesMetricType)
	}

	// And back, into a fresh model, so a field that writes but does not read
	// is caught rather than assumed symmetric.
	var back ospfRouterKitModel
	for _, field := range spec.Fields {
		if diags := field.ToModel(ctx, &sdk, &back); diags.HasError() {
			t.Fatalf("ToModel(%s): %v", field.WireName(), diags)
		}
	}
	if back.RouterID != model.RouterID {
		t.Errorf("router_id round trip: got %v want %v", back.RouterID, model.RouterID)
	}
	if back.AnnounceDefaultRoute != model.AnnounceDefaultRoute {
		t.Errorf("announce_default_route round trip: got %v want %v",
			back.AnnounceDefaultRoute, model.AnnounceDefaultRoute)
	}
	if !back.Areas.Equal(model.Areas) {
		t.Errorf("areas round trip: got %v want %v", back.Areas, model.Areas)
	}
	if !back.Interfaces.Equal(model.Interfaces) {
		t.Errorf("interfaces round trip: got %v want %v", back.Interfaces, model.Interfaces)
	}
	if back.RedistributeConnectedRoutesMetricType != model.RedistributeConnectedRoutesMetricType {
		t.Errorf("redistribute_connected_routes_metric_type round trip: got %v want %v",
			back.RedistributeConnectedRoutesMetricType, model.RedistributeConnectedRoutesMetricType)
	}
	// A metric type the controller returns as "" reads back as null.
	if !back.RedistributeStaticRoutesMetricType.IsNull() {
		t.Errorf("redistribute_static_routes_metric_type should elide to null, got %v",
			back.RedistributeStaticRoutesMetricType)
	}
}

// Test_ospfRouterSpec_ToModel_elidesUnsetNestedMembers pins the nested-member
// elision the schema cannot see: an optional string a practitioner never set
// comes back from the controller as "" and must read as null, while area_id
// and network_id (Required) and passive_interface (defaulted) stay concrete.
func Test_ospfRouterSpec_ToModel_elidesUnsetNestedMembers(t *testing.T) {
	ctx := context.Background()
	router := &ui.OSPFRouter{
		ID:       "router-id",
		RouterID: "10.255.0.1",
		Areas:    []ui.OSPFRouterAreas{{AreaID: "0.0.0.0"}},
		Interfaces: []ui.OSPFRouterInterfaces{{
			NetworkID: "networkid1",
		}},
	}

	var model ospfRouterKitModel
	diags := ospfRouterKitSpec().ToModel(ctx, router, &model, "default")
	if diags.HasError() {
		t.Fatalf("ToModel: %v", diags)
	}

	areas := model.Areas.Elements()
	if len(areas) != 1 {
		t.Fatalf("areas = %v, want one element", model.Areas)
	}
	areaObject, ok := areas[0].(types.Object)
	if !ok {
		t.Fatalf("areas element is %T, want types.Object", areas[0])
	}
	area := areaObject.Attributes()
	if got, ok := area["area_id"].(types.String); !ok || got.ValueString() != "0.0.0.0" {
		t.Errorf("area_id = %v", area["area_id"])
	}
	for _, name := range []string{"area_type", "name"} {
		if !area[name].IsNull() {
			t.Errorf("area member %q should be null for a wire \"\", got %v", name, area[name])
		}
	}
	if !area["network_ids"].IsNull() {
		t.Errorf("network_ids should be null when the controller returns none, got %v",
			area["network_ids"])
	}

	ifaces := model.Interfaces.Elements()
	if len(ifaces) != 1 {
		t.Fatalf("interfaces = %v, want one element", model.Interfaces)
	}
	ifaceObject, ok := ifaces[0].(types.Object)
	if !ok {
		t.Fatalf("interfaces element is %T, want types.Object", ifaces[0])
	}
	iface := ifaceObject.Attributes()
	if got, ok := iface["network_id"].(types.String); !ok || got.ValueString() != "networkid1" {
		t.Errorf("network_id = %v", iface["network_id"])
	}
	if got, ok := iface["passive_interface"].(types.Bool); !ok || got.IsNull() || got.ValueBool() {
		t.Errorf("passive_interface should be a concrete false, got %v", iface["passive_interface"])
	}
	for _, name := range []string{"authentication_type", "cost", "dead_interval", "hello_interval", "priority"} {
		if !iface[name].IsNull() {
			t.Errorf("interface member %q should be null for a wire \"\", got %v", name, iface[name])
		}
	}
}

// Test_ospfRouterWireFields pins the update mask: every attribute the plan
// sets joins it, and interfaces left null stays off it, so a masked write
// never zeroes a member the practitioner is not managing.
func Test_ospfRouterWireFields(t *testing.T) {
	area, diags := types.ObjectValue(ospfAreaModel{}.AttributeTypes(), map[string]attr.Value{
		"area_id":     types.StringValue("0.0.0.0"),
		"area_type":   types.StringNull(),
		"name":        types.StringNull(),
		"network_ids": types.ListNull(types.StringType),
	})
	if diags.HasError() {
		t.Fatalf("building the area object: %v", diags)
	}
	plan := &ospfRouterKitModel{
		RouterID: types.StringValue("10.255.0.1"),
		Areas: types.ListValueMust(
			types.ObjectType{AttrTypes: ospfAreaModel{}.AttributeTypes()},
			[]attr.Value{area},
		),
		Enabled:              types.BoolValue(true),
		AnnounceDefaultRoute: types.BoolValue(false),
	}

	fields, err := ospfRouterKitSpec().WireFields(plan)
	if err != nil {
		t.Fatalf("WireFields: %v", err)
	}
	set := map[string]bool{}
	for _, f := range fields {
		set[f] = true
	}
	for _, want := range []string{"router_id", "areas", "enabled", "announce_default_route"} {
		if !set[want] {
			t.Errorf("mask is missing %q: %v", want, fields)
		}
	}
	if set["interfaces"] {
		t.Errorf("interfaces is unset in the plan and must stay off the mask: %v", fields)
	}
}
