package resourcekit

import (
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework-nettypes/iptypes"
)

// THE CHECK'S OWN POSITIVE CONTROLS, in the same spirit as the elide file's:
// each way a zero read can go wrong is built deliberately and the check must
// name it, or a clean run over the shipped descriptors proves nothing.

// A field whose ToModel cannot run at all. device's mac shipped without a New,
// which no unit check exercised and every acceptance read tripped over: the
// nil closure is only called when a value comes back.
func TestZeroReadProblemsCatchesAFieldWhoseToModelPanics(t *testing.T) {
	spec := customProbeSpec(NullZero)
	field, ok := spec.Fields[0].(StringLikeField[customProbeModel, customProbeSDK, iptypes.IPv4Address])
	if !ok {
		t.Fatalf("the probe spec no longer leads with its StringLikeField: %T", spec.Fields[0])
	}
	field.New = nil
	spec.Fields[0] = field

	problems := ZeroReadProblems(spec, customProbeSchema(false))
	if len(problems) != 1 {
		t.Fatalf("a field with a nil New produced %d problem(s), want 1: %v", len(problems), problems)
	}
	if !strings.Contains(problems[0], "panic") || !strings.Contains(problems[0], "addr") {
		t.Errorf("the problem does not say the field panics or which field it is: %q", problems[0])
	}
}

// A zero read that lands a value the model's own type refuses. client's
// fixed_ip shipped on KeepZero, so an unset controller value became
// IPv4Address(""), and the type failed every read that carried it.
func TestZeroReadProblemsCatchesAValueTheTypeRefuses(t *testing.T) {
	problems := ZeroReadProblems(customProbeSpec(KeepZero), customProbeSchema(false))
	if len(problems) != 1 {
		t.Fatalf("a kept zero the type refuses produced %d problem(s), want 1: %v",
			len(problems), problems)
	}
	if !strings.Contains(problems[0], "addr") {
		t.Errorf("the problem does not name the attribute: %q", problems[0])
	}

	if problems := ZeroReadProblems(customProbeSpec(NullZero), customProbeSchema(false)); len(problems) != 0 {
		t.Errorf("a zero elided to null should be clean: %v", problems)
	}
}

// A Required attribute is exempt from the value check -- a real read always
// carries it, so its zero-read value is unreachable -- but NOT from the panic
// check, which needs no controller to fire.
func TestZeroReadProblemsSkipsRequiredValuesButNotPanics(t *testing.T) {
	if problems := ZeroReadProblems(customProbeSpec(KeepZero), customProbeSchema(true)); len(problems) != 0 {
		t.Errorf("a Required attribute's unreachable zero was reported: %v", problems)
	}

	spec := customProbeSpec(NullZero)
	field, ok := spec.Fields[0].(StringLikeField[customProbeModel, customProbeSDK, iptypes.IPv4Address])
	if !ok {
		t.Fatalf("the probe spec no longer leads with its StringLikeField: %T", spec.Fields[0])
	}
	field.New = nil
	spec.Fields[0] = field
	if problems := ZeroReadProblems(spec, customProbeSchema(true)); len(problems) != 1 {
		t.Errorf("a panic on a Required field went unreported: %v", problems)
	}
}

// The scattered kinds and the plain kinds must both survive the walk -- the
// probe spec exercises only StringLikeField, so this control runs the check
// over a spec with no custom types at all and expects silence, proving the
// check does not fire on shapes it has no claim about.
func TestZeroReadProblemsIsSilentOnAPlainSpec(t *testing.T) {
	if problems := ZeroReadProblems(probeSpec(KeepZero, NullZero, KeepZero), probeSchema()); len(problems) != 0 {
		t.Errorf("a plain string spec has nothing to refuse: %v", problems)
	}
}
