// Command policy-scaffold writes the shape half of a surface policy.
//
// A policy has two halves. The behaviour half — validators, plan modifiers,
// defaults, custom types — is derived by cmd/schema-behaviour from the
// hand-written Go. The shape half says which SDK field each released attribute
// comes from, and which SDK fields the provider deliberately does not expose.
// That half was written by hand for the first five surfaces and is the same
// work sixty-two more times.
//
// Most of it is mechanical: an attribute whose name matches an SDK field maps
// to it, and an SDK field with no attribute is an omission. What is not
// mechanical is a rename, and this tool does not guess at one. It reports the
// attributes it could not place and stops short of inventing a source for
// them, because a wrong rename is the one mistake in this pipeline that does
// not announce itself — wlan's schedule block matches an SDK field of the same
// name and of a plausible type, and that field is the wrong one.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/ubiquiti-community/terraform-provider-unifi/internal/cmdio"
)

type bootstrapDocument struct {
	Source struct {
		SpecificationSHA256 string `json:"specification_sha256"`
	} `json:"source"`
	Resource struct {
		Fields []bootstrapField `json:"fields"`
	} `json:"resource"`
}

type bootstrapField struct {
	Name   string           `json:"name"`
	Type   string           `json:"type"`
	Fields []bootstrapField `json:"fields,omitempty"`
}

// surfaceKind carries the three things that differ between a managed resource
// and a data source: which baseline subtree describes it, what the policy calls
// it, and which digests it declares. A data source has no identity or list
// companion, and declaring empty ones is not the same as omitting them --
// validateBaseline cross-checks a declared digest in both directions.
type surfaceKind struct {
	name            string
	baselineSubtree string
	digests         func(all map[string]string, resource string) map[string]any
}

var surfaceKinds = map[string]surfaceKind{
	"managed_resource": {
		name:            "managed_resource",
		baselineSubtree: "resource_schemas",
		digests: func(all map[string]string, resource string) map[string]any {
			return map[string]any{
				"resource":      all["resource_schemas."+resource],
				"identity":      all["resource_identity_schemas."+resource],
				"list_resource": all["list_resource_schemas."+resource],
			}
		},
	},
	"data_source": {
		name:            "data_source",
		baselineSubtree: "data_source_schemas",
		digests: func(all map[string]string, resource string) map[string]any {
			return map[string]any{
				"data_source": all["data_source_schemas."+resource],
			}
		},
	},
}

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

func run(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("policy-scaffold", flag.ContinueOnError)
	flags.SetOutput(stderr)
	bootstrapPath := flags.String("bootstrap", "", "derived structural bootstrap")
	baselinePath := flags.String("baseline", "", "released provider schema projection")
	resource := flags.String("resource", "", "surface name, e.g. unifi_site")
	generator := flags.String("generator-name", "", "generator name, e.g. site")
	digestsPath := flags.String("digests", "", "M0 schema digest manifest")
	output := flags.String("output", "", "policy to write")
	surfaceKind := flags.String("surface-kind", "managed_resource",
		"managed_resource or data_source")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	// A data source reads a different subtree of the baseline, declares its
	// digest under a different key, and is emitted under a different member of
	// the code specification. Scaffolding one as a managed resource produces a
	// policy that looks right and fails at compile with "baseline data_source
	// digest mismatch", which reads as a stale digest rather than as the wrong
	// kind. Naming the kind up front is one flag; correcting it afterwards is
	// two edits per surface across thirteen of them.
	surface, ok := surfaceKinds[*surfaceKind]
	if !ok {
		fmt.Fprintf(stderr, "unsupported surface kind %q; want managed_resource or data_source\n",
			*surfaceKind)
		return 2
	}
	for name, value := range map[string]string{
		"bootstrap": *bootstrapPath, "baseline": *baselinePath, "resource": *resource,
		"generator-name": *generator, "digests": *digestsPath, "output": *output,
	} {
		if value == "" {
			fmt.Fprintf(stderr, "%s is required\n", name)
			return 2
		}
	}

	var source bootstrapDocument
	if err := readJSON(*bootstrapPath, &source); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	var baseline map[string]any
	if err := readJSON(*baselinePath, &baseline); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	var digests struct {
		SchemaSHA256 map[string]string `json:"schema_sha256"`
	}
	if err := readJSON(*digestsPath, &digests); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}

	schemas, _ := baseline[surface.baselineSubtree].(map[string]any)
	entry, found := schemas[*resource].(map[string]any)
	if !found {
		fmt.Fprintf(stderr, "%s is not in the released baseline under %s\n",
			*resource, surface.baselineSubtree)
		return 1
	}
	block, _ := entry["block"].(map[string]any)
	attributes := object(block["attributes"])
	blockTypes := object(block["block_types"])

	observed := map[string]bootstrapField{}
	for _, field := range source.Resource.Fields {
		observed[field.Name] = field
	}

	fields := []map[string]any{}
	placed := map[string]bool{}
	unplaced := []string{}

	for _, name := range cmdio.SortedKeys(attributes) {
		attribute := object(attributes[name])
		field, matched := observed[name]
		if !matched {
			unplaced = append(unplaced, name)
			continue
		}
		entry := map[string]any{
			"structural_name": name,
			"terraform_name":  name,
			"disposition":     "managed",
			"attribute":       attributeBody(attribute),
		}
		if kind := collectionKind(attribute); kind != "" {
			entry["terraform_type"] = kind
		}
		if nested, ok := attribute["nested_type"].(map[string]any); ok {
			entry["terraform_type"] = nestedKind(baselineString(nested["nesting_mode"]))
			entry["fields"] = nestedMembers(object(nested["attributes"]), field)
		}
		fields = append(fields, entry)
		placed[name] = true
	}

	for _, name := range cmdio.SortedKeys(blockTypes) {
		unplaced = append(unplaced, name+" (block)")
	}

	omitted := []string{}
	for _, name := range sortedNames(observed) {
		if placed[name] {
			continue
		}
		omitted = append(omitted, name)
		fields = append(fields, map[string]any{
			"structural_name": name, "terraform_name": name, "disposition": "omitted",
		})
	}
	sort.Slice(fields, func(i, j int) bool {
		return structuralName(fields[i]) < structuralName(fields[j])
	})

	policy := map[string]any{
		"format_version":              1,
		"surface_kind":                surface.name,
		"resource":                    *resource,
		"generator_name":              *generator,
		"source_specification_sha256": source.Source.SpecificationSHA256,
		"description":                 baselineString(block["description"]),
		"fields":                      fields,
		"provider_owned": []map[string]any{
			{"terraform_name": "timeouts", "disposition": "managed", "generated": false},
		},
		"baseline_digests": surface.digests(digests.SchemaSHA256, *resource),
	}

	encoded, err := json.MarshalIndent(policy, "", "  ")
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if err := os.WriteFile(*output, append(encoded, '\n'), 0o644); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}

	fmt.Fprintf(stdout, "%s: %d attribute(s) mapped by name, %d SDK field(s) omitted\n",
		*resource, len(placed), len(omitted))
	// Every automatic bind is listed, not just counted. A name match is a guess
	// the scaffold made without reading anything, and it is the guess that goes
	// wrong silently: static_route's type matched the SDK's type, which is the
	// record discriminator set to the constant "static-route", while the route
	// kind the schema serves lives in static-route_type. Both referees compare
	// the Terraform schema, which is identical either way, so nothing downstream
	// can catch it -- only the mapping report records which field is written.
	//
	// A count cannot be checked against anything. A list can, and it sends the
	// reader to the same conversion code the unplaced ones already send them to.
	if len(placed) > 0 {
		fmt.Fprintf(stdout, "\n  BOUND BY NAME — confirm each against the conversion code (%d):\n    %s\n",
			len(placed), strings.Join(sortedNamesOf(placed), "\n    "))
	}
	if len(unplaced) > 0 {
		fmt.Fprintf(stdout, "\n  NOT PLACED — resolve each from the resource's conversion code, not by name (%d):\n    %s\n",
			len(unplaced), strings.Join(unplaced, "\n    "))
	}
	fmt.Fprintln(stdout, "\n  A rename is the one mistake here that does not announce itself. Read the\n"+
		"  model-to-SDK assignments in the resource and write the structural_name in.\n"+
		"  Do not match on a name that merely looks right: wlan's schedule block\n"+
		"  matches an SDK field of the same name and of a plausible type, and that\n"+
		"  field is the legacy one; and static_route's type matches the SDK's\n"+
		"  record discriminator rather than the route kind it serves.")
	return 0
}

func sortedNamesOf(set map[string]bool) []string {
	names := make([]string, 0, len(set))
	for name := range set {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func nestedMembers(members map[string]any, field bootstrapField) []map[string]any {
	observed := map[string]bool{}
	for _, inner := range field.Fields {
		observed[inner.Name] = true
	}
	out := []map[string]any{}
	for _, name := range cmdio.SortedKeys(members) {
		if !observed[name] {
			continue
		}
		out = append(out, map[string]any{
			"structural_name": name, "terraform_name": name,
			"disposition": "managed", "attribute": attributeBody(object(members[name])),
		})
		delete(observed, name)
	}
	for _, name := range sortedNames(observed) {
		out = append(out, map[string]any{
			"structural_name": name, "terraform_name": name, "disposition": "omitted",
		})
	}
	return out
}

func attributeBody(attribute map[string]any) map[string]any {
	body := map[string]any{"computed_optional_required": disposition(attribute)}
	if description := baselineString(attribute["description"]); description != "" {
		body["description"] = description
	}
	if sensitive, _ := attribute["sensitive"].(bool); sensitive {
		body["sensitive"] = true
	}
	if kind := collectionKind(attribute); kind != "" {
		if listed, ok := attribute["type"].([]any); ok && len(listed) == 2 {
			body["element_type"] = map[string]any{baselineString(listed[1]): map[string]any{}}
		}
	}
	return body
}

func disposition(attribute map[string]any) string {
	optional, _ := attribute["optional"].(bool)
	computed, _ := attribute["computed"].(bool)
	required, _ := attribute["required"].(bool)
	switch {
	case required:
		return "required"
	case optional && computed:
		return "computed_optional"
	case optional:
		return "optional"
	default:
		return "computed"
	}
}

func collectionKind(attribute map[string]any) string {
	listed, ok := attribute["type"].([]any)
	if !ok || len(listed) == 0 {
		return ""
	}
	switch baselineString(listed[0]) {
	case "list":
		return "list"
	case "set":
		return "set"
	default:
		return ""
	}
}

func nestedKind(mode string) string {
	switch mode {
	case "single":
		return "single_nested"
	case "set":
		return "set_nested"
	default:
		return "list_nested"
	}
}

func readJSON(path string, target any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, target)
}

func object(value any) map[string]any {
	if m, ok := value.(map[string]any); ok {
		return m
	}
	return map[string]any{}
}

func baselineString(value any) string {
	s, _ := value.(string)
	return s
}

// structuralName is the sort key for a scaffolded field. Every entry is built
// in this file with a string structural_name, so the assertion held -- but it
// sat inside a sort.Slice comparator, where a malformed entry would panic
// naming sort.Slice rather than the field that caused it. An entry without one
// sorts first and stays visible in the output.
func structuralName(field map[string]any) string {
	name, _ := field["structural_name"].(string)
	return name
}

func sortedNames[T any](m map[string]T) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
