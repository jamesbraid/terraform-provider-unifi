package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// portSpec is the estate's action. Cases that want a defect mutate a copy, so
// each differs from a working document by exactly the thing under test.
const portSpec = `{
  "provider": {"name": "unifi"},
  "actions": [{
    "name": "port",
    "schema": {
      "markdown_description": "Performs an action on a UniFi device port.",
      "attributes": [
        {"name": "device_mac", "string": {"computed_optional_required": "required",
          "description": "MAC address of the device.",
          "custom_type": {"import": {"path": "github.com/hashicorp/terraform-plugin-framework-nettypes/hwtypes"},
            "type": "hwtypes.MACAddressType{}", "value_type": "hwtypes.MACAddress"}}},
        {"name": "port_number", "int64": {"computed_optional_required": "required",
          "description": "Port number."}}
      ]
    }
  }],
  "version": "0.1"
}`

func TestEmitsTheActionSchema(t *testing.T) {
	source := emit(t)
	for _, want := range []string{
		"package action_port",
		"func PortActionSchema(ctx context.Context) schema.Schema {",
		`"github.com/hashicorp/terraform-plugin-framework/action/schema"`,
		`"device_mac": schema.StringAttribute{`,
		"CustomType:          hwtypes.MACAddressType{},",
		`"port_number": schema.Int64Attribute{`,
		"Required:            true,",
	} {
		if !strings.Contains(source, want) {
			t.Errorf("the emitted source does not contain %q:\n%s", want, source)
		}
	}
}

// TestEmitsTheCustomTypeImport: a custom type without its import is Go that
// won't compile, so the import must be emitted exactly once.
func TestEmitsTheCustomTypeImport(t *testing.T) {
	source := emit(t)
	want := `"github.com/hashicorp/terraform-plugin-framework-nettypes/hwtypes"`
	if !strings.Contains(source, want) {
		t.Errorf("the custom type's import was not emitted:\n%s", source)
	}
	if strings.Count(source, want) != 1 {
		t.Errorf("the import appears %d times, want once", strings.Count(source, want))
	}
}

func TestActionRefusals(t *testing.T) {
	for name, testCase := range map[string]struct{ spec, want string }{
		// A custom type without its import does not compile: refusing here
		// names the attribute, not just the package.
		"a custom type with no import": {
			spec: strings.Replace(portSpec,
				`"import": {"path": "github.com/hashicorp/terraform-plugin-framework-nettypes/hwtypes"},`,
				"", 1),
			want: `attribute "device_mac" declares custom type hwtypes.MACAddressType{} with no import path`,
		},
		"an unrecognised disposition": {
			spec: strings.Replace(portSpec, `"computed_optional_required": "required"`,
				`"computed_optional_required": "whenever"`, 1),
			want: `declares disposition "whenever"`,
		},
		// A collection or nested attribute has no scalar member set; picking one
		// arbitrarily would produce a schema nobody asked for.
		"an attribute with no scalar type": {
			spec: strings.Replace(portSpec,
				`"int64": {"computed_optional_required": "required",
          "description": "Port number."}`,
				`"list": {"computed_optional_required": "required"}`, 1),
			want: `attribute "port_number" declares no scalar type`,
		},
		"a document with no actions": {
			spec: `{"provider": {"name": "unifi"}, "resources": [], "version": "0.1"}`,
			want: "carries no actions member",
		},
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			writeSpec(t, dir, testCase.spec)
			stderr := &bytes.Buffer{}

			code := run([]string{
				"--input", filepath.Join(dir, "spec.json"),
				"--output", filepath.Join(dir, "out"),
				"--package", "action_port",
			}, stderr)

			if code == 0 {
				t.Fatalf("the tool accepted the document and exited 0.\nEmitted:\n%s", readEmitted(t, dir))
			}
			if !strings.Contains(stderr.String(), testCase.want) {
				t.Errorf("the refusal does not say %q:\n%s", testCase.want, stderr)
			}
			if entries, err := os.ReadDir(filepath.Join(dir, "out")); err == nil && len(entries) > 0 {
				t.Errorf("the tool refused but wrote %d file(s) anyway", len(entries))
			}
		})
	}
}

// An attribute declaring two scalar kinds must not be resolved by field order,
// which is a silent choice between two things the author asked for.
func TestRefusesAnAmbiguousAttribute(t *testing.T) {
	spec := strings.Replace(portSpec,
		`{"name": "port_number", "int64": {"computed_optional_required": "required",
          "description": "Port number."}}`,
		`{"name": "port_number", "int64": {"computed_optional_required": "required"},
          "string": {"computed_optional_required": "required"}}`, 1)

	dir := t.TempDir()
	writeSpec(t, dir, spec)
	stderr := &bytes.Buffer{}
	code := run([]string{
		"--input", filepath.Join(dir, "spec.json"),
		"--output", filepath.Join(dir, "out"),
		"--package", "action_port",
	}, stderr)

	if code == 0 {
		t.Fatal("an attribute declaring two types was accepted")
	}
	if !strings.Contains(stderr.String(), "declares 2 types") {
		t.Errorf("the refusal does not count the types:\n%s", stderr)
	}
}

func TestActionEmissionIsDeterministic(t *testing.T) {
	first := emit(t)
	for range 5 {
		if again := emit(t); again != first {
			t.Fatalf("two emissions from the same document differ:\n%s\n---\n%s", first, again)
		}
	}
}

func emit(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeSpec(t, dir, portSpec)
	stderr := &bytes.Buffer{}
	if code := run([]string{
		"--input", filepath.Join(dir, "spec.json"),
		"--output", filepath.Join(dir, "out"),
		"--package", "action_port",
	}, stderr); code != 0 {
		t.Fatalf("the tool exited %d: %s", code, stderr)
	}
	return readEmitted(t, dir)
}

func writeSpec(t *testing.T, dir, spec string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "spec.json"), []byte(spec), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readEmitted(t *testing.T, dir string) string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(dir, "out"))
	if err != nil {
		return "(nothing was written)"
	}
	body := &strings.Builder{}
	for _, entry := range entries {
		content, err := os.ReadFile(filepath.Join(dir, "out", entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		body.Write(content)
	}
	return body.String()
}
