package main

import (
	"go/token"
	"go/types"
	"reflect"
	"strings"
	"testing"

	unifi "github.com/ubiquiti-community/go-unifi/unifi"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
)

// Test_sdkConstraintsResolvesATopLevelSettingsType pins that production's
// lookup finds the SDK's own constraint for a top-level settings field.
// settings.FieldConstraints keys every entry with a "Setting" prefix
// ("SettingGlobalSwitch"), but the emitted Go struct is named "GlobalSwitch"
// -- the prefix is stripped when the type is generated. A lookup keyed by
// the bare Go type name therefore misses the table entirely, for every
// top-level settings type, silently: newSDKConstraints returns ok=false and
// no validator is ever derived.
func Test_sdkConstraintsResolvesATopLevelSettingsType(t *testing.T) {
	got, ok := newSDKConstraints(settingsPackagePath)("GlobalSwitch", "stp_version")
	if !ok {
		t.Fatalf("newSDKConstraints(settingsPackagePath)(%q, %q) ok = false, want true: the SDK's settings.FieldConstraints[\"SettingGlobalSwitch\"][\"stp_version\"] entry exists but is never found", "GlobalSwitch", "stp_version")
	}
	want := []string{"stp", "rstp", "disabled"}
	if !reflect.DeepEqual(got.Values, want) {
		t.Errorf("newSDKConstraints(settingsPackagePath)(%q, %q).Values = %v, want %v", "GlobalSwitch", "stp_version", got.Values, want)
	}
}

// Test_sdkConstraintsResolvesANestedSettingsType pins the case that already
// works: a nested settings struct's own Go type name already carries the
// "Setting<Parent><Child>" prefix baked in by the generator (it is never
// stripped for anything but the top-level type), so it matches the table
// key as-is. A fix to the top-level case must not regress this.
func Test_sdkConstraintsResolvesANestedSettingsType(t *testing.T) {
	got, ok := newSDKConstraints(settingsPackagePath)("SettingDashboardWidgets", "name")
	if !ok {
		t.Fatalf("newSDKConstraints(settingsPackagePath)(%q, %q) ok = false, want true", "SettingDashboardWidgets", "name")
	}
	if len(got.Values) == 0 {
		t.Errorf("newSDKConstraints(settingsPackagePath)(%q, %q).Values is empty, want the widget-name enumeration", "SettingDashboardWidgets", "name")
	}
}

// Test_sdkConstraintsGatesTheSettingsFallbackOnPackage pins the gate itself:
// an invocation against a package OTHER than settingsPackagePath must never
// consult settings.FieldConstraints, bare or "Setting"-prefixed, even for a
// (goType, wire) pair that table would otherwise resolve. Without this gate,
// an invocation against the top-level unifi package could silently borrow a
// settings constraint for an unrelated struct that happens to share a name
// -- unifi.Dashboard alongside settings.FieldConstraints["SettingDashboard"]
// is a real instance of exactly that collision (see newSDKConstraints).
func Test_sdkConstraintsGatesTheSettingsFallbackOnPackage(t *testing.T) {
	_, ok := newSDKConstraints("github.com/ubiquiti-community/go-unifi/unifi")("GlobalSwitch", "stp_version")
	if ok {
		t.Fatalf("newSDKConstraints(<non-settings package>)(%q, %q) ok = true, want false: "+
			"settings.FieldConstraints must not be consulted for a non-settings invocation",
			"GlobalSwitch", "stp_version")
	}
}

// Test_settingsFieldConstraintsKeysAllCarryTheSettingPrefix pins the SDK
// table's own *shape*, not one hand-picked lookup: newSDKConstraints'
// "Setting"+goType fallback only works because every key in
// settings.FieldConstraints happens to carry that prefix today. That is a
// fact about the SDK's generator convention, not something this repo
// controls or the compiler enforces -- if the SDK ever emitted an
// unprefixed key (or the convention changed for one new settings section),
// derivation would silently return nothing for it: the redundancy gate in
// provider-spec-compiler only fires when derivation *succeeds*, so a hand-
// typed validator for that field would pass straight through unnoticed,
// which is exactly the two-copies state this whole task exists to end.
// This converts that silent-drift risk into a named test failure instead of
// leaving it to a single tripwire field (GlobalSwitch/stp_version) that
// says nothing about the other 41 keys or about whatever section arrives
// next.
func Test_settingsFieldConstraintsKeysAllCarryTheSettingPrefix(t *testing.T) {
	for key := range settings.FieldConstraints {
		if !strings.HasPrefix(key, "Setting") {
			t.Errorf("settings.FieldConstraints has key %q, which does not carry the \"Setting\" "+
				"prefix newSDKConstraints' fallback assumes -- derivation silently returns nothing "+
				"for this struct's fields until newSDKConstraints is updated to match", key)
		}
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
