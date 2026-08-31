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

// TestTrafficFlowBackendUpdateFieldsSendsOnlyTheNamedWiresPlusKey is the
// unit half of traffic_flow's masked-write gate, shaped exactly like
// TestCountryBackendUpdateFieldsSendsOnlyTheNamedWiresPlusKey
// (setting_country_descriptor_test.go): it runs trafficFlowKitBackend's
// UpdateFields closure -- the same one Configure wires into the live
// resource -- against an httptest server that keeps the raw, undecoded PUT
// body, and asserts it carries exactly the fields the mask named plus
// "key", not all four of settings.TrafficFlow's force-emitted bools.
func TestTrafficFlowBackendUpdateFieldsSendsOnlyTheNamedWiresPlusKey(t *testing.T) {
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

	backend := trafficFlowKitBackend(api)
	sdk := &settings.TrafficFlow{EnabledAllowedTraffic: true, UnifiServicesEnabled: true}
	if _, err := backend.UpdateFields(
		context.Background(), "default", sdk, "enabled_allowed_traffic", "unifi_services_enabled",
	); err != nil {
		t.Fatalf("UpdateFields: %v", err)
	}

	want := map[string]bool{"key": true, "enabled_allowed_traffic": true, "unifi_services_enabled": true}
	if len(body) != len(want) {
		t.Fatalf("PUT body has %d key(s) %v, want exactly %v", len(body), keysOf(body), want)
	}
	for name := range want {
		if _, ok := body[name]; !ok {
			t.Errorf("PUT body is missing %q; got %v", name, keysOf(body))
		}
	}
}

// TestTrafficFlowKitSpecConformance runs the same conformance instruments
// every other kit descriptor's test applies (see
// setting_mgmt_descriptor_test.go's TestMgmtKitSpecConformance), scoped to
// traffic_flow's own nested schema rather than a whole resource's, since
// traffic_flow is one section of unifi_setting rather than a surface of
// its own.
func TestTrafficFlowKitSpecConformance(t *testing.T) {
	ctx := context.Background()
	spec := trafficFlowKitSpec()
	for _, problem := range resourcekit.WireNameProblems(spec) {
		t.Error(problem)
	}
	for _, problem := range resourcekit.NestedProblems(spec) {
		t.Error(problem)
	}
	built := trafficFlowNestedSchema(ctx)
	for _, problem := range resourcekit.ElideProblems(spec, built) {
		t.Error(problem)
	}
	for _, problem := range resourcekit.ZeroReadProblems(spec, built) {
		t.Error(problem)
	}
}

// TestTrafficFlowNestedSchemaHasExactlyItsAttributes guards
// trafficFlowNestedSchema's type assertion against a generator regression:
// "traffic_flow" moving off SingleNestedAttribute would panic every
// conformance test above instead of naming the actual problem, so this
// pins the shape ahead of that.
func TestTrafficFlowNestedSchemaHasExactlyItsAttributes(t *testing.T) {
	ctx := context.Background()
	built := resource_setting.SettingResourceSchema(ctx)
	if _, ok := built.Attributes["traffic_flow"]; !ok {
		t.Fatal(`the generated setting schema has no "traffic_flow" attribute`)
	}
	nested := trafficFlowNestedSchema(ctx)
	if len(nested.Attributes) != 4 {
		t.Errorf("traffic_flow has %d attribute(s), want 4; update trafficFlowKitSpec and this count together",
			len(nested.Attributes))
	}
}
