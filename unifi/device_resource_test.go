package unifi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"sort"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-nettypes/hwtypes"
	"github.com/hashicorp/terraform-plugin-framework-timetypes/timetypes"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	fwlist "github.com/hashicorp/terraform-plugin-framework/list"
	fwresource "github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/querycheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/hashicorp/terraform-plugin-testing/tfversion"
	"github.com/ubiquiti-community/go-unifi/unifi"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/acctestenv"
)

func TestAccDeviceList_basic(t *testing.T) {
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
list "unifi_device" "test" {
  provider = unifi
  config {}
}
`,
			QueryResultChecks: []querycheck.QueryResultCheck{
				querycheck.ExpectLengthAtLeast("unifi_device.test", 1),
			},
		}},
	})
}

func indexOverrides(pos []unifi.DevicePortOverrides) map[int64]unifi.DevicePortOverrides {
	m := make(map[int64]unifi.DevicePortOverrides, len(pos))
	for _, po := range pos {
		if po.PortIDX != nil {
			m[*po.PortIDX] = po
		}
	}
	return m
}

// TestAccDeviceFramework_basic drives whichever device the harness started for
// it. The MAC comes from the herder's ready event rather than from a literal,
// because a literal can only name a controller-simulated demo device, which
// never informs and so never exercises adoption for real.
func TestAccDeviceFramework_basic(t *testing.T) {
	mac := os.Getenv(acctestenv.EnvAccDeviceMAC)
	if mac == "" {
		t.Skipf("%s not set; skipping device acceptance test", acctestenv.EnvAccDeviceMAC)
	}
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccDeviceFrameworkConfig_basic(mac),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet("unifi_device.test", "id"),
					resource.TestCheckResourceAttr("unifi_device.test", "name", "Test Device"),
					resource.TestCheckResourceAttr("unifi_device.test", "adopted", "true"),
				),
			},
			{
				ResourceName:      "unifi_device.test",
				ImportState:       true,
				ImportStateVerify: true,
				// state is live controller telemetry, not configuration: a
				// device that is still provisioning reports 5 and settles to 1
				// on its own schedule. Comparing it across the two reads that
				// import performs is a race, and it has failed both suites
				// intermittently.
				ImportStateVerifyIgnore: []string{"allow_adoption", "forget_on_destroy", "state"},
			},
		},
	})
}

// testAccRawDeviceClient builds an ApiClient straight from the same env vars
// preCheck requires, for assertions Terraform state cannot make:
// port_override state only ever holds the ports a config block declares
// (deviceReconcilePortOverrides' whole reason to exist), so "did an
// undeclared port change" has to be answered by reading the controller
// directly.
func testAccRawDeviceClient(t *testing.T) *unifi.ApiClient {
	t.Helper()
	client, err := unifi.New(context.Background(), &unifi.Config{
		BaseURL:       os.Getenv("UNIFI_API"),
		Username:      os.Getenv("UNIFI_USERNAME"),
		Password:      os.Getenv("UNIFI_PASSWORD"),
		AllowInsecure: os.Getenv("UNIFI_INSECURE") != "",
	})
	if err != nil {
		t.Fatalf("building a raw API client: %v", err)
	}
	return client
}

func testAccDeviceSite() string {
	if site := os.Getenv("UNIFI_SITE"); site != "" {
		return site
	}
	return "default"
}

// TestAccDeviceFramework_portOverrideLeavesOtherPortsAlone is the live
// counterpart to the fake-controller tests above (Test_deviceUpdate_*): it
// declares two port_override blocks with disjoint member sets -- port 1
// names only "name", port 2 only "poe_mode" -- on a real controller, and
// checks, by reading the device directly rather than through Terraform
// state, that each port's own declared write takes effect while the
// member it did NOT declare survives untouched. That is the exact
// disjoint-set scenario measured live against a controller and found
// unsafe under a single union-mask call: a call carrying both fields would
// force each port's undeclared member to its Go zero value. Two
// differently-shaped blocks in one config also means this apply issues two
// UpdateDevicePortOverrides calls rather than one, so the multi-call path
// (updateDevicePortOverridesGrouped/groupPortOverridesByFieldSet) runs
// against a real controller here, not only against a fake one.
func TestAccDeviceFramework_portOverrideLeavesOtherPortsAlone(t *testing.T) {
	mac := os.Getenv(acctestenv.EnvAccDeviceMAC)
	if mac == "" {
		t.Skipf("%s not set; skipping device acceptance test", acctestenv.EnvAccDeviceMAC)
	}

	const port1IDX = 1
	const port2IDX = 2

	client := testAccRawDeviceClient(t)
	site := testAccDeviceSite()
	ctx := context.Background()

	// A freshly adopted device reports no port_overrides at all -- there is
	// nothing to compare against until something has been written -- so
	// both ports are seeded first, with a matched mask, before the apply.
	seedDevice, err := client.GetDeviceByMAC(ctx, site, mac)
	if err != nil {
		t.Fatalf("reading the device to seed it: %v", err)
	}
	if _, err := client.UpdateDevicePortOverrides(ctx, site, seedDevice, []unifi.DevicePortOverrides{
		{PortIDX: ptrInt64(port1IDX), Name: "seed-one", PoeMode: "auto"},
		{PortIDX: ptrInt64(port2IDX), Name: "seed-two", PoeMode: "auto"},
	}, "name", "poe_mode"); err != nil {
		t.Fatalf("seeding both ports: %v", err)
	}

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccDeviceFrameworkConfig_twoPortOverrides(mac, port1IDX, port2IDX),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet("unifi_device.test", "id"),
					resource.TestCheckResourceAttr("unifi_device.test", "port_override.#", "2"),
					func(*terraform.State) error {
						after, err := client.GetDeviceByMAC(context.Background(), site, mac)
						if err != nil {
							return fmt.Errorf("reading the device after apply: %w", err)
						}
						byIdx := indexOverrides(after.PortOverrides)

						port1, ok := byIdx[port1IDX]
						if !ok {
							return fmt.Errorf("port %d is missing after apply", port1IDX)
						}
						if port1.Name != "renamed-one" {
							return fmt.Errorf("port %d name = %q, want renamed-one "+
								"(its own declared write did not take effect)", port1IDX, port1.Name)
						}
						if port1.PoeMode != "auto" {
							return fmt.Errorf("port %d poe_mode = %q, want auto unchanged -- "+
								"the member it did not declare must survive port 2's write",
								port1IDX, port1.PoeMode)
						}

						port2, ok := byIdx[port2IDX]
						if !ok {
							return fmt.Errorf("port %d is missing after apply", port2IDX)
						}
						if port2.PoeMode != "off" {
							return fmt.Errorf("port %d poe_mode = %q, want off "+
								"(its own declared write did not take effect)", port2IDX, port2.PoeMode)
						}
						if port2.Name != "seed-two" {
							return fmt.Errorf("port %d name = %q, want seed-two unchanged -- "+
								"the member it did not declare must survive port 1's write",
								port2IDX, port2.Name)
						}
						return nil
					},
				),
			},
		},
	})
}

func testAccDeviceFrameworkConfig_twoPortOverrides(mac string, port1IDX, port2IDX int) string {
	return fmt.Sprintf(`
resource "unifi_device" "test" {
	mac  = %q
	name = "Test Device"
	allow_adoption = true
	forget_on_destroy = false

	port_override {
		index = %d
		name  = "renamed-one"
	}

	port_override {
		index    = %d
		poe_mode = "off"
	}
}
`, mac, port1IDX, port2IDX)
}

func testAccDeviceFrameworkConfig_basic(mac string) string {
	return fmt.Sprintf(`
resource "unifi_device" "test" {
	mac  = %q
	name = "Test Device"
	allow_adoption = true
	forget_on_destroy = false
}
`, mac)
}

func TestNewDeviceFrameworkResource(t *testing.T) {
	// A populated Spec carries closures, which are never DeepEqual, so the
	// thing worth asserting is the type the provider registers.
	got := NewDeviceFrameworkResource()
	if got == nil {
		t.Fatal("NewDeviceFrameworkResource() = nil")
	}
	if _, ok := got.(*deviceKitResource); !ok {
		t.Errorf("NewDeviceFrameworkResource() = %T, want *deviceKitResource", got)
	}
}

func TestNewDeviceListResource(t *testing.T) {
	got := NewDeviceListResource()
	if got == nil {
		t.Fatal("NewDeviceListResource() = nil")
	}
	if _, ok := got.(*deviceKitResource); !ok {
		t.Errorf("NewDeviceListResource() = %T, want *deviceKitResource", got)
	}
}

func Test_portOverrideModel_AttributeTypes(t *testing.T) {
	tests := []struct {
		name string
		m    portOverrideModel
		want map[string]attr.Type
	}{
		{
			name: "returns portOverrideAttrTypes",
			m:    portOverrideModel{},
			want: portOverrideAttrTypes(),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.m.AttributeTypes(); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("portOverrideModel.AttributeTypes() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_deviceResource_IdentitySchema(t *testing.T) {
	type args struct {
		in0  context.Context
		in1  fwresource.IdentitySchemaRequest
		resp *fwresource.IdentitySchemaResponse
	}
	tests := []struct {
		name string
		r    *deviceKitResource
		args args
	}{
		{
			name: "returns identity schema",
			r:    newDeviceKitResource(),
			args: args{
				in0:  context.Background(),
				in1:  fwresource.IdentitySchemaRequest{},
				resp: &fwresource.IdentitySchemaResponse{},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.r.IdentitySchema(tt.args.in0, tt.args.in1, tt.args.resp)
		})
	}
}

func Test_deviceResource_Schema(t *testing.T) {
	type args struct {
		ctx  context.Context
		req  fwresource.SchemaRequest
		resp *fwresource.SchemaResponse
	}
	tests := []struct {
		name string
		r    *deviceKitResource
		args args
	}{
		{
			name: "returns schema",
			r:    newDeviceKitResource(),
			args: args{
				ctx:  context.Background(),
				req:  fwresource.SchemaRequest{},
				resp: &fwresource.SchemaResponse{},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.r.Schema(tt.args.ctx, tt.args.req, tt.args.resp)
		})
	}
}

func Test_deviceResource_UpgradeState(t *testing.T) {
	got := newDeviceKitResource().UpgradeState(context.Background())
	for _, version := range []int64{0, 1, 2} {
		if _, ok := got[version]; !ok {
			t.Errorf("missing state upgrader for schema version %d", version)
		}
	}
	if len(got) != 3 {
		t.Errorf("UpgradeState() has %d upgraders, want 3", len(got))
	}
}

// TestDeviceNetworkconfIDsAreSets: the port_override networkconf_ids
// attributes are order-insensitive Sets, and a v2->v3 state upgrader
// exists so existing List state migrates instead of erroring on refresh.
// Adapted from the hand-written resource's equivalent test, to construct
// the kit resource and to the kit's own schema-version numbering, which
// already carried an unrelated v1->v2 bump (dropping
// radio_table.assisted_roaming_*) before this fix landed here.
func TestDeviceNetworkconfIDsAreSets(t *testing.T) {
	ctx := context.Background()
	r := newDeviceKitResource()

	var schemaResp fwresource.SchemaResponse
	r.Schema(ctx, fwresource.SchemaRequest{}, &schemaResp)

	if schemaResp.Schema.Version != 3 {
		t.Errorf("device schema Version = %d, want 3", schemaResp.Schema.Version)
	}

	block, ok := schemaResp.Schema.Blocks["port_override"].(schema.SetNestedBlock)
	if !ok {
		t.Fatalf(
			"port_override is not a SetNestedBlock: %T",
			schemaResp.Schema.Blocks["port_override"],
		)
	}
	for _, name := range []string{
		"excluded_networkconf_ids",
		"multicast_router_networkconf_ids",
		"tagged_networkconf_ids",
	} {
		if _, ok := block.NestedObject.Attributes[name].(schema.SetAttribute); !ok {
			t.Errorf(
				"port_override.%s must be a SetAttribute, got %T",
				name, block.NestedObject.Attributes[name],
			)
		}
	}

	ups := r.UpgradeState(ctx)
	for _, v := range []int64{0, 1, 2} {
		if _, ok := ups[v]; !ok {
			t.Errorf("UpgradeState is missing an upgrader for schema version %d", v)
		}
	}
}

// Test_deviceResource_UpgradeState_networkconfIDsListToSet guards the v2 ->
// v3 migration end to end: port_override.{excluded,
// multicast_router,tagged}_networkconf_ids changed from List to Set. The
// upgrader does no field rewrite for this change -- it relies entirely on
// reconcileType decoding a JSON array against the current (Set) schema type
// -- so unlike Test_deviceResource_UpgradeState_dropsAssistedRoaming below,
// nothing here asserts an attribute was REMOVED. What has to be proven is
// that prior List-shaped state decodes losslessly: every declared element
// survives, an empty declared collection survives as empty (not null), and
// an attribute the kit descriptor never populates (tagged_networkconf_ids is
// unwired in both directions -- see the comment on devicePortOverrideEncode
// -- so no real prior state ever carried a value for it; fabricated here as
// a declared attribute simply absent from the raw JSON) reconciles to a
// null Set rather than erroring.
func Test_deviceResource_UpgradeState_networkconfIDsListToSet(t *testing.T) {
	ctx := context.Background()
	r := newDeviceKitResource()

	priorState := `{
		"id": "abc123",
		"site": "default",
		"mac": "00:11:22:33:44:55",
		"port_override": [
			{
				"index": 1,
				"excluded_networkconf_ids": ["net-a", "net-b", "net-c"],
				"multicast_router_networkconf_ids": ["net-x", "net-y"]
			},
			{
				"index": 2,
				"excluded_networkconf_ids": []
			}
		]
	}`

	upgrader, ok := r.UpgradeState(ctx)[2]
	if !ok {
		t.Fatal("no v2 state upgrader registered")
	}

	resp := &fwresource.UpgradeStateResponse{}
	upgrader.StateUpgrader(ctx, fwresource.UpgradeStateRequest{
		RawState: &tfprotov6.RawState{JSON: []byte(priorState)},
	}, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("upgrade produced diagnostics: %v", resp.Diagnostics)
	}
	if resp.DynamicValue == nil {
		t.Fatal("upgrade produced no value")
	}

	// Decoding against the CURRENT (v3) schema is the assertion: a List
	// value left in place by a botched upgrade would fail this decode
	// outright, the same way a removed attribute surviving fails
	// Test_deviceResource_UpgradeState_dropsAssistedRoaming.
	var schemaResp fwresource.SchemaResponse
	r.Schema(ctx, fwresource.SchemaRequest{}, &schemaResp)
	schemaType := schemaResp.Schema.Type().TerraformType(ctx)

	val, err := resp.DynamicValue.Unmarshal(schemaType)
	if err != nil {
		t.Fatalf("upgraded state does not decode against the v3 schema: %v", err)
	}

	var obj map[string]tftypes.Value
	if err := val.As(&obj); err != nil {
		t.Fatalf("decoding upgraded object: %v", err)
	}
	var overrides []tftypes.Value
	if err := obj["port_override"].As(&overrides); err != nil {
		t.Fatalf("decoding port_override: %v", err)
	}
	if len(overrides) != 2 {
		t.Fatalf("port_override has %d entries, want 2", len(overrides))
	}

	byIndex := make(map[int64]map[string]tftypes.Value, len(overrides))
	for _, ov := range overrides {
		var m map[string]tftypes.Value
		if err := ov.As(&m); err != nil {
			t.Fatalf("decoding port_override entry: %v", err)
		}
		var idx *big.Float
		if err := m["index"].As(&idx); err != nil {
			t.Fatalf("decoding port_override.index: %v", err)
		}
		i, _ := idx.Int64()
		byIndex[i] = m
	}

	port1, ok := byIndex[1]
	if !ok {
		t.Fatal("port_override entry with index 1 did not survive the upgrade")
	}
	if got := deviceUpgradeTestStringSet(t, port1["excluded_networkconf_ids"]); !slicesEqualAsSets(
		got, []string{"net-a", "net-b", "net-c"},
	) {
		t.Errorf(
			"port_override[index=1].excluded_networkconf_ids = %v, want {net-a, net-b, net-c}",
			got,
		)
	}
	if got := deviceUpgradeTestStringSet(t, port1["multicast_router_networkconf_ids"]); !slicesEqualAsSets(
		got, []string{"net-x", "net-y"},
	) {
		t.Errorf(
			"port_override[index=1].multicast_router_networkconf_ids = %v, want {net-x, net-y}",
			got,
		)
	}
	// tagged_networkconf_ids is absent from the raw JSON entirely (matching
	// every real prior write -- devicePortOverrideEncode never writes this
	// attribute, unwired end to end, not an SDK limitation) and must
	// reconcile to a null Set, not an error.
	if !port1["tagged_networkconf_ids"].IsNull() {
		t.Errorf(
			"port_override[index=1].tagged_networkconf_ids = %v, want null",
			port1["tagged_networkconf_ids"],
		)
	}

	port2, ok := byIndex[2]
	if !ok {
		t.Fatal("port_override entry with index 2 did not survive the upgrade")
	}
	// A declared-but-empty collection must survive as empty, not null: it
	// means "explicitly managing zero networks", which is different from
	// never having declared the attribute at all.
	if port2["excluded_networkconf_ids"].IsNull() {
		t.Error("port_override[index=2].excluded_networkconf_ids = null, want an empty (non-null) set")
	}
	if got := deviceUpgradeTestStringSet(t, port2["excluded_networkconf_ids"]); len(got) != 0 {
		t.Errorf("port_override[index=2].excluded_networkconf_ids = %v, want empty", got)
	}
	// multicast_router_networkconf_ids is absent from index 2's JSON at all
	// (the fabricated absent-attribute case): must reconcile to null, not
	// panic or error.
	if !port2["multicast_router_networkconf_ids"].IsNull() {
		t.Errorf(
			"port_override[index=2].multicast_router_networkconf_ids = %v, want null",
			port2["multicast_router_networkconf_ids"],
		)
	}
}

// deviceUpgradeTestStringSet decodes a Set(String) tftypes.Value into a
// plain []string for comparison, tolerating a null value as empty.
func deviceUpgradeTestStringSet(t *testing.T, v tftypes.Value) []string {
	t.Helper()
	if v.IsNull() {
		return nil
	}
	var elems []tftypes.Value
	if err := v.As(&elems); err != nil {
		t.Fatalf("decoding set value: %v", err)
	}
	out := make([]string, 0, len(elems))
	for _, e := range elems {
		var s string
		if err := e.As(&s); err != nil {
			t.Fatalf("decoding set element: %v", err)
		}
		out = append(out, s)
	}
	return out
}

// slicesEqualAsSets compares two string slices ignoring order, since a Set
// carries no order contract for this comparison to depend on.
func slicesEqualAsSets(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	counts := make(map[string]int, len(a))
	for _, v := range a {
		counts[v]++
	}
	for _, v := range b {
		counts[v]--
	}
	for _, n := range counts {
		if n != 0 {
			return false
		}
	}
	return true
}

func Test_dropAssistedRoaming(t *testing.T) {
	state := map[string]any{
		"mac": "00:11:22:33:44:55",
		"radio_table": []any{
			map[string]any{
				"radio":                    "na",
				"channel":                  "36",
				"assisted_roaming_enabled": true,
				"assisted_roaming_rssi":    -75,
			},
			map[string]any{"radio": "ng"},
		},
	}

	dropAssistedRoaming(state)

	radios, ok := state["radio_table"].([]any)
	if !ok {
		t.Fatalf("radio_table is %T, want []any", state["radio_table"])
	}
	first, ok := radios[0].(map[string]any)
	if !ok {
		t.Fatalf("radio_table[0] is %T, want map[string]any", radios[0])
	}
	for _, k := range []string{"assisted_roaming_enabled", "assisted_roaming_rssi"} {
		if _, present := first[k]; present {
			t.Errorf("%s survived the rewrite", k)
		}
	}
	if first["channel"] != "36" {
		t.Errorf("channel = %v, want 36", first["channel"])
	}
	if len(radios) != 2 {
		t.Errorf("radio_table has %d entries, want 2", len(radios))
	}
}

// Test_dropAssistedRoaming_nonRadioState covers state shapes the rewrite must
// pass through untouched rather than panic on.
func Test_dropAssistedRoaming_nonRadioState(t *testing.T) {
	for name, state := range map[string]map[string]any{
		"no radio_table":       {"mac": "00:11:22:33:44:55"},
		"null radio_table":     {"radio_table": nil},
		"radio_table not list": {"radio_table": "unexpected"},
		"entry not an object":  {"radio_table": []any{"unexpected"}},
	} {
		t.Run(name, func(t *testing.T) {
			before := map[string]any{}
			for k, v := range state {
				before[k] = v
			}
			dropAssistedRoaming(state)
			if !reflect.DeepEqual(before, state) {
				t.Errorf("dropAssistedRoaming rewrote %s: got %v, want unchanged %v", name, state, before)
			}
		})
	}
}

// Test_deviceResource_UpgradeState_dropsAssistedRoaming guards the v1 -> v2
// migration end to end. UniFi Network 10.x dropped the per-radio assisted
// roaming setting, so the attributes left the schema — but prior state still
// carries them, and cty rejects attributes the schema no longer declares.
// Without a registered upgrader every refresh fails with "unsupported
// attribute". Acceptance tests cannot reach this path, since they never start
// from old state.
func Test_deviceResource_UpgradeState_dropsAssistedRoaming(t *testing.T) {
	ctx := context.Background()
	r := newDeviceKitResource()

	priorState := `{
		"id": "abc123",
		"site": "default",
		"mac": "00:11:22:33:44:55",
		"radio_table": [
			{
				"radio": "na",
				"channel": "36",
				"tx_power_mode": "auto",
				"assisted_roaming_enabled": true,
				"assisted_roaming_rssi": -75
			},
			{
				"radio": "ng",
				"channel": "6",
				"assisted_roaming_enabled": false
			}
		]
	}`

	upgrader, ok := r.UpgradeState(ctx)[1]
	if !ok {
		t.Fatal("no v1 state upgrader registered")
	}

	resp := &fwresource.UpgradeStateResponse{}
	upgrader.StateUpgrader(ctx, fwresource.UpgradeStateRequest{
		RawState: &tfprotov6.RawState{JSON: []byte(priorState)},
	}, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("upgrade produced diagnostics: %v", resp.Diagnostics)
	}
	if resp.DynamicValue == nil {
		t.Fatal("upgrade produced no value")
	}

	// Decoding against the current schema is the assertion: it fails outright if
	// the removed attributes survived.
	var schemaResp fwresource.SchemaResponse
	r.Schema(ctx, fwresource.SchemaRequest{}, &schemaResp)
	schemaType := schemaResp.Schema.Type().TerraformType(ctx)

	val, err := resp.DynamicValue.Unmarshal(schemaType)
	if err != nil {
		t.Fatalf("upgraded state does not decode against the v2 schema: %v", err)
	}

	// The rest of the radio table must survive the rewrite.
	var obj map[string]tftypes.Value
	if err := val.As(&obj); err != nil {
		t.Fatalf("decoding upgraded object: %v", err)
	}
	var radios []tftypes.Value
	if err := obj["radio_table"].As(&radios); err != nil {
		t.Fatalf("decoding radio_table: %v", err)
	}
	if len(radios) != 2 {
		t.Fatalf("radio_table has %d entries, want 2", len(radios))
	}
	var first map[string]tftypes.Value
	if err := radios[0].As(&first); err != nil {
		t.Fatalf("decoding radio_table[0]: %v", err)
	}
	var channel *string
	if err := first["channel"].As(&channel); err != nil {
		t.Fatalf("decoding radio_table[0].channel: %v", err)
	}
	if channel == nil || *channel != "36" {
		t.Errorf("radio_table[0].channel = %v, want 36", channel)
	}
}

// A device is adopted if the create succeeded, whatever the write answered
// with: BeforeSend adopts and waits for Connected before anything is
// written, so a response still carrying the pre-adoption flag would
// otherwise record the device as unadopted immediately after the apply
// that adopted it -- and the next plan would proceed to adopt it again.
func Test_deviceRestoreCreateValues_preservesSuccessfulAdoption(t *testing.T) {
	created := &unifi.Device{Adopted: false, Name: ""}
	deviceRestoreCreateValues(created, &unifi.Device{Name: "planned-name"})

	if !created.Adopted {
		t.Error("Adopted = false, want true after a successful create")
	}
	if created.Name != "planned-name" {
		t.Errorf("Name = %q, want the name that was just written", created.Name)
	}
}

// A controller-chosen name is kept when the request did not carry one.
func Test_deviceRestoreCreateValues_keepsControllerNameWhenUnset(t *testing.T) {
	created := &unifi.Device{Name: "from-controller"}
	deviceRestoreCreateValues(created, &unifi.Device{Name: ""})

	if created.Name != "from-controller" {
		t.Errorf("Name = %q, want the controller's own", created.Name)
	}
}

func Test_cleanMAC(t *testing.T) {
	type args struct {
		mac string
	}
	tests := []struct {
		name string
		args args
		want string
	}{
		{
			name: "converts dashes to colons and lowercases",
			args: args{mac: "AA-BB-CC-DD-EE-FF"},
			want: "aa:bb:cc:dd:ee:ff",
		},
		{
			name: "already lowercase colons unchanged",
			args: args{mac: "aa:bb:cc:dd:ee:ff"},
			want: "aa:bb:cc:dd:ee:ff",
		},
		{
			name: "uppercase colons lowercased",
			args: args{mac: "AA:BB:CC:DD:EE:FF"},
			want: "aa:bb:cc:dd:ee:ff",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := cleanMAC(tt.args.mac); got != tt.want {
				t.Errorf("cleanMAC() = %v, want %v", got, tt.want)
			}
		})
	}
}

// Test_devicePortOverrideDeclaredFields_matchesEveryModeledMember pins the
// full set of wire names devicePortOverrideDeclaredFields can produce,
// against every attribute portOverrideAttrTypes models except index (it
// addresses the entry rather than configuring it) and
// tagged_networkconf_ids (declarable-but-inert, never written -- see the
// comment on devicePortOverrideEncode). devicePortOverrideDeclaredFields has
// no compiler-checked link to that attribute set -- it is a second,
// hand-written pass over the same model -- so this is what catches a member
// added to one without the other.
func Test_devicePortOverrideDeclaredFields_matchesEveryModeledMember(t *testing.T) {
	members, d := types.ListValue(types.Int64Type, []attr.Value{types.Int64Value(9)})
	if d.HasError() {
		t.Fatalf("building aggregate_members: %v", d)
	}
	idSet, d := types.SetValue(types.StringType, []attr.Value{types.StringValue("net-1")})
	if d.HasError() {
		t.Fatalf("building a network id set: %v", d)
	}
	macList, d := types.ListValue(types.StringType, []attr.Value{types.StringValue("aa:bb:cc:dd:ee:ff")})
	if d.HasError() {
		t.Fatalf("building port_security_mac_address: %v", d)
	}

	model := portOverrideModel{
		Index:                      types.Int64Value(1), // excluded regardless -- see the test comment
		Name:                       types.StringValue("x"),
		PortProfileID:              types.StringValue("prof"),
		OpMode:                     types.StringValue("aggregate"), // non-default, so it counts as declared
		PoeMode:                    types.StringValue("auto"),
		AggregateMembers:           members,
		Autoneg:                    types.BoolValue(false), // null-vs-false is the point; not-null is what matters here
		Dot1XCtrl:                  types.StringValue("auto"),
		Dot1XIDleTimeout:           timetypes.NewGoDurationValue(30 * time.Second),
		EgressRateLimitKbps:        types.Int64Value(100),
		EgressRateLimitKbpsEnabled: types.BoolValue(true),
		ExcludedNetworkIDs:         idSet,
		FecMode:                    types.StringValue("default"),
		FlowControlEnabled:         types.BoolValue(true),
		Forward:                    types.StringValue("all"),
		FullDuplex:                 types.BoolValue(true),
		Isolation:                  types.BoolValue(false),
		LldpmedEnabled:             types.BoolValue(true),
		LldpmedNotifyEnabled:       types.BoolValue(true),
		MirrorPortIDX:              types.Int64Value(2),
		MulticastRouterNetworkIDs:  idSet,
		NativeNetworkID:            types.StringValue("net"),
		PortKeepaliveEnabled:       types.BoolValue(true),
		PortSecurityEnabled:        types.BoolValue(true),
		PortSecurityMACAddress:     macList,
		PriorityQueue1Level:        types.Int64Value(1),
		PriorityQueue2Level:        types.Int64Value(2),
		PriorityQueue3Level:        types.Int64Value(3),
		PriorityQueue4Level:        types.Int64Value(4),
		SettingPreference:          types.StringValue("auto"),
		Speed:                      types.Int64Value(1000),
		StormctrlBroadcastEnabled:  types.BoolValue(true),
		StormctrlBroadcastLevel:    types.Int64Value(10),
		StormctrlBroadcastRate:     types.Int64Value(10),
		StormctrlMcastEnabled:      types.BoolValue(true),
		StormctrlMcastLevel:        types.Int64Value(10),
		StormctrlMcastRate:         types.Int64Value(10),
		StormctrlType:              types.StringValue("level"),
		StormctrlUcastEnabled:      types.BoolValue(true),
		StormctrlUcastLevel:        types.Int64Value(10),
		StormctrlUcastRate:         types.Int64Value(10),
		StpPortMode:                types.BoolValue(true),
		TaggedNetworkIDs:           idSet, // must never appear in the result
		TaggedVLANMgmt:             types.StringValue("auto"),
		VoiceNetworkID:             types.StringValue("voice-net"),
	}

	got := devicePortOverrideDeclaredFields(model)
	sort.Strings(got)

	// portOverrideAttrTypes' keys are tfsdk attribute names, which match the
	// wire name for every attribute except this one -- SDK field
	// PortProfileID carries json:"portconf_id".
	want := make([]string, 0, len(portOverrideAttrTypes()))
	for name := range portOverrideAttrTypes() {
		switch name {
		case "index", "tagged_networkconf_ids":
			continue
		case "port_profile_id":
			name = "portconf_id"
		}
		want = append(want, name)
	}
	sort.Strings(want)

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("declared fields = %v,\nwant             %v", got, want)
	}
}

func Test_portOverrideAttrTypes(t *testing.T) {
	tests := []struct {
		name string
		want map[string]attr.Type
	}{
		{
			name: "returns non-empty map with expected keys",
			want: portOverrideAttrTypes(),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := portOverrideAttrTypes(); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("portOverrideAttrTypes() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_configNetworkAttrTypes(t *testing.T) {
	tests := []struct {
		name string
		want map[string]attr.Type
	}{
		{
			name: "returns correct attribute types",
			want: map[string]attr.Type{
				"type":            types.StringType,
				"ip":              types.StringType,
				"netmask":         types.StringType,
				"gateway":         types.StringType,
				"dns1":            types.StringType,
				"dns2":            types.StringType,
				"dnssuffix":       types.StringType,
				"bonding_enabled": types.BoolType,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := configNetworkAttrTypes(); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("configNetworkAttrTypes() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_radioTableAttrTypes(t *testing.T) {
	tests := []struct {
		name string
		want map[string]attr.Type
	}{
		{
			name: "returns correct attribute types",
			want: radioTableAttrTypes(),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := radioTableAttrTypes(); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("radioTableAttrTypes() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_outletOverrideAttrTypes(t *testing.T) {
	tests := []struct {
		name string
		want map[string]attr.Type
	}{
		{
			name: "returns correct attribute types",
			want: map[string]attr.Type{
				"index":         types.Int64Type,
				"name":          types.StringType,
				"relay_state":   types.BoolType,
				"cycle_enabled": types.BoolType,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := outletOverrideAttrTypes(); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("outletOverrideAttrTypes() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_deviceResource_ListResourceConfigSchema(t *testing.T) {
	type args struct {
		in0  context.Context
		in1  fwlist.ListResourceSchemaRequest
		resp *fwlist.ListResourceSchemaResponse
	}
	tests := []struct {
		name string
		r    *deviceKitResource
		args args
	}{
		{
			name: "returns list schema",
			r:    newDeviceKitResource(),
			args: args{
				in0:  context.Background(),
				in1:  fwlist.ListResourceSchemaRequest{},
				resp: &fwlist.ListResourceSchemaResponse{},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.r.ListResourceConfigSchema(tt.args.in0, tt.args.in1, tt.args.resp)
		})
	}
}

// deviceFakePortOverridesController is a minimal in-memory stand-in for the
// controller endpoints deviceKitBeforeSend and Backend.UpdateFields touch,
// so a test can drive the real Resource.Update sequence
// (deviceRunUpdateSequence, below) and inspect the actual outgoing PUT
// bodies and the resulting stored state -- not a mock of the SDK, an
// httptest server underneath it.
//
// Its PUT handling reproduces the one controller behaviour measured
// against a live controller: a top-level key present in the body replaces
// the whole port_overrides array, entry by entry and member by member; a
// top-level key the body omits leaves the stored device untouched. That is
// also exactly what unifi.UpdateDevicePortOverrides's own doc comment says the
// real controller does.
type deviceFakePortOverridesController struct {
	mu    sync.Mutex
	id    string
	mac   string
	typ   string
	ports map[string]map[string]any // port_idx (as a string key) -> raw stored fields
	puts  []json.RawMessage
}

// deviceFakeID and deviceFakeMAC are the same for every scenario below --
// none of them cares about the device's own identity, only about what
// happens to its port overrides.
const (
	deviceFakeID  = "dev1"
	deviceFakeMAC = "00:00:00:00:00:01"
)

func newDeviceFakePortOverridesController(
	ports map[string]map[string]any,
) *deviceFakePortOverridesController {
	return &deviceFakePortOverridesController{id: deviceFakeID, mac: deviceFakeMAC, typ: "usw", ports: ports}
}

func (f *deviceFakePortOverridesController) port(idx int64) (map[string]any, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	entry, ok := f.ports[strconv.FormatInt(idx, 10)]
	return entry, ok
}

func (f *deviceFakePortOverridesController) puttedBodies() []json.RawMessage {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]json.RawMessage, len(f.puts))
	copy(out, f.puts)
	return out
}

func (f *deviceFakePortOverridesController) deviceJSON() map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	keys := make([]string, 0, len(f.ports))
	for k := range f.ports {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		a, _ := strconv.Atoi(keys[i])
		b, _ := strconv.Atoi(keys[j])
		return a < b
	})
	entries := make([]map[string]any, 0, len(keys))
	for _, k := range keys {
		entries = append(entries, f.ports[k])
	}
	return map[string]any{
		"_id": f.id, "mac": f.mac, "type": f.typ, "adopted": true,
		"port_overrides": entries,
	}
}

func (f *deviceFakePortOverridesController) server(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/":
			w.WriteHeader(http.StatusOK) // new-style API probe
			return
		case r.URL.Path == "/proxy/network/status":
			_, _ = w.Write([]byte(`{"meta":{"server_version":"8.0.0"}}`))
			return
		case r.Method == http.MethodPut:
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Fatalf("reading PUT body: %v", err)
			}
			f.mu.Lock()
			f.puts = append(f.puts, json.RawMessage(append([]byte(nil), body...)))
			f.mu.Unlock()

			// A pointer, not a plain slice: nil after decode means the key
			// was absent, [] means it was present but empty -- the same
			// distinction overlayKeyedEntries/maskedBody rely on.
			var decoded struct {
				PortOverrides *[]map[string]any `json:"port_overrides"`
			}
			if err := json.Unmarshal(body, &decoded); err != nil {
				t.Fatalf("decode PUT body: %v\nbody: %s", err, body)
			}
			if decoded.PortOverrides != nil {
				next := make(map[string]map[string]any, len(*decoded.PortOverrides))
				for _, entry := range *decoded.PortOverrides {
					idx, _ := entry["port_idx"].(float64)
					next[strconv.Itoa(int(idx))] = entry
				}
				f.mu.Lock()
				f.ports = next
				f.mu.Unlock()
			}
			_, _ = w.Write([]byte(`{"meta":{"rc":"ok"},"data":[]}`))
			return
		default:
			raw, err := json.Marshal(map[string]any{
				"meta": map[string]any{"rc": "ok"},
				"data": []any{f.deviceJSON()},
			})
			if err != nil {
				t.Fatalf("marshalling fake device: %v", err)
			}
			_, _ = w.Write(raw)
		}
	}))
}

// devicePortOverrideFromPUTs decodes every captured PUT body's
// port_overrides array (if it has one) and returns the last entry seen for
// portIdx across all of them, in call order -- a later call's view of the
// port is the one that ends up stored. ok is false if no captured PUT ever
// named that port at all.
func devicePortOverrideFromPUTs(t *testing.T, puts []json.RawMessage, portIdx float64) (map[string]any, bool) {
	t.Helper()
	var found map[string]any
	var ok bool
	for _, raw := range puts {
		var body struct {
			PortOverrides []map[string]any `json:"port_overrides"`
		}
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Fatalf("decoding a captured PUT body: %v\nbody: %s", err, raw)
		}
		for _, entry := range body.PortOverrides {
			if idx, _ := entry["port_idx"].(float64); idx == portIdx {
				found, ok = entry, true
			}
		}
	}
	return found, ok
}

// anyPUTNamesPortOverrides reports whether any captured PUT body's top level
// carries a port_overrides key at all, present or empty.
func anyPUTNamesPortOverrides(t *testing.T, puts []json.RawMessage) bool {
	t.Helper()
	for _, raw := range puts {
		var body map[string]json.RawMessage
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Fatalf("decoding a captured PUT body: %v\nbody: %s", err, raw)
		}
		if _, ok := body["port_overrides"]; ok {
			return true
		}
	}
	return false
}

// deviceRunUpdateSequence drives resourcekit's own Update() sequence for
// deviceKitSpec by hand -- ApplyPlanToState, WireFields from the raw plan,
// ToSDK from the merged state, SetID, Prefetch, BeforeSend, then
// Backend.UpdateFields -- in the exact order internal/resourcekit.
// Resource.Update follows, without the tfprotov6 request/response plumbing
// around it. Mirrors vpnServerRunDNSMaskScenario's pattern
// (vpn_server_resource_test.go).
//
// config is threaded through separately from plan because that is what
// Resource.Update itself does, and what deviceKitBeforeSend now depends on:
// BeforeSend needs the practitioner's raw config specifically to tell
// "declared" from "computed but currently holds a value", which plan/state
// cannot (see devicePortOverridesDeclaredFromConfig).
func deviceRunUpdateSequence(
	t *testing.T, client *unifi.ApiClient, prior, plan, config deviceKitModel,
) {
	t.Helper()
	ctx := context.Background()
	spec := deviceKitSpec()
	spec.Backend = deviceKitBackend(client)
	spec.BeforeSend = deviceKitBeforeSend(client)

	state := prior
	spec.ApplyPlanToState(&plan, &state)
	site := state.Site.ValueString()

	fields, err := spec.WireFields(&plan)
	if err != nil {
		t.Fatalf("WireFields: %v", err)
	}

	sdk, diags := spec.ToSDK(ctx, &state)
	if diags.HasError() {
		t.Fatalf("ToSDK: %v", diags)
	}
	spec.Backend.SetID(sdk, state.ID.ValueString())

	prefetched, prefetchDiags := spec.Prefetch(ctx, site)
	if prefetchDiags.HasError() {
		t.Fatalf("Prefetch: %v", prefetchDiags)
	}

	diags = spec.BeforeSend(ctx, &config, &state, prior, sdk, prefetched)
	if diags.HasError() {
		t.Fatalf("BeforeSend: %v", diags)
	}

	if _, err := spec.Backend.UpdateFields(ctx, site, sdk, fields...); err != nil {
		t.Fatalf("Backend.UpdateFields: %v", err)
	}
}

// deviceUpdateSequenceModel builds the minimum deviceKitModel a device
// update needs to run: an id and mac matching the fake controller, and a
// site.
func deviceUpdateSequenceModel(id, mac string) deviceKitModel {
	return deviceKitModel{
		ID:   types.StringValue(id),
		Site: types.StringValue("default"),
		MAC:  hwtypes.NewMACAddressValue(mac),
		Type: types.StringValue("usw"),
	}
}

// Test_deviceUpdate_configuredPortKeepsUnmodelledMember pins a fixed
// defect: a port with a port_override block that declares only "name"
// must not disturb eee_enabled, a member unifi.DevicePortOverrides
// carries but portOverrideModel never models at all. Against today's
// whole-array encode this is RED -- the merged struct
// devicePortOverrideEncode builds starts from the Go zero value for every
// member it does not know about, and that zero (an omitted, "false" bool)
// replaces whatever the controller held once the whole array goes back out.
func Test_deviceUpdate_configuredPortKeepsUnmodelledMember(t *testing.T) {
	fake := newDeviceFakePortOverridesController(map[string]map[string]any{
		"1": {"port_idx": float64(1), "name": "uplink", "poe_mode": "auto", "eee_enabled": true},
	})
	srv := fake.server(t)
	defer srv.Close()
	client, err := unifi.New(context.Background(), &unifi.Config{BaseURL: srv.URL, APIKey: "test-key"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	config := portOverrideSetWith(t, map[string]attr.Value{
		"index": types.Int64Value(1),
		"name":  types.StringValue("renamed"),
	})

	prior := deviceUpdateSequenceModel(fake.id, fake.mac)
	plan := prior
	plan.PortOverride = config
	planConfig := prior
	planConfig.PortOverride = config

	deviceRunUpdateSequence(t, client, prior, plan, planConfig)

	got, ok := fake.port(1)
	if !ok {
		t.Fatalf("port 1 is gone from the controller's stored overrides")
	}
	if got["name"] != "renamed" {
		t.Errorf("port 1 name = %v, want renamed (the write itself did not take effect)", got["name"])
	}
	if eee, ok := got["eee_enabled"]; !ok || eee != true {
		t.Errorf("port 1 eee_enabled = %v (present=%v), want true -- "+
			"a member this provider never models must survive a write that "+
			"only declares name", eee, ok)
	}
}

// Test_deviceUpdate_unconfiguredPortKeepsItsMembers pins a fixed defect: a
// port with no port_override block at all -- port 2 here, while port 1 is
// declared -- must keep every member exactly as it was, an explicit
// "false" (autoneg) included. Port 2's raw entry legitimately still
// travels on the wire under the fix (updateDevicePortOverridesGrouped
// overlays onto the full stored array so the controller's whole-array
// replace doesn't drop it), so the invariant this checks is its content,
// not its absence.
//
// Against today's whole-array encode this is RED: current.PortOverrides is
// decoded straight from a GET, so port 2's Autoneg field holds a real Go
// false either way -- but a Go bool has no "unset" state, so re-marshalling
// that struct for the resend drops the member via omitempty regardless of
// whether the controller's own JSON had it explicitly. The fake controller
// then stores whatever it was sent, key and all -- an explicit false becomes
// no key at all.
func Test_deviceUpdate_unconfiguredPortKeepsItsMembers(t *testing.T) {
	fake := newDeviceFakePortOverridesController(map[string]map[string]any{
		"1": {"port_idx": float64(1), "name": "uplink", "poe_mode": "auto"},
		"2": {"port_idx": float64(2), "name": "desk", "poe_mode": "auto", "autoneg": false},
	})
	srv := fake.server(t)
	defer srv.Close()
	client, err := unifi.New(context.Background(), &unifi.Config{BaseURL: srv.URL, APIKey: "test-key"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	config := portOverrideSetWith(t, map[string]attr.Value{
		"index": types.Int64Value(1),
		"name":  types.StringValue("renamed"),
	})

	prior := deviceUpdateSequenceModel(fake.id, fake.mac)
	plan := prior
	plan.PortOverride = config
	planConfig := prior
	planConfig.PortOverride = config

	deviceRunUpdateSequence(t, client, prior, plan, planConfig)

	got, ok := fake.port(2)
	if !ok {
		t.Fatalf("port 2 is gone from the controller's stored overrides")
	}
	if got["autoneg"] != false || got["name"] != "desk" || got["poe_mode"] != "auto" {
		t.Errorf("port 2 = %+v, want it byte-for-byte unchanged", got)
	}
}

// Test_deviceUpdate_explicitFalseReachesTheWire pins a fixed defect: a
// port_override block that sets autoneg = false must send that false
// explicitly, not omit the member. Against today's whole-array encode
// this is RED: a Go bool has no "unset" state, so model.Autoneg.ValueBool()
// on a null attribute and on an explicit false both produce the same zero
// value, and the JSON encoder's omitempty then drops it either way -- an
// explicit false is wire-indistinguishable from never having named the
// member at all.
func Test_deviceUpdate_explicitFalseReachesTheWire(t *testing.T) {
	fake := newDeviceFakePortOverridesController(map[string]map[string]any{
		"1": {"port_idx": float64(1), "name": "uplink", "poe_mode": "auto", "autoneg": true},
	})
	srv := fake.server(t)
	defer srv.Close()
	client, err := unifi.New(context.Background(), &unifi.Config{BaseURL: srv.URL, APIKey: "test-key"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	config := portOverrideSetWith(t, map[string]attr.Value{
		"index":   types.Int64Value(1),
		"autoneg": types.BoolValue(false),
	})

	prior := deviceUpdateSequenceModel(fake.id, fake.mac)
	plan := prior
	plan.PortOverride = config
	planConfig := prior
	planConfig.PortOverride = config

	deviceRunUpdateSequence(t, client, prior, plan, planConfig)

	entry, ok := devicePortOverrideFromPUTs(t, fake.puttedBodies(), 1)
	if !ok {
		t.Fatalf("port 1 never appeared in any outgoing port-overrides write")
	}
	if v, present := entry["autoneg"]; !present || v != false {
		t.Errorf("port 1's wire entry autoneg = %v (present=%v), want the "+
			"literal key \"autoneg\":false -- an explicit false must not be "+
			"an omission", v, present)
	}

	got, ok := fake.port(1)
	if !ok {
		t.Fatalf("port 1 is gone from the controller's stored overrides")
	}
	if got["autoneg"] != false {
		t.Errorf("port 1 autoneg = %v after the update, want false to have taken effect", got["autoneg"])
	}
}

// Test_deviceUpdate_noDeclaredPorts_sendsNoPortArray pins a fixed defect: an
// update that declares no port_override block at all must never put
// port_overrides on the wire, in any call. Against today's whole-array
// encode this is RED: port_overrides is unconditionally in AlwaysWire, so
// the general masked write carries the whole current port array on every
// update regardless of what config says.
func Test_deviceUpdate_noDeclaredPorts_sendsNoPortArray(t *testing.T) {
	fake := newDeviceFakePortOverridesController(map[string]map[string]any{
		"1": {"port_idx": float64(1), "name": "uplink", "poe_mode": "auto"},
	})
	srv := fake.server(t)
	defer srv.Close()
	client, err := unifi.New(context.Background(), &unifi.Config{BaseURL: srv.URL, APIKey: "test-key"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	prior := deviceUpdateSequenceModel(fake.id, fake.mac)
	plan := prior
	plan.Name = types.StringValue("renamed-device") // something to write, unrelated to ports
	config := prior
	config.Name = plan.Name

	deviceRunUpdateSequence(t, client, prior, plan, config)

	if anyPUTNamesPortOverrides(t, fake.puttedBodies()) {
		t.Errorf("an update declaring no port_override block put port_overrides " +
			"on the wire; it must send no port array at all")
	}
	got, ok := fake.port(1)
	if !ok || got["name"] != "uplink" {
		t.Errorf("port 1 = %+v (present=%v), want it untouched", got, ok)
	}
}

// portOverrideSetWithMany builds a port_override set with one object per
// overrides map, in the order given -- portOverrideSetWith's multi-port
// sibling, for scenarios that need more than one declared block in the same
// config.
func portOverrideSetWithMany(t *testing.T, many ...map[string]attr.Value) types.Set {
	t.Helper()
	objs := make([]attr.Value, 0, len(many))
	for _, overrides := range many {
		attrs := nullPortOverrideAttrValues()
		for k, v := range overrides {
			attrs[k] = v
		}
		obj, d := types.ObjectValue(portOverrideAttrTypes(), attrs)
		if d.HasError() {
			t.Fatalf("building port override object: %v", d)
		}
		objs = append(objs, obj)
	}
	set, d := types.SetValue(types.ObjectType{AttrTypes: portOverrideAttrTypes()}, objs)
	if d.HasError() {
		t.Fatalf("building port override set: %v", d)
	}
	return set
}

// Test_deviceUpdate_emptyDeclaredBlockIsANoOpNotAnAbort pins a fix flagged in
// review: a port_override block whose config sets no writable member --
// port_override { index = 2 } here, alongside a real change on port 1 --
// used to produce a group with an empty member mask.
// UpdateDevicePortOverrides refuses an empty mask outright, and since
// groupPortOverridesByFieldSet's groups are issued in first-appearance
// order, port 1's write had already gone out by the time port 2's group
// errored -- one harmless-looking block both failed the whole apply and
// left the device half-written. A block that declares nothing means
// "manage this port, change nothing", so it must be dropped before
// grouping rather than sent.
func Test_deviceUpdate_emptyDeclaredBlockIsANoOpNotAnAbort(t *testing.T) {
	fake := newDeviceFakePortOverridesController(map[string]map[string]any{
		"1": {"port_idx": float64(1), "name": "uplink", "poe_mode": "auto"},
		"2": {"port_idx": float64(2), "name": "desk", "poe_mode": "auto"},
	})
	srv := fake.server(t)
	defer srv.Close()
	client, err := unifi.New(context.Background(), &unifi.Config{BaseURL: srv.URL, APIKey: "test-key"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	config := portOverrideSetWithMany(t,
		map[string]attr.Value{"index": types.Int64Value(1), "name": types.StringValue("renamed")},
		map[string]attr.Value{"index": types.Int64Value(2)}, // declares nothing but its own index
	)

	prior := deviceUpdateSequenceModel(fake.id, fake.mac)
	plan := prior
	plan.PortOverride = config
	planConfig := prior
	planConfig.PortOverride = config

	// deviceRunUpdateSequence itself t.Fatalf's on any error from BeforeSend
	// or Backend.UpdateFields, so reaching the assertions below already
	// proves the empty block did not abort the apply.
	deviceRunUpdateSequence(t, client, prior, plan, planConfig)

	got1, ok := fake.port(1)
	if !ok || got1["name"] != "renamed" {
		t.Errorf("port 1 = %+v (present=%v), want name=renamed -- the real "+
			"change must still take effect", got1, ok)
	}
	got2, ok := fake.port(2)
	if !ok {
		t.Fatalf("port 2 is gone from the controller's stored overrides")
	}
	if got2["name"] != "desk" || got2["poe_mode"] != "auto" {
		t.Errorf("port 2 = %+v, want it byte-for-byte unchanged -- a block "+
			"naming only index declares nothing", got2)
	}
}

// Test_deviceUpdate_unknownNameIsNotDeclared pins a fix flagged in review:
// declare() used to key off IsNull() alone, so an unknown config value --
// one Terraform has not resolved yet -- counted as declared.
// model.Name.ValueString() on an unknown value returns "", the same as on
// null, so the member joined the mask and the masked write forced the
// empty string over whatever the controller held -- the exact
// silent-overwrite shape a declared-fields mask exists to prevent, reached
// through an unknown instead of a whole-array resend. Since name is the only thing
// this block would have declared, the fixed behaviour is that nothing
// about the block reaches the wire at all.
func Test_deviceUpdate_unknownNameIsNotDeclared(t *testing.T) {
	fake := newDeviceFakePortOverridesController(map[string]map[string]any{
		"1": {"port_idx": float64(1), "name": "uplink", "poe_mode": "auto"},
	})
	srv := fake.server(t)
	defer srv.Close()
	client, err := unifi.New(context.Background(), &unifi.Config{BaseURL: srv.URL, APIKey: "test-key"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	config := portOverrideSetWith(t, map[string]attr.Value{
		"index": types.Int64Value(1),
		"name":  types.StringUnknown(),
	})

	prior := deviceUpdateSequenceModel(fake.id, fake.mac)
	plan := prior
	plan.PortOverride = config
	planConfig := prior
	planConfig.PortOverride = config

	deviceRunUpdateSequence(t, client, prior, plan, planConfig)

	if anyPUTNamesPortOverrides(t, fake.puttedBodies()) {
		t.Errorf("a block whose only attribute is unknown put port_overrides " +
			"on the wire; an unknown value is not declared")
	}
	got, ok := fake.port(1)
	if !ok || got["name"] != "uplink" {
		t.Errorf("port 1 = %+v (present=%v), want name=uplink unchanged -- "+
			"an unknown name must not overwrite it with an empty string", got, ok)
	}
}

// Test_deviceUpdate_unknownIndexDoesNotAddressPortZero pins a fix flagged in
// review: Int64.ValueInt64Pointer() returns a pointer to 0 for an unknown
// value, not nil -- worse than the unknown-member case, because an
// unresolved index used to address a real port (port 0) instead of
// dropping out of the write.
func Test_deviceUpdate_unknownIndexDoesNotAddressPortZero(t *testing.T) {
	fake := newDeviceFakePortOverridesController(map[string]map[string]any{
		"1": {"port_idx": float64(1), "name": "uplink"},
	})
	srv := fake.server(t)
	defer srv.Close()
	client, err := unifi.New(context.Background(), &unifi.Config{BaseURL: srv.URL, APIKey: "test-key"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	config := portOverrideSetWith(t, map[string]attr.Value{
		"index": types.Int64Unknown(),
		"name":  types.StringValue("renamed"),
	})

	prior := deviceUpdateSequenceModel(fake.id, fake.mac)
	plan := prior
	plan.PortOverride = config
	planConfig := prior
	planConfig.PortOverride = config

	deviceRunUpdateSequence(t, client, prior, plan, planConfig)

	if anyPUTNamesPortOverrides(t, fake.puttedBodies()) {
		t.Errorf("a block with an unresolved index put port_overrides on the " +
			"wire; it cannot address a real port yet")
	}
	if _, ok := fake.port(0); ok {
		t.Errorf("port 0 appeared in the controller's stored overrides; " +
			"an unknown index must not fall back to port 0")
	}
	got, ok := fake.port(1)
	if !ok || got["name"] != "uplink" {
		t.Errorf("port 1 = %+v (present=%v), want it untouched", got, ok)
	}
}

// Test_deviceUpdate_computedMemberNullInConfigIsNotDeclared pins the premise
// the whole task turns on: declaredness has to come from config, not from
// plan/state. autoneg is Optional+Computed in the schema, so a practitioner
// who never wrote it can still see a non-null value in plan/state -- filled
// in from a prior read, never from what they typed. Reading that value
// instead of config's null would send it on every future update regardless
// of what the practitioner wrote, silently.
//
// Port 1's config sets name (a real, intentional change) and leaves autoneg
// null; plan carries a *different* autoneg than what the controller
// actually holds, standing in for a prior read's carried-forward value, so
// a wrong read is directly observable: the stored autoneg would flip to
// plan's value instead of staying put.
func Test_deviceUpdate_computedMemberNullInConfigIsNotDeclared(t *testing.T) {
	fake := newDeviceFakePortOverridesController(map[string]map[string]any{
		"1": {"port_idx": float64(1), "name": "uplink", "poe_mode": "auto", "autoneg": true},
	})
	srv := fake.server(t)
	defer srv.Close()
	client, err := unifi.New(context.Background(), &unifi.Config{BaseURL: srv.URL, APIKey: "test-key"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	planPortOverride := portOverrideSetWith(t, map[string]attr.Value{
		"index":   types.Int64Value(1),
		"name":    types.StringValue("renamed"),
		"autoneg": types.BoolValue(false), // stands in for a Computed carry-forward, not config
	})
	configPortOverride := portOverrideSetWith(t, map[string]attr.Value{
		"index": types.Int64Value(1),
		"name":  types.StringValue("renamed"),
		// autoneg left null: the practitioner did not write it.
	})

	prior := deviceUpdateSequenceModel(fake.id, fake.mac)
	plan := prior
	plan.PortOverride = planPortOverride
	planConfig := prior
	planConfig.PortOverride = configPortOverride

	deviceRunUpdateSequence(t, client, prior, plan, planConfig)

	// The overlay carries the whole stored entry forward, autoneg included --
	// that is how updateDevicePortOverridesGrouped leaves an undeclared
	// member alone (Test_deviceUpdate_unconfiguredPortKeepsItsMembers pins
	// exactly that), so autoneg is expected to appear in the wire entry
	// regardless of whether it was declared. What distinguishes "declared
	// from config" (excluded, so the overlay's own stored true survives)
	// from "declared from plan" (an explicit false, force-written) is the
	// *value*, not presence.
	entry, ok := devicePortOverrideFromPUTs(t, fake.puttedBodies(), 1)
	if !ok {
		t.Fatalf("port 1 never appeared in any outgoing port-overrides write")
	}
	if v, present := entry["autoneg"]; present && v != true {
		t.Errorf("port 1's wire entry autoneg = %v, want true (the stored "+
			"value passed through unmodified) -- a false here means the mask "+
			"read autoneg from plan/state instead of config", v)
	}

	got, ok := fake.port(1)
	if !ok {
		t.Fatalf("port 1 is gone from the controller's stored overrides")
	}
	if got["name"] != "renamed" {
		t.Errorf("port 1 name = %v, want renamed (the real change must take effect)", got["name"])
	}
	if got["autoneg"] != true {
		t.Errorf("port 1 autoneg = %v, want true unchanged -- reading autoneg "+
			"from plan instead of config would have sent plan's false", got["autoneg"])
	}
}
