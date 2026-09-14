package unifi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/ubiquiti-community/go-unifi/unifi"
)

// fullRadioModel is a radioTableModel with every member set to a known,
// in-range value -- the "everything declared" case the declared-fields pin
// needs.
func fullRadioModel() radioTableModel {
	return radioTableModel{
		Radio:                 types.StringValue("na"),
		Channel:               types.StringValue("36"),
		Ht:                    types.Int64Value(80),
		TxPower:               types.StringValue("auto"),
		TxPowerMode:           types.StringValue("auto"),
		MinRssiEnabled:        types.BoolValue(true),
		MinRssi:               types.Int64Value(-80),
		AntennaGain:           types.Int64Value(1),
		AntennaID:             types.Int64Value(1),
		Dfs:                   types.BoolValue(true),
		HardNoiseFloorEnabled: types.BoolValue(true),
		LoadbalanceEnabled:    types.BoolValue(true),
		Maxsta:                types.Int64Value(100),
		Name:                  types.StringValue("wifi-na"),
		SensLevel:             types.Int64Value(-70),
		SensLevelEnabled:      types.BoolValue(true),
		VwireEnabled:          types.BoolValue(true),
	}
}

// Test_deviceRadioTableDeclaredFields_matchesEveryModeledMember pins that the
// presence mask covers every modeled member except the two it deliberately
// omits: name (the key, carried by the SDK writer) and dfs (discarded on
// write, per the capture). A member added to the model or the encode without
// a matching declare() here would silently stop being writable, and this is
// what turns that into a failing test.
func Test_deviceRadioTableDeclaredFields_matchesEveryModeledMember(t *testing.T) {
	model := fullRadioModel()
	radio := unifi.DeviceRadioTable{
		Ht:        ptrInt64(80),
		MinRssi:   ptrInt64(-80),
		Maxsta:    ptrInt64(100),
		SensLevel: ptrInt64(-70),
	}

	got := deviceRadioTableDeclaredFields(model, radio)
	sort.Strings(got)

	want := make([]string, 0, len(radioTableAttrTypes()))
	for wire := range radioTableAttrTypes() {
		if wire == "name" || wire == "dfs" {
			continue
		}
		want = append(want, wire)
	}
	sort.Strings(want)

	if !reflect.DeepEqual(got, want) {
		t.Errorf("declared fields =\n  %v\nwant\n  %v\n"+
			"(every modeled member except name (key) and dfs (discarded) must be declarable)",
			got, want)
	}
}

func Test_deviceRadioTableDeclaredFields_presence(t *testing.T) {
	// Only radio, channel, name and a false min_rssi_enabled set; the rest null.
	// A false bool must be representable, and name must not appear (it is the key).
	model := radioTableModel{
		Radio:                 types.StringValue("na"),
		Channel:               types.StringValue("auto"),
		Ht:                    types.Int64Null(),
		TxPower:               types.StringNull(),
		TxPowerMode:           types.StringNull(),
		MinRssiEnabled:        types.BoolValue(false),
		MinRssi:               types.Int64Null(),
		AntennaGain:           types.Int64Null(),
		AntennaID:             types.Int64Null(),
		Dfs:                   types.BoolValue(false),
		HardNoiseFloorEnabled: types.BoolNull(),
		LoadbalanceEnabled:    types.BoolNull(),
		Maxsta:                types.Int64Null(),
		Name:                  types.StringValue("wifi-na"),
		SensLevel:             types.Int64Null(),
		SensLevelEnabled:      types.BoolNull(),
		VwireEnabled:          types.BoolNull(),
	}
	got := deviceRadioTableDeclaredFields(model, unifi.DeviceRadioTable{})
	sort.Strings(got)
	want := []string{"channel", "min_rssi_enabled", "radio"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("declared fields = %v, want %v", got, want)
	}
}

// Test_deviceRadioTableDeclaredFields_sanitizedMemberOmitted checks that a
// pointer member the guard drops (a disabled min_rssi) leaves the mask, so
// maskedBody omits it rather than sending a null.
func Test_deviceRadioTableDeclaredFields_sanitizedMemberOmitted(t *testing.T) {
	model := radioTableModel{
		MinRssiEnabled: types.BoolValue(false),
		MinRssi:        types.Int64Value(-80),
		Name:           types.StringValue("wifi-na"),
	}
	// Guard nil'd min_rssi because the radio is disabled.
	radio := unifi.DeviceRadioTable{MinRssiEnabled: false, MinRssi: nil}
	got := deviceRadioTableDeclaredFields(model, radio)
	for _, f := range got {
		if f == "min_rssi" {
			t.Errorf("min_rssi is in the mask %v, but the guard dropped it -- it would be sent as null", got)
		}
	}
	// min_rssi_enabled (the explicit false) still travels.
	found := false
	for _, f := range got {
		if f == "min_rssi_enabled" {
			found = true
		}
	}
	if !found {
		t.Errorf("min_rssi_enabled missing from %v; an explicit false must travel", got)
	}
}

func Test_deviceRadioTableEncode_dropsUnknownName(t *testing.T) {
	// An unknown name cannot address a radio; encode reports nothing declared.
	attrs := radioTableAttrTypes()
	values := map[string]attr.Value{}
	for k, typ := range attrs {
		values[k] = nullValueFor(typ)
	}
	values["radio"] = types.StringValue("na")
	values["channel"] = types.StringValue("36")
	values["name"] = types.StringUnknown()
	obj, diags := types.ObjectValue(attrs, values)
	if diags.HasError() {
		t.Fatalf("building object: %v", diags)
	}

	radio, fields, d := deviceRadioTableEncode(context.Background(), obj)
	if d.HasError() {
		t.Fatalf("encode: %v", d)
	}
	if fields != nil {
		t.Errorf("fields = %v, want nil for an unknown name", fields)
	}
	if radio.Name != "" {
		t.Errorf("name = %q, want empty for an unknown name", radio.Name)
	}
}

func nullValueFor(typ attr.Type) attr.Value {
	switch typ {
	case types.StringType:
		return types.StringNull()
	case types.Int64Type:
		return types.Int64Null()
	case types.BoolType:
		return types.BoolNull()
	}
	return types.StringNull()
}

func TestGroupRadiosByFieldSet(t *testing.T) {
	t.Run("same set in different order shares a group", func(t *testing.T) {
		declared := []declaredRadio{
			{Radio: unifi.DeviceRadioTable{Name: "wifi-na"}, Fields: []string{"channel", "ht"}},
			{Radio: unifi.DeviceRadioTable{Name: "wifi-ng"}, Fields: []string{"ht", "channel"}},
		}
		got := groupRadiosByFieldSet(declared)
		if len(got) != 1 {
			t.Fatalf("groups = %d, want 1: %+v", len(got), got)
		}
		if len(got[0].Radios) != 2 {
			t.Fatalf("group has %d radios, want 2", len(got[0].Radios))
		}
	})

	t.Run("disjoint sets produce two groups in first-appearance order", func(t *testing.T) {
		declared := []declaredRadio{
			{Radio: unifi.DeviceRadioTable{Name: "wifi-na"}, Fields: []string{"min_rssi"}},
			{Radio: unifi.DeviceRadioTable{Name: "wifi-ng"}, Fields: []string{"maxsta"}},
		}
		got := groupRadiosByFieldSet(declared)
		if len(got) != 2 {
			t.Fatalf("groups = %d, want 2: %+v", len(got), got)
		}
		if !reflect.DeepEqual(got[0].Fields, []string{"min_rssi"}) {
			t.Errorf("group 0 fields = %v, want [min_rssi]", got[0].Fields)
		}
		if !reflect.DeepEqual(got[1].Fields, []string{"maxsta"}) {
			t.Errorf("group 1 fields = %v, want [maxsta]", got[1].Fields)
		}
	})

	t.Run("no declared radios produces no groups", func(t *testing.T) {
		if got := groupRadiosByFieldSet(nil); len(got) != 0 {
			t.Errorf("groups = %+v, want none", got)
		}
	})
}

func TestDedupeDeclaredRadios(t *testing.T) {
	declared := []declaredRadio{
		{Radio: unifi.DeviceRadioTable{Name: "wifi-na", Channel: "36"}, Fields: []string{"channel"}},
		{Radio: unifi.DeviceRadioTable{Name: "wifi-na", Channel: "40"}, Fields: []string{"channel"}},
		{Radio: unifi.DeviceRadioTable{Name: "wifi-ng", Channel: "6"}, Fields: []string{"channel"}},
	}
	got := dedupeDeclaredRadios(declared)
	if len(got) != 2 {
		t.Fatalf("deduped to %d entries, want 2", len(got))
	}
	if got[0].Radio.Name != "wifi-na" || got[0].Radio.Channel != "40" {
		t.Errorf("wifi-na = %+v, want the last block (channel 40) to win", got[0].Radio)
	}
}

func TestDeclaredRadiosWithFields(t *testing.T) {
	declared := []declaredRadio{
		{Radio: unifi.DeviceRadioTable{Name: "wifi-na"}, Fields: []string{"channel"}},
		{Radio: unifi.DeviceRadioTable{Name: "wifi-ng"}, Fields: nil},          // no writable member
		{Radio: unifi.DeviceRadioTable{Name: ""}, Fields: []string{"channel"}}, // no name
	}
	got := declaredRadiosWithFields(declared)
	if len(got) != 1 {
		t.Fatalf("kept %d entries, want 1 (only wifi-na declares a writable member and a name)", len(got))
	}
	if got[0].Radio.Name != "wifi-na" {
		t.Errorf("kept %q, want wifi-na", got[0].Radio.Name)
	}
}

// TestUpdateDeviceRadioTableGrouped_MergePreservesEverythingElse pins the
// merge contract the whole change exists for: a write that names one member of
// one radio must leave every other member of that radio (including one this
// client does not model, like nss), every other radio, and every radio the
// write never named untouched. Disjoint member sets must also issue separate
// calls, since one member mask serves the whole call.
//
// The fake controller MERGES member by member, which is what a 10.6.101
// controller does for radio_table -- the opposite of the port_overrides fake,
// which replaces. A regression that resent the whole array (the port_override
// contract) would still pass a replace fake; only a merge fake catches it.
func TestUpdateDeviceRadioTableGrouped_MergePreservesEverythingElse(t *testing.T) {
	const deviceID = "dev1"
	const deviceMAC = "00:00:00:00:00:01"

	stored := map[string]map[string]any{
		"wifi-na": {"name": "wifi-na", "radio": "na", "maxsta": float64(42), "nss": float64(2)},
		"wifi-ng": {"name": "wifi-ng", "radio": "ng", "maxsta": float64(77)},
		"wifi-6e": {"name": "wifi-6e", "radio": "6e", "channel": "37"},
	}
	order := []string{"wifi-na", "wifi-ng", "wifi-6e"}

	var putCount int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/":
			w.WriteHeader(http.StatusOK)
			return
		case r.URL.Path == "/proxy/network/status":
			_, _ = w.Write([]byte(`{"meta":{"server_version":"8.0.0"}}`))
			return
		case r.Method == http.MethodPut:
			putCount++
			var body struct {
				RadioTable []map[string]any `json:"radio_table"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode PUT body: %v", err)
			}
			// Merge member by member into the stored entry keyed by name, the
			// way the real controller does. A radio the write never names is
			// left as-is by construction.
			for _, entry := range body.RadioTable {
				name, _ := entry["name"].(string)
				target, ok := stored[name]
				if !ok {
					t.Fatalf("write named %q, which no stored radio carries", name)
				}
				for k, v := range entry {
					target[k] = v
				}
			}
			_, _ = w.Write([]byte(`{"meta":{"rc":"ok"},"data":[]}`))
			return
		default:
			entries := make([]map[string]any, 0, len(stored))
			for _, k := range order {
				entries = append(entries, stored[k])
			}
			device := map[string]any{"_id": deviceID, "mac": deviceMAC, "radio_table": entries}
			raw, _ := json.Marshal(map[string]any{
				"meta": map[string]any{"rc": "ok"},
				"data": []any{device},
			})
			_, _ = w.Write(raw)
		}
	}))
	defer srv.Close()

	client, err := unifi.New(context.Background(), &unifi.Config{BaseURL: srv.URL, APIKey: "test-key"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	declared := []declaredRadio{
		{
			Radio:  unifi.DeviceRadioTable{Name: "wifi-na", MinRssiEnabled: true, MinRssi: ptrInt64(-80)},
			Fields: []string{"min_rssi_enabled", "min_rssi"},
		},
		{
			Radio:  unifi.DeviceRadioTable{Name: "wifi-ng", Maxsta: ptrInt64(99)},
			Fields: []string{"maxsta"},
		},
	}

	got, err := updateDeviceRadioTableGrouped(context.Background(), client, "default",
		&unifi.Device{ID: deviceID, MAC: deviceMAC}, declared)
	if err != nil {
		t.Fatalf("updateDeviceRadioTableGrouped: %v", err)
	}

	if putCount != 2 {
		t.Fatalf("PUT count = %d, want 2 -- disjoint member sets must not share a call", putCount)
	}

	byName := map[string]unifi.DeviceRadioTable{}
	for _, r := range got.RadioTable {
		byName[r.Name] = r
	}

	// wifi-na got its min_rssi, and kept maxsta (a member the write did not name).
	if r := byName["wifi-na"]; r.MinRssi == nil || *r.MinRssi != -80 {
		t.Errorf("wifi-na min_rssi = %v, want -80", r.MinRssi)
	}
	if r := byName["wifi-na"]; r.Maxsta == nil || *r.Maxsta != 42 {
		t.Errorf("wifi-na maxsta = %v, want 42 -- a member the write did not name must survive", r.Maxsta)
	}
	// nss is a member this client does not model at all; it must survive on the wire.
	if _, ok := stored["wifi-na"]["nss"]; !ok {
		t.Error("wifi-na lost nss, an unmodeled member the merge must preserve")
	}
	// wifi-ng got its maxsta.
	if r := byName["wifi-ng"]; r.Maxsta == nil || *r.Maxsta != 99 {
		t.Errorf("wifi-ng maxsta = %v, want 99", r.Maxsta)
	}
	// wifi-6e was never named and must be untouched.
	if r := byName["wifi-6e"]; r.Channel != "37" {
		t.Errorf("wifi-6e channel = %q, want 37 -- a radio the write never named must survive", r.Channel)
	}
}

func priorRadioList(t *testing.T, models ...radioTableModel) types.List {
	t.Helper()
	elemType := types.ObjectType{AttrTypes: radioTableAttrTypes()}
	elements := make([]attr.Value, 0, len(models))
	for _, m := range models {
		obj, diags := types.ObjectValueFrom(context.Background(), radioTableAttrTypes(), m)
		if diags.HasError() {
			t.Fatalf("building prior radio object: %v", diags)
		}
		elements = append(elements, obj)
	}
	list, diags := types.ListValue(elemType, elements)
	if diags.HasError() {
		t.Fatalf("building prior radio list: %v", diags)
	}
	return list
}

func reconciledByName(t *testing.T, list types.List) map[string]radioTableModel {
	t.Helper()
	var models []radioTableModel
	diags := list.ElementsAs(context.Background(), &models, false)
	if diags.HasError() {
		t.Fatalf("decoding reconciled list: %v", diags)
	}
	out := make(map[string]radioTableModel, len(models))
	for _, m := range models {
		out[m.Name.ValueString()] = m
	}
	return out
}

func Test_deviceReconcileRadioTable(t *testing.T) {
	ctx := context.Background()

	t.Run("declared member reads back the controller value so drift surfaces", func(t *testing.T) {
		prior := priorRadioList(t, radioTableModel{
			Name:    types.StringValue("wifi-na"),
			Channel: types.StringValue("36"),
			Maxsta:  types.Int64Value(42),
			// members the config never set stay null
			Radio:                 types.StringNull(),
			Ht:                    types.Int64Null(),
			TxPower:               types.StringNull(),
			TxPowerMode:           types.StringNull(),
			MinRssiEnabled:        types.BoolNull(),
			MinRssi:               types.Int64Null(),
			AntennaGain:           types.Int64Null(),
			AntennaID:             types.Int64Null(),
			Dfs:                   types.BoolNull(),
			HardNoiseFloorEnabled: types.BoolNull(),
			LoadbalanceEnabled:    types.BoolNull(),
			SensLevel:             types.Int64Null(),
			SensLevelEnabled:      types.BoolNull(),
			VwireEnabled:          types.BoolNull(),
		})
		api := []unifi.DeviceRadioTable{
			{Name: "wifi-na", Channel: "40", Maxsta: ptrInt64(100), Radio: "na"},
		}
		out, diags := deviceReconcileRadioTable(ctx, prior, api)
		if diags.HasError() {
			t.Fatalf("reconcile: %v", diags)
		}
		got := reconciledByName(t, out)["wifi-na"]
		if got.Channel.ValueString() != "40" {
			t.Errorf("channel = %q, want 40 (declared drift surfaces)", got.Channel.ValueString())
		}
		if got.Maxsta.ValueInt64() != 100 {
			t.Errorf("maxsta = %d, want 100 (declared drift surfaces)", got.Maxsta.ValueInt64())
		}
		// radio was null in prior (never declared) -- it must stay null, not
		// gain the controller's "na" and diff forever.
		if !got.Radio.IsNull() {
			t.Errorf("radio = %v, want null -- an undeclared member must not gain a value", got.Radio)
		}
	})

	t.Run("radio missing from the response keeps its prior value", func(t *testing.T) {
		prior := priorRadioList(t, radioTableModel{
			Name:    types.StringValue("wifi-6e"),
			Channel: types.StringValue("37"),
			Radio:   types.StringNull(), Ht: types.Int64Null(), TxPower: types.StringNull(),
			TxPowerMode: types.StringNull(), MinRssiEnabled: types.BoolNull(), MinRssi: types.Int64Null(),
			AntennaGain: types.Int64Null(), AntennaID: types.Int64Null(), Dfs: types.BoolNull(),
			HardNoiseFloorEnabled: types.BoolNull(), LoadbalanceEnabled: types.BoolNull(),
			Maxsta: types.Int64Null(), SensLevel: types.Int64Null(), SensLevelEnabled: types.BoolNull(),
			VwireEnabled: types.BoolNull(),
		})
		// The controller reports a different radio, not wifi-6e.
		api := []unifi.DeviceRadioTable{{Name: "wifi-na", Channel: "40"}}
		out, diags := deviceReconcileRadioTable(ctx, prior, api)
		if diags.HasError() {
			t.Fatalf("reconcile: %v", diags)
		}
		got := reconciledByName(t, out)["wifi-6e"]
		if got.Channel.ValueString() != "37" {
			t.Errorf("channel = %q, want 37 (a radio absent from the response keeps its prior value)",
				got.Channel.ValueString())
		}
	})

	t.Run("dfs set by the practitioner is kept, not overwritten by the discarded controller value", func(t *testing.T) {
		prior := priorRadioList(t, radioTableModel{
			Name:  types.StringValue("wifi-na"),
			Dfs:   types.BoolValue(true), // practitioner set it
			Radio: types.StringNull(), Channel: types.StringNull(), Ht: types.Int64Null(),
			TxPower: types.StringNull(), TxPowerMode: types.StringNull(), MinRssiEnabled: types.BoolNull(),
			MinRssi: types.Int64Null(), AntennaGain: types.Int64Null(), AntennaID: types.Int64Null(),
			HardNoiseFloorEnabled: types.BoolNull(), LoadbalanceEnabled: types.BoolNull(),
			Maxsta: types.Int64Null(), SensLevel: types.Int64Null(), SensLevelEnabled: types.BoolNull(),
			VwireEnabled: types.BoolNull(),
		})
		// Controller reports dfs false (it discarded the written true).
		api := []unifi.DeviceRadioTable{{Name: "wifi-na", Dfs: false}}
		out, diags := deviceReconcileRadioTable(ctx, prior, api)
		if diags.HasError() {
			t.Fatalf("reconcile: %v", diags)
		}
		got := reconciledByName(t, out)["wifi-na"]
		if got.Dfs.ValueBool() != true {
			t.Errorf("dfs = %v, want true kept -- a discarded member must not surface the controller's value as drift", got.Dfs)
		}
	})

	t.Run("dfs unknown resolves to the controller value", func(t *testing.T) {
		// Build a prior with dfs unknown, everything else null.
		attrs := radioTableAttrTypes()
		values := map[string]attr.Value{}
		for k, typ := range attrs {
			values[k] = nullValueFor(typ)
		}
		values["name"] = types.StringValue("wifi-na")
		values["dfs"] = types.BoolUnknown()
		obj, diags := types.ObjectValue(attrs, values)
		if diags.HasError() {
			t.Fatalf("building object: %v", diags)
		}
		prior, diags := types.ListValue(types.ObjectType{AttrTypes: attrs}, []attr.Value{obj})
		if diags.HasError() {
			t.Fatalf("building list: %v", diags)
		}
		api := []unifi.DeviceRadioTable{{Name: "wifi-na", Dfs: true}}
		out, d := deviceReconcileRadioTable(ctx, prior, api)
		if d.HasError() {
			t.Fatalf("reconcile: %v", d)
		}
		got := reconciledByName(t, out)["wifi-na"]
		if got.Dfs.IsUnknown() {
			t.Fatal("dfs stayed unknown; a Computed member must resolve to a known value")
		}
		if got.Dfs.ValueBool() != true {
			t.Errorf("dfs = %v, want true resolved from the controller", got.Dfs)
		}
	})

	t.Run("empty prior yields an empty list", func(t *testing.T) {
		prior := priorRadioList(t)
		out, diags := deviceReconcileRadioTable(ctx, prior, []unifi.DeviceRadioTable{{Name: "wifi-na"}})
		if diags.HasError() {
			t.Fatalf("reconcile: %v", diags)
		}
		if len(out.Elements()) != 0 {
			t.Errorf("reconciled %d radios, want 0 -- an undeclared radio_table stays as it was (empty)", len(out.Elements()))
		}
	})
}
