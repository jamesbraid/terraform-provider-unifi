package main

import (
	"go/token"
	"go/types"
	"testing"
)

// tagged builds one struct field carrying a json tag.
func tagged(name string, kind types.BasicKind) *types.Var {
	return types.NewField(token.NoPos, nil, name, types.Typ[kind], false)
}

// embedded builds one anonymous struct field, which is how the SDK writes
// BaseSetting into every setting type.
func embedded(named *types.Named) *types.Var {
	return types.NewField(token.NoPos, nil, named.Obj().Name(), named, true)
}

// named wraps a struct so it can be embedded; an embedded field must have a
// named type.
func named(name string, underlying *types.Struct) *types.Named {
	return types.NewNamed(types.NewTypeName(token.NoPos, nil, name, nil), underlying, nil)
}

func fieldNames(fields []field) []string {
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		out = append(out, f.Name)
	}
	return out
}

// Test_walkPromotesEmbeddedStructFields pins the shape every go-unifi settings
// type has: an anonymous BaseSetting carrying the record ID, the site ID and
// `key` -- the discriminator the provider's own GetKey/SetKey depends on.
//
// An embedded field carries no struct tag of its own, so a walk that keys on
// the tag alone skips the whole embedded struct and reports the outer fields as
// if they were the complete projection. Nothing downstream can notice: the
// compiler's exactly-once accounting only checks fields the bootstrap mentions,
// so a field the bootstrap never names is not observed, and the policy comes
// out complete with respect to a projection that is itself incomplete.
//
// This is the same defect the six silently-omitted firewall_policy fields were,
// reintroduced through the derived path that was meant to have closed it.
//
// Reverting the embedded-field promotion in walk fails this test.
func Test_walkPromotesEmbeddedStructFields(t *testing.T) {
	base := types.NewStruct(
		[]*types.Var{
			tagged("ID", types.String),
			tagged("Key", types.String),
		},
		[]string{`json:"_id,omitempty"`, `json:"key"`},
	)
	outer := types.NewStruct(
		[]*types.Var{
			embedded(named("BaseSetting", base)),
			tagged("Code", types.Int64),
		},
		[]string{"", `json:"code,omitempty"`},
	)

	got := fieldNames(walk(outer))

	want := []string{"_id", "code", "key"}
	if len(got) != len(want) {
		t.Fatalf("walk() returned %d fields %v, want %d %v", len(got), got, len(want), want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("walk()[%d] = %q, want %q (full: %v)", i, got[i], want[i], got)
		}
	}
}

// Test_walkKeepsATaggedEmbeddedFieldAsAMember guards the other half of the
// promotion rule, which is the half that could regress silently.
//
// Promotion happens because an anonymous field has no name of its own on the
// wire. An embedded field that DOES carry a json tag has one, so encoding/json
// sends it as a nested object rather than spreading it -- and a walk that
// promoted on embeddedness alone would flatten a real nested member into its
// parent, inventing fields the controller never sees at that level and losing
// the object that does exist.
//
// Promoting on `member.Embedded()` without first checking that the tag yielded
// no name fails this test.
func Test_walkKeepsATaggedEmbeddedFieldAsAMember(t *testing.T) {
	inner := types.NewStruct(
		[]*types.Var{tagged("Enabled", types.Bool)},
		[]string{`json:"enabled"`},
	)
	outer := types.NewStruct(
		[]*types.Var{
			types.NewField(token.NoPos, nil, "Inner", named("Inner", inner), true),
			tagged("Code", types.Int64),
		},
		[]string{`json:"inner"`, `json:"code,omitempty"`},
	)

	got := walk(outer)

	if names := fieldNames(got); len(names) != 2 || names[0] != "code" || names[1] != "inner" {
		t.Fatalf("walk() = %v, want [code inner]: a tagged embedded field is a member, not a promotion", names)
	}
	var innerField field
	for _, f := range got {
		if f.Name == "inner" {
			innerField = f
		}
	}
	if innerField.Type != "object" {
		t.Errorf("inner.Type = %q, want %q", innerField.Type, "object")
	}
	if names := fieldNames(innerField.Fields); len(names) != 1 || names[0] != "enabled" {
		t.Errorf("inner.Fields = %v, want [enabled]", names)
	}
}
