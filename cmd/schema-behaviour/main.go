// Command schema-behaviour derives a hand-written schema's validators, plan
// modifiers, defaults and custom types from Go source, and writes them in the
// form a migration policy carries.
//
// A surface being migrated has to restate all four in its policy: the compiler
// cannot infer them from a catalog, and no schema comparison can see that they
// are gone. wlan carries eighty-three of them. Typing that many by hand is the
// largest per-surface cost left in the migration, and it is the last step of
// the recipe still resting on someone being careful -- which is the same
// position bootstraps were in before they were derived, and deriving those
// immediately turned up six SDK fields a hand-written projection had silently
// omitted.
//
// Two modes:
//
//	-resource unifi_wlan
//	    write the derived behaviour as a document, keyed by attribute path.
//
//	-resource unifi_wlan -policy provider-codegen/policy/wlan.json
//	    merge the derived behaviour INTO that policy, matching each attribute
//	    by the terraform_name the policy already declares.
//
// The merge matches on terraform_name deliberately. A migration renames fields
// -- wlan renames nine, two of which nobody recovers by inspection -- and the
// rename decision belongs to the policy, which was written against the
// resource's own conversion code. Matching on a name this command works out
// for itself would be guessing at exactly the point guessing has already gone
// wrong twice. So: the policy says which SDK field is which Terraform
// attribute, this command says what that attribute does, and a policy naming an
// attribute the schema does not have is reported rather than skipped -- which
// makes the merge a rename check as well as a transcription.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/ubiquiti-community/terraform-provider-unifi/internal/schemabehaviour"
)

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

func run(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("schema-behaviour", flag.ContinueOnError)
	flags.SetOutput(stderr)
	dir := flags.String("dir", "unifi", "directory holding the provider's hand-written resources")
	resourceName := flags.String("resource", "", "resource type name, e.g. unifi_wlan; omit for all")
	policyPath := flags.String("policy", "", "policy to merge the derived behaviour into, in place")
	output := flags.String("output", "", "file to write the derived document to (default stdout)")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if *policyPath != "" && *resourceName == "" {
		fmt.Fprintln(stderr, "merging into a policy needs -resource to say which surface to take")
		return 2
	}

	surfaces, err := schemabehaviour.DeriveDir(*dir)
	if err != nil {
		fmt.Fprintf(stderr, "derive from %s: %v\n", *dir, err)
		return 1
	}

	if *resourceName != "" {
		surface, err := pick(surfaces, *resourceName)
		if err != nil {
			fmt.Fprintf(stderr, "%v\n", err)
			return 1
		}
		if surface.Delegated {
			fmt.Fprintf(stderr,
				"%s already serves a generated schema (%s), so there is no hand-written "+
					"schema here to derive from\n", surface.TypeName, surface.DelegatedTo)
			return 1
		}
		surfaces = []schemabehaviour.Surface{surface}
	}

	if *policyPath != "" {
		report, err := schemabehaviour.MergeIntoPolicy(*policyPath, surfaces[0])
		if err != nil {
			fmt.Fprintf(stderr, "merge into %s: %v\n", *policyPath, err)
			return 1
		}
		fmt.Fprint(stderr, report)
		return 0
	}

	body, err := json.MarshalIndent(document{FormatVersion: 1, Surfaces: surfaces}, "", "  ")
	if err != nil {
		fmt.Fprintf(stderr, "encode: %v\n", err)
		return 1
	}
	body = append(body, '\n')
	if *output == "" {
		if _, err := stdout.Write(body); err != nil {
			fmt.Fprintf(stderr, "write: %v\n", err)
			return 1
		}
		return 0
	}
	if err := os.WriteFile(*output, body, 0o644); err != nil {
		fmt.Fprintf(stderr, "write %s: %v\n", *output, err)
		return 1
	}
	return 0
}

type document struct {
	FormatVersion int                       `json:"format_version"`
	Surfaces      []schemabehaviour.Surface `json:"surfaces"`
}

// pick reports the names it does have when asked for one it does not, because
// a resource name is easy to mistype and a bare "not found" sends the reader
// back to grep for the answer this command is already holding.
func pick(surfaces []schemabehaviour.Surface, name string) (schemabehaviour.Surface, error) {
	var names []string
	for _, surface := range surfaces {
		if surface.TypeName == name {
			return surface, nil
		}
		names = append(names, surface.TypeName)
	}
	sort.Strings(names)
	return schemabehaviour.Surface{}, fmt.Errorf(
		"no managed resource called %s; the source declares %s", name, strings.Join(names, ", "))
}
