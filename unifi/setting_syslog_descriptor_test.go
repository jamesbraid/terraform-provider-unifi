package unifi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
	resource_setting "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_setting"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/resourcekit"
)

// TestSyslogSettingRoundTrip ports the deleted TestSettingBlocksRoundTrip's
// syslog subtest (setting_resource_test.go), which exercised
// syslogModelToSetting/syslogSettingToModel directly: model -> go-unifi
// setting -> model preserves the fields. It now drives the Spec's own
// ToSDK/ToModel instead of the deleted mappers.
func TestSyslogSettingRoundTrip(t *testing.T) {
	ctx := context.Background()
	spec := syslogKitSpec()

	contents, diags := types.ListValueFrom(ctx, types.StringType, []string{"device", "client"})
	if diags.HasError() {
		t.Fatalf("building contents: %v", diags)
	}
	in := &settingSyslogModel{
		Enabled:  types.BoolValue(true),
		IP:       types.StringValue("10.0.0.9"),
		Port:     types.Int64Value(514),
		Contents: contents,
	}
	sdk, diags := spec.ToSDK(ctx, in)
	if diags.HasError() {
		t.Fatalf("ToSDK: %v", diags)
	}
	if !sdk.Enabled || sdk.IP != "10.0.0.9" || sdk.Port == nil ||
		*sdk.Port != 514 || len(sdk.Contents) != 2 {
		t.Fatalf("ToSDK = %+v, want enabled ip=10.0.0.9 port=514 contents len 2", sdk)
	}

	var out settingSyslogModel
	if diags := spec.ToModel(ctx, sdk, &out, ""); diags.HasError() {
		t.Fatalf("ToModel: %v", diags)
	}
	var gotContents []string
	if diags := out.Contents.ElementsAs(ctx, &gotContents, false); diags.HasError() {
		t.Fatalf("reading contents: %v", diags)
	}
	if out.IP.ValueString() != "10.0.0.9" || len(gotContents) != 2 {
		t.Errorf("syslog round-trip mismatch: %+v", out)
	}
}

// TestSyslogSpecOmitsAnUnsetPort ports the deleted TestSyslogOmitsUnsetPorts:
// the #303 guard, now Int64PtrField{OmitZero: true}. An unset port /
// netconsole_port must never reach the wire as a pointer to zero -- the
// controller rejects port 0. It now drives the Spec's own ToSDK instead of
// the deleted syslogModelToSetting.
func TestSyslogSpecOmitsAnUnsetPort(t *testing.T) {
	ctx := context.Background()
	spec := syslogKitSpec()

	m := &settingSyslogModel{
		Enabled:        types.BoolValue(true),
		IP:             types.StringValue("10.0.10.15"),
		Port:           types.Int64Value(1514),
		NetconsolePort: types.Int64Null(), // netconsole disabled / unset
		Contents:       types.ListNull(types.StringType),
	}
	sdk, diags := spec.ToSDK(ctx, m)
	if diags.HasError() {
		t.Fatalf("ToSDK: %v", diags)
	}
	if sdk.NetconsolePort != nil {
		t.Errorf("netconsole_port must be omitted when unset, got %d", *sdk.NetconsolePort)
	}
	if sdk.Port == nil || *sdk.Port != 1514 {
		t.Errorf("port = %v, want 1514", sdk.Port)
	}

	// Unknown (Optional+Computed at create) must also omit, not send 0.
	m.Port = types.Int64Unknown()
	sdk, diags = spec.ToSDK(ctx, m)
	if diags.HasError() {
		t.Fatalf("ToSDK: %v", diags)
	}
	if sdk.Port != nil {
		t.Errorf("unknown port must be omitted, got %d", *sdk.Port)
	}
}

// TestSyslogSpecKeyIsRsyslogd pins the controller key settings.Rsyslogd's
// own GetSettingKey answers: "rsyslogd", not "syslog". syslogKitBackend
// reads and writes through the SDK's generic GetSetting/UpdateSettingFields,
// which derive the key from the Go type alone, so a wrong assumption here
// would silently address the wrong document -- an httptest server records
// the GET path and the PUT body's own "key" field, rather than trusting the
// Go type switch by inspection.
func TestSyslogSpecKeyIsRsyslogd(t *testing.T) {
	var gotGetPath string
	var putBody map[string]json.RawMessage
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/proxy/network/status" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"meta":{"server_version":"10.4.57"}}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch req.Method {
		case http.MethodGet:
			gotGetPath = req.URL.Path
			_, _ = w.Write([]byte(
				`{"meta":{},"data":[{"key":"rsyslogd","enabled":true}]}`,
			))
		case http.MethodPut:
			raw, _ := io.ReadAll(req.Body)
			if err := json.Unmarshal(raw, &putBody); err != nil {
				t.Errorf("the provider sent a body that is not an object: %v", err)
			}
			_, _ = w.Write(append(append([]byte(`{"data":[`), raw...), []byte(`]}`)...))
		}
	}))
	t.Cleanup(server.Close)

	api, err := ui.New(context.Background(), &ui.Config{BaseURL: server.URL, APIKey: "test-key"})
	if err != nil {
		t.Fatalf("create the API client: %v", err)
	}
	backend := syslogKitBackend(api)

	if _, err := backend.Read(context.Background(), "default", ""); err != nil {
		t.Fatalf("Read: %v", err)
	}
	const wantSuffix = "/api/s/default/get/setting/rsyslogd"
	if !strings.HasSuffix(gotGetPath, wantSuffix) {
		t.Errorf("GET path = %q, want a path ending %q", gotGetPath, wantSuffix)
	}

	sdk := &settings.Rsyslogd{Enabled: true}
	if _, err := backend.UpdateFields(context.Background(), "default", sdk, "enabled"); err != nil {
		t.Fatalf("UpdateFields: %v", err)
	}
	raw, ok := putBody["key"]
	if !ok {
		t.Fatal(`PUT body has no "key" field`)
	}
	if string(raw) != `"rsyslogd"` {
		t.Errorf(`PUT body "key" = %s, want "rsyslogd"`, raw)
	}
}

// TestSyslogBackendUpdateFieldsSendsOnlyTheNamedWiresPlusKey is the unit
// half of syslog's masked-write gate, shaped exactly like
// TestMgmtBackendUpdateFieldsSendsOnlyTheNamedWiresPlusKey
// (setting_mgmt_descriptor_test.go): it runs syslogKitBackend's UpdateFields
// closure -- the same one Configure wires into the live resource -- against
// an httptest server that keeps the raw, undecoded PUT body, and asserts it
// carries exactly the field the mask named plus "key": no force-emitted
// sibling (debug, which carries no omitempty on settings.Rsyslogd).
func TestSyslogBackendUpdateFieldsSendsOnlyTheNamedWiresPlusKey(t *testing.T) {
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

	backend := syslogKitBackend(api)
	sdk := &settings.Rsyslogd{Enabled: true, Debug: true}
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
	// debug has no omitempty on settings.Rsyslogd -- an unmasked encode
	// would always carry it. Its absence here is the assertion this test
	// exists for.
	if _, ok := body["debug"]; ok {
		t.Error(`PUT body carries "debug", which the mask never named; ` +
			"the masked write is supposed to leave it out")
	}
}

// TestSyslogKitSpecConformance runs the same conformance instruments every
// other kit descriptor's test applies (see e.g. dns_record's case in
// descriptor_elide_test.go), scoped to syslog's own nested schema rather
// than a whole resource's, since syslog is one section of unifi_setting
// rather than a surface of its own.
func TestSyslogKitSpecConformance(t *testing.T) {
	ctx := context.Background()
	spec := syslogKitSpec()
	for _, problem := range resourcekit.WireNameProblems(spec) {
		t.Error(problem)
	}
	for _, problem := range resourcekit.NestedProblems(spec) {
		t.Error(problem)
	}
	built := syslogNestedSchema(ctx)
	for _, problem := range resourcekit.ElideProblems(spec, built) {
		t.Error(problem)
	}
	for _, problem := range resourcekit.ZeroReadProblems(spec, built) {
		t.Error(problem)
	}
}

// TestSyslogNestedSchemaHasExactlyItsAttributes guards syslogNestedSchema's
// type assertion against a generator regression: "syslog" moving off
// SingleNestedAttribute would panic every conformance test above instead of
// naming the actual problem, so this pins the shape ahead of that.
func TestSyslogNestedSchemaHasExactlyItsAttributes(t *testing.T) {
	ctx := context.Background()
	built := resource_setting.SettingResourceSchema(ctx)
	if _, ok := built.Attributes["syslog"]; !ok {
		t.Fatal(`the generated setting schema has no "syslog" attribute`)
	}
	nested := syslogNestedSchema(ctx)
	if len(nested.Attributes) != 11 {
		t.Errorf("syslog has %d attribute(s), want 11; update syslogKitSpec and this count together",
			len(nested.Attributes))
	}
}
