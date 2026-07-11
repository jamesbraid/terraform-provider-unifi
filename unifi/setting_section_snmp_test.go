package unifi

import (
	"context"
	"testing"

	fwresource "github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
)

func Test_snmpModelToData(t *testing.T) {
	m := &settingSnmpModel{
		Community: types.StringValue("tfacc-ro"),
		Enabled:   types.BoolValue(true),
		EnabledV3: types.BoolValue(true),
		Username:  types.StringValue("tfacc-user"),
		Password:  types.StringValue("tfacc-password-1"),
	}
	// Pre-existing raw fields must be preserved, not clobbered.
	data := map[string]any{"unmodeled_field": "keep"}

	snmpModelToData(m, data)

	if data["community"] != "tfacc-ro" {
		t.Fatalf("community = %v", data["community"])
	}
	if data["enabled"] != true {
		t.Fatalf("enabled = %v", data["enabled"])
	}
	// The controller document uses camelCase for this one key.
	if data["enabledV3"] != true {
		t.Fatalf("enabledV3 = %v", data["enabledV3"])
	}
	if _, present := data["enabled_v3"]; present {
		t.Fatal("wrote snake_case enabled_v3; controller key is enabledV3")
	}
	if data["username"] != "tfacc-user" {
		t.Fatalf("username = %v", data["username"])
	}
	if data["x_password"] != "tfacc-password-1" {
		t.Fatalf("x_password = %v", data["x_password"])
	}
	if _, present := data["password"]; present {
		t.Fatal("wrote bare password key; controller key is x_password")
	}
	if data["unmodeled_field"] != "keep" {
		t.Fatal("unmodeled_field was clobbered")
	}
}

func Test_snmpModelToData_nullsLeaveRemoteValues(t *testing.T) {
	m := &settingSnmpModel{
		Community: types.StringNull(),
		Enabled:   types.BoolNull(),
		EnabledV3: types.BoolNull(),
		Username:  types.StringNull(),
		Password:  types.StringNull(),
	}
	data := map[string]any{"community": "remote-value", "enabled": true}

	snmpModelToData(m, data)

	if data["community"] != "remote-value" {
		t.Fatalf("null community overwrote remote value: %v", data["community"])
	}
	if data["enabled"] != true {
		t.Fatalf("null enabled overwrote remote value: %v", data["enabled"])
	}
	for _, key := range []string{"enabledV3", "username", "x_password"} {
		if _, present := data[key]; present {
			t.Fatalf("null model wrote %s", key)
		}
	}
}

func Test_snmpSettingToModel(t *testing.T) {
	m := snmpSettingToModel(&settings.Snmp{
		Community: "tfacc-ro",
		Enabled:   true,
		EnabledV3: false,
		Username:  "tfacc-user",
		Password:  "controller-echoed-hash",
	})
	if m.Community.ValueString() != "tfacc-ro" {
		t.Fatalf("community = %v", m.Community)
	}
	if !m.Enabled.ValueBool() || m.EnabledV3.ValueBool() {
		t.Fatalf("bools wrong: enabled=%v enabled_v3=%v", m.Enabled, m.EnabledV3)
	}
	if m.Username.ValueString() != "tfacc-user" {
		t.Fatalf("username = %v", m.Username)
	}
	// x_password is write-only: whatever the controller returns is ignored.
	if !m.Password.IsNull() {
		t.Fatalf("password must never be read back, got %v", m.Password)
	}

	empty := snmpSettingToModel(&settings.Snmp{})
	if !empty.Community.IsNull() || !empty.Username.IsNull() {
		t.Fatalf("empty strings should map to null: %v / %v", empty.Community, empty.Username)
	}
}

func Test_preserveSnmpPassword(t *testing.T) {
	ctx := context.Background()

	mkObj := func(pw types.String) types.Object {
		obj, d := types.ObjectValueFrom(ctx, snmpAttrTypes, settingSnmpModel{
			Community: types.StringValue("tfacc-ro"),
			Enabled:   types.BoolValue(true),
			EnabledV3: types.BoolValue(false),
			Username:  types.StringNull(),
			Password:  pw,
		})
		if d.HasError() {
			t.Fatal(d)
		}
		return obj
	}

	prior := mkObj(types.StringValue("tfacc-password-1"))
	fresh := mkObj(types.StringNull())

	merged := preserveSnmpPassword(prior, fresh)
	pw := merged.Attributes()["password"].(types.String)
	if pw.ValueString() != "tfacc-password-1" {
		t.Fatalf("configured password not preserved: %v", pw)
	}

	// No prior object: the fresh read passes through untouched.
	if got := preserveSnmpPassword(types.ObjectNull(snmpAttrTypes), fresh); !got.Equal(fresh) {
		t.Fatalf("null prior should pass fresh through, got %v", got)
	}
	// Prior without a password: nothing to preserve.
	if got := preserveSnmpPassword(mkObj(types.StringNull()), fresh); !got.Equal(fresh) {
		t.Fatalf("passwordless prior should pass fresh through, got %v", got)
	}
}

func Test_settingResource_Schema_snmp(t *testing.T) {
	r := &settingResource{}
	var resp fwresource.SchemaResponse
	r.Schema(context.Background(), fwresource.SchemaRequest{}, &resp)
	attrRaw, ok := resp.Schema.Attributes["snmp"]
	if !ok {
		t.Fatal("schema is missing the snmp section attribute")
	}
	nested, ok := attrRaw.(schema.SingleNestedAttribute)
	if !ok {
		t.Fatalf("snmp attribute is %T, want SingleNestedAttribute", attrRaw)
	}
	for _, name := range []string{"community", "password"} {
		a, ok := nested.Attributes[name].(schema.StringAttribute)
		if !ok || !a.Sensitive {
			t.Fatalf("snmp.%s must be a Sensitive string attribute", name)
		}
	}
	if pw := nested.Attributes["password"].(schema.StringAttribute); pw.Computed {
		t.Fatal("snmp.password must not be Computed (write-only x_ field)")
	}
	if user, ok := nested.Attributes["username"].(schema.StringAttribute); !ok || user.Sensitive {
		t.Fatal("snmp.username must exist and not be Sensitive")
	}
}
