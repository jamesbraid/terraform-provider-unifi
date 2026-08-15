// Command nested-custom-type-strip removes the CustomType binding that
// tfplugingen-framework puts on every nested object attribute, and refuses to
// do anything else.
//
// WHY THIS EXISTS. The generator declares a Go type per nested object and binds
// the schema attribute to it: `CustomType: DhcpRelayType{...}`. Nothing in the
// provider can produce a value of those types -- there are ZERO references to
// any generated <X>Value or <X>Type from non-test runtime code -- because every
// runtime model carries the nested object as a plain types.Object. The
// framework then refuses the mismatch at apply time with a value conversion
// error. Fifty-two attributes across the estate are bound this way and not one
// is backed, which is why this is a generator-output problem rather than a
// per-surface oversight.
//
// WHY IT IS NOT FIXED AT THE GENERATOR. It cannot be.
// cmd/nested-type-dedup/main.go:16-25 already records the finding, under a
// heading asking that nobody repeat it: every nested emitter in the framework
// calls NewCustomNestedObjectType(name) UNCONDITIONALLY, there is no guard in
// any of the nested attribute or block forms, `generate resources` has three
// flags and none of them control this, and upstream main differs from the
// pinned version only by a copyright header. So the binding is removed after
// generation, exactly as the duplicate type declaration is.
//
// WHY REMOVING IT CANNOT MOVE THE PUBLIC SCHEMA. Measured, not reasoned: all
// fifty-four nested attributes carrying a CustomType were compared against the
// plain nested-object type the framework derives for the same attribute, and
// all fifty-four are identical at the tftypes level -- which is what the wire
// carries. A custom object type and a plain object of the same members are the
// same type to Terraform. That is also why every schema referee we own was
// blind to this: there is nothing in the protocol to see.
//
// THE TRAP THIS TOOL MUST NOT FALL INTO, and the reason it is this narrow.
// `CustomType:` is emitted for two unrelated purposes. The generated nested
// object types are one. The other is imported scalar types -- GoDurationType,
// MACAddressType, IPAddressType, IPv4AddressType, IPv4PrefixType -- and for
// those the custom type IS the validation. Dropping hwtypes.MACAddressType
// makes the provider accept any string as a MAC address, and every schema
// referee stays green because the protocol renders both as a plain string. That
// would trade fifty-two loud failures for a silent correctness regression.
//
// The two are told apart by DECLARATION SITE, not by name: a generated nested
// object type is declared in the same package it is used in and appears
// unqualified, an imported scalar type is package-qualified. This tool strips
// only the first and refuses anything it cannot classify.
package main

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: nested-custom-type-strip <generated-package-dir>...")
		return 2
	}
	stripped, kept := 0, 0
	for _, root := range args {
		dirs, err := packageDirs(root)
		if err != nil {
			fmt.Fprintf(stderr, "%v\n", err)
			return 1
		}
		for _, dir := range dirs {
			s, k, err := stripDir(dir)
			if err != nil {
				fmt.Fprintf(stderr, "%v\n", err)
				return 1
			}
			stripped += s
			kept += k
		}
	}
	fmt.Fprintf(stdout, "stripped %d generated nested-object CustomType bindings; kept %d imported scalar ones\n",
		stripped, kept)
	return 0
}

// packageDirs resolves one argument to the package directories under it.
//
// A single sweep over the generated root beats one wired line per surface: a
// per-surface line is a thing somebody forgets when they add a surface, and the
// binding it would have removed is invisible to every schema referee we own.
// This way a new generated package is covered the moment it exists.
func packageDirs(root string) ([]string, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".go") {
			return []string{root}, nil
		}
	}
	dirs := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			dirs = append(dirs, filepath.Join(root, entry.Name()))
		}
	}
	if len(dirs) == 0 {
		return nil, fmt.Errorf("%s: contains neither Go files nor package directories", root)
	}
	sort.Strings(dirs)
	return dirs, nil
}

// stripDir rewrites every .go file in one generated package.
//
// The package is parsed as a whole first, because whether a CustomType is
// generated or imported is decided by whether THIS package declares the type,
// and a type used in one file can be declared in another.
func stripDir(dir string) (int, int, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, 0, err
	}
	paths := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".go") {
			paths = append(paths, filepath.Join(dir, entry.Name()))
		}
	}
	sort.Strings(paths)
	if len(paths) == 0 {
		return 0, 0, fmt.Errorf("%s: no Go files", dir)
	}

	fileSet := token.NewFileSet()
	parsed := make(map[string]*ast.File, len(paths))
	sources := make(map[string][]byte, len(paths))
	declared := map[string]struct{}{}
	for _, path := range paths {
		source, err := os.ReadFile(path)
		if err != nil {
			return 0, 0, err
		}
		file, err := parser.ParseFile(fileSet, path, source, parser.ParseComments)
		if err != nil {
			return 0, 0, err
		}
		parsed[path] = file
		sources[path] = source
		for _, name := range declaredTypeNames(file) {
			declared[name] = struct{}{}
		}
	}

	stripped, kept := 0, 0
	for _, path := range paths {
		cuts, k, err := classify(fileSet, parsed[path], declared, path)
		if err != nil {
			return 0, 0, err
		}
		kept += k
		if len(cuts) == 0 {
			continue
		}
		out, err := splice(sources[path], cuts)
		if err != nil {
			return 0, 0, fmt.Errorf("%s: %w", path, err)
		}
		if err := os.WriteFile(path, out, 0o644); err != nil {
			return 0, 0, err
		}
		stripped += len(cuts)
	}
	return stripped, kept, nil
}

func declaredTypeNames(file *ast.File) []string {
	names := make([]string, 0)
	for _, declaration := range file.Decls {
		general, ok := declaration.(*ast.GenDecl)
		if !ok || general.Tok != token.TYPE {
			continue
		}
		for _, spec := range general.Specs {
			if typeSpec, ok := spec.(*ast.TypeSpec); ok {
				names = append(names, typeSpec.Name.Name)
			}
		}
	}
	return names
}

// cut is a half-open byte range to remove.
type cut struct{ start, end int }

// classify decides, for every CustomType in one file, whether it is a generated
// nested object binding to remove or an imported scalar type to keep.
//
// It REFUSES on anything it cannot place. A post-processor that silently skips
// a shape it does not recognise is a check that cannot fail: the binding would
// survive, the apply would still break, and the run would look clean.
func classify(
	fileSet *token.FileSet,
	file *ast.File,
	declared map[string]struct{},
	path string,
) ([]cut, int, error) {
	cuts := make([]cut, 0)
	kept := 0
	var refusal error
	ast.Inspect(file, func(node ast.Node) bool {
		if refusal != nil {
			return false
		}
		pair, ok := node.(*ast.KeyValueExpr)
		if !ok {
			return true
		}
		key, ok := pair.Key.(*ast.Ident)
		if !ok || key.Name != "CustomType" {
			return true
		}
		where := fileSet.Position(pair.Pos())
		literal, ok := pair.Value.(*ast.CompositeLit)
		if !ok {
			refusal = fmt.Errorf(
				"%s:%d:%d: CustomType is a %T, not a composite literal; refusing rather than guessing",
				path, where.Line, where.Column, pair.Value)
			return false
		}
		switch typeExpr := literal.Type.(type) {
		case *ast.SelectorExpr:
			// Package-qualified: an imported scalar custom type. This is the
			// validation for MAC addresses, durations and IP addresses. Never
			// touch it.
			kept++
			return false
		case *ast.Ident:
			if _, local := declared[typeExpr.Name]; !local {
				refusal = fmt.Errorf(
					"%s:%d:%d: CustomType %s is unqualified but this package does not declare it; "+
						"refusing because it cannot be classified",
					path, where.Line, where.Column, typeExpr.Name)
				return false
			}
			cuts = append(cuts, cut{
				start: fileSet.Position(pair.Pos()).Offset,
				end:   fileSet.Position(pair.End()).Offset,
			})
			return false
		default:
			refusal = fmt.Errorf(
				"%s:%d:%d: CustomType has an unrecognised type expression %T; refusing",
				path, where.Line, where.Column, literal.Type)
			return false
		}
	})
	if refusal != nil {
		return nil, 0, refusal
	}
	return cuts, kept, nil
}

// splice removes each cut along with the comma and blank line it leaves behind,
// so the result is what the generator would have emitted rather than what it
// emitted minus some bytes.
func splice(source []byte, cuts []cut) ([]byte, error) {
	sort.Slice(cuts, func(a, b int) bool { return cuts[a].start > cuts[b].start })
	out := append([]byte(nil), source...)
	for _, c := range cuts {
		if c.start < 0 || c.end > len(out) || c.start >= c.end {
			return nil, fmt.Errorf("cut [%d,%d) is outside the file", c.start, c.end)
		}
		end := c.end
		// The element's trailing comma, then the rest of that line, then the
		// newline: a struct field occupies a whole line in gofmt output.
		if end < len(out) && out[end] == ',' {
			end++
		}
		for end < len(out) && (out[end] == ' ' || out[end] == '\t') {
			end++
		}
		if end < len(out) && out[end] == '\n' {
			end++
		}
		// And the indentation that preceded it, so no blank line is left.
		start := c.start
		for start > 0 && (out[start-1] == ' ' || out[start-1] == '\t') {
			start--
		}
		out = append(out[:start], out[end:]...)
	}
	if bytes.Contains(out, []byte("CustomType: \n")) {
		return nil, fmt.Errorf("a cut left a dangling CustomType key")
	}
	return out, nil
}
