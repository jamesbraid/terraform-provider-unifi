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

// TestIpsAfterReceiveNullsWhatThePlanDidNotName pins ipsAfterReceive against
// the deleted ipsSettingToModel's own behaviour, read off setting_resource.go
// before the migration (git history): every one of ips's eleven attributes --
// the nine ipsKitSpec maps plus the two suppression_alerts/suppression_whitelist
// ipsSuppressionKitSpec's own document hydrates into this same shared model --
// was conditioned on `!plan.<Attr>.IsNull() && !plan.<Attr>.IsUnknown()`,
// mirroring the controller's value only when the practitioner's own
// plan/prior named the attribute, else forcing null. This alternates
// configured/unconfigured across every kind (bool, string and list) so both
// branches are covered.
func TestIpsAfterReceiveNullsWhatThePlanDidNotName(t *testing.T) {
	ctx := context.Background()
	sdk := &settings.Ips{}

	honeypotType := types.ObjectType{AttrTypes: ipsHoneypotAttrTypes}
	whitelistType := types.ObjectType{AttrTypes: ipsWhitelistAttrTypes}
	alertType := types.ObjectType{AttrTypes: ipsAlertAttrTypes}
	trackingType := types.ObjectType{AttrTypes: ipsTrackingAttrTypes}

	honeypot, diags := types.ListValueFrom(ctx, honeypotType, []settingIpsHoneypotModel{{
		IPAddress: types.StringValue("10.1.10.254"),
		NetworkID: types.StringValue("net-a"),
		Version:   types.StringValue("v4"),
	}})
	if diags.HasError() {
		t.Fatalf("building honeypot: %v", diags)
	}
	whitelist, diags := types.ListValueFrom(ctx, whitelistType, []settingIpsWhitelistModel{{
		Direction: types.StringValue("both"),
		Mode:      types.StringValue("ip"),
		Value:     types.StringValue("10.0.0.5"),
	}})
	if diags.HasError() {
		t.Fatalf("building whitelist: %v", diags)
	}
	tracking, diags := types.ListValueFrom(ctx, trackingType, []settingIpsTrackingModel{})
	if diags.HasError() {
		t.Fatalf("building tracking: %v", diags)
	}
	alerts, diags := types.ListValueFrom(ctx, alertType, []settingIpsAlertModel{{
		Category:  types.StringValue("malware"),
		Gid:       types.Int64Value(1),
		ID:        types.Int64Value(2001),
		Signature: types.StringValue("ET MALWARE"),
		Type:      types.StringValue("track"),
		Tracking:  tracking,
	}})
	if diags.HasError() {
		t.Fatalf("building alerts: %v", diags)
	}
	categories, diags := types.ListValueFrom(ctx, types.StringType, []string{"tor"})
	if diags.HasError() {
		t.Fatalf("building enabled_categories: %v", diags)
	}
	networks, diags := types.ListValueFrom(ctx, types.StringType, []string{"net-1"})
	if diags.HasError() {
		t.Fatalf("building enabled_networks: %v", diags)
	}

	// model starts as whatever Spec.ToModel (ipsKitSpec's own Fields) and
	// ipsSuppressionKitSpec's own document Read would have decoded straight
	// off the wire, before ipsAfterReceive applies the plan-conditioned
	// nulls -- every field carries a concrete, non-null value.
	model := &settingIpsModel{
		AdvancedFilteringPreference:         types.StringValue("manual"),
		ContentFilteringBlockingPageEnabled: types.BoolValue(true),
		EnabledCategories:                   categories,
		EnabledNetworks:                     networks,
		Honeypot:                            honeypot,
		HoneypotEnabled:                     types.BoolValue(true),
		IPSMode:                             types.StringValue("ips"),
		MemoryOptimized:                     types.BoolValue(true),
		RestrictTorrents:                    types.BoolValue(true),
		SuppressionWhitelist:                whitelist,
		SuppressionAlerts:                   alerts,
	}

	// prior alternates configured/unconfigured, in settingIpsModel's own
	// declaration order, exercising both branches of ipsAfterReceive for
	// every kind: AdvancedFilteringPreference configured,
	// ContentFilteringBlockingPageEnabled not, EnabledCategories configured,
	// and so on through SuppressionAlerts.
	prior := settingIpsModel{
		AdvancedFilteringPreference: types.StringValue("manual"),
		// ContentFilteringBlockingPageEnabled left null: unconfigured.
		EnabledCategories: categories,
		// EnabledNetworks left null: unconfigured.
		Honeypot: honeypot,
		// HoneypotEnabled left null: unconfigured.
		// IPSMode left null: unconfigured.
		MemoryOptimized: types.BoolValue(true),
		// RestrictTorrents left null: unconfigured.
		// SuppressionWhitelist left null: unconfigured.
		SuppressionAlerts: alerts,
	}

	if diags := ipsAfterReceive(ctx, sdk, model, prior); diags.HasError() {
		t.Fatalf("ipsAfterReceive: %v", diags)
	}

	if model.AdvancedFilteringPreference.IsNull() {
		t.Error("advanced_filtering_preference = null, want the decoded value kept (configured in prior)")
	}
	if !model.ContentFilteringBlockingPageEnabled.IsNull() {
		t.Errorf("content_filtering_blocking_page_enabled = %v, want null (unconfigured in prior)",
			model.ContentFilteringBlockingPageEnabled)
	}
	if model.EnabledCategories.IsNull() {
		t.Error("enabled_categories = null, want the decoded value kept (configured in prior)")
	}
	if !model.EnabledNetworks.IsNull() {
		t.Errorf("enabled_networks = %v, want null (unconfigured in prior)", model.EnabledNetworks)
	}
	if model.Honeypot.IsNull() {
		t.Error("honeypot = null, want the decoded value kept (configured in prior)")
	}
	if !model.HoneypotEnabled.IsNull() {
		t.Errorf("honeypot_enabled = %v, want null (unconfigured in prior)", model.HoneypotEnabled)
	}
	if !model.IPSMode.IsNull() {
		t.Errorf("ips_mode = %v, want null (unconfigured in prior)", model.IPSMode)
	}
	if model.MemoryOptimized.IsNull() {
		t.Error("memory_optimized = null, want the decoded value kept (configured in prior)")
	}
	if !model.RestrictTorrents.IsNull() {
		t.Errorf("restrict_torrents = %v, want null (unconfigured in prior)", model.RestrictTorrents)
	}
	if !model.SuppressionWhitelist.IsNull() {
		t.Errorf("suppression_whitelist = %v, want null (unconfigured in prior)", model.SuppressionWhitelist)
	}
	if model.SuppressionAlerts.IsNull() {
		t.Error("suppression_alerts = null, want the decoded value kept (configured in prior)")
	}
}

// TestIpsKitSpecConformance runs the same conformance instruments every
// other kit descriptor's test applies, scoped to ips's own nested schema
// rather than a whole resource's, since ips is one section of unifi_setting
// rather than a surface of its own.
func TestIpsKitSpecConformance(t *testing.T) {
	ctx := context.Background()
	spec := ipsKitSpec()
	for _, problem := range resourcekit.WireNameProblems(spec) {
		t.Error(problem)
	}
	for _, problem := range resourcekit.NestedProblems(spec) {
		t.Error(problem)
	}
	built := ipsNestedSchema(ctx)
	for _, problem := range resourcekit.ElideProblems(spec, built) {
		t.Error(problem)
	}
	for _, problem := range resourcekit.ZeroReadProblems(spec, built) {
		t.Error(problem)
	}
}

// TestIpsSuppressionKitSpecConformance is TestIpsKitSpecConformance's
// counterpart for ipsSuppressionKitSpec, scoped to ipsSuppressionNestedSchema
// -- the two suppression_alerts/suppression_whitelist attributes
// ips_suppression owns, not the whole ips SingleNestedAttribute.
func TestIpsSuppressionKitSpecConformance(t *testing.T) {
	ctx := context.Background()
	spec := ipsSuppressionKitSpec()
	for _, problem := range resourcekit.WireNameProblems(spec) {
		t.Error(problem)
	}
	for _, problem := range resourcekit.NestedProblems(spec) {
		t.Error(problem)
	}
	built := ipsSuppressionNestedSchema(ctx)
	for _, problem := range resourcekit.ElideProblems(spec, built) {
		t.Error(problem)
	}
	for _, problem := range resourcekit.ZeroReadProblems(spec, built) {
		t.Error(problem)
	}
}

// TestIpsSuppressionAttributesMapToNamedSDKMembers resolves the ambiguity
// schema_model_agreement_test.go's declaredAmbiguous map records for
// "unifi_setting.ips.suppression_alerts.tracking" and
// "unifi_setting.ips.suppression_whitelist": both settingIpsTrackingModel
// and settingIpsWhitelistModel share the same member shape
// (direction/mode/value), so which one a given schema path resolves to
// cannot be told apart by shape alone. This pins the actual wire each
// terraform attribute reaches: suppression_whitelist must land on
// settings.IpsSuppression's own "whitelist" member, and an alert's own
// tracking sub-list must stay scoped to that one alert's own
// SettingIpsSuppressionAlerts.Tracking -- never confused with the top-level
// whitelist, despite the identical shape -- in both directions (ToSDK/write
// and ToModel/read).
func TestIpsSuppressionAttributesMapToNamedSDKMembers(t *testing.T) {
	ctx := context.Background()
	spec := ipsSuppressionKitSpec()

	whitelistType := types.ObjectType{AttrTypes: ipsWhitelistAttrTypes}
	trackingType := types.ObjectType{AttrTypes: ipsTrackingAttrTypes}
	alertType := types.ObjectType{AttrTypes: ipsAlertAttrTypes}

	whitelist, diags := types.ListValueFrom(ctx, whitelistType, []settingIpsWhitelistModel{{
		Direction: types.StringValue("src"),
		Mode:      types.StringValue("subnet"),
		Value:     types.StringValue("10.0.0.0/24"),
	}})
	if diags.HasError() {
		t.Fatalf("building suppression_whitelist: %v", diags)
	}
	tracking, diags := types.ListValueFrom(ctx, trackingType, []settingIpsTrackingModel{{
		Direction: types.StringValue("dest"),
		Mode:      types.StringValue("network"),
		Value:     types.StringValue("net-id-1"),
	}})
	if diags.HasError() {
		t.Fatalf("building the alert's own tracking: %v", diags)
	}
	alerts, diags := types.ListValueFrom(ctx, alertType, []settingIpsAlertModel{{
		Category:  types.StringValue("malware"),
		Gid:       types.Int64Value(1),
		ID:        types.Int64Value(2001),
		Signature: types.StringValue("ET MALWARE"),
		Type:      types.StringValue("track"),
		Tracking:  tracking,
	}})
	if diags.HasError() {
		t.Fatalf("building suppression_alerts: %v", diags)
	}

	model := &settingIpsModel{SuppressionWhitelist: whitelist, SuppressionAlerts: alerts}
	sdk, diags := spec.ToSDK(ctx, model)
	if diags.HasError() {
		t.Fatalf("ToSDK: %v", diags)
	}

	if len(sdk.Whitelist) != 1 || sdk.Whitelist[0].Direction != "src" ||
		sdk.Whitelist[0].Mode != "subnet" || sdk.Whitelist[0].Value != "10.0.0.0/24" {
		t.Fatalf("Whitelist = %+v, want the one configured suppression_whitelist entry, "+
			"named on the wire as \"whitelist\"", sdk.Whitelist)
	}
	if len(sdk.Alerts) != 1 {
		t.Fatalf("Alerts = %+v, want exactly the one configured suppression_alerts entry", sdk.Alerts)
	}
	alert := sdk.Alerts[0]
	if alert.Category != "malware" || alert.Gid == nil || *alert.Gid != 1 ||
		alert.ID == nil || *alert.ID != 2001 || alert.Signature != "ET MALWARE" || alert.Type != "track" {
		t.Fatalf("Alerts[0] = %+v, want the configured suppression_alerts entry", alert)
	}
	if len(alert.Tracking) != 1 || alert.Tracking[0].Direction != "dest" ||
		alert.Tracking[0].Mode != "network" || alert.Tracking[0].Value != "net-id-1" {
		t.Fatalf("Alerts[0].Tracking = %+v, want the alert's own tracking entry -- not "+
			"the top-level suppression_whitelist entry, whose members share the same shape",
			alert.Tracking)
	}

	// And the read direction: settings.IpsSuppression's "alerts"/"whitelist"
	// must decode onto suppression_alerts/suppression_whitelist
	// respectively, not swapped.
	var out settingIpsModel
	if diags := spec.ToModel(ctx, sdk, &out, ""); diags.HasError() {
		t.Fatalf("ToModel: %v", diags)
	}
	var outWhitelist []settingIpsWhitelistModel
	if diags := out.SuppressionWhitelist.ElementsAs(ctx, &outWhitelist, false); diags.HasError() {
		t.Fatalf("decoding suppression_whitelist: %v", diags)
	}
	if len(outWhitelist) != 1 || outWhitelist[0].Value.ValueString() != "10.0.0.0/24" {
		t.Errorf("suppression_whitelist read back as %+v, want the one whitelist entry", outWhitelist)
	}
	var outAlerts []settingIpsAlertModel
	if diags := out.SuppressionAlerts.ElementsAs(ctx, &outAlerts, false); diags.HasError() {
		t.Fatalf("decoding suppression_alerts: %v", diags)
	}
	if len(outAlerts) != 1 || outAlerts[0].Signature.ValueString() != "ET MALWARE" ||
		outAlerts[0].Gid.ValueInt64() != 1 {
		t.Errorf("suppression_alerts read back as %+v, want the one alert entry", outAlerts)
	}
}

// TestIpsHoneypotListReplacesNotAppends ports the deleted
// TestIpsHoneypotReplacesTheControllersList for the masked-write design: a
// prior write that landed two honeypots on the controller must not leak
// into a later write that configures one -- the second PUT carries exactly
// the plan's own single entry, not a union of both. Under the old
// read-modify-write writeIpsSection, a mapper had to nil the base's own
// Honeypot slice by hand before appending (setting_resource.go's own
// comment on the deleted ipsModelToSetting said so verbatim); under the kit
// design this falls out for free -- ipsKitSpec.ToSDK builds a fresh
// settings.Ips from spec.New() and ObjectListField.ToSDK builds its slice
// from the model alone, with no read of the controller's current value at
// all.
func TestIpsHoneypotListReplacesNotAppends(t *testing.T) {
	var lastBody map[string]json.RawMessage
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if req.URL.Path == "/proxy/network/status" {
			_, _ = w.Write([]byte(`{"meta":{"server_version":"10.4.57"}}`))
			return
		}
		raw, _ := io.ReadAll(req.Body)
		if err := json.Unmarshal(raw, &lastBody); err != nil {
			t.Errorf("the provider sent a body that is not an object: %v", err)
		}
		_, _ = w.Write(append(append([]byte(`{"data":[`), raw...), []byte(`]}`)...))
	}))
	t.Cleanup(server.Close)

	api, err := ui.New(context.Background(), &ui.Config{BaseURL: server.URL, APIKey: "test-key"})
	if err != nil {
		t.Fatalf("create the API client: %v", err)
	}

	ctx := context.Background()
	spec := ipsKitSpec()
	spec.Backend = ipsKitBackend(api)
	honeypotType := types.ObjectType{AttrTypes: ipsHoneypotAttrTypes}

	twoHoneypots, diags := types.ListValueFrom(ctx, honeypotType, []settingIpsHoneypotModel{
		{IPAddress: types.StringValue("10.0.0.1"), NetworkID: types.StringValue("net-a"), Version: types.StringValue("v4")},
		{IPAddress: types.StringValue("10.0.0.2"), NetworkID: types.StringValue("net-b"), Version: types.StringValue("v4")},
	})
	if diags.HasError() {
		t.Fatalf("building the prior honeypot list: %v", diags)
	}
	priorSDK, diags := spec.ToSDK(ctx, &settingIpsModel{Honeypot: twoHoneypots})
	if diags.HasError() {
		t.Fatalf("ToSDK (prior): %v", diags)
	}
	// The "prior write": the controller now holds two honeypots.
	if _, err := spec.Backend.UpdateFields(ctx, "default", priorSDK, "honeypot"); err != nil {
		t.Fatalf("UpdateFields (prior): %v", err)
	}
	var priorSent []map[string]any
	if err := json.Unmarshal(lastBody["honeypot"], &priorSent); err != nil {
		t.Fatalf("decoding the prior honeypot body: %v", err)
	}
	if len(priorSent) != 2 {
		t.Fatalf("the prior write sent %d honeypot(s), want 2 -- the control for what follows", len(priorSent))
	}

	oneHoneypot, diags := types.ListValueFrom(ctx, honeypotType, []settingIpsHoneypotModel{
		{IPAddress: types.StringValue("10.1.10.254"), NetworkID: types.StringValue("net-configured"), Version: types.StringValue("v4")},
	})
	if diags.HasError() {
		t.Fatalf("building the configured honeypot list: %v", diags)
	}
	sdk, diags := spec.ToSDK(ctx, &settingIpsModel{Honeypot: oneHoneypot})
	if diags.HasError() {
		t.Fatalf("ToSDK: %v", diags)
	}
	if _, err := spec.Backend.UpdateFields(ctx, "default", sdk, "honeypot"); err != nil {
		t.Fatalf("UpdateFields: %v", err)
	}

	var honeypots []map[string]any
	if err := json.Unmarshal(lastBody["honeypot"], &honeypots); err != nil {
		t.Fatalf("decoding honeypot: %v", err)
	}
	if len(honeypots) != 1 {
		t.Fatalf("after a prior write of two honeypots, a plan naming one sent %d honeypot(s), "+
			"want exactly 1 -- the list must replace, not append to, what the controller "+
			"already held: %v", len(honeypots), honeypots)
	}
	if honeypots[0]["network_id"] != "net-configured" {
		t.Errorf("honeypot.0.network_id = %v, want net-configured", honeypots[0]["network_id"])
	}
}

// TestIpsSpecMasksOnlyTheFieldsThePlanSet ports
// TestIpsPartialBlockKeepsWhatTheControllerHolds' worry into what actually
// matters under a masked write: settings.Ips force-emits four fields (no
// omitempty), which the deleted ipsModelToSetting had to protect with a
// read-modify-write base. UpdateSettingFields' own mask makes that
// unnecessary -- a plan naming one field produces a PUT body with exactly
// that field plus "key", none of the four force-emitted bools included.
func TestIpsSpecMasksOnlyTheFieldsThePlanSet(t *testing.T) {
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

	backend := ipsKitBackend(api)
	sdk := &settings.Ips{RestrictTorrents: true}
	if _, err := backend.UpdateFields(context.Background(), "default", sdk, "restrict_torrents"); err != nil {
		t.Fatalf("UpdateFields: %v", err)
	}

	want := map[string]bool{"key": true, "restrict_torrents": true}
	if len(body) != len(want) {
		t.Fatalf("PUT body has %d key(s) %v, want exactly %v", len(body), keysOf(body), want)
	}
	for name := range want {
		if _, ok := body[name]; !ok {
			t.Errorf("PUT body is missing %q; got %v", name, keysOf(body))
		}
	}
	// The four fields settings.Ips force-emits without omitempty -- a census
	// re-run by hand from ips.generated.go's own json tags. None of them was
	// named by the mask, so none should appear.
	for _, forceEmitted := range []string{
		"content_filtering_blocking_page_enabled", "honeypot_enabled", "memory_optimized",
	} {
		if _, ok := body[forceEmitted]; ok {
			t.Errorf("PUT body carries %q, a force-emitted field the mask never named; "+
				"the masked write is supposed to leave it out", forceEmitted)
		}
	}
}

// TestIpsSuppressionIsWrittenOnlyWhenConfigured pins the deleted
// ipsSuppressionConfigured's own predicate, now falling out of
// SpecDocument's own empty-mask skip: a plan that names neither
// suppression_alerts nor suppression_whitelist produces an empty mask, and
// specDocumentWrite returns before Backend.UpdateFields (and so before any
// HTTP call) ever runs.
func TestIpsSuppressionIsWrittenOnlyWhenConfigured(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/proxy/network/status" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"meta":{"server_version":"10.4.57"}}`))
			return
		}
		t.Errorf("unexpected request %s %s; an unconfigured ips_suppression must not touch "+
			"the controller at all", req.Method, req.URL.Path)
	}))
	t.Cleanup(server.Close)

	api, err := ui.New(context.Background(), &ui.Config{BaseURL: server.URL, APIKey: "test-key"})
	if err != nil {
		t.Fatalf("create the API client: %v", err)
	}

	document := ipsSuppressionKitDocument(api)
	plan := &settingIpsModel{
		// Both suppression lists left null: unconfigured. A non-suppression
		// attribute is set to prove the predicate is scoped to suppression
		// alone, not "is the section object configured at all".
		IPSMode: types.StringValue("disabled"),
	}
	prior := *plan
	diags := document.Write(context.Background(), "default", plan, &prior, "Creating")
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
}

// TestIpsSuppressionNotFoundIsTheControllerTooOldDiagnostic pins
// ipsSuppressionKitDocument's OnWriteNotFound: a controller old enough to
// lack the ips_suppression endpoint answers the write with a NotFoundError
// (UpdateSetting's own len(data)!=1 rule), which must surface as the
// deleted writeIpsSuppression's exact diagnostic, not the generic "Error
// Creating IPS Suppression Setting".
func TestIpsSuppressionNotFoundIsTheControllerTooOldDiagnostic(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case req.URL.Path == "/proxy/network/status":
			_, _ = w.Write([]byte(`{"meta":{"server_version":"10.4.57"}}`))
		case req.Method == http.MethodPut:
			// The controller does not recognise ips_suppression:
			// UpdateSetting synthesizes a NotFoundError from a reply with
			// no data.
			_, _ = w.Write([]byte(`{"meta":{},"data":[]}`))
		default:
			t.Errorf("unexpected request %s %s", req.Method, req.URL.Path)
		}
	}))
	t.Cleanup(server.Close)

	api, err := ui.New(context.Background(), &ui.Config{BaseURL: server.URL, APIKey: "test-key"})
	if err != nil {
		t.Fatalf("create the API client: %v", err)
	}

	document := ipsSuppressionKitDocument(api)
	whitelist, diags := types.ListValueFrom(context.Background(),
		types.ObjectType{AttrTypes: ipsWhitelistAttrTypes},
		[]settingIpsWhitelistModel{{
			Direction: types.StringValue("both"),
			Mode:      types.StringValue("ip"),
			Value:     types.StringValue("10.0.0.5"),
		}})
	if diags.HasError() {
		t.Fatalf("building the configured whitelist: %v", diags)
	}
	plan := &settingIpsModel{SuppressionWhitelist: whitelist}
	prior := *plan
	diags = document.Write(context.Background(), "default", plan, &prior, "Creating")
	if !diags.HasError() {
		t.Fatal("expected the controller-too-old diagnostic")
	}
	got := diags.Errors()
	wantSummary := "IPS Suppression Not Supported By This Controller"
	if len(got) != 1 || got[0].Summary() != wantSummary {
		t.Fatalf("diagnostics = %v, want a single %q summary", diags, wantSummary)
	}
	wantDetail := "The `suppression_alerts` and `suppression_whitelist` attributes are stored in " +
		"the `ips_suppression` setting, which this controller does not expose. UniFi " +
		"Network 10.x moved them out of the `ips` setting. Remove them from the `ips` " +
		"block, or upgrade the controller."
	if got[0].Detail() != wantDetail {
		t.Errorf("detail = %q, want the legacy writeIpsSuppression text verbatim:\n%q", got[0].Detail(), wantDetail)
	}
}

// TestIpsSuppressionDocumentReadNotFoundYieldsEmptyLists pins
// ipsSuppressionKitBackend's own not-found tolerance (see
// setting_ips_descriptor.go's top comment for why this lives at the
// backend rather than through OnReadNotFound the way usg_geo's does): a
// controller that predates the split, or a site that never configured
// suppression, answers Read with a NotFoundError, which the backend
// absorbs into a zero &settings.IpsSuppression{} rather than propagating
// it -- so Spec.ToModel still runs, and ObjectListField's own KeepZero
// elision decodes the zero Alerts/Whitelist slices as empty, non-null,
// lists. This is a Document-level behaviour, independent of whether the
// plan configured either list: SpecSection's own ipsAfterReceive is what
// later nulls it back down for an attribute the plan never named.
func TestIpsSuppressionDocumentReadNotFoundYieldsEmptyLists(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if req.URL.Path == "/proxy/network/status" {
			_, _ = w.Write([]byte(`{"meta":{"server_version":"10.4.57"}}`))
			return
		}
		_, _ = w.Write([]byte(`{"meta":{},"data":[]}`))
	}))
	t.Cleanup(server.Close)

	api, err := ui.New(context.Background(), &ui.Config{BaseURL: server.URL, APIKey: "test-key"})
	if err != nil {
		t.Fatalf("create the API client: %v", err)
	}

	document := ipsSuppressionKitDocument(api)
	var model settingIpsModel
	diags := document.Read(context.Background(), "default", &model)
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	if model.SuppressionAlerts.IsNull() || len(model.SuppressionAlerts.Elements()) != 0 {
		t.Errorf("suppression_alerts = %v, want a non-null empty list (readIpsSection's own "+
			"not-found tolerance, reproduced at the backend)", model.SuppressionAlerts)
	}
	if model.SuppressionWhitelist.IsNull() || len(model.SuppressionWhitelist.Elements()) != 0 {
		t.Errorf("suppression_whitelist = %v, want a non-null empty list", model.SuppressionWhitelist)
	}
}

// TestIpsNestedSchemaHasExactlyItsAttributes guards ipsNestedSchema's type
// assertion against a generator regression: "ips" moving off
// SingleNestedAttribute would panic every conformance test above instead
// of naming the actual problem, so this pins the shape ahead of that.
func TestIpsNestedSchemaHasExactlyItsAttributes(t *testing.T) {
	ctx := context.Background()
	built := resource_setting.SettingResourceSchema(ctx)
	if _, ok := built.Attributes["ips"]; !ok {
		t.Fatal(`the generated setting schema has no "ips" attribute`)
	}
	nested := ipsNestedSchema(ctx)
	if len(nested.Attributes) != 11 {
		t.Errorf("ips has %d attribute(s), want 11; update ipsKitSpec/ipsSuppressionKitSpec "+
			"and this count together", len(nested.Attributes))
	}
}

// TestIpsSuppressionNestedSchemaHasExactlyItsAttributes is
// TestIpsNestedSchemaHasExactlyItsAttributes' counterpart for
// ipsSuppressionNestedSchema: exactly the two
// suppression_alerts/suppression_whitelist attributes, filtered out of
// ips's own eleven.
func TestIpsSuppressionNestedSchemaHasExactlyItsAttributes(t *testing.T) {
	ctx := context.Background()
	nested := ipsSuppressionNestedSchema(ctx)
	if len(nested.Attributes) != 2 {
		t.Errorf("ips_suppression has %d attribute(s), want 2; update ipsSuppressionKitSpec "+
			"and this count together", len(nested.Attributes))
	}
	for _, name := range []string{"suppression_alerts", "suppression_whitelist"} {
		if nested.Attributes[name] == nil {
			t.Errorf("ips_suppression's nested schema is missing %q", name)
		}
	}
}
