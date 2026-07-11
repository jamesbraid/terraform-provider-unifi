package unifi

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
	"github.com/ubiquiti-community/terraform-provider-unifi/unifi/util"
)

func Test_netflowModelToData(t *testing.T) {
	ctx := context.Background()
	var diags diag.Diagnostics
	ids, d := types.SetValueFrom(ctx, types.StringType, []string{"net1"})
	if d.HasError() {
		t.Fatal(d)
	}
	m := &settingNetflowModel{
		AutoEngineIDEnabled: types.BoolValue(false),
		Enabled:             types.BoolValue(true),
		EngineID:            types.Int64Value(42),
		ExportFrequency:     types.Int64Null(),
		NetworkIDs:          ids,
		Port:                types.Int64Value(2055),
		RefreshRate:         types.Int64Null(),
		SamplingMode:        types.StringValue("off"),
		SamplingRate:        types.Int64Null(),
		Server:              types.StringValue("192.0.2.10"),
		Version:             types.Int64Value(10),
	}
	// Pre-existing raw fields must be preserved, not clobbered.
	data := map[string]any{"unmodeled_field": "keep", "refresh_rate": float64(20)}

	netflowModelToData(ctx, m, data, &diags)
	if diags.HasError() {
		t.Fatal(diags)
	}

	if data["unmodeled_field"] != "keep" {
		t.Fatal("unmodeled_field was clobbered")
	}
	if data["refresh_rate"] != float64(20) {
		t.Fatal("null refresh_rate overwrote remote value")
	}
	if data["enabled"] != true || data["auto_engine_id_enabled"] != false {
		t.Fatalf("bools wrong: %v", data)
	}
	if data["engine_id"] != int64(42) || data["port"] != int64(2055) || data["version"] != int64(10) {
		t.Fatalf("ints wrong: %v", data)
	}
	if data["sampling_mode"] != "off" || data["server"] != "192.0.2.10" {
		t.Fatalf("strings wrong: %v", data)
	}
	got, ok := data["network_ids"].([]string)
	if !ok || len(got) != 1 || got[0] != "net1" {
		t.Fatalf("network_ids = %v", data["network_ids"])
	}
	for _, key := range []string{"export_frequency", "sampling_rate"} {
		if _, present := data[key]; present {
			t.Fatalf("null model wrote %s", key)
		}
	}
}

func Test_netflowSettingToModel(t *testing.T) {
	ctx := context.Background()
	var diags diag.Diagnostics
	m := netflowSettingToModel(ctx, &settings.Netflow{
		AutoEngineIDEnabled: true,
		Enabled:             false,
		ExportFrequency:     util.Ptr(int64(5)),
		NetworkIDs:          []string{"net1"},
		Port:                util.Ptr(int64(2055)),
		RefreshRate:         util.Ptr(int64(20)),
		Version:             util.Ptr(int64(10)),
	}, &diags)
	if diags.HasError() {
		t.Fatal(diags)
	}

	if !m.AutoEngineIDEnabled.ValueBool() || m.Enabled.ValueBool() {
		t.Fatalf("bools wrong: %v / %v", m.AutoEngineIDEnabled, m.Enabled)
	}
	if m.Port.ValueInt64() != 2055 || m.Version.ValueInt64() != 10 {
		t.Fatalf("ints wrong: %v / %v", m.Port, m.Version)
	}
	// nil pointers and empty strings map to null.
	if !m.EngineID.IsNull() || !m.SamplingRate.IsNull() {
		t.Fatalf("nil int pointers should be null: %v / %v", m.EngineID, m.SamplingRate)
	}
	if !m.SamplingMode.IsNull() || !m.Server.IsNull() {
		t.Fatalf("empty strings should be null: %v / %v", m.SamplingMode, m.Server)
	}
	var ids []string
	diags.Append(m.NetworkIDs.ElementsAs(ctx, &ids, false)...)
	if len(ids) != 1 || ids[0] != "net1" {
		t.Fatalf("network_ids = %v", ids)
	}
}
