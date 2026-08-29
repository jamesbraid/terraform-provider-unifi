package unifi

import (
	"context"
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	fwlist "github.com/hashicorp/terraform-plugin-framework/list"
	fwresource "github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/querycheck"
	"github.com/hashicorp/terraform-plugin-testing/tfversion"
	"github.com/ubiquiti-community/go-unifi/unifi"
)

func TestAccTrafficRoute_basic(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccTrafficRouteConfig_basic(),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"unifi_traffic_route.test",
						"description",
						"tfacc-basic-route",
					),
					resource.TestCheckResourceAttr("unifi_traffic_route.test", "enabled", "true"),
					resource.TestCheckResourceAttr(
						"unifi_traffic_route.test",
						"destination.ip.#",
						"1",
					),
					resource.TestCheckResourceAttr(
						"unifi_traffic_route.test",
						"destination.ip.0.address",
						"192.168.1.2",
					),
					resource.TestCheckResourceAttr(
						"unifi_traffic_route.test",
						"kill_switch_enabled",
						"false",
					),
				),
			},
			{
				ResourceName:    "unifi_traffic_route.test",
				ImportState:     true,
				ImportStateKind: resource.ImportBlockWithResourceIdentity,
			},
		},
	})
}

func testAccTrafficRouteConfig_basic() string {
	return `
data "unifi_network" "default" {
	name = "Default"
}

resource "unifi_traffic_route" "test" {
	description         = "tfacc-basic-route"
	enabled             = true
	next_hop				    = "192.168.1.1"
	network_id			    = data.unifi_network.default.id
	destination = {
		ip = [{ address = "192.168.1.2" }]
	}
	kill_switch_enabled = false
}
`
}

func TestAccTrafficRoute_ipAddresses(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccTrafficRouteConfig_ipAddresses(),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"unifi_traffic_route.test",
						"description",
						"tfacc-ip-route",
					),
					resource.TestCheckResourceAttr(
						"unifi_traffic_route.test",
						"destination.ip.#",
						"2",
					),
					resource.TestCheckResourceAttr(
						"unifi_traffic_route.test",
						"destination.ip.0.address",
						"10.0.0.0/8",
					),
					resource.TestCheckResourceAttr(
						"unifi_traffic_route.test",
						"destination.ip.0.ports.#",
						"2",
					),
					resource.TestCheckResourceAttr(
						"unifi_traffic_route.test",
						"destination.ip.0.ports.0",
						"80",
					),
					resource.TestCheckResourceAttr(
						"unifi_traffic_route.test",
						"destination.ip.0.ports.1",
						"443",
					),
					resource.TestCheckResourceAttr(
						"unifi_traffic_route.test",
						"destination.ip.1.address",
						"192.168.1.0/24",
					),
					resource.TestCheckResourceAttr(
						"unifi_traffic_route.test",
						"destination.ip.1.ports.#",
						"1",
					),
					resource.TestCheckResourceAttr(
						"unifi_traffic_route.test",
						"destination.ip.1.ports.0",
						"8080-8090",
					),
				),
			},
			{
				ResourceName:    "unifi_traffic_route.test",
				ImportState:     true,
				ImportStateKind: resource.ImportBlockWithResourceIdentity,
			},
		},
	})
}

func testAccTrafficRouteConfig_ipAddresses() string {
	return `
resource "unifi_traffic_route" "test" {
	description     = "tfacc-ip-route"
	enabled         = true

	destination = {
		ip = [
			{
				address = "10.0.0.0/8"
				ports   = ["80", "443"]
			},
			{
				address = "192.168.1.0/24"
				ports   = ["8080-8090"]
			},
		]
	}
}
`
}

func TestAccTrafficRoute_ipRanges(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccTrafficRouteConfig_ipRanges(),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"unifi_traffic_route.test",
						"description",
						"tfacc-iprange-route",
					),
					resource.TestCheckResourceAttr(
						"unifi_traffic_route.test",
						"destination.ip.#",
						"1",
					),
					resource.TestCheckResourceAttr(
						"unifi_traffic_route.test",
						"destination.ip.0.address",
						"10.0.0.1-10.0.0.100",
					),
				),
			},
			{
				ResourceName:    "unifi_traffic_route.test",
				ImportState:     true,
				ImportStateKind: resource.ImportBlockWithResourceIdentity,
			},
		},
	})
}

func testAccTrafficRouteConfig_ipRanges() string {
	return `
resource "unifi_traffic_route" "test" {
	description     = "tfacc-iprange-route"
	enabled         = true

	destination = {
		ip = [{ address = "10.0.0.1-10.0.0.100" }]
	}
}
`
}

func TestAccTrafficRoute_sourceDefault(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccTrafficRouteConfig_sourceDefault(),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"unifi_traffic_route.test",
						"description",
						"tfacc-source-default-route",
					),
					resource.TestCheckNoResourceAttr(
						"unifi_traffic_route.test",
						"source.networks.#",
					),
					resource.TestCheckNoResourceAttr(
						"unifi_traffic_route.test",
						"source.clients.#",
					),
				),
			},
		},
	})
}

func testAccTrafficRouteConfig_sourceDefault() string {
	return `
resource "unifi_traffic_route" "test" {
	description     = "tfacc-source-default-route"
	enabled         = true
	destination = {
		domain = ["test.example.com"]
	}
}
`
}

func TestAccTrafficRoute_update(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccTrafficRouteConfig_updateStep1(),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"unifi_traffic_route.test",
						"description",
						"tfacc-update-route",
					),
					resource.TestCheckResourceAttr("unifi_traffic_route.test", "enabled", "true"),
					resource.TestCheckResourceAttr(
						"unifi_traffic_route.test",
						"destination.domain.#",
						"1",
					),
					resource.TestCheckResourceAttr(
						"unifi_traffic_route.test",
						"destination.domain.0",
						"before.example.com",
					),
					resource.TestCheckResourceAttr(
						"unifi_traffic_route.test",
						"kill_switch_enabled",
						"false",
					),
				),
			},
			{
				Config: testAccTrafficRouteConfig_updateStep2(),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"unifi_traffic_route.test",
						"description",
						"tfacc-update-route-modified",
					),
					resource.TestCheckResourceAttr("unifi_traffic_route.test", "enabled", "true"),
					resource.TestCheckResourceAttr(
						"unifi_traffic_route.test",
						"destination.domain.#",
						"2",
					),
					resource.TestCheckResourceAttr(
						"unifi_traffic_route.test",
						"destination.domain.0",
						"after1.example.com",
					),
					resource.TestCheckResourceAttr(
						"unifi_traffic_route.test",
						"destination.domain.1",
						"after2.example.com",
					),
					resource.TestCheckResourceAttr(
						"unifi_traffic_route.test",
						"kill_switch_enabled",
						"true",
					),
				),
			},
			{
				Config: testAccTrafficRouteConfig_updateStep3(),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("unifi_traffic_route.test", "enabled", "false"),
				),
			},
		},
	})
}

func testAccTrafficRouteConfig_updateStep1() string {
	return `
resource "unifi_traffic_route" "test" {
	description     = "tfacc-update-route"
	enabled         = true
	destination = {
		domain = ["before.example.com"]
	}
}
`
}

func testAccTrafficRouteConfig_updateStep2() string {
	return `
resource "unifi_traffic_route" "test" {
	description        = "tfacc-update-route-modified"
	enabled            = true
	destination = {
		domain = ["after1.example.com", "after2.example.com"]
	}
	kill_switch_enabled = true
}
`
}

func testAccTrafficRouteConfig_updateStep3() string {
	return `
resource "unifi_traffic_route" "test" {
	description     = "tfacc-update-route-modified"
	enabled         = false
	destination = {
		domain = ["after1.example.com", "after2.example.com"]
	}
}
`
}

func TestAccTrafficRoute_regions(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccTrafficRouteConfig_regions(),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"unifi_traffic_route.test",
						"description",
						"tfacc-region-route",
					),
					resource.TestCheckResourceAttr(
						"unifi_traffic_route.test",
						"destination.region.#",
						"2",
					),
					resource.TestCheckResourceAttr(
						"unifi_traffic_route.test",
						"destination.region.0",
						"US",
					),
					resource.TestCheckResourceAttr(
						"unifi_traffic_route.test",
						"destination.region.1",
						"CA",
					),
				),
			},
			{
				ResourceName:    "unifi_traffic_route.test",
				ImportState:     true,
				ImportStateKind: resource.ImportBlockWithResourceIdentity,
			},
		},
	})
}

func testAccTrafficRouteConfig_regions() string {
	return `
resource "unifi_traffic_route" "test" {
	description     = "tfacc-region-route"
	enabled         = true
	destination = {
		region = ["US", "CA"]
	}
}
`
}

func TestAccTrafficRoute_fullConfig(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccTrafficRouteConfig_full(),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"unifi_traffic_route.test",
						"description",
						"tfacc-full-route",
					),
					resource.TestCheckResourceAttr("unifi_traffic_route.test", "enabled", "true"),
					resource.TestCheckResourceAttr(
						"unifi_traffic_route.test",
						"kill_switch_enabled",
						"true",
					),
					resource.TestCheckResourceAttr(
						"unifi_traffic_route.test",
						"destination.ip.#",
						"2",
					),
					resource.TestCheckResourceAttr(
						"unifi_traffic_route.test",
						"destination.ip.0.address",
						"172.16.0.0/12",
					),
					resource.TestCheckResourceAttr(
						"unifi_traffic_route.test",
						"destination.ip.1.address",
						"192.168.0.1-192.168.0.50",
					),
					resource.TestCheckResourceAttr(
						"unifi_traffic_route.test",
						"source.clients.#",
						"1",
					),
					resource.TestCheckResourceAttr(
						"unifi_traffic_route.test",
						"source.clients.0.mac",
						"aa:bb:cc:dd:ee:ff",
					),
				),
			},
			{
				ResourceName:    "unifi_traffic_route.test",
				ImportState:     true,
				ImportStateKind: resource.ImportBlockWithResourceIdentity,
			},
		},
	})
}

func testAccTrafficRouteConfig_full() string {
	return `
resource "unifi_traffic_route" "test" {
	description         = "tfacc-full-route"
	enabled             = true
	kill_switch_enabled = true

	destination = {
		ip = [
			{ address = "172.16.0.0/12" },
			{ address = "192.168.0.1-192.168.0.50" },
		]
	}

	source = { clients = [{ mac = "aa:bb:cc:dd:ee:ff" }] }
}
`
}

func TestNewTrafficRouteResource(t *testing.T) {
	r := NewTrafficRouteResource()
	if r == nil {
		t.Fatal("NewTrafficRouteResource() returned nil")
	}
	if _, ok := r.(fwresource.ResourceWithConfigure); !ok {
		t.Error("expected ResourceWithConfigure interface")
	}
	if _, ok := r.(fwresource.ResourceWithImportState); !ok {
		t.Error("expected ResourceWithImportState interface")
	}
}

func TestNewTrafficRouteListResource(t *testing.T) {
	r := NewTrafficRouteListResource()
	if r == nil {
		t.Fatal("NewTrafficRouteListResource() returned nil")
	}
}

func Test_destinationIPModel_AttributeTypes(t *testing.T) {
	m := destinationIPModel{}
	got := m.AttributeTypes()
	for _, key := range []string{"address", "ports"} {
		if _, ok := got[key]; !ok {
			t.Errorf("AttributeTypes() missing key %q", key)
		}
	}
	if got["address"] != types.StringType {
		t.Errorf("address type = %v, want StringType", got["address"])
	}
}

func Test_sourceNetworkModel_AttributeTypes(t *testing.T) {
	m := sourceNetworkModel{}
	got := m.AttributeTypes()
	if _, ok := got["id"]; !ok {
		t.Error("AttributeTypes() missing key 'id'")
	}
	if got["id"] != types.StringType {
		t.Errorf("id type = %v, want StringType", got["id"])
	}
}

func Test_sourceClientModel_AttributeTypes(t *testing.T) {
	m := sourceClientModel{}
	got := m.AttributeTypes()
	if _, ok := got["mac"]; !ok {
		t.Error("AttributeTypes() missing key 'mac'")
	}
	if got["mac"] != types.StringType {
		t.Errorf("mac type = %v, want StringType", got["mac"])
	}
}

func Test_sourceModel_AttributeTypes(t *testing.T) {
	m := sourceModel{}
	got := m.AttributeTypes()
	for _, key := range []string{"networks", "clients"} {
		if _, ok := got[key]; !ok {
			t.Errorf("AttributeTypes() missing key %q", key)
		}
	}
}

func Test_destinationModel_AttributeTypes(t *testing.T) {
	m := destinationModel{}
	got := m.AttributeTypes()
	for _, key := range []string{"domain", "ip", "region"} {
		if _, ok := got[key]; !ok {
			t.Errorf("AttributeTypes() missing key %q", key)
		}
	}
}

func Test_trafficRouteResource_IdentitySchema(t *testing.T) {
	r := newTrafficRouteKitResource()
	resp := &fwresource.IdentitySchemaResponse{}
	r.IdentitySchema(context.Background(), fwresource.IdentitySchemaRequest{}, resp)
	if resp.Diagnostics.HasError() {
		t.Errorf("IdentitySchema() produced errors: %v", resp.Diagnostics)
	}
	if _, ok := resp.IdentitySchema.Attributes["id"]; !ok {
		t.Error("IdentitySchema missing 'id' attribute")
	}
}

func Test_trafficRouteSpec_ToSDK(t *testing.T) {
	ctx := context.Background()

	t.Run("nil client causes error on network lookup", func(t *testing.T) {
		// The empty-network_id case is BeforeSend's (Test_trafficRouteBeforeSend);
		// ToSDK no longer reaches a client at all.
		model := &trafficRouteKitModel{
			Description:       types.StringValue("test-route"),
			Enabled:           types.BoolValue(true),
			KillSwitchEnabled: types.BoolValue(false),
			NetworkID:         types.StringValue("some-network-id"),
			Destination:       types.ObjectNull(destinationModel{}.AttributeTypes()),
			Source:            types.ObjectNull(sourceModel{}.AttributeTypes()),
		}
		got, diags := trafficRouteKitSpec().ToSDK(ctx, model)
		if diags.HasError() {
			t.Fatalf("unexpected diags: %v", diags)
		}
		if got == nil {
			t.Fatal("expected non-nil result")
		}
		if got.Description != "test-route" {
			t.Errorf("Description = %q, want test-route", got.Description)
		}
		if !got.Enabled {
			t.Error("Enabled should be true")
		}
		if got.NetworkID != "some-network-id" {
			t.Errorf("NetworkID = %q, want some-network-id", got.NetworkID)
		}
	})

	t.Run("domain destination sets MatchingTarget", func(t *testing.T) {
		domainList, d := types.ListValueFrom(ctx, types.StringType, []string{"example.com"})
		if d.HasError() {
			t.Fatalf("building domain list: %v", d)
		}
		dest := destinationModel{
			Domain: domainList,
			IP: types.ListNull(
				types.ObjectType{AttrTypes: destinationIPModel{}.AttributeTypes()},
			),
			Region: types.ListNull(types.StringType),
		}
		destObj, d := types.ObjectValueFrom(ctx, destinationModel{}.AttributeTypes(), dest)
		if d.HasError() {
			t.Fatalf("building destination object: %v", d)
		}
		model := &trafficRouteKitModel{
			Enabled:           types.BoolValue(true),
			KillSwitchEnabled: types.BoolValue(false),
			NetworkID:         types.StringValue("net-1"),
			Destination:       destObj,
			Source:            types.ObjectNull(sourceModel{}.AttributeTypes()),
		}
		got, diags := trafficRouteKitSpec().ToSDK(ctx, model)
		if diags.HasError() {
			t.Fatalf("unexpected diags: %v", diags)
		}
		if got.MatchingTarget != "DOMAIN" {
			t.Errorf("MatchingTarget = %q, want DOMAIN", got.MatchingTarget)
		}
		if len(got.Domains) != 1 || got.Domains[0].Domain != "example.com" {
			t.Errorf("Domains = %v, want [{example.com}]", got.Domains)
		}
	})
}

func Test_trafficRouteSpec_ToModel(t *testing.T) {
	ctx := context.Background()

	t.Run("basic fields populated", func(t *testing.T) {
		route := &unifi.TrafficRoute{
			ID:                "route-123",
			Description:       "my-route",
			Enabled:           true,
			KillSwitchEnabled: false,
			NetworkID:         "net-abc",
			MatchingTarget:    "INTERNET",
			TargetDevices:     []unifi.TrafficRouteTargetDevices{{Type: "ALL_CLIENTS"}},
		}
		var model trafficRouteKitModel
		diags := trafficRouteKitSpec().ToModel(ctx, route, &model, "default")
		if diags.HasError() {
			t.Fatalf("unexpected diags: %v", diags)
		}
		if model.ID.ValueString() != "route-123" {
			t.Errorf("ID = %q, want route-123", model.ID.ValueString())
		}
		if model.Description.ValueString() != "my-route" {
			t.Errorf("Description = %q, want my-route", model.Description.ValueString())
		}
		if !model.Enabled.ValueBool() {
			t.Error("Enabled should be true")
		}
		if model.Site.ValueString() != "default" {
			t.Errorf("Site = %q, want default", model.Site.ValueString())
		}
	})

	t.Run("domain route sets destination", func(t *testing.T) {
		route := &unifi.TrafficRoute{
			ID:             "route-456",
			Enabled:        true,
			MatchingTarget: "DOMAIN",
			Domains: []unifi.TrafficRouteDomains{
				{Domain: "example.com"},
				{Domain: "test.com"},
			},
			TargetDevices: []unifi.TrafficRouteTargetDevices{{Type: "ALL_CLIENTS"}},
		}
		var model trafficRouteKitModel
		diags := trafficRouteKitSpec().ToModel(ctx, route, &model, "site1")
		if diags.HasError() {
			t.Fatalf("unexpected diags: %v", diags)
		}
		if model.Destination.IsNull() {
			t.Fatal("Destination should not be null for a domain route")
		}
		var dest destinationModel
		if d := model.Destination.As(
			ctx,
			&dest,
			struct{ UnhandledNullAsEmpty, UnhandledUnknownAsEmpty bool }{},
		); d.HasError() {
			t.Fatalf("reading destination: %v", d)
		}
		var domains []string
		if d := dest.Domain.ElementsAs(ctx, &domains, false); d.HasError() {
			t.Fatalf("reading domains: %v", d)
		}
		if len(domains) != 2 {
			t.Errorf("domains len = %d, want 2", len(domains))
		}
	})
}

func Test_trafficRouteResource_ListResourceConfigSchema(t *testing.T) {
	r := newTrafficRouteKitResource()
	resp := &fwlist.ListResourceSchemaResponse{}
	r.ListResourceConfigSchema(context.Background(), fwlist.ListResourceSchemaRequest{}, resp)
	if resp.Diagnostics.HasError() {
		t.Errorf("ListResourceConfigSchema() produced errors: %v", resp.Diagnostics)
	}
	if _, ok := resp.Schema.Attributes["site"]; !ok {
		t.Error("ListResourceConfigSchema missing 'site' attribute")
	}
}

// Test_trafficRouteSpec_ToSDK_ipRange verifies IP range addresses are
// converted to TrafficRouteIPRanges (not IPAddresses) in the API struct.
func Test_trafficRouteSpec_ToSDK_ipRange(t *testing.T) {
	ctx := context.Background()

	ipEntry := destinationIPModel{
		Address: types.StringValue("10.0.0.1-10.0.0.100"),
		Ports:   types.ListNull(types.StringType),
	}
	ipObj, d := types.ObjectValueFrom(ctx, destinationIPModel{}.AttributeTypes(), ipEntry)
	if d.HasError() {
		t.Fatalf("building ip object: %v", d)
	}
	ipList, d := types.ListValue(
		types.ObjectType{AttrTypes: destinationIPModel{}.AttributeTypes()},
		[]attr.Value{ipObj},
	)
	if d.HasError() {
		t.Fatalf("building ip list: %v", d)
	}
	dest := destinationModel{
		Domain: types.ListNull(types.StringType),
		IP:     ipList,
		Region: types.ListNull(types.StringType),
	}
	destObj, d := types.ObjectValueFrom(ctx, destinationModel{}.AttributeTypes(), dest)
	if d.HasError() {
		t.Fatalf("building destination object: %v", d)
	}

	model := &trafficRouteKitModel{
		Enabled:           types.BoolValue(true),
		KillSwitchEnabled: types.BoolValue(false),
		NetworkID:         types.StringValue("net-1"),
		Destination:       destObj,
		Source:            types.ObjectNull(sourceModel{}.AttributeTypes()),
	}

	got, diags := trafficRouteKitSpec().ToSDK(ctx, model)
	if diags.HasError() {
		t.Fatalf("unexpected diags: %v", diags)
	}
	if len(got.IPRanges) != 1 {
		t.Fatalf("IPRanges len = %d, want 1", len(got.IPRanges))
	}
	if got.IPRanges[0].Start != "10.0.0.1" || got.IPRanges[0].Stop != "10.0.0.100" {
		t.Errorf("IPRange = {%s-%s}, want {10.0.0.1-10.0.0.100}",
			got.IPRanges[0].Start, got.IPRanges[0].Stop)
	}
	if len(got.IPAddresses) != 0 {
		t.Errorf("IPAddresses should be empty for a range, got %v", got.IPAddresses)
	}
}

func TestAccTrafficRouteList_basic(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		TerraformVersionChecks: []tfversion.TerraformVersionCheck{
			tfversion.SkipBelow(tfversion.Version1_14_0),
		},
		Steps: []resource.TestStep{
			{
				Config: testAccTrafficRouteConfig_basic(),
			},
			{
				Query: true,
				Config: `
					provider "unifi" {}
					list "unifi_traffic_route" "test" {
						provider = unifi
						config {
							filter {
								name  = "description"
								value = "tfacc-basic-route"
						  }
					  }
					}
				`,
				QueryResultChecks: []querycheck.QueryResultCheck{
					querycheck.ExpectLengthAtLeast("unifi_traffic_route.test", 1),
				},
			},
		},
	})
}

// Test_trafficRouteWireFields is the check the scattered kind exists for: a
// mask missing even one of destination's five wire attributes silently
// under-writes. matching_target is the one that's easy to miss -- no member
// of the destination object is named that.
func Test_trafficRouteWireFields(t *testing.T) {
	ctx := context.Background()

	domainList, d := types.ListValueFrom(ctx, types.StringType, []string{"example.com"})
	if d.HasError() {
		t.Fatalf("building domain list: %v", d)
	}
	destObj, d := types.ObjectValueFrom(ctx, destinationModel{}.AttributeTypes(), destinationModel{
		Domain: domainList,
		IP:     types.ListNull(types.ObjectType{AttrTypes: destinationIPModel{}.AttributeTypes()}),
		Region: types.ListNull(types.StringType),
	})
	if d.HasError() {
		t.Fatalf("building destination object: %v", d)
	}

	plan := &trafficRouteKitModel{
		Description:       types.StringValue("r"),
		Enabled:           types.BoolValue(true),
		KillSwitchEnabled: types.BoolValue(false),
		NetworkID:         types.StringValue("net-1"),
		Destination:       destObj,
		Source:            types.ObjectNull(sourceModel{}.AttributeTypes()),
	}

	fields, err := trafficRouteKitSpec().WireFields(plan)
	if err != nil {
		t.Fatalf("WireFields: %v", err)
	}
	got := make(map[string]bool, len(fields))
	for _, f := range fields {
		got[f] = true
	}
	for _, want := range []string{"domains", "regions", "ip_addresses", "ip_ranges", "matching_target"} {
		if !got[want] {
			t.Errorf("mask is missing %q: the apply succeeds and the controller keeps its old value", want)
		}
	}

	// target_devices and network_id are AlwaysWire, so they travel even though
	// source is null here and nothing in the plan carries the derived WAN id.
	for _, want := range []string{"target_devices", "network_id"} {
		if !got[want] {
			t.Errorf("mask is missing AlwaysWire field %q", want)
		}
	}
}

// Test_trafficRouteWireFields_nullDestination pins the other half: a route
// with no destination still says INTERNET.
func Test_trafficRouteWireFields_nullDestination(t *testing.T) {
	ctx := context.Background()

	plan := &trafficRouteKitModel{
		Enabled:           types.BoolValue(true),
		KillSwitchEnabled: types.BoolValue(false),
		NetworkID:         types.StringValue("net-1"),
		Destination:       types.ObjectNull(destinationModel{}.AttributeTypes()),
		Source:            types.ObjectNull(sourceModel{}.AttributeTypes()),
	}

	fields, err := trafficRouteKitSpec().WireFields(plan)
	if err != nil {
		t.Fatalf("WireFields: %v", err)
	}
	found := false
	for _, f := range fields {
		if f == "matching_target" {
			found = true
		}
	}
	if !found {
		t.Error("matching_target must stay on the mask when destination is null")
	}

	sdk, diags := trafficRouteKitSpec().ToSDK(ctx, plan)
	if diags.HasError() {
		t.Fatalf("ToSDK: %v", diags)
	}
	if sdk.MatchingTarget != "INTERNET" {
		t.Errorf("MatchingTarget = %q, want INTERNET", sdk.MatchingTarget)
	}
	if len(sdk.TargetDevices) != 1 || sdk.TargetDevices[0].Type != "ALL_CLIENTS" {
		t.Errorf("TargetDevices = %v, want one ALL_CLIENTS entry", sdk.TargetDevices)
	}
}

// Test_trafficRouteBeforeSend covers the network_id derivation: it fires
// only when the encoded object left the field empty, so a pinned network_id
// survives an update that touches anything else.
func Test_trafficRouteBeforeSend(t *testing.T) {
	ctx := context.Background()
	lookup := func(context.Context) (string, error) { return "derived-wan", nil }

	t.Run("fills an empty network_id", func(t *testing.T) {
		sdk := &unifi.TrafficRoute{}
		if d := trafficRouteBeforeSend(ctx, nil, nil, trafficRouteKitModel{}, sdk, lookup); d.HasError() {
			t.Fatalf("unexpected diags: %v", d)
		}
		if sdk.NetworkID != "derived-wan" {
			t.Errorf("NetworkID = %q, want derived-wan", sdk.NetworkID)
		}
	})

	t.Run("leaves a set network_id alone", func(t *testing.T) {
		sdk := &unifi.TrafficRoute{NetworkID: "pinned"}
		if d := trafficRouteBeforeSend(ctx, nil, nil, trafficRouteKitModel{}, sdk, lookup); d.HasError() {
			t.Fatalf("unexpected diags: %v", d)
		}
		if sdk.NetworkID != "pinned" {
			t.Errorf("NetworkID = %q, want pinned", sdk.NetworkID)
		}
	})

	t.Run("reports a lookup failure", func(t *testing.T) {
		failing := func(context.Context) (string, error) {
			return "", fmt.Errorf("no default WAN network found")
		}
		sdk := &unifi.TrafficRoute{}
		d := trafficRouteBeforeSend(ctx, nil, nil, trafficRouteKitModel{}, sdk, failing)
		if !d.HasError() {
			t.Fatal("expected an error diagnostic")
		}
	})
}
