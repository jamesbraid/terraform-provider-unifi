package unifi

import (
	"context"
	"testing"

	fwresource "github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
)

func Test_globalNetworkModelToData(t *testing.T) {
	m := &settingGlobalNetworkModel{
		DefaultSecurityPosture: types.StringValue("ALLOW_ALL"),
	}
	// Pre-existing raw fields must be preserved, not clobbered.
	data := map[string]any{"unmodeled_field": "keep"}

	globalNetworkModelToData(m, data)

	if data["default_security_posture"] != "ALLOW_ALL" {
		t.Fatalf("default_security_posture = %v", data["default_security_posture"])
	}
	if data["unmodeled_field"] != "keep" {
		t.Fatal("unmodeled_field was clobbered")
	}
}

func Test_globalNetworkModelToData_nullLeavesRemoteValue(t *testing.T) {
	m := &settingGlobalNetworkModel{DefaultSecurityPosture: types.StringNull()}
	data := map[string]any{"default_security_posture": "ALLOW_ALL"}

	globalNetworkModelToData(m, data)

	if data["default_security_posture"] != "ALLOW_ALL" {
		t.Fatal("null posture overwrote remote value")
	}
}

func Test_globalNetworkSettingToModel(t *testing.T) {
	m := globalNetworkSettingToModel(&settings.GlobalNetwork{
		DefaultSecurityPosture: "ALLOW_ALL",
	})
	if m.DefaultSecurityPosture.ValueString() != "ALLOW_ALL" {
		t.Fatalf("default_security_posture = %v", m.DefaultSecurityPosture)
	}
	empty := globalNetworkSettingToModel(&settings.GlobalNetwork{})
	if !empty.DefaultSecurityPosture.IsNull() {
		t.Fatalf("empty posture should map to null, got %v", empty.DefaultSecurityPosture)
	}
}

func Test_settingResource_Schema_globalNetwork(t *testing.T) {
	r := &settingResource{}
	var resp fwresource.SchemaResponse
	r.Schema(context.Background(), fwresource.SchemaRequest{}, &resp)
	if _, ok := resp.Schema.Attributes["global_network"]; !ok {
		t.Fatal("schema is missing the global_network section attribute")
	}
}
