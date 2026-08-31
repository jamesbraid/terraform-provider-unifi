package resourcekit

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
)

// ssModel stands in for settingResourceModel: one section attribute (plus a
// second field a real whole-resource model would have but a section Spec
// never touches).
type ssModel struct {
	Other   types.String `tfsdk:"other"`
	Section types.Object `tfsdk:"section"`
}

// ssSDK stands in for a settings.T struct: two independent wire fields, so a
// mask naming only one is distinguishable from a mask naming both.
type ssSDK struct {
	Name  string
	Extra string
}

type ssSectionModel struct {
	Name  types.String `tfsdk:"name"`
	Extra types.String `tfsdk:"extra"`
}

func ssAttrTypes() map[string]attr.Type {
	return map[string]attr.Type{"name": types.StringType, "extra": types.StringType}
}

func ssSpec(backend Backend[ssSDK]) Spec[ssSectionModel, ssSDK] {
	return Spec[ssSectionModel, ssSDK]{
		TypeName: "ss_probe",
		Subject:  "SS Probe",
		New:      func() *ssSDK { return &ssSDK{} },
		Fields: []Field[ssSectionModel, ssSDK]{
			StringField[ssSectionModel, ssSDK]{
				Wire:  "name",
				Model: func(m *ssSectionModel) *types.String { return &m.Name },
				SDK:   func(s *ssSDK) *string { return &s.Name },
				Elide: KeepZero,
			},
			StringField[ssSectionModel, ssSDK]{
				Wire:  "extra",
				Model: func(m *ssSectionModel) *types.String { return &m.Extra },
				SDK:   func(s *ssSDK) *string { return &s.Extra },
				Elide: KeepZero,
			},
		},
		Backend: backend,
	}
}

// ssSectionObject builds a section object with Name set (or left null) and
// Extra always null -- every test case here configures at most Name, using
// Extra purely to observe how a plan-conditioned field is treated.
func ssSectionObject(t *testing.T, name *string) types.Object {
	t.Helper()
	ctx := context.Background()
	nameValue := types.StringNull()
	if name != nil {
		nameValue = types.StringValue(*name)
	}
	object, diags := types.ObjectValueFrom(ctx, ssAttrTypes(), ssSectionModel{
		Name: nameValue, Extra: types.StringNull(),
	})
	if diags.HasError() {
		t.Fatalf("build section object: %v", diags)
	}
	return object
}

// ssExtraSDK stands in for a second controller document -- usg_geo,
// ips_suppression -- mapping onto the SAME section model as ssSDK, on its
// own wire member.
type ssExtraSDK struct {
	Value string
}

func ssExtraSpec(backend Backend[ssExtraSDK]) Spec[ssSectionModel, ssExtraSDK] {
	return Spec[ssSectionModel, ssExtraSDK]{
		TypeName: "ss_probe_extra",
		Subject:  "SS Probe Extra",
		New:      func() *ssExtraSDK { return &ssExtraSDK{} },
		Fields: []Field[ssSectionModel, ssExtraSDK]{
			StringField[ssSectionModel, ssExtraSDK]{
				Wire:  "value",
				Model: func(m *ssSectionModel) *types.String { return &m.Extra },
				SDK:   func(s *ssExtraSDK) *string { return &s.Value },
				Elide: KeepZero,
			},
		},
		Backend: backend,
	}
}

// fakeDocument is a Document[ssSectionModel] that only logs that it ran --
// for TestSpecSectionWritesExtraDocumentsAfterThePrimaryInOrder and
// TestSpecSectionExtraErrorAbortsBeforeLaterExtras, which care about call
// order rather than any actual masking or wire behaviour (ssExtraSDK covers
// that instead).
type fakeDocument struct {
	name     string
	calls    *[]string
	writeErr error
}

func (f fakeDocument) Write(context.Context, string, *ssSectionModel, *ssSectionModel, string) diag.Diagnostics {
	*f.calls = append(*f.calls, "write:"+f.name)
	var diags diag.Diagnostics
	if f.writeErr != nil {
		diags.AddError("Error Writing "+f.name, f.writeErr.Error())
	}
	return diags
}

func (f fakeDocument) Read(context.Context, string, *ssSectionModel) diag.Diagnostics {
	*f.calls = append(*f.calls, "read:"+f.name)
	return nil
}

func TestSpecSectionWriteMasksOnlyThePlanSetFields(t *testing.T) {
	var gotFields []string
	section := SpecSection[ssModel, ssSectionModel, ssSDK]{
		SectionName: "section",
		Get:         func(m *ssModel) *types.Object { return &m.Section },
		Set:         func(m *ssModel, o types.Object) { m.Section = o },
		AttrTypes:   ssAttrTypes(),
		Spec: ssSpec(Backend[ssSDK]{
			UpdateFields: func(_ context.Context, _ string, in *ssSDK, fields ...string) (*ssSDK, error) {
				gotFields = append([]string(nil), fields...)
				return in, nil
			},
		}),
	}

	name := "foo"
	plan := ssModel{Section: ssSectionObject(t, &name)}
	diags := section.Write(context.Background(), "site-1", &plan, nil, "Creating")
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	if len(gotFields) != 1 || gotFields[0] != "name" {
		t.Errorf("mask = %v, want [name] (extra was never set in the plan)", gotFields)
	}
}

func TestSpecSectionWriteSkipsAnEmptyMaskInsteadOfErroring(t *testing.T) {
	called := false
	section := SpecSection[ssModel, ssSectionModel, ssSDK]{
		SectionName: "section",
		Get:         func(m *ssModel) *types.Object { return &m.Section },
		Set:         func(m *ssModel, o types.Object) { m.Section = o },
		AttrTypes:   ssAttrTypes(),
		Spec: ssSpec(Backend[ssSDK]{
			UpdateFields: func(_ context.Context, _ string, in *ssSDK, fields ...string) (*ssSDK, error) {
				called = true
				return in, nil
			},
		}),
	}

	// Configured (the object itself is non-null) but every member is null:
	// nothing for the plan to have set.
	plan := ssModel{Section: ssSectionObject(t, nil)}
	diags := section.Write(context.Background(), "site-1", &plan, nil, "Creating")
	if diags.HasError() {
		t.Fatalf("a configured-but-empty section object must not error: %v", diags)
	}
	if called {
		t.Error("Backend.UpdateFields was called with an empty mask; it should have been skipped")
	}
}

// TestSpecSectionWriteDiagnosticNeverCarriesTheRealSDKsPayload is the
// load-bearing regression test for the guest_access-secrets-review finding:
// go-unifi's client appends the raw request body to every non-2xx error
// ("... \npayload: {...}"), redacted by a fixed substring list keyed on
// field NAME that the SDK guesses at without ever consulting this
// provider's own schema. Eleven of guest_access's 18 Sensitive wire names
// (and snmp's community) are outside that list, so a failed write printed
// them verbatim into a practitioner-visible diagnostic -- the console, CI
// logs, TF_LOG files, Terraform Cloud run logs.
//
// This drives the real pinned go-unifi client (ui.New, the same
// constructor every kit backend uses) against an httptest server that
// returns a genuine 400, so the error text this test inspects is whatever
// unifi.go's doRequest actually formats today, not a string this test
// wrote. A future go-unifi bump that changes the marker text, the field
// order, or drops the payload tail entirely changes what this test
// exercises along with it, instead of leaving a hand-built stand-in green
// while the real thing silently stops matching.
func TestSpecSectionWriteDiagnosticNeverCarriesTheRealSDKsPayload(t *testing.T) {
	const marker = "LEAKMARK-should-never-reach-a-diagnostic"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/proxy/network/status" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"meta":{"server_version":"10.4.57"}}`))
			return
		}
		// Every other request -- including the real masked PUT
		// UpdateSettingFields sends -- is rejected, so the client's real
		// error-formatting path runs and appends its own payload tail.
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"meta":{"rc":"error","msg":"api.err.InvalidPayload"}}`))
	}))
	t.Cleanup(server.Close)

	client, err := ui.New(context.Background(), &ui.Config{BaseURL: server.URL, APIKey: "test-key"})
	if err != nil {
		t.Fatalf("create the API client: %v", err)
	}

	// x_stripe_api_key is a real settings.GuestAccess field, and one of the
	// five genuine credentials the review measured leaking through this
	// exact path -- wire name unmatched by the SDK's own redaction
	// substrings, same as it was in production.
	attrTypes := map[string]attr.Type{"stripe_api_key": types.StringType}
	spec := Spec[sdkPayloadProbeModel, settings.GuestAccess]{
		TypeName: "sdk_payload_probe",
		Subject:  "SDK Payload Probe",
		New:      func() *settings.GuestAccess { return &settings.GuestAccess{} },
		Fields: []Field[sdkPayloadProbeModel, settings.GuestAccess]{
			StringField[sdkPayloadProbeModel, settings.GuestAccess]{
				Wire:  "x_stripe_api_key",
				Model: func(m *sdkPayloadProbeModel) *types.String { return &m.StripeApiKey },
				SDK:   func(s *settings.GuestAccess) *string { return &s.StripeApiKey },
				Elide: KeepZero,
			},
		},
		Backend: Backend[settings.GuestAccess]{
			UpdateFields: func(ctx context.Context, site string, in *settings.GuestAccess, fields ...string) (*settings.GuestAccess, error) {
				if err := client.UpdateSettingFields(ctx, site, in, fields...); err != nil {
					return nil, err
				}
				return in, nil
			},
		},
	}

	section := SpecSection[ssModel, sdkPayloadProbeModel, settings.GuestAccess]{
		SectionName: "section",
		Get:         func(m *ssModel) *types.Object { return &m.Section },
		Set:         func(m *ssModel, o types.Object) { m.Section = o },
		AttrTypes:   attrTypes,
		Spec:        spec,
	}

	object, diags := types.ObjectValueFrom(context.Background(), attrTypes, sdkPayloadProbeModel{
		StripeApiKey: types.StringValue(marker),
	})
	if diags.HasError() {
		t.Fatalf("build section object: %v", diags)
	}
	plan := ssModel{Section: object}

	writeDiags := section.Write(context.Background(), "default", &plan, nil, "Creating")
	if !writeDiags.HasError() {
		t.Fatal("expected the controller's 400 to produce a diagnostic")
	}
	for _, d := range writeDiags.Errors() {
		if strings.Contains(d.Detail(), marker) {
			t.Errorf("diagnostic detail carries the real SDK's request payload verbatim: %q", d.Detail())
		}
		if strings.Contains(d.Detail(), "payload:") {
			t.Errorf("diagnostic detail still carries a payload tail: %q", d.Detail())
		}
	}
}

// sdkPayloadProbeModel is the minimal model
// TestSpecSectionWriteDiagnosticNeverCarriesTheRealSDKsPayload needs: one
// string attribute standing in for a Sensitive field on a real SDK struct.
type sdkPayloadProbeModel struct {
	StripeApiKey types.String `tfsdk:"stripe_api_key"`
}

// TestSpecSectionWriteDiagnosticDropsAHandBuiltSDKShapedPayload documents
// the shape unifi.go's doRequest formats a non-2xx error in
// ("... \npayload: {...}"), against a string this test constructs rather
// than the SDK's own. Kept alongside
// TestSpecSectionWriteDiagnosticNeverCarriesTheRealSDKsPayload (the
// load-bearing case, which drives the real client) because a hand-built
// string is cheap to read and exercises the truncation directly, but it
// cannot by itself prove the fix still matches a future SDK format change
// -- only the real-client test can.
func TestSpecSectionWriteDiagnosticDropsAHandBuiltSDKShapedPayload(t *testing.T) {
	sdkErr := errors.New(
		"api.err.InvalidPayload (400 Bad Request) for PUT https://example.invalid/api/s/default/set/setting/guest_access\n" +
			`payload: {"key":"guest_access","name":"LEAKMARK-should-never-reach-a-diagnostic"}`,
	)
	section := SpecSection[ssModel, ssSectionModel, ssSDK]{
		SectionName: "section",
		Get:         func(m *ssModel) *types.Object { return &m.Section },
		Set:         func(m *ssModel, o types.Object) { m.Section = o },
		AttrTypes:   ssAttrTypes(),
		Spec: ssSpec(Backend[ssSDK]{
			UpdateFields: func(_ context.Context, _ string, _ *ssSDK, _ ...string) (*ssSDK, error) {
				return nil, sdkErr
			},
		}),
	}

	name := "foo"
	plan := ssModel{Section: ssSectionObject(t, &name)}
	diags := section.Write(context.Background(), "site-1", &plan, nil, "Creating")
	if !diags.HasError() {
		t.Fatal("expected the backend's error to produce a diagnostic")
	}
	for _, d := range diags.Errors() {
		if strings.Contains(d.Detail(), "LEAKMARK") {
			t.Errorf("diagnostic detail carries the SDK's request payload verbatim: %q", d.Detail())
		}
		if strings.Contains(d.Detail(), "payload:") {
			t.Errorf("diagnostic detail still carries a payload tail: %q", d.Detail())
		}
	}
}

func TestSpecSectionWriteAfterReceiveSeesPriorAndOutranksTheResponse(t *testing.T) {
	var sawPriorName, sawPriorExtra string
	var sawPriorExtraNull bool
	section := SpecSection[ssModel, ssSectionModel, ssSDK]{
		SectionName: "section",
		Get:         func(m *ssModel) *types.Object { return &m.Section },
		Set:         func(m *ssModel, o types.Object) { m.Section = o },
		AttrTypes:   ssAttrTypes(),
		Spec: ssSpec(Backend[ssSDK]{
			UpdateFields: func(_ context.Context, _ string, in *ssSDK, _ ...string) (*ssSDK, error) {
				// The server normalizes the written value -- ApplyPlanToState
				// must still prefer the plan's own spelling for "name".
				return &ssSDK{Name: "server-normalized", Extra: "server-extra"}, nil
			},
		}),
		AfterReceive: func(_ context.Context, _ *ssSDK, model *ssSectionModel, prior ssSectionModel) diag.Diagnostics {
			sawPriorName = prior.Name.ValueString()
			sawPriorExtraNull = prior.Extra.IsNull()
			sawPriorExtra = prior.Extra.ValueString()
			// mgmt-style plan-conditioned null: extra wasn't in the plan, so
			// null it regardless of what the controller reported.
			if prior.Extra.IsNull() {
				model.Extra = types.StringNull()
			}
			return nil
		},
	}

	name := "plan-value"
	plan := ssModel{Section: ssSectionObject(t, &name)}
	diags := section.Write(context.Background(), "site-1", &plan, nil, "Creating")
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	if sawPriorName != "plan-value" {
		t.Errorf("AfterReceive saw prior.Name = %q, want %q", sawPriorName, "plan-value")
	}
	if !sawPriorExtraNull {
		t.Errorf("AfterReceive saw prior.Extra = %q, want null (unset in the plan)", sawPriorExtra)
	}

	var got ssSectionModel
	diags = plan.Section.As(context.Background(), &got, basetypes.ObjectAsOptions{})
	if diags.HasError() {
		t.Fatalf("decode written-back section: %v", diags)
	}
	if got.Name.ValueString() != "plan-value" {
		t.Errorf("name = %q, want %q (the plan's own value outranks the server's response)",
			got.Name.ValueString(), "plan-value")
	}
	if !got.Extra.IsNull() {
		t.Errorf("extra = %v, want null (AfterReceive's plan-conditioned null)", got.Extra)
	}
}

func TestSpecSectionReadUnconfiguredIsNullAndNeverFetches(t *testing.T) {
	called := false
	section := SpecSection[ssModel, ssSectionModel, ssSDK]{
		SectionName: "section",
		Get:         func(m *ssModel) *types.Object { return &m.Section },
		Set:         func(m *ssModel, o types.Object) { m.Section = o },
		AttrTypes:   ssAttrTypes(),
		Spec: ssSpec(Backend[ssSDK]{
			Read: func(context.Context, string, string) (*ssSDK, error) {
				called = true
				return &ssSDK{}, nil
			},
		}),
	}

	plan := ssModel{Section: types.ObjectNull(ssAttrTypes())}
	var out ssModel
	diags := section.Read(context.Background(), "site-1", &plan, &out)
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	if called {
		t.Error("Backend.Read was called for an unconfigured section")
	}
	if !out.Section.IsNull() {
		t.Errorf("section = %v, want null", out.Section)
	}
}

func TestSpecSectionReadFetchesAndAfterReceiveSeesThePlanAsPrior(t *testing.T) {
	var sawPriorName string
	section := SpecSection[ssModel, ssSectionModel, ssSDK]{
		SectionName: "section",
		Get:         func(m *ssModel) *types.Object { return &m.Section },
		Set:         func(m *ssModel, o types.Object) { m.Section = o },
		AttrTypes:   ssAttrTypes(),
		Spec: ssSpec(Backend[ssSDK]{
			Read: func(context.Context, string, string) (*ssSDK, error) {
				return &ssSDK{Name: "server-name", Extra: "server-extra"}, nil
			},
		}),
		AfterReceive: func(_ context.Context, _ *ssSDK, model *ssSectionModel, prior ssSectionModel) diag.Diagnostics {
			sawPriorName = prior.Name.ValueString()
			if prior.Extra.IsNull() {
				model.Extra = types.StringNull()
			}
			return nil
		},
	}

	name := "configured-name"
	plan := ssModel{Section: ssSectionObject(t, &name)}
	var out ssModel
	diags := section.Read(context.Background(), "site-1", &plan, &out)
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	if sawPriorName != "configured-name" {
		t.Errorf("AfterReceive saw prior.Name = %q, want the plan's own value %q",
			sawPriorName, "configured-name")
	}

	var got ssSectionModel
	diags = out.Section.As(context.Background(), &got, basetypes.ObjectAsOptions{})
	if diags.HasError() {
		t.Fatalf("decode read section: %v", diags)
	}
	if got.Name.ValueString() != "server-name" {
		t.Errorf("name = %q, want the fetched value %q", got.Name.ValueString(), "server-name")
	}
	if !got.Extra.IsNull() {
		t.Errorf("extra = %v, want null (AfterReceive's plan-conditioned null)", got.Extra)
	}
}

func TestSpecSectionWritesExtraDocumentsAfterThePrimaryInOrder(t *testing.T) {
	var calls []string
	section := SpecSection[ssModel, ssSectionModel, ssSDK]{
		SectionName: "section",
		Get:         func(m *ssModel) *types.Object { return &m.Section },
		Set:         func(m *ssModel, o types.Object) { m.Section = o },
		AttrTypes:   ssAttrTypes(),
		Spec: ssSpec(Backend[ssSDK]{
			UpdateFields: func(_ context.Context, _ string, in *ssSDK, _ ...string) (*ssSDK, error) {
				calls = append(calls, "primary")
				return in, nil
			},
		}),
		Extra: []Document[ssSectionModel]{
			fakeDocument{name: "first", calls: &calls},
			fakeDocument{name: "second", calls: &calls},
		},
	}

	name := "foo"
	plan := ssModel{Section: ssSectionObject(t, &name)}
	diags := section.Write(context.Background(), "site-1", &plan, nil, "Creating")
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	want := []string{"primary", "write:first", "write:second"}
	if !reflect.DeepEqual(calls, want) {
		t.Errorf("call order = %v, want %v", calls, want)
	}
}

// TestSpecSectionSkipsAnExtraWhoseMaskIsEmpty exercises a real SpecDocument
// Extra (not the logging fakeDocument), since the behaviour under test is
// the actual mask computation: a plan that never named the Extra's field
// must not send it, yet the Extra must still be read.
func TestSpecSectionSkipsAnExtraWhoseMaskIsEmpty(t *testing.T) {
	var writeCalled, readCalled bool
	extra := SpecDocument[ssSectionModel, ssExtraSDK]{
		Spec: ssExtraSpec(Backend[ssExtraSDK]{
			UpdateFields: func(_ context.Context, _ string, in *ssExtraSDK, _ ...string) (*ssExtraSDK, error) {
				writeCalled = true
				return in, nil
			},
			Read: func(context.Context, string, string) (*ssExtraSDK, error) {
				readCalled = true
				return &ssExtraSDK{Value: "hydrated"}, nil
			},
		}),
	}
	section := SpecSection[ssModel, ssSectionModel, ssSDK]{
		SectionName: "section",
		Get:         func(m *ssModel) *types.Object { return &m.Section },
		Set:         func(m *ssModel, o types.Object) { m.Section = o },
		AttrTypes:   ssAttrTypes(),
		Spec: ssSpec(Backend[ssSDK]{
			UpdateFields: func(_ context.Context, _ string, in *ssSDK, _ ...string) (*ssSDK, error) {
				return in, nil
			},
			Read: func(context.Context, string, string) (*ssSDK, error) {
				return &ssSDK{Name: "server-name"}, nil
			},
		}),
		Extra: []Document[ssSectionModel]{extra},
	}

	// The plan sets name (the primary's own field) but leaves extra null:
	// the Extra's own mask is empty even though the section itself is
	// configured.
	name := "foo"
	plan := ssModel{Section: ssSectionObject(t, &name)}
	diags := section.Write(context.Background(), "site-1", &plan, nil, "Creating")
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	if writeCalled {
		t.Error("the Extra's UpdateFields was called with an empty mask; it should have been skipped")
	}

	var out ssModel
	diags = section.Read(context.Background(), "site-1", &plan, &out)
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	if !readCalled {
		t.Error("the Extra's Read was not called; an Extra must be read even when its write was skipped")
	}
	var got ssSectionModel
	diags = out.Section.As(context.Background(), &got, basetypes.ObjectAsOptions{})
	if diags.HasError() {
		t.Fatalf("decode read section: %v", diags)
	}
	if got.Extra.ValueString() != "hydrated" {
		t.Errorf("extra = %q, want the fetched value %q (hydrated even though never configured)",
			got.Extra.ValueString(), "hydrated")
	}
}

func TestSpecSectionExtraErrorAbortsBeforeLaterExtras(t *testing.T) {
	var calls []string
	failing := fakeDocument{name: "failing", calls: &calls, writeErr: errors.New("boom")}
	never := fakeDocument{name: "never", calls: &calls}
	section := SpecSection[ssModel, ssSectionModel, ssSDK]{
		SectionName: "section",
		Get:         func(m *ssModel) *types.Object { return &m.Section },
		Set:         func(m *ssModel, o types.Object) { m.Section = o },
		AttrTypes:   ssAttrTypes(),
		Spec: ssSpec(Backend[ssSDK]{
			UpdateFields: func(_ context.Context, _ string, in *ssSDK, _ ...string) (*ssSDK, error) {
				calls = append(calls, "primary")
				return in, nil
			},
		}),
		Extra: []Document[ssSectionModel]{failing, never},
	}

	name := "foo"
	plan := ssModel{Section: ssSectionObject(t, &name)}
	diags := section.Write(context.Background(), "site-1", &plan, nil, "Creating")
	if !diags.HasError() {
		t.Fatal("expected an error from the failing Extra")
	}
	want := []string{"primary", "write:failing"}
	if !reflect.DeepEqual(calls, want) {
		t.Errorf("calls = %v, want %v (the second Extra must never run)", calls, want)
	}
}

// TestSpecDocumentOnReadNotFoundIsReportedAsItsDiagnostics pins
// OnReadNotFound's two branches: set gets its own diagnostics, nil leaves
// the model untouched with no diagnostic at all -- a read-time not-found is
// ordinarily benign (the document may simply not exist yet).
func TestSpecDocumentOnReadNotFoundIsReportedAsItsDiagnostics(t *testing.T) {
	notFound := &ui.NotFoundError{Type: "SSProbeExtra", Attr: "id", Value: "site-1"}
	tooOld := func(err error) diag.Diagnostics {
		var diags diag.Diagnostics
		diags.AddError("SS Probe Extra is too old for this controller", err.Error())
		return diags
	}

	t.Run("set", func(t *testing.T) {
		document := SpecDocument[ssSectionModel, ssExtraSDK]{
			Spec: ssExtraSpec(Backend[ssExtraSDK]{
				Read: func(context.Context, string, string) (*ssExtraSDK, error) {
					return nil, notFound
				},
			}),
			OnReadNotFound: tooOld,
		}
		var model ssSectionModel
		diags := document.Read(context.Background(), "site-1", &model)
		if !diags.HasError() {
			t.Fatal("expected OnReadNotFound's diagnostics")
		}
		got := diags.Errors()
		if len(got) != 1 || got[0].Summary() != "SS Probe Extra is too old for this controller" {
			t.Errorf("diagnostics = %v, want OnReadNotFound's own diagnostic", diags)
		}
		if !strings.Contains(got[0].Detail(), notFound.Error()) {
			t.Errorf("detail = %q, want it to carry the backend's own not-found error %q",
				got[0].Detail(), notFound.Error())
		}
	})

	t.Run("nil", func(t *testing.T) {
		document := SpecDocument[ssSectionModel, ssExtraSDK]{
			Spec: ssExtraSpec(Backend[ssExtraSDK]{
				Read: func(context.Context, string, string) (*ssExtraSDK, error) {
					return nil, notFound
				},
			}),
		}
		model := ssSectionModel{Name: types.StringValue("untouched"), Extra: types.StringValue("also-untouched")}
		diags := document.Read(context.Background(), "site-1", &model)
		if diags.HasError() {
			t.Fatalf("unexpected diagnostics: %v", diags)
		}
		if model.Name.ValueString() != "untouched" || model.Extra.ValueString() != "also-untouched" {
			t.Errorf("model = %+v, want untouched (nil OnReadNotFound leaves the model alone)", model)
		}
	})
}

// TestSpecDocumentOnWriteNotFoundIsReportedAsItsDiagnostics pins
// OnWriteNotFound's two branches: set gets its own diagnostics; nil falls
// back to today's plain "Error <verb> <Subject>" diagnostic -- a
// write-time not-found is still a failure to write what the plan asked
// for, unlike a read-time one, unless a document opts into something else.
func TestSpecDocumentOnWriteNotFoundIsReportedAsItsDiagnostics(t *testing.T) {
	notFound := &ui.NotFoundError{Type: "SSProbeExtra", Attr: "id", Value: "site-1"}
	tooOld := func(err error) diag.Diagnostics {
		var diags diag.Diagnostics
		diags.AddError("SS Probe Extra is too old for this controller", err.Error())
		return diags
	}

	// usg_geo's own write reports a masked UpdateFields' not-found as "Geo
	// IP Filtering Not Supported By This Controller" (writeUsgGeo).
	t.Run("set", func(t *testing.T) {
		document := SpecDocument[ssSectionModel, ssExtraSDK]{
			Spec: ssExtraSpec(Backend[ssExtraSDK]{
				UpdateFields: func(context.Context, string, *ssExtraSDK, ...string) (*ssExtraSDK, error) {
					return nil, notFound
				},
			}),
			OnWriteNotFound: tooOld,
		}
		plan := ssSectionModel{Extra: types.StringValue("configured")}
		prior := plan
		diags := document.Write(context.Background(), "site-1", &plan, &prior, "Creating")
		if !diags.HasError() {
			t.Fatal("expected OnWriteNotFound's diagnostics")
		}
		got := diags.Errors()
		if len(got) != 1 || got[0].Summary() != "SS Probe Extra is too old for this controller" {
			t.Errorf("diagnostics = %v, want OnWriteNotFound's own diagnostic", diags)
		}
	})

	t.Run("nil", func(t *testing.T) {
		document := SpecDocument[ssSectionModel, ssExtraSDK]{
			Spec: ssExtraSpec(Backend[ssExtraSDK]{
				UpdateFields: func(context.Context, string, *ssExtraSDK, ...string) (*ssExtraSDK, error) {
					return nil, notFound
				},
			}),
		}
		plan := ssSectionModel{Extra: types.StringValue("configured")}
		prior := ssSectionModel{}
		diags := document.Write(context.Background(), "site-1", &plan, &prior, "Updating")
		if !diags.HasError() {
			t.Fatal("expected today's default diagnostic, not silence")
		}
		got := diags.Errors()
		wantSummary := "Error Updating SS Probe Extra"
		if len(got) != 1 || got[0].Summary() != wantSummary {
			t.Errorf("diagnostics = %v, want a single %q summary (today's default)", diags, wantSummary)
		}
		if !strings.Contains(got[0].Detail(), notFound.Error()) {
			t.Errorf("detail = %q, want it to carry the backend's own not-found error %q",
				got[0].Detail(), notFound.Error())
		}
	})
}

// TestSpecSectionReadNotFoundStaysTodaysErrorSummary pins the primary's own
// not-found behaviour through the read-path change: routing the primary's
// Read through the same specDocumentRead machinery an Extra uses must not
// turn its not-found silent -- the primary has no OnNotFound of its own to
// opt into that, and today's diagnostic text must survive unchanged.
func TestSpecSectionReadNotFoundStaysTodaysErrorSummary(t *testing.T) {
	notFound := &ui.NotFoundError{Type: "SSProbe", Attr: "id", Value: "site-1"}
	section := SpecSection[ssModel, ssSectionModel, ssSDK]{
		SectionName: "section",
		Get:         func(m *ssModel) *types.Object { return &m.Section },
		Set:         func(m *ssModel, o types.Object) { m.Section = o },
		AttrTypes:   ssAttrTypes(),
		Spec: ssSpec(Backend[ssSDK]{
			Read: func(context.Context, string, string) (*ssSDK, error) {
				return nil, notFound
			},
		}),
	}

	name := "foo"
	plan := ssModel{Section: ssSectionObject(t, &name)}
	var out ssModel
	diags := section.Read(context.Background(), "site-1", &plan, &out)
	if !diags.HasError() {
		t.Fatal("expected a diagnostic; the primary has no OnNotFound to opt out of one")
	}
	got := diags.Errors()
	wantSummary := "Error Reading SS Probe"
	if len(got) != 1 || got[0].Summary() != wantSummary {
		t.Errorf("diagnostics = %v, want a single %q summary (today's text, unchanged)", diags, wantSummary)
	}
	if !strings.Contains(got[0].Detail(), notFound.Error()) {
		t.Errorf("detail = %q, want it to carry the backend's own error %q", got[0].Detail(), notFound.Error())
	}
}

// ssObjNestedSDK stands in for a nested SDK object shaped like usg's own
// dns_verification (settings.SettingUsgDNSVerification): a struct the
// primary's ObjectField owns, not the extra.
type ssObjNestedSDK struct {
	A string
	B string
}

// ssObjNestedModel is ssObjNestedSDK's model-side shape -- ObjectField's own
// Encode/Decode need a real tfsdk-tagged struct to decode into, the same way
// usg's dnsVerificationModel does.
type ssObjNestedModel struct {
	A types.String `tfsdk:"a"`
	B types.String `tfsdk:"b"`
}

var ssObjNestedAttrTypes = map[string]attr.Type{
	"a": types.StringType,
	"b": types.StringType,
}

// ssObjSectionModel is ssSectionModel's sibling for the ObjectField case:
// Nested is the primary's own nested object (usg's dns_verification),
// Extra is a second, unrelated attribute an Extra document owns (usg_geo's
// own attributes) -- sharing one model between the two the same way usg and
// usg_geo share settingUSGModel.
type ssObjSectionModel struct {
	Name   types.String `tfsdk:"name"`
	Nested types.Object `tfsdk:"nested"`
	Extra  types.String `tfsdk:"extra"`
}

func ssObjAttrTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"name":   types.StringType,
		"nested": types.ObjectType{AttrTypes: ssObjNestedAttrTypes},
		"extra":  types.StringType,
	}
}

// ssObjSDK stands in for a settings.Usg-shaped struct: Name is an ordinary
// field, Nested is the ObjectField's own target.
type ssObjSDK struct {
	Name   string
	Nested *ssObjNestedSDK
}

// ssObjSpec is the primary Spec: Name is an ordinary StringField, Nested is
// an ObjectField over ssObjNestedSDK -- the shape whose own CopyPlanToState
// does a member-by-member merge rather than a wholesale copy, specifically
// to keep an Unknown inner member (a Computed sub-attribute the plan didn't
// resolve) out of state. TestSpecSectionExtraWriteLeavesThePrimarysObjectMergeIntact
// is what an Extra sharing this model must not undo.
func ssObjSpec(backend Backend[ssObjSDK]) Spec[ssObjSectionModel, ssObjSDK] {
	return Spec[ssObjSectionModel, ssObjSDK]{
		TypeName: "ss_obj_probe",
		Subject:  "SS Obj Probe",
		New:      func() *ssObjSDK { return &ssObjSDK{} },
		Fields: []Field[ssObjSectionModel, ssObjSDK]{
			StringField[ssObjSectionModel, ssObjSDK]{
				Wire:  "name",
				Model: func(m *ssObjSectionModel) *types.String { return &m.Name },
				SDK:   func(s *ssObjSDK) *string { return &s.Name },
				Elide: KeepZero,
			},
			ObjectField[ssObjSectionModel, ssObjSDK, ssObjNestedSDK]{
				Wire:      "nested",
				Model:     func(m *ssObjSectionModel) *types.Object { return &m.Nested },
				SDK:       func(s *ssObjSDK) **ssObjNestedSDK { return &s.Nested },
				AttrTypes: ssObjNestedAttrTypes,
				Encode: func(ctx context.Context, object types.Object) (*ssObjNestedSDK, diag.Diagnostics) {
					var model ssObjNestedModel
					diags := object.As(ctx, &model, basetypes.ObjectAsOptions{})
					return &ssObjNestedSDK{A: model.A.ValueString(), B: model.B.ValueString()}, diags
				},
				Decode: func(ctx context.Context, sdk *ssObjNestedSDK) (types.Object, diag.Diagnostics) {
					return types.ObjectValueFrom(ctx, ssObjNestedAttrTypes, ssObjNestedModel{
						A: types.StringValue(sdk.A), B: types.StringValue(sdk.B),
					})
				},
				Elide: KeepZero,
			},
		},
		Backend: backend,
	}
}

// ssObjExtraSDK stands in for usg_geo's own document: a second controller
// document mapping onto the SAME section model as ssObjSDK, on a field the
// primary's own Fields never touch (Extra).
type ssObjExtraSDK struct {
	Value string
}

func ssObjExtraSpec(backend Backend[ssObjExtraSDK]) Spec[ssObjSectionModel, ssObjExtraSDK] {
	return Spec[ssObjSectionModel, ssObjExtraSDK]{
		TypeName: "ss_obj_probe_extra",
		Subject:  "SS Obj Probe Extra",
		New:      func() *ssObjExtraSDK { return &ssObjExtraSDK{} },
		Fields: []Field[ssObjSectionModel, ssObjExtraSDK]{
			StringField[ssObjSectionModel, ssObjExtraSDK]{
				Wire:  "value",
				Model: func(m *ssObjSectionModel) *types.String { return &m.Extra },
				SDK:   func(s *ssObjExtraSDK) *string { return &s.Value },
				Elide: KeepZero,
			},
		},
		Backend: backend,
	}
}

// TestSpecSectionExtraWriteLeavesThePrimarysObjectMergeIntact is the
// regression test for a bug an earlier version of this package's
// SpecDocument.Write had: it called the full Spec.ApplyPlanToState, whose
// copyUncoveredPlanValues catch-up treats every model field a Spec's own
// Fields don't cover as "nobody's job, copy the plan's raw value forward."
// That premise only holds for a section's sole Spec -- for an Extra sharing
// a model with a primary that has its own ObjectField, it's false: the
// Extra's own catch-up saw the primary's nested object as uncovered and
// overwrote ObjectField's careful member-by-member merge with a wholesale
// copy of the plan's raw object, Unknown inner member included. usg hit
// this the moment usg_geo became its first Extra (dns_verification is the
// primary's ObjectField there). SpecDocument.Write now calls
// applyOwnFieldsToState instead -- this section's model.
func TestSpecSectionExtraWriteLeavesThePrimarysObjectMergeIntact(t *testing.T) {
	section := SpecSection[ssModel, ssObjSectionModel, ssObjSDK]{
		SectionName: "section",
		Get:         func(m *ssModel) *types.Object { return &m.Section },
		Set:         func(m *ssModel, o types.Object) { m.Section = o },
		AttrTypes:   ssObjAttrTypes(),
		Spec: ssObjSpec(Backend[ssObjSDK]{
			UpdateFields: func(_ context.Context, _ string, in *ssObjSDK, _ ...string) (*ssObjSDK, error) {
				return in, nil
			},
		}),
		Extra: []Document[ssObjSectionModel]{
			SpecDocument[ssObjSectionModel, ssObjExtraSDK]{
				Spec: ssObjExtraSpec(Backend[ssObjExtraSDK]{
					UpdateFields: func(_ context.Context, _ string, in *ssObjExtraSDK, _ ...string) (*ssObjExtraSDK, error) {
						return in, nil
					},
				}),
			},
		},
	}

	// nested is configured (non-null) but its "b" member is Unknown -- the
	// shape an Optional+Computed sub-attribute takes when the practitioner
	// sets the object but not every member on create (firewall_policy's own
	// matching_target_type does this; ObjectField.CopyPlanToState exists
	// specifically to keep an Unknown member like this out of state).
	nested, diags := types.ObjectValue(ssObjNestedAttrTypes, map[string]attr.Value{
		"a": types.StringValue("configured-a"),
		"b": types.StringUnknown(),
	})
	if diags.HasError() {
		t.Fatalf("build nested: %v", diags)
	}
	// extra is also configured, which is what makes the Extra's own Write
	// (and its old, buggy ApplyPlanToState call) actually run.
	sectionObject, diags := types.ObjectValue(ssObjAttrTypes(), map[string]attr.Value{
		"name":   types.StringValue("n"),
		"nested": nested,
		"extra":  types.StringValue("configured-extra"),
	})
	if diags.HasError() {
		t.Fatalf("build section: %v", diags)
	}

	plan := ssModel{Section: sectionObject}
	diags = section.Write(context.Background(), "site-1", &plan, nil, "Creating")
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}

	var got ssObjSectionModel
	diags = plan.Section.As(context.Background(), &got, basetypes.ObjectAsOptions{})
	if diags.HasError() {
		t.Fatalf("decode written-back section: %v", diags)
	}
	b, ok := got.Nested.Attributes()["b"]
	if !ok || b.IsUnknown() {
		t.Errorf("nested.b = %v, want a known value -- the Extra's own write must not "+
			"overwrite the primary's ObjectField merge with the plan's raw (Unknown-carrying) "+
			"object", b)
	}
}

// ssPartialModel is ssSectionModel's sibling for a partially-configured
// Extra: an Extra document (like usg_geo) maps TWO fields (A, B) of the
// SAME model, the same shape the four geo_ip_filtering_* attributes take
// against usgGeoKitSpec.
type ssPartialModel struct {
	Name types.String `tfsdk:"name"`
	A    types.String `tfsdk:"a"`
	B    types.String `tfsdk:"b"`
}

func ssPartialAttrTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"name": types.StringType,
		"a":    types.StringType,
		"b":    types.StringType,
	}
}

// ssPartialSDK stands in for the primary's own SDK struct -- Name only, the
// same shape settings.Usg has relative to its geo fields, which live on a
// wholly separate document (settings.SettingUsgGeoIPFiltering).
type ssPartialSDK struct {
	Name string
}

func ssPartialSpec(backend Backend[ssPartialSDK]) Spec[ssPartialModel, ssPartialSDK] {
	return Spec[ssPartialModel, ssPartialSDK]{
		TypeName: "ss_partial_probe",
		Subject:  "SS Partial Probe",
		New:      func() *ssPartialSDK { return &ssPartialSDK{} },
		Fields: []Field[ssPartialModel, ssPartialSDK]{
			StringField[ssPartialModel, ssPartialSDK]{
				Wire:  "name",
				Model: func(m *ssPartialModel) *types.String { return &m.Name },
				SDK:   func(s *ssPartialSDK) *string { return &s.Name },
				Elide: KeepZero,
			},
		},
		Backend: backend,
	}
}

// ssPartialExtraSDK stands in for settings.SettingUsgGeoIPFiltering: two
// wire members on one document, enough to configure one and leave the
// other null -- usg_geo has four, but two is the minimum that reproduces
// the bug.
type ssPartialExtraSDK struct {
	A string
	B string
}

func ssPartialExtraSpec(backend Backend[ssPartialExtraSDK]) Spec[ssPartialModel, ssPartialExtraSDK] {
	return Spec[ssPartialModel, ssPartialExtraSDK]{
		TypeName: "ss_partial_probe_extra",
		Subject:  "SS Partial Probe Extra",
		New:      func() *ssPartialExtraSDK { return &ssPartialExtraSDK{} },
		Fields: []Field[ssPartialModel, ssPartialExtraSDK]{
			StringField[ssPartialModel, ssPartialExtraSDK]{
				Wire:  "a",
				Model: func(m *ssPartialModel) *types.String { return &m.A },
				SDK:   func(s *ssPartialExtraSDK) *string { return &s.A },
				Elide: KeepZero,
			},
			StringField[ssPartialModel, ssPartialExtraSDK]{
				Wire:  "b",
				Model: func(m *ssPartialModel) *types.String { return &m.B },
				SDK:   func(s *ssPartialExtraSDK) *string { return &s.B },
				Elide: KeepZero,
			},
		},
		Backend: backend,
	}
}

// TestSpecSectionExtraFieldThePlanLeftNullStaysNullAfterWriteAndRead is
// final-review's Important 1, reproduced directly: an Extra document maps
// two fields of the section model (A, B), the same shape usg_geo's four
// geo_ip_filtering_* attributes take against usg's own 33. The plan sets
// only A; the Extra's own masked write still gets back BOTH fields in the
// controller's response (UpdateSettingFields unmarshals the whole document,
// the same way go-unifi's UpdateSetting does for usg_geo), so B must stay
// null both right after Write and after the follow-up Read Composite always
// runs next -- using the SAME plan Write just mutated, exactly as
// Composite.Create/Update do.
//
// Before the fix this fails both assertions with the review's own
// reproduction: b = "controller-b" (null=false) in both places, because
// AfterReceive ran before the Extra loop (so the Extra's own response
// merge undid the null it had just set) and Composite's follow-up Read
// then decoded its prior from that same wrong, non-null value.
func TestSpecSectionExtraFieldThePlanLeftNullStaysNullAfterWriteAndRead(t *testing.T) {
	section := SpecSection[ssModel, ssPartialModel, ssPartialSDK]{
		SectionName: "section",
		Get:         func(m *ssModel) *types.Object { return &m.Section },
		Set:         func(m *ssModel, o types.Object) { m.Section = o },
		AttrTypes:   ssPartialAttrTypes(),
		Spec: ssPartialSpec(Backend[ssPartialSDK]{
			UpdateFields: func(_ context.Context, _ string, in *ssPartialSDK, _ ...string) (*ssPartialSDK, error) {
				return in, nil
			},
			Read: func(context.Context, string, string) (*ssPartialSDK, error) {
				return &ssPartialSDK{Name: "server-name"}, nil
			},
		}),
		Extra: []Document[ssPartialModel]{
			SpecDocument[ssPartialModel, ssPartialExtraSDK]{
				Spec: ssPartialExtraSpec(Backend[ssPartialExtraSDK]{
					UpdateFields: func(_ context.Context, _ string, in *ssPartialExtraSDK, _ ...string) (*ssPartialExtraSDK, error) {
						// The controller's response carries every field this
						// document maps, whether or not the plan set it -- the
						// same shape UpdateSettingFields' own unmarshal gives
						// usg_geo's response.
						return &ssPartialExtraSDK{A: in.A, B: "controller-b"}, nil
					},
					Read: func(context.Context, string, string) (*ssPartialExtraSDK, error) {
						return &ssPartialExtraSDK{A: "controller-a", B: "controller-b"}, nil
					},
				}),
			},
		},
		AfterReceive: func(_ context.Context, _ *ssPartialSDK, model *ssPartialModel, prior ssPartialModel) diag.Diagnostics {
			if prior.A.IsNull() || prior.A.IsUnknown() {
				model.A = types.StringNull()
			}
			if prior.B.IsNull() || prior.B.IsUnknown() {
				model.B = types.StringNull()
			}
			return nil
		},
	}

	object, diags := types.ObjectValue(ssPartialAttrTypes(), map[string]attr.Value{
		"name": types.StringValue("configured-name"),
		"a":    types.StringValue("configured-a"),
		"b":    types.StringNull(),
	})
	if diags.HasError() {
		t.Fatalf("build section: %v", diags)
	}

	plan := ssModel{Section: object}
	diags = section.Write(context.Background(), "site-1", &plan, nil, "Creating")
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}

	var afterWrite ssPartialModel
	diags = plan.Section.As(context.Background(), &afterWrite, basetypes.ObjectAsOptions{})
	if diags.HasError() {
		t.Fatalf("decode written-back section: %v", diags)
	}
	if !afterWrite.B.IsNull() {
		t.Errorf("after Write: b = %q (null=%v), want null -- the plan never set it, but the "+
			"Extra's own response echoed the controller's value",
			afterWrite.B.ValueString(), afterWrite.B.IsNull())
	}

	// Read again, reusing the SAME plan Write just mutated for both the
	// plan and out arguments -- the shape Composite's Create/Update used
	// before composite.go was fixed to hand read() a copy of the original
	// plan instead (composite_test.go pins that half separately). Reusing
	// it here, even though Composite itself no longer does, proves the
	// kit-level fix alone -- AfterReceive running once, after the Extra
	// loop -- is enough to keep b null without leaning on the Composite
	// fix too.
	diags = section.Read(context.Background(), "site-1", &plan, &plan)
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	var afterRead ssPartialModel
	diags = plan.Section.As(context.Background(), &afterRead, basetypes.ObjectAsOptions{})
	if diags.HasError() {
		t.Fatalf("decode read section: %v", diags)
	}
	if !afterRead.B.IsNull() {
		t.Errorf("after the follow-up Read: b = %q (null=%v), want null",
			afterRead.B.ValueString(), afterRead.B.IsNull())
	}
}
