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

// TestSslInspectionBackendUpdateFieldsSendsOnlyTheNamedWiresPlusKey is the
// unit half of ssl_inspection's masked-write gate, shaped exactly like
// TestCountryBackendUpdateFieldsSendsOnlyTheNamedWiresPlusKey
// (setting_country_descriptor_test.go): it runs
// sslInspectionKitBackend's UpdateFields closure -- the same one Configure
// wires into the live resource -- against an httptest server that keeps
// the raw, undecoded PUT body, and asserts it carries exactly the field
// the mask named plus "key".
func TestSslInspectionBackendUpdateFieldsSendsOnlyTheNamedWiresPlusKey(t *testing.T) {
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

	backend := sslInspectionKitBackend(api)
	sdk := &settings.SslInspection{State: "simple"}
	if _, err := backend.UpdateFields(context.Background(), "default", sdk, "state"); err != nil {
		t.Fatalf("UpdateFields: %v", err)
	}

	want := map[string]bool{"key": true, "state": true}
	if len(body) != len(want) {
		t.Fatalf("PUT body has %d key(s) %v, want exactly %v", len(body), keysOf(body), want)
	}
	for name := range want {
		if _, ok := body[name]; !ok {
			t.Errorf("PUT body is missing %q; got %v", name, keysOf(body))
		}
	}
}

// TestSslInspectionKitSpecConformance runs the same conformance
// instruments every other kit descriptor's test applies (see
// setting_mgmt_descriptor_test.go's TestMgmtKitSpecConformance), scoped to
// ssl_inspection's own nested schema rather than a whole resource's, since
// ssl_inspection is one section of unifi_setting rather than a surface of
// its own.
func TestSslInspectionKitSpecConformance(t *testing.T) {
	ctx := context.Background()
	spec := sslInspectionKitSpec()
	for _, problem := range resourcekit.WireNameProblems(spec) {
		t.Error(problem)
	}
	for _, problem := range resourcekit.NestedProblems(spec) {
		t.Error(problem)
	}
	built := sslInspectionNestedSchema(ctx)
	for _, problem := range resourcekit.ElideProblems(spec, built) {
		t.Error(problem)
	}
	for _, problem := range resourcekit.ZeroReadProblems(spec, built) {
		t.Error(problem)
	}
}

// TestSslInspectionNestedSchemaHasExactlyItsAttributes guards
// sslInspectionNestedSchema's type assertion against a generator
// regression: "ssl_inspection" moving off SingleNestedAttribute would
// panic every conformance test above instead of naming the actual
// problem, so this pins the shape ahead of that.
func TestSslInspectionNestedSchemaHasExactlyItsAttributes(t *testing.T) {
	ctx := context.Background()
	built := resource_setting.SettingResourceSchema(ctx)
	if _, ok := built.Attributes["ssl_inspection"]; !ok {
		t.Fatal(`the generated setting schema has no "ssl_inspection" attribute`)
	}
	nested := sslInspectionNestedSchema(ctx)
	if len(nested.Attributes) != 1 {
		t.Errorf("ssl_inspection has %d attribute(s), want 1; update sslInspectionKitSpec and this count together",
			len(nested.Attributes))
	}
}
