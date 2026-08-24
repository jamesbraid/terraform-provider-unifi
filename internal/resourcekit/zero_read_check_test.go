package resourcekit

import (
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework-nettypes/iptypes"
)

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

func TestZeroReadProblemsIsSilentOnAPlainSpec(t *testing.T) {
	if problems := ZeroReadProblems(probeSpec(KeepZero, NullZero, KeepZero), probeSchema()); len(problems) != 0 {
		t.Errorf("a plain string spec has nothing to refuse: %v; the check "+
			"must not fire on a shape it has no claim about", problems)
	}
}
