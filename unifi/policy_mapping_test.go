package unifi

import (
	"bytes"
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
// claim says relate its Terraform members to the observed fields.
//
// The names live at .claims[].mapping. They were read from
// .groupings[].members[].mapping until the format moved, and nothing noticed:
// a struct that decodes no such key yields a zero-length slice, the loop below
// ran zero times, and the test reported success. The unreachable check below
// is what makes that same move fail next time rather than pass.
type mappingPolicy struct {
	Resource string `json:"resource"`
	Claims   []struct {
		TerraformMembers []string `json:"terraform_members"`
		StructuralNames  []string `json:"structural_names"`
		Mapping          *struct {
			ToAPI   string `json:"to_api"`
			FromAPI string `json:"from_api"`
		} `json:"mapping"`
	} `json:"claims"`
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

	var missing, unreachable []string
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

		found := 0
		for _, claim := range policy.Claims {
			if claim.Mapping == nil {
				continue
			}
			// The file name is part of the owner because it is not redundant:
			// network.json and network_ds.json both declare resource
			// "unifi_network", and several of their claims cover the same
			// members with different functions. Without it the two collapse
			// into duplicate lines naming no file anyone can open.
			owner := fmt.Sprintf("%s (%s) %s",
				policy.Resource, filepath.Base(path), strings.Join(claim.TerraformMembers, "+"))
			for _, named := range []struct{ half, name string }{
				{"to_api", claim.Mapping.ToAPI},
				{"from_api", claim.Mapping.FromAPI},
			} {
				found++
				mappings++
				if named.name == "" || declared[named.name] {
					continue
				}
				missing = append(missing, fmt.Sprintf(
					"%s names %s %q, which package unifi does not declare",
					owner, named.half, named.name))
			}
		}

		// The denominator is derived from the corpus rather than counted by
		// hand. A file that carries the key but yields nothing means the struct
		// above no longer reaches it -- which is exactly how this test came to
		// check nothing at all -- and it is named here rather than skipped.
		if found == 0 && bytes.Contains(body, []byte(`"mapping"`)) {
			unreachable = append(unreachable, filepath.Base(path))
		}
	}

	sort.Strings(missing)
	sort.Strings(unreachable)

	// Before reporting on what was checked, establish that anything was. A
	// mapping that moves to another key is invisible to the decode, and an
	// empty corpus and a clean one are the same green run.
	if len(unreachable) > 0 {
		t.Errorf("%d polic(ies) carry a \"mapping\" key that mappingPolicy does not reach:\n    %s\n\n"+
			"    The names are still there and are no longer being checked. Point the\n"+
			"    struct at wherever they moved to; do not delete this check.",
			len(unreachable), strings.Join(unreachable, "\n    "))
	}
	if mappings == 0 {
		t.Fatal("no mapping was read from any policy, so every check below would pass " +
			"vacuously; mappingPolicy expects the names at .claims[].mapping")
	}

	if len(missing) > 0 {
		t.Errorf("%d of %d mapping function name(s) cannot be opened:\n    %s\n\n"+
			"    A mapping is taken on trust by the compiler because nothing can verify\n"+
			"    what the function does. That is only defensible while the name resolves\n"+
			"    to something a reader can read.",
			len(missing), mappings, strings.Join(missing, "\n    "))
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
