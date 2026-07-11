package unifi

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	fwresource "github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
)

func etherLightingSpeedOverrideSet(
	t *testing.T, ctx context.Context,
	overrides []settingEtherLightingSpeedOverrideModel,
) types.Set {
	t.Helper()
	set, d := types.SetValueFrom(ctx,
		types.ObjectType{AttrTypes: etherLightingSpeedOverrideAttrTypes}, overrides)
	if d.HasError() {
		t.Fatal(d)
	}
	return set
}

func Test_etherLightingModelToData(t *testing.T) {
	ctx := context.Background()
	var diags diag.Diagnostics

	network, d := types.SetValueFrom(ctx,
		types.ObjectType{AttrTypes: etherLightingNetworkOverrideAttrTypes},
		[]settingEtherLightingNetworkOverrideModel{{
			NetworkID: types.StringValue("5dbaa47ea7986c04d72d4f5e"),
			ColorHex:  types.StringValue("0544ff"),
		}})
	if d.HasError() {
		t.Fatal(d)
	}
	speed := etherLightingSpeedOverrideSet(t, ctx,
		[]settingEtherLightingSpeedOverrideModel{{
			Speed:    types.StringValue("GbE"),
			ColorHex: types.StringValue("ff6c14"),
		}})

	m := &settingEtherLightingModel{
		NetworkOverrides: network,
		SpeedOverrides:   speed,
	}

	// The live controller carries default palettes go-unifi does not model
	// (network_defaults, speed_defaults); the raw merge must preserve them.
	data := map[string]any{
		"network_defaults": []any{map[string]any{
			"key": "5dbaa47ea7986c04d72d4f5e", "raw_color_hex": "0544ff",
		}},
		"speed_defaults": []any{map[string]any{
			"key": "10M", "raw_color_hex": "FFC105",
		}},
	}

	etherLightingModelToData(ctx, m, data, &diags)
	if diags.HasError() {
		t.Fatal(diags)
	}

	if _, present := data["network_defaults"]; !present {
		t.Fatal("unmodeled network_defaults was clobbered")
	}
	if _, present := data["speed_defaults"]; !present {
		t.Fatal("unmodeled speed_defaults was clobbered")
	}
	nets, ok := data["network_overrides"].([]map[string]any)
	if !ok || len(nets) != 1 || nets[0]["key"] != "5dbaa47ea7986c04d72d4f5e" ||
		nets[0]["raw_color_hex"] != "0544ff" {
		t.Fatalf("network_overrides = %v", data["network_overrides"])
	}
	speeds, ok := data["speed_overrides"].([]map[string]any)
	if !ok || len(speeds) != 1 || speeds[0]["key"] != "GbE" ||
		speeds[0]["raw_color_hex"] != "ff6c14" {
		t.Fatalf("speed_overrides = %v", data["speed_overrides"])
	}
}

func Test_etherLightingModelToData_nullsLeaveRemoteValues(t *testing.T) {
	ctx := context.Background()
	var diags diag.Diagnostics
	m := &settingEtherLightingModel{
		NetworkOverrides: types.SetNull(
			types.ObjectType{AttrTypes: etherLightingNetworkOverrideAttrTypes}),
		SpeedOverrides: types.SetNull(
			types.ObjectType{AttrTypes: etherLightingSpeedOverrideAttrTypes}),
	}
	data := map[string]any{
		"speed_overrides": []any{map[string]any{
			"key": "GbE", "raw_color_hex": "aabbcc",
		}},
	}

	etherLightingModelToData(ctx, m, data, &diags)
	if diags.HasError() {
		t.Fatal(diags)
	}

	speeds, ok := data["speed_overrides"].([]any)
	if !ok || len(speeds) != 1 {
		t.Fatalf("null set overwrote remote speed_overrides: %v",
			data["speed_overrides"])
	}
	if _, present := data["network_overrides"]; present {
		t.Fatal("null set should not write network_overrides")
	}
}

func Test_etherLightingModelToData_duplicateKeysRejected(t *testing.T) {
	ctx := context.Background()
	var diags diag.Diagnostics
	speed := etherLightingSpeedOverrideSet(t, ctx,
		[]settingEtherLightingSpeedOverrideModel{
			{Speed: types.StringValue("GbE"), ColorHex: types.StringValue("ff6c14")},
			{Speed: types.StringValue("GbE"), ColorHex: types.StringValue("0544ff")},
		})
	m := &settingEtherLightingModel{
		NetworkOverrides: types.SetNull(
			types.ObjectType{AttrTypes: etherLightingNetworkOverrideAttrTypes}),
		SpeedOverrides: speed,
	}
	data := map[string]any{}

	etherLightingModelToData(ctx, m, data, &diags)

	if !diags.HasError() {
		t.Fatal("duplicate speed keys with different colors must be rejected")
	}
}

func Test_etherLightingSettingToModel(t *testing.T) {
	ctx := context.Background()
	var diags diag.Diagnostics

	m := etherLightingSettingToModel(ctx, &settings.EtherLighting{
		NetworkOverrides: []settings.SettingEtherLightingNetworkOverrides{
			{Key: "5dbaa47ea7986c04d72d4f5e", RawColorHex: "0544ff"},
		},
		SpeedOverrides: []settings.SettingEtherLightingSpeedOverrides{
			{Key: "GbE", RawColorHex: "ff6c14"},
		},
	}, &diags)
	if diags.HasError() {
		t.Fatal(diags)
	}

	var nets []settingEtherLightingNetworkOverrideModel
	diags.Append(m.NetworkOverrides.ElementsAs(ctx, &nets, false)...)
	if len(nets) != 1 || nets[0].NetworkID.ValueString() != "5dbaa47ea7986c04d72d4f5e" ||
		nets[0].ColorHex.ValueString() != "0544ff" {
		t.Fatalf("network_overrides = %v", nets)
	}
	var speeds []settingEtherLightingSpeedOverrideModel
	diags.Append(m.SpeedOverrides.ElementsAs(ctx, &speeds, false)...)
	if len(speeds) != 1 || speeds[0].Speed.ValueString() != "GbE" ||
		speeds[0].ColorHex.ValueString() != "ff6c14" {
		t.Fatalf("speed_overrides = %v", speeds)
	}
}

func Test_settingResource_Schema_etherLighting(t *testing.T) {
	r := &settingResource{}
	var resp fwresource.SchemaResponse
	r.Schema(context.Background(), fwresource.SchemaRequest{}, &resp)
	if _, ok := resp.Schema.Attributes["ether_lighting"]; !ok {
		t.Fatal("schema is missing the ether_lighting section attribute")
	}
}
