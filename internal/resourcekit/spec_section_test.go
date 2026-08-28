package resourcekit

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
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
