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

// TestNetflowBackendUpdateFieldsSendsOnlyTheNamedWiresPlusKey is the unit
// half of netflow's masked-write gate, shaped exactly like
// TestCountryBackendUpdateFieldsSendsOnlyTheNamedWiresPlusKey
// (setting_country_descriptor_test.go): it runs netflowKitBackend's
// UpdateFields closure -- the same one Configure wires into the live
// resource -- against an httptest server that keeps the raw, undecoded PUT
// body, and asserts it carries exactly the fields the mask named plus
// "key", not sampling_mode when only enabled changed.
func TestNetflowBackendUpdateFieldsSendsOnlyTheNamedWiresPlusKey(t *testing.T) {
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

	backend := netflowKitBackend(api)
	sdk := &settings.Netflow{Enabled: true, SamplingMode: "random"}
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
}

// TestNetflowKitSpecConformance runs the same conformance instruments every
// other kit descriptor's test applies (see setting_mgmt_descriptor_test.go's
// TestMgmtKitSpecConformance), scoped to netflow's own nested schema rather
// than a whole resource's, since netflow is one section of unifi_setting
// rather than a surface of its own.
func TestNetflowKitSpecConformance(t *testing.T) {
	ctx := context.Background()
	spec := netflowKitSpec()
	for _, problem := range resourcekit.WireNameProblems(spec) {
		t.Error(problem)
	}
	for _, problem := range resourcekit.NestedProblems(spec) {
		t.Error(problem)
	}
	built := netflowNestedSchema(ctx)
	for _, problem := range resourcekit.ElideProblems(spec, built) {
		t.Error(problem)
	}
	for _, problem := range resourcekit.ZeroReadProblems(spec, built) {
		t.Error(problem)
	}
}

// TestNetflowOmitsAZeroTheControllerRejects is netflow's own local version
// of the OmitZeroProblems census: unifi_setting is not walked by
// TestEveryKitSurfaceOmitsAZeroTheControllerRejects
// (omit_zero_census_test.go), which only reaches resources implementing
// OmitZeroProblems() directly -- settingResource does not -- so this section
// gates its own six Int64PtrFields against settings.FieldConstraints by
// hand, the same shape that census gates every other kit surface's,
// following this dispatch's own report requirement to check each nullable
// integer's pattern for a rejected zero. settings.FieldConstraint and
// unifi.FieldConstraint are structurally identical (both generated from the
// same capture, one per package) -- see cmd/sdk-bootstrap/constraints.go's
// own sdkConstraints, which converts the same way -- so a plain per-entry
// conversion is enough to hand OmitZeroProblems the map shape it expects.
func TestNetflowOmitsAZeroTheControllerRejects(t *testing.T) {
	constraints := make(map[string]ui.FieldConstraint, len(settings.FieldConstraints["SettingNetflow"]))
	for wire, constraint := range settings.FieldConstraints["SettingNetflow"] {
		constraints[wire] = ui.FieldConstraint(constraint)
	}
	for _, problem := range resourcekit.OmitZeroProblems(netflowKitSpec(), constraints) {
		t.Error(problem)
	}
}

// TestNetflowNestedSchemaHasExactlyItsAttributes guards
// netflowNestedSchema's type assertion against a generator regression:
// "netflow" moving off SingleNestedAttribute would panic every conformance
// test above instead of naming the actual problem, so this pins the shape
// ahead of that.
func TestNetflowNestedSchemaHasExactlyItsAttributes(t *testing.T) {
	ctx := context.Background()
	built := resource_setting.SettingResourceSchema(ctx)
	if _, ok := built.Attributes["netflow"]; !ok {
		t.Fatal(`the generated setting schema has no "netflow" attribute`)
	}
	nested := netflowNestedSchema(ctx)
	if len(nested.Attributes) != 11 {
		t.Errorf("netflow has %d attribute(s), want 11; update netflowKitSpec and this count together",
			len(nested.Attributes))
	}
}
