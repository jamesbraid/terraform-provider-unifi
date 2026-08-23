// Command generated-value-strip removes the value plumbing that
// tfplugingen-framework emits alongside each generated schema, and keeps the
// schema functions and nothing else.
//
// WHY THIS EXISTS. The generator writes, per surface, one <X>Schema function
// and then an entire value layer for every nested object in it: a Go type, a
// types.Object-shaped Value, four constructors, an AttributeTypes method, and
// the attr.Value implementation. NOTHING IN THE PROVIDER CALLS ANY OF IT. Every
// runtime model carries a nested object as a plain types.Object, and the
// framework builds those from the schema.
//
// MEASURED WITH go/types RATHER THAN BY GREP, because a name search cannot tell
// a reference from a coincidence. Loading every package in the module and
// walking TypesInfo.Uses: 65 distinct objects declared in internal/generated
// are referenced from outside it, and ALL SIXTY-FIVE ARE *Schema FUNCTIONS. The
// test packages add exactly one more, metadatacontract.FrozenTypeNames, which
// this tool does not touch.
//
// WHY IT IS NOT FIXED AT THE GENERATOR. The same reason its two siblings are
// not: tfplugingen-framework emits the value layer unconditionally, `generate
// resources` has no flag for it, and the pinned version differs from upstream
// main only by a copyright header. cmd/nested-type-dedup and
// cmd/nested-custom-type-strip already rewrite this generator's output after
// the fact for the same reason, and this is the third step of that pattern.
//
// WHY REMOVING IT CANNOT MOVE THE PUBLIC SCHEMA. The kept function is the whole
// of what the provider serves, and it is kept byte for byte. Everything removed
// is unreachable from it -- the tool REFUSES rather than guessing if a kept
// function names anything it is about to drop, so "unreachable" is checked per
// file rather than assumed once. The proof beyond that is the artifact: the
// provider binary's symbol table is compared before and after.
//
// WHICH FILES. Only those whose first line names
// terraform-plugin-framework-generator. internal/generated also holds output
// from cmd/list-resource-gen, cmd/action-gen and cmd/metadata-contract-gen, and
// all three are hand-written emitters that produce no value layer -- and
// metadatacontract exports FrozenTypeNames, which IS referenced. Keying on the
// generator's own header rather than on a path or a filename suffix is what
// keeps this tool pointed at the one generator it understands.
package main

import (
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

	"bytes"

	"golang.org/x/tools/imports"
)

// generatorHeader is the marker tfplugingen-framework writes on its output. A
// file without it belongs to another generator and is left alone.
const generatorHeader = "terraform-plugin-framework-generator"

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	// -n lists what would go and writes nothing, for the reason
	// nested-custom-type-strip gives: the list a reader audits has to come from
	// the parse that does the work rather than from a second implementation.
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
	// A SWEEP THAT MATCHED NOTHING IS A BROKEN SWEEP, NOT A CLEAN TREE. The
	// header is the only thing selecting files, so a generator that changes it
	// would silently turn this into a no-op that reports success.
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
		err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() || !strings.HasSuffix(path, ".go") {
				return nil
			}
			source, err := os.ReadFile(path)
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
			// THE IMPORT BLOCK IS KEPT AND PRUNED, NOT DROPPED AND REBUILT.
			// Dropping it and letting goimports re-derive resolved `schema` to
			// resource/schema in the data-source files, where it means
			// datasource/schema -- four packages in this framework export a
			// `schema` identifier and the file is the only thing that knows
			// which one it meant. goimports removes what the survivors no
			// longer use and leaves what they do.
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

	// THE REFUSAL, AND IT IS THE WHOLE WARRANT PER FILE. "Nothing references the
	// value layer" is a measurement over the module; this checks the one thing
	// that measurement cannot see -- whether the surviving function names
	// something in the same file. A build error three steps later would say the
	// same thing far less usefully.
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
	// The doc comment and any floating comment belong to declarations that are
	// gone; keeping them would attach a value type's documentation to a schema
	// function.
	file.Comments = nil

	var buffer bytes.Buffer
	if err := printer.Fprint(&buffer, fset, file); err != nil {
		return 0, 0, fmt.Errorf("%s: %w", path, err)
	}
	formatted, err := imports.Process(path, buffer.Bytes(), nil)
	if err != nil {
		return 0, 0, fmt.Errorf("%s: resolving imports: %w", path, err)
	}
	// The generated marker goes back on the front: goimports has no reason to
	// keep a comment this tool deliberately cleared, and a generated file that
	// stops saying so is one someone edits by hand.
	output := append([]byte("// Code generated by "+generatorHeader+" DO NOT EDIT.\n// Value plumbing removed by cmd/generated-value-strip.\n\n"), formatted...)
	if err := os.WriteFile(path, output, 0o644); err != nil {
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
