package unifi

import (
	"context"
	"testing"

	fwresource "github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
)

func Test_teleportModelToData(t *testing.T) {
	m := &settingTeleportModel{
		Enabled: types.BoolValue(true),
		Subnet:  types.StringValue("192.168.100.0/24"),
	}
	// Pre-existing raw fields must be preserved, not clobbered.
	data := map[string]any{"unmodeled_field": "keep"}

	teleportModelToData(m, data)

	if data["enabled"] != true {
		t.Fatalf("enabled = %v", data["enabled"])
	}
	if data["subnet_cidr"] != "192.168.100.0/24" {
		t.Fatalf("subnet_cidr = %v", data["subnet_cidr"])
	}
	if data["unmodeled_field"] != "keep" {
		t.Fatal("unmodeled_field was clobbered")
	}
}

func Test_teleportModelToData_nullsLeaveRemoteValues(t *testing.T) {
	m := &settingTeleportModel{
		Enabled: types.BoolNull(),
		Subnet:  types.StringNull(),
	}
	data := map[string]any{"enabled": true, "subnet_cidr": "192.168.2.1/24"}

	teleportModelToData(m, data)

	if data["enabled"] != true {
		t.Fatalf("null enabled overwrote remote value: %v", data["enabled"])
	}
	if data["subnet_cidr"] != "192.168.2.1/24" {
		t.Fatalf("null subnet overwrote remote value: %v", data["subnet_cidr"])
	}
}

func Test_teleportSettingToModel(t *testing.T) {
	m := teleportSettingToModel(&settings.Teleport{
		Enabled:    true,
		SubnetCidr: "192.168.2.1/24",
	})
	if !m.Enabled.ValueBool() {
		t.Fatalf("enabled = %v", m.Enabled)
	}
	if m.Subnet.ValueString() != "192.168.2.1/24" {
		t.Fatalf("subnet = %v", m.Subnet)
	}

	// Empty subnet is meaningful ("auto") and must read back as "" — not
	// null — so an explicit subnet = "" in config stays consistent.
	empty := teleportSettingToModel(&settings.Teleport{})
	if empty.Subnet.IsNull() || empty.Subnet.ValueString() != "" {
		t.Fatalf("empty subnet should be \"\", got %v", empty.Subnet)
	}
	if empty.Enabled.ValueBool() {
		t.Fatalf("enabled should be false, got %v", empty.Enabled)
	}
}

func Test_settingResource_Schema_teleport(t *testing.T) {
	r := &settingResource{}
	var resp fwresource.SchemaResponse
	r.Schema(context.Background(), fwresource.SchemaRequest{}, &resp)
	if _, ok := resp.Schema.Attributes["teleport"]; !ok {
		t.Fatal("schema is missing the teleport section attribute")
	}
}
