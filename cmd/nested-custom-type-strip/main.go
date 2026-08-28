// Command nested-custom-type-strip removes the CustomType binding that
// tfplugingen-framework puts on every nested object attribute, and refuses to
// do anything else.
//
// Safe because none of the generated nested-object types override TerraformType
// (the method that decides the wire representation), so this changes only the Go type.
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
	// -n lists what would be removed without writing anything.
	dryRun := false
	if len(args) > 0 && args[0] == "-n" {
		dryRun, args = true, args[1:]
	}
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: nested-custom-type-strip [-n] <generated-package-dir>...")
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
			s, k, err := stripDir(dir, dryRun, stdout)
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

// stripDir rewrites every .go file in one generated package. It parses the whole
// package before classifying: a nested type used in one file can be declared in another.
func stripDir(dir string, dryRun bool, stdout io.Writer) (int, int, error) {
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
		source, err := os.ReadFile(path) // #nosec G304 G703 -- path is built from a go:generate-supplied directory argument, not user input
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
		if dryRun {
			for _, c := range cuts {
				where := fileSet.Position(token.Pos(0))
				_ = where
				fmt.Fprintf(stdout, "%s\t%s\n", path, c.what)
			}
			stripped += len(cuts)
			continue
		}
		out, err := splice(sources[path], cuts)
		if err != nil {
			return 0, 0, fmt.Errorf("%s: %w", path, err)
		}
		if err := os.WriteFile(path, out, 0o600); err != nil { // #nosec G703 -- path is built from a go:generate-supplied directory argument, not user input
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
type cut struct {
	start, end int
	// what names the binding, for -n's output.
	what string
}

// classify tells generated nested-object types from imported scalar ones by
// declaration site, not name, and refuses anything it can't place.
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
			// Package-qualified: imported scalar type (MAC/duration/IP address
			// validation) -- never strip these.
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
				what:  fmt.Sprintf("%s (line %d)", typeExpr.Name, where.Line),
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

// splice removes each cut, including its trailing comma and leading indentation.
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
