package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// oneFamily renders the declarations the generator emits for one nested object
// type; `extra` is appended to the value struct so two families can be made to differ.
func oneFamily(name, extra string) string {
	return `
var _ basetypes.ObjectTypable = ` + name + `Type{}

type ` + name + `Type struct {
	basetypes.ObjectType
}

func (t ` + name + `Type) String() string {
	return "` + name + `Type"
}

func New` + name + `ValueNull() ` + name + `Value {
	return ` + name + `Value{}
}

var _ basetypes.ObjectValuable = ` + name + `Value{}

type ` + name + `Value struct {
	Number int64 ` + "`tfsdk:\"number\"`" + `
` + extra + `}

func (v ` + name + `Value) IsNull() bool {
	return false
}
`
}

const preamble = `package repro

import "github.com/hashicorp/terraform-plugin-framework/types/basetypes"

func ThingSchema() string { return "unrelated declaration, must survive untouched" }
`

func writeFile(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "thing_resource_gen.go")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// Test_anExactDuplicateIsRemoved is the case wan actually produces: two
// attributes named `options` yield two identical OptionsType declarations.
func Test_anExactDuplicateIsRemoved(t *testing.T) {
	path := writeFile(t, preamble+oneFamily("Options", "")+oneFamily("Dhcp", "")+oneFamily("Options", ""))

	var stdout, stderr bytes.Buffer
	if code := run([]string{path}, &stdout, &stderr); code != 0 {
		t.Fatalf("exit %d, stderr: %s", code, stderr.String())
	}

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := bytes.Count(after, []byte("type OptionsType struct")); got != 1 {
		t.Errorf("OptionsType declared %d time(s) after the pass, want exactly 1", got)
	}
	if got := bytes.Count(after, []byte("type DhcpType struct")); got != 1 {
		t.Errorf("DhcpType declared %d time(s); an unrelated family must be untouched", got)
	}
	if !bytes.Contains(after, []byte("unrelated declaration, must survive untouched")) {
		t.Error("a declaration belonging to no family was removed")
	}
	if !strings.Contains(stdout.String(), "removed 1 duplicate") {
		t.Errorf("the pass must say what it did; got %q", stdout.String())
	}
}

// Test_aDivergentDuplicateIsRefused: two same-named declarations that differ
// must be refused, and the refusal must name what it refused on.
func Test_aDivergentDuplicateIsRefused(t *testing.T) {
	body := preamble + oneFamily("Options", "") + oneFamily("Options", "\tValue string `tfsdk:\"value\"`\n")
	path := writeFile(t, body)

	var stdout, stderr bytes.Buffer
	code := run([]string{path}, &stdout, &stderr)
	if code == 0 {
		t.Fatal("the pass accepted two declarations that differ; a dedup that cannot fail is " +
			"worse than the duplicate it removes")
	}

	message := stderr.String()
	for _, want := range []string{"OptionsType/OptionsValue", "THE TWO DECLARATIONS DIFFER", path} {
		if !strings.Contains(message, want) {
			t.Errorf("the refusal must name %q; got:\n%s", want, message)
		}
	}

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != body {
		t.Error("the file was modified on a refusal; a refused pass must change nothing")
	}
}

// Test_aFileWithNoDuplicateIsUntouched: being inert where nothing needs fixing
// is a property worth pinning, not assuming, since this pass runs over the whole chain.
func Test_aFileWithNoDuplicateIsUntouched(t *testing.T) {
	body := preamble + oneFamily("Dhcp", "") + oneFamily("Dhcpv6", "")
	path := writeFile(t, body)

	var stdout, stderr bytes.Buffer
	if code := run([]string{path}, &stdout, &stderr); code != 0 {
		t.Fatalf("exit %d, stderr: %s", code, stderr.String())
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != body {
		t.Error("a file with no duplicate was rewritten")
	}
	if stdout.String() != "" {
		t.Errorf("a no-op run must say nothing; got %q", stdout.String())
	}
}

// Test_threeCopiesCollapseToOne guards the loop rather than assuming the
// duplicate arrives in pairs. Nothing in the generator promises two.
func Test_threeCopiesCollapseToOne(t *testing.T) {
	path := writeFile(t, preamble+oneFamily("Options", "")+oneFamily("Options", "")+oneFamily("Options", ""))

	var stdout, stderr bytes.Buffer
	if code := run([]string{path}, &stdout, &stderr); code != 0 {
		t.Fatalf("exit %d, stderr: %s", code, stderr.String())
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := bytes.Count(after, []byte("type OptionsType struct")); got != 1 {
		t.Errorf("OptionsType declared %d time(s), want exactly 1", got)
	}
}
