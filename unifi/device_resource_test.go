package unifi

import (
	"context"
	"fmt"
	"math/big"
	"os"
	"reflect"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	fwlist "github.com/hashicorp/terraform-plugin-framework/list"
	fwresource "github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/querycheck"
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

// TestMergePortOverridesByIndex guards a rule: declaring a subset of
// port_override blocks must not wipe the device's other ports. The UniFi
// PUT replaces the whole port_overrides array, so the provider merges the
// declared ports (by port_idx) onto the device's current overrides before
// sending.
func TestMergePortOverridesByIndex(t *testing.T) {
	current := []unifi.DevicePortOverrides{
		{PortIDX: ptrInt64(3), NATiveNetworkID: "vlan-a"},
		{PortIDX: ptrInt64(4), NATiveNetworkID: "vlan-b"},
		{PortIDX: ptrInt64(5), NATiveNetworkID: "vlan-c"},
	}

	t.Run("subset replaces only its port, keeps the rest", func(t *testing.T) {
		declared := []unifi.DevicePortOverrides{
			{PortIDX: ptrInt64(5), NATiveNetworkID: "vlan-z"},
		}
		got := mergePortOverridesByIndex(current, declared)
		byIdx := indexOverrides(got)
		if len(got) != 3 {
			t.Fatalf("merged length = %d, want 3 (ports 3,4 must survive): %+v", len(got), got)
		}
		if byIdx[3].NATiveNetworkID != "vlan-a" || byIdx[4].NATiveNetworkID != "vlan-b" {
			t.Errorf("undeclared ports were altered: %+v", got)
		}
		if byIdx[5].NATiveNetworkID != "vlan-z" {
			t.Errorf("declared port 5 = %q, want vlan-z", byIdx[5].NATiveNetworkID)
		}
	})

	t.Run("declared new port is appended", func(t *testing.T) {
		declared := []unifi.DevicePortOverrides{
			{PortIDX: ptrInt64(7), NATiveNetworkID: "vlan-new"},
		}
		got := mergePortOverridesByIndex(current, declared)
		byIdx := indexOverrides(got)
		if len(got) != 4 {
			t.Fatalf("merged length = %d, want 4: %+v", len(got), got)
		}
		if byIdx[7].NATiveNetworkID != "vlan-new" {
			t.Errorf("new port 7 not appended: %+v", got)
		}
	})

	t.Run("no declared overrides returns current unchanged", func(t *testing.T) {
		got := mergePortOverridesByIndex(current, nil)
		if len(got) != 3 {
			t.Errorf("merged length = %d, want 3", len(got))
		}
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

func Test_mergePortOverridesByIndex(t *testing.T) {
	type args struct {
		current  []unifi.DevicePortOverrides
		declared []unifi.DevicePortOverrides
	}
	tests := []struct {
		name string
		args args
		want []unifi.DevicePortOverrides
	}{
		{
			name: "nil current and nil declared returns nil",
			args: args{current: nil, declared: nil},
			want: nil,
		},
		{
			name: "nil current with declared returns declared",
			args: args{
				current: nil,
				declared: []unifi.DevicePortOverrides{
					{PortIDX: ptrInt64(1), NATiveNetworkID: "net-a"},
				},
			},
			want: []unifi.DevicePortOverrides{
				{PortIDX: ptrInt64(1), NATiveNetworkID: "net-a"},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := mergePortOverridesByIndex(
				tt.args.current,
				tt.args.declared,
			); !reflect.DeepEqual(
				got,
				tt.want,
			) {
				t.Errorf("mergePortOverridesByIndex() = %v, want %v", got, tt.want)
			}
		})
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

// Test_portOverridesForUpdate_noDeclaredBlocks guards a case the merge fix
// left open: a device managed with no port_override blocks at all.
//
// The merge was gated on len(declared) > 0, so that case skipped it and
// the body carried deviceReq's own empty slice -- mergePortOverridesByIndex
// has always returned `current` for an empty declared set, and there's a
// test asserting exactly that, but the gate meant production never
// reached it. The tested path and the taken path were different paths.
func Test_portOverridesForUpdate_noDeclaredBlocks(t *testing.T) {
	current := &unifi.Device{
		ID: "d1",
		PortOverrides: []unifi.DevicePortOverrides{
			{PortIDX: ptrInt64(1), Name: "uplink"},
			{PortIDX: ptrInt64(2), Name: "camera"},
		},
	}

	t.Run("nil declared keeps every controller override", func(t *testing.T) {
		got := portOverridesForUpdate(current, nil)
		if len(got) != 2 {
			t.Fatalf("kept %d override(s), want 2; an update that declares no "+
				"port_override block must not clear the ones the controller holds", len(got))
		}
	})

	t.Run("empty non-nil declared keeps them too", func(t *testing.T) {
		// A SetNestedBlock with no elements converts to an allocated empty
		// slice rather than nil, so both spellings have to be covered.
		got := portOverridesForUpdate(current, []unifi.DevicePortOverrides{})
		if len(got) != 2 {
			t.Fatalf("kept %d override(s), want 2", len(got))
		}
	})

	t.Run("declared blocks still merge by index", func(t *testing.T) {
		got := portOverridesForUpdate(current, []unifi.DevicePortOverrides{
			{PortIDX: ptrInt64(2), Name: "printer"},
			{PortIDX: ptrInt64(9), Name: "new"},
		})
		if len(got) != 3 {
			t.Fatalf("merged to %d, want 3 (1 kept, 2 replaced, 9 appended)", len(got))
		}
		for _, po := range got {
			if po.PortIDX != nil && *po.PortIDX == 2 && po.Name != "printer" {
				t.Errorf("port 2 was not replaced by the declared block: %+v", po)
			}
		}
	})

	t.Run("no current device leaves the declared set alone", func(t *testing.T) {
		if got := portOverridesForUpdate(nil, nil); got != nil {
			t.Errorf("with no fetched device there is nothing to preserve, got %v", got)
		}
	})
}
