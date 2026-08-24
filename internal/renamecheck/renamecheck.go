// Package renamecheck reads which SDK field each Terraform attribute is
// actually written to and read from, out of a resource's own conversion
// code. A wrong rename is the one mistake no other referee can see: both
// compare the released schema, and a misbound attribute produces a
// byte-identical schema while still reading and writing the wrong field on
// the controller.
//
// It reports the pairs it can see and names what it couldn't read; it never
// guesses. A conversion routed through a helper, a switch or a loop is
// reported as unread rather than resolved, since a wrong answer here would
// license exactly the bind this package exists to catch.
package renamecheck

import (
	"fmt"
	"go/ast"
	"go/types"
	"reflect"
	"sort"
	"strings"

	"golang.org/x/tools/go/packages"
)

// Binding pairs one Terraform attribute with the SDK field the conversion code
// reads it from or writes it to.
type Binding struct {
	// File is the resource's own source file, which is how a binding is
	// attributed to a policy.
	File string
	// TerraformName is the tfsdk tag on the model field.
	TerraformName string
	// StructuralName is the json tag on the SDK field: the same name a policy
	// calls structural_name and a bootstrap calls name.
	StructuralName string
	// SDKType is the SDK struct the field belongs to, so a surface fronting a
	// shared struct can be told apart from one that does not.
	SDKType string
}

// Unread records a conversion this package could not resolve, with the
// reason. It is a floor on what was skipped, not a ceiling: a conversion
// this package never visits at all (composite is walked only for literals
// of go-unifi structs) produces neither a binding nor an Unread. What the
// result names is right; what it omits is not evidence of absence.
type Unread struct {
	File   string
	Detail string
}

// Result is what one package walk produced.
type Result struct {
	Bindings []Binding
	Unread   []Unread
}

// sdkModulePath is the SDK whose structs count as the far side of a conversion.
const sdkModulePath = "github.com/ubiquiti-community/go-unifi/unifi"

// Derive loads the package in dir and reports every attribute-to-SDK-field pair
// it can read.
//
// Type information rather than syntax alone: telling the model side of an
// assignment from the SDK side by variable name would be guesswork, and the
// names vary per resource (model, data, plan, state on one side; network, route,
// portProfile on the other). The types say it exactly -- one side is a struct
// with tfsdk tags, the other is a struct from go-unifi.
func Derive(dir string) (Result, error) {
	cfg := &packages.Config{
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedSyntax |
			packages.NeedTypes | packages.NeedTypesInfo | packages.NeedDeps |
			packages.NeedImports | packages.NeedCompiledGoFiles,
		Dir: dir,
	}
	loaded, err := packages.Load(cfg, ".")
	if err != nil {
		return Result{}, fmt.Errorf("load %s: %w", dir, err)
	}
	if len(loaded) != 1 {
		return Result{}, fmt.Errorf("load %s: got %d packages, want 1", dir, len(loaded))
	}
	pkg := loaded[0]
	if len(pkg.Errors) > 0 {
		return Result{}, fmt.Errorf("load %s: %v", dir, pkg.Errors[0])
	}

	w := &walker{pkg: pkg, seen: map[Binding]bool{}, skipped: map[Unread]bool{}}
	for _, file := range pkg.Syntax {
		// The file name comes from the position rather than from
		// CompiledGoFiles, which is only populated under a mode flag this does
		// not otherwise need and whose order is not promised to match Syntax.
		w.file = shortName(pkg.Fset.Position(file.Pos()).Filename)
		ast.Inspect(file, w.visit)
	}

	sort.Slice(w.result.Bindings, func(i, j int) bool {
		a, b := w.result.Bindings[i], w.result.Bindings[j]
		if a.File != b.File {
			return a.File < b.File
		}
		if a.TerraformName != b.TerraformName {
			return a.TerraformName < b.TerraformName
		}
		return a.StructuralName < b.StructuralName
	})
	sort.Slice(w.result.Unread, func(i, j int) bool {
		a, b := w.result.Unread[i], w.result.Unread[j]
		if a.File != b.File {
			return a.File < b.File
		}
		return a.Detail < b.Detail
	})
	return w.result, nil
}

type walker struct {
	pkg     *packages.Package
	file    string
	result  Result
	seen    map[Binding]bool
	skipped map[Unread]bool
}

func (w *walker) visit(node ast.Node) bool {
	switch n := node.(type) {
	case *ast.AssignStmt:
		w.assignment(n)
	case *ast.CompositeLit:
		w.composite(n)
	}
	return true
}

// assignment reads `sdk.Field = <expr mentioning model.Attr>` and the reverse,
// `model.Attr = <expr mentioning sdk.Field>`. Both directions are recorded: a
// surface may only write a field on create and only read it on refresh, and
// either one establishes the pairing.
func (w *walker) assignment(stmt *ast.AssignStmt) {
	for index, lhs := range stmt.Lhs {
		if index >= len(stmt.Rhs) {
			return
		}
		rhs := stmt.Rhs[index]

		if structural, sdkType, ok := w.sdkField(lhs); ok {
			candidates := w.terraformNamesIn(rhs)
			if terraform, ok := sole(candidates); ok {
				w.record(terraform, structural, sdkType)
			} else {
				w.skip(sdkType+"."+structural, "written from", "model attribute", candidates)
			}
			continue
		}
		if terraform, ok := w.terraformField(lhs); ok {
			names, kinds := w.sdkFieldsIn(rhs)
			if structural, ok := sole(names); ok {
				w.record(terraform, structural, kinds[0])
			} else {
				w.skip(terraform, "read from", "SDK field", names)
			}
		}
	}
}

// skip records one conversion this package declined to resolve.
//
// The two reasons are kept apart because they mean different things. NO
// candidate is not a binding at all -- a constant, or a local the value was
// staged through, which is how every many-into-one conversion in this provider
// is written. SEVERAL candidates IS a binding, and the one this package must
// not resolve: taking the first would invent exactly the pairing it exists to
// catch.
func (w *walker) skip(subject, direction, kind string, candidates []string) {
	var detail string
	if len(candidates) == 0 {
		detail = fmt.Sprintf(
			"%s is %s an expression naming no %s: a constant, or a value staged through a local",
			subject, direction, kind)
	} else {
		detail = fmt.Sprintf(
			"%s is %s an expression naming %d %ss (%s); resolving it would be a guess",
			subject, direction, len(candidates), kind, strings.Join(candidates, ", "))
	}
	entry := Unread{File: w.file, Detail: detail}
	if w.skipped[entry] {
		return
	}
	w.skipped[entry] = true
	w.result.Unread = append(w.result.Unread, entry)
}

// composite reads `&unifi.Thing{Field: model.Attr.ValueString()}`, which is how
// most resources build the request in one expression.
func (w *walker) composite(lit *ast.CompositeLit) {
	named, structure := w.sdkStruct(w.pkg.TypesInfo.TypeOf(lit))
	if structure == nil {
		return
	}
	for _, element := range lit.Elts {
		kv, ok := element.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		key, ok := kv.Key.(*ast.Ident)
		if !ok {
			continue
		}
		structural, ok := jsonNameOf(structure, key.Name)
		if !ok {
			continue
		}
		candidates := w.terraformNamesIn(kv.Value)
		if terraform, ok := sole(candidates); ok {
			w.record(terraform, structural, named)
		} else {
			w.skip(named+"."+structural, "written from", "model attribute", candidates)
		}
	}
}

func (w *walker) record(terraform, structural, sdkType string) {
	binding := Binding{
		File:           w.file,
		TerraformName:  terraform,
		StructuralName: structural,
		SDKType:        sdkType,
	}
	if w.seen[binding] {
		return
	}
	w.seen[binding] = true
	w.result.Bindings = append(w.result.Bindings, binding)
}

// sdkField reports the json name of `x.Field` when x is a go-unifi struct.
func (w *walker) sdkField(expr ast.Expr) (string, string, bool) {
	selector, ok := expr.(*ast.SelectorExpr)
	if !ok {
		return "", "", false
	}
	named, structure := w.sdkStruct(w.pkg.TypesInfo.TypeOf(selector.X))
	if structure == nil {
		return "", "", false
	}
	name, ok := jsonNameOf(structure, selector.Sel.Name)
	return name, named, ok
}

// terraformField reports the tfsdk tag of `x.Field` when x is a struct carrying
// tfsdk tags -- which is what a Terraform model is and nothing else in this
// package is.
func (w *walker) terraformField(expr ast.Expr) (string, bool) {
	selector, ok := expr.(*ast.SelectorExpr)
	if !ok {
		return "", false
	}
	structure := structOf(w.pkg.TypesInfo.TypeOf(selector.X))
	if structure == nil {
		return "", false
	}
	return tagOf(structure, selector.Sel.Name, "tfsdk")
}

// terraformNamesIn returns every model attribute an expression mentions.
//
// The caller pairs only when there is exactly one. That is the point:
// `model.A.ValueString()` is a pairing; `a + b` or `choose(model.A, model.B)` is
// not one this package may resolve, and silently taking the first would invent a
// binding. Returning the whole list rather than a sole-or-nothing answer is what
// lets a skip say WHICH names it saw, so a gap is named rather than silent.
func (w *walker) terraformNamesIn(expr ast.Expr) []string {
	var found []string
	ast.Inspect(expr, func(node ast.Node) bool {
		if selector, ok := node.(*ast.SelectorExpr); ok {
			if name, ok := w.terraformField(selector); ok {
				found = append(found, name)
			}
		}
		return true
	})
	return found
}

func (w *walker) sdkFieldsIn(expr ast.Expr) ([]string, []string) {
	var names, kinds []string
	ast.Inspect(expr, func(node ast.Node) bool {
		if selector, ok := node.(*ast.SelectorExpr); ok {
			if name, kind, ok := w.sdkField(selector); ok {
				names = append(names, name)
				kinds = append(kinds, kind)
			}
		}
		return true
	})
	return names, kinds
}

func sole(found []string) (string, bool) {
	if len(found) == 0 {
		return "", false
	}
	first := found[0]
	for _, name := range found[1:] {
		if name != first {
			return "", false
		}
	}
	return first, true
}

// sdkStruct returns the struct behind a type when it comes from go-unifi.
func (w *walker) sdkStruct(t types.Type) (string, *types.Struct) {
	if t == nil {
		return "", nil
	}
	if pointer, ok := t.Underlying().(*types.Pointer); ok {
		t = pointer.Elem()
	}
	named, ok := t.(*types.Named)
	if !ok {
		if pointer, ok := t.(*types.Pointer); ok {
			named, ok = pointer.Elem().(*types.Named)
			if !ok {
				return "", nil
			}
		} else {
			return "", nil
		}
	}
	if named.Obj().Pkg() == nil || named.Obj().Pkg().Path() != sdkModulePath {
		return "", nil
	}
	structure, ok := named.Underlying().(*types.Struct)
	if !ok {
		return "", nil
	}
	return named.Obj().Name(), structure
}

func structOf(t types.Type) *types.Struct {
	if t == nil {
		return nil
	}
	if pointer, ok := t.(*types.Pointer); ok {
		t = pointer.Elem()
	}
	structure, _ := t.Underlying().(*types.Struct)
	return structure
}

func jsonNameOf(structure *types.Struct, field string) (string, bool) {
	name, ok := tagOf(structure, field, "json")
	if !ok {
		return "", false
	}
	return strings.SplitN(name, ",", 2)[0], true
}

func tagOf(structure *types.Struct, field, key string) (string, bool) {
	for index := range structure.NumFields() {
		if structure.Field(index).Name() != field {
			continue
		}
		value := reflect.StructTag(structure.Tag(index)).Get(key)
		value = strings.SplitN(value, ",", 2)[0]
		if value == "" || value == "-" {
			return "", false
		}
		return value, true
	}
	return "", false
}

func shortName(path string) string {
	if index := strings.LastIndex(path, "/"); index >= 0 {
		return path[index+1:]
	}
	return path
}
