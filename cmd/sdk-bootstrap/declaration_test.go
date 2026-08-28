package main

import (
	"os"
	"path/filepath"
	"testing"
)

func writeFixture(t *testing.T, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fixture.go")
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return path
}

// Test_declarationBytesIgnoresChangesOutsideTheStruct pins the whole point of
// this file: a method added beside the struct -- exactly what go-unifi's
// masked-update methods do -- must not move the digest.
func Test_declarationBytesIgnoresChangesOutsideTheStruct(t *testing.T) {
	before := writeFixture(t, `package fixture

// Widget is a thing with a name.
type Widget struct {
	Name string `+"`json:\"name\"`"+`
}
`)
	after := writeFixture(t, `package fixture

// Widget is a thing with a name.
type Widget struct {
	Name string `+"`json:\"name\"`"+`
}

// UpdateWidgetFields writes only the named fields.
func UpdateWidgetFields() {}

type Unrelated struct{}
`)

	got1, err := declarationBytes(before, "Widget")
	if err != nil {
		t.Fatalf("declarationBytes(before): %v", err)
	}
	got2, err := declarationBytes(after, "Widget")
	if err != nil {
		t.Fatalf("declarationBytes(after): %v", err)
	}
	if string(got1) != string(got2) {
		t.Errorf("declarationBytes differed after an unrelated method and struct were added to the file:\nbefore: %q\nafter:  %q", got1, got2)
	}
}

// Test_declarationBytesCapturesFieldAndCommentChanges guards the other half:
// the struct's own body, including comments (which is where go-unifi
// records enum facts), must be part of the digest.
func Test_declarationBytesCapturesFieldAndCommentChanges(t *testing.T) {
	base := writeFixture(t, `package fixture

// Widget is a thing with a name.
type Widget struct {
	Name string `+"`json:\"name\"`"+`
}
`)
	taggedDifferently := writeFixture(t, `package fixture

// Widget is a thing with a name.
type Widget struct {
	Name string `+"`json:\"name,omitempty\"`"+`
}
`)
	commentedDifferently := writeFixture(t, `package fixture

// Widget is a thing with a name.
type Widget struct {
	// Name is one of "a", "b", or "c".
	Name string `+"`json:\"name\"`"+`
}
`)

	baseBytes, err := declarationBytes(base, "Widget")
	if err != nil {
		t.Fatalf("declarationBytes(base): %v", err)
	}
	tagBytes, err := declarationBytes(taggedDifferently, "Widget")
	if err != nil {
		t.Fatalf("declarationBytes(taggedDifferently): %v", err)
	}
	if string(baseBytes) == string(tagBytes) {
		t.Errorf("declarationBytes did not change when the field's json tag changed")
	}
	commentBytes, err := declarationBytes(commentedDifferently, "Widget")
	if err != nil {
		t.Fatalf("declarationBytes(commentedDifferently): %v", err)
	}
	if string(baseBytes) == string(commentBytes) {
		t.Errorf("declarationBytes did not change when a field comment (an enum fact carrier) changed")
	}
}

// Test_declarationBytesInAGroupedDeclExcludesSiblingSpecs guards a grouped
// `type ( A struct{...}; B struct{...} )` block carrying a group-level doc
// comment: the group's doc covers every member, so it must not be attributed
// to any one of them -- only a change to B's own body may move B's digest.
func Test_declarationBytesInAGroupedDeclExcludesSiblingSpecs(t *testing.T) {
	grouped := func(aField, bField string) string {
		return `package fixture

// Package-level group doc covering both A and B.
type (
	A struct {
		Value string ` + aField + `
	}
	B struct {
		Value string ` + bField + `
	}
)
`
	}
	aTag := "`json:\"value\"`"
	aTagChanged := "`json:\"value,omitempty\"`"

	base := writeFixture(t, grouped(aTag, aTag))
	aChanged := writeFixture(t, grouped(aTagChanged, aTag))
	bChanged := writeFixture(t, grouped(aTag, aTagChanged))

	baseB, err := declarationBytes(base, "B")
	if err != nil {
		t.Fatalf("declarationBytes(base, B): %v", err)
	}
	if string(baseB) == "" {
		t.Fatal("declarationBytes(base, B) returned nothing")
	}
	baseA, err := declarationBytes(base, "A")
	if err != nil {
		t.Fatalf("declarationBytes(base, A): %v", err)
	}
	if string(baseA) == string(baseB) {
		t.Fatalf("declarationBytes(A) and declarationBytes(B) came back identical: %q", baseA)
	}

	aChangedB, err := declarationBytes(aChanged, "B")
	if err != nil {
		t.Fatalf("declarationBytes(aChanged, B): %v", err)
	}
	if string(aChangedB) != string(baseB) {
		t.Errorf("declarationBytes(B) changed when only A's field tag changed -- B's slice must exclude A's declaration:\nbase: %q\nafter A changed: %q", baseB, aChangedB)
	}

	bChangedB, err := declarationBytes(bChanged, "B")
	if err != nil {
		t.Fatalf("declarationBytes(bChanged, B): %v", err)
	}
	if string(bChangedB) == string(baseB) {
		t.Errorf("declarationBytes(B) did not change when B's own field tag changed")
	}
}

// Test_declarationBytesErrorsOnAMissingStruct guards the not-found path: a
// struct name the file does not declare must fail clearly, not silently
// return an empty or wrong slice.
func Test_declarationBytesErrorsOnAMissingStruct(t *testing.T) {
	path := writeFixture(t, `package fixture

type Widget struct {
	Name string `+"`json:\"name\"`"+`
}
`)

	_, err := declarationBytes(path, "Gadget")
	if err == nil {
		t.Fatal("declarationBytes(\"Gadget\") = nil error, want an error naming the missing struct")
	}
}
