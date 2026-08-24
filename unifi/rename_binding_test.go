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
// path is not compared -- renamecheck reports a bare tfsdk tag with no path.
type claim struct {
	terraform  string
	path       string
	structural string
}

// Test_policyRenamesMatchTheConversionCode checks that each policy's claimed
// SDK field matches what the conversion code actually binds. A schema
// comparison alone can't catch this: a wrong field produces the same schema.
func Test_policyRenamesMatchTheConversionCode(t *testing.T) {
	derived, err := renamecheck.Derive(".")
	if err != nil {
		t.Fatalf("deriving bindings from the conversion code: %v", err)
	}
	if len(derived.Bindings) == 0 {
		t.Fatal("no bindings were derived, so this comparison would pass against an empty set")
	}

	// file -> terraform name -> SDK fields it's paired with. A name used at
	// more than one path is ambiguous and handled by ambiguousNames below.
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
			// A list/action surface's schema is site+filter, all provider-owned,
			// so counting it as checked would overstate this test's reach.
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
		ambiguous := ambiguousNames(claims)

		for _, c := range claims {
			if at, shared := ambiguous[c.terraform]; shared {
				unchecked = append(unchecked, fmt.Sprintf(
					"%s.%s (claims %s): %d attributes share the terraform name %q (%s), and "+
						"the deriver reports a bare name, so no binding of it can be attributed "+
						"to one of them",
					policy.Resource, c.path, c.structural, len(at), c.terraform,
					strings.Join(at, ", ")))
				continue
			}
			targets, mentioned := byName[c.terraform]
			if !mentioned {
				unchecked = append(unchecked, fmt.Sprintf(
					"%s.%s (claims %s): the conversion code never pairs this attribute with an "+
						"SDK field directly", policy.Resource, c.path, c.structural))
				continue
			}
			checked++
			if targets[c.structural] {
				continue
			}
			contradicted = append(contradicted, fmt.Sprintf(
				"%s.%s claims %q but %s pairs it with %s",
				policy.Resource, c.path, c.structural,
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

	// checked is coverage only of what the deriver resolved; report both
	// counts so the unstated denominator isn't hidden.
	var ambiguous []string
	for _, u := range derived.Unread {
		if strings.Contains(u.Detail, "would be a guess") {
			ambiguous = append(ambiguous, u.File+": "+u.Detail)
		}
	}
	sort.Strings(ambiguous)
	t.Logf("the deriver resolved %d conversion(s) and declined %d; the checked count above "+
		"is a FLOOR on this referee's reach, not its extent",
		len(derived.Bindings), len(derived.Unread))
	if len(ambiguous) > 0 {
		// A field paired with several attributes (or the reverse) can't be
		// resolved without guessing the exact binding this test exists to catch.
		t.Logf("%d of those decline because the conversion names several candidates:\n    %s",
			len(ambiguous), strings.Join(ambiguous, "\n    "))
	}
}

// conversionFile maps a policy to the file that converts it, or "" when the
// kind has no such file.
//
// Matched explicitly, not defaulted: a list policy's generator_name is the
// managed surface's name, so falling through would compare its claims
// against the wrong resource's bindings.
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
	var walk func(prefix string, fields []policyField)
	walk = func(prefix string, fields []policyField) {
		for _, f := range fields {
			path := prefix + f.TerraformName
			if f.Disposition == "managed" && f.Invented == "" &&
				f.StructuralName != "" && f.TerraformName != "" {
				out = append(out, claim{
					terraform: f.TerraformName, path: path, structural: f.StructuralName,
				})
			}
			walk(path+".", f.Fields)
		}
	}
	walk("", policy.Fields)
	for _, flattening := range policy.Flattenings {
		walk("", flattening.Members)
	}
	for _, grouping := range policy.Groupings {
		for _, m := range grouping.Members {
			if m.Disposition == "managed" && m.Invented == "" && m.StructuralName != "" {
				out = append(out, claim{
					terraform:  m.TerraformName,
					path:       grouping.TerraformName + "." + m.TerraformName,
					structural: m.StructuralName,
				})
			}
		}
	}
	return out
}

// ambiguousNames reports which terraform names a policy uses at more than one
// path. renamecheck reports a bare tfsdk tag, so when two attributes share a
// name, a binding of it can't be attributed to either -- reported as
// unchecked rather than compared.
func ambiguousNames(claims []claim) map[string][]string {
	paths := map[string][]string{}
	for _, c := range claims {
		paths[c.terraform] = append(paths[c.terraform], c.path)
	}
	ambiguous := map[string][]string{}
	for name, at := range paths {
		unique := map[string]bool{}
		for _, path := range at {
			unique[path] = true
		}
		if len(unique) > 1 {
			ambiguous[name] = sortedKeys(unique)
		}
	}
	return ambiguous
}

func sortedKeys(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
