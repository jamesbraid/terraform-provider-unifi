package resourcekit

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
)

type overrideModel struct {
	A types.String `tfsdk:"a"`
	B types.String `tfsdk:"b"`
	C types.String `tfsdk:"c"`
}

type overrideSDK struct {
	A string `json:"a"`
	B string `json:"b"`
	C string `json:"c"`
}

func overrideStringField(wire string) Field[overrideModel, overrideSDK] {
	return StringField[overrideModel, overrideSDK]{
		Wire:  wire,
		Model: func(m *overrideModel) *types.String { return &m.A },
		SDK:   func(s *overrideSDK) *string { return &s.A },
	}
}

func overrideWires(fields []Field[overrideModel, overrideSDK]) []string {
	out := make([]string, 0, len(fields))
	for _, field := range fields {
		out = append(out, fieldWireNames(field)...)
	}
	return out
}

func TestOverrideReplacesByWireAndAppendsTheRest(t *testing.T) {
	generated := []Field[overrideModel, overrideSDK]{
		overrideStringField("a"), overrideStringField("b"),
	}
	overrides := []Field[overrideModel, overrideSDK]{
		StringField[overrideModel, overrideSDK]{
			Wire:      "b",
			Model:     func(m *overrideModel) *types.String { return &m.B },
			SDK:       func(s *overrideSDK) *string { return &s.B },
			WriteWhen: func(*overrideModel) bool { return false },
		},
		overrideStringField("c"),
	}
	got := overrideWires(Override(generated, overrides))
	want := []string{"a", "b", "c"}
	if len(got) != len(want) {
		t.Fatalf("wires = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("wires = %v, want %v", got, want)
		}
	}
	// The override, not the generated original, must be what survives: its
	// WriteWhen suppresses the write, so a swap is observable here.
	var model overrideModel
	model.B = types.StringValue("set")
	if Override(generated, overrides)[1].SetInPlan(&model) {
		t.Fatal("the surviving b entry is the generated one; the override's WriteWhen was lost")
	}
}

func TestOverrideByAScatteredFieldDisplacesEveryFlatEntryItAbsorbs(t *testing.T) {
	generated := []Field[overrideModel, overrideSDK]{
		overrideStringField("a"), overrideStringField("b"), overrideStringField("c"),
	}
	overrides := []Field[overrideModel, overrideSDK]{
		ScatteredObjectField[overrideModel, overrideSDK]{
			Wires: []string{"a", "c"},
		},
	}
	got := overrideWires(Override(generated, overrides))
	want := []string{"b", "a", "c"}
	if len(got) != len(want) {
		t.Fatalf("wires = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("wires = %v, want %v", got, want)
		}
	}
}
