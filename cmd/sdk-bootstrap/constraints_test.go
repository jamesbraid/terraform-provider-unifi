package main

import (
	"go/token"
	"go/types"
	"reflect"
	"testing"

	unifi "github.com/ubiquiti-community/go-unifi/unifi"
)

// Test_sdkConstraintsResolvesATopLevelSettingsType pins that production's
// lookup finds the SDK's own constraint for a top-level settings field.
// settings.FieldConstraints keys every entry with a "Setting" prefix
// ("SettingGlobalSwitch"), but the emitted Go struct is named "GlobalSwitch"
// -- the prefix is stripped when the type is generated. A lookup keyed by
// the bare Go type name therefore misses the table entirely, for every
// top-level settings type, silently: sdkConstraints returns ok=false and no
// validator is ever derived.
func Test_sdkConstraintsResolvesATopLevelSettingsType(t *testing.T) {
	got, ok := sdkConstraints("GlobalSwitch", "stp_version")
	if !ok {
		t.Fatalf("sdkConstraints(%q, %q) ok = false, want true: the SDK's settings.FieldConstraints[\"SettingGlobalSwitch\"][\"stp_version\"] entry exists but is never found", "GlobalSwitch", "stp_version")
	}
	want := []string{"stp", "rstp", "disabled"}
	if !reflect.DeepEqual(got.Values, want) {
		t.Errorf("sdkConstraints(%q, %q).Values = %v, want %v", "GlobalSwitch", "stp_version", got.Values, want)
	}
}

// Test_sdkConstraintsResolvesANestedSettingsType pins the case that already
// works: a nested settings struct's own Go type name already carries the
// "Setting<Parent><Child>" prefix baked in by the generator (it is never
// stripped for anything but the top-level type), so it matches the table
// key as-is. A fix to the top-level case must not regress this.
func Test_sdkConstraintsResolvesANestedSettingsType(t *testing.T) {
	got, ok := sdkConstraints("SettingDashboardWidgets", "name")
	if !ok {
		t.Fatalf("sdkConstraints(%q, %q) ok = false, want true", "SettingDashboardWidgets", "name")
	}
	if len(got.Values) == 0 {
		t.Errorf("sdkConstraints(%q, %q).Values is empty, want the widget-name enumeration", "SettingDashboardWidgets", "name")
	}
}

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
