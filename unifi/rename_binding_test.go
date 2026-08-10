package unifi

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/ubiquiti-community/terraform-provider-unifi/internal/renamecheck"
)

const policyDir = "../provider-codegen/policy"

// policyDocument is the part of a policy this test reads: which SDK field each
// attribute claims to come from.
type policyDocument struct {
	Resource      string        `json:"resource"`
	GeneratorName string        `json:"generator_name"`
	SurfaceKind   string        `json:"surface_kind"`
	Fields        []policyField `json:"fields"`
	Groupings     []struct {
		TerraformName string `json:"terraform_name"`
		Members       []struct {
			StructuralName string `json:"structural_name"`
			TerraformName  string `json:"terraform_name"`
			Disposition    string `json:"disposition"`
			Invented       string `json:"invented"`
		} `json:"members"`
	} `json:"groupings"`
	Flattenings []struct {
		Members []policyField `json:"members"`
	} `json:"flattenings"`
}

type policyField struct {
	StructuralName string        `json:"structural_name"`
	TerraformName  string        `json:"terraform_name"`
	Disposition    string        `json:"disposition"`
	Invented       string        `json:"invented"`
	Fields         []policyField `json:"fields"`
}

// claim is one policy assertion: this attribute comes from this SDK field.
type claim struct {
	terraform  string
	structural string
}

// Test_policyRenamesMatchTheConversionCode is the third referee, and it checks
// the one thing the other two cannot.
//
// The baseline projection compares the built schema against the released
// contract. The behaviour inventory compares validators, plan modifiers and
// defaults against a golden. Both are about the TERRAFORM SCHEMA, and an
// attribute bound to the wrong SDK field produces a byte-identical schema --
// same name, same type, same validator, same description. The resource simply
// reads and writes the wrong field on the controller.
//
// static_route is the proof. go-unifi's Routing carries both `type` and
// `static-route_type`. The released `type` attribute is the route kind and comes
// from the second; the first is a record discriminator the provider sets to the
// constant "static-route" and never reads. policy-scaffold matched on the name,
// and the whole suite stayed green.
//
// Three of the first nineteen migrated surfaces had a trap of this shape. At
// that rate a careful reader is not a control, so the pairing is derived from
// the conversion code and compared here.
//
// WHAT COUNTS AS A FAILURE, deliberately narrow: only a claim the conversion
// code CONTRADICTS. If the code pairs an attribute with a field, and the policy
// names a different one, that is a defect. If the code does not mention the
// attribute at all -- a helper, a loop, a switch, a computed value -- this
// reports it as unchecked rather than failing, because a referee that guesses is
// the thing being guarded against.
func Test_policyRenamesMatchTheConversionCode(t *testing.T) {
	derived, err := renamecheck.Derive(".")
	if err != nil {
		t.Fatalf("deriving bindings from the conversion code: %v", err)
	}
	if len(derived.Bindings) == 0 {
		t.Fatal("no bindings were derived, so this comparison would pass against an empty set")
	}

	// Per file, every SDK field each attribute name is paired with. A name can
	// appear more than once -- nested shapes reuse `enabled` and `id` -- and
	// that only makes the check more permissive, never wrong: a claim passes if
	// ANY binding of that name supports it.
	paired := map[string]map[string]map[string]bool{}
	for _, b := range derived.Bindings {
		if paired[b.File] == nil {
			paired[b.File] = map[string]map[string]bool{}
		}
		if paired[b.File][b.TerraformName] == nil {
			paired[b.File][b.TerraformName] = map[string]bool{}
		}
		paired[b.File][b.TerraformName][b.StructuralName] = true
	}

	policies, err := filepath.Glob(filepath.Join(policyDir, "*.json"))
	if err != nil {
		t.Fatalf("listing policies: %v", err)
	}

	var contradicted, unchecked []string
	checked, surfaces, claimless := 0, 0, 0

	for _, path := range policies {
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("reading %s: %v", path, err)
		}
		var policy policyDocument
		if err := json.Unmarshal(body, &policy); err != nil {
			// Not every file in the directory is a surface policy.
			continue
		}
		if policy.Resource == "" || policy.GeneratorName == "" {
			continue
		}

		claims := claimsOf(policy)
		if len(claims) == 0 {
			// A list resource or action claims no SDK field at all: its schema is
			// site plus filter, all provider-owned. Counting it as a checked
			// surface would overstate this referee's reach, which is the kind of
			// false confidence it exists to remove.
			claimless++
			continue
		}

		file := conversionFile(policy)
		if file == "" {
			unchecked = append(unchecked, fmt.Sprintf(
				"%s (%s): no conversion file is defined for this surface kind, so its %d "+
					"claim(s) are not checked", policy.Resource, policy.SurfaceKind, len(claims)))
			continue
		}
		if _, err := os.Stat(file); err != nil {
			// Named rather than skipped: a policy whose conversion code cannot
			// be found is unchecked, and silence would read as checked.
			unchecked = append(unchecked, fmt.Sprintf(
				"%s: no conversion file at %s, so none of its renames are checked",
				policy.Resource, file))
			continue
		}
		surfaces++
		byName := paired[filepath.Base(file)]

		for _, c := range claims {
			targets, mentioned := byName[c.terraform]
			if !mentioned {
				unchecked = append(unchecked, fmt.Sprintf(
					"%s.%s (claims %s): the conversion code never pairs this attribute with an "+
						"SDK field directly", policy.Resource, c.terraform, c.structural))
				continue
			}
			checked++
			if targets[c.structural] {
				continue
			}
			contradicted = append(contradicted, fmt.Sprintf(
				"%s.%s claims %q but %s pairs it with %s",
				policy.Resource, c.terraform, c.structural,
				filepath.Base(file), strings.Join(sortedKeys(targets), " or ")))
		}
	}

	sort.Strings(contradicted)
	sort.Strings(unchecked)

	if len(contradicted) > 0 {
		t.Errorf("%d attribute(s) whose policy names a different SDK field than the resource "+
			"actually uses:\n    %s\n\n"+
			"    Each of these produces the correct Terraform schema and the wrong request.\n"+
			"    Neither the baseline projection nor the behaviour inventory can see it:\n"+
			"    the schema is identical whichever field backs the attribute.\n"+
			"    Read the model-to-SDK assignments and correct structural_name.",
			len(contradicted), strings.Join(contradicted, "\n    "))
	}

	if surfaces == 0 {
		t.Fatal("no policy was matched to a conversion file, so nothing was compared")
	}
	t.Logf("%d rename claim(s) checked against the conversion code across %d surface(s); "+
		"%d further surface(s) claim no SDK field at all and so have nothing to check",
		checked, surfaces, claimless)
	if len(unchecked) > 0 {
		t.Logf("%d claim(s) not checked, because the conversion code does not pair the "+
			"attribute with a field directly:\n    %s",
			len(unchecked), strings.Join(unchecked, "\n    "))
	}
}

// conversionFile maps a policy to the file that converts it, or "" when the
// kind has no such file.
//
// The kind is matched explicitly rather than defaulted. A list policy's
// generator_name is the MANAGED surface's name -- ap_group_list.json declares
// generator_name "ap_group" -- so falling through to "<name>_resource.go" would
// silently compare a list policy's claims against the managed resource's
// bindings. That is harmless today only because list policies omit every SDK
// field; it would become a false failure the moment one did not.
func conversionFile(policy policyDocument) string {
	name := policy.GeneratorName
	switch policy.SurfaceKind {
	case "managed_resource":
		return filepath.Join(".", name+"_resource.go")
	case "data_source":
		return filepath.Join(".", strings.TrimSuffix(name, "_ds")+"_data_source.go")
	default:
		return ""
	}
}

// claimsOf collects every managed attribute the policy says comes from a named
// SDK field, at any depth. An invented member claims no field and is skipped.
func claimsOf(policy policyDocument) []claim {
	var out []claim
	var walk func(fields []policyField)
	walk = func(fields []policyField) {
		for _, f := range fields {
			if f.Disposition == "managed" && f.Invented == "" &&
				f.StructuralName != "" && f.TerraformName != "" {
				out = append(out, claim{terraform: f.TerraformName, structural: f.StructuralName})
			}
			walk(f.Fields)
		}
	}
	walk(policy.Fields)
	for _, flattening := range policy.Flattenings {
		walk(flattening.Members)
	}
	for _, grouping := range policy.Groupings {
		for _, m := range grouping.Members {
			if m.Disposition == "managed" && m.Invented == "" && m.StructuralName != "" {
				out = append(out, claim{terraform: m.TerraformName, structural: m.StructuralName})
			}
		}
	}
	return out
}

func sortedKeys(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
