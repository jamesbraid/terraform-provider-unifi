package unifi

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
)

const goldenGlobalNat = `{"excluded_network_ids":["net-a"],"key":"global_nat","mode":"auto"}`

func TestGlobalNatSection_GoldenReproduction(t *testing.T) {
	ctx := context.Background()
	sec := globalNatSection{}

	excluded, diags := types.ListValueFrom(ctx, types.StringType, []string{"net-a"})
	if diags.HasError() {
		t.Fatalf("building excluded_network_ids list: %v", diags)
	}
	m := settingGlobalNatModel{
		ExcludedNetworkIDs: excluded,
		Mode:               types.StringValue("auto"),
	}
	obj, diags2 := types.ObjectValueFrom(ctx, globalNatAttrTypes, m)
	if diags2.HasError() {
		t.Fatalf("building global_nat object: %v", diags2)
	}

	model := settingResourceModel{GlobalNat: obj}
	prior := settingResourceModel{}
	snap := newRawSettings(nil)

	rs, configured, oDiags := sec.overlay(ctx, model, prior, snap)
	if oDiags.HasError() {
		t.Fatalf("overlay diagnostics: %v", oDiags)
	}
	if !configured {
		t.Fatalf("overlay configured = false, want true")
	}
	if rs.Key != "global_nat" {
		t.Errorf("rs.Key = %q, want %q", rs.Key, "global_nat")
	}

	assertPUTBodyMatchesGolden(t, rs, goldenGlobalNat)
}

func TestGlobalNatSection_DecodeRoundTrip(t *testing.T) {
	ctx := context.Background()
	sec := globalNatSection{}

	snap := newRawSettings([]settings.RawSetting{{
		BaseSetting: settings.BaseSetting{Key: "global_nat"},
		Data: map[string]any{
			"excluded_network_ids": []any{"net-a"},
			"mode":                 "auto",
		},
	}})

	prior := settingResourceModel{}
	model := settingResourceModel{}

	diags := sec.decode(ctx, snap, prior, &model)
	if diags.HasError() {
		t.Fatalf("decode diagnostics: %v", diags)
	}

	var got settingGlobalNatModel
	if diags := model.GlobalNat.As(ctx, &got, basetypes.ObjectAsOptions{}); diags.HasError() {
		t.Fatalf("extracting settingGlobalNatModel: %v", diags)
	}

	if got.Mode.ValueString() != "auto" {
		t.Errorf("Mode = %q, want %q", got.Mode.ValueString(), "auto")
	}
	var ids []string
	if diags := got.ExcludedNetworkIDs.ElementsAs(ctx, &ids, false); diags.HasError() {
		t.Fatalf("extracting ExcludedNetworkIDs: %v", diags)
	}
	if len(ids) != 1 || ids[0] != "net-a" {
		t.Errorf("ExcludedNetworkIDs = %v, want [net-a]", ids)
	}
}

func TestGlobalNatSection_Preservation(t *testing.T) {
	ctx := context.Background()
	sec := globalNatSection{}

	snap := newRawSettings([]settings.RawSetting{{
		BaseSetting: settings.BaseSetting{Key: "global_nat"},
		Data: map[string]any{
			"mode":        "auto",
			"x_unmanaged": "keep",
		},
	}})

	m := settingGlobalNatModel{
		ExcludedNetworkIDs: types.ListNull(types.StringType),
		Mode:               types.StringValue("auto"),
	}
	obj, diags := types.ObjectValueFrom(ctx, globalNatAttrTypes, m)
	if diags.HasError() {
		t.Fatalf("building global_nat object: %v", diags)
	}

	model := settingResourceModel{GlobalNat: obj}
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

func TestGlobalNatSection_NotConfigured(t *testing.T) {
	ctx := context.Background()
	sec := globalNatSection{}

	model := settingResourceModel{GlobalNat: types.ObjectNull(globalNatAttrTypes)}
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

func TestGlobalNatSection_InterfaceWiring(t *testing.T) {
	sec := globalNatSection{}
	if sec.key() != "global_nat" {
		t.Errorf("key() = %q, want %q", sec.key(), "global_nat")
	}
	if sec.attrName() != "global_nat" {
		t.Errorf("attrName() = %q, want %q", sec.attrName(), "global_nat")
	}

	var found bool
	for _, s := range settingSections {
		if _, ok := s.(globalNatSection); ok {
			found = true
		}
	}
	if !found {
		t.Errorf("globalNatSection not found in settingSections registry")
	}
}
