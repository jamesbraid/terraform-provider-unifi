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

// TestLcmSettingRoundTrip exercises the whole lcm struct through the Spec's
// own ToSDK/ToModel: model -> go-unifi setting -> model preserves the
// fields.
func TestLcmSettingRoundTrip(t *testing.T) {
	ctx := context.Background()
	spec := lcmKitSpec()

	in := &settingLcmModel{
		Brightness:  types.Int64Value(50),
		Enabled:     types.BoolValue(true),
		IdleTimeout: types.Int64Value(300),
		Sync:        types.BoolValue(true),
		TouchEvent:  types.BoolValue(false),
	}
	sdk, diags := spec.ToSDK(ctx, in)
	if diags.HasError() {
		t.Fatalf("ToSDK: %v", diags)
	}
	if sdk.Brightness == nil || *sdk.Brightness != 50 || !sdk.Enabled ||
		sdk.IDleTimeout == nil || *sdk.IDleTimeout != 300 || !sdk.Sync || sdk.TouchEvent {
		t.Fatalf("ToSDK = %+v, want brightness=50 enabled idle=300 sync !touch_event", sdk)
	}

	var out settingLcmModel
	if diags := spec.ToModel(ctx, sdk, &out, ""); diags.HasError() {
		t.Fatalf("ToModel: %v", diags)
	}
	if out.Brightness.ValueInt64() != 50 || !out.Enabled.ValueBool() ||
		out.IdleTimeout.ValueInt64() != 300 || !out.Sync.ValueBool() || out.TouchEvent.ValueBool() {
		t.Errorf("lcm round-trip mismatch: %+v", out)
	}
}

// TestLcmSpecOmitsAnUnsetBrightness ports the deleted TestLcmOmitsUnsetInts:
// the #288 guard, now Int64PtrField{OmitZero: true}. A null or unknown
// brightness/idle_timeout must never reach the wire as a pointer to zero --
// the controller rejects 0 as out of range for both (1-100, 10-3600). It
// now drives the Spec's own ToSDK instead of the deleted lcmModelToSetting.
func TestLcmSpecOmitsAnUnsetBrightness(t *testing.T) {
	ctx := context.Background()
	spec := lcmKitSpec()

	in := &settingLcmModel{
		Enabled:     types.BoolValue(true),
		Brightness:  types.Int64Null(),
		IdleTimeout: types.Int64Unknown(),
	}
	sdk, diags := spec.ToSDK(ctx, in)
	if diags.HasError() {
		t.Fatalf("ToSDK: %v", diags)
	}
	if sdk.Brightness != nil || sdk.IDleTimeout != nil {
		t.Errorf("unset lcm ints must be omitted: brightness=%v idle=%v",
			sdk.Brightness, sdk.IDleTimeout)
	}
}

// TestLcmBackendUpdateFieldsSendsOnlyTheNamedWiresPlusKey is the unit half
// of lcm's masked-write gate, shaped exactly like
// TestMgmtBackendUpdateFieldsSendsOnlyTheNamedWiresPlusKey
// (setting_mgmt_descriptor_test.go): it runs lcmKitBackend's UpdateFields
// closure -- the same one Configure wires into the live resource -- against
// an httptest server that keeps the raw, undecoded PUT body, and asserts it
// carries exactly the field the mask named plus "key": no force-emitted
// sibling (sync/touch_event, which carry no omitempty on settings.Lcm).
func TestLcmBackendUpdateFieldsSendsOnlyTheNamedWiresPlusKey(t *testing.T) {
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

	backend := lcmKitBackend(api)
	sdk := &settings.Lcm{Enabled: true, Sync: true, TouchEvent: true}
	if _, err := backend.UpdateFields(context.Background(), "default", sdk, "enabled"); err != nil {
		t.Fatalf("UpdateFields: %v", err)
	}

	want := map[string]bool{"key": true, "enabled": true}
	if len(body) != len(want) {
		t.Fatalf("PUT body has %d key(s) %v, want exactly %v", len(body), keysOf(body), want)
	}
	for name := range want {
		if _, ok := body[name]; !ok {
			t.Errorf("PUT body is missing %q; got %v", name, keysOf(body))
		}
	}
	// sync has no omitempty on settings.Lcm -- an unmasked encode would
	// always carry it. Its absence here is the assertion this test exists
	// for.
	if _, ok := body["sync"]; ok {
		t.Error(`PUT body carries "sync", which the mask never named; ` +
			"the masked write is supposed to leave it out")
	}
}

// TestLcmKitSpecConformance runs the same conformance instruments every
// other kit descriptor's test applies (see e.g. dns_record's case in
// descriptor_elide_test.go), scoped to lcm's own nested schema rather than a
// whole resource's, since lcm is one section of unifi_setting rather than a
// surface of its own.
func TestLcmKitSpecConformance(t *testing.T) {
	ctx := context.Background()
	spec := lcmKitSpec()
	for _, problem := range resourcekit.WireNameProblems(spec) {
		t.Error(problem)
	}
	for _, problem := range resourcekit.NestedProblems(spec) {
		t.Error(problem)
	}
	built := lcmNestedSchema(ctx)
	for _, problem := range resourcekit.ElideProblems(spec, built) {
		t.Error(problem)
	}
	for _, problem := range resourcekit.ZeroReadProblems(spec, built) {
		t.Error(problem)
	}
}

// TestLcmNestedSchemaHasExactlyItsAttributes guards lcmNestedSchema's type
// assertion against a generator regression: "lcm" moving off
// SingleNestedAttribute would panic every conformance test above instead of
// naming the actual problem, so this pins the shape ahead of that.
func TestLcmNestedSchemaHasExactlyItsAttributes(t *testing.T) {
	ctx := context.Background()
	built := resource_setting.SettingResourceSchema(ctx)
	if _, ok := built.Attributes["lcm"]; !ok {
		t.Fatal(`the generated setting schema has no "lcm" attribute`)
	}
	nested := lcmNestedSchema(ctx)
	if len(nested.Attributes) != 5 {
		t.Errorf("lcm has %d attribute(s), want 5; update lcmKitSpec and this count together",
			len(nested.Attributes))
	}
}
