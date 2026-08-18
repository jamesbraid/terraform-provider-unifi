package resourcekit

import (
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
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
