// Command generated-value-strip removes the value plumbing that
// tfplugingen-framework emits alongside each generated schema, and keeps the
// schema functions and nothing else.
//
// Selects files by their generator header, not by path: internal/generated also
// holds hand-written output whose metadatacontract.FrozenTypeNames IS referenced.
package main

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/tools/imports"
)

// generatorHeader is the marker tfplugingen-framework writes on its output. A
// file without it belongs to another generator and is left alone.
const generatorHeader = "terraform-plugin-framework-generator"

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
		fmt.Fprintln(stderr, "usage: generated-value-strip [-n] <generated-root>...")
		return 2
	}

	files, err := generatedFiles(args)
	if err != nil {
		fmt.Fprintf(stderr, "%v\n", err)
		return 1
	}
	// Zero matches is treated as a broken selector, not a clean tree -- otherwise
	// a generator header change would silently turn this into a no-op that reports success.
	if len(files) == 0 {
		fmt.Fprintf(stderr,
			"no file under %v carries the %q header; the selector is wrong and this "+
				"would report an untouched tree as a stripped one\n", args, generatorHeader)
		return 1
	}

	removed, kept := 0, 0
	for _, path := range files {
		r, k, err := stripFile(path, dryRun, stdout)
		if err != nil {
			fmt.Fprintf(stderr, "%v\n", err)
			return 1
		}
		removed += r
		kept += k
	}
	fmt.Fprintf(stdout,
		"generated-value-strip: kept %d schema function(s), removed %d unreferenced "+
			"declaration(s) across %d file(s)\n", kept, removed, len(files))
	return 0
}

func generatedFiles(roots []string) ([]string, error) {
	var files []string
	for _, root := range roots {
		err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error { // #nosec G703 -- root is a go:generate-supplied directory argument, not user input
			if err != nil {
				return err
			}
			if entry.IsDir() || !strings.HasSuffix(path, ".go") {
				return nil
			}
			source, err := os.ReadFile(path) // #nosec G304 G122 G703 -- path is discovered by walking a go:generate-supplied root, not user input
			if err != nil {
				return err
			}
			first, _, _ := strings.Cut(string(source), "\n")
			if strings.Contains(first, generatorHeader) {
				files = append(files, path)
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	sort.Strings(files)
	return files, nil
}

// stripFile keeps the schema functions and drops every other top-level
// declaration.
func stripFile(path string, dryRun bool, stdout io.Writer) (removed, kept int, err error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	if err != nil {
		return 0, 0, fmt.Errorf("%s: %w", path, err)
	}

	var keepers []ast.Decl
	dropped := map[string]bool{}
	for _, decl := range file.Decls {
		if isSchemaFunc(decl) {
			keepers = append(keepers, decl)
			kept++
			continue
		}
		if _, isImport := importDecl(decl); isImport {
			// Kept and pruned, not dropped and rebuilt: goimports can't tell which
			// of the framework's four `schema`-named packages a bare import resolves to.
			keepers = append(keepers, decl)
			continue
		}
		for _, name := range declaredNames(decl) {
			dropped[name] = true
		}
		removed++
	}
	if kept == 0 {
		return 0, 0, fmt.Errorf(
			"%s: carries the generator header and declares no schema function, so this "+
				"tool would empty it; the file is not the shape this tool understands", path)
	}

	// Catches what a whole-module reference check cannot: whether a surviving
	// function in THIS file names something this pass is about to drop.
	if used := namesUsedBy(keepers, dropped); len(used) > 0 {
		return 0, 0, fmt.Errorf(
			"%s: the schema function references %v, which this tool would remove; "+
				"the generator's output has changed shape and stripping it would not compile",
			path, used)
	}

	if dryRun {
		fmt.Fprintf(stdout, "%s: would remove %d declaration(s), keep %d\n", path, removed, kept)
		return removed, kept, nil
	}

	file.Decls = keepers
	// Comments belong to the dropped declarations; keeping them would attach a
	// value type's documentation to a schema function.
	file.Comments = nil

	var buffer bytes.Buffer
	if err := printer.Fprint(&buffer, fset, file); err != nil {
		return 0, 0, fmt.Errorf("%s: %w", path, err)
	}
	formatted, err := imports.Process(path, buffer.Bytes(), nil)
	if err != nil {
		return 0, 0, fmt.Errorf("%s: resolving imports: %w", path, err)
	}
	// Re-adds the generated marker goimports has no reason to keep: a generated
	// file that stops declaring itself as one gets hand-edited.
	output := append([]byte("// Code generated by "+generatorHeader+" DO NOT EDIT.\n// Value plumbing removed by cmd/generated-value-strip.\n\n"), formatted...)
	if err := os.WriteFile(path, output, 0o600); err != nil { // #nosec G703 -- path is discovered by walking a go:generate-supplied root, not user input
		return 0, 0, fmt.Errorf("%s: %w", path, err)
	}
	fmt.Fprintf(stdout, "%s: removed %d declaration(s), kept %d\n", path, removed, kept)
	return removed, kept, nil
}

func isSchemaFunc(decl ast.Decl) bool {
	fn, ok := decl.(*ast.FuncDecl)
	return ok && fn.Recv == nil && strings.HasSuffix(fn.Name.Name, "Schema")
}

func importDecl(decl ast.Decl) (*ast.GenDecl, bool) {
	gen, ok := decl.(*ast.GenDecl)
	return gen, ok && gen.Tok == token.IMPORT
}

func declaredNames(decl ast.Decl) []string {
	var names []string
	switch concrete := decl.(type) {
	case *ast.FuncDecl:
		if concrete.Recv == nil {
			names = append(names, concrete.Name.Name)
		}
	case *ast.GenDecl:
		for _, spec := range concrete.Specs {
			switch s := spec.(type) {
			case *ast.TypeSpec:
				names = append(names, s.Name.Name)
			case *ast.ValueSpec:
				for _, ident := range s.Names {
					names = append(names, ident.Name)
				}
			}
		}
	}
	return names
}

// namesUsedBy reports which of the dropped names the kept declarations mention.
func namesUsedBy(keepers []ast.Decl, dropped map[string]bool) []string {
	found := map[string]bool{}
	for _, decl := range keepers {
		ast.Inspect(decl, func(node ast.Node) bool {
			// A selector's field name is not a file-scope identifier, so only
			// the expression it selects from is examined.
			if selector, ok := node.(*ast.SelectorExpr); ok {
				ast.Inspect(selector.X, func(inner ast.Node) bool {
					if ident, ok := inner.(*ast.Ident); ok && dropped[ident.Name] {
						found[ident.Name] = true
					}
					return true
				})
				return false
			}
			if ident, ok := node.(*ast.Ident); ok && dropped[ident.Name] {
				found[ident.Name] = true
			}
			return true
		})
	}
	names := make([]string, 0, len(found))
	for name := range found {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
