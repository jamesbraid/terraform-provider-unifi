package resourcekit

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
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

func (f fakeDocument) Write(context.Context, string, *ssSectionModel, *ssSectionModel) diag.Diagnostics {
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
// back to today's plain "Error Writing <Subject>" diagnostic -- a
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
		diags := document.Write(context.Background(), "site-1", &plan, &prior)
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
		diags := document.Write(context.Background(), "site-1", &plan, &prior)
		if !diags.HasError() {
			t.Fatal("expected today's default diagnostic, not silence")
		}
		got := diags.Errors()
		wantSummary := "Error Writing SS Probe Extra"
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
