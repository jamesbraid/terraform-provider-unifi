package unifi

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// Everything in a *_descriptor_gen.go is unexported and exists for one
// reader: the hand descriptor beside it, or an instrument in this package.
// A generated declaration nothing names is therefore not a spare part, it is
// a generator emitting something it should not -- and it is invisible from
// every other direction. golangci-lint's unused linter is off over generated
// files (.golangci.yaml sets exclusions.generated: lax), which is exactly
// where an unused-declaration check has no other backstop, and a hand
// deletion cannot stand in: the next go generate puts it straight back and
// the byte-identity gate fails.
//
// Three such declarations survived the emitter's first release this way:
// wlanMacFilterAttrTypes, shadowed by an AttributeTypes() method the hand
// descriptor declares, and power_supervisor's power-sources model and
// attr-type map, orphaned by a hand ObjectListField carrying its own.
func TestEveryGeneratedDescriptorDeclarationIsReachable(t *testing.T) {
	declared, declaredIn := generatedDescriptorDeclarations(t)
	uses := packageIdentifierUses(t)

	var unreferenced []string
	for _, name := range declared {
		if uses[name] > 1 {
			continue
		}
		unreferenced = append(unreferenced, declaredIn[name]+" declares "+name+
			", which nothing in package unifi names")
	}
	if len(unreferenced) > 0 {
		t.Errorf("%d generated declaration(s) nothing can reach:\n    %s\n\n"+
			"    Each is emitted source that compiles, ships and means nothing. Fix the\n"+
			"    emitter so it stops writing them -- deleting them by hand fails the next\n"+
			"    go generate -- and regenerate.",
			len(unreferenced), strings.Join(unreferenced, "\n    "))
	}
}

// TestGeneratedDescriptorReachCountsRealUses is the walk's control. The tree
// is expected to be clean, so the two things worth proving are that the
// counter can tell a used declaration from an unused one at all.
func TestGeneratedDescriptorReachCountsRealUses(t *testing.T) {
	declared, _ := generatedDescriptorDeclarations(t)
	uses := packageIdentifierUses(t)

	if len(declared) < 100 {
		t.Fatalf("only %d generated declaration(s) were collected, so the walk is not "+
			"reading the emitted files", len(declared))
	}

	// A generated Fields constructor is named by the hand descriptor beside
	// it, so its count has to be above the one its own declaration donates.
	const used = "wlanGenFields"
	if uses[used] < 2 {
		t.Errorf("%s counts %d use(s), so a name with real call sites reads as unreferenced "+
			"and the check would report the whole package", used, uses[used])
	}
	if uses["thisIdentifierIsDeclaredNowhereInPackageUnifi"] != 0 {
		t.Error("an identifier nothing declares counts uses, so the check cannot tell " +
			"a reachable declaration from an unreachable one")
	}
}

// generatedDescriptorDeclarations returns every top-level name the emitter
// writes, and the file each came from.
func generatedDescriptorDeclarations(t *testing.T) (names []string, file map[string]string) {
	t.Helper()
	paths, err := filepath.Glob("*_descriptor_gen.go")
	if err != nil {
		t.Fatalf("globbing *_descriptor_gen.go: %v", err)
	}
	if len(paths) == 0 {
		t.Fatal("no generated descriptor was found, so every emitted declaration would " +
			"go unchecked and this test would pass having read nothing")
	}

	file = map[string]string{}
	fset := token.NewFileSet()
	for _, path := range paths {
		parsed, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parsing %s: %v", path, err)
		}
		for _, decl := range parsed.Decls {
			switch typed := decl.(type) {
			case *ast.FuncDecl:
				if typed.Recv == nil {
					names = append(names, typed.Name.Name)
					file[typed.Name.Name] = path
				}
			case *ast.GenDecl:
				for _, spec := range typed.Specs {
					switch shaped := spec.(type) {
					case *ast.TypeSpec:
						names = append(names, shaped.Name.Name)
						file[shaped.Name.Name] = path
					case *ast.ValueSpec:
						for _, name := range shaped.Names {
							names = append(names, name.Name)
							file[name.Name] = path
						}
					}
				}
			}
		}
	}
	sort.Strings(names)
	return names, file
}

// packageIdentifierUses counts every identifier occurrence in the package,
// test files included. A selector's right-hand name is skipped so that
// pkg.Thing cannot donate a count to a local Thing.
func packageIdentifierUses(t *testing.T) map[string]int {
	t.Helper()
	paths, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("globbing *.go: %v", err)
	}
	if len(paths) == 0 {
		t.Fatal("no Go file was found in the package directory")
	}

	uses := map[string]int{}
	fset := token.NewFileSet()
	for _, path := range paths {
		parsed, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parsing %s: %v", path, err)
		}
		ast.Inspect(parsed, func(node ast.Node) bool {
			switch typed := node.(type) {
			case *ast.Ident:
				uses[typed.Name]++
			case *ast.SelectorExpr:
				// Counted once by the Ident case above and taken back here,
				// so that pkg.Thing donates nothing to a local Thing. The
				// walk still descends into X.
				uses[typed.Sel.Name]--
			}
			return true
		})
	}
	return uses
}
