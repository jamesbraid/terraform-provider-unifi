package unifi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	ui "github.com/ubiquiti-community/go-unifi/unifi"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
	resource_setting "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_setting"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/resourcekit"
)

// TestGlobalApBackendUpdateFieldsSendsOnlyTheNamedWiresPlusKey is the unit
// half of global_ap's masked-write gate, shaped exactly like
// TestCountryBackendUpdateFieldsSendsOnlyTheNamedWiresPlusKey
// (setting_country_descriptor_test.go): it runs globalApKitBackend's
// UpdateFields closure -- the same one Configure wires into the live
// resource -- against an httptest server that keeps the raw, undecoded PUT
// body, and asserts it carries exactly the field the mask named plus
// "key", not ng_tx_power_mode when only na_tx_power_mode changed.
func TestGlobalApBackendUpdateFieldsSendsOnlyTheNamedWiresPlusKey(t *testing.T) {
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

	backend := globalApKitBackend(api)
	sdk := &settings.GlobalAp{NaTxPowerMode: "auto", NgTxPowerMode: "medium"}
	if _, err := backend.UpdateFields(context.Background(), "default", sdk, "na_tx_power_mode"); err != nil {
		t.Fatalf("UpdateFields: %v", err)
	}

	want := map[string]bool{"key": true, "na_tx_power_mode": true}
	if len(body) != len(want) {
		t.Fatalf("PUT body has %d key(s) %v, want exactly %v", len(body), keysOf(body), want)
	}
	for name := range want {
		if _, ok := body[name]; !ok {
			t.Errorf("PUT body is missing %q; got %v", name, keysOf(body))
		}
	}
}

// TestGlobalApOmitsAZeroTheControllerRejects is global_ap's own local
// version of the OmitZeroProblems census, the same shape
// TestNetflowOmitsAZeroTheControllerRejects records: unifi_setting is not
// walked by TestEveryKitSurfaceOmitsAZeroTheControllerRejects
// (omit_zero_census_test.go), which only reaches resources implementing
// OmitZeroProblems() directly -- settingResource does not -- so this
// section gates its own four Int64PtrFields against
// settings.FieldConstraints by hand.
func TestGlobalApOmitsAZeroTheControllerRejects(t *testing.T) {
	constraints := make(map[string]ui.FieldConstraint, len(settings.FieldConstraints["SettingGlobalAp"]))
	for wire, constraint := range settings.FieldConstraints["SettingGlobalAp"] {
		constraints[wire] = ui.FieldConstraint(constraint)
	}
	for _, problem := range resourcekit.OmitZeroProblems(globalApKitSpec(), constraints, nil) {
		t.Error(problem)
	}
}

// TestGlobalApNestedSchemaHasExactlyItsAttributes guards
// globalApNestedSchema's type assertion against a generator regression:
// "global_ap" moving off SingleNestedAttribute would panic every
// conformance test above instead of naming the actual problem, so this
// pins the shape ahead of that.
func TestGlobalApNestedSchemaHasExactlyItsAttributes(t *testing.T) {
	ctx := context.Background()
	built := resource_setting.SettingResourceSchema(ctx)
	if _, ok := built.Attributes["global_ap"]; !ok {
		t.Fatal(`the generated setting schema has no "global_ap" attribute`)
	}
	nested := globalApNestedSchema(ctx)
	if len(nested.Attributes) != 7 {
		t.Errorf("global_ap has %d attribute(s), want 7; update globalApKitSpec and this count together",
			len(nested.Attributes))
	}
}
