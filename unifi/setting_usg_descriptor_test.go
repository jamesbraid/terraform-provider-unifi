package unifi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-timetypes/timetypes"
	"github.com/hashicorp/terraform-plugin-framework/types"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
	resource_setting "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_setting"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/resourcekit"
)

// TestUsgAfterReceiveNullsWhatThePlanDidNotName pins usgAfterReceive against
// the deleted usgSettingToModel's own behaviour, read off setting_resource.go
// before the migration (git history): every one of usg's thirty-seven
// attributes -- the thirty-three usgKitSpec maps plus the four
// geo_ip_filtering_* ones usgGeoKitSpec's own document hydrates into this
// same shared model -- was conditioned on
// `!plan.<Attr>.IsNull() && !plan.<Attr>.IsUnknown()`, mirroring the
// controller's value only when the practitioner's own plan/prior named the
// attribute, else forcing null. This exercises all thirty-seven in one
// prior/model pair, alternating configured/unconfigured field by field so
// both branches are covered for every kind (bool, string, duration, and the
// dns_verification object).
func TestUsgAfterReceiveNullsWhatThePlanDidNotName(t *testing.T) {
	ctx := context.Background()
	sdk := &settings.Usg{}

	dnsVerification, diags := types.ObjectValueFrom(ctx, dnsVerificationAttrTypes, dnsVerificationModel{
		Domain:             types.StringValue("example.com"),
		PrimaryDNSServer:   types.StringValue("1.1.1.1"),
		SecondaryDNSServer: types.StringValue("1.0.0.1"),
		SettingPreference:  types.StringValue("manual"),
	})
	if diags.HasError() {
		t.Fatalf("building dns_verification: %v", diags)
	}
	duration := timetypes.NewGoDurationValue(30 * time.Second)

	// model starts as whatever Spec.ToModel (usgKitSpec's own Fields) and
	// usgGeoKitSpec's own document Read would have decoded straight off the
	// wire, before usgAfterReceive applies the plan-conditioned nulls --
	// every field carries a concrete, non-null value.
	model := &settingUSGModel{
		BroadcastPing:                  types.BoolValue(true),
		DNSVerification:                dnsVerification,
		FtpModule:                      types.BoolValue(true),
		GeoIPFilteringBlock:            types.StringValue("block"),
		GeoIPFilteringCountries:        types.StringValue("NZ,AU"),
		GeoIPFilteringEnabled:          types.BoolValue(true),
		GeoIPFilteringTrafficDirection: types.StringValue("both"),
		GreModule:                      types.BoolValue(true),
		H323Module:                     types.BoolValue(true),
		ICMPTimeout:                    duration,
		MssClamp:                       types.StringValue("auto"),
		OffloadAccounting:              types.BoolValue(true),
		OffloadL2Blocking:              types.BoolValue(true),
		OffloadSch:                     types.BoolValue(true),
		OtherTimeout:                   duration,
		PptpModule:                     types.BoolValue(true),
		ReceiveRedirects:               types.BoolValue(true),
		SendRedirects:                  types.BoolValue(true),
		SipModule:                      types.BoolValue(true),
		SynCookies:                     types.BoolValue(true),
		TCPCloseTimeout:                duration,
		TCPCloseWaitTimeout:            duration,
		TCPEstablishedTimeout:          duration,
		TCPFinWaitTimeout:              duration,
		TCPLastAckTimeout:              duration,
		TCPSynRecvTimeout:              duration,
		TCPSynSentTimeout:              duration,
		TCPTimeWaitTimeout:             duration,
		TFTPModule:                     types.BoolValue(true),
		TimeoutSettingPreference:       types.StringValue("auto"),
		UDPOtherTimeout:                duration,
		UDPStreamTimeout:               duration,
		UnbindWANMonitors:              types.BoolValue(true),
		UPnPEnabled:                    types.BoolValue(true),
		UPnPNATPmpEnabled:              types.BoolValue(true),
		UPnPSecureMode:                 types.BoolValue(true),
		UPnPWANInterface:               types.StringValue("WAN"),
	}

	// prior alternates configured/unconfigured field by field, in
	// settingUSGModel's own declaration order, so every field's own branch
	// of usgAfterReceive is exercised: BroadcastPing configured,
	// DNSVerification not, FtpModule configured, GeoIPFilteringBlock not,
	// and so on through UPnPWANInterface.
	prior := settingUSGModel{
		BroadcastPing: types.BoolValue(true),
		// DNSVerification left null: unconfigured.
		FtpModule: types.BoolValue(true),
		// GeoIPFilteringBlock left null: unconfigured.
		GeoIPFilteringCountries: types.StringValue("NZ,AU"),
		// GeoIPFilteringEnabled left null: unconfigured.
		GeoIPFilteringTrafficDirection: types.StringValue("both"),
		// GreModule left null: unconfigured.
		H323Module: types.BoolValue(true),
		// ICMPTimeout left null: unconfigured.
		MssClamp: types.StringValue("auto"),
		// OffloadAccounting left null: unconfigured.
		OffloadL2Blocking: types.BoolValue(true),
		// OffloadSch left null: unconfigured.
		OtherTimeout: duration,
		// PptpModule left null: unconfigured.
		ReceiveRedirects: types.BoolValue(true),
		// SendRedirects left null: unconfigured.
		SipModule: types.BoolValue(true),
		// SynCookies left null: unconfigured.
		TCPCloseTimeout: duration,
		// TCPCloseWaitTimeout left null: unconfigured.
		TCPEstablishedTimeout: duration,
		// TCPFinWaitTimeout left null: unconfigured.
		TCPLastAckTimeout: duration,
		// TCPSynRecvTimeout left null: unconfigured.
		TCPSynSentTimeout: duration,
		// TCPTimeWaitTimeout left null: unconfigured.
		TFTPModule: types.BoolValue(true),
		// TimeoutSettingPreference left null: unconfigured.
		UDPOtherTimeout: duration,
		// UDPStreamTimeout left null: unconfigured.
		UnbindWANMonitors: types.BoolValue(true),
		// UPnPEnabled left null: unconfigured.
		UPnPNATPmpEnabled: types.BoolValue(true),
		// UPnPSecureMode left null: unconfigured.
		UPnPWANInterface: types.StringValue("WAN"),
	}

	if diags := usgAfterReceive(ctx, sdk, model, prior); diags.HasError() {
		t.Fatalf("usgAfterReceive: %v", diags)
	}

	type boolCase struct {
		name       string
		got        types.Bool
		configured bool
	}
	for _, c := range []boolCase{
		{"broadcast_ping", model.BroadcastPing, true},
		{"ftp_module", model.FtpModule, true},
		{"geo_ip_filtering_enabled", model.GeoIPFilteringEnabled, false},
		{"gre_module", model.GreModule, false},
		{"h323_module", model.H323Module, true},
		{"offload_accounting", model.OffloadAccounting, false},
		{"offload_l2_blocking", model.OffloadL2Blocking, true},
		{"offload_sch", model.OffloadSch, false},
		{"pptp_module", model.PptpModule, false},
		{"receive_redirects", model.ReceiveRedirects, true},
		{"send_redirects", model.SendRedirects, false},
		{"sip_module", model.SipModule, true},
		{"syn_cookies", model.SynCookies, false},
		{"tftp_module", model.TFTPModule, true},
		{"unbind_wan_monitors", model.UnbindWANMonitors, true},
		{"upnp_enabled", model.UPnPEnabled, false},
		{"upnp_nat_pmp_enabled", model.UPnPNATPmpEnabled, true},
		{"upnp_secure_mode", model.UPnPSecureMode, false},
	} {
		if c.configured {
			if c.got.IsNull() {
				t.Errorf("%s = null, want the decoded value kept (configured in prior)", c.name)
			}
		} else if !c.got.IsNull() {
			t.Errorf("%s = %v, want null (unconfigured in prior, so it must not drift)", c.name, c.got)
		}
	}

	type stringCase struct {
		name       string
		got        types.String
		configured bool
	}
	for _, c := range []stringCase{
		{"geo_ip_filtering_block", model.GeoIPFilteringBlock, false},
		{"geo_ip_filtering_countries", model.GeoIPFilteringCountries, true},
		{"geo_ip_filtering_traffic_direction", model.GeoIPFilteringTrafficDirection, true},
		{"mss_clamp", model.MssClamp, true},
		{"timeout_setting_preference", model.TimeoutSettingPreference, false},
		{"upnp_wan_interface", model.UPnPWANInterface, true},
	} {
		if c.configured {
			if c.got.IsNull() {
				t.Errorf("%s = null, want the decoded value kept (configured in prior)", c.name)
			}
		} else if !c.got.IsNull() {
			t.Errorf("%s = %v, want null (unconfigured in prior, so it must not drift)", c.name, c.got)
		}
	}

	type durationCase struct {
		name       string
		got        timetypes.GoDuration
		configured bool
	}
	for _, c := range []durationCase{
		{"icmp_timeout", model.ICMPTimeout, false},
		{"other_timeout", model.OtherTimeout, true},
		{"tcp_close_timeout", model.TCPCloseTimeout, true},
		{"tcp_close_wait_timeout", model.TCPCloseWaitTimeout, false},
		{"tcp_established_timeout", model.TCPEstablishedTimeout, true},
		{"tcp_fin_wait_timeout", model.TCPFinWaitTimeout, false},
		{"tcp_last_ack_timeout", model.TCPLastAckTimeout, true},
		{"tcp_syn_recv_timeout", model.TCPSynRecvTimeout, false},
		{"tcp_syn_sent_timeout", model.TCPSynSentTimeout, true},
		{"tcp_time_wait_timeout", model.TCPTimeWaitTimeout, false},
		{"udp_other_timeout", model.UDPOtherTimeout, true},
		{"udp_stream_timeout", model.UDPStreamTimeout, false},
	} {
		if c.configured {
			if c.got.IsNull() {
				t.Errorf("%s = null, want the decoded value kept (configured in prior)", c.name)
			}
		} else if !c.got.IsNull() {
			t.Errorf("%s = %v, want null (unconfigured in prior, so it must not drift)", c.name, c.got)
		}
	}

	// dns_verification is left unconfigured in prior (see the
	// declaration-order table above), so it must have been nulled.
	if !model.DNSVerification.IsNull() {
		t.Errorf("dns_verification = %v, want null (unconfigured in prior, so it must not drift)",
			model.DNSVerification)
	}
}

// TestUsgKitSpecConformance runs the same conformance instruments every
// other kit descriptor's test applies (see e.g. dns_record's case in
// descriptor_elide_test.go), scoped to usg's own nested schema rather than
// a whole resource's, since usg is one section of unifi_setting rather than
// a surface of its own.
func TestUsgKitSpecConformance(t *testing.T) {
	ctx := context.Background()
	spec := usgKitSpec()
	for _, problem := range resourcekit.WireNameProblems(spec) {
		t.Error(problem)
	}
	for _, problem := range resourcekit.NestedProblems(spec) {
		t.Error(problem)
	}
	built := usgNestedSchema(ctx)
	for _, problem := range resourcekit.ElideProblems(spec, built) {
		t.Error(problem)
	}
	for _, problem := range resourcekit.ZeroReadProblems(spec, built) {
		t.Error(problem)
	}
}

// TestUsgGeoKitSpecConformance is TestUsgKitSpecConformance's counterpart
// for usgGeoKitSpec, scoped to usgGeoNestedSchema -- the four
// geo_ip_filtering_* attributes usg_geo owns, not the whole usg
// SingleNestedAttribute.
func TestUsgGeoKitSpecConformance(t *testing.T) {
	ctx := context.Background()
	spec := usgGeoKitSpec()
	for _, problem := range resourcekit.WireNameProblems(spec) {
		t.Error(problem)
	}
	for _, problem := range resourcekit.NestedProblems(spec) {
		t.Error(problem)
	}
	built := usgGeoNestedSchema(ctx)
	for _, problem := range resourcekit.ElideProblems(spec, built) {
		t.Error(problem)
	}
	for _, problem := range resourcekit.ZeroReadProblems(spec, built) {
		t.Error(problem)
	}
}

// TestUsgGeoRenamesBlockToActionOnTheWire pins the one rename UniFi Network
// 10.x's split introduced: the Terraform attribute stayed
// geo_ip_filtering_block, but the wire (and settings.SettingUsgGeoIPFiltering's
// own field) calls it action. Both directions are exercised: ToSDK (write)
// and ToModel (read).
func TestUsgGeoRenamesBlockToActionOnTheWire(t *testing.T) {
	ctx := context.Background()
	spec := usgGeoKitSpec()

	model := &settingUSGModel{GeoIPFilteringBlock: types.StringValue("block")}
	sdk, diags := spec.ToSDK(ctx, model)
	if diags.HasError() {
		t.Fatalf("ToSDK: %v", diags)
	}
	if sdk.Action != "block" {
		t.Errorf("ToSDK: Action = %q, want block", sdk.Action)
	}

	var out settingUSGModel
	if diags := spec.ToModel(ctx, &settings.SettingUsgGeoIPFiltering{Action: "allow"}, &out, ""); diags.HasError() {
		t.Fatalf("ToModel: %v", diags)
	}
	if out.GeoIPFilteringBlock.ValueString() != "allow" {
		t.Errorf("ToModel: geo_ip_filtering_block = %q, want allow", out.GeoIPFilteringBlock.ValueString())
	}
}

// TestUsgGeoIsWrittenOnlyWhenConfigured pins the deleted usgGeoConfigured's
// own predicate, now falling out of SpecDocument's own empty-mask skip: a
// plan that names none of the four geo_ip_filtering_* attributes produces
// an empty mask, and specDocumentWrite returns before Backend.UpdateFields
// (and so before any HTTP call) ever runs.
func TestUsgGeoIsWrittenOnlyWhenConfigured(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/proxy/network/status" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"meta":{"server_version":"10.4.57"}}`))
			return
		}
		t.Errorf("unexpected request %s %s; an unconfigured usg_geo must not touch the "+
			"controller at all", req.Method, req.URL.Path)
	}))
	t.Cleanup(server.Close)

	api, err := ui.New(context.Background(), &ui.Config{BaseURL: server.URL, APIKey: "test-key"})
	if err != nil {
		t.Fatalf("create the API client: %v", err)
	}

	document := usgGeoKitDocument(api)
	plan := &settingUSGModel{
		// Every geo_ip_filtering_* attribute left null: unconfigured. A
		// non-geo attribute is set to prove the predicate is scoped to geo
		// alone, not "is the section object configured at all".
		FtpModule: types.BoolValue(true),
	}
	prior := *plan
	diags := document.Write(context.Background(), "default", plan, &prior, "Creating")
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
}

// TestUsgGeoNotFoundIsTheControllerTooOldDiagnostic pins usgGeoKitDocument's
// OnWriteNotFound: a controller old enough to lack the usg_geo endpoint
// answers the write with a NotFoundError (UpdateSetting's own
// len(data)!=1 rule), which must surface as the deleted writeUsgGeo's exact
// diagnostic, not the generic "Error Creating USG Geo Setting".
func TestUsgGeoNotFoundIsTheControllerTooOldDiagnostic(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case req.URL.Path == "/proxy/network/status":
			_, _ = w.Write([]byte(`{"meta":{"server_version":"10.4.57"}}`))
		case req.Method == http.MethodGet:
			// The pre-write read: absent, same as a controller that
			// predates the split -- usgGeoKitBackend treats this as
			// "start from empty", not an error.
			_, _ = w.Write([]byte(`{"meta":{},"data":[]}`))
		case req.Method == http.MethodPut:
			// The controller does not recognise usg_geo: UpdateSetting
			// synthesizes a NotFoundError from a reply with no data.
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

	document := usgGeoKitDocument(api)
	plan := &settingUSGModel{GeoIPFilteringEnabled: types.BoolValue(true)}
	prior := *plan
	diags := document.Write(context.Background(), "default", plan, &prior, "Creating")
	if !diags.HasError() {
		t.Fatal("expected the controller-too-old diagnostic")
	}
	got := diags.Errors()
	wantSummary := "Geo IP Filtering Not Supported By This Controller"
	if len(got) != 1 || got[0].Summary() != wantSummary {
		t.Fatalf("diagnostics = %v, want a single %q summary", diags, wantSummary)
	}
	wantDetail := "The `geo_ip_filtering_*` attributes are stored in the `usg_geo` setting, which " +
		"this controller does not expose. UniFi Network 10.x moved them out of the " +
		"`usg` setting. Remove them from the `usg` block, or upgrade the controller."
	if got[0].Detail() != wantDetail {
		t.Errorf("detail = %q, want the legacy writeUsgGeo text verbatim:\n%q", got[0].Detail(), wantDetail)
	}
}

// TestUsgGeoBackendPreservesUnmanagedSubFieldsOnAPartialWrite ports the
// deleted TestUsgGeoPreservesUnmanagedFields: usg_geo's own ip_filtering
// object has no per-member wire (UpdateSettingFields' mask only reaches
// UsgGeo's own top-level json tag), so a write naming only "countries" must
// still preserve action/enabled/traffic_direction as the controller already
// held them, not reset them to Go zero.
func TestUsgGeoBackendPreservesUnmanagedSubFieldsOnAPartialWrite(t *testing.T) {
	var putBody map[string]json.RawMessage
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case req.URL.Path == "/proxy/network/status":
			_, _ = w.Write([]byte(`{"meta":{"server_version":"10.4.57"}}`))
		case req.Method == http.MethodGet:
			_, _ = w.Write([]byte(`{"meta":{},"data":[` +
				`{"_id":"1","key":"usg_geo","ip_filtering":` +
				`{"action":"allow","countries":"NZ","enabled":true,"traffic_direction":"ingress"}}` +
				`]}`))
		case req.Method == http.MethodPut:
			raw, _ := io.ReadAll(req.Body)
			var decoded struct {
				IPFiltering map[string]json.RawMessage `json:"ip_filtering"`
			}
			if err := json.Unmarshal(raw, &decoded); err != nil {
				t.Errorf("the provider sent a body that is not an object: %v", err)
			}
			putBody = decoded.IPFiltering
			_, _ = w.Write(append(append([]byte(`{"data":[`), raw...), []byte(`]}`)...))
		default:
			t.Errorf("unexpected request %s %s", req.Method, req.URL.Path)
		}
	}))
	t.Cleanup(server.Close)

	api, err := ui.New(context.Background(), &ui.Config{BaseURL: server.URL, APIKey: "test-key"})
	if err != nil {
		t.Fatalf("create the API client: %v", err)
	}

	document := usgGeoKitDocument(api)
	// Only countries is managed here.
	plan := &settingUSGModel{GeoIPFilteringCountries: types.StringValue("NZ,AU")}
	prior := *plan
	if diags := document.Write(context.Background(), "default", plan, &prior, "Creating"); diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}

	var countries string
	if err := json.Unmarshal(putBody["countries"], &countries); err != nil {
		t.Fatalf("decoding countries: %v", err)
	}
	if countries != "NZ,AU" {
		t.Errorf("countries = %q, want the managed value NZ,AU", countries)
	}
	var action string
	if err := json.Unmarshal(putBody["action"], &action); err != nil {
		t.Fatalf("decoding action: %v", err)
	}
	if action != "allow" {
		t.Errorf("action = %q, want the stored value allow", action)
	}
	var enabled bool
	if err := json.Unmarshal(putBody["enabled"], &enabled); err != nil {
		t.Fatalf("decoding enabled: %v", err)
	}
	if !enabled {
		t.Error("enabled was reset; the stored value should survive")
	}
	var trafficDirection string
	if err := json.Unmarshal(putBody["traffic_direction"], &trafficDirection); err != nil {
		t.Fatalf("decoding traffic_direction: %v", err)
	}
	if trafficDirection != "ingress" {
		t.Errorf("traffic_direction = %q, want the stored value ingress", trafficDirection)
	}
}

// TestUsgGeoBackendReadTreatsIPFilteringNilAsZero ports half of the deleted
// TestUsgGeoAbsentSetting's read-side cases: a usg_geo document whose own
// IPFiltering member is nil (present but never touched) reads back as a
// zero SettingUsgGeoIPFiltering, not a panic on the nil pointer.
func TestUsgGeoBackendReadTreatsIPFilteringNilAsZero(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if req.URL.Path == "/proxy/network/status" {
			_, _ = w.Write([]byte(`{"meta":{"server_version":"10.4.57"}}`))
			return
		}
		_, _ = w.Write([]byte(`{"meta":{},"data":[{"_id":"1","key":"usg_geo"}]}`))
	}))
	t.Cleanup(server.Close)

	api, err := ui.New(context.Background(), &ui.Config{BaseURL: server.URL, APIKey: "test-key"})
	if err != nil {
		t.Fatalf("create the API client: %v", err)
	}

	backend := usgGeoKitBackend(api)
	got, err := backend.Read(context.Background(), "default", "")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if *got != (settings.SettingUsgGeoIPFiltering{}) {
		t.Errorf("Read = %+v, want the zero value", *got)
	}
}

// TestUsgGeoDocumentReadNotFoundLeavesModelUntouched ports the other half of
// the deleted TestUsgGeoAbsentSetting: a controller that predates the split
// (no usg_geo document at all) answers Backend.Read with a NotFoundError,
// which usgGeoKitDocument's own OnReadNotFound (nil) turns into silence --
// no diagnostic, model left exactly as it was -- the same tolerance
// readUSGSection gave a not-found usg_geo. This is a Document-level
// behaviour, not Backend.Read's own: the NotFoundError has to actually
// reach SpecDocument.Read for OnReadNotFound to run.
func TestUsgGeoDocumentReadNotFoundLeavesModelUntouched(t *testing.T) {
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

	document := usgGeoKitDocument(api)
	model := &settingUSGModel{
		GeoIPFilteringBlock:   types.StringValue("untouched"),
		GeoIPFilteringEnabled: types.BoolValue(true),
	}
	diags := document.Read(context.Background(), "default", model)
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	if model.GeoIPFilteringBlock.ValueString() != "untouched" {
		t.Errorf("geo_ip_filtering_block = %v, want untouched (a not-found read must not "+
			"touch the model)", model.GeoIPFilteringBlock)
	}
	if !model.GeoIPFilteringEnabled.ValueBool() {
		t.Error("geo_ip_filtering_enabled changed, want untouched (a not-found read must " +
			"not touch the model)")
	}
}

// TestUsgSpecMasksOnlyTheFieldsThePlanSet ports Test_usgForceEmittedFieldCountIsPinned's
// worry into what actually matters under a masked write: settings.Usg
// force-emits 23 fields (no omitempty), which the deleted usgModelToSetting
// had to protect with a read-modify-write base. UpdateSettingFields' own
// mask makes that unnecessary -- a plan naming one field produces a PUT
// body with exactly that field plus "key", none of the 23 included.
func TestUsgSpecMasksOnlyTheFieldsThePlanSet(t *testing.T) {
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

	backend := usgKitBackend(api)
	sdk := &settings.Usg{FtpModule: true}
	if _, err := backend.UpdateFields(context.Background(), "default", sdk, "ftp_module"); err != nil {
		t.Fatalf("UpdateFields: %v", err)
	}

	want := map[string]bool{"key": true, "ftp_module": true}
	if len(body) != len(want) {
		t.Fatalf("PUT body has %d key(s) %v, want exactly %v", len(body), keysOf(body), want)
	}
	for name := range want {
		if _, ok := body[name]; !ok {
			t.Errorf("PUT body is missing %q; got %v", name, keysOf(body))
		}
	}
	// The 23 fields settings.Usg force-emits without omitempty -- a census
	// re-run by hand from usg.generated.go's own json tags. None of them was
	// named by the mask, so none should appear.
	for _, forceEmitted := range []string{
		"broadcast_ping", "dhcpd_hostfile_update", "dhcpd_use_dnsmasq",
		"dhcp_relay_agents_packets", "dnsmasq_all_servers", "gre_module",
		"h323_module", "lldp_enable_all", "mdns_enabled", "offload_accounting",
		"offload_l2_blocking", "offload_sch", "pptp_module", "receive_redirects",
		"send_redirects", "sip_module", "syn_cookies", "tftp_module",
		"upnp_enabled", "upnp_nat_pmp_enabled", "upnp_secure_mode", "unbind_wan_monitors",
	} {
		if _, ok := body[forceEmitted]; ok {
			t.Errorf("PUT body carries %q, a force-emitted field the mask never named; "+
				"the masked write is supposed to leave it out", forceEmitted)
		}
	}
}

// TestUsgNestedSchemaHasExactlyItsAttributes guards usgNestedSchema's type
// assertion against a generator regression: "usg" moving off
// SingleNestedAttribute would panic every conformance test above instead of
// naming the actual problem, so this pins the shape ahead of that.
func TestUsgNestedSchemaHasExactlyItsAttributes(t *testing.T) {
	ctx := context.Background()
	built := resource_setting.SettingResourceSchema(ctx)
	if _, ok := built.Attributes["usg"]; !ok {
		t.Fatal(`the generated setting schema has no "usg" attribute`)
	}
	nested := usgNestedSchema(ctx)
	if len(nested.Attributes) != 37 {
		t.Errorf("usg has %d attribute(s), want 37; update usgKitSpec/usgGeoKitSpec and "+
			"this count together", len(nested.Attributes))
	}
}

// TestUsgGeoNestedSchemaHasExactlyItsAttributes is
// TestUsgNestedSchemaHasExactlyItsAttributes' counterpart for
// usgGeoNestedSchema: exactly the four geo_ip_filtering_* attributes,
// filtered out of usg's own thirty-seven.
func TestUsgGeoNestedSchemaHasExactlyItsAttributes(t *testing.T) {
	ctx := context.Background()
	nested := usgGeoNestedSchema(ctx)
	if len(nested.Attributes) != 4 {
		t.Errorf("usg_geo has %d attribute(s), want 4; update usgGeoKitSpec and this count together",
			len(nested.Attributes))
	}
	for _, name := range []string{
		"geo_ip_filtering_block",
		"geo_ip_filtering_countries",
		"geo_ip_filtering_enabled",
		"geo_ip_filtering_traffic_direction",
	} {
		if nested.Attributes[name] == nil {
			t.Errorf("usg_geo's nested schema is missing %q", name)
		}
	}
}
