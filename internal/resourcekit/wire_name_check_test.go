package resourcekit

import (
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
)

type wireModel struct {
	Key  types.String `tfsdk:"name"`
	Note types.String `tfsdk:"note"`
}

type wireSDK struct {
	// The Terraform name is `name` and the controller calls it `key`, which is
	// the real dns_record case and the reason a descriptor cannot just reuse
	// the schema's names.
	Key      string `json:"key"`
	Note     string `json:"note,omitempty"`
	internal string //nolint:unused // present to prove unexported fields are skipped
}

func wireSpec(keyWire, noteWire string) Spec[wireModel, wireSDK] {
	return Spec[wireModel, wireSDK]{
		TypeName: "wire",
		Fields: []Field[wireModel, wireSDK]{
			StringField[wireModel, wireSDK]{
				Wire:  keyWire,
				Model: func(m *wireModel) *types.String { return &m.Key },
				SDK:   func(s *wireSDK) *string { return &s.Key },
			},
			StringField[wireModel, wireSDK]{
				Wire:  noteWire,
				Model: func(m *wireModel) *types.String { return &m.Note },
				SDK:   func(s *wireSDK) *string { return &s.Note },
			},
		},
	}
}

// The control. Without it every must-fail case below would pass for a check
// that reports every field.
func TestCorrectWireNamesProduceNoProblems(t *testing.T) {
	if problems := WireNameProblems(wireSpec("key", "note")); len(problems) != 0 {
		t.Fatalf("correct wire names were reported: %v", problems)
	}
}

func TestAWrongWireNameIsReportedWithBothNames(t *testing.T) {
	// "name" is the Terraform spelling, which is exactly the mistake this
	// catches: it looks right and the controller has no such attribute.
	problems := WireNameProblems(wireSpec("name", "note"))
	if len(problems) != 1 {
		t.Fatalf("want exactly 1 problem, got %d: %v", len(problems), problems)
	}
	if !strings.Contains(problems[0], `"name"`) || !strings.Contains(problems[0], `"key"`) {
		t.Errorf("the report must name what was written and what the SDK calls it: %q", problems[0])
	}
}

func TestTheOmitemptySuffixIsStripped(t *testing.T) {
	if problems := WireNameProblems(wireSpec("key", "note")); len(problems) != 0 {
		t.Fatalf("a field tagged omitempty was reported: %v", problems)
	}
	problems := WireNameProblems(wireSpec("key", "note,omitempty"))
	if len(problems) != 1 {
		t.Fatalf("writing the tag verbatim, suffix and all, was accepted: %v", problems)
	}
}

type untaggedSDK struct {
	Loose string
}

func TestAFieldWithNoJSONTagIsReported(t *testing.T) {
	spec := Spec[wireModel, untaggedSDK]{
		TypeName: "untagged",
		Fields: []Field[wireModel, untaggedSDK]{
			StringField[wireModel, untaggedSDK]{
				Wire:  "loose",
				Model: func(m *wireModel) *types.String { return &m.Key },
				SDK:   func(s *untaggedSDK) *string { return &s.Loose },
			},
		},
	}
	problems := WireNameProblems(spec)
	if len(problems) != 1 || !strings.Contains(problems[0], "no json tag") {
		t.Fatalf("an untagged SDK field was not reported: %v", problems)
	}
}

func TestAlwaysWireNamesAreCheckedAgainstTheSDK(t *testing.T) {
	spec := wireSpec("key", "note")
	spec.AlwaysWire = []string{"key"}
	if problems := WireNameProblems(spec); len(problems) != 0 {
		t.Fatalf("a real json field named in AlwaysWire was reported: %v", problems)
	}

	spec.AlwaysWire = []string{"no_such_field"}
	problems := WireNameProblems(spec)
	if len(problems) != 1 {
		t.Fatalf("want exactly 1 problem, got %d: %v", len(problems), problems)
	}
	if !strings.Contains(problems[0], "no_such_field") ||
		!strings.Contains(problems[0], "AlwaysWire") {
		t.Errorf("the report must name the bad entry and where it came from: %q", problems[0])
	}
}
