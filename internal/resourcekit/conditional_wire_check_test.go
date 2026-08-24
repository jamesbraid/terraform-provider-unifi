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
		Decode: func(context.Context, *condSDK, types.Object) (types.Object, diag.Diagnostics) {
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

func TestConditionalWireProblemsReportsAnUndeclaredConditionalWire(t *testing.T) {
	plain := condField(nil)
	plain.ConditionalWires = nil
	problems := ConditionalWireProblems(plain, condObjects(t), nil)
	if len(problems) != 1 || !strings.Contains(problems[0], `not in ConditionalWires`) {
		t.Errorf("an undeclared conditional wire reported %v", problems)
	}
}

func TestConditionalWireProblemsIsSilentWhenNothingIsConditional(t *testing.T) {
	unconditional := condField(nil)
	unconditional.ConditionalWires = nil
	unconditional.Wires = []string{"always"}
	if problems := ConditionalWireProblems(unconditional, condObjects(t), nil); problems != nil {
		t.Errorf("a field whose wires are all unconditional reported %v; a check "+
			"that always reports everything would also pass the undeclared-wire "+
			"test above", problems)
	}
}

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

func TestReadOnlyWireThatEncodeWritesIsReported(t *testing.T) {
	field := condField(nil)
	field.ConditionalWires = nil
	field.Wires = []string{"always", "maybe"}
	field.ReadOnlyWires = []string{"always"} // and Encode writes `always`
	problems := ConditionalWireProblems(field, condObjects(t), nil)
	if len(problems) == 0 {
		t.Fatal("a read-only wire Encode writes reported nothing")
	}
	if !strings.Contains(problems[0], "Encode writes it") {
		t.Errorf("problem = %q, want it to say Encode writes the read-only wire", problems[0])
	}
}

type hiddenSDK struct {
	Always string `json:"always"`
	// Hidden is a real field the encoder below drops, modeling vpn_server's
	// wireguard_public_key: the controller issues it but the encoder never
	// emits it.
	Hidden string `json:"hidden"`
}

func (h hiddenSDK) MarshalJSON() ([]byte, error) {
	return []byte(`{"always":` + `"` + h.Always + `"}`), nil
}

func TestAWireTheEncoderNeverEmitsReadsAsNotWritten(t *testing.T) {
	field := ScatteredObjectField[condModel, hiddenSDK]{
		Wires:         []string{"always", "hidden"},
		ReadOnlyWires: []string{"hidden"},
		Model:         func(m *condModel) *types.Object { return &m.Object },
		AttrTypes:     condAttrs,
		Encode: func(_ context.Context, _ types.Object, sdk *hiddenSDK) diag.Diagnostics {
			sdk.Always = "written"
			return nil
		},
		Decode: func(context.Context, *hiddenSDK, types.Object) (types.Object, diag.Diagnostics) {
			return types.ObjectNull(condAttrs), nil
		},
	}
	if problems := ConditionalWireProblems(field, condObjects(t), nil); problems != nil {
		t.Errorf("a read-only wire the encoder never emits reported %v; the encoded form "+
			"cannot represent \"not written\" and the struct can", problems)
	}
	field.Encode = func(_ context.Context, _ types.Object, sdk *hiddenSDK) diag.Diagnostics {
		sdk.Always = "written"
		sdk.Hidden = "written"
		return nil
	}
	if problems := ConditionalWireProblems(field, condObjects(t), nil); len(problems) == 0 {
		t.Error("the same wire, now written by Encode, reported nothing; " +
			"the pass above would be vacuous otherwise")
	}
}

type opaqueSDK struct {
	Always string    `json:"always"`
	Opaque *struct{} `json:"opaque"`
}

func TestAWireTheProbesCannotDistinguishIsRefused(t *testing.T) {
	field := ScatteredObjectField[condModel, opaqueSDK]{
		Wires:     []string{"always", "opaque"},
		Model:     func(m *condModel) *types.Object { return &m.Object },
		AttrTypes: condAttrs,
		Encode: func(_ context.Context, _ types.Object, sdk *opaqueSDK) diag.Diagnostics {
			sdk.Always = "written"
			return nil
		},
		Decode: func(context.Context, *opaqueSDK, types.Object) (types.Object, diag.Diagnostics) {
			return types.ObjectNull(condAttrs), nil
		},
		ConditionalWires: map[string]func(types.Object) bool{
			"opaque": func(types.Object) bool { return false },
		},
	}
	problems := ConditionalWireProblems(field, condObjects(t), nil)
	if len(problems) == 0 {
		t.Fatal("a wire the probes cannot tell apart reported nothing")
	}
	if !strings.Contains(problems[0], "indistinguishable") {
		t.Errorf("problem = %q, want it to say a write and a skip cannot be told apart",
			problems[0])
	}
}
