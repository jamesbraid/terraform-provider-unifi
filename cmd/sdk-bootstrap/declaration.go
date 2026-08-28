package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
)

// declarationBytes returns one struct's own declaration -- its doc comment,
// if any, through the closing brace -- not the whole file it happens to
// share with other declarations. cmd/sdk-bootstrap digests this slice, so a
// method added beside the struct (or an unrelated struct in the same file)
// no longer moves every policy pinned to it: a re-pin means the struct's
// fields, or the comments that carry its enum facts, actually changed.
func declarationBytes(filename, name string) ([]byte, error) {
	source, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", filename, err)
	}
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filename, source, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", filename, err)
	}
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.TYPE {
			continue
		}
		for _, spec := range gen.Specs {
			typeSpec, ok := spec.(*ast.TypeSpec)
			if !ok || typeSpec.Name.Name != name {
				continue
			}
			// An ungrouped decl (`type Foo struct{...}`) attaches its doc
			// comment to the GenDecl, not the TypeSpec; a grouped decl
			// (`type ( A struct{...}; B struct{...} )`) attaches a spec's own
			// doc to that spec, and the GenDecl's doc (if any) covers the
			// whole group -- so it must not be used to widen one member's slice.
			start := typeSpec.Pos()
			switch {
			case typeSpec.Doc != nil:
				start = typeSpec.Doc.Pos()
			case gen.Lparen == token.NoPos && gen.Doc != nil:
				start = gen.Doc.Pos()
			}
			return source[fset.Position(start).Offset:fset.Position(typeSpec.End()).Offset], nil
		}
	}
	return nil, fmt.Errorf("%s defines no type %s", filename, name)
}
