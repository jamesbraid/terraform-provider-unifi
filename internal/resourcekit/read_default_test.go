package resourcekit

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-timetypes/timetypes"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

type defaultModel struct {
	Gateway types.String `tfsdk:"gateway_type"`
}

type defaultSDK struct {
	Gateway string
}

func defaultField(readDefault string) StringField[defaultModel, defaultSDK] {
	return StringField[defaultModel, defaultSDK]{
		Wire:        "gateway_type",
		Model:       func(m *defaultModel) *types.String { return &m.Gateway },
		SDK:         func(s *defaultSDK) *string { return &s.Gateway },
		Elide:       KeepZero,
		ReadDefault: readDefault,
	}
}

func TestReadDefaultSubstitutesOnlyForAnEmptyRead(t *testing.T) {
	for _, testCase := range []struct {
		name string
		sdk  string
		want string
	}{
		{"empty read takes the default", "", "default"},
		{"a real value is untouched", "wan2", "wan2"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			var model defaultModel
			sdk := defaultSDK{Gateway: testCase.sdk}
			if diags := defaultField(
				"default",
			).ToModel(context.Background(), &sdk, &model); diags.HasError() {
				t.Fatalf("ToModel: %v", diags)
			}
			if got := model.Gateway.ValueString(); got != testCase.want {
				t.Fatalf("gateway_type = %q, want %q", got, testCase.want)
			}
		})
	}
}

func TestAFieldWithNoReadDefaultIsUnchanged(t *testing.T) {
	var model defaultModel
	sdk := defaultSDK{}
	if diags := defaultField("").ToModel(context.Background(), &sdk, &model); diags.HasError() {
		t.Fatalf("ToModel: %v", diags)
	}
	if got := model.Gateway.ValueString(); got != "" {
		t.Fatalf("a field declaring no default substituted %q anyway, which "+
			"the previous test alone wouldn't catch", got)
	}
}

func TestAReadDefaultDoesNotChangeWhatThePlanReportsAsSet(t *testing.T) {
	var absent defaultModel
	absent.Gateway = types.StringNull()
	for _, testCase := range []struct {
		name  string
		field StringField[defaultModel, defaultSDK]
	}{
		{"with a read default", defaultField("default")},
		{"without one", defaultField("")},
	} {
		if testCase.field.SetInPlan(&absent) {
			t.Errorf(
				"%s: a null plan value reported as set, so the default would be sent on update",
				testCase.name,
			)
		}
	}
	// The positive half: a value the practitioner did write is still
	// reported, or the assertions above would hold for a SetInPlan that
	// always says no.
	present := defaultModel{Gateway: types.StringValue("wan2")}
	if !defaultField("default").SetInPlan(&present) {
		t.Error("a value the practitioner wrote was not reported as set")
	}
}

func defaultSchema(optional, computed bool) schema.Schema {
	return schema.Schema{Attributes: map[string]schema.Attribute{
		"gateway_type": schema.StringAttribute{Optional: optional, Computed: computed},
	}}
}

func defaultSpec(readDefault string) Spec[defaultModel, defaultSDK] {
	return Spec[defaultModel, defaultSDK]{
		TypeName: "probe_default",
		Fields:   []Field[defaultModel, defaultSDK]{defaultField(readDefault)},
	}
}

func TestReadDefaultIsAcceptedOnOptionalComputed(t *testing.T) {
	problems := ElideProblems(defaultSpec("default"), defaultSchema(true, true))
	if len(problems) != 0 {
		t.Fatalf("a legal default was reported: %v", problems)
	}
}

// Each case is Optional+Computed with exactly one flag removed.
func TestReadDefaultIsRefusedWhereTheValueCannotLegallyBeSupplied(t *testing.T) {
	for _, testCase := range []struct {
		name               string
		optional, computed bool
		wantIn             string
	}{
		{"not computed", true, false, "Optional-only"},
		{"not optional", false, true, "Computed-only"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			problems := ElideProblems(defaultSpec("default"),
				defaultSchema(testCase.optional, testCase.computed))
			if len(problems) != 1 {
				t.Fatalf("want exactly 1 problem, got %d: %v", len(problems), problems)
			}
			if !strings.Contains(problems[0], "gateway_type") ||
				!strings.Contains(problems[0], testCase.wantIn) {
				t.Fatalf("problem %q does not name the attribute and its flags (%s)",
					problems[0], testCase.wantIn)
			}
		})
	}
}

func TestADefaultedFieldIsNotAlsoJudgedOnItsElide(t *testing.T) {
	// Optional+Computed wants KeepZero under the elide rule; this spec declares
	// NullZero, which WOULD be reported were the field not defaulted.
	spec := defaultSpec("default")
	field := defaultField("default")
	field.Elide = NullZero
	spec.Fields = []Field[defaultModel, defaultSDK]{field}
	if problems := ElideProblems(spec, defaultSchema(true, true)); len(problems) != 0 {
		t.Fatalf("a defaulted field was judged on its unreachable Elide: %v", problems)
	}

	field.ReadDefault = ""
	spec.Fields = []Field[defaultModel, defaultSDK]{field}
	if problems := ElideProblems(spec, defaultSchema(true, true)); len(problems) != 1 {
		t.Fatalf("the elide rule does not fire on this shape at all, so the exemption above "+
			"proved nothing: %v", problems)
	}
}

type durationModel struct {
	Idle timetypes.GoDuration `tfsdk:"idle"`
}

type durationSDK struct {
	Idle *int64
}

func durationPtrField() DurationPtrField[durationModel, durationSDK] {
	return DurationPtrField[durationModel, durationSDK]{
		Wire:  "idle",
		Model: func(m *durationModel) *timetypes.GoDuration { return &m.Idle },
		SDK:   func(s *durationSDK) **int64 { return &s.Idle },
		Units: time.Second,
		Elide: KeepZero,
	}
}

func TestDurationPtrDistinguishesNilFromZero(t *testing.T) {
	zero := int64(0)
	ninety := int64(90)
	for _, testCase := range []struct {
		name     string
		sdk      *int64
		wantNull bool
		want     string
	}{
		{"nil is null", nil, true, ""},
		{"a pointer to zero is a value under KeepZero", &zero, false, "0s"},
		{"a real value round-trips", &ninety, false, "1m30s"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			var model durationModel
			sdk := durationSDK{Idle: testCase.sdk}
			if diags := durationPtrField().ToModel(context.Background(), &sdk, &model); diags.HasError() {
				t.Fatalf("ToModel: %v", diags)
			}
			if model.Idle.IsNull() != testCase.wantNull {
				t.Fatalf("IsNull = %v, want %v", model.Idle.IsNull(), testCase.wantNull)
			}
			if !testCase.wantNull && model.Idle.ValueString() != testCase.want {
				t.Errorf("value = %q, want %q", model.Idle.ValueString(), testCase.want)
			}
		})
	}
}

func TestDurationPtrLeavesTheSDKNilForANullPlan(t *testing.T) {
	var sdk durationSDK
	model := durationModel{Idle: timetypes.NewGoDurationNull()}
	if diags := durationPtrField().ToSDK(context.Background(), &model, &sdk); diags.HasError() {
		t.Fatalf("ToSDK: %v", diags)
	}
	if sdk.Idle != nil {
		t.Fatalf("a null plan set the pointer to %d", *sdk.Idle)
	}
	// The control: a real value does reach it, or the assertion above would
	// hold for a ToSDK that never writes anything.
	model.Idle = timetypes.NewGoDurationValueFromStringMust("2m")
	if diags := durationPtrField().ToSDK(context.Background(), &model, &sdk); diags.HasError() {
		t.Fatalf("ToSDK: %v", diags)
	}
	if sdk.Idle == nil || *sdk.Idle != 120 {
		t.Fatalf("a real duration did not reach the SDK as 120 seconds: %v", sdk.Idle)
	}
}

type intModel struct {
	Group types.Int64 `tfsdk:"group"`
}

type intSDK struct {
	Group *int64
}

func intPtrField(omitZero bool) Int64PtrField[intModel, intSDK] {
	return Int64PtrField[intModel, intSDK]{
		Wire:     "group",
		Model:    func(m *intModel) *types.Int64 { return &m.Group },
		SDK:      func(s *intSDK) **int64 { return &s.Group },
		Elide:    NullZero,
		OmitZero: omitZero,
	}
}

func TestInt64PtrOmitsZeroOnlyWhenAsked(t *testing.T) {
	for _, testCase := range []struct {
		name     string
		omitZero bool
		value    types.Int64
		wantNil  bool
	}{
		{"zero is sent by default", false, types.Int64Value(0), false},
		{"zero is omitted when asked", true, types.Int64Value(0), true},
		{"a real value is always sent", true, types.Int64Value(14), false},
		{"null is never sent", false, types.Int64Null(), true},
		{"unknown is omitted when asked", true, types.Int64Unknown(), true},
		{"unknown is sent as a pointer to zero by default", false, types.Int64Unknown(), false},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			var sdk intSDK
			model := intModel{Group: testCase.value}
			if diags := intPtrField(
				testCase.omitZero,
			).ToSDK(context.Background(), &model, &sdk); diags.HasError() {
				t.Fatalf("ToSDK: %v", diags)
			}
			if (sdk.Group == nil) != testCase.wantNil {
				got := "nil"
				if sdk.Group != nil {
					got = "a pointer to " + types.Int64PointerValue(sdk.Group).String()
				}
				t.Fatalf("sent %s, wantNil=%v", got, testCase.wantNil)
			}
		})
	}
}
