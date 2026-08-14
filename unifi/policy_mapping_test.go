package unifi

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// mappingPolicy is the part of a policy this test reads: the function names a
// multi-field member says relate its attribute to the observed fields.
type mappingPolicy struct {
	Resource  string `json:"resource"`
	Groupings []struct {
		TerraformName string `json:"terraform_name"`
		Members       []struct {
			TerraformName   string   `json:"terraform_name"`
			StructuralNames []string `json:"structural_names"`
			Mapping         *struct {
				ToAPI   string `json:"to_api"`
				FromAPI string `json:"from_api"`
			} `json:"mapping"`
		} `json:"members"`
	} `json:"groupings"`
}

// Test_policyMappingsNameFunctionsThatExist makes a mapping a checkable claim
// rather than prose.
//
// The compiler cannot see how a provider relates one attribute to several
// observed fields, so the policy names the two functions that do it and the
// compiler takes the names on trust. That trust is the whole design -- a named
// function is something a reader can open -- and until this test existed nothing
// checked the name could be opened at all. A mapping naming
// `vpnClientPeerToNetwork` when the function is called `peerToNetwork` compiles,
// generates a correct schema, and leaves a reader chasing a function that is not
// there.
//
// Deliberately narrow: this checks the name is DECLARED, not that it does what
// the policy claims. Nothing can check the second, which is why the first is
// worth having.
func Test_policyMappingsNameFunctionsThatExist(t *testing.T) {
	declared := declaredFunctionNames(t, ".")

	// The lookup is proven on known answers FIRST, because the corpus is
	// allowed to be empty. A test that only iterates over policies would pass
	// with an empty set and with a broken parser alike, which is the shape of
	// defect this repository keeps finding: a check that cannot fail.
	if !declared["collectNonEmptyStrings"] {
		t.Fatal("the function index does not contain collectNonEmptyStrings, which " +
			"network_data_source.go declares; every check below would pass vacuously")
	}
	if !declared["parseWireGuardBase64Config"] {
		t.Fatal("the function index does not contain parseWireGuardBase64Config, which " +
			"vpn_client_resource.go declares; the index is not reading the package")
	}
	if declared["thisFunctionIsNotDeclaredAnywhere"] {
		t.Fatal("the function index reports an undeclared name as present, so it would " +
			"never refuse a mapping")
	}

	policies, err := filepath.Glob(filepath.Join(policyDir, "*.json"))
	if err != nil {
		t.Fatalf("listing policies: %v", err)
	}

	var missing []string
	mappings := 0
	for _, path := range policies {
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("reading %s: %v", path, err)
		}
		var policy mappingPolicy
		if err := json.Unmarshal(body, &policy); err != nil || policy.Resource == "" {
			continue
		}
		for _, grouping := range policy.Groupings {
			for _, member := range grouping.Members {
				if member.Mapping == nil {
					continue
				}
				owner := fmt.Sprintf("%s %s.%s",
					policy.Resource, grouping.TerraformName, member.TerraformName)
				for _, named := range []struct{ half, name string }{
					{"to_api", member.Mapping.ToAPI},
					{"from_api", member.Mapping.FromAPI},
				} {
					mappings++
					if named.name == "" || declared[named.name] {
						continue
					}
					missing = append(missing, fmt.Sprintf(
						"%s names %s %q, which package unifi does not declare",
						owner, named.half, named.name))
				}
			}
		}
	}

	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("%d mapping function name(s) that cannot be opened:\n    %s\n\n"+
			"    A mapping is taken on trust by the compiler because nothing can verify\n"+
			"    what the function does. That is only defensible while the name resolves\n"+
			"    to something a reader can read.",
			len(missing), strings.Join(missing, "\n    "))
	}
	t.Logf("%d mapping function name(s) checked against %d function(s) declared in package unifi",
		mappings, len(declared))
}

// declaredFunctionNames indexes every function and method declared in the
// package's non-test files. Methods are indexed by their bare name: a policy
// names a function, not a receiver, and a conversion is as often a method as
// not.
func declaredFunctionNames(t *testing.T, dir string) map[string]bool {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading %s: %v", dir, err)
	}
	fset := token.NewFileSet()
	names := map[string]bool{}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, filepath.Join(dir, name), nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parsing %s: %v", name, err)
		}
		for _, decl := range file.Decls {
			if function, ok := decl.(*ast.FuncDecl); ok {
				names[function.Name.Name] = true
			}
		}
	}
	return names
}
