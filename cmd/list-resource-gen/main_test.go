package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// canonicalSpec is the shape twenty-two of the twenty-five list surfaces share.
// Tests that want a defect mutate a copy of it, so each case differs from a
// working document by exactly the thing under test.
const canonicalSpec = `{
  "provider": {"name": "unifi"},
  "listresources": [{
    "name": "firewall_zone",
    "schema": {
      "markdown_description": "List firewall zones in a site.",
      "attributes": [
        {"name": "site", "string": {"computed_optional_required": "optional",
          "description": "The name of the site to list firewall zones from."}}
      ],
      "blocks": [
        {"name": "filter", "list_nested": {"nested_object": {"attributes": [
          {"name": "name", "string": {"computed_optional_required": "required",
            "description": "The name of the filter to apply."}},
          {"name": "value", "string": {"computed_optional_required": "required",
            "description": "The value to filter by."}}
        ]}}}
      ]
    }
  }],
  "version": "0.1"
}`

func TestEmitsTheCanonicalShape(t *testing.T) {
	source := emit(t)

	for _, want := range []string{
		"package listresource_firewall_zone",
		"func FirewallZoneListResourceSchema(ctx context.Context) schema.Schema {",
		`MarkdownDescription: "List firewall zones in a site.",`,
		`"site": schema.StringAttribute{`,
		"Optional:            true,",
		`"filter": schema.ListNestedBlock{`,
		"NestedObject: schema.NestedBlockObject{",
		"Required:            true,",
	} {
		if !strings.Contains(source, want) {
			t.Errorf("the emitted source does not contain %q:\n%s", want, source)
		}
	}
}

// TestEmitsEveryDescription: a description is the one thing an emitter can drop
// while still compiling, serving the right shape, and passing every structural comparison.
func TestEmitsEveryDescription(t *testing.T) {
	source := emit(t)
	for _, want := range []string{
		"List firewall zones in a site.",
		"The name of the site to list firewall zones from.",
		"The name of the filter to apply.",
		"The value to filter by.",
	} {
		if !strings.Contains(source, want) {
			t.Errorf("description %q was dropped:\n%s", want, source)
		}
	}
}

// TestEmittedSourceIsFormatted: the file must be gofmt-clean as written, since
// it's committed -- if format.Source were removed, the only symptom would be a stray diff later.
func TestEmittedSourceIsFormatted(t *testing.T) {
	dir := t.TempDir()
	writeSpec(t, dir, canonicalSpec)
	mustRun(t, dir)

	path := filepath.Join(dir, "out", "firewall_zone_list_resource_gen.go")
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "\t\t\t\t\t\t\t\t") {
		t.Errorf("the emitted source was not run through go/format:\n%s", body)
	}
}

// TestRefusals: each case's alternative is Go that compiles and a schema that's
// wrong in a way no downstream comparison would catch.
func TestRefusals(t *testing.T) {
	for name, testCase := range map[string]struct {
		spec string
		want string
	}{
		// An attribute that is neither required nor optional nor computed
		// compiles perfectly well and is unusable. Emitting no field at all is
		// what a default branch would do.
		"an unrecognised disposition": {
			spec: strings.Replace(canonicalSpec, `"computed_optional_required": "optional"`,
				`"computed_optional_required": "sometimes"`, 1),
			want: `declares disposition "sometimes"`,
		},
		// Every list config attribute in this estate is a string. A second type
		// is a decision about what the emitter is for, not a case to add.
		"an attribute that is not a string": {
			spec: strings.Replace(canonicalSpec,
				`{"name": "site", "string": {"computed_optional_required": "optional",
          "description": "The name of the site to list firewall zones from."}}`,
				`{"name": "site", "bool": {"computed_optional_required": "optional"}}`, 1),
			want: `attribute "site" is not a string`,
		},
		"a block that is not list-nested": {
			spec: strings.Replace(canonicalSpec, `"list_nested"`, `"set_nested"`, 1),
			want: `block "filter" is not list-nested`,
		},
		// The likeliest mistake is pointing this tool at a resource's own spec
		// by accident; an empty output would read as a missing generate step, not the wrong input.
		"a document with no list surfaces": {
			spec: `{"provider": {"name": "unifi"}, "resources": [], "version": "0.1"}`,
			want: "carries no listresources member",
		},
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			writeSpec(t, dir, testCase.spec)

			stderr := &bytes.Buffer{}
			code := run([]string{
				"--input", filepath.Join(dir, "spec.json"),
				"--output", filepath.Join(dir, "out"),
				"--package", "listresource_firewall_zone",
			}, stderr)

			if code == 0 {
				t.Fatalf("the tool accepted the document and exited 0; it should have refused.\n"+
					"Emitted:\n%s", readEmitted(t, dir))
			}
			if !strings.Contains(stderr.String(), testCase.want) {
				t.Errorf("the refusal does not say %q:\n%s", testCase.want, stderr)
			}
			// A refusal that still wrote a file leaves half an artifact behind,
			// and the next build compiles it.
			if _, err := os.Stat(filepath.Join(dir, "out")); err == nil {
				if entries, _ := os.ReadDir(filepath.Join(dir, "out")); len(entries) > 0 {
					t.Errorf("the tool refused but wrote %d file(s) anyway", len(entries))
				}
			}
		})
	}
}

// TestEmissionIsDeterministic: attributes come from a list but are keyed into a
// Go map; iterating that map instead of the sorted list would make output vary run to run.
func TestEmissionIsDeterministic(t *testing.T) {
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
	writeSpec(t, dir, canonicalSpec)
	mustRun(t, dir)
	return readEmitted(t, dir)
}

func mustRun(t *testing.T, dir string) {
	t.Helper()
	stderr := &bytes.Buffer{}
	code := run([]string{
		"--input", filepath.Join(dir, "spec.json"),
		"--output", filepath.Join(dir, "out"),
		"--package", "listresource_firewall_zone",
	}, stderr)
	if code != 0 {
		t.Fatalf("the tool exited %d: %s", code, stderr)
	}
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
