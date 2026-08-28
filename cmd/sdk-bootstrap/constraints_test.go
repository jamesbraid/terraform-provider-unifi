package main

import (
	"go/token"
	"go/types"
	"reflect"
	"testing"

	unifi "github.com/ubiquiti-community/go-unifi/unifi"
)

// stubLookup builds a constraintLookup over a plain table, standing in for
// the real merged unifi/settings tables in these tests.
func stubLookup(table map[string]map[string]unifi.FieldConstraint) constraintLookup {
	return func(goType, wire string) (unifi.FieldConstraint, bool) {
		byWire, ok := table[goType]
		if !ok {
			return unifi.FieldConstraint{}, false
		}
		constraint, ok := byWire[wire]
		return constraint, ok
	}
}

// Test_walkCopiesAConstraintPresentInTheStubTable pins the case a (type,
// wire) pair the table knows about gets its constraint copied verbatim.
func Test_walkCopiesAConstraintPresentInTheStubTable(t *testing.T) {
	outer := types.NewStruct(
		[]*types.Var{tagged("Mode", types.String)},
		[]string{`json:"mode"`},
	)
	want := unifi.FieldConstraint{Pattern: "a|b", Values: []string{"a", "b"}}
	lookup := stubLookup(map[string]map[string]unifi.FieldConstraint{
		"Outer": {"mode": want},
	})

	got := walk(outer, "Outer", lookup)

	if len(got) != 1 || got[0].Constraint == nil {
		t.Fatalf("walk()[0].Constraint = nil, want a copy of %+v", want)
	}
	if got[0].Constraint.Pattern != want.Pattern || !reflect.DeepEqual(got[0].Constraint.Values, want.Values) {
		t.Errorf("walk()[0].Constraint = %+v, want pattern/values from %+v", got[0].Constraint, want)
	}
}

// Test_walkLeavesConstraintUnsetWhenThePairIsAbsent pins the other half: a
// field whose (type, wire) pair the table doesn't know gets no constraint.
func Test_walkLeavesConstraintUnsetWhenThePairIsAbsent(t *testing.T) {
	outer := types.NewStruct(
		[]*types.Var{tagged("Mode", types.String)},
		[]string{`json:"mode"`},
	)
	lookup := stubLookup(map[string]map[string]unifi.FieldConstraint{
		"Outer": {"other": {Pattern: "x"}},
	})

	got := walk(outer, "Outer", lookup)

	if len(got) != 1 || got[0].Constraint != nil {
		t.Fatalf("walk()[0].Constraint = %+v, want nil: %q is not in the stub table", got[0].Constraint, "mode")
	}
}

// Test_walkResolvesANestedMemberThroughItsOwnTypeName pins that a member
// which is itself a struct is looked up under ITS OWN Go type name, not the
// name of the struct that contains it -- FieldConstraints is keyed per
// declaring struct.
func Test_walkResolvesANestedMemberThroughItsOwnTypeName(t *testing.T) {
	inner := types.NewStruct(
		[]*types.Var{tagged("Width", types.Int64)},
		[]string{`json:"width"`},
	)
	outer := types.NewStruct(
		[]*types.Var{
			types.NewField(token.NoPos, nil, "Inner", named("Inner", inner), true),
		},
		[]string{`json:"inner"`},
	)
	want := unifi.FieldConstraint{Int64Values: []int64{20, 40}}
	lookup := stubLookup(map[string]map[string]unifi.FieldConstraint{
		"Inner": {"width": want},
	})

	got := walk(outer, "Outer", lookup)

	var innerField field
	for _, f := range got {
		if f.Name == "inner" {
			innerField = f
		}
	}
	if len(innerField.Fields) != 1 || innerField.Fields[0].Constraint == nil {
		t.Fatalf("inner.Fields[0].Constraint = nil, want a copy of %+v looked up via \"Inner\", not \"Outer\"", want)
	}
	if !reflect.DeepEqual(innerField.Fields[0].Constraint.Int64Values, want.Int64Values) {
		t.Errorf("inner.Fields[0].Constraint.Int64Values = %v, want %v", innerField.Fields[0].Constraint.Int64Values, want.Int64Values)
	}
}
