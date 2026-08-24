package unifi

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// The policy file and the surfaces on the kit must agree in both
// directions: a missing closure doesn't compile, but missing data just
// isn't there, so a check that only asks "does every policy entry name a
// real surface" lets a surface drop out of generation with everything
// green, and one that only asks "does every surface have an entry" lets
// the file accumulate entries for surfaces that no longer exist. Each
// failure names the surface, since "the policy is out of sync" sends the
// next person to read the whole file.

type descriptorPolicy struct {
	Surfaces map[string]struct {
		SDKType       string            `json:"sdk_type"`
		DurationUnits map[string]string `json:"duration_units"`
		SchemaVersion int               `json:"schema_version"`
	} `json:"surfaces"`
}

func loadDescriptorPolicy(t *testing.T) descriptorPolicy {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "provider-codegen", "policy", "resource-decisions.json"))
	if err != nil {
		t.Fatalf("reading the policy: %v", err)
	}
	var policy descriptorPolicy
	if err := json.Unmarshal(raw, &policy); err != nil {
		t.Fatalf("parsing the policy: %v", err)
	}
	if len(policy.Surfaces) == 0 {
		t.Fatal("the policy declares no surfaces, so both directions below would pass vacuously")
	}
	return policy
}

// kitServedSurfaces finds every type in this package that embeds
// resourcekit.Resource, and reads the Terraform type name off its Metadata.
func kitServedSurfaces(t *testing.T) map[string]string {
	t.Helper()
	fset := token.NewFileSet()
	embeds := map[string]bool{}
	names := map[string]string{}

	files, _ := filepath.Glob("*.go")
	parsed := make([]*ast.File, 0, len(files))
	for _, path := range files {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Fatalf("parsing %s: %v", path, err)
		}
		parsed = append(parsed, file)
	}

	for _, file := range parsed {
		ast.Inspect(file, func(n ast.Node) bool {
			spec, ok := n.(*ast.TypeSpec)
			if !ok {
				return true
			}
			structure, ok := spec.Type.(*ast.StructType)
			if !ok {
				return true
			}
			for _, field := range structure.Fields.List {
				if len(field.Names) != 0 {
					continue // embedded fields have no name
				}
				index, ok := field.Type.(*ast.IndexListExpr)
				if !ok {
					continue
				}
				selector, ok := index.X.(*ast.SelectorExpr)
				if !ok {
					continue
				}
				pkg, ok := selector.X.(*ast.Ident)
				if ok && pkg.Name == "resourcekit" && selector.Sel.Name == "Resource" {
					embeds[spec.Name.Name] = true
				}
			}
			return true
		})
	}

	for _, file := range parsed {
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Name.Name != "Metadata" || fn.Recv == nil || fn.Body == nil {
				continue
			}
			star, ok := fn.Recv.List[0].Type.(*ast.StarExpr)
			if !ok {
				continue
			}
			ident, ok := star.X.(*ast.Ident)
			if !ok || !embeds[ident.Name] {
				continue
			}
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				assign, ok := n.(*ast.AssignStmt)
				if !ok || len(assign.Lhs) != 1 || len(assign.Rhs) != 1 {
					return true
				}
				sel, ok := assign.Lhs[0].(*ast.SelectorExpr)
				if !ok || sel.Sel.Name != "TypeName" {
					return true
				}
				if binary, ok := assign.Rhs[0].(*ast.BinaryExpr); ok {
					if lit, ok := binary.Y.(*ast.BasicLit); ok {
						names["unifi"+strings.Trim(lit.Value, `"`)] = ident.Name
					}
				}
				return true
			})
		}
	}

	if len(embeds) == 0 {
		t.Fatal("no type in this package embeds resourcekit.Resource; the detector is broken, " +
			"not the tree -- an empty result here would pass both directions below")
	}
	if len(names) != len(embeds) {
		t.Errorf("%d type(s) embed the kit but %d declare a Metadata TypeName; a kit-served "+
			"surface with no Metadata in this package cannot be matched against its policy "+
			"entry here",
			len(embeds), len(names))
	}
	return names
}

func TestEveryKitSurfaceHasAPolicyEntry(t *testing.T) {
	policy := loadDescriptorPolicy(t)
	served := kitServedSurfaces(t)

	var missing []string
	for name := range served {
		if _, ok := policy.Surfaces[name]; !ok {
			missing = append(missing, name)
		}
	}
	sort.Strings(missing)
	for _, name := range missing {
		t.Errorf("%s is served by the kit and has no entry in "+
			"provider-codegen/policy/resource-decisions.json", name)
	}
}

func TestEveryPolicyEntryNamesAKitSurface(t *testing.T) {
	policy := loadDescriptorPolicy(t)
	served := kitServedSurfaces(t)

	var stale []string
	for name := range policy.Surfaces {
		if _, ok := served[name]; !ok {
			stale = append(stale, name)
		}
	}
	sort.Strings(stale)
	for _, name := range stale {
		t.Errorf("the policy carries an entry for %s, which no type in this package serves "+
			"from the kit; an entry for a surface that does not exist describes nothing", name)
	}
}

// TestEveryPolicyEntryDeclaresAnSDKType stops an entry existing but saying
// nothing, which would satisfy both directions above while carrying no decision.
func TestEveryPolicyEntryDeclaresAnSDKType(t *testing.T) {
	policy := loadDescriptorPolicy(t)
	for name, entry := range policy.Surfaces {
		if strings.TrimSpace(entry.SDKType) == "" {
			t.Errorf("%s has a policy entry with no sdk_type, so it decides nothing", name)
		}
	}
}
