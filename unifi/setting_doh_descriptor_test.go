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

// TestDohAfterReceiveNullsWhatThePlanDidNotName pins dohAfterReceive against
// the deleted dohSettingToModel's own behaviour, read off setting_sections.go
// before the migration (git history): every one of doh's three attributes
// (state, server_names, custom_servers) was conditioned on
// `!plan.<Attr>.IsNull() && !plan.<Attr>.IsUnknown()`, mirroring the
// controller's value only when the practitioner's own plan/prior named the
// attribute, else forcing null -- so an unmanaged doh attribute never
// drifts. This exercises all three in one prior/model pair, each named or
// not, the same shape as TestMgmtAfterReceive.
func TestDohAfterReceiveNullsWhatThePlanDidNotName(t *testing.T) {
	sdk := &settings.Doh{}
	// model starts as whatever Spec.ToModel would have decoded straight off
	// the wire, before AfterReceive applies the plan-conditioned nulls.
	model := &settingDohModel{
		State:         types.StringValue("auto"),
		ServerNames:   types.ListValueMust(types.StringType, nil),
		CustomServers: types.ListValueMust(types.ObjectType{AttrTypes: dohCustomServerAttrTypes}, nil),
	}
	prior := settingDohModel{
		State: types.StringValue("custom"), // configured, so it survives
		// ServerNames and CustomServers left null: unconfigured.
		ServerNames:   types.ListNull(types.StringType),
		CustomServers: types.ListNull(types.ObjectType{AttrTypes: dohCustomServerAttrTypes}),
	}

	diags := dohAfterReceive(context.Background(), sdk, model, prior)
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}

	if model.State.ValueString() != "auto" {
		t.Errorf("state = %v, want the controller's decoded value kept (configured in prior)",
			model.State)
	}
	if !model.ServerNames.IsNull() {
		t.Errorf("server_names = %v, want null (unconfigured in prior, so it must not drift)",
			model.ServerNames)
	}
	if !model.CustomServers.IsNull() {
		t.Errorf("custom_servers = %v, want null (unconfigured in prior, so it must not drift)",
			model.CustomServers)
	}
}

// TestDohConfiguredEmptyCustomServersReadsBackAsEmptyList pins a deliberate
// divergence from mgmt's ssh_keys template: the deleted dohSettingToModel
// built custom_servers straight from setting.CustomServers whenever
// plan.CustomServers was configured (empty list included) -- unlike
// mgmt's mgmtSettingToModel, it never renulled a configured-but-empty
// result. dohKitSpec's custom_servers Field carries Elide: KeepZero, so
// Spec.ToModel already produces a non-null empty list for a zero SDK slice;
// dohAfterReceive must not null it a second time when prior is a configured
// (non-null) empty list, only when prior itself is null/unknown.
func TestDohConfiguredEmptyCustomServersReadsBackAsEmptyList(t *testing.T) {
	ctx := context.Background()
	spec := dohKitSpec()
	sdk := &settings.Doh{} // no custom servers on the wire

	var model settingDohModel
	if diags := spec.ToModel(ctx, sdk, &model, ""); diags.HasError() {
		t.Fatalf("ToModel: %v", diags)
	}
	if model.CustomServers.IsNull() {
		t.Fatal("custom_servers is null straight off ToModel; want a known empty list " +
			"(Elide: KeepZero) -- a zero read is a value here, not an absence")
	}
	if len(model.CustomServers.Elements()) != 0 {
		t.Fatalf("custom_servers has %d element(s) after ToModel, want 0",
			len(model.CustomServers.Elements()))
	}

	// prior is configured (non-null, empty) -- the practitioner DID write
	// custom_servers = [] in their config.
	prior := settingDohModel{
		CustomServers: types.ListValueMust(types.ObjectType{AttrTypes: dohCustomServerAttrTypes}, nil),
	}
	if diags := dohAfterReceive(ctx, sdk, &model, prior); diags.HasError() {
		t.Fatalf("dohAfterReceive: %v", diags)
	}
	if model.CustomServers.IsNull() {
		t.Error("custom_servers came back null after dohAfterReceive; a configured empty " +
			"list must stay a known empty list, not be renulled -- nulling it here would " +
			"be mgmt's ssh_keys behaviour, not doh's own")
	}
	if len(model.CustomServers.Elements()) != 0 {
		t.Errorf("custom_servers has %d element(s) after dohAfterReceive, want 0",
			len(model.CustomServers.Elements()))
	}
}

// TestDohCustomServerEnabledDefaultsTrueWhenUnset pins the deleted
// dohModelToSetting's per-element loop: `enabled := true` unless the model
// explicitly sets it, ported here against dohCustomServerEncode directly
// since the generated schema's own Default: booldefault.StaticBool(true)
// means a real plan never actually leaves it null/unknown by the time
// Encode runs -- this is the defensive fallback pinned regardless.
func TestDohCustomServerEnabledDefaultsTrueWhenUnset(t *testing.T) {
	ctx := context.Background()
	object, diags := types.ObjectValueFrom(ctx, dohCustomServerAttrTypes, settingDohCustomServerModel{
		Enabled:    types.BoolNull(),
		SDNSStamp:  types.StringValue("sdns://stamp"),
		ServerName: types.StringValue("my-resolver"),
	})
	if diags.HasError() {
		t.Fatalf("building the object: %v", diags)
	}

	got, diags := dohCustomServerEncode(ctx, object)
	if diags.HasError() {
		t.Fatalf("dohCustomServerEncode: %v", diags)
	}
	if !got.Enabled {
		t.Error("Enabled = false, want true (the model left it unset)")
	}

	// An explicit false must survive, not be overridden by the default.
	object, diags = types.ObjectValueFrom(ctx, dohCustomServerAttrTypes, settingDohCustomServerModel{
		Enabled:    types.BoolValue(false),
		SDNSStamp:  types.StringValue("sdns://stamp"),
		ServerName: types.StringValue("my-resolver"),
	})
	if diags.HasError() {
		t.Fatalf("building the object: %v", diags)
	}
	got, diags = dohCustomServerEncode(ctx, object)
	if diags.HasError() {
		t.Fatalf("dohCustomServerEncode: %v", diags)
	}
	if got.Enabled {
		t.Error("Enabled = true, want false (the model explicitly set it)")
	}
}

// TestDohSettingRoundTrip ports the deleted TestSettingBlocksRoundTrip-style
// coverage of dohModelToSetting/dohSettingToModel
// (Test_settingResource_dohModelToSetting, Test_settingResource_dohSettingToModel,
// setting_resource_test.go): model -> go-unifi setting -> model preserves
// state, server_names and every custom_servers member. It now drives the
// Spec's own ToSDK/ToModel instead of the deleted mappers; the
// null-fields-produce-empty-setting and plan-conditioned-null halves of the
// old tests are ported separately, as
// TestDohAfterReceiveNullsWhatThePlanDidNotName and the generic
// null-leaves-SDK-untouched behaviour every Field kind already carries
// (internal/resourcekit's own field tests).
func TestDohSettingRoundTrip(t *testing.T) {
	ctx := context.Background()
	spec := dohKitSpec()

	customServersType := types.ObjectType{AttrTypes: dohCustomServerAttrTypes}
	customServers, diags := types.ListValueFrom(ctx, customServersType, []settingDohCustomServerModel{
		{
			Enabled:    types.BoolValue(true),
			SDNSStamp:  types.StringValue("sdns://AQcAAAAAAAAABzguOC44Ljg"),
			ServerName: types.StringValue("google"),
		},
	})
	if diags.HasError() {
		t.Fatalf("building custom_servers: %v", diags)
	}
	serverNames, diags := types.ListValueFrom(ctx, types.StringType, []string{"cloudflare"})
	if diags.HasError() {
		t.Fatalf("building server_names: %v", diags)
	}

	in := &settingDohModel{
		CustomServers: customServers,
		ServerNames:   serverNames,
		State:         types.StringValue("custom"),
	}
	sdk, diags := spec.ToSDK(ctx, in)
	if diags.HasError() {
		t.Fatalf("ToSDK: %v", diags)
	}
	if sdk.State != "custom" {
		t.Errorf("ToSDK: State = %q, want custom", sdk.State)
	}
	if len(sdk.ServerNames) != 1 || sdk.ServerNames[0] != "cloudflare" {
		t.Errorf("ToSDK: ServerNames = %v, want [cloudflare]", sdk.ServerNames)
	}
	if len(sdk.CustomServers) != 1 {
		t.Fatalf("ToSDK: CustomServers has %d element(s), want 1", len(sdk.CustomServers))
	}
	if !sdk.CustomServers[0].Enabled ||
		sdk.CustomServers[0].SdnsStamp != "sdns://AQcAAAAAAAAABzguOC44Ljg" ||
		sdk.CustomServers[0].ServerName != "google" {
		t.Errorf("ToSDK: CustomServers[0] = %+v, want Enabled=true SdnsStamp=sdns://... ServerName=google",
			sdk.CustomServers[0])
	}

	var out settingDohModel
	if diags := spec.ToModel(ctx, sdk, &out, ""); diags.HasError() {
		t.Fatalf("ToModel: %v", diags)
	}
	if out.State.ValueString() != "custom" {
		t.Errorf("ToModel: State = %q, want custom", out.State.ValueString())
	}
	var gotServerNames []string
	if diags := out.ServerNames.ElementsAs(ctx, &gotServerNames, false); diags.HasError() {
		t.Fatalf("reading ServerNames: %v", diags)
	}
	if len(gotServerNames) != 1 || gotServerNames[0] != "cloudflare" {
		t.Errorf("ToModel: ServerNames = %v, want [cloudflare]", gotServerNames)
	}
	var gotCustomServers []settingDohCustomServerModel
	if diags := out.CustomServers.ElementsAs(ctx, &gotCustomServers, false); diags.HasError() {
		t.Fatalf("reading CustomServers: %v", diags)
	}
	if len(gotCustomServers) != 1 {
		t.Fatalf("ToModel: CustomServers has %d element(s), want 1", len(gotCustomServers))
	}
	if !gotCustomServers[0].Enabled.ValueBool() ||
		gotCustomServers[0].SDNSStamp.ValueString() != "sdns://AQcAAAAAAAAABzguOC44Ljg" ||
		gotCustomServers[0].ServerName.ValueString() != "google" {
		t.Errorf("ToModel: CustomServers[0] = %+v, want Enabled=true SDNSStamp=sdns://... ServerName=google",
			gotCustomServers[0])
	}
}

// TestDohKitSpecConformance runs the same conformance instruments every
// other kit descriptor's test applies (see e.g. dns_record's case in
// descriptor_elide_test.go), scoped to doh's own nested schema rather than
// a whole resource's, since doh is one section of unifi_setting rather than
// a surface of its own.
func TestDohKitSpecConformance(t *testing.T) {
	ctx := context.Background()
	spec := dohKitSpec()
	for _, problem := range resourcekit.WireNameProblems(spec) {
		t.Error(problem)
	}
	for _, problem := range resourcekit.NestedProblems(spec) {
		t.Error(problem)
	}
	built := dohNestedSchema(ctx)
	for _, problem := range resourcekit.ElideProblems(spec, built) {
		t.Error(problem)
	}
	for _, problem := range resourcekit.ZeroReadProblems(spec, built) {
		t.Error(problem)
	}
}

// TestDohBackendUpdateFieldsSendsOnlyTheNamedWiresPlusKey is the unit half
// of doh's masked-write gate, shaped exactly like
// TestMgmtBackendUpdateFieldsSendsOnlyTheNamedWiresPlusKey
// (setting_mgmt_descriptor_test.go): it runs dohKitBackend's UpdateFields
// closure -- the same one Configure wires into the live resource -- against
// an httptest server that keeps the raw, undecoded PUT body, and asserts it
// carries exactly the field the mask named plus "key".
func TestDohBackendUpdateFieldsSendsOnlyTheNamedWiresPlusKey(t *testing.T) {
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

	backend := dohKitBackend(api)
	sdk := &settings.Doh{State: "auto"}
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
	// server_names has no omitempty on settings.Doh's own tag semantics in a
	// masked write's neighbouring context -- custom_servers is the one that
	// matters here: it carries omitempty, but this still proves the mask
	// leaves an unnamed field out entirely.
	if _, ok := body["custom_servers"]; ok {
		t.Error(`PUT body carries "custom_servers", which the mask never named; ` +
			"the masked write is supposed to leave it out")
	}
}

// TestDohNestedSchemaHasExactlyItsAttributes guards dohNestedSchema's type
// assertion against a generator regression: "doh" moving off
// SingleNestedAttribute would panic every conformance test above instead of
// naming the actual problem, so this pins the shape ahead of that.
func TestDohNestedSchemaHasExactlyItsAttributes(t *testing.T) {
	ctx := context.Background()
	built := resource_setting.SettingResourceSchema(ctx)
	if _, ok := built.Attributes["doh"]; !ok {
		t.Fatal(`the generated setting schema has no "doh" attribute`)
	}
	nested := dohNestedSchema(ctx)
	if len(nested.Attributes) != 3 {
		t.Errorf("doh has %d attribute(s), want 3; update dohKitSpec and this count together",
			len(nested.Attributes))
	}
}
