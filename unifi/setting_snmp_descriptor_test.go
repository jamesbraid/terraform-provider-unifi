package unifi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
	resource_setting "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_setting"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/resourcekit"
)

// TestSnmpAfterReceiveKeepsThePlansSecretWhenNamed pins snmpAfterReceive
// against the shape Task 0's live-controller probe measured: the controller
// echoes community and x_password back verbatim (no mask, no hash), so
// snmpAfterReceive's only job is the same plan-conditioned null
// radiusAfterReceive applies to radius.secret -- an unconfigured secret
// (prior null or unknown) always comes back null, and a configured one
// surfaces whatever Spec.ToModel already decoded off the wire, not the
// prior/plan string verbatim. Both secrets get their own subtest pair; the
// two are independent (each nulled on its own prior), so a case that names
// one and not the other is included too.
func TestSnmpAfterReceiveKeepsThePlansSecretWhenNamed(t *testing.T) {
	ctx := context.Background()
	spec := snmpKitSpec()

	t.Run("nil community and password plan produces null community and password model", func(t *testing.T) {
		sdk := &settings.Snmp{
			Community: "remote-community",
			Password:  "remote-password",
			Username:  "remote-user",
		}
		var model settingSnmpModel
		if diags := spec.ToModel(ctx, sdk, &model, ""); diags.HasError() {
			t.Fatalf("ToModel: %v", diags)
		}
		prior := settingSnmpModel{Community: types.StringNull(), Password: types.StringNull()}
		if diags := snmpAfterReceive(ctx, sdk, &model, prior); diags.HasError() {
			t.Fatalf("snmpAfterReceive: %v", diags)
		}
		if !model.Community.IsNull() {
			t.Errorf("community = %q, want null when the plan never named it", model.Community.ValueString())
		}
		if !model.Password.IsNull() {
			t.Errorf("password = %q, want null when the plan never named it", model.Password.ValueString())
		}
		// username carries no AfterReceive treatment: it is not a secret, so
		// the ordinary unconditional-mirror read stands even though the
		// plan named neither secret.
		if model.Username.ValueString() != "remote-user" {
			t.Errorf("username = %q, want %q (username is not plan-conditioned)",
				model.Username.ValueString(), "remote-user")
		}
	})

	t.Run("non-null community and password plan reflects the controller's own echo", func(t *testing.T) {
		sdk := &settings.Snmp{Community: "the-community", Password: "the-password"}
		var model settingSnmpModel
		if diags := spec.ToModel(ctx, sdk, &model, ""); diags.HasError() {
			t.Fatalf("ToModel: %v", diags)
		}
		// prior names both secrets with DIFFERENT strings than the wire
		// holds -- the point of this case is that snmpAfterReceive does not
		// restore "old"; it leaves the controller's own decoded echo alone,
		// the same distinction radiusAfterReceive's own comment makes.
		prior := settingSnmpModel{
			Community: types.StringValue("old-community"),
			Password:  types.StringValue("old-password"),
		}
		if diags := snmpAfterReceive(ctx, sdk, &model, prior); diags.HasError() {
			t.Fatalf("snmpAfterReceive: %v", diags)
		}
		if model.Community.ValueString() != "the-community" {
			t.Errorf("community = %q, want %q (the controller's own echo, not the prior string)",
				model.Community.ValueString(), "the-community")
		}
		if model.Password.ValueString() != "the-password" {
			t.Errorf("password = %q, want %q (the controller's own echo, not the prior string)",
				model.Password.ValueString(), "the-password")
		}
	})

	t.Run("only community named nulls password alone", func(t *testing.T) {
		sdk := &settings.Snmp{Community: "the-community", Password: "the-password"}
		var model settingSnmpModel
		if diags := spec.ToModel(ctx, sdk, &model, ""); diags.HasError() {
			t.Fatalf("ToModel: %v", diags)
		}
		prior := settingSnmpModel{
			Community: types.StringValue("the-community"),
			Password:  types.StringNull(),
		}
		if diags := snmpAfterReceive(ctx, sdk, &model, prior); diags.HasError() {
			t.Fatalf("snmpAfterReceive: %v", diags)
		}
		if model.Community.ValueString() != "the-community" {
			t.Errorf("community = %q, want %q", model.Community.ValueString(), "the-community")
		}
		if !model.Password.IsNull() {
			t.Errorf("password = %q, want null (the plan never named it, independent of community)",
				model.Password.ValueString())
		}
	})
}

// TestSnmpSettingRoundTrip drives snmpKitSpec's own ToSDK/ToModel over every
// field, the same shape TestRadiusSettingRoundTrip pins for radius.
func TestSnmpSettingRoundTrip(t *testing.T) {
	ctx := context.Background()
	spec := snmpKitSpec()

	in := &settingSnmpModel{
		Community: types.StringValue("my-community"),
		Enabled:   types.BoolValue(true),
		EnabledV3: types.BoolValue(true),
		Password:  types.StringValue("my-password"),
		Username:  types.StringValue("my-user"),
	}
	sdk, diags := spec.ToSDK(ctx, in)
	if diags.HasError() {
		t.Fatalf("ToSDK: %v", diags)
	}
	if sdk.Community != "my-community" {
		t.Errorf("ToSDK: Community = %q, want my-community", sdk.Community)
	}
	if !sdk.Enabled || !sdk.EnabledV3 {
		t.Errorf("ToSDK: Enabled/EnabledV3 = %v/%v, want true/true", sdk.Enabled, sdk.EnabledV3)
	}
	if sdk.Password != "my-password" {
		t.Errorf("ToSDK: Password = %q, want my-password", sdk.Password)
	}
	if sdk.Username != "my-user" {
		t.Errorf("ToSDK: Username = %q, want my-user", sdk.Username)
	}

	var out settingSnmpModel
	if diags := spec.ToModel(ctx, sdk, &out, ""); diags.HasError() {
		t.Fatalf("ToModel: %v", diags)
	}
	if out.Community.ValueString() != "my-community" {
		t.Errorf("ToModel: Community = %q, want my-community", out.Community.ValueString())
	}
	if !out.Enabled.ValueBool() || !out.EnabledV3.ValueBool() {
		t.Errorf("ToModel: Enabled/EnabledV3 = %v/%v, want true/true", out.Enabled, out.EnabledV3)
	}
	if out.Password.ValueString() != "my-password" {
		t.Errorf("ToModel: Password = %q, want my-password", out.Password.ValueString())
	}
	if out.Username.ValueString() != "my-user" {
		t.Errorf("ToModel: Username = %q, want my-user", out.Username.ValueString())
	}
}

// TestSnmpKitSpecConformance runs the same conformance instruments every
// other kit descriptor's test applies, scoped to snmp's own nested schema.
func TestSnmpKitSpecConformance(t *testing.T) {
	ctx := context.Background()
	spec := snmpKitSpec()
	for _, problem := range resourcekit.WireNameProblems(spec) {
		t.Error(problem)
	}
	for _, problem := range resourcekit.NestedProblems(spec) {
		t.Error(problem)
	}
	built := snmpNestedSchema(ctx)
	for _, problem := range resourcekit.ElideProblems(spec, built) {
		t.Error(problem)
	}
	for _, problem := range resourcekit.ZeroReadProblems(spec, built) {
		t.Error(problem)
	}
}

// TestSnmpBackendUpdateFieldsSendsOnlyTheNamedWiresPlusKey is the unit half
// of snmp's masked-write gate, shaped exactly like
// TestRadiusBackendUpdateFieldsSendsOnlyTheNamedWiresPlusKey: it runs
// snmpKitBackend's UpdateFields closure -- the same one Configure wires into
// the live resource -- against an httptest server that keeps the raw,
// undecoded PUT body, and asserts it carries exactly the field the mask
// named plus "key", with no force-emitted sibling.
func TestSnmpBackendUpdateFieldsSendsOnlyTheNamedWiresPlusKey(t *testing.T) {
	var body map[string]json.RawMessage
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/proxy/network/status" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"meta":{"server_version":"10.4.57"}}`))
			return
		}
		raw, _ := io.ReadAll(req.Body)
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Errorf("the provider sent a body that is not an object: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(append(append([]byte(`{"data":[`), raw...), []byte(`]}`)...))
	}))
	t.Cleanup(server.Close)

	api, err := ui.New(context.Background(), &ui.Config{BaseURL: server.URL, APIKey: "test-key"})
	if err != nil {
		t.Fatalf("create the API client: %v", err)
	}

	backend := snmpKitBackend(api)
	sdk := &settings.Snmp{Community: "my-community", Enabled: true}
	if _, err := backend.UpdateFields(context.Background(), "default", sdk, "community"); err != nil {
		t.Fatalf("UpdateFields: %v", err)
	}

	want := map[string]bool{"key": true, "community": true}
	if len(body) != len(want) {
		t.Fatalf("PUT body has %d key(s) %v, want exactly %v", len(body), keysOf(body), want)
	}
	for name := range want {
		if _, ok := body[name]; !ok {
			t.Errorf("PUT body is missing %q; got %v", name, keysOf(body))
		}
	}
	// enabled has no omitempty on settings.Snmp -- an unmasked encode would
	// always carry it. Its absence here is the assertion this test exists
	// for.
	if _, ok := body["enabled"]; ok {
		t.Error(`PUT body carries "enabled", which the mask never named; ` +
			"the masked write is supposed to leave it out")
	}
}

// TestSnmpNestedSchemaHasExactlyItsAttributes guards snmpNestedSchema's type
// assertion against a generator regression: "snmp" moving off
// SingleNestedAttribute would panic every conformance test above instead of
// naming the actual problem, so this pins the shape ahead of that.
func TestSnmpNestedSchemaHasExactlyItsAttributes(t *testing.T) {
	ctx := context.Background()
	built := resource_setting.SettingResourceSchema(ctx)
	if _, ok := built.Attributes["snmp"]; !ok {
		t.Fatal(`the generated setting schema has no "snmp" attribute`)
	}
	nested := snmpNestedSchema(ctx)
	if len(nested.Attributes) != 5 {
		t.Errorf("snmp has %d attribute(s), want 5; update snmpKitSpec and this count together",
			len(nested.Attributes))
	}
}
