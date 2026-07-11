package unifi

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	fwresource "github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
)

func Test_mdnsModelToData(t *testing.T) {
	ctx := context.Background()
	var diags diag.Diagnostics

	svc, d := types.ObjectValueFrom(ctx, mdnsCustomServiceAttrTypes,
		settingMdnsCustomServiceModel{
			Name:    types.StringValue("Home Assistant"),
			Address: types.StringValue("_home-assistant._tcp"),
		})
	if d.HasError() {
		t.Fatal(d)
	}
	custom, d := types.SetValueFrom(ctx,
		types.ObjectType{AttrTypes: mdnsCustomServiceAttrTypes},
		[]types.Object{svc})
	if d.HasError() {
		t.Fatal(d)
	}
	predefined, d := types.SetValueFrom(ctx, types.StringType,
		[]string{"apple_airPlay"})
	if d.HasError() {
		t.Fatal(d)
	}

	m := &settingMdnsModel{
		Mode:               types.StringValue("custom"),
		CustomServices:     custom,
		PredefinedServices: predefined,
	}

	// The live controller carries fields go-unifi does not model
	// (enabled_for, enabled_for_network_ids); the raw merge must preserve
	// them verbatim.
	data := map[string]any{
		"enabled_for":             "some",
		"enabled_for_network_ids": []any{"6068a1508bf47808f667f3e8"},
	}

	mdnsModelToData(ctx, m, data, &diags)
	if diags.HasError() {
		t.Fatal(diags)
	}

	if data["mode"] != "custom" {
		t.Fatalf("mode = %v", data["mode"])
	}
	if data["enabled_for"] != "some" {
		t.Fatal("unmodeled enabled_for was clobbered")
	}
	ids, ok := data["enabled_for_network_ids"].([]any)
	if !ok || len(ids) != 1 {
		t.Fatalf("unmodeled enabled_for_network_ids was clobbered: %v",
			data["enabled_for_network_ids"])
	}
	svcs, ok := data["custom_services"].([]map[string]any)
	if !ok || len(svcs) != 1 || svcs[0]["address"] != "_home-assistant._tcp" ||
		svcs[0]["name"] != "Home Assistant" {
		t.Fatalf("custom_services = %v", data["custom_services"])
	}
	codes, ok := data["predefined_services"].([]map[string]any)
	if !ok || len(codes) != 1 || codes[0]["code"] != "apple_airPlay" {
		t.Fatalf("predefined_services = %v", data["predefined_services"])
	}
}

func Test_mdnsModelToData_nullsLeaveRemoteValues(t *testing.T) {
	ctx := context.Background()
	var diags diag.Diagnostics
	m := &settingMdnsModel{
		Mode: types.StringNull(),
		CustomServices: types.SetNull(
			types.ObjectType{AttrTypes: mdnsCustomServiceAttrTypes}),
		PredefinedServices: types.SetNull(types.StringType),
	}
	data := map[string]any{"mode": "all"}

	mdnsModelToData(ctx, m, data, &diags)
	if diags.HasError() {
		t.Fatal(diags)
	}

	if data["mode"] != "all" {
		t.Fatalf("null mode overwrote remote value: %v", data["mode"])
	}
	if _, present := data["custom_services"]; present {
		t.Fatal("null set should not write custom_services")
	}
	if _, present := data["predefined_services"]; present {
		t.Fatal("null set should not write predefined_services")
	}
}

func Test_mdnsSettingToModel(t *testing.T) {
	ctx := context.Background()
	var diags diag.Diagnostics

	m := mdnsSettingToModel(ctx, &settings.Mdns{
		Mode: "custom",
		CustomServices: []settings.SettingMdnsCustomServices{
			{Name: "Home Assistant", Address: "_home-assistant._tcp"},
		},
		PredefinedServices: []settings.SettingMdnsPredefinedServices{
			{Code: "sonos"},
		},
	}, &diags)
	if diags.HasError() {
		t.Fatal(diags)
	}

	if m.Mode.ValueString() != "custom" {
		t.Fatalf("mode = %v", m.Mode)
	}
	var svcs []settingMdnsCustomServiceModel
	diags.Append(m.CustomServices.ElementsAs(ctx, &svcs, false)...)
	if len(svcs) != 1 || svcs[0].Address.ValueString() != "_home-assistant._tcp" {
		t.Fatalf("custom_services = %v", svcs)
	}
	var codes []string
	diags.Append(m.PredefinedServices.ElementsAs(ctx, &codes, false)...)
	if len(codes) != 1 || codes[0] != "sonos" {
		t.Fatalf("predefined_services = %v", codes)
	}

	empty := mdnsSettingToModel(ctx, &settings.Mdns{}, &diags)
	if !empty.Mode.IsNull() {
		t.Fatalf("empty mode should map to null, got %v", empty.Mode)
	}
}

func Test_settingResource_Schema_mdns(t *testing.T) {
	r := &settingResource{}
	var resp fwresource.SchemaResponse
	r.Schema(context.Background(), fwresource.SchemaRequest{}, &resp)
	if _, ok := resp.Schema.Attributes["mdns"]; !ok {
		t.Fatal("schema is missing the mdns section attribute")
	}
}
