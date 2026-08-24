package resourcekit

import (
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-nettypes/iptypes"
	"github.com/hashicorp/terraform-plugin-framework-timetypes/timetypes"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
)

type probeModel struct {
	Req types.String `tfsdk:"req"`
	Opt types.String `tfsdk:"opt"`
	Cmp types.String `tfsdk:"cmp"`
}

type probeSDK struct {
	Req string
	Opt string
	Cmp string
}

func probeSchema() schema.Schema {
	return schema.Schema{Attributes: map[string]schema.Attribute{
		"req": schema.StringAttribute{Required: true},
		"opt": schema.StringAttribute{Optional: true},
		"cmp": schema.StringAttribute{Computed: true},
	}}
}

// probeSpec builds a descriptor whose Elide values are correct by the rule.
func probeSpec(req, opt, cmp ElideZero) Spec[probeModel, probeSDK] {
	return Spec[probeModel, probeSDK]{
		TypeName: "probe",
		Fields: []Field[probeModel, probeSDK]{
			StringField[probeModel, probeSDK]{
				Wire:  "req",
				Model: func(m *probeModel) *types.String { return &m.Req },
				SDK:   func(s *probeSDK) *string { return &s.Req }, Elide: req,
			},
			StringField[probeModel, probeSDK]{
				Wire:  "opt",
				Model: func(m *probeModel) *types.String { return &m.Opt },
				SDK:   func(s *probeSDK) *string { return &s.Opt }, Elide: opt,
			},
			StringField[probeModel, probeSDK]{
				Wire:  "cmp",
				Model: func(m *probeModel) *types.String { return &m.Cmp },
				SDK:   func(s *probeSDK) *string { return &s.Cmp }, Elide: cmp,
			},
		},
	}
}

func TestACorrectDescriptorProducesNoProblems(t *testing.T) {
	problems := ElideProblems(probeSpec(KeepZero, NullZero, KeepZero), probeSchema())
	if len(problems) != 0 {
		t.Fatalf("a descriptor obeying the rule was reported wrong, so every must-fail case "+
			"below would pass for the wrong reason: %v", problems)
	}
}

func TestTheCheckGoesRedOnTheMutationThatWentUnnoticed(t *testing.T) {
	flipped := probeSpec(NullZero, KeepZero, NullZero)
	problems := ElideProblems(flipped, probeSchema())
	if len(problems) != 3 {
		t.Fatalf("flipping every Elide produced %d problem(s), want 3: %v", len(problems), problems)
	}
	for _, want := range []string{"probe.req", "probe.opt", "probe.cmp"} {
		if !strings.Contains(strings.Join(problems, "\n"), want) {
			t.Errorf("no problem named %s; got %v", want, problems)
		}
	}
}

func TestEachFieldIsJudgedOnItsOwn(t *testing.T) {
	only := probeSpec(NullZero, NullZero, KeepZero) // req alone is wrong
	problems := ElideProblems(only, probeSchema())
	if len(problems) != 1 {
		t.Fatalf("one wrong field produced %d problem(s), want 1 (else a check "+
			"that always fires would also pass the case above): %v", len(problems), problems)
	}
	if !strings.Contains(problems[0], "probe.req") {
		t.Errorf("the wrong field was not the one reported: %v", problems)
	}
	if !strings.Contains(problems[0], "Required") || !strings.Contains(problems[0], "KeepZero") {
		t.Errorf(
			"the message does not say what the schema declares or what it wants: %q",
			problems[0],
		)
	}
}

func TestAnAttributeMissingFromTheSchemaIsReported(t *testing.T) {
	bare := schema.Schema{
		Attributes: map[string]schema.Attribute{"req": schema.StringAttribute{Required: true}},
	}
	problems := ElideProblems(probeSpec(KeepZero, NullZero, KeepZero), bare)
	if len(problems) != 2 {
		t.Fatalf(
			"two attributes absent from the schema produced %d problem(s): %v",
			len(problems),
			problems,
		)
	}
	if !strings.Contains(strings.Join(problems, "\n"), "does not declare") {
		t.Errorf("the message does not say the schema lacks the attribute: %v", problems)
	}
}

func TestOptionalComputedKeepsItsZero(t *testing.T) {
	optionalComputed := schema.Schema{Attributes: map[string]schema.Attribute{
		"req": schema.StringAttribute{Required: true},
		"opt": schema.StringAttribute{Optional: true},
		"cmp": schema.StringAttribute{Optional: true, Computed: true},
	}}

	// cmp is Optional+Computed, so KeepZero is correct and NullZero is not.
	if problems := ElideProblems(
		probeSpec(KeepZero, NullZero, KeepZero),
		optionalComputed,
	); len(
		problems,
	) != 0 {
		t.Fatalf("Optional+Computed with KeepZero was reported wrong: %v", problems)
	}
	problems := ElideProblems(probeSpec(KeepZero, NullZero, NullZero), optionalComputed)
	if len(problems) != 1 {
		t.Fatalf(
			"Optional+Computed with NullZero produced %d problem(s), want 1: %v",
			len(problems),
			problems,
		)
	}
	if !strings.Contains(problems[0], "Optional+Computed") {
		t.Errorf("the message collapses the combination that is at issue: %q", problems[0])
	}
}

func TestAFieldKindWithNoElideIsReportedUnlessExempt(t *testing.T) {
	spec := Spec[flagModel, flagSDK]{
		TypeName: "probe",
		Fields: []Field[flagModel, flagSDK]{
			// BoolField is exempt by design and must stay silent.
			BoolField[flagModel, flagSDK]{
				Wire:  "req",
				Model: func(m *flagModel) *types.Bool { return &m.Flag },
				SDK:   func(s *flagSDK) *bool { return &s.Flag },
			},
		},
	}
	bare := schema.Schema{Attributes: map[string]schema.Attribute{
		"req": schema.BoolAttribute{Required: true},
	}}
	if problems := ElideProblems(spec, bare); len(problems) != 0 {
		t.Errorf("an exempt field kind was reported: %v", problems)
	}
}

type flagModel struct {
	Flag types.Bool `tfsdk:"req"`
}

type flagSDK struct {
	Flag bool
}

func TestTheCheckReachesThroughReadOnly(t *testing.T) {
	optional := schema.Schema{Attributes: map[string]schema.Attribute{
		"req": schema.StringAttribute{Required: true},
		"opt": schema.StringAttribute{Optional: true},
		"cmp": schema.StringAttribute{Optional: true},
	}}
	// cmp is Optional-only, so NullZero is correct and KeepZero is not.
	spec := probeSpec(KeepZero, NullZero, NullZero)
	spec.Fields[2] = ReadOnly(spec.Fields[2])
	if problems := ElideProblems(spec, optional); len(problems) != 0 {
		t.Fatalf("a correct read-only field was reported: %v", problems)
	}

	wrong := probeSpec(KeepZero, NullZero, KeepZero)
	wrong.Fields[2] = ReadOnly(wrong.Fields[2])
	problems := ElideProblems(wrong, optional)
	if len(problems) != 1 {
		t.Fatalf("a wrong Elide inside ReadOnly produced %d problem(s), want 1: %v",
			len(problems), problems)
	}
	if !strings.Contains(problems[0], "probe.cmp") {
		t.Errorf("the wrapped field was not the one named: %v", problems)
	}
}

type splitModel struct {
	Enum types.String `tfsdk:"enum"`
	Free types.String `tfsdk:"free"`
}

type splitSDK struct {
	Enum string
	Free string
}

func splitSpec(enum, free ElideZero) Spec[splitModel, splitSDK] {
	return Spec[splitModel, splitSDK]{
		TypeName: "split",
		Fields: []Field[splitModel, splitSDK]{
			StringField[splitModel, splitSDK]{
				Wire:  "enum",
				Model: func(m *splitModel) *types.String { return &m.Enum },
				SDK:   func(s *splitSDK) *string { return &s.Enum }, Elide: enum,
			},
			StringField[splitModel, splitSDK]{
				Wire:  "free",
				Model: func(m *splitModel) *types.String { return &m.Free },
				SDK:   func(s *splitSDK) *string { return &s.Free }, Elide: free,
			},
		},
	}
}

func splitSchema(required bool) schema.Schema {
	enum := schema.StringAttribute{
		Optional:   !required,
		Computed:   !required,
		Required:   required,
		Validators: []validator.String{stringvalidator.OneOf("auto", "manual")},
	}
	return schema.Schema{Attributes: map[string]schema.Attribute{
		"enum": enum,
		// Same flags, no validator: a free-text attribute whose empty value is
		// something the practitioner could legitimately have written.
		"free": schema.StringAttribute{
			Optional: !required,
			Computed: !required,
			Required: required,
		},
	}}
}

func TestOptionalComputedSplitsOnWhetherTheZeroIsLegal(t *testing.T) {
	// Correct: the enum nulls its illegal zero, the free-text one keeps its
	// legal one. Nothing reported.
	if problems := ElideProblems(
		splitSpec(NullZero, KeepZero),
		splitSchema(false),
	); len(
		problems,
	) != 0 {
		t.Fatalf(
			"the correct pair was reported, so the must-fail cases below prove nothing: %v",
			problems,
		)
	}

	// Swapped: both must be reported, and each names its own attribute. One
	// problem would mean only half the rule is doing work.
	problems := ElideProblems(splitSpec(KeepZero, NullZero), splitSchema(false))
	if len(problems) != 2 {
		t.Fatalf("swapping both values produced %d problem(s), want 2: %v", len(problems), problems)
	}
	joined := strings.Join(problems, "\n")
	if !strings.Contains(joined, "split.enum") || !strings.Contains(joined, "split.free") {
		t.Errorf("the report does not name both attributes: %v", problems)
	}
}

func TestARequiredAttributeKeepsItsZeroEvenWithARejectingValidator(t *testing.T) {
	if problems := ElideProblems(
		splitSpec(KeepZero, KeepZero),
		splitSchema(true),
	); len(
		problems,
	) != 0 {
		t.Fatalf("a Required attribute was made subject to the zero-is-legal split: %v", problems)
	}
	if problems := ElideProblems(
		splitSpec(NullZero, NullZero),
		splitSchema(true),
	); len(
		problems,
	) != 2 {
		t.Fatalf("the Required case reports %d problem(s) for two wrong values, so the "+
			"assertion above passes for a rule that never fires: %v", len(problems), problems)
	}
}

func TestZeroIsRejectedAsksTheValidatorsRatherThanGuessing(t *testing.T) {
	for _, testCase := range []struct {
		name      string
		attribute schema.Attribute
		want      bool
	}{
		{"OneOf excluding the empty string", schema.StringAttribute{
			Validators: []validator.String{stringvalidator.OneOf("auto", "manual")},
		}, true},
		{"OneOf including the empty string", schema.StringAttribute{
			Validators: []validator.String{stringvalidator.OneOf("", "auto")},
		}, false},
		{"no validators at all", schema.StringAttribute{}, false},
		{"a length floor, which is not a OneOf", schema.StringAttribute{
			Validators: []validator.String{stringvalidator.LengthAtLeast(1)},
		}, true},
		{"a non-string attribute", schema.SetAttribute{ElementType: types.StringType}, false},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if got := zeroIsRejected(testCase.attribute, true); got != testCase.want {
				t.Errorf("zeroIsRejected = %v, want %v", got, testCase.want)
			}
		})
	}
}

func TestACustomTypedAttributeIsJudgedByTheFieldKindsZero(t *testing.T) {
	custom := schema.StringAttribute{
		Optional:   true,
		Computed:   true,
		CustomType: timetypes.GoDurationType{},
		Validators: []validator.String{stringvalidator.LengthAtLeast(1)},
	}
	if zeroIsRejected(custom, false) {
		t.Error("a kind whose zero is not \"\" was probed with it, so its zero was judged by " +
			"whether the empty string parses rather than whether zero is legal")
	}
	if !zeroIsRejected(custom, true) {
		t.Error("a kind whose zero IS \"\" was not asked the type's own opinion of it")
	}
	plain := custom
	plain.CustomType = nil
	if !zeroIsRejected(plain, false) {
		t.Fatal("the validator does not reject \"\" at all, so the assertion above proves nothing")
	}
}

// This branch currently has no live instance in the generated schemas (the
// descriptor that shipped the bug had its conflicting default removed) -- kept
// here, synthetic and permanent, as the only guard against a policy author
// reintroducing the shape in one line.
func TestAZeroDefaultOutranksAValidatorThatRejectsIt(t *testing.T) {
	rejectsEmpty := []validator.String{stringvalidator.OneOf("disabled", "optional", "required")}

	// Without the default the validator decides: the zero is not a legal value,
	// so an empty from the controller is an absence.
	noDefault := schema.Schema{Attributes: map[string]schema.Attribute{
		"req": schema.StringAttribute{Required: true},
		"opt": schema.StringAttribute{Optional: true},
		"cmp": schema.StringAttribute{
			Optional: true, Computed: true, Validators: rejectsEmpty,
		},
	}}
	if problems := ElideProblems(probeSpec(KeepZero, NullZero, NullZero), noDefault); len(problems) != 0 {
		t.Fatalf("a rejecting validator with no default should want NullZero: %v", problems)
	}
	if problems := ElideProblems(probeSpec(KeepZero, NullZero, KeepZero), noDefault); len(problems) != 1 {
		t.Fatalf("KeepZero against a rejecting validator produced %d problem(s), want 1: %v",
			len(problems), problems)
	}

	// WITH a zero default the answer flips, because the default is what decides
	// the plan and the read has to give back what the plan holds.
	zeroDefault := schema.Schema{Attributes: map[string]schema.Attribute{
		"req": schema.StringAttribute{Required: true},
		"opt": schema.StringAttribute{Optional: true},
		"cmp": schema.StringAttribute{
			Optional: true, Computed: true,
			Validators: rejectsEmpty,
			Default:    stringdefault.StaticString(""),
		},
	}}
	if problems := ElideProblems(probeSpec(KeepZero, NullZero, KeepZero), zeroDefault); len(problems) != 0 {
		t.Fatalf("a zero default should make KeepZero correct despite the validator: %v", problems)
	}
	problems := ElideProblems(probeSpec(KeepZero, NullZero, NullZero), zeroDefault)
	if len(problems) != 1 {
		t.Fatalf("NullZero against a zero default produced %d problem(s), want 1: %v",
			len(problems), problems)
	}

	realDefault := schema.Schema{Attributes: map[string]schema.Attribute{
		"req": schema.StringAttribute{Required: true},
		"opt": schema.StringAttribute{Optional: true},
		"cmp": schema.StringAttribute{
			Optional: true, Computed: true,
			Validators: rejectsEmpty,
			Default:    stringdefault.StaticString("disabled"),
		},
	}}
	if problems := ElideProblems(probeSpec(KeepZero, NullZero, NullZero), realDefault); len(problems) != 0 {
		t.Fatalf("a non-zero default should leave the validator deciding, or the "+
			"veto would wrongly swallow firewall_policy's protocol and "+
			"ip_version: %v", problems)
	}
}

type customProbeModel struct {
	Addr iptypes.IPv4Address `tfsdk:"addr"`
}

type customProbeSDK struct{ Addr string }

func customProbeSpec(elide ElideZero) Spec[customProbeModel, customProbeSDK] {
	return Spec[customProbeModel, customProbeSDK]{
		TypeName: "probe",
		Fields: []Field[customProbeModel, customProbeSDK]{
			StringLikeField[customProbeModel, customProbeSDK, iptypes.IPv4Address]{
				Wire:  "addr",
				Model: func(m *customProbeModel) *iptypes.IPv4Address { return &m.Addr },
				SDK:   func(s *customProbeSDK) *string { return &s.Addr },
				New: func(v basetypes.StringValue) iptypes.IPv4Address {
					return iptypes.IPv4Address{StringValue: v}
				},
				Elide: elide,
			},
		},
	}
}

func customProbeSchema(required bool) schema.Schema {
	attribute := schema.StringAttribute{CustomType: iptypes.IPv4AddressType{}}
	if required {
		attribute.Required = true
	} else {
		attribute.Optional = true
		attribute.Computed = true
	}
	return schema.Schema{Attributes: map[string]schema.Attribute{"addr": attribute}}
}

func TestElideProblemsAsksACustomTypeAboutItsOwnZero(t *testing.T) {
	if problems := ElideProblems(customProbeSpec(NullZero), customProbeSchema(false)); len(problems) != 0 {
		t.Errorf("NullZero on an optional custom type that rejects \"\" should be clean: %v", problems)
	}
	problems := ElideProblems(customProbeSpec(KeepZero), customProbeSchema(false))
	if len(problems) != 1 {
		t.Fatalf("KeepZero on an optional custom type that rejects \"\" produced %d problem(s), "+
			"want 1: it reads an unset value back as one the type refuses", len(problems))
	}

	// Required stays KeepZero exactly as it does for plain strings: the value
	// is always present on a real read, and the split deliberately stops
	// short of Required.
	if problems := ElideProblems(customProbeSpec(KeepZero), customProbeSchema(true)); len(problems) != 0 {
		t.Errorf("Required keeps KeepZero even for a rejecting custom type: %v", problems)
	}
}

type durationProbeModel struct {
	Wait timetypes.GoDuration `tfsdk:"wait"`
}

type durationProbeSDK struct{ Wait *int64 }

func durationProbeSpec(elide ElideZero) Spec[durationProbeModel, durationProbeSDK] {
	return Spec[durationProbeModel, durationProbeSDK]{
		TypeName: "probe",
		Fields: []Field[durationProbeModel, durationProbeSDK]{
			DurationPtrField[durationProbeModel, durationProbeSDK]{
				Wire:  "wait",
				Model: func(m *durationProbeModel) *timetypes.GoDuration { return &m.Wait },
				SDK:   func(s *durationProbeSDK) **int64 { return &s.Wait },
				Units: time.Second,
				Elide: elide,
			},
		},
	}
}

func TestElideProblemsDoesNotProbeAKindWhoseZeroIsNotTheEmptyString(t *testing.T) {
	built := schema.Schema{Attributes: map[string]schema.Attribute{
		"wait": schema.StringAttribute{
			CustomType: timetypes.GoDurationType{},
			Optional:   true, Computed: true,
		},
	}}
	if problems := ElideProblems(durationProbeSpec(KeepZero), built); len(problems) != 0 {
		t.Errorf("KeepZero on a duration pointer should be clean; its elided zero is 0, "+
			"which GoDuration accepts: %v", problems)
	}
	if problems := ElideProblems(durationProbeSpec(NullZero), built); len(problems) != 1 {
		t.Errorf("NullZero on a duration pointer should be flagged; nulling a zero "+
			"turns a real 0s into an absence: got %v", problems)
	}
}
