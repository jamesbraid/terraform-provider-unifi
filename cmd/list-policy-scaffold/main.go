// Command list-policy-scaffold derives a list surface's policy from the
// released schema.
//
// A list resource's config schema is not a projection of an SDK struct. It is
// a query: which site to look in, and which filters to apply. Every attribute
// in it is either provider-owned or invented outright, and none of them comes
// from the wire. That makes cmd/policy-scaffold's core move -- bind an
// attribute to the SDK field of the same name -- wrong here rather than merely
// unhelpful, which is why this is a separate tool instead of a flag on that
// one.
//
// What it derives, it derives from the RELEASED schema projection rather than
// from the hand-written Go. The released schema is the contract: it is what
// shipped, what practitioners wrote configuration against, and what the
// migration has to reproduce. Reading the Go instead would mean the policy and
// the thing it is checked against were transcribed from the same source, and a
// mistake in the transcription would agree with itself.
//
// It refuses to declare a member invented when an SDK field of that name
// exists. An invented member says "no wire field carries this"; if one does,
// the member may really be bound to it and the tool has no way to tell. That
// is the same class of mistake cmd/policy-scaffold declines to guess at, in
// the one direction this tool could make it.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/ubiquiti-community/terraform-provider-unifi/internal/cmdio"
)

type bootstrapDocument struct {
	Source struct {
		SpecificationSHA256 string `json:"specification_sha256"`
	} `json:"source"`
	Resource struct {
		Fields []struct {
			Name string `json:"name"`
		} `json:"fields"`
	} `json:"resource"`
}

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

func run(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("list-policy-scaffold", flag.ContinueOnError)
	flags.SetOutput(stderr)
	bootstrapPath := flags.String("bootstrap", "", "derived structural bootstrap for the listed resource")
	schemaPath := flags.String("schema", "", "released provider schema projection")
	resource := flags.String("resource", "", "surface name, e.g. unifi_firewall_zone")
	generator := flags.String("generator-name", "", "generator name, e.g. firewall_zone")
	digestsPath := flags.String("digests", "", "M0 schema digest manifest")
	output := flags.String("output", "", "policy to write")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	for name, value := range map[string]string{
		"bootstrap": *bootstrapPath, "schema": *schemaPath, "resource": *resource,
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
	var schema map[string]any
	if err := readJSON(*schemaPath, &schema); err != nil {
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

	schemas := object(schema["list_resource_schemas"])
	entry, found := schemas[*resource].(map[string]any)
	if !found {
		fmt.Fprintf(stderr, "%s is not in the released schema under list_resource_schemas; "+
			"it is not a list surface, or the projection predates it\n", *resource)
		return 1
	}
	block := object(entry["block"])

	observed := map[string]bool{}
	for _, field := range source.Resource.Fields {
		observed[field.Name] = true
	}

	// Every SDK field is recorded as omitted. Stating it is not ceremony: the
	// compiler requires every observed field to carry a decision, so an empty
	// fields array would be rejected, and an omission written out by name is a
	// claim that can be read and disagreed with. "This config schema exposes
	// none of the listed resource's fields" is true and worth being explicit
	// about, because the surface next to it -- the managed resource -- exposes
	// most of them.
	fields := make([]map[string]any, 0, len(observed))
	for _, name := range cmdio.SortedKeys(observed) {
		fields = append(fields, map[string]any{
			"structural_name": name, "terraform_name": name, "disposition": "omitted",
		})
	}

	attributes := object(block["attributes"])
	providerOwned := make([]map[string]any, 0, len(attributes))
	for _, name := range cmdio.SortedKeys(attributes) {
		attribute := object(attributes[name])
		kind, err := scalarKind(attribute["type"])
		if err != nil {
			fmt.Fprintf(stderr, "%s: attribute %q is %v; every list config attribute in this "+
				"estate is a string, so a new type is a decision rather than a case to add\n",
				*resource, name, attribute["type"])
			return 1
		}
		providerOwned = append(providerOwned, map[string]any{
			"terraform_name": name,
			"terraform_type": kind,
			"disposition":    "managed",
			"generated":      true,
			"attribute":      attributeBody(attribute),
		})
	}

	blockTypes := object(block["block_types"])
	groupings := make([]map[string]any, 0, len(blockTypes))
	conflicts := []string{}
	for _, name := range cmdio.SortedKeys(blockTypes) {
		declared := object(blockTypes[name])
		nested := object(object(declared["block"])["attributes"])
		// Is this the query block every list surface in this estate has? Its
		// members are a key and a value: the key names which field to select
		// on and the value is what to compare against. Both are terms the
		// practitioner writes, so both are invented no matter what the listed
		// resource's fields are called.
		//
		// The distinction matters because `name` collides with an SDK field on
		// most surfaces. Refusing on the collision alone would refuse
		// twenty-odd of the twenty-five, and a guard that fires on the normal
		// case is one people learn to route around. Refusing on an unrecognised
		// member set instead keeps it rare and keeps it meaningful.
		queryBlock := isQueryBlock(nested)
		members := make([]map[string]any, 0, len(nested))
		for _, member := range cmdio.SortedKeys(nested) {
			body := object(nested[member])
			kind, err := scalarKind(body["type"])
			if err != nil {
				fmt.Fprintf(stderr, "%s: %s.%s is %v, and only strings are handled here\n",
					*resource, name, member, body["type"])
				return 1
			}
			// The refusal. Outside the known query shape, an invented member
			// asserting "no wire field carries this" may simply be false, and
			// this tool cannot tell which.
			if !queryBlock && observed[member] {
				conflicts = append(conflicts, fmt.Sprintf("%s.%s", name, member))
			}
			members = append(members, map[string]any{
				"terraform_name": member,
				"terraform_type": kind,
				"disposition":    "managed",
				"invented":       inventedReason(name, member, queryBlock, observed[member]),
				"attribute":      attributeBody(body),
			})
		}
		groupings = append(groupings, map[string]any{
			"terraform_name": name,
			"terraform_type": blockNesting(baselineString(declared["nesting_mode"])),
			"members":        members,
		})
	}

	if len(conflicts) > 0 {
		fmt.Fprintf(stderr,
			"%s: refusing to write a policy. These members sit in a block that is not the "+
				"key/value query shape, and each shares a name with an SDK field:\n    %s\n\n"+
				"    An invented member asserts that no wire field carries it. In the query\n"+
				"    block that assertion holds by construction -- the member is a filter key,\n"+
				"    and its value is what gets compared against the field. Outside it there is\n"+
				"    no such guarantee, and whether this member is a query term or a binding is\n"+
				"    a judgement about what the List method does with it. Read that method and\n"+
				"    write the member by hand.\n",
			*resource, strings.Join(conflicts, "\n    "))
		return 1
	}

	policy := map[string]any{
		"format_version":              1,
		"surface_kind":                "list_resource",
		"resource":                    *resource,
		"generator_name":              *generator,
		"source_specification_sha256": source.Source.SpecificationSHA256,
		"description":                 baselineString(block["description"]),
		"fields":                      fields,
		"provider_owned":              providerOwned,
		"baseline_digests": map[string]any{
			"list_resource": digests.SchemaSHA256["list_resource_schemas."+*resource],
		},
	}
	if len(groupings) > 0 {
		policy["groupings"] = groupings
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

	fmt.Fprintf(stdout, "%s: %d provider-owned attribute(s), %d block(s), %d SDK field(s) omitted\n",
		*resource, len(providerOwned), len(groupings), len(fields))
	return 0
}

// isQueryBlock reports whether a block is the key/value pair every list surface
// here filters with. Measured rather than assumed: all twenty-five carry
// exactly this block, and TestListResourceConfigSchemasAreUniform is what keeps
// that true.
func isQueryBlock(members map[string]any) bool {
	if len(members) != 2 {
		return false
	}
	_, hasName := members["name"]
	_, hasValue := members["value"]
	return hasName && hasValue
}

// inventedReason says why the member corresponds to nothing on the wire, and
// says it differently when the name collides with an SDK field -- because that
// is the case a reader will stop at, and the answer is not obvious.
func inventedReason(block, member string, queryBlock, collides bool) string {
	if !queryBlock {
		return fmt.Sprintf(
			"A %s block member is a query term the practitioner writes; no field of the "+
				"listed resource carries it.", block)
	}
	if member == "name" {
		reason := "The filter KEY: it holds the name of the field to select on, not that " +
			"field's value. Nothing on the wire carries it."
		if collides {
			reason += " The listed resource has a field of the same name, and this member is " +
				"still not bound to it -- the key names the field, the sibling value is what " +
				"gets compared against it."
		}
		return reason
	}
	return "The filter VALUE: what the key's field is compared against. A term the " +
		"practitioner writes, carried by no wire field."
}

// scalarKind accepts only the scalar spellings. A collection reaches here as a
// two-element array and is rejected by name rather than silently rendered as
// whatever its first element says.
func scalarKind(declared any) (string, error) {
	kind, ok := declared.(string)
	if !ok {
		return "", fmt.Errorf("not a scalar type")
	}
	switch kind {
	case "string", "bool", "number":
		return kind, nil
	default:
		return "", fmt.Errorf("unsupported scalar %q", kind)
	}
}

func blockNesting(mode string) string {
	switch mode {
	case "single":
		return "single_nested_block"
	case "set":
		return "set_nested_block"
	default:
		return "list_nested_block"
	}
}

func attributeBody(attribute map[string]any) map[string]any {
	body := map[string]any{"computed_optional_required": disposition(attribute)}
	if description := baselineString(attribute["description"]); description != "" {
		body["description"] = description
	}
	if sensitive, _ := attribute["sensitive"].(bool); sensitive {
		body["sensitive"] = true
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
