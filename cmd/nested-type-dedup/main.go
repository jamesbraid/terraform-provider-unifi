// Command nested-type-dedup removes a nested object type that
// tfplugingen-framework declared twice, and refuses to do anything else.
//
// A custom_type on the attribute does not prevent this: every nested emitter
// calls NewCustomNestedObjectType unconditionally, so the redeclaration still happens.
package main

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os"
	"sort"
	"strings"
)

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: nested-type-dedup <generated.go>...")
		return 2
	}
	for _, path := range args {
		changed, err := dedupeFile(path)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		if changed > 0 {
			fmt.Fprintf(stdout, "%s: removed %d duplicate nested object type declaration(s)\n",
				path, changed)
		}
	}
	return 0
}

// declRun is a maximal consecutive group of top-level declarations that all
// belong to one nested object type, with the exact source bytes behind it.
type declRun struct {
	family    string
	startByte int
	endByte   int
	startLine int
}

func dedupeFile(path string) (int, error) {
	source, err := os.ReadFile(path) // #nosec G304 G703 -- path is a go:generate-supplied file argument, not user input
	if err != nil {
		return 0, err
	}
	runs, err := runsOf(path, source)
	if err != nil {
		return 0, err
	}

	byFamily := map[string][]declRun{}
	var order []string
	for _, r := range runs {
		if _, seen := byFamily[r.family]; !seen {
			order = append(order, r.family)
		}
		byFamily[r.family] = append(byFamily[r.family], r)
	}
	sort.Strings(order)

	var drop []declRun
	for _, family := range order {
		group := byFamily[family]
		if len(group) < 2 {
			continue
		}
		first := bytes.TrimRight(source[group[0].startByte:group[0].endByte], "\n")
		for _, later := range group[1:] {
			other := bytes.TrimRight(source[later.startByte:later.endByte], "\n")
			if !bytes.Equal(first, other) {
				return 0, fmt.Errorf(
					"%s: %sType/%sValue is declared twice and THE TWO DECLARATIONS DIFFER "+
						"(line %d and line %d).\n"+
						"    This pass may only delete an exact duplicate. A nested object type "+
						"encodes the nested attribute's names and types, so two that differ are "+
						"two different shapes sharing one name -- deleting either would silently "+
						"discard a real difference, and the survivor would still compile.\n"+
						"    Resolve it in the schema, not here: the two attributes must either "+
						"have the same nested shape or be given different names",
					path, family, family, group[0].startLine, later.startLine)
			}
			drop = append(drop, later)
		}
	}
	if len(drop) == 0 {
		return 0, nil
	}

	sort.Slice(drop, func(i, j int) bool { return drop[i].startByte > drop[j].startByte })
	out := source
	for _, r := range drop {
		out = append(out[:r.startByte:r.startByte], out[r.endByte:]...)
	}

	if _, err := parser.ParseFile(token.NewFileSet(), path, out, parser.SkipObjectResolution); err != nil {
		return 0, fmt.Errorf("%s: deduplicated output does not parse: %w", path, err)
	}
	if err := os.WriteFile(path, out, 0o600); err != nil { // #nosec G703 -- path is a go:generate-supplied file argument, not user input
		return 0, err
	}
	return len(drop), nil
}

// runsOf groups a file's top-level declarations into consecutive runs sharing
// one nested object type family.
func runsOf(path string, source []byte) ([]declRun, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, source, parser.ParseComments|parser.SkipObjectResolution)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}

	var runs []declRun
	for _, decl := range file.Decls {
		family := familyOf(decl)
		start := fset.Position(declStart(decl)).Offset
		end := fset.Position(decl.End()).Offset
		line := fset.Position(declStart(decl)).Line
		if family == "" {
			continue
		}
		// A fresh ObjectTypable assertion always starts a new emission, even for a
		// family already seen -- adjacency alone would merge duplicates and skip the divergence check.
		if n := len(runs); n > 0 && !opensEmission(decl) && runs[n-1].family == family &&
			onlyGapBetween(source, runs[n-1].endByte, start) {
			runs[n-1].endByte = end
			continue
		}
		runs = append(runs, declRun{family: family, startByte: start, endByte: end, startLine: line})
	}
	// A run ends at the last declaration; take the blank lines after it too, so
	// removing a run does not leave a double blank behind.
	for i := range runs {
		for runs[i].endByte < len(source) && source[runs[i].endByte] == '\n' {
			runs[i].endByte++
		}
	}
	return runs, nil
}

func onlyGapBetween(source []byte, from, to int) bool {
	return strings.TrimSpace(string(source[from:to])) == ""
}

// opensEmission reports whether a declaration is the marker the generator puts
// first when it emits a nested object type: `var _ basetypes.ObjectTypable = FooType{}`.
func opensEmission(decl ast.Decl) bool {
	d, ok := decl.(*ast.GenDecl)
	if !ok || d.Tok != token.VAR || len(d.Specs) != 1 {
		return false
	}
	spec, ok := d.Specs[0].(*ast.ValueSpec)
	if !ok || len(spec.Names) != 1 || spec.Names[0].Name != "_" || len(spec.Values) != 1 {
		return false
	}
	selector, ok := spec.Type.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	return selector.Sel.Name == "ObjectTypable"
}

func declStart(decl ast.Decl) token.Pos {
	switch d := decl.(type) {
	case *ast.GenDecl:
		if d.Doc != nil {
			return d.Doc.Pos()
		}
	case *ast.FuncDecl:
		if d.Doc != nil {
			return d.Doc.Pos()
		}
	}
	return decl.Pos()
}

// familyOf names the nested object type a declaration belongs to (Foo owns
// FooType, FooValue, their methods, assertions, and NewFooValue* constructors).
func familyOf(decl ast.Decl) string {
	switch d := decl.(type) {
	case *ast.FuncDecl:
		if d.Recv != nil && len(d.Recv.List) == 1 {
			return familyOfTypeName(receiverName(d.Recv.List[0].Type))
		}
		if name := strings.TrimPrefix(d.Name.Name, "New"); name != d.Name.Name {
			// NewFooValue, NewFooValueNull, NewFooValueUnknown, NewFooValueMust
			for _, suffix := range []string{"ValueNull", "ValueUnknown", "ValueMust", "Value"} {
				if trimmed := strings.TrimSuffix(name, suffix); trimmed != name && trimmed != "" {
					return trimmed
				}
			}
		}
	case *ast.GenDecl:
		switch d.Tok {
		case token.TYPE:
			if len(d.Specs) == 1 {
				if spec, ok := d.Specs[0].(*ast.TypeSpec); ok {
					return familyOfTypeName(spec.Name.Name)
				}
			}
		case token.VAR:
			// var _ basetypes.ObjectTypable = FooType{}
			if len(d.Specs) == 1 {
				if spec, ok := d.Specs[0].(*ast.ValueSpec); ok &&
					len(spec.Names) == 1 && spec.Names[0].Name == "_" && len(spec.Values) == 1 {
					if lit, ok := spec.Values[0].(*ast.CompositeLit); ok {
						if ident, ok := lit.Type.(*ast.Ident); ok {
							return familyOfTypeName(ident.Name)
						}
					}
				}
			}
		}
	}
	return ""
}

func familyOfTypeName(name string) string {
	for _, suffix := range []string{"Type", "Value"} {
		if trimmed := strings.TrimSuffix(name, suffix); trimmed != name && trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func receiverName(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.Ident:
		return e.Name
	case *ast.StarExpr:
		return receiverName(e.X)
	}
	return ""
}
