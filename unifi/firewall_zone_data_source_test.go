package unifi

import (
	"context"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/datasource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	fwdatasource "github.com/hashicorp/terraform-plugin-framework/datasource"
	dsschema "github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// firewallZoneDataSourceTimeoutTypes is the data source's timeouts shape --
// "read" only, unlike the resource's create/read/update/delete.
var firewallZoneDataSourceTimeoutTypes = map[string]attr.Type{"read": types.StringType}

// firewallZoneDataSourceHarness builds the data source against a fake
// controller, the datasource twin of firewallZoneHarness.
func firewallZoneDataSourceHarness(t *testing.T, client *Client) (*firewallZoneDataSource, dsschema.Schema) {
	t.Helper()
	ctx := context.Background()
	d := &firewallZoneDataSource{}
	configureResp := &fwdatasource.ConfigureResponse{}
	d.Configure(ctx, fwdatasource.ConfigureRequest{ProviderData: client}, configureResp)
	if configureResp.Diagnostics.HasError() {
		t.Fatalf("configure: %v", configureResp.Diagnostics)
	}
	schemaResp := &fwdatasource.SchemaResponse{}
	d.Schema(ctx, fwdatasource.SchemaRequest{}, schemaResp)
	if schemaResp.Diagnostics.HasError() {
		t.Fatalf("build the schema: %v", schemaResp.Diagnostics)
	}
	return d, schemaResp.Schema
}

// firewallZoneDataSourceConfigFor builds a practitioner config that looks up
// a zone by name, the only key this data source accepts.
func firewallZoneDataSourceConfigFor(t *testing.T, s dsschema.Schema, name string) tfsdk.Config {
	t.Helper()
	ctx := context.Background()
	staging := tfsdk.State{Schema: s}
	model := firewallZoneDataSourceModel{
		ID:         types.StringNull(),
		Site:       types.StringValue("default"),
		Name:       types.StringValue(name),
		ZoneKey:    types.StringNull(),
		NetworkIDs: types.ListNull(types.StringType),
		Timeouts:   timeouts.Value{Object: types.ObjectNull(firewallZoneDataSourceTimeoutTypes)},
	}
	if diags := staging.Set(ctx, model); diags.HasError() {
		t.Fatalf("set the data source config: %v", diags)
	}
	return tfsdk.Config{Schema: s, Raw: staging.Raw}
}

// TestFirewallZoneDataSourceRejectsAnAmbiguousName is the data source's twin
// of TestFirewallZoneReadByNameRejectsAnAmbiguousName: today the data source
// takes the first match and silently ignores the rest.
func TestFirewallZoneDataSourceRejectsAnAmbiguousName(t *testing.T) {
	server := &zoneServer{zones: []map[string]any{
		{"_id": "zone-1", "name": "Duplicate", "network_ids": []string{}},
		{"_id": "zone-2", "name": "Duplicate", "network_ids": []string{}},
	}}
	d, s := firewallZoneDataSourceHarness(t, server.start(t))
	cfg := firewallZoneDataSourceConfigFor(t, s, "Duplicate")

	resp := &fwdatasource.ReadResponse{State: tfsdk.State{Schema: s}}
	d.Read(context.Background(), fwdatasource.ReadRequest{Config: cfg}, resp)
	if !resp.Diagnostics.HasError() {
		t.Fatal("an ambiguous name resolved to one of the matches instead of erroring")
	}
	found := false
	for _, diagnostic := range resp.Diagnostics {
		if strings.Contains(diagnostic.Detail(), "multiple firewall zones named") {
			found = true
		}
	}
	if !found {
		t.Errorf("no diagnostic named the ambiguity; got: %v", resp.Diagnostics)
	}
}

// TestFirewallZoneDataSourceReadsTheSingleMatch is the control: an
// unambiguous name must still resolve.
func TestFirewallZoneDataSourceReadsTheSingleMatch(t *testing.T) {
	server := &zoneServer{zones: []map[string]any{
		{"_id": "zone-1", "name": "Trusted", "zone_key": "trusted", "network_ids": []string{"net-a"}},
	}}
	d, s := firewallZoneDataSourceHarness(t, server.start(t))
	cfg := firewallZoneDataSourceConfigFor(t, s, "Trusted")

	resp := &fwdatasource.ReadResponse{State: tfsdk.State{Schema: s}}
	d.Read(context.Background(), fwdatasource.ReadRequest{Config: cfg}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("Read: %v", resp.Diagnostics)
	}
	var got firewallZoneDataSourceModel
	if diags := resp.State.Get(context.Background(), &got); diags.HasError() {
		t.Fatalf("read back the state: %v", diags)
	}
	if got.ID.ValueString() != "zone-1" {
		t.Errorf("id = %q, want zone-1", got.ID.ValueString())
	}
	if got.ZoneKey.ValueString() != "trusted" {
		t.Errorf("zone_key = %q, want trusted", got.ZoneKey.ValueString())
	}
}

func TestAccFirewallZoneDataSource_basic(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: `
resource "unifi_network" "firewall_zone_data_test" {
  name   = "Firewall Zone Data Test Network"
  subnet = "192.168.251.1/24"
  vlan   = 251

  # A zone claiming this network flips it to manual controller-side, while the
  # schema default asks for auto. Declare the end state so the post-apply
  # refresh plan stays empty.
  setting_preference = "manual"
}

resource "unifi_firewall_zone" "firewall_zone_data_test" {
  name        = "Firewall Zone Data Test"
  network_ids = [unifi_network.firewall_zone_data_test.id]
}

data "unifi_firewall_zone" "test" {
  name = unifi_firewall_zone.firewall_zone_data_test.name
}
`,
			Check: resource.TestCheckResourceAttr(
				"data.unifi_firewall_zone.test", "name", "Firewall Zone Data Test",
			),
		}},
	})
}

func TestNewFirewallZoneDataSource(t *testing.T) {
	d := NewFirewallZoneDataSource()
	if d == nil {
		t.Fatal("NewFirewallZoneDataSource() returned nil")
	}
	if _, ok := d.(fwdatasource.DataSourceWithConfigure); !ok {
		t.Error("expected DataSourceWithConfigure interface")
	}
}
