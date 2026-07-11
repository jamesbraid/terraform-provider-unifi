package unifi

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
)

func Test_globalNatModelToData(t *testing.T) {
	ctx := context.Background()
	var diags diag.Diagnostics
	excluded, d := types.SetValueFrom(ctx, types.StringType, []string{"abc123"})
	if d.HasError() {
		t.Fatal(d)
	}
	m := &settingGlobalNatModel{
		Mode:               types.StringValue("auto"),
		ExcludedNetworkIDs: excluded,
	}
	data := map[string]any{"unmodeled_field": "keep"}

	globalNatModelToData(ctx, m, data, &diags)

	if diags.HasError() {
		t.Fatal(diags)
	}
	if data["mode"] != "auto" {
		t.Fatalf("mode = %v", data["mode"])
	}
	ids, ok := data["excluded_network_ids"].([]string)
	if !ok || len(ids) != 1 || ids[0] != "abc123" {
		t.Fatalf("excluded_network_ids = %v", data["excluded_network_ids"])
	}
	if data["unmodeled_field"] != "keep" {
		t.Fatal("unmodeled_field was clobbered")
	}
}

func Test_globalNatModelToData_nullsLeaveRemoteValues(t *testing.T) {
	ctx := context.Background()
	var diags diag.Diagnostics
	m := &settingGlobalNatModel{
		Mode:               types.StringNull(),
		ExcludedNetworkIDs: types.SetNull(types.StringType),
	}
	data := map[string]any{"mode": "custom"}

	globalNatModelToData(ctx, m, data, &diags)

	if data["mode"] != "custom" {
		t.Fatalf("null mode overwrote remote value: %v", data["mode"])
	}
	if _, present := data["excluded_network_ids"]; present {
		t.Fatal("null set should not write excluded_network_ids")
	}
}

func Test_globalNatSettingToModel(t *testing.T) {
	ctx := context.Background()
	var diags diag.Diagnostics
	m := globalNatSettingToModel(ctx, &settings.GlobalNat{
		Mode:               "auto",
		ExcludedNetworkIDs: []string{"abc123"},
	}, &diags)
	if diags.HasError() {
		t.Fatal(diags)
	}
	if m.Mode.ValueString() != "auto" {
		t.Fatalf("mode = %v", m.Mode)
	}
	var ids []string
	diags.Append(m.ExcludedNetworkIDs.ElementsAs(ctx, &ids, false)...)
	if len(ids) != 1 || ids[0] != "abc123" {
		t.Fatalf("ids = %v", ids)
	}
}
