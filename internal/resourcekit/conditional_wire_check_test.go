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
	Always string   `json:"always"`
	Maybe  string   `json:"maybe"`
	List   []string `json:"list,omitempty"`
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

// A SLICE THE Encode NEVER TOUCHES MUST READ AS UNWRITTEN, and it did not.
//
// fillSentinel switched on Kind over strings, bools, numbers and pointers to
// those, and had no case for a slice. So a slice field stayed nil in the
// sentinel struct exactly as in the zero one, both marshalled the same, and
// "equal across the two runs" -- the definition of written -- reported every
// untouched slice as written.
//
// THE DIRECTION IS WHY THIS IS WORSE THAN A GAP. The check then tells the author
// that Encode writes the wire and their predicate denies it, and an author who
// believes it declares the predicate always true. That masks the name when
// nothing wrote it, and under a would-emit narrowing go-unifi sends the empty
// list over whatever the controller holds. A silent check would have been safer
// than one arguing for the blanking.
//
// network's dhcp_relay_servers is where it surfaced: vpn_client's seven
// conditional wires are all strings and pointers, so nothing had exercised a
// slice through this.
func TestConditionalWireProblemsSeesAnUntouchedSlice(t *testing.T) {
	// The predicate is truthful: the list is written exactly when `want` is set.
	truthful := func(o types.Object) bool {
		want, ok := o.Attributes()["want"].(types.Bool)
		return ok && want.ValueBool()
	}
	field := condField(truthful)
	field.Wires = []string{"always", "list"}
	field.ConditionalWires = map[string]func(types.Object) bool{"list": truthful}
	field.Encode = func(_ context.Context, object types.Object, sdk *condSDK) diag.Diagnostics {
		sdk.Always = "written"
		want, ok := object.Attributes()["want"].(types.Bool)
		if ok && want.ValueBool() {
			sdk.List = []string{"written"}
		}
		return nil
	}
	if problems := ConditionalWireProblems(field, condObjects(t), nil); problems != nil {
		t.Errorf("a truthful predicate over a slice wire reported %v", problems)
	}
}

// A READ-ONLY WIRE STAYS OFF THE MASK AND OUT OF THE REPORT.
//
// vpn_server's wireguard.public_key is the shape: unifi.Network carries the tag,
// marshalUserVPN does not emit it, so masking it makes go-unifi refuse the whole
// update. It is not conditional -- it is unwritable -- and none of Fields,
// AlwaysWire or a never-true predicate can say that.
func TestReadOnlyWireStaysOffTheMask(t *testing.T) {
	field := condField(nil)
	field.ConditionalWires = nil
	field.Wires = []string{"always", "maybe"}
	field.ReadOnlyWires = []string{"maybe"}
	field.Encode = func(_ context.Context, _ types.Object, sdk *condSDK) diag.Diagnostics {
		sdk.Always = "written"
		return nil
	}
	if problems := ConditionalWireProblems(field, condObjects(t), nil); problems != nil {
		t.Errorf("a read-only wire nothing encodes reported %v", problems)
	}
	plan := &condModel{Object: condObject(t, true)}
	got := field.maskedWireNames(plan)
	if len(got) != 1 || got[0] != "always" {
		t.Errorf("mask = %v, want only [always]", got)
	}
}

// A name that is not one of Wires keeps nothing off the mask.
func TestReadOnlyWireNotInWiresIsReported(t *testing.T) {
	field := condField(nil)
	field.ConditionalWires = nil
	field.Wires = []string{"always"}
	field.ReadOnlyWires = []string{"typo"}
	problems := ConditionalWireProblems(field, condObjects(t), nil)
	if len(problems) != 1 || !strings.Contains(problems[0], "names nothing") {
		t.Errorf("a misspelled read-only wire reported %v", problems)
	}
}
