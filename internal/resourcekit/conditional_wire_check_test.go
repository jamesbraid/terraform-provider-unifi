package resourcekit

import (
	"context"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

type condSDK struct {
	Always string `json:"always"`
	Maybe  string `json:"maybe"`
}

type condModel struct{ Object types.Object }

var condAttrs = map[string]attr.Type{"want": types.BoolType}

func condObject(t *testing.T, want bool) types.Object {
	t.Helper()
	object, diags := types.ObjectValue(condAttrs, map[string]attr.Value{
		"want": types.BoolValue(want),
	})
	if diags.HasError() {
		t.Fatal(diags)
	}
	return object
}

// condField writes `maybe` only when the object says so; the predicate is
// supplied by the caller so it can be made to lie in either direction.
func condField(predicate func(types.Object) bool) ScatteredObjectField[condModel, condSDK] {
	return ScatteredObjectField[condModel, condSDK]{
		Wires:     []string{"always", "maybe"},
		Model:     func(m *condModel) *types.Object { return &m.Object },
		AttrTypes: condAttrs,
		Encode: func(_ context.Context, object types.Object, sdk *condSDK) diag.Diagnostics {
			sdk.Always = "written"
			want, ok := object.Attributes()["want"].(types.Bool)
			if ok && want.ValueBool() {
				sdk.Maybe = "written"
			}
			return nil
		},
		Decode: func(context.Context, *condSDK) (types.Object, diag.Diagnostics) {
			return types.ObjectNull(condAttrs), nil
		},
		ConditionalWires: map[string]func(types.Object) bool{"maybe": predicate},
	}
}

func condObjects(t *testing.T) []types.Object {
	return []types.Object{condObject(t, true), condObject(t, false)}
}

func TestConditionalWireProblemsPassesWhenTheyAgree(t *testing.T) {
	truthful := func(o types.Object) bool {
		want, ok := o.Attributes()["want"].(types.Bool)
		return ok && want.ValueBool()
	}
	if problems := ConditionalWireProblems(condField(truthful), condObjects(t), nil); len(problems) != 0 {
		t.Errorf("an honest predicate was reported: %v", problems)
	}
}

// THE DESTRUCTIVE DIRECTION. A predicate claiming a write Encode does not make
// puts the name in the mask, and go-unifi sends its zero.
func TestConditionalWireProblemsCatchesAPredicateThatOverclaims(t *testing.T) {
	problems := ConditionalWireProblems(
		condField(func(types.Object) bool { return true }), condObjects(t), nil)
	if len(problems) == 0 {
		t.Fatal("a predicate that always claims a write was not reported")
	}
	if !strings.Contains(problems[0], "sends its zero") {
		t.Errorf("the message does not name the consequence: %s", problems[0])
	}
}

// THE OTHER DIRECTION, which is a no-op rather than destruction and still wrong:
// the value is written and never sent.
func TestConditionalWireProblemsCatchesAPredicateThatUnderclaims(t *testing.T) {
	problems := ConditionalWireProblems(
		condField(func(types.Object) bool { return false }), condObjects(t), nil)
	if len(problems) == 0 {
		t.Fatal("a predicate that always denies a write was not reported")
	}
	if !strings.Contains(strings.Join(problems, " "), "never sent") {
		t.Errorf("the message does not name the consequence: %v", problems)
	}
}

// A FALSE-ONLY RUN PASSES FOR A PREDICATE THAT ALWAYS RETURNS FALSE, which is
// why the floor exists: it reports the direction no object exercised rather than
// reporting nothing.
func TestConditionalWireProblemsReportsAnUnexercisedDirection(t *testing.T) {
	truthful := func(o types.Object) bool {
		want, ok := o.Attributes()["want"].(types.Bool)
		return ok && want.ValueBool()
	}
	only := []types.Object{condObject(t, false)}
	problems := ConditionalWireProblems(condField(truthful), only, nil)
	if len(problems) != 1 || !strings.Contains(problems[0], "written direction is unexercised") {
		t.Errorf("a run exercising one direction reported %v", problems)
	}
}

// THIS TEST ASSERTED THE BLINDNESS IT WAS NAMED AFTER. It handed the check a
// field whose Encode writes "maybe" conditionally, removed the declaration, and
// required silence -- which is exactly the case that leaves the wire on the mask
// with nothing behind it. The check returned early on an empty declaration and
// so agreed.
//
// It now asserts the opposite, on the same fixture, because the fixture was
// always the destructive shape.
func TestConditionalWireProblemsReportsAnUndeclaredConditionalWire(t *testing.T) {
	plain := condField(nil)
	plain.ConditionalWires = nil
	problems := ConditionalWireProblems(plain, condObjects(t), nil)
	if len(problems) != 1 || !strings.Contains(problems[0], `not in ConditionalWires`) {
		t.Errorf("an undeclared conditional wire reported %v", problems)
	}
}

// The silence that IS correct: every wire the field declares is written on every
// path, so there is nothing to declare and nothing to report. Without this the
// change above would be satisfied by a check that reports everything.
func TestConditionalWireProblemsIsSilentWhenNothingIsConditional(t *testing.T) {
	unconditional := condField(nil)
	unconditional.ConditionalWires = nil
	unconditional.Wires = []string{"always"}
	if problems := ConditionalWireProblems(unconditional, condObjects(t), nil); problems != nil {
		t.Errorf("a field whose wires are all unconditional reported %v", problems)
	}
}
