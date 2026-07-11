package unifi

import (
	"context"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	fwresource "github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
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

func TestAccSettingResource_usgGeo(t *testing.T) {
	// usg_geo requires a real gateway-class controller; the demo/simulation
	// controller rejects the section. Verified directly: GET returns an
	// empty {} document (unlike e.g. netflow, which is fully populated),
	// and PUT with a minimal payload (with or without "key") fails
	// identically with api.err.Invalid (400) — not a payload-shape bug in
	// this test's config.
	if os.Getenv("UNIFI_SKIP_CONTAINER") == "" {
		t.Skip("usg_geo requires a real controller; set UNIFI_SKIP_CONTAINER to run")
	}

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: `
resource "unifi_setting" "test" {
  usg_geo = {
    ip_filtering = {
      enabled           = false
      action            = "block"
      countries         = "NZ"
      traffic_direction = "both"
    }
  }
}
`,
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"unifi_setting.test", "usg_geo.ip_filtering.enabled", "false",
					),
					resource.TestCheckResourceAttr(
						"unifi_setting.test", "usg_geo.ip_filtering.countries", "NZ",
					),
				),
			},
		},
	})
}
