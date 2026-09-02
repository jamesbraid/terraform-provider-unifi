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

// TestDeviceSupervisionBackendUpdateFieldsSendsOnlyTheNamedWiresPlusKey is
// the unit half of device_supervision's masked-write gate, shaped exactly
// like TestCountryBackendUpdateFieldsSendsOnlyTheNamedWiresPlusKey
// (setting_country_descriptor_test.go): it runs
// deviceSupervisionKitBackend's UpdateFields closure -- the same one
// Configure wires into the live resource -- against an httptest server
// that keeps the raw, undecoded PUT body, and asserts it carries exactly
// the field the mask named plus "key", not heartbeat_interval_seconds when
// only global_supervision_enabled changed.
func TestDeviceSupervisionBackendUpdateFieldsSendsOnlyTheNamedWiresPlusKey(t *testing.T) {
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

	backend := deviceSupervisionKitBackend(api)
	heartbeat := int64(120)
	sdk := &settings.DeviceSupervision{GlobalSupervisionEnabled: true, HeartbeatIntervalSeconds: &heartbeat}
	if _, err := backend.UpdateFields(context.Background(), "default", sdk, "global_supervision_enabled"); err != nil {
		t.Fatalf("UpdateFields: %v", err)
	}

	want := map[string]bool{"key": true, "global_supervision_enabled": true}
	if len(body) != len(want) {
		t.Fatalf("PUT body has %d key(s) %v, want exactly %v", len(body), keysOf(body), want)
	}
	for name := range want {
		if _, ok := body[name]; !ok {
			t.Errorf("PUT body is missing %q; got %v", name, keysOf(body))
		}
	}
}

// TestDeviceSupervisionKitSpecConformance runs the same conformance
// instruments every other kit descriptor's test applies (see
// setting_mgmt_descriptor_test.go's TestMgmtKitSpecConformance), scoped to
// device_supervision's own nested schema rather than a whole resource's,
// since device_supervision is one section of unifi_setting rather than a
// surface of its own.
func TestDeviceSupervisionKitSpecConformance(t *testing.T) {
	ctx := context.Background()
	spec := deviceSupervisionKitSpec()
	for _, problem := range resourcekit.WireNameProblems(spec) {
		t.Error(problem)
	}
	for _, problem := range resourcekit.NestedProblems(spec) {
		t.Error(problem)
	}
	built := deviceSupervisionNestedSchema(ctx)
	for _, problem := range resourcekit.ElideProblems(spec, built) {
		t.Error(problem)
	}
	for _, problem := range resourcekit.ZeroReadProblems(spec, built) {
		t.Error(problem)
	}
}

// TestDeviceSupervisionOmitsAZeroTheControllerRejects is
// device_supervision's own local version of the OmitZeroProblems census,
// the same shape TestNetflowOmitsAZeroTheControllerRejects records:
// unifi_setting is not walked by
// TestEveryKitSurfaceOmitsAZeroTheControllerRejects
// (omit_zero_census_test.go), which only reaches resources implementing
// OmitZeroProblems() directly -- settingResource does not -- so this
// section gates its own three Int64PtrFields against
// settings.FieldConstraints by hand.
func TestDeviceSupervisionOmitsAZeroTheControllerRejects(t *testing.T) {
	constraints := make(map[string]ui.FieldConstraint, len(settings.FieldConstraints["SettingDeviceSupervision"]))
	for wire, constraint := range settings.FieldConstraints["SettingDeviceSupervision"] {
		constraints[wire] = ui.FieldConstraint(constraint)
	}
	for _, problem := range resourcekit.OmitZeroProblems(deviceSupervisionKitSpec(), constraints) {
		t.Error(problem)
	}
}

// TestDeviceSupervisionNestedSchemaHasExactlyItsAttributes guards
// deviceSupervisionNestedSchema's type assertion against a generator
// regression: "device_supervision" moving off SingleNestedAttribute would
// panic every conformance test above instead of naming the actual problem,
// so this pins the shape ahead of that.
func TestDeviceSupervisionNestedSchemaHasExactlyItsAttributes(t *testing.T) {
	ctx := context.Background()
	built := resource_setting.SettingResourceSchema(ctx)
	if _, ok := built.Attributes["device_supervision"]; !ok {
		t.Fatal(`the generated setting schema has no "device_supervision" attribute`)
	}
	nested := deviceSupervisionNestedSchema(ctx)
	if len(nested.Attributes) != 4 {
		t.Errorf("device_supervision has %d attribute(s), want 4; update deviceSupervisionKitSpec and this count together",
			len(nested.Attributes))
	}
}
