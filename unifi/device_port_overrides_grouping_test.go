package unifi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"testing"

	"github.com/ubiquiti-community/go-unifi/unifi"
)

func TestGroupPortOverridesByFieldSet(t *testing.T) {
	t.Run("same set in different order shares a group", func(t *testing.T) {
		declared := []declaredPortOverride{
			{Override: unifi.DevicePortOverrides{PortIDX: ptrInt64(1)}, Fields: []string{"name", "poe_mode"}},
			{Override: unifi.DevicePortOverrides{PortIDX: ptrInt64(2)}, Fields: []string{"poe_mode", "name"}},
		}
		got := groupPortOverridesByFieldSet(declared)
		if len(got) != 1 {
			t.Fatalf("groups = %d, want 1: %+v", len(got), got)
		}
		if len(got[0].Ports) != 2 {
			t.Fatalf("group has %d ports, want 2: %+v", len(got[0].Ports), got[0])
		}
	})

	t.Run("disjoint sets produce two groups in first-appearance order", func(t *testing.T) {
		declared := []declaredPortOverride{
			{Override: unifi.DevicePortOverrides{PortIDX: ptrInt64(1)}, Fields: []string{"name"}},
			{Override: unifi.DevicePortOverrides{PortIDX: ptrInt64(2)}, Fields: []string{"poe_mode"}},
		}
		got := groupPortOverridesByFieldSet(declared)
		if len(got) != 2 {
			t.Fatalf("groups = %d, want 2: %+v", len(got), got)
		}
		if !reflect.DeepEqual(got[0].Fields, []string{"name"}) {
			t.Errorf("group 0 fields = %v, want [name]", got[0].Fields)
		}
		if !reflect.DeepEqual(got[1].Fields, []string{"poe_mode"}) {
			t.Errorf("group 1 fields = %v, want [poe_mode]", got[1].Fields)
		}
	})

	t.Run("a subset and a superset are different groups", func(t *testing.T) {
		declared := []declaredPortOverride{
			{Override: unifi.DevicePortOverrides{PortIDX: ptrInt64(1)}, Fields: []string{"name"}},
			{Override: unifi.DevicePortOverrides{PortIDX: ptrInt64(2)}, Fields: []string{"name", "poe_mode"}},
		}
		got := groupPortOverridesByFieldSet(declared)
		if len(got) != 2 {
			t.Fatalf("groups = %d, want 2 -- a subset is not the same set as its superset: %+v", len(got), got)
		}
	})

	t.Run("three ports, two sharing a set", func(t *testing.T) {
		declared := []declaredPortOverride{
			{Override: unifi.DevicePortOverrides{PortIDX: ptrInt64(1)}, Fields: []string{"name"}},
			{Override: unifi.DevicePortOverrides{PortIDX: ptrInt64(2)}, Fields: []string{"poe_mode"}},
			{Override: unifi.DevicePortOverrides{PortIDX: ptrInt64(3)}, Fields: []string{"name"}},
		}
		got := groupPortOverridesByFieldSet(declared)
		if len(got) != 2 {
			t.Fatalf("groups = %d, want 2: %+v", len(got), got)
		}
		if len(got[0].Ports) != 2 {
			t.Errorf("the \"name\" group has %d ports, want 2 (ports 1 and 3)", len(got[0].Ports))
		}
	})

	t.Run("no declared ports produces no groups", func(t *testing.T) {
		if got := groupPortOverridesByFieldSet(nil); len(got) != 0 {
			t.Errorf("groups = %+v, want none", got)
		}
	})
}

// TestUpdateDevicePortOverridesGrouped_DisjointSetsLoseNoMember pins the
// ruling groupPortOverridesByFieldSet documents: UpdateDevicePortOverrides
// takes one member mask for the whole call, so port 1 declaring only
// "name" and port 2 declaring only "poe_mode" cannot share a call -- a
// union mask would carry poe_mode onto port 1's write and name onto port
// 2's, both at their Go zero value, clobbering whatever the controller
// held. Measured against a live controller.
//
// This fails on either regression: the call count drops to one, or either
// port loses the member it did not declare.
func TestUpdateDevicePortOverridesGrouped_DisjointSetsLoseNoMember(t *testing.T) {
	const deviceID = "dev1"
	const deviceMAC = "00:00:00:00:00:01"

	stored := map[string]map[string]any{
		"1": {"port_idx": float64(1), "name": "uplink", "poe_mode": "auto"},
		"2": {"port_idx": float64(2), "name": "desk", "poe_mode": "auto"},
	}

	var putCount int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/":
			w.WriteHeader(http.StatusOK) // new-style API probe
			return
		case r.URL.Path == "/proxy/network/status":
			_, _ = w.Write([]byte(`{"meta":{"server_version":"8.0.0"}}`))
			return
		case r.Method == http.MethodPut:
			putCount++
			var body struct {
				PortOverrides []map[string]any `json:"port_overrides"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode PUT body: %v", err)
			}
			// The real controller replaces the whole array with what it
			// receives -- the behaviour UpdateDevicePortOverrides exists to
			// compensate for -- so the fake reproduces that instead of
			// merging on the fake's side.
			next := make(map[string]map[string]any, len(body.PortOverrides))
			for _, entry := range body.PortOverrides {
				idx, _ := entry["port_idx"].(float64)
				next[strconv.Itoa(int(idx))] = entry
			}
			stored = next
			_, _ = w.Write([]byte(`{"meta":{"rc":"ok"},"data":[]}`))
			return
		default:
			entries := make([]map[string]any, 0, len(stored))
			for _, k := range []string{"1", "2"} {
				if e, ok := stored[k]; ok {
					entries = append(entries, e)
				}
			}
			device := map[string]any{
				"_id": deviceID, "mac": deviceMAC, "port_overrides": entries,
			}
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

	declared := []declaredPortOverride{
		{Override: unifi.DevicePortOverrides{PortIDX: ptrInt64(1), Name: "renamed"}, Fields: []string{"name"}},
		{Override: unifi.DevicePortOverrides{PortIDX: ptrInt64(2), PoeMode: "off"}, Fields: []string{"poe_mode"}},
	}

	got, err := updateDevicePortOverridesGrouped(context.Background(), client, "default",
		&unifi.Device{ID: deviceID, MAC: deviceMAC}, declared)
	if err != nil {
		t.Fatalf("updateDevicePortOverridesGrouped: %v", err)
	}

	if putCount != 2 {
		t.Fatalf("PUT count = %d, want 2 -- disjoint member sets must not share a call", putCount)
	}

	byIdx := indexOverrides(got.PortOverrides)
	if byIdx[1].Name != "renamed" {
		t.Errorf("port 1 name = %q, want renamed", byIdx[1].Name)
	}
	if byIdx[1].PoeMode != "auto" {
		t.Errorf("port 1 poe_mode = %q, want auto -- the member it did not declare must survive", byIdx[1].PoeMode)
	}
	if byIdx[2].PoeMode != "off" {
		t.Errorf("port 2 poe_mode = %q, want off", byIdx[2].PoeMode)
	}
	if byIdx[2].Name != "desk" {
		t.Errorf("port 2 name = %q, want desk -- the member it did not declare must survive", byIdx[2].Name)
	}
}
