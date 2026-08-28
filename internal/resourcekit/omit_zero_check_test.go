package resourcekit

import (
	"strings"
	"testing"

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
	problems := OmitZeroProblems(omitZeroProbeSpec(false), constraints)
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
	if problems := OmitZeroProblems(omitZeroProbeSpec(true), constraints); len(problems) != 0 {
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
		if problems := OmitZeroProblems(
			omitZeroProbeSpec(omit), constraints,
		); len(problems) != 0 {
			t.Errorf("omit=%v: a pattern that permits zero was flagged: %v", omit, problems)
		}
	}
}

func TestOmitZeroProblemsSkipsAFieldWithNoConstraintEntry(t *testing.T) {
	if problems := OmitZeroProblems(
		omitZeroProbeSpec(false), map[string]ui.FieldConstraint{},
	); len(problems) != 0 {
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
	if problems := OmitZeroProblems(spec, constraints); len(problems) != 0 {
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
	if problems := OmitZeroProblems(omitZeroProbeSpec(false), constraints); len(problems) != 1 {
		t.Fatalf("want exactly 1 problem, got %d: %v", len(problems), problems)
	}
}
