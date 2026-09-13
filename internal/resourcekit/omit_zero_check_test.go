package resourcekit

import (
	"context"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
)

type omitZeroProbeModel struct {
	Value types.Int64 `tfsdk:"value"`
}

type omitZeroProbeSDK struct {
	Value *int64
}

func omitZeroProbeField(omit bool) Int64PtrField[omitZeroProbeModel, omitZeroProbeSDK] {
	return Int64PtrField[omitZeroProbeModel, omitZeroProbeSDK]{
		Wire:     "value",
		Model:    func(m *omitZeroProbeModel) *types.Int64 { return &m.Value },
		SDK:      func(s *omitZeroProbeSDK) **int64 { return &s.Value },
		OmitZero: omit,
	}
}

func omitZeroProbeSpec(omit bool) Spec[omitZeroProbeModel, omitZeroProbeSDK] {
	return Spec[omitZeroProbeModel, omitZeroProbeSDK]{
		TypeName: "probe_omit_zero",
		Fields:   []Field[omitZeroProbeModel, omitZeroProbeSDK]{omitZeroProbeField(omit)},
	}
}

// The positive control the census depends on: a synthetic spec whose
// constraint pattern rejects "0" and whose field sets no OmitZero must be
// flagged by name, or the real walk over every kit surface proves nothing.
func TestOmitZeroProblemsFlagsAZeroRejectingPatternWithNoOmitZero(t *testing.T) {
	constraints := map[string]ui.FieldConstraint{
		// No |^$ arm, same shape as wlan's roaming_assistant_*_rssi: 0 is
		// rejected and there is no empty-string escape hatch either.
		"value": {Pattern: `^[1-9][0-9]*$`},
	}
	problems := OmitZeroProblems(omitZeroProbeSpec(false), constraints, nil)
	if len(problems) != 1 {
		t.Fatalf("want exactly 1 problem, got %d: %v", len(problems), problems)
	}
	if !strings.Contains(problems[0], "probe_omit_zero.value") {
		t.Errorf("problem does not name the surface and field: %q", problems[0])
	}
}

func TestOmitZeroProblemsIsSilentOnceOmitZeroIsSet(t *testing.T) {
	constraints := map[string]ui.FieldConstraint{
		"value": {Pattern: `^[1-9][0-9]*$`},
	}
	if problems := OmitZeroProblems(omitZeroProbeSpec(true), constraints, nil); len(problems) != 0 {
		t.Fatalf("a field that already sets OmitZero was flagged anyway: %v", problems)
	}
}

// The other half of the control: a pattern that DOES permit zero must never
// be flagged, whether or not OmitZero is set -- omitting a legal value would
// be its own bug.
func TestOmitZeroProblemsLeavesALegalZeroAlone(t *testing.T) {
	constraints := map[string]ui.FieldConstraint{
		"value": {Pattern: `^[0-9]+$`},
	}
	for _, omit := range []bool{false, true} {
		if problems := OmitZeroProblems(omitZeroProbeSpec(omit), constraints, nil); len(problems) != 0 {
			t.Errorf("omit=%v: a pattern that permits zero was flagged: %v", omit, problems)
		}
	}
}

func TestOmitZeroProblemsSkipsAFieldWithNoConstraintEntry(t *testing.T) {
	if problems := OmitZeroProblems(omitZeroProbeSpec(false), map[string]ui.FieldConstraint{}, nil); len(problems) != 0 {
		t.Fatalf("a field absent from the constraint table was flagged anyway: %v", problems)
	}
}

// A read-only field's ToSDK never writes anything at all ("the field never
// reaches the controller" -- see readOnlyField's own doc comment), so a
// zero-rejecting pattern on one is not a hazard: there is no wire path for
// OmitZero to protect. Caught the census's own first bug
// (firewall_policy.index, unwrapped and flagged as a false positive before
// this fix).
func TestOmitZeroProblemsSkipsAReadOnlyField(t *testing.T) {
	constraints := map[string]ui.FieldConstraint{
		"value": {Pattern: `^[1-9][0-9]*$`},
	}
	spec := omitZeroProbeSpec(false)
	spec.Fields = []Field[omitZeroProbeModel, omitZeroProbeSDK]{
		ReadOnly[omitZeroProbeModel, omitZeroProbeSDK](omitZeroProbeField(false)),
	}
	if problems := OmitZeroProblems(spec, constraints, nil); len(problems) != 0 {
		t.Fatalf("a read-only field was flagged even though it never writes: %v", problems)
	}
}

// dtim_6e's real pattern has a |^$ empty-string arm and is still correctly
// flagged when OmitZero is unset: the empty arm only says an empty STRING is
// legal, and OmitZero omits the field from the wire entirely rather than
// sending "", so its presence doesn't change the verdict.
func TestOmitZeroProblemsFlagsAPatternWithAnEmptyStringArmToo(t *testing.T) {
	constraints := map[string]ui.FieldConstraint{
		"value": {Pattern: `^([1-9]|[1-9][0-9]|1[0-9]{2}|2[0-4][0-9]|25[0-5])$|^$`},
	}
	if problems := OmitZeroProblems(omitZeroProbeSpec(false), constraints, nil); len(problems) != 1 {
		t.Fatalf("want exactly 1 problem, got %d: %v", len(problems), problems)
	}
}

// nestedProbeElem is one element of a nested block. It carries three
// pointer-to-integer members so one test exercises both halves of the
// descent's verdict at once: port's pattern rejects zero (must be flagged),
// weight's admits it (must not be), and untracked has no constraint entry at
// all (must not be).
type nestedProbeElem struct {
	Port      *int64 `json:"port"`
	Weight    *int64 `json:"weight"`
	Untracked *int64 `json:"untracked"`
	Label     string `json:"label"`
}

type nestedProbeSDK struct {
	Servers []nestedProbeElem
}

type nestedProbeModel struct {
	Servers types.List `tfsdk:"servers"`
}

func nestedProbeElemAttrTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"port":      types.Int64Type,
		"weight":    types.Int64Type,
		"untracked": types.Int64Type,
		"label":     types.StringType,
	}
}

func nestedProbeSpec() Spec[nestedProbeModel, nestedProbeSDK] {
	return Spec[nestedProbeModel, nestedProbeSDK]{
		TypeName: "nested_probe",
		Fields: []Field[nestedProbeModel, nestedProbeSDK]{
			ObjectListField[nestedProbeModel, nestedProbeSDK, nestedProbeElem]{
				Wire:      "servers",
				Model:     func(m *nestedProbeModel) *types.List { return &m.Servers },
				SDK:       func(s *nestedProbeSDK) *[]nestedProbeElem { return &s.Servers },
				AttrTypes: nestedProbeElemAttrTypes(),
				Encode: func(context.Context, types.Object) (nestedProbeElem, diag.Diagnostics) {
					return nestedProbeElem{}, nil
				},
				Decode: func(context.Context, nestedProbeElem) (types.Object, diag.Diagnostics) {
					return types.ObjectNull(nestedProbeElemAttrTypes()), nil
				},
			},
		},
	}
}

func nestedProbeConstraints() map[string]map[string]ui.FieldConstraint {
	return map[string]map[string]ui.FieldConstraint{
		// The top-level struct carries no Int64PtrField of its own, so the
		// only hits can come from the descent into nestedProbeElem.
		"nestedProbeElem": {
			"port":   {Pattern: `^[1-9][0-9]{0,4}$|^$`}, // rejects 0, admits "" -- radius port's shape
			"weight": {Pattern: `^[0-9]+$`},             // admits 0
			// "untracked" is intentionally absent: a member with no
			// constraint entry has no pattern to reject a zero.
		},
	}
}

// The descent's positive control: a pointer-to-integer member of a nested
// element type whose pattern rejects "0" and which, being encoded element by
// element, carries no OmitZero, must be flagged by name -- one level down from
// the top-level walk.
func TestOmitZeroProblemsDescendsIntoANestedZeroRejectingMember(t *testing.T) {
	problems := OmitZeroProblems(nestedProbeSpec(), nil, nestedProbeConstraints())
	if len(problems) != 1 {
		t.Fatalf("want exactly 1 problem, got %d: %v", len(problems), problems)
	}
	if !strings.Contains(problems[0], "nested_probe.servers.port") {
		t.Errorf("problem does not name the outer wire and nested member: %q", problems[0])
	}
}

// The descent's negative control: a nested member whose pattern admits zero
// (weight) and one absent from the constraint table (untracked) are both left
// alone -- flagging either would omit a legal value or invent a constraint.
// TestOmitZeroProblemsDescendsIntoANestedZeroRejectingMember already asserts
// exactly one hit; this pins the two silences by name so a regression that
// widened the net would fail here rather than there.
func TestOmitZeroProblemsLeavesLegalAndUnconstrainedNestedMembersAlone(t *testing.T) {
	for _, problem := range OmitZeroProblems(nestedProbeSpec(), nil, nestedProbeConstraints()) {
		if strings.Contains(problem, "weight") {
			t.Errorf("a nested member whose pattern admits zero was flagged: %q", problem)
		}
		if strings.Contains(problem, "untracked") {
			t.Errorf("a nested member with no constraint entry was flagged: %q", problem)
		}
	}
}
