package unifi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-timetypes/timetypes"
	"github.com/hashicorp/terraform-plugin-framework/types"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
	resource_setting "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_setting"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/resourcekit"
)

// TestRadiusAfterReceiveKeepsThePlansSecretWhenNamed pins radiusAfterReceive
// against the deleted radiusSettingToModel's own behaviour, read off
// setting_resource.go before the migration (git history) and reproduced
// here exactly: an unconfigured secret (prior null or unknown) always comes
// back null, and a configured one surfaces whatever Spec.ToModel already
// decoded off the wire -- the controller's own echo -- not the prior/plan
// string verbatim. The two subtests below are the deleted
// Test_settingResource_radiusSettingToModel's own two cases, ported.
func TestRadiusAfterReceiveKeepsThePlansSecretWhenNamed(t *testing.T) {
	ctx := context.Background()
	spec := radiusKitSpec()

	t.Run("nil secret plan produces null secret model", func(t *testing.T) {
		sdk := &settings.Radius{
			AccountingEnabled: true,
			Secret:            "remote-secret",
		}
		var model settingRadiusModel
		if diags := spec.ToModel(ctx, sdk, &model, ""); diags.HasError() {
			t.Fatalf("ToModel: %v", diags)
		}
		prior := settingRadiusModel{Secret: types.StringNull()}
		if diags := radiusAfterReceive(ctx, sdk, &model, prior); diags.HasError() {
			t.Fatalf("radiusAfterReceive: %v", diags)
		}
		if !model.Secret.IsNull() {
			t.Errorf("secret = %q, want null when the plan never named it (regardless of the "+
				"controller's live value)", model.Secret.ValueString())
		}
	})

	t.Run("non-null secret plan reflects remote value", func(t *testing.T) {
		sdk := &settings.Radius{Secret: "the-secret"}
		var model settingRadiusModel
		if diags := spec.ToModel(ctx, sdk, &model, ""); diags.HasError() {
			t.Fatalf("ToModel: %v", diags)
		}
		// prior names secret with a DIFFERENT string than the wire holds --
		// the point of this case is that radiusAfterReceive does not
		// restore "old"; it leaves the controller's own decoded echo alone.
		prior := settingRadiusModel{Secret: types.StringValue("old")}
		if diags := radiusAfterReceive(ctx, sdk, &model, prior); diags.HasError() {
			t.Fatalf("radiusAfterReceive: %v", diags)
		}
		if model.Secret.ValueString() != "the-secret" {
			t.Errorf("secret = %q, want %q (the controller's own echo, not the prior string)",
				model.Secret.ValueString(), "the-secret")
		}
	})
}

// TestRadiusSettingRoundTrip ports the deleted TestSettingBlocksRoundTrip-
// style coverage of radiusModelToSetting/radiusSettingToModel's "non-null
// fields overlay" case (Test_settingResource_radiusModelToSetting,
// setting_resource_test.go): model -> go-unifi setting -> model preserves
// every field, including interim_update_interval's GoDuration<->seconds
// conversion. It now drives the Spec's own ToSDK/ToModel instead of the
// deleted mappers. The base-unchanged-on-null-plan half of the old test is
// not ported: that is Int64PtrField/BoolField/StringField/DurationPtrField's
// own generic null-leaves-SDK-untouched behaviour (internal/resourcekit's
// own field tests), not anything radius-specific.
func TestRadiusSettingRoundTrip(t *testing.T) {
	ctx := context.Background()
	spec := radiusKitSpec()

	in := &settingRadiusModel{
		AccountingEnabled:     types.BoolValue(true),
		Enabled:               types.BoolValue(true),
		AcctPort:              types.Int64Value(1813),
		AuthPort:              types.Int64Value(1812),
		InterimUpdateInterval: timetypes.NewGoDurationValue(time.Hour),
		Secret:                types.StringValue("mysecret"),
	}
	sdk, diags := spec.ToSDK(ctx, in)
	if diags.HasError() {
		t.Fatalf("ToSDK: %v", diags)
	}
	if !sdk.AccountingEnabled || !sdk.Enabled {
		t.Errorf("ToSDK: AccountingEnabled/Enabled = %v/%v, want true/true",
			sdk.AccountingEnabled, sdk.Enabled)
	}
	if sdk.AcctPort == nil || *sdk.AcctPort != 1813 {
		t.Errorf("ToSDK: AcctPort = %v, want 1813", sdk.AcctPort)
	}
	if sdk.AuthPort == nil || *sdk.AuthPort != 1812 {
		t.Errorf("ToSDK: AuthPort = %v, want 1812", sdk.AuthPort)
	}
	if sdk.InterimUpdateInterval == nil || *sdk.InterimUpdateInterval != 3600 {
		t.Errorf("ToSDK: InterimUpdateInterval = %v, want 3600", sdk.InterimUpdateInterval)
	}
	if sdk.Secret != "mysecret" {
		t.Errorf("ToSDK: Secret = %q, want mysecret", sdk.Secret)
	}

	var out settingRadiusModel
	if diags := spec.ToModel(ctx, sdk, &out, ""); diags.HasError() {
		t.Fatalf("ToModel: %v", diags)
	}
	if !out.AccountingEnabled.ValueBool() || !out.Enabled.ValueBool() {
		t.Errorf("ToModel: AccountingEnabled/Enabled = %v/%v, want true/true",
			out.AccountingEnabled, out.Enabled)
	}
	if out.AcctPort.ValueInt64() != 1813 {
		t.Errorf("ToModel: AcctPort = %v, want 1813", out.AcctPort)
	}
	if out.AuthPort.ValueInt64() != 1812 {
		t.Errorf("ToModel: AuthPort = %v, want 1812", out.AuthPort)
	}
	gotDuration, diags := out.InterimUpdateInterval.ValueGoDuration()
	if diags.HasError() || gotDuration != time.Hour {
		t.Errorf("ToModel: InterimUpdateInterval = %v, want 1h0m0s", gotDuration)
	}
	if out.Secret.ValueString() != "mysecret" {
		t.Errorf("ToModel: Secret = %q, want mysecret", out.Secret.ValueString())
	}
}

// TestRadiusKitSpecConformance runs the same conformance instruments every
// other kit descriptor's test applies (see e.g. dns_record's case in
// descriptor_elide_test.go), scoped to radius's own nested schema rather
// than a whole resource's, since radius is one section of unifi_setting
// rather than a surface of its own.
func TestRadiusKitSpecConformance(t *testing.T) {
	ctx := context.Background()
	spec := radiusKitSpec()
	for _, problem := range resourcekit.WireNameProblems(spec) {
		t.Error(problem)
	}
	for _, problem := range resourcekit.NestedProblems(spec) {
		t.Error(problem)
	}
	built := radiusNestedSchema(ctx)
	for _, problem := range resourcekit.ElideProblems(spec, built) {
		t.Error(problem)
	}
	for _, problem := range resourcekit.ZeroReadProblems(spec, built) {
		t.Error(problem)
	}
}

// TestRadiusBackendUpdateFieldsSendsOnlyTheNamedWiresPlusKey is the unit
// half of radius's masked-write gate, shaped exactly like
// TestMgmtBackendUpdateFieldsSendsOnlyTheNamedWiresPlusKey
// (setting_mgmt_descriptor_test.go): it runs radiusKitBackend's UpdateFields
// closure -- the same one Configure wires into the live resource -- against
// an httptest server that keeps the raw, undecoded PUT body, and asserts it
// carries exactly the field the mask named plus "key": no force-emitted
// sibling (enabled and configure_whole_network/tunneled_reply, which carry
// no omitempty on settings.Radius).
func TestRadiusBackendUpdateFieldsSendsOnlyTheNamedWiresPlusKey(t *testing.T) {
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

	backend := radiusKitBackend(api)
	sdk := &settings.Radius{AccountingEnabled: true, Enabled: true}
	if _, err := backend.UpdateFields(context.Background(), "default", sdk, "accounting_enabled"); err != nil {
		t.Fatalf("UpdateFields: %v", err)
	}

	want := map[string]bool{"key": true, "accounting_enabled": true}
	if len(body) != len(want) {
		t.Fatalf("PUT body has %d key(s) %v, want exactly %v", len(body), keysOf(body), want)
	}
	for name := range want {
		if _, ok := body[name]; !ok {
			t.Errorf("PUT body is missing %q; got %v", name, keysOf(body))
		}
	}
	// enabled has no omitempty on settings.Radius -- an unmasked encode
	// would always carry it. Its absence here is the assertion this test
	// exists for.
	if _, ok := body["enabled"]; ok {
		t.Error(`PUT body carries "enabled", which the mask never named; ` +
			"the masked write is supposed to leave it out")
	}
}

// TestRadiusNestedSchemaHasExactlyItsAttributes guards radiusNestedSchema's
// type assertion against a generator regression: "radius" moving off
// SingleNestedAttribute would panic every conformance test above instead of
// naming the actual problem, so this pins the shape ahead of that.
func TestRadiusNestedSchemaHasExactlyItsAttributes(t *testing.T) {
	ctx := context.Background()
	built := resource_setting.SettingResourceSchema(ctx)
	if _, ok := built.Attributes["radius"]; !ok {
		t.Fatal(`the generated setting schema has no "radius" attribute`)
	}
	nested := radiusNestedSchema(ctx)
	if len(nested.Attributes) != 6 {
		t.Errorf("radius has %d attribute(s), want 6; update radiusKitSpec and this count together",
			len(nested.Attributes))
	}
}
