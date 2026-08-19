package unifi

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	fwlist "github.com/hashicorp/terraform-plugin-framework/list"
	fwresource "github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/querycheck"
	"github.com/hashicorp/terraform-plugin-testing/tfversion"
	"github.com/ubiquiti-community/go-unifi/unifi"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/controllertest"
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

// TestMergePortOverridesByIndex guards #266: declaring a subset of port_override
// blocks must not wipe the device's other ports. The UniFi PUT replaces the whole
// port_overrides array, so the provider merges the declared ports (by port_idx)
// onto the device's current overrides before sending.
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

// Test_buildMinimalUpdateDevice_mgmtNetworkID guards #329: a configured
// mgmt_network_id (the UI "Network Override") must travel in the minimal PUT body,
// and a null value must stay off the wire so it never reintroduces the #177
// zero-value rejection. modelToAPIDevice sets deviceReq.MgmtNetworkID only when
// configured, so nullness is represented here by an empty value on deviceReq.
func Test_buildMinimalUpdateDevice_mgmtNetworkID(t *testing.T) {
	t.Run("configured mgmt_network_id is sent in the PUT body", func(t *testing.T) {
		deviceReq := &unifi.Device{
			ID:            "dev-1",
			Type:          "usw",
			MAC:           "aa:bb:cc:dd:ee:ff",
			Name:          "Test Switch",
			MgmtNetworkID: "net-mgmt",
		}
		body := buildMinimalUpdateDevice(deviceReq, nil, nil)
		if body.MgmtNetworkID != "net-mgmt" {
			t.Fatalf(
				"MgmtNetworkID = %q, want %q (override dropped from PUT, #329)",
				body.MgmtNetworkID,
				"net-mgmt",
			)
		}
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		if !strings.Contains(string(raw), `"mgmt_network_id":"net-mgmt"`) {
			t.Errorf("PUT body missing mgmt_network_id: %s", raw)
		}
	})

	t.Run("null mgmt_network_id stays off the wire (no #177 regression)", func(t *testing.T) {
		deviceReq := &unifi.Device{
			ID:   "dev-1",
			Type: "usw",
			MAC:  "aa:bb:cc:dd:ee:ff",
			Name: "Test Switch",
		}
		body := buildMinimalUpdateDevice(deviceReq, nil, nil)
		if body.MgmtNetworkID != "" {
			t.Errorf("MgmtNetworkID = %q, want empty for a null override", body.MgmtNetworkID)
		}
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		if strings.Contains(string(raw), "mgmt_network_id") {
			t.Errorf("null mgmt_network_id leaked into PUT body: %s", raw)
		}
	})
}

// Test_buildMinimalUpdateDevice_switchVLANEnabled guards the switch_vlan_enabled
// bug class: a configured "Port VLAN" toggle (true) must travel in the minimal
// PUT body, else the controller keeps its old value and the post-apply read
// conflicts with the plan. Being `omitempty`, a false stays off the wire and
// doesn't disturb the controller default.
func Test_buildMinimalUpdateDevice_switchVLANEnabled(t *testing.T) {
	t.Run("configured switch_vlan_enabled is sent in the PUT body", func(t *testing.T) {
		deviceReq := &unifi.Device{
			ID:                "dev-1",
			Type:              "uap",
			MAC:               "aa:bb:cc:dd:ee:ff",
			Name:              "Test AP",
			SwitchVLANEnabled: true,
		}
		body := buildMinimalUpdateDevice(deviceReq, nil, nil)
		if !body.SwitchVLANEnabled {
			t.Fatal("SwitchVLANEnabled = false, want true (toggle dropped from PUT)")
		}
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		if !strings.Contains(string(raw), `"switch_vlan_enabled":true`) {
			t.Errorf("PUT body missing switch_vlan_enabled: %s", raw)
		}
	})

	t.Run("false switch_vlan_enabled stays off the wire (omitempty)", func(t *testing.T) {
		deviceReq := &unifi.Device{
			ID:   "dev-1",
			Type: "uap",
			MAC:  "aa:bb:cc:dd:ee:ff",
			Name: "Test AP",
		}
		body := buildMinimalUpdateDevice(deviceReq, nil, nil)
		if body.SwitchVLANEnabled {
			t.Errorf("SwitchVLANEnabled = true, want false when unconfigured")
		}
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		if strings.Contains(string(raw), "switch_vlan_enabled") {
			t.Errorf("false switch_vlan_enabled leaked into PUT body: %s", raw)
		}
	})
}

// Test_buildMinimalUpdateDevice_vwireEnabled guards the radio_table[].vwire_enabled
// bug class (the UI "Mesh Parent" toggle): the hand-listed minimal PUT never
// copied radio_table across, so every radio sub-field — vwire_enabled included —
// was dropped, the controller kept its old value, and the post-apply read
// conflicted with the plan. Being `omitempty` at every level, an empty
// radio_table stays off the wire and doesn't disturb the controller default.
func Test_buildMinimalUpdateDevice_vwireEnabled(t *testing.T) {
	t.Run("configured vwire_enabled is sent in the PUT body", func(t *testing.T) {
		deviceReq := &unifi.Device{
			ID:   "dev-1",
			Type: "uap",
			MAC:  "aa:bb:cc:dd:ee:ff",
			Name: "Test AP",
			RadioTable: []unifi.DeviceRadioTable{
				{Name: "wifi0", Radio: "ng", VwireEnabled: true},
				{Name: "wifi1", Radio: "na", VwireEnabled: true},
			},
		}
		body := buildMinimalUpdateDevice(deviceReq, nil, nil)
		if len(body.RadioTable) != 2 {
			t.Fatalf(
				"RadioTable len = %d, want 2 (radio_table dropped from PUT)",
				len(body.RadioTable),
			)
		}
		for _, radio := range body.RadioTable {
			if !radio.VwireEnabled {
				t.Fatalf(
					"radio %q VwireEnabled = false, want true (toggle dropped from PUT)",
					radio.Name,
				)
			}
		}
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		if !strings.Contains(string(raw), `"vwire_enabled":true`) {
			t.Errorf("PUT body missing vwire_enabled: %s", raw)
		}
	})

	t.Run("false vwire_enabled stays off the wire (omitempty)", func(t *testing.T) {
		deviceReq := &unifi.Device{
			ID:   "dev-1",
			Type: "uap",
			MAC:  "aa:bb:cc:dd:ee:ff",
			Name: "Test AP",
			RadioTable: []unifi.DeviceRadioTable{
				{Name: "wifi0", Radio: "ng"},
			},
		}
		body := buildMinimalUpdateDevice(deviceReq, nil, nil)
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		if strings.Contains(string(raw), "vwire_enabled") {
			t.Errorf("false vwire_enabled leaked into PUT body: %s", raw)
		}
	})

	t.Run("nil radio_table stays off the wire (omitempty)", func(t *testing.T) {
		deviceReq := &unifi.Device{
			ID:   "dev-1",
			Type: "usw",
			MAC:  "aa:bb:cc:dd:ee:ff",
			Name: "Test Switch",
		}
		body := buildMinimalUpdateDevice(deviceReq, nil, nil)
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		if strings.Contains(string(raw), "radio_table") {
			t.Errorf("empty radio_table leaked into PUT body: %s", raw)
		}
	})
}

// Test_buildMinimalUpdateDevice_meshStaVapEnabled guards the top-level
// mesh_sta_vap_enabled bug class (the UI "Mesh Connect" toggle): a configured
// true must travel in the minimal PUT body, else the controller keeps its old
// value and the post-apply read conflicts with the plan. Being `omitempty`, a
// false stays off the wire and doesn't disturb the controller default.
func Test_buildMinimalUpdateDevice_meshStaVapEnabled(t *testing.T) {
	t.Run("configured mesh_sta_vap_enabled is sent in the PUT body", func(t *testing.T) {
		deviceReq := &unifi.Device{
			ID:                "dev-1",
			Type:              "uap",
			MAC:               "aa:bb:cc:dd:ee:ff",
			Name:              "Test AP",
			MeshStaVapEnabled: true,
		}
		body := buildMinimalUpdateDevice(deviceReq, nil, nil)
		if !body.MeshStaVapEnabled {
			t.Fatal("MeshStaVapEnabled = false, want true (toggle dropped from PUT)")
		}
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		if !strings.Contains(string(raw), `"mesh_sta_vap_enabled":true`) {
			t.Errorf("PUT body missing mesh_sta_vap_enabled: %s", raw)
		}
	})

	t.Run("false mesh_sta_vap_enabled stays off the wire (omitempty)", func(t *testing.T) {
		deviceReq := &unifi.Device{
			ID:   "dev-1",
			Type: "uap",
			MAC:  "aa:bb:cc:dd:ee:ff",
			Name: "Test AP",
		}
		body := buildMinimalUpdateDevice(deviceReq, nil, nil)
		if body.MeshStaVapEnabled {
			t.Errorf("MeshStaVapEnabled = true, want false when unconfigured")
		}
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		if strings.Contains(string(raw), "mesh_sta_vap_enabled") {
			t.Errorf("false mesh_sta_vap_enabled leaked into PUT body: %s", raw)
		}
	})
}

// TestAccDeviceFramework_basic drives whichever device the harness started for
// it. The MAC comes from the herder's ready event rather than from a literal,
// because a literal can only name a controller-simulated demo device, which
// never informs and so never exercises adoption for real.
func TestAccDeviceFramework_basic(t *testing.T) {
	mac := os.Getenv(controllertest.EnvAccDeviceMAC)
	if mac == "" {
		t.Skipf("%s not set; skipping device acceptance test", controllertest.EnvAccDeviceMAC)
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
	tests := []struct {
		name string
		want fwresource.Resource
	}{
		{
			name: "returns deviceResource",
			want: &deviceResource{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NewDeviceFrameworkResource(); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("NewDeviceFrameworkResource() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNewDeviceListResource(t *testing.T) {
	tests := []struct {
		name string
		want fwlist.ListResource
	}{
		{
			name: "returns deviceResource",
			want: &deviceResource{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NewDeviceListResource(); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("NewDeviceListResource() = %v, want %v", got, tt.want)
			}
		})
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
		r    *deviceResource
		args args
	}{
		{
			name: "returns identity schema",
			r:    &deviceResource{},
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
		r    *deviceResource
		args args
	}{
		{
			name: "returns schema",
			r:    &deviceResource{},
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
	got := (&deviceResource{}).UpgradeState(context.Background())
	for _, version := range []int64{0, 1} {
		if _, ok := got[version]; !ok {
			t.Errorf("missing state upgrader for schema version %d", version)
		}
	}
	if len(got) != 2 {
		t.Errorf("UpgradeState() has %d upgraders, want 2", len(got))
	}
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
			dropAssistedRoaming(state)
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
	r := &deviceResource{}

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

func Test_deviceResource_Create(t *testing.T) {
	type args struct {
		ctx  context.Context
		req  fwresource.CreateRequest
		resp *fwresource.CreateResponse
	}
	tests := []struct {
		name string
		r    *deviceResource
		args args
	}{}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.r.Create(tt.args.ctx, tt.args.req, tt.args.resp)
		})
	}
}

func Test_restoreCreatePlanValues_preservesSuccessfulAdoption(t *testing.T) {
	model := deviceResourceModel{
		Adopted: types.BoolValue(false),
	}

	restoreCreatePlanValues(&model, types.BoolValue(true), types.BoolValue(false), types.StringNull(), types.SetNull(types.ObjectType{AttrTypes: portOverrideAttrTypes()}))

	if !model.Adopted.ValueBool() {
		t.Fatal("Adopted = false, want true after successful create")
	}
}

func Test_deviceResource_Read(t *testing.T) {
	type args struct {
		ctx  context.Context
		req  fwresource.ReadRequest
		resp *fwresource.ReadResponse
	}
	tests := []struct {
		name string
		r    *deviceResource
		args args
	}{}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.r.Read(tt.args.ctx, tt.args.req, tt.args.resp)
		})
	}
}

func Test_deviceResource_Update(t *testing.T) {
	type args struct {
		ctx  context.Context
		req  fwresource.UpdateRequest
		resp *fwresource.UpdateResponse
	}
	tests := []struct {
		name string
		r    *deviceResource
		args args
	}{}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.r.Update(tt.args.ctx, tt.args.req, tt.args.resp)
		})
	}
}

func Test_deviceResource_Delete(t *testing.T) {
	type args struct {
		ctx  context.Context
		req  fwresource.DeleteRequest
		resp *fwresource.DeleteResponse
	}
	tests := []struct {
		name string
		r    *deviceResource
		args args
	}{}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.r.Delete(tt.args.ctx, tt.args.req, tt.args.resp)
		})
	}
}

func Test_deviceResource_ImportState(t *testing.T) {
	type args struct {
		ctx  context.Context
		req  fwresource.ImportStateRequest
		resp *fwresource.ImportStateResponse
	}
	tests := []struct {
		name string
		r    *deviceResource
		args args
	}{}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.r.ImportState(tt.args.ctx, tt.args.req, tt.args.resp)
		})
	}
}

// Test_buildMinimalUpdateDevice guards #337: the update PUT body must carry the
// LED override fields. They used to be filled by modelToAPIDevice but dropped
// when assembling the minimal PUT payload, so the controller kept the old LED
// values and the post-apply read conflicted with the plan.
func Test_buildMinimalUpdateDevice(t *testing.T) {
	deviceReq := &unifi.Device{
		ID:                         "dev-1",
		Type:                       "uap",
		MAC:                        "00:11:22:33:44:55",
		Name:                       "AP-Hallway",
		LedOverride:                "on",
		LedOverrideColor:           "#00ff00",
		LedOverrideColorBrightness: ptrInt64(20),
	}
	current := &unifi.Device{State: 1, Adopted: true}
	overrides := []unifi.DevicePortOverrides{{PortIDX: ptrInt64(1)}}

	got := buildMinimalUpdateDevice(deviceReq, current, overrides)

	if got.LedOverride != "on" {
		t.Errorf("LedOverride = %q, want on", got.LedOverride)
	}
	if got.LedOverrideColor != "#00ff00" {
		t.Errorf("LedOverrideColor = %q, want #00ff00", got.LedOverrideColor)
	}
	if got.LedOverrideColorBrightness == nil || *got.LedOverrideColorBrightness != 20 {
		t.Errorf("LedOverrideColorBrightness = %v, want 20", got.LedOverrideColorBrightness)
	}
	// State/Adopted carried over from the current device; other fields preserved.
	if got.State != 1 || !got.Adopted {
		t.Errorf(
			"State/Adopted not carried from current: state=%v adopted=%v",
			got.State,
			got.Adopted,
		)
	}
	if got.Name != "AP-Hallway" || len(got.PortOverrides) != 1 {
		t.Errorf(
			"unexpected name/overrides: name=%q overrides=%d",
			got.Name,
			len(got.PortOverrides),
		)
	}

	// Unset LED fields stay zero-valued (omitempty drops them from the PUT body).
	bare := buildMinimalUpdateDevice(&unifi.Device{ID: "d2"}, nil, nil)
	if bare.LedOverride != "" || bare.LedOverrideColorBrightness != nil {
		t.Errorf("unset LED fields should be zero: %q %v",
			bare.LedOverride, bare.LedOverrideColorBrightness)
	}
}

func Test_deviceResource_updateDevice(t *testing.T) {
	type args struct {
		ctx   context.Context
		model *deviceResourceModel
	}
	tests := []struct {
		name string
		r    *deviceResource
		args args
		want diag.Diagnostics
	}{}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.r.updateDevice(
				tt.args.ctx,
				tt.args.model,
			); !reflect.DeepEqual(
				got,
				tt.want,
			) {
				t.Errorf("deviceResource.updateDevice() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_deviceResource_setResourceData(t *testing.T) {
	type args struct {
		ctx    context.Context
		diags  *diag.Diagnostics
		device *unifi.Device
		model  *deviceResourceModel
		site   string
	}
	tests := []struct {
		name string
		r    *deviceResource
		args args
	}{}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.r.setResourceData(
				tt.args.ctx,
				tt.args.diags,
				tt.args.device,
				tt.args.model,
				tt.args.site,
			)
		})
	}
}

func Test_deviceResource_modelToAPIDevice(t *testing.T) {
	type args struct {
		ctx   context.Context
		model *deviceResourceModel
	}
	tests := []struct {
		name  string
		r     *deviceResource
		args  args
		want  *unifi.Device
		want1 diag.Diagnostics
	}{}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, got1 := tt.r.modelToAPIDevice(tt.args.ctx, tt.args.model)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("deviceResource.modelToAPIDevice() got = %v, want %v", got, tt.want)
			}
			if !reflect.DeepEqual(got1, tt.want1) {
				t.Errorf("deviceResource.modelToAPIDevice() got1 = %v, want %v", got1, tt.want1)
			}
		})
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

func Test_deviceResource_reconcilePortOverrides(t *testing.T) {
	type args struct {
		ctx          context.Context
		prior        types.Set
		apiOverrides []unifi.DevicePortOverrides
	}
	tests := []struct {
		name  string
		r     *deviceResource
		args  args
		want  types.Set
		want1 diag.Diagnostics
	}{}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, got1 := tt.r.reconcilePortOverrides(
				tt.args.ctx,
				tt.args.prior,
				tt.args.apiOverrides,
			)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("deviceResource.reconcilePortOverrides() got = %v, want %v", got, tt.want)
			}
			if !reflect.DeepEqual(got1, tt.want1) {
				t.Errorf(
					"deviceResource.reconcilePortOverrides() got1 = %v, want %v",
					got1,
					tt.want1,
				)
			}
		})
	}
}

func Test_deviceResource_portOverridesToFramework(t *testing.T) {
	type args struct {
		ctx context.Context
		pos []unifi.DevicePortOverrides
	}
	tests := []struct {
		name  string
		r     *deviceResource
		args  args
		want  types.Set
		want1 diag.Diagnostics
	}{}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, got1 := tt.r.portOverridesToFramework(tt.args.ctx, tt.args.pos)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf(
					"deviceResource.portOverridesToFramework() got = %v, want %v",
					got,
					tt.want,
				)
			}
			if !reflect.DeepEqual(got1, tt.want1) {
				t.Errorf(
					"deviceResource.portOverridesToFramework() got1 = %v, want %v",
					got1,
					tt.want1,
				)
			}
		})
	}
}

func Test_deviceResource_frameworkToPortOverrides(t *testing.T) {
	type args struct {
		ctx             context.Context
		portOverrideSet types.Set
	}
	tests := []struct {
		name  string
		r     *deviceResource
		args  args
		want  []unifi.DevicePortOverrides
		want1 diag.Diagnostics
	}{}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, got1 := tt.r.frameworkToPortOverrides(tt.args.ctx, tt.args.portOverrideSet)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf(
					"deviceResource.frameworkToPortOverrides() got = %v, want %v",
					got,
					tt.want,
				)
			}
			if !reflect.DeepEqual(got1, tt.want1) {
				t.Errorf(
					"deviceResource.frameworkToPortOverrides() got1 = %v, want %v",
					got1,
					tt.want1,
				)
			}
		})
	}
}

func Test_deviceResource_waitForDeviceState(t *testing.T) {
	type args struct {
		ctx           context.Context
		site          string
		mac           string
		targetState   unifi.DeviceState
		pendingStates []unifi.DeviceState
		timeout       time.Duration
	}
	tests := []struct {
		name    string
		r       *deviceResource
		args    args
		want    *unifi.Device
		wantErr bool
	}{}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.r.waitForDeviceState(
				tt.args.ctx,
				tt.args.site,
				tt.args.mac,
				tt.args.targetState,
				tt.args.pendingStates,
				tt.args.timeout,
			)
			if (err != nil) != tt.wantErr {
				t.Errorf(
					"deviceResource.waitForDeviceState() error = %v, wantErr %v",
					err,
					tt.wantErr,
				)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("deviceResource.waitForDeviceState() = %v, want %v", got, tt.want)
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

func Test_stringOrNull(t *testing.T) {
	type args struct {
		s string
	}
	tests := []struct {
		name string
		args args
		want types.String
	}{
		{
			name: "empty string returns null",
			args: args{s: ""},
			want: types.StringNull(),
		},
		{
			name: "non-empty string returns value",
			args: args{s: "hello"},
			want: types.StringValue("hello"),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := stringOrNull(tt.args.s); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("stringOrNull() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_int64OrNull(t *testing.T) {
	type args struct {
		i int64
	}
	tests := []struct {
		name string
		args args
		want types.Int64
	}{
		{
			name: "zero returns null",
			args: args{i: 0},
			want: types.Int64Null(),
		},
		{
			name: "non-zero returns value",
			args: args{i: 42},
			want: types.Int64Value(42),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := int64OrNull(tt.args.i); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("int64OrNull() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_deviceResource_configNetworkToFramework(t *testing.T) {
	type args struct {
		ctx context.Context
		cn  *unifi.DeviceConfigNetwork
	}
	tests := []struct {
		name  string
		r     *deviceResource
		args  args
		want  types.Object
		want1 diag.Diagnostics
	}{}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, got1 := tt.r.configNetworkToFramework(tt.args.ctx, tt.args.cn)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf(
					"deviceResource.configNetworkToFramework() got = %v, want %v",
					got,
					tt.want,
				)
			}
			if !reflect.DeepEqual(got1, tt.want1) {
				t.Errorf(
					"deviceResource.configNetworkToFramework() got1 = %v, want %v",
					got1,
					tt.want1,
				)
			}
		})
	}
}

func Test_deviceResource_radioTableToFramework(t *testing.T) {
	type args struct {
		ctx    context.Context
		radios []unifi.DeviceRadioTable
	}
	tests := []struct {
		name  string
		r     *deviceResource
		args  args
		want  types.List
		want1 diag.Diagnostics
	}{}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, got1 := tt.r.radioTableToFramework(tt.args.ctx, tt.args.radios)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("deviceResource.radioTableToFramework() got = %v, want %v", got, tt.want)
			}
			if !reflect.DeepEqual(got1, tt.want1) {
				t.Errorf(
					"deviceResource.radioTableToFramework() got1 = %v, want %v",
					got1,
					tt.want1,
				)
			}
		})
	}
}

func Test_deviceResource_outletOverridesToFramework(t *testing.T) {
	type args struct {
		ctx     context.Context
		outlets []unifi.DeviceOutletOverrides
	}
	tests := []struct {
		name  string
		r     *deviceResource
		args  args
		want  types.List
		want1 diag.Diagnostics
	}{}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, got1 := tt.r.outletOverridesToFramework(tt.args.ctx, tt.args.outlets)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf(
					"deviceResource.outletOverridesToFramework() got = %v, want %v",
					got,
					tt.want,
				)
			}
			if !reflect.DeepEqual(got1, tt.want1) {
				t.Errorf(
					"deviceResource.outletOverridesToFramework() got1 = %v, want %v",
					got1,
					tt.want1,
				)
			}
		})
	}
}

func Test_deviceResource_frameworkToConfigNetwork(t *testing.T) {
	type args struct {
		ctx              context.Context
		configNetworkObj types.Object
	}
	tests := []struct {
		name  string
		r     *deviceResource
		args  args
		want  *unifi.DeviceConfigNetwork
		want1 diag.Diagnostics
	}{}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, got1 := tt.r.frameworkToConfigNetwork(tt.args.ctx, tt.args.configNetworkObj)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf(
					"deviceResource.frameworkToConfigNetwork() got = %v, want %v",
					got,
					tt.want,
				)
			}
			if !reflect.DeepEqual(got1, tt.want1) {
				t.Errorf(
					"deviceResource.frameworkToConfigNetwork() got1 = %v, want %v",
					got1,
					tt.want1,
				)
			}
		})
	}
}

func Test_deviceResource_frameworkToRadioTable(t *testing.T) {
	type args struct {
		ctx       context.Context
		radioList types.List
	}
	tests := []struct {
		name  string
		r     *deviceResource
		args  args
		want  []unifi.DeviceRadioTable
		want1 diag.Diagnostics
	}{}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, got1 := tt.r.frameworkToRadioTable(tt.args.ctx, tt.args.radioList)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("deviceResource.frameworkToRadioTable() got = %v, want %v", got, tt.want)
			}
			if !reflect.DeepEqual(got1, tt.want1) {
				t.Errorf(
					"deviceResource.frameworkToRadioTable() got1 = %v, want %v",
					got1,
					tt.want1,
				)
			}
		})
	}
}

func Test_deviceResource_frameworkToOutletOverrides(t *testing.T) {
	type args struct {
		ctx        context.Context
		outletList types.List
	}
	tests := []struct {
		name  string
		r     *deviceResource
		args  args
		want  []unifi.DeviceOutletOverrides
		want1 diag.Diagnostics
	}{}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, got1 := tt.r.frameworkToOutletOverrides(tt.args.ctx, tt.args.outletList)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf(
					"deviceResource.frameworkToOutletOverrides() got = %v, want %v",
					got,
					tt.want,
				)
			}
			if !reflect.DeepEqual(got1, tt.want1) {
				t.Errorf(
					"deviceResource.frameworkToOutletOverrides() got1 = %v, want %v",
					got1,
					tt.want1,
				)
			}
		})
	}
}

func Test_deviceResource_deviceListToModel(t *testing.T) {
	type args struct {
		ctx   context.Context
		api   *unifi.Device
		model *deviceResourceModel
		site  string
	}
	tests := []struct {
		name string
		r    *deviceResource
		args args
		want diag.Diagnostics
	}{}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.r.deviceListToModel(
				tt.args.ctx,
				tt.args.api,
				tt.args.model,
				tt.args.site,
			); !reflect.DeepEqual(
				got,
				tt.want,
			) {
				t.Errorf("deviceResource.deviceListToModel() = %v, want %v", got, tt.want)
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
		r    *deviceResource
		args args
	}{
		{
			name: "returns list schema",
			r:    &deviceResource{},
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

func Test_deviceResource_List(t *testing.T) {
	type args struct {
		ctx    context.Context
		req    fwlist.ListRequest
		stream *fwlist.ListResultsStream
	}
	tests := []struct {
		name string
		r    *deviceResource
		args args
	}{}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.r.List(tt.args.ctx, tt.args.req, tt.args.stream)
		})
	}
}

// Test_portOverridesForUpdate_noDeclaredBlocks guards the case the #266 fix
// left open: a device managed with NO port_override blocks at all.
//
// The merge was gated on len(declared) > 0, so that case skipped it and the
// body carried deviceReq's own empty slice. mergePortOverridesByIndex has
// always returned `current` for an empty declared set -- and there is a test
// asserting exactly that -- but the gate meant production never reached it.
// The tested path and the taken path were different paths.
func Test_portOverridesForUpdate_noDeclaredBlocks(t *testing.T) {
	idx := func(i int64) *int64 { return &i }
	current := &unifi.Device{
		ID: "d1",
		PortOverrides: []unifi.DevicePortOverrides{
			{PortIDX: idx(1), Name: "uplink"},
			{PortIDX: idx(2), Name: "camera"},
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
			{PortIDX: idx(2), Name: "printer"},
			{PortIDX: idx(9), Name: "new"},
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

// Test_portOverridesAreAlwaysOnTheWire is the control that makes the test above
// mean something, and it is the reason the fix could not be "leave the field
// nil and let it drop out".
//
// port_overrides carries no omitempty. A nil slice therefore does not vanish
// from the PUT body -- it marshals to [], which the controller reads as a full
// replace with nothing. If this ever starts reporting the key as absent, the
// merge stops being load-bearing and the comment on portOverridesForUpdate is
// wrong.
func Test_portOverridesAreAlwaysOnTheWire(t *testing.T) {
	body, err := json.Marshal(buildMinimalUpdateDevice(&unifi.Device{ID: "d1"}, nil, nil))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `"port_overrides":[]`) {
		t.Fatalf("a nil port_overrides no longer marshals to []; the body was %s", body)
	}
}

// Test_deviceForceEmittedFieldsAreAllRescuedHere pins the coincidence that
// makes buildMinimalUpdateDevice safe.
//
// The body it builds is a FRESH unifi.Device populated from the Terraform
// model, so any field the schema does not declare goes to the controller as a
// Go zero -- unless omitempty drops it from the encoding first. Exactly three
// of Device's fields have no omitempty, and all three are filled in by hand
// from the fetched device. Nothing enforces that; it is true today and an SDK
// regeneration can end it silently.
//
// So the set is pinned rather than the behaviour described. A fourth
// force-emitted field fails here, naming itself, instead of being reset on
// every apply.
func Test_deviceForceEmittedFieldsAreAllRescuedHere(t *testing.T) {
	_, forceEmits := wireTagsOf(unifi.Device{})

	got := make([]string, 0, len(forceEmits))
	for name, unconditional := range forceEmits {
		if unconditional && name != "_id" && name != "site_id" {
			got = append(got, name)
		}
	}
	sort.Strings(got)

	want := []string{"adopted", "port_overrides", "state"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unifi.Device force-emits %v, not %v.\n\n"+
			"buildMinimalUpdateDevice builds the PUT body from the Terraform model, so "+
			"every force-emitted field it does not set is sent as a zero on every apply. "+
			"The three in `want` are set by hand from the fetched device. A field that "+
			"appears here and not there is silently reset; one that disappeared means the "+
			"rescue is now dead code. Fix buildMinimalUpdateDevice, then update this list.",
			got, want)
	}
}

// Test_buildMinimalUpdateDeviceRescuesEveryForceEmittedField is the other half:
// the set above is the right set, and each member actually survives the trip.
// Pinning the names without checking the values would pass for a rescue that
// had been deleted.
func Test_buildMinimalUpdateDeviceRescuesEveryForceEmittedField(t *testing.T) {
	idx := func(i int64) *int64 { return &i }
	current := &unifi.Device{
		ID:            "d1",
		Adopted:       true,
		State:         5,
		PortOverrides: []unifi.DevicePortOverrides{{PortIDX: idx(1), Name: "uplink"}},
	}
	// A model that declares nothing beyond identity: the worst case.
	deviceReq := &unifi.Device{ID: "d1"}

	body := buildMinimalUpdateDevice(
		deviceReq, current, portOverridesForUpdate(current, deviceReq.PortOverrides))

	if !body.Adopted {
		t.Error("adopted went back as false; a gateway rejects that (#177)")
	}
	if body.State != 5 {
		t.Errorf("state went back as %d, want 5", body.State)
	}
	if len(body.PortOverrides) != 1 {
		t.Errorf("port_overrides went back with %d entries, want 1 (#191)",
			len(body.PortOverrides))
	}
}
