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

// TestNtpSettingRoundTrip ports TestSettingBlocksRoundTrip's ntp subtest
// (setting_resource_test.go), which exercised
// ntpModelToSetting/ntpSettingToModel directly: model -> go-unifi setting ->
// model preserves the fields. It now drives the Spec's own ToSDK/ToModel
// instead of the deleted mappers.
func TestNtpSettingRoundTrip(t *testing.T) {
	ctx := context.Background()
	spec := ntpKitSpec()

	in := &settingNtpModel{
		NtpServer1:        types.StringValue("pool.ntp.org"),
		SettingPreference: types.StringValue("manual"),
	}
	sdk, diags := spec.ToSDK(ctx, in)
	if diags.HasError() {
		t.Fatalf("ToSDK: %v", diags)
	}
	var out settingNtpModel
	if diags := spec.ToModel(ctx, sdk, &out, ""); diags.HasError() {
		t.Fatalf("ToModel: %v", diags)
	}
	if out.NtpServer1.ValueString() != "pool.ntp.org" || out.SettingPreference.ValueString() != "manual" {
		t.Errorf("ntp round-trip mismatch: %+v", out)
	}
}

// TestNtpSettingRoundTripStateNormalization ports the deleted
// TestNtpSettingStateNormalization: the controller's "" for an unset NTP
// server -- a valid configured value -- isn't rewritten to null during the
// post-apply read. It now drives the Spec's own ToSDK/ToModel.
func TestNtpSettingRoundTripStateNormalization(t *testing.T) {
	ctx := context.Background()
	spec := ntpKitSpec()

	in := &settingNtpModel{
		NtpServer1:        types.StringValue("pool.ntp.org"),
		NtpServer2:        types.StringValue(""),
		NtpServer3:        types.StringNull(),
		NtpServer4:        types.StringNull(),
		SettingPreference: types.StringValue("manual"),
	}
	sdk, diags := spec.ToSDK(ctx, in)
	if diags.HasError() {
		t.Fatalf("ToSDK: %v", diags)
	}

	var state settingNtpModel
	if diags := spec.ToModel(ctx, sdk, &state, ""); diags.HasError() {
		t.Fatalf("ToModel: %v", diags)
	}
	if state.NtpServer1.ValueString() != "pool.ntp.org" {
		t.Errorf("ntp_server_1 = %q, want pool.ntp.org", state.NtpServer1.ValueString())
	}
	for key, value := range map[string]types.String{
		"ntp_server_2": state.NtpServer2,
		"ntp_server_3": state.NtpServer3,
		"ntp_server_4": state.NtpServer4,
	} {
		if value.IsNull() || value.IsUnknown() || value.ValueString() != "" {
			t.Errorf("%s = %v, want known empty string", key, value)
		}
	}
}

// TestNtpSpecKeepsAnEmptyServerOnTheWire pins round 1's finding at the wire,
// not just in Go: ntp_server_2's json tag carries omitempty, so an unmasked
// encode of a zero-value Ntp struct would already drop it, and the interesting
// question is only answerable through the masked write. ntpKitBackend's
// UpdateFields closure is what Configure wires into the live resource, so
// this drives that -- an httptest server keeping the raw, undecoded PUT body
// -- rather than asserting on the Go struct alone, which would pass whether
// the mask carried "" or nothing at all.
func TestNtpSpecKeepsAnEmptyServerOnTheWire(t *testing.T) {
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

	backend := ntpKitBackend(api)
	sdk := &settings.Ntp{NtpServer1: "pool.ntp.org", NtpServer2: ""}
	if _, err := backend.UpdateFields(
		context.Background(), "default", sdk, "ntp_server_1", "ntp_server_2",
	); err != nil {
		t.Fatalf("UpdateFields: %v", err)
	}

	raw, ok := body["ntp_server_2"]
	if !ok {
		t.Fatalf(`PUT body has no "ntp_server_2" key (got %v); an explicit "" must reach `+
			"the wire, not be dropped by omitempty", keysOf(body))
	}
	if string(raw) != `""` {
		t.Errorf(`ntp_server_2 = %s, want "" (an empty string, neither omitted nor null)`, raw)
	}
}

// TestNtpBackendUpdateFieldsSendsOnlyTheNamedWiresPlusKey is the unit half of
// ntp's masked-write gate, shaped exactly like
// TestMgmtBackendUpdateFieldsSendsOnlyTheNamedWiresPlusKey
// (setting_mgmt_descriptor_test.go). Every field of settings.Ntp carries
// omitempty, so there is no non-empty sibling here that an unmasked encode
// would force onto the wire the way mgmt's led_enabled does -- the length
// check is what catches a masked write regressing into an unmasked one for a
// struct this sparse.
func TestNtpBackendUpdateFieldsSendsOnlyTheNamedWiresPlusKey(t *testing.T) {
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

	backend := ntpKitBackend(api)
	sdk := &settings.Ntp{NtpServer1: "pool.ntp.org", SettingPreference: "manual"}
	if _, err := backend.UpdateFields(context.Background(), "default", sdk, "ntp_server_1"); err != nil {
		t.Fatalf("UpdateFields: %v", err)
	}

	want := map[string]bool{"key": true, "ntp_server_1": true}
	if len(body) != len(want) {
		t.Fatalf("PUT body has %d key(s) %v, want exactly %v", len(body), keysOf(body), want)
	}
	for name := range want {
		if _, ok := body[name]; !ok {
			t.Errorf("PUT body is missing %q; got %v", name, keysOf(body))
		}
	}
	if _, ok := body["setting_preference"]; ok {
		t.Error(`PUT body carries "setting_preference", which the mask never named; ` +
			"the masked write is supposed to leave it out")
	}
}

// TestNtpKitSpecConformance runs the same conformance instruments every
// other kit descriptor's test applies (see e.g. dns_record's case in
// descriptor_elide_test.go), scoped to ntp's own nested schema rather than a
// whole resource's, since ntp is one section of unifi_setting rather than a
// surface of its own.
func TestNtpKitSpecConformance(t *testing.T) {
	ctx := context.Background()
	spec := ntpKitSpec()
	for _, problem := range resourcekit.WireNameProblems(spec) {
		t.Error(problem)
	}
	for _, problem := range resourcekit.NestedProblems(spec) {
		t.Error(problem)
	}
	built := ntpNestedSchema(ctx)
	for _, problem := range resourcekit.ElideProblems(spec, built) {
		t.Error(problem)
	}
	for _, problem := range resourcekit.ZeroReadProblems(spec, built) {
		t.Error(problem)
	}
}

// TestNtpNestedSchemaHasExactlyItsAttributes guards ntpNestedSchema's type
// assertion against a generator regression: "ntp" moving off
// SingleNestedAttribute would panic every conformance test above instead of
// naming the actual problem, so this pins the shape ahead of that.
func TestNtpNestedSchemaHasExactlyItsAttributes(t *testing.T) {
	ctx := context.Background()
	built := resource_setting.SettingResourceSchema(ctx)
	if _, ok := built.Attributes["ntp"]; !ok {
		t.Fatal(`the generated setting schema has no "ntp" attribute`)
	}
	nested := ntpNestedSchema(ctx)
	if len(nested.Attributes) != 5 {
		t.Errorf("ntp has %d attribute(s), want 5; update ntpKitSpec and this count together",
			len(nested.Attributes))
	}
}
