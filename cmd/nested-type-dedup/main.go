// Command nested-type-dedup removes a nested object type that
// tfplugingen-framework declared twice, and refuses to do anything else.
//
// WHY THIS EXISTS, and why the obvious fix does not work.
//
// The generator names the Go type it emits for a nested object from the
// ATTRIBUTE NAME, not from the attribute's path. wan's released schema has both
// dhcp.options and dhcpv6.options, so the generator emits `OptionsType` and
// `OptionsValue` twice into one package, and the package does not compile:
//
//	wan_resource_gen.go: OptionsType redeclared in this block
//
// `generate resources` has three flags -- input, output, package -- and no
// control over type naming.
//
// WHAT WAS TRIED FIRST, so nobody repeats it: declaring a custom_type on the
// nested object. It does not work and cannot be made to work from the policy.
// Every nested emitter in the framework calls NewCustomNestedObjectType(name)
// UNCONDITIONALLY -- there is no guard on CustomType in any of
// single_nested_attribute, list_nested_attribute, set_nested_attribute,
// map_nested_attribute or the three block forms. A custom_type changes which
// type the ATTRIBUTE USES; it does not stop the generator DECLARING its own,
// and a redeclaration is fatal whether or not the type is used. Upstream main
// (v0.4.2-0.20260414051213) differs from the pinned v0.4.1 only by a copyright
// header, so there is no version to move to either.
//
// WHY DELETING IS SOUND, and why this is not a rename tool. The two emitted
// declarations are not merely compatible, they are BYTE-IDENTICAL: 379 lines
// each. That is not luck. A generated object type encodes only the attribute
// NAMES and TYPES of the nested object, and the released contract gives
// dhcp.options and dhcpv6.options the same two children, option_number and
// value, at the same types. The hand-written provider already agreed, building
// both from one dhcpOptionModel. So one shared type is what the contract says,
// and emitting it once makes the generated code agree with the contract rather
// than imposing a convenience on it.
//
// Descriptions and validators are NOT part of these declarations -- they live
// in the schema literal, per attribute. dhcpv6.options.option_number carries an
// int64validator.OneOf that dhcp.options.option_number does not, and that
// difference survives this pass untouched, which is exactly why the pass may
// only ever delete an EXACT duplicate.
//
// THE GUARD IS THE WHOLE ARGUMENT. If the two declarations ever stop matching,
// deleting one would silently discard a real difference, and the survivor would
// still compile -- so nothing downstream could notice. This refuses on any
// difference and names both declarations and the file. There is no fuzzy
// match, no "close enough" and no first-wins: exact bytes or refuse.
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
	source, err := os.ReadFile(path)
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
				// Named rather than counted. A caller who only learns that
				// "a duplicate differed" has to go and find which one.
				return 0, fmt.Errorf(
					"%s: %sType/%sValue is declared twice and THE TWO DECLARATIONS DIFFER "+
						"(line %d and line %d).\n"+
						"    This pass may only delete an exact duplicate. A nested object type "+
						"encodes the nested attribute's names and types, so two that differ are "+
						"two different shapes sharing one name -- deleting either would silently "+
						"discard a real difference, and the survivor would still compile.\n"+
						"    Resolve it in the schema, not here: the two attributes must either "+
						"have the same nested shape or be given different names.",
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

	// Re-parse before writing. A pass that produces a file Go cannot read has
	// failed regardless of what it believed it was deleting.
	if _, err := parser.ParseFile(token.NewFileSet(), path, out, parser.SkipObjectResolution); err != nil {
		return 0, fmt.Errorf("%s: deduplicated output does not parse: %w", path, err)
	}
	if err := os.WriteFile(path, out, 0o644); err != nil {
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
		// Extend the previous run when it is the same family and nothing
		// unrelated came between them -- UNLESS this declaration opens a fresh
		// emission. The generator starts every nested object type with its
		// ObjectTypable assertion, so that marker is what separates two
		// emissions of one family; adjacency alone does not, and grouping on
		// adjacency merged two duplicates into a single run whenever nothing
		// happened to be emitted between them. That hid both the duplicate and
		// the divergence check, which is the one thing this pass may not do.
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
// first when it emits a nested object type: `var _ basetypes.ObjectTypable =
// FooType{}`. Seeing it again for a family already emitted is what makes a
// second emission a second emission rather than more of the first.
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

// familyOf reports which nested object type a declaration belongs to, by the
// naming the generator uses: a family Foo owns FooType, FooValue, their
// methods, their ObjectTypable/ObjectValuable assertions, and the
// NewFooValue... constructors. Anything else -- the schema function, the model
// struct -- belongs to no family and is never touched.
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
