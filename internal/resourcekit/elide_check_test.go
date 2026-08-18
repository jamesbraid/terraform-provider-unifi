package resourcekit

import (
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework-timetypes/timetypes"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// THE CHECK'S OWN POSITIVE CONTROL.
//
// The defect that prompted this was invisible: flipping every Elide in a
// descriptor left the whole provider suite green. So the first thing this file
// establishes is that the check itself can go red -- a check that passes on a
// deliberately wrong descriptor is worth less than no check, because it reports
// agreement it never tested for.

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
			StringField[probeModel, probeSDK]{Wire: "req",
				Model: func(m *probeModel) *types.String { return &m.Req },
				SDK:   func(s *probeSDK) *string { return &s.Req }, Elide: req},
			StringField[probeModel, probeSDK]{Wire: "opt",
				Model: func(m *probeModel) *types.String { return &m.Opt },
				SDK:   func(s *probeSDK) *string { return &s.Opt }, Elide: opt},
			StringField[probeModel, probeSDK]{Wire: "cmp",
				Model: func(m *probeModel) *types.String { return &m.Cmp },
				SDK:   func(s *probeSDK) *string { return &s.Cmp }, Elide: cmp},
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

// TestTheCheckGoesRedOnTheMutationThatWentUnnoticed is the control that makes
// this file mean anything: the exact mutation the provider suite could not see.
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

// TestEachFieldIsJudgedOnItsOwn stops a check that fires on everything from
// satisfying the case above.
func TestEachFieldIsJudgedOnItsOwn(t *testing.T) {
	only := probeSpec(NullZero, NullZero, KeepZero) // req alone is wrong
	problems := ElideProblems(only, probeSchema())
	if len(problems) != 1 {
		t.Fatalf("one wrong field produced %d problem(s), want 1: %v", len(problems), problems)
	}
	if !strings.Contains(problems[0], "probe.req") {
		t.Errorf("the wrong field was not the one reported: %v", problems)
	}
	if !strings.Contains(problems[0], "Required") || !strings.Contains(problems[0], "KeepZero") {
		t.Errorf("the message does not say what the schema declares or what it wants: %q", problems[0])
	}
}

// TestAnAttributeMissingFromTheSchemaIsReported keeps the check from passing
// silently when it cannot find what it is meant to compare against.
func TestAnAttributeMissingFromTheSchemaIsReported(t *testing.T) {
	bare := schema.Schema{Attributes: map[string]schema.Attribute{"req": schema.StringAttribute{Required: true}}}
	problems := ElideProblems(probeSpec(KeepZero, NullZero, KeepZero), bare)
	if len(problems) != 2 {
		t.Fatalf("two attributes absent from the schema produced %d problem(s): %v", len(problems), problems)
	}
	if !strings.Contains(strings.Join(problems, "\n"), "does not declare") {
		t.Errorf("the message does not say the schema lacks the attribute: %v", problems)
	}
}

// TestOptionalComputedKeepsItsZero pins the case the rule originally got wrong
// and the surface it was validated on could not contain.
//
// dns_record has two Optional+Computed attributes and NEITHER reaches this
// check: enabled is a BoolField, which elides nothing, and site is served by
// Spec.Site rather than by a Field. So the rule shipped sending every
// Optional+Computed field to NullZero and the check agreed with it, because the
// specimen had no instance of the majority case. 277 of 528 attributes in the
// generated schemas are Optional+Computed.
//
// The behaviour is settled by the tree rather than by argument: ap_group's
// acceptance config sets `device_macs = []` explicitly, five steps use it, and
// the schema description says "May be empty -- the controller accepts a group
// with no members. Omit it to leave the membership as the controller has it."
// An explicit empty that the provider nulls makes state disagree with config.
func TestOptionalComputedKeepsItsZero(t *testing.T) {
	optionalComputed := schema.Schema{Attributes: map[string]schema.Attribute{
		"req": schema.StringAttribute{Required: true},
		"opt": schema.StringAttribute{Optional: true},
		"cmp": schema.StringAttribute{Optional: true, Computed: true},
	}}

	// cmp is Optional+Computed, so KeepZero is correct and NullZero is not.
	if problems := ElideProblems(probeSpec(KeepZero, NullZero, KeepZero), optionalComputed); len(problems) != 0 {
		t.Fatalf("Optional+Computed with KeepZero was reported wrong: %v", problems)
	}
	problems := ElideProblems(probeSpec(KeepZero, NullZero, NullZero), optionalComputed)
	if len(problems) != 1 {
		t.Fatalf("Optional+Computed with NullZero produced %d problem(s), want 1: %v", len(problems), problems)
	}
	if !strings.Contains(problems[0], "Optional+Computed") {
		t.Errorf("the message collapses the combination that is at issue: %q", problems[0])
	}
}

// TestAFieldKindWithNoElideIsReportedUnlessExempt closes the hole that hid the
// collection types: skipping every field without an Elide made "makes no claim"
// and "nobody wrote one" indistinguishable.
func TestAFieldKindWithNoElideIsReportedUnlessExempt(t *testing.T) {
	spec := Spec[flagModel, flagSDK]{
		TypeName: "probe",
		Fields: []Field[flagModel, flagSDK]{
			// BoolField is exempt by design and must stay silent.
			BoolField[flagModel, flagSDK]{Wire: "req",
				Model: func(m *flagModel) *types.Bool { return &m.Flag },
				SDK:   func(s *flagSDK) *bool { return &s.Flag }},
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

// TestTheCheckReachesThroughReadOnly is the control for the unwrap.
//
// A read-only field forwards ToModel, so its Elide still decides whether an API
// zero becomes null in state. Before Unwrap existed the check reported it as
// "carries no Elide", which was false; the risk in fixing that was silencing
// the field instead of checking it, and zero problems looks identical either
// way. So this asserts the check goes RED through the wrapper.
func TestTheCheckReachesThroughReadOnly(t *testing.T) {
	optional := schema.Schema{Attributes: map[string]schema.Attribute{
		"req": schema.StringAttribute{Required: true},
		"opt": schema.StringAttribute{Optional: true},
		"cmp": schema.StringAttribute{Optional: true},
	}}
	// cmp is Optional-only, so NullZero is correct and KeepZero is not --
	// wrapped in ReadOnly, which previously hid the claim entirely.
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

// The Optional+Computed split, which is the part of the rule that took three
// attempts to get right.
//
// The first version made every Optional+Computed KeepZero. That was written
// against ap_group's device_macs, where `device_macs = []` is a real
// configuration and nulling it would fight the config -- but applied to
// firewall_rule's setting_preference it demands the opposite of what the
// hand-written mapper does, and putting "" in state for an attribute whose own
// schema says OneOf("auto","manual") is a value the provider would reject if a
// practitioner wrote it.
//
// The second attempt split on whether the attribute declares a schema Default.
// That is refuted by the rule's own worked example: device_macs has no default
// either, so the test would have flipped the case it was built from.
//
// What separates them is whether the zero is a legal value, and the schema
// already knows because its validators are what would reject it.

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
			StringField[splitModel, splitSDK]{Wire: "enum",
				Model: func(m *splitModel) *types.String { return &m.Enum },
				SDK:   func(s *splitSDK) *string { return &s.Enum }, Elide: enum},
			StringField[splitModel, splitSDK]{Wire: "free",
				Model: func(m *splitModel) *types.String { return &m.Free },
				SDK:   func(s *splitSDK) *string { return &s.Free }, Elide: free},
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
		"free": schema.StringAttribute{Optional: !required, Computed: !required, Required: required},
	}}
}

func TestOptionalComputedSplitsOnWhetherTheZeroIsLegal(t *testing.T) {
	// Correct: the enum nulls its illegal zero, the free-text one keeps its
	// legal one. Nothing reported.
	if problems := ElideProblems(splitSpec(NullZero, KeepZero), splitSchema(false)); len(problems) != 0 {
		t.Fatalf("the correct pair was reported, so the must-fail cases below prove nothing: %v", problems)
	}

	// Swapped: BOTH must be reported, and each names its own attribute. One
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

// REQUIRED IS EXEMPT FROM THE SPLIT, and this is not hypothetical: three
// Required attributes across the shipped descriptors carry a OneOf that
// excludes the empty string, so a rule applying the split to Required would
// have demanded NullZero on all three and broken them.
func TestARequiredAttributeKeepsItsZeroEvenWithARejectingValidator(t *testing.T) {
	if problems := ElideProblems(splitSpec(KeepZero, KeepZero), splitSchema(true)); len(problems) != 0 {
		t.Fatalf("a Required attribute was made subject to the zero-is-legal split: %v", problems)
	}
	if problems := ElideProblems(splitSpec(NullZero, NullZero), splitSchema(true)); len(problems) != 2 {
		t.Fatalf("the Required case reports %d problem(s) for two wrong values, so the "+
			"assertion above passes for a rule that never fires: %v", len(problems), problems)
	}
}

// zeroIsRejected is the load-bearing half, so it is measured on its own rather
// than only through the rule. A validator set that silently answered "no" to
// everything would leave every Optional+Computed on KeepZero and look exactly
// like the unrefined rule.
func TestZeroIsRejectedAsksTheValidatorsRatherThanGuessing(t *testing.T) {
	for _, testCase := range []struct {
		name      string
		attribute schema.Attribute
		want      bool
	}{
		{"OneOf excluding the empty string", schema.StringAttribute{
			Validators: []validator.String{stringvalidator.OneOf("auto", "manual")}}, true},
		{"OneOf including the empty string", schema.StringAttribute{
			Validators: []validator.String{stringvalidator.OneOf("", "auto")}}, false},
		{"no validators at all", schema.StringAttribute{}, false},
		{"a length floor, which is not a OneOf", schema.StringAttribute{
			Validators: []validator.String{stringvalidator.LengthAtLeast(1)}}, true},
		{"a non-string attribute", schema.SetAttribute{ElementType: types.StringType}, false},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if got := zeroIsRejected(testCase.attribute); got != testCase.want {
				t.Errorf("zeroIsRejected = %v, want %v", got, testCase.want)
			}
		})
	}
}

// A custom-typed attribute is not probed with "", because "" is not its zero.
// The first one the check met -- port_profile's dot1x_idle_timeout, a
// timetypes.GoDuration -- was reported as wanting NullZero because its duration
// validators reject "" as unparseable, which would have nulled a legitimate
// zero duration.
func TestACustomTypedAttributeIsNotProbedWithTheEmptyString(t *testing.T) {
	custom := schema.StringAttribute{
		Optional:   true,
		Computed:   true,
		CustomType: timetypes.GoDurationType{},
		Validators: []validator.String{stringvalidator.LengthAtLeast(1)},
	}
	if zeroIsRejected(custom) {
		t.Error("a custom-typed attribute was probed with \"\", so its zero was judged by " +
			"whether the empty string parses rather than whether zero is legal")
	}
	// The control: the identical attribute WITHOUT a custom type is probed and
	// rejected, so the case above is not passing because the validator is inert.
	plain := custom
	plain.CustomType = nil
	if !zeroIsRejected(plain) {
		t.Fatal("the validator does not reject \"\" at all, so the assertion above proves nothing")
	}
}
