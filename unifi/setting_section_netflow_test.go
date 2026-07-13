package unifi

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
)

const goldenNetflow = `{"auto_engine_id_enabled":false,"enabled":true,"engine_id":1,"export_frequency":60,"key":"netflow","network_ids":["net-a"],"port":2055,"refresh_rate":600,"sampling_mode":"deterministic","sampling_rate":100,"server":"netflow-collector.example.com","version":9}`

func netflowRepresentativeModel(ctx context.Context, t *testing.T) types.Object {
	t.Helper()
	networkIDs, diags := types.ListValueFrom(ctx, types.StringType, []string{"net-a"})
	if diags.HasError() {
		t.Fatalf("building network_ids list: %v", diags)
	}
	m := settingNetflowModel{
		AutoEngineIDEnabled: types.BoolValue(false),
		Enabled:             types.BoolValue(true),
		EngineID:            types.Int64Value(1),
		ExportFrequency:     types.Int64Value(60),
		NetworkIDs:          networkIDs,
		Port:                types.Int64Value(2055),
		RefreshRate:         types.Int64Value(600),
		SamplingMode:        types.StringValue("deterministic"),
		SamplingRate:        types.Int64Value(100),
		Server:              types.StringValue("netflow-collector.example.com"),
		Version:             types.Int64Value(9),
	}
	obj, diags2 := types.ObjectValueFrom(ctx, netflowAttrTypes, m)
	if diags2.HasError() {
		t.Fatalf("building netflow object: %v", diags2)
	}
	return obj
}

func TestNetflowSection_GoldenReproduction(t *testing.T) {
	ctx := context.Background()
	sec := netflowSection{}

	model := settingResourceModel{Netflow: netflowRepresentativeModel(ctx, t)}
	prior := settingResourceModel{}
	snap := newRawSettings(nil)

	rs, configured, oDiags := sec.overlay(ctx, model, prior, snap)
	if oDiags.HasError() {
		t.Fatalf("overlay diagnostics: %v", oDiags)
	}
	if !configured {
		t.Fatalf("overlay configured = false, want true")
	}
	if rs.Key != "netflow" {
		t.Errorf("rs.Key = %q, want %q", rs.Key, "netflow")
	}

	assertPUTBodyMatchesGolden(t, rs, goldenNetflow)
}

func TestNetflowSection_DecodeRoundTrip(t *testing.T) {
	ctx := context.Background()
	sec := netflowSection{}

	snap := newRawSettings([]settings.RawSetting{{
		BaseSetting: settings.BaseSetting{Key: "netflow"},
		Data: map[string]any{
			"auto_engine_id_enabled": false,
			"enabled":                true,
			"engine_id":              float64(1),
			"export_frequency":       float64(60),
			"network_ids":            []any{"net-a"},
			"port":                   float64(2055),
			"refresh_rate":           float64(600),
			"sampling_mode":          "deterministic",
			"sampling_rate":          float64(100),
			"server":                 "netflow-collector.example.com",
			"version":                float64(9),
		},
	}})

	prior := settingResourceModel{}
	model := settingResourceModel{}

	diags := sec.decode(ctx, snap, prior, &model)
	if diags.HasError() {
		t.Fatalf("decode diagnostics: %v", diags)
	}

	var got settingNetflowModel
	if diags := model.Netflow.As(ctx, &got, basetypes.ObjectAsOptions{}); diags.HasError() {
		t.Fatalf("extracting settingNetflowModel: %v", diags)
	}

	if !got.Enabled.ValueBool() {
		t.Errorf("Enabled = %v, want true", got.Enabled.ValueBool())
	}
	if got.Port.ValueInt64() != 2055 {
		t.Errorf("Port = %d, want 2055", got.Port.ValueInt64())
	}
	if got.SamplingMode.ValueString() != "deterministic" {
		t.Errorf("SamplingMode = %q, want %q", got.SamplingMode.ValueString(), "deterministic")
	}
	if got.Version.ValueInt64() != 9 {
		t.Errorf("Version = %d, want 9", got.Version.ValueInt64())
	}
}

func TestNetflowSection_Preservation(t *testing.T) {
	ctx := context.Background()
	sec := netflowSection{}

	snap := newRawSettings([]settings.RawSetting{{
		BaseSetting: settings.BaseSetting{Key: "netflow"},
		Data: map[string]any{
			"enabled":     true,
			"x_unmanaged": "keep",
		},
	}})

	model := settingResourceModel{Netflow: netflowRepresentativeModel(ctx, t)}
	prior := settingResourceModel{}

	rs, configured, oDiags := sec.overlay(ctx, model, prior, snap)
	if oDiags.HasError() {
		t.Fatalf("overlay diagnostics: %v", oDiags)
	}
	if !configured {
		t.Fatalf("overlay configured = false, want true")
	}

	if got, ok := rs.Data["x_unmanaged"]; !ok || got != "keep" {
		t.Errorf("rs.Data[%q] = %v (ok=%v), want %q", "x_unmanaged", got, ok, "keep")
	}
}

func TestNetflowSection_NotConfigured(t *testing.T) {
	ctx := context.Background()
	sec := netflowSection{}

	model := settingResourceModel{Netflow: types.ObjectNull(netflowAttrTypes)}
	prior := settingResourceModel{}
	snap := newRawSettings(nil)

	rs, configured, oDiags := sec.overlay(ctx, model, prior, snap)
	if oDiags.HasError() {
		t.Fatalf("overlay diagnostics: %v", oDiags)
	}
	if configured {
		t.Fatalf("overlay configured = true, want false")
	}
	if rs.Key != "" || len(rs.Data) != 0 {
		t.Errorf("overlay returned non-zero RawSetting when not configured: %+v", rs)
	}
}

func TestNetflowSection_InterfaceWiring(t *testing.T) {
	sec := netflowSection{}
	if sec.key() != "netflow" {
		t.Errorf("key() = %q, want %q", sec.key(), "netflow")
	}
	if sec.attrName() != "netflow" {
		t.Errorf("attrName() = %q, want %q", sec.attrName(), "netflow")
	}

	var found bool
	for _, s := range settingSections {
		if _, ok := s.(netflowSection); ok {
			found = true
		}
	}
	if !found {
		t.Errorf("netflowSection not found in settingSections registry")
	}
}
