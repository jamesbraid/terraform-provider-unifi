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

// TestMgmtAfterReceive ports the two assertions of the deleted
// TestMgmtNewFields that still apply once mgmt is served through
// mgmtAfterReceive rather than mgmtSettingToModel: ssh_password is restored
// from the plan/prior regardless of what the controller reports (it never
// echoes plaintext), and an attribute the plan never set comes back null
// rather than the controller's live value, so an unmanaged mgmt attribute
// never drifts. See setting_resource_test.go for what did NOT port.
func TestMgmtAfterReceive(t *testing.T) {
	sdk := &settings.Mgmt{}
	model := &settingMgmtModel{
		AutoUpgrade: types.BoolValue(true), // read off the wire below
		SSHUsername: types.StringValue("controller-reported"),
	}
	prior := settingMgmtModel{
		SSHUsername: types.StringValue("admin"),
		SSHPassword: types.StringValue("s3cret"),
		AutoUpgrade: types.BoolValue(true), // configured, so it survives
		// WifimanEnabled left null: unconfigured.
	}
	// model's fields start as whatever ToModel would have decoded straight
	// off the wire, before AfterReceive applies the plan-conditioned nulls.
	model.SSHUsername = types.StringValue("controller-reported")
	model.SSHPassword = types.StringValue("controller-hash-not-plaintext")
	model.WifimanEnabled = types.BoolValue(true)

	diags := mgmtAfterReceive(context.Background(), sdk, model, prior)
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}

	if model.SSHPassword.ValueString() != "s3cret" {
		t.Errorf("ssh_password = %q, want the prior/plan value %q restored",
			model.SSHPassword.ValueString(), "s3cret")
	}
	if model.SSHUsername.ValueString() != "controller-reported" {
		t.Errorf("ssh_username = %v, want the controller's value kept (configured in prior)",
			model.SSHUsername)
	}
	if !model.AutoUpgrade.Equal(types.BoolValue(true)) {
		t.Errorf("auto_upgrade = %v, want true (configured in prior)", model.AutoUpgrade)
	}
	if !model.WifimanEnabled.IsNull() {
		t.Errorf("wifiman_enabled = %v, want null (unconfigured in prior, so it must not drift)",
			model.WifimanEnabled)
	}
}

// TestMgmtSSHUsernameEmptyReadStaysEmptyWhenConfigured pins a deliberate
// divergence from the deleted mgmtSettingToModel: that mapper additionally
// nulled a *configured* ssh_username when the controller read back "" (via
// util.StringValueOrNull), stacked on top of its plan-conditioned null.
// mgmtKitSpec's ssh_username Field carries Elide: KeepZero -- required by
// ElideProblems, since the schema declares ssh_username Optional+Computed
// with no validator rejecting an empty string, so the schema's own contract
// says an empty read is a value, not an absence. mgmtAfterReceive only
// nulls ssh_username when the plan/prior never configured it at all; it
// does not re-null an empty value the controller legitimately reports for
// an attribute the practitioner DID configure. Both steps of the read path
// are exercised here, in order: Spec.ToModel (the Field's own Elide) and
// then mgmtAfterReceive (the plan-conditioned check) -- either one nulling
// the empty string would be the old, superseded behaviour, not this one.
func TestMgmtSSHUsernameEmptyReadStaysEmptyWhenConfigured(t *testing.T) {
	ctx := context.Background()
	spec := mgmtKitSpec()
	sdk := &settings.Mgmt{SSHUsername: ""}

	var model settingMgmtModel
	if diags := spec.ToModel(ctx, sdk, &model, ""); diags.HasError() {
		t.Fatalf("ToModel: %v", diags)
	}
	if model.SSHUsername.IsNull() {
		t.Fatal("ssh_username is null straight off ToModel; want a known empty string " +
			"(Elide: KeepZero) -- an empty controller read is a value here, not an absence")
	}
	if model.SSHUsername.ValueString() != "" {
		t.Fatalf("ssh_username = %q after ToModel, want \"\"", model.SSHUsername.ValueString())
	}

	// prior is configured (non-null) -- the practitioner DID manage
	// ssh_username, they just happen to be reading back a live "".
	prior := settingMgmtModel{SSHUsername: types.StringValue("admin")}
	if diags := mgmtAfterReceive(ctx, sdk, &model, prior); diags.HasError() {
		t.Fatalf("mgmtAfterReceive: %v", diags)
	}
	if model.SSHUsername.IsNull() {
		t.Error("ssh_username came back null after mgmtAfterReceive; a configured " +
			"attribute's empty read must stay a known \"\", not be renulled -- " +
			"nulling it here would be the legacy mapper's behaviour, not this one's")
	}
	if model.SSHUsername.ValueString() != "" {
		t.Errorf("ssh_username = %q after mgmtAfterReceive, want \"\"", model.SSHUsername.ValueString())
	}
}

// TestMgmtKitSpecConformance runs the same conformance instruments every
// other kit descriptor's test applies (see e.g. dns_record's case in
// descriptor_elide_test.go), scoped to mgmt's own nested schema rather than
// a whole resource's, since mgmt is one section of unifi_setting rather
// than a surface of its own.
func TestMgmtKitSpecConformance(t *testing.T) {
	ctx := context.Background()
	spec := mgmtKitSpec()
	for _, problem := range resourcekit.WireNameProblems(spec) {
		t.Error(problem)
	}
	for _, problem := range resourcekit.NestedProblems(spec) {
		t.Error(problem)
	}
	built := mgmtNestedSchema(ctx)
	for _, problem := range resourcekit.ElideProblems(spec, built) {
		t.Error(problem)
	}
	for _, problem := range resourcekit.ZeroReadProblems(spec, built) {
		t.Error(problem)
	}
}

// TestMgmtBackendUpdateFieldsSendsOnlyTheNamedWiresPlusKey is the unit half
// of the spike's measured gate (the acceptance half is
// TestAccSettingResource_mgmtMaskedWriteLeavesSiblingsAlone). It runs
// mgmtKitBackend's UpdateFields closure -- the same one Configure wires into
// the live resource -- against an httptest server that keeps the raw,
// undecoded PUT body, and asserts it carries exactly the field the mask
// named plus "key": no force-emitted sibling (led_enabled and every other
// unmodelled settings.Mgmt member), which is what makes the masked write
// different from the old whole-document UpdateSetting.
func TestMgmtBackendUpdateFieldsSendsOnlyTheNamedWiresPlusKey(t *testing.T) {
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

	backend := mgmtKitBackend(api)
	sdk := &settings.Mgmt{SSHEnabled: true}
	if _, err := backend.UpdateFields(context.Background(), "default", sdk, "x_ssh_enabled"); err != nil {
		t.Fatalf("UpdateFields: %v", err)
	}

	want := map[string]bool{"key": true, "x_ssh_enabled": true}
	if len(body) != len(want) {
		t.Fatalf("PUT body has %d key(s) %v, want exactly %v", len(body), keysOf(body), want)
	}
	for name := range want {
		if _, ok := body[name]; !ok {
			t.Errorf("PUT body is missing %q; got %v", name, keysOf(body))
		}
	}
	// led_enabled has no omitempty on settings.Mgmt -- an unmasked encode
	// would always carry it. Its absence here is the assertion this test
	// exists for.
	if _, ok := body["led_enabled"]; ok {
		t.Error(`PUT body carries "led_enabled", which the mask never named; ` +
			"the masked write is supposed to leave it out")
	}
}

func keysOf(m map[string]json.RawMessage) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// TestMgmtNestedSchemaHasExactlyItsAttributes guards mgmtNestedSchema's type
// assertion against a generator regression: "mgmt" moving off
// SingleNestedAttribute would panic every conformance test above instead of
// naming the actual problem, so this pins the shape ahead of that.
func TestMgmtNestedSchemaHasExactlyItsAttributes(t *testing.T) {
	ctx := context.Background()
	built := resource_setting.SettingResourceSchema(ctx)
	if _, ok := built.Attributes["mgmt"]; !ok {
		t.Fatal(`the generated setting schema has no "mgmt" attribute`)
	}
	nested := mgmtNestedSchema(ctx)
	if len(nested.Attributes) != 12 {
		t.Errorf("mgmt has %d attribute(s), want 12; update mgmtKitSpec and this count together",
			len(nested.Attributes))
	}
}
