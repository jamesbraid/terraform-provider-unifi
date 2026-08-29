package unifi

import (
	"context"
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

// TestAutoSpeedtestSettingRoundTrip ports the deleted
// TestAutoSpeedtestSettingRoundTrip (setting_resource_test.go), which
// exercised autoSpeedtestModelToSetting/autoSpeedtestSettingToModel
// directly: model -> go-unifi setting -> model preserves the fields. It now
// drives the Spec's own ToSDK/ToModel instead of the deleted mappers.
func TestAutoSpeedtestSettingRoundTrip(t *testing.T) {
	ctx := context.Background()
	spec := autoSpeedtestKitSpec()

	in := &settingAutoSpeedtestModel{
		Enabled:  types.BoolValue(true),
		CronExpr: types.StringValue("0 3 * * *"),
	}
	setting, diags := spec.ToSDK(ctx, in)
	if diags.HasError() {
		t.Fatalf("ToSDK: %v", diags)
	}
	if !setting.Enabled || setting.CronExpr != "0 3 * * *" {
		t.Fatalf("ToSDK = %+v, want enabled cron=0 3 * * *", setting)
	}

	var out settingAutoSpeedtestModel
	if diags := spec.ToModel(ctx, setting, &out, ""); diags.HasError() {
		t.Fatalf("ToModel: %v", diags)
	}
	if !out.Enabled.ValueBool() || out.CronExpr.ValueString() != "0 3 * * *" {
		t.Errorf("ToModel = %+v, want enabled cron preserved", out)
	}
}

// TestAutoSpeedtestBackendUpdateFieldsSendsOnlyTheNamedWiresPlusKey is the
// unit half of auto_speedtest's masked-write gate, shaped exactly like
// TestMgmtBackendUpdateFieldsSendsOnlyTheNamedWiresPlusKey
// (setting_mgmt_descriptor_test.go): it runs autoSpeedtestKitBackend's
// UpdateFields closure -- the same one Configure wires into the live
// resource -- against an httptest server that keeps the raw, undecoded PUT
// body. Unlike the other three sections migrated alongside auto_speedtest,
// this is the ONLY test that exercises its masked write at all --
// TestAccSettingResource_autoSpeedtest is a structural skip in this
// environment (the emulated controller has no WAN uplink for a real speed
// test), so there is no live acceptance run backing this one up. The
// assertion is therefore the exact PUT body string, not just which keys are
// present: cron_expr is the only field named, so the body must be exactly
// that field plus "key" and nothing else, byte for byte.
func TestAutoSpeedtestBackendUpdateFieldsSendsOnlyTheNamedWiresPlusKey(t *testing.T) {
	var body []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/proxy/network/status" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"meta":{"server_version":"10.4.57"}}`))
			return
		}
		raw, _ := io.ReadAll(req.Body)
		body = raw
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(append(append([]byte(`{"data":[`), raw...), []byte(`]}`)...))
	}))
	t.Cleanup(server.Close)

	api, err := ui.New(context.Background(), &ui.Config{BaseURL: server.URL, APIKey: "test-key"})
	if err != nil {
		t.Fatalf("create the API client: %v", err)
	}

	backend := autoSpeedtestKitBackend(api)
	sdk := &settings.AutoSpeedtest{CronExpr: "0 3 * * *"}
	if _, err := backend.UpdateFields(context.Background(), "default", sdk, "cron_expr"); err != nil {
		t.Fatalf("UpdateFields: %v", err)
	}

	// enabled has no omitempty on settings.AutoSpeedtest -- an unmasked
	// encode would always carry it. Its absence, and the exact shape of
	// what remains, is what this byte-for-byte comparison exists to pin.
	want := `{"cron_expr":"0 3 * * *","key":"auto_speedtest"}`
	if string(body) != want {
		t.Fatalf("PUT body = %s, want exactly %s", body, want)
	}
}

// TestAutoSpeedtestKitSpecConformance runs the same conformance instruments
// every other kit descriptor's test applies (see setting_mgmt_descriptor_test.go's
// TestMgmtKitSpecConformance), scoped to auto_speedtest's own nested schema
// rather than a whole resource's, since auto_speedtest is one section of
// unifi_setting rather than a surface of its own.
func TestAutoSpeedtestKitSpecConformance(t *testing.T) {
	ctx := context.Background()
	spec := autoSpeedtestKitSpec()
	for _, problem := range resourcekit.WireNameProblems(spec) {
		t.Error(problem)
	}
	for _, problem := range resourcekit.NestedProblems(spec) {
		t.Error(problem)
	}
	built := autoSpeedtestNestedSchema(ctx)
	for _, problem := range resourcekit.ElideProblems(spec, built) {
		t.Error(problem)
	}
	for _, problem := range resourcekit.ZeroReadProblems(spec, built) {
		t.Error(problem)
	}
}

// TestAutoSpeedtestNestedSchemaHasExactlyItsAttributes guards
// autoSpeedtestNestedSchema's type assertion against a generator regression:
// "auto_speedtest" moving off SingleNestedAttribute would panic every
// conformance test above instead of naming the actual problem, so this pins
// the shape ahead of that.
func TestAutoSpeedtestNestedSchemaHasExactlyItsAttributes(t *testing.T) {
	ctx := context.Background()
	built := resource_setting.SettingResourceSchema(ctx)
	if _, ok := built.Attributes["auto_speedtest"]; !ok {
		t.Fatal(`the generated setting schema has no "auto_speedtest" attribute`)
	}
	nested := autoSpeedtestNestedSchema(ctx)
	if len(nested.Attributes) != 2 {
		t.Errorf("auto_speedtest has %d attribute(s), want 2; update autoSpeedtestKitSpec and this count together",
			len(nested.Attributes))
	}
}
