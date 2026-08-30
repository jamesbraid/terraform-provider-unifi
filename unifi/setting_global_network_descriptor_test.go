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

// TestGlobalNetworkBackendUpdateFieldsSendsOnlyTheNamedWiresPlusKey is the
// unit half of global_network's masked-write gate, shaped exactly like
// TestCountryBackendUpdateFieldsSendsOnlyTheNamedWiresPlusKey
// (setting_country_descriptor_test.go): it runs globalNetworkKitBackend's
// UpdateFields closure -- the same one Configure wires into the live
// resource -- against an httptest server that keeps the raw, undecoded PUT
// body, and asserts it carries exactly the field the mask named plus "key".
func TestGlobalNetworkBackendUpdateFieldsSendsOnlyTheNamedWiresPlusKey(t *testing.T) {
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

	backend := globalNetworkKitBackend(api)
	sdk := &settings.GlobalNetwork{DefaultSecurityPosture: "ALLOW_ALL"}
	if _, err := backend.UpdateFields(
		context.Background(), "default", sdk, "default_security_posture",
	); err != nil {
		t.Fatalf("UpdateFields: %v", err)
	}

	want := map[string]bool{"key": true, "default_security_posture": true}
	if len(body) != len(want) {
		t.Fatalf("PUT body has %d key(s) %v, want exactly %v", len(body), keysOf(body), want)
	}
	for name := range want {
		if _, ok := body[name]; !ok {
			t.Errorf("PUT body is missing %q; got %v", name, keysOf(body))
		}
	}
}

// TestGlobalNetworkKitSpecConformance runs the same conformance instruments
// every other kit descriptor's test applies (see
// setting_mgmt_descriptor_test.go's TestMgmtKitSpecConformance), scoped to
// global_network's own nested schema rather than a whole resource's, since
// global_network is one section of unifi_setting rather than a surface of
// its own.
func TestGlobalNetworkKitSpecConformance(t *testing.T) {
	ctx := context.Background()
	spec := globalNetworkKitSpec()
	for _, problem := range resourcekit.WireNameProblems(spec) {
		t.Error(problem)
	}
	for _, problem := range resourcekit.NestedProblems(spec) {
		t.Error(problem)
	}
	built := globalNetworkNestedSchema(ctx)
	for _, problem := range resourcekit.ElideProblems(spec, built) {
		t.Error(problem)
	}
	for _, problem := range resourcekit.ZeroReadProblems(spec, built) {
		t.Error(problem)
	}
}

// TestGlobalNetworkNestedSchemaHasExactlyItsAttributes guards
// globalNetworkNestedSchema's type assertion against a generator
// regression: "global_network" moving off SingleNestedAttribute would
// panic every conformance test above instead of naming the actual
// problem, so this pins the shape ahead of that.
func TestGlobalNetworkNestedSchemaHasExactlyItsAttributes(t *testing.T) {
	ctx := context.Background()
	built := resource_setting.SettingResourceSchema(ctx)
	if _, ok := built.Attributes["global_network"]; !ok {
		t.Fatal(`the generated setting schema has no "global_network" attribute`)
	}
	nested := globalNetworkNestedSchema(ctx)
	if len(nested.Attributes) != 1 {
		t.Errorf("global_network has %d attribute(s), want 1; update globalNetworkKitSpec and this count together",
			len(nested.Attributes))
	}
}
