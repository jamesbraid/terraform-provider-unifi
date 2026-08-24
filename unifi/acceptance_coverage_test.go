package unifi

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
)

// coverageIndex is the acceptance corpus read as a call graph rather than a
// pile of files: a per-function scan of string literals undercounts badly,
// since most configs come from a helper (e.g.
// testAccAPGroupFrameworkConfig_basic("...")) and the HCL lives one call
// away.
type coverageIndex struct {
	// functions is every function declared in the package, test files included,
	// keyed by name. Methods are excluded: a config helper is never one here,
	// and including them would collide on names like String.
	functions map[string]*ast.FuncDecl
	// constants is every package-level string binding, so a config held in a
	// const is reachable by the same walk as one held in a helper.
	constants map[string]string
	// file names the file each function was declared in, for reporting.
	file map[string]string
}

var (
	// declaredResource matches an HCL managed-resource block header. Data
	// sources are `data "unifi_x"` and do not match, which is the point: this
	// asks which surfaces a test CREATES.
	declaredResource = regexp.MustCompile(`resource\s+"(unifi_[a-z0-9_]+)"`)
	// checkedAddress matches the resource address a check names. The address is
	// always the first argument of a TestCheckResourceAttr* call or the value
	// of a ResourceName field, and both are plain string literals here.
	checkedAddress = regexp.MustCompile(`^(unifi_[a-z0-9_]+)\.[A-Za-z0-9_]`)
)

// TestRankTheAcceptanceCoverageGap answers a question the coverage counts
// cannot: not how many surfaces have a TestAcc function, but which surfaces
// nothing ever creates and asserts on.
//
// The two differ: a test that seeds a network to attach a firewall policy
// to it creates a network, crediting unifi_network in a count of "surfaces
// appearing in acceptance HCL" -- but asserts nothing about it, so a wrong
// network would still pass. Coverage that only creates is coverage against
// a panic, not a defect.
//
// So two sets are collected per surface: what a test declares, and what a
// test checks. The ranking is over the second.
func TestRankTheAcceptanceCoverageGap(t *testing.T) {
	ctx := context.Background()
	served := servedManagedTypes(ctx, t)
	if len(served) == 0 {
		t.Fatal("the provider served no managed resources, so every count below would be zero for the wrong reason")
	}

	index := indexPackage(t)
	if len(index.functions) == 0 {
		t.Fatal("no functions parsed, so nothing could be found either way")
	}

	acceptance := 0
	declaredBy := map[string][]string{}
	checkedBy := map[string][]string{}
	for name := range index.functions {
		if !strings.HasPrefix(name, "TestAcc") {
			continue
		}
		acceptance++
		declares, checks := index.surfaces(name)
		for _, surface := range declares {
			declaredBy[surface] = append(declaredBy[surface], name)
		}
		for _, surface := range checks {
			checkedBy[surface] = append(checkedBy[surface], name)
		}
	}
	if acceptance == 0 {
		t.Fatal("no TestAcc functions were found; the walk is not reaching the corpus, " +
			"so an uncovered surface would read the same as a covered one")
	}

	// Every surface a test names must be one the provider actually serves: a
	// name that isn't served is either an HCL typo (which terraform would
	// catch, but only if the test ran) or a surface removed and left behind.
	for surface := range declaredBy {
		if !served[surface] {
			t.Errorf("an acceptance config declares resource %q, which this provider does not serve: %v",
				surface, sortedUnique(declaredBy[surface]))
		}
	}
	for surface := range checkedBy {
		if !served[surface] {
			t.Errorf("an acceptance check names an address on %q, which this provider does not serve: %v",
				surface, sortedUnique(checkedBy[surface]))
		}
	}

	// Each control below names a specific fact, so a walk that silently
	// stopped reaching the corpus fails here rather than reporting a
	// clean sweep. The indirection control is the load-bearing one:
	// ap_group's config is only ever produced by a helper, so it can only
	// be found through a call.
	for _, control := range []struct {
		test    string
		surface string
		why     string
	}{
		{
			"TestAccFirewallPolicyFramework_basic", "unifi_firewall_policy",
			"its config is a literal in the test body, so a literal scan alone must find it",
		},
		{
			"TestAccAPGroupFramework_basic", "unifi_ap_group",
			"its config comes from testAccAPGroupFrameworkConfig_basic, so only a call walk finds it",
		},
	} {
		if _, ok := index.functions[control.test]; !ok {
			t.Errorf("control %s is missing from the corpus; it anchored the claim that %s",
				control.test, control.why)
			continue
		}
		declares, _ := index.surfaces(control.test)
		if !contains(declares, control.surface) {
			t.Errorf("control failed: %s does not resolve to %s, and %s.\n"+
				"    it resolved to %v",
				control.test, control.surface, control.why, declares)
		}
	}

	var noTest, createdOnly []string
	for surface := range served {
		switch {
		case len(checkedBy[surface]) > 0:
		case len(declaredBy[surface]) > 0:
			createdOnly = append(createdOnly, surface)
		default:
			noTest = append(noTest, surface)
		}
	}
	sort.Strings(noTest)
	sort.Strings(createdOnly)

	// The ranking itself, weakest first. A surface with one checker has one
	// test standing between it and a silent regression; the census above only
	// separates zero from non-zero.
	ranked := make([]string, 0, len(served))
	for surface := range served {
		ranked = append(ranked, surface)
	}
	sort.Slice(ranked, func(i, j int) bool {
		if len(checkedBy[ranked[i]]) != len(checkedBy[ranked[j]]) {
			return len(checkedBy[ranked[i]]) < len(checkedBy[ranked[j]])
		}
		return ranked[i] < ranked[j]
	})
	for _, surface := range ranked {
		t.Logf("  %2d checking %2d declaring  %s  %v",
			len(sortedUnique(checkedBy[surface])), len(sortedUnique(declaredBy[surface])),
			surface, sortedUnique(checkedBy[surface]))
	}

	t.Logf("%d served managed resources, %d TestAcc functions across the package",
		len(served), acceptance)
	t.Logf("NOTHING CREATES THESE %d: %s", len(noTest), strings.Join(noTest, " "))
	t.Logf("CREATED BUT NEVER ASSERTED ON, %d: %s", len(createdOnly), strings.Join(createdOnly, " "))
	for _, surface := range createdOnly {
		t.Logf("    %s is seeded by %v and checked by nothing", surface, sortedUnique(declaredBy[surface]))
	}
}

// servedManagedTypes asks the provider itself, rather than reading a list.
//
// A hand-kept list of surfaces is the thing that goes stale first, and a census
// keyed off one would report a surface as covered by never mentioning it.
func servedManagedTypes(ctx context.Context, t *testing.T) map[string]bool {
	t.Helper()
	provider := &unifiProvider{}
	served := map[string]bool{}
	for _, newResource := range provider.Resources(ctx) {
		res := newResource()
		var meta resource.MetadataResponse
		res.Metadata(ctx, resource.MetadataRequest{ProviderTypeName: "unifi"}, &meta)
		served[meta.TypeName] = true
	}
	return served
}

// indexPackage parses every Go file in the package, tests included.
func indexPackage(t *testing.T) *coverageIndex {
	t.Helper()
	set := token.NewFileSet()
	paths, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) == 0 {
		t.Fatal("no Go files in the package directory, so the walk would find nothing")
	}
	index := &coverageIndex{
		functions: map[string]*ast.FuncDecl{},
		constants: map[string]string{},
		file:      map[string]string{},
	}
	for _, path := range paths {
		file, err := parser.ParseFile(set, path, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		{
			for _, decl := range file.Decls {
				switch typed := decl.(type) {
				case *ast.FuncDecl:
					if typed.Recv != nil || typed.Body == nil {
						continue
					}
					index.functions[typed.Name.Name] = typed
					index.file[typed.Name.Name] = path
				case *ast.GenDecl:
					for _, spec := range typed.Specs {
						value, ok := spec.(*ast.ValueSpec)
						if !ok {
							continue
						}
						for i, name := range value.Names {
							if i >= len(value.Values) {
								continue
							}
							if literal, ok := stringLiteral(value.Values[i]); ok {
								index.constants[name.Name] = literal
							}
						}
					}
				}
			}
		}
	}
	return index
}

// surfaces returns the managed surfaces a test declares and the ones it checks,
// following calls into the same package so a config held in a helper is found.
//
// A check inside a step that sets ExpectError is NOT counted. The framework
// returns a failing check as an error and matches it against the same pattern
// (testing_new_config.go:238, testing_new.go:465), so those assertions cannot
// fail the test and crediting them would report coverage that does not exist.
func (c *coverageIndex) surfaces(entry string) (declares, checks []string) {
	declaredSet := map[string]bool{}
	checkedSet := map[string]bool{}
	visited := map[string]bool{}

	var walk func(name string)
	walk = func(name string) {
		if visited[name] {
			return
		}
		visited[name] = true
		decl, ok := c.functions[name]
		if !ok || decl.Body == nil {
			return
		}
		shadowed := expectErrorRanges(decl.Body)
		inShadow := func(pos token.Pos) bool {
			for _, span := range shadowed {
				if pos >= span[0] && pos < span[1] {
					return true
				}
			}
			return false
		}
		ast.Inspect(decl.Body, func(node ast.Node) bool {
			switch typed := node.(type) {
			case *ast.BasicLit:
				if literal, ok := stringLiteral(typed); ok {
					for _, match := range declaredResource.FindAllStringSubmatch(literal, -1) {
						declaredSet[match[1]] = true
					}
				}
			case *ast.Ident:
				// A config held in a package-level const is reachable the same
				// way a call is.
				if literal, ok := c.constants[typed.Name]; ok {
					for _, match := range declaredResource.FindAllStringSubmatch(literal, -1) {
						declaredSet[match[1]] = true
					}
				}
			case *ast.CallExpr:
				if callee := calleeName(typed.Fun); callee != "" {
					if strings.HasPrefix(callee, "TestCheckResourceAttr") && len(typed.Args) > 0 &&
						!inShadow(typed.Pos()) {
						if address, ok := c.resolveString(typed.Args[0]); ok {
							if match := checkedAddress.FindStringSubmatch(address); match != nil {
								checkedSet[match[1]] = true
							}
						}
					}
					walk(callee)
				}
			case *ast.KeyValueExpr:
				key, ok := typed.Key.(*ast.Ident)
				if !ok || key.Name != "ResourceName" || inShadow(typed.Pos()) {
					return true
				}
				if address, ok := c.resolveString(typed.Value); ok {
					if match := checkedAddress.FindStringSubmatch(address); match != nil {
						checkedSet[match[1]] = true
					}
				}
			}
			return true
		})
	}
	walk(entry)

	return sortedKeys(declaredSet), sortedKeys(checkedSet)
}

// resolveString reads a string literal, a package-level const, or the leading
// literal of a concatenation -- which is enough, because a checked address is
// written as "unifi_x.test" or as "unifi_x.test" + suffix.
func (c *coverageIndex) resolveString(expr ast.Expr) (string, bool) {
	switch typed := expr.(type) {
	case *ast.BasicLit:
		return stringLiteral(typed)
	case *ast.Ident:
		value, ok := c.constants[typed.Name]
		return value, ok
	case *ast.BinaryExpr:
		return c.resolveString(typed.X)
	}
	return "", false
}

func calleeName(fun ast.Expr) string {
	switch typed := fun.(type) {
	case *ast.Ident:
		return typed.Name
	case *ast.SelectorExpr:
		return typed.Sel.Name
	}
	return ""
}

func stringLiteral(expr ast.Expr) (string, bool) {
	literal, ok := expr.(*ast.BasicLit)
	if !ok || literal.Kind != token.STRING {
		return "", false
	}
	value, err := strconv.Unquote(literal.Value)
	if err != nil {
		return "", false
	}
	return value, true
}

func sortedUnique(values []string) []string {
	seen := map[string]bool{}
	for _, value := range values {
		seen[value] = true
	}
	return sortedKeys(seen)
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

// TestNoAcceptanceStepExpectsAnyError is the gap the coverage census does
// not find, because the surfaces it names DO have acceptance tests.
//
// ExpectError makes a step's success conditional on the apply failing; a
// pattern of ".*" is satisfied by any message, so the step passes on every
// failure and fails only when everything works.
//
// It also swallows the checks: testStepNewConfig runs step.Check and
// returns its failure as an error (testing_new_config.go:238), which
// testing_new.go:465 then matches against the same pattern, so a ".*"
// pattern matches a check failure as happily as a controller rejection.
//
// A pattern that names the message is a real assertion and is left alone --
// network_resource_test.go's "Conflicting network purpose" is one.
func TestNoAcceptanceStepExpectsAnyError(t *testing.T) {
	index := indexPackage(t)
	found, vacuous := 0, []string{}
	// Patterns that match every possible error message, so the step asserts
	// only that something went wrong.
	matchesAnything := map[string]bool{".*": true, ".+": true, "": true, "(?s).*": true}

	for name, decl := range index.functions {
		if !strings.HasPrefix(name, "TestAcc") || decl.Body == nil {
			continue
		}
		ast.Inspect(decl.Body, func(node ast.Node) bool {
			pair, ok := node.(*ast.KeyValueExpr)
			if !ok {
				return true
			}
			key, ok := pair.Key.(*ast.Ident)
			if !ok || key.Name != "ExpectError" {
				return true
			}
			found++
			call, ok := pair.Value.(*ast.CallExpr)
			if !ok || len(call.Args) == 0 {
				return true
			}
			pattern, ok := index.resolveString(call.Args[0])
			if !ok {
				return true
			}
			if matchesAnything[pattern] {
				vacuous = append(vacuous, name+" expects "+strconv.Quote(pattern))
			}
			return true
		})
	}

	// Without this the check passes by finding nothing, which is also what a
	// renamed field or a helper-built TestStep would look like.
	if found == 0 {
		t.Fatal("no ExpectError step was found at all; the walk is not reaching the corpus, " +
			"so a vacuous pattern would go unreported")
	}
	sort.Strings(vacuous)
	if len(vacuous) > 0 {
		t.Errorf("%d acceptance step(s) expect an error that any message satisfies:\n    %s\n\n"+
			"    Such a step passes when the apply fails for ANY reason, and passes again when\n"+
			"    step.Check fails, because a check failure is returned as an error and matched\n"+
			"    against the same pattern. It fails only when the surface works. Name the\n"+
			"    message the step is really asserting, or delete the step and let the test\n"+
			"    assert what it creates.",
			len(vacuous), strings.Join(vacuous, "\n    "))
	}
	t.Logf("%d ExpectError step(s), each naming a specific message", found)
}

// expectErrorRanges returns the source span of every composite literal that
// sets ExpectError, so checks written inside one can be recognised as
// unreachable.
func expectErrorRanges(body *ast.BlockStmt) [][2]token.Pos {
	var spans [][2]token.Pos
	ast.Inspect(body, func(node ast.Node) bool {
		literal, ok := node.(*ast.CompositeLit)
		if !ok {
			return true
		}
		for _, element := range literal.Elts {
			pair, ok := element.(*ast.KeyValueExpr)
			if !ok {
				continue
			}
			if key, ok := pair.Key.(*ast.Ident); ok && key.Name == "ExpectError" {
				spans = append(spans, [2]token.Pos{literal.Pos(), literal.End()})
				break
			}
		}
		return true
	})
	return spans
}
