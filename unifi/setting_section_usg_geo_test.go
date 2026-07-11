package unifi

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	fwresource "github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
)

func Test_usgGeoModelToData(t *testing.T) {
	ctx := context.Background()
	var diags diag.Diagnostics
	filtering, d := types.ObjectValueFrom(ctx, usgGeoIPFilteringAttrTypes,
		settingUsgGeoIPFilteringModel{
			Action:           types.StringNull(),
			Countries:        types.StringValue("NZ,AU"),
			Enabled:          types.BoolValue(true),
			TrafficDirection: types.StringValue("both"),
		})
	if d.HasError() {
		t.Fatal(d)
	}
	m := &settingUsgGeoModel{IPFiltering: filtering}

	// Both top-level and nested unmodeled fields must survive the merge, and
	// a null nested attribute must not clobber the remote nested value.
	data := map[string]any{
		"unmodeled_field": "keep",
		"ip_filtering": map[string]any{
			"action":           "block",
			"nested_unmodeled": true,
		},
	}

	usgGeoModelToData(ctx, m, data, &diags)
	if diags.HasError() {
		t.Fatal(diags)
	}

	if data["unmodeled_field"] != "keep" {
		t.Fatal("unmodeled_field was clobbered")
	}
	nested, ok := data["ip_filtering"].(map[string]any)
	if !ok {
		t.Fatalf("ip_filtering = %T", data["ip_filtering"])
	}
	if nested["action"] != "block" {
		t.Fatal("null action overwrote remote nested value")
	}
	if nested["nested_unmodeled"] != true {
		t.Fatal("nested unmodeled field was clobbered")
	}
	if nested["countries"] != "NZ,AU" || nested["enabled"] != true ||
		nested["traffic_direction"] != "both" {
		t.Fatalf("nested fields wrong: %v", nested)
	}
}

func Test_usgGeoModelToData_nullObjectLeavesRemoteValues(t *testing.T) {
	ctx := context.Background()
	var diags diag.Diagnostics
	m := &settingUsgGeoModel{IPFiltering: types.ObjectNull(usgGeoIPFilteringAttrTypes)}
	data := map[string]any{"ip_filtering": map[string]any{"action": "block"}}

	usgGeoModelToData(ctx, m, data, &diags)

	nested := data["ip_filtering"].(map[string]any)
	if nested["action"] != "block" {
		t.Fatal("null ip_filtering object overwrote remote values")
	}
}

func Test_usgGeoSettingToModel(t *testing.T) {
	ctx := context.Background()
	var diags diag.Diagnostics
	m := usgGeoSettingToModel(ctx, &settings.UsgGeo{
		IPFiltering: &settings.SettingUsgGeoIPFiltering{
			Action:           "block",
			Countries:        "NZ",
			Enabled:          true,
			TrafficDirection: "both",
		},
	}, &diags)
	if diags.HasError() {
		t.Fatal(diags)
	}
	var f settingUsgGeoIPFilteringModel
	diags.Append(m.IPFiltering.As(ctx, &f, basetypes.ObjectAsOptions{})...)
	if f.Action.ValueString() != "block" || f.Countries.ValueString() != "NZ" ||
		!f.Enabled.ValueBool() || f.TrafficDirection.ValueString() != "both" {
		t.Fatalf("ip_filtering = %+v", f)
	}

	empty := usgGeoSettingToModel(ctx, &settings.UsgGeo{}, &diags)
	if !empty.IPFiltering.IsNull() {
		t.Fatalf("nil IPFiltering should map to a null object, got %v", empty.IPFiltering)
	}
}

func Test_settingResource_Schema_usgGeo(t *testing.T) {
	r := &settingResource{}
	var resp fwresource.SchemaResponse
	r.Schema(context.Background(), fwresource.SchemaRequest{}, &resp)
	if _, ok := resp.Schema.Attributes["usg_geo"]; !ok {
		t.Fatal("schema is missing the usg_geo section attribute")
	}
}
