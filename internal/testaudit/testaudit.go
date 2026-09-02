// Package testaudit finds test functions that cannot fail. A test that
// cannot fail is worse than a missing one: a missing test is an absence a
// reader can see, while a passing test that exercises nothing reads as
// coverage to the reviewer, the release checklist, and everyone after them.
//
// Three shapes are detected, and they are not equally bad:
//
//	EmptyTable    a table-driven test whose table is empty, so the body
//	              never runs. This is the gotests scaffold, committed
//	              unfilled.
//	SkipStub      an unconditional t.Skip, with no condition that could
//	              ever make it run.
//	NoAssertion   a populated table that calls the code under test and
//	              checks nothing. The worst of the three: it executes, so
//	              it produces coverage and a green PASS while asserting
//	              nothing at all.
//
// Assertion reachability is transitive through same-package helpers: a test
// whose only assertion lives in a helper it calls does assert. A method call
// resolves only against the receiver's type, and only when that type is
// knowable from the syntax alone; a call whose receiver cannot be pinned down
// is judged non-asserting. Fail closed: a bare name must never donate an
// assertion verdict across unrelated types.
package testaudit

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Kind is which shape of unfailable test was found.
type Kind string

const (
	EmptyTable  Kind = "empty-table"
	SkipStub    Kind = "skip-stub"
	NoAssertion Kind = "no-assertion"
)

// Finding is one test that cannot fail.
//
// It deliberately carries no line number. A line number would make this file
// churn on every edit anywhere above the test, and the inventory built from it
// would be rewritten constantly -- which is how a ratchet stops being read.
type Finding struct {
	File string // repository-relative
	Name string
	Kind Kind
}

// String is the finding line format: stable, one per line.
func (f Finding) String() string {
	return fmt.Sprintf("%s\t%s\t%s", f.File, f.Name, f.Kind)
}

// methods on *testing.T that can actually fail a test. Skip and Log are
// deliberately absent: a test that only logs cannot fail, and that is the
// point of the audit.
//
// "Error" is the one name here that collides with something ubiquitous: the
// error interface's own Error() string. Matching it on any receiver scored
// every function containing err.Error() as asserting, and reachability is
// transitive, so a test that checked nothing passed this audit as long as some
// production function it called formatted an error. See errorIsFailMethod.
var failMethods = map[string]bool{
	"Error": true, "Errorf": true,
	"Fatal": true, "Fatalf": true,
	"Fail": true, "FailNow": true,
}

// Assertion helper packages. require.NoError(t, err) fails the test without
// ever naming a fail method.
var assertPackages = map[string]bool{"require": true, "assert": true}

// testingParams collects the parameter names declared as *testing.T, *testing.B,
// *testing.F or testing.TB, so a call on one can be told from a call on an error.
func testingParams(decl *ast.FuncDecl) map[string]bool {
	names := map[string]bool{}
	if decl.Type.Params == nil {
		return names
	}
	for _, field := range decl.Type.Params.List {
		t := field.Type
		if star, ok := t.(*ast.StarExpr); ok {
			t = star.X
		}
		sel, ok := t.(*ast.SelectorExpr)
		if !ok {
			continue
		}
		if id, ok := sel.X.(*ast.Ident); !ok || id.Name != "testing" {
			continue
		}
		switch sel.Sel.Name {
		case "T", "B", "F", "TB":
			for _, n := range field.Names {
				names[n.Name] = true
			}
		}
	}
	return names
}

// errorIsFailMethod reports whether a call to .Error() is t.Error and not
// err.Error. Only a bare identifier receiver is judged: h.t.Error(...) keeps
// counting, since nothing there proves it is an error value.
func errorIsFailMethod(fn *ast.SelectorExpr, params map[string]bool) bool {
	id, ok := fn.X.(*ast.Ident)
	if !ok {
		return true
	}
	return params[id.Name]
}

// skipDirs are never descended into. testdata is here because this package's
// own fixtures are deliberately unfailable tests; scanning them would put them
// in the inventory and make the fixtures indistinguishable from findings.
var skipDirs = map[string]bool{".git": true, "vendor": true, "build": true, "testdata": true}

type funcInfo struct {
	decl    *ast.FuncDecl
	pkgDir  string
	recv    string // receiver type name, "" for a free function
	file    string
	isTest  bool
	asserts int // -1 unknown, 0 no, 1 yes
}

// Scan walks root and returns every test function that cannot fail, sorted.
//
// A directory named testdata is skipped, but only on the way down: passing a
// path inside one as root scans it, which is what this package's own tests do.
func Scan(root string) ([]Finding, error) {
	fset := token.NewFileSet()
	funcs := map[string]*funcInfo{}
	var order []string

	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			if skipDirs[filepath.Base(path)] {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		parsed, perr := parser.ParseFile(fset, path, nil, 0)
		if perr != nil {
			// A file that does not parse is not evidence of a passing test.
			// Report it rather than counting it as clean.
			return fmt.Errorf("parsing %s: %w", path, perr)
		}
		pkgDir := filepath.Dir(path)
		for _, decl := range parsed.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok {
				continue
			}
			recv := ""
			if fd.Recv != nil && len(fd.Recv.List) > 0 {
				recv = receiverName(fd.Recv.List[0].Type)
			}
			key := pkgDir + "|" + recv + "|" + fd.Name.Name
			funcs[key] = &funcInfo{
				decl:   fd,
				pkgDir: pkgDir,
				recv:   recv,
				file:   path,
				// TestMain is a harness, not a test. It has no assertions by
				// design and counting it would put a permanent false entry at
				// the top of the inventory.
				isTest: strings.HasSuffix(path, "_test.go") &&
					strings.HasPrefix(fd.Name.Name, "Test") &&
					fd.Name.Name != "TestMain" &&
					recv == "",
				asserts: -1,
			}
			order = append(order, key)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	resolver := &assertResolver{funcs: funcs, visiting: map[*funcInfo]bool{}}

	var findings []Finding
	for _, key := range order {
		fi := funcs[key]
		if !fi.isTest {
			continue
		}
		rel := fi.file
		if r, rerr := filepath.Rel(root, fi.file); rerr == nil {
			rel = r
		}
		switch {
		case isSkipStub(fi.decl):
			findings = append(findings, Finding{File: rel, Name: fi.decl.Name.Name, Kind: SkipStub})
		case !resolver.asserts(fi):
			findings = append(findings, Finding{File: rel, Name: fi.decl.Name.Name, Kind: NoAssertion})
		case rangesOverEmptyTable(fi.decl):
			findings = append(findings, Finding{File: rel, Name: fi.decl.Name.Name, Kind: EmptyTable})
		}
	}

	sort.Slice(findings, func(i, j int) bool {
		if findings[i].File != findings[j].File {
			return findings[i].File < findings[j].File
		}
		return findings[i].Name < findings[j].Name
	})
	return findings, nil
}

type assertResolver struct {
	funcs    map[string]*funcInfo // pkgDir|recvType|name; recvType "" for free functions
	visiting map[*funcInfo]bool
}

func (r *assertResolver) lookup(pkgDir, recv, name string) *funcInfo {
	return r.funcs[pkgDir+"|"+recv+"|"+name]
}

// asserts reports whether fi can fail the test, following calls into
// same-package helpers.
func (r *assertResolver) asserts(fi *funcInfo) bool {
	if fi.asserts >= 0 {
		return fi.asserts == 1
	}
	if r.visiting[fi] {
		// Recursion carries no new information on this path. Returning false
		// here cannot produce a false positive on its own: the caller still
		// inspects every other call it makes.
		return false
	}
	r.visiting[fi] = true
	defer delete(r.visiting, fi)

	params := testingParams(fi.decl)
	binds := r.bindings(fi)
	found := false
	ast.Inspect(fi.decl, func(n ast.Node) bool {
		if found {
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		switch fn := call.Fun.(type) {
		case *ast.SelectorExpr:
			if failMethods[fn.Sel.Name] {
				if fn.Sel.Name != "Error" || errorIsFailMethod(fn, params) {
					found = true
					return false
				}
				// err.Error(). Stop here rather than resolving the method:
				// x.Error on a non-testing receiver is the error interface
				// until proven otherwise.
				return true
			}
			id, ok := fn.X.(*ast.Ident)
			if !ok {
				// A chained receiver (a.b.M(), f().M()) is not resolvable
				// from syntax. Judged non-asserting.
				return true
			}
			if assertPackages[id.Name] {
				found = true
				return false
			}
			// A method call resolves only against the receiver's bound type.
			// An unbound or ambiguous receiver resolves to nothing: a method
			// on an unrelated type that shares the name must not donate its
			// verdict, which is exactly how three unfailable schema tests
			// passed for months (they reached a logger's Error by bare name).
			if typ, ok := binds[id.Name]; ok {
				if m := r.lookup(fi.pkgDir, typ, fn.Sel.Name); m != nil && m != fi {
					found = r.asserts(m)
				}
			}
		case *ast.Ident:
			// A bare call is a free function, never a method.
			if callee := r.lookup(fi.pkgDir, "", fn.Name); callee != nil && callee != fi {
				found = r.asserts(callee)
			}
		}
		return !found
	})

	if found {
		fi.asserts = 1
	} else {
		fi.asserts = 0
	}
	return found
}

// unknownType poisons a name whose type the syntax cannot pin down, or that
// is bound to two different types in one function. It matches no receiver, so
// a call through it resolves to nothing: fail closed.
const unknownType = "?"

// namedType reduces a syntactic type to a bare in-package type name. *T is T;
// []T is kept as "[]T" so ranging over such a value can recover the element
// type; anything qualified or composite is unknown -- its methods could not
// be resolved in this package anyway.
func namedType(e ast.Expr) string {
	switch t := e.(type) {
	case *ast.StarExpr:
		return namedType(t.X)
	case *ast.Ident:
		return t.Name
	case *ast.ArrayType:
		if t.Len == nil {
			if elem := namedType(t.Elt); elem != unknownType && !strings.HasPrefix(elem, "[]") {
				return "[]" + elem
			}
		}
	}
	return unknownType
}

func elemType(typ string) string {
	if s, ok := strings.CutPrefix(typ, "[]"); ok {
		return s
	}
	return unknownType
}

// valueType names the type of an expression used as a declaration value:
// a composite literal, its address, a type assertion, or a call to a
// same-package free function with one declared result.
func (r *assertResolver) valueType(pkgDir string, e ast.Expr) string {
	switch v := e.(type) {
	case *ast.UnaryExpr:
		if v.Op == token.AND {
			return r.valueType(pkgDir, v.X)
		}
	case *ast.CompositeLit:
		if v.Type != nil {
			return namedType(v.Type)
		}
	case *ast.TypeAssertExpr:
		if v.Type != nil {
			return namedType(v.Type)
		}
	case *ast.CallExpr:
		if types := r.callResultTypes(pkgDir, e); len(types) == 1 {
			return types[0]
		}
	}
	return unknownType
}

// callResultTypes resolves a call to a same-package free function into its
// declared result types, one entry per returned value. Anything else -- a
// method call, a conversion, an imported function -- resolves to nothing.
func (r *assertResolver) callResultTypes(pkgDir string, e ast.Expr) []string {
	call, ok := e.(*ast.CallExpr)
	if !ok {
		return nil
	}
	id, ok := call.Fun.(*ast.Ident)
	if !ok {
		return nil
	}
	fn := r.lookup(pkgDir, "", id.Name)
	if fn == nil || fn.decl.Type.Results == nil {
		return nil
	}
	var out []string
	for _, f := range fn.decl.Type.Results.List {
		n := len(f.Names)
		if n == 0 {
			n = 1
		}
		for range n {
			out = append(out, namedType(f.Type))
		}
	}
	return out
}

// bindings maps each name declared anywhere in fi -- receiver, parameters,
// results, function-literal parameters, var declarations, := assignments,
// range and type-switch variables -- to the one type the syntax names for it.
// Every declaration site contributes: a site whose type cannot be read binds
// the name to unknownType, and a name bound to two different types anywhere
// in the function is poisoned the same way, because which declaration a given
// call sees is a scope question the parser cannot answer.
func (r *assertResolver) bindings(fi *funcInfo) map[string]string {
	binds := map[string]string{}
	set := func(name, typ string) {
		if name == "" || name == "_" {
			return
		}
		if prev, ok := binds[name]; ok && prev != typ {
			typ = unknownType
		}
		binds[name] = typ
	}
	bindFields := func(fl *ast.FieldList) {
		if fl == nil {
			return
		}
		for _, f := range fl.List {
			typ := namedType(f.Type)
			for _, n := range f.Names {
				set(n.Name, typ)
			}
		}
	}
	bindFields(fi.decl.Recv)
	bindFields(fi.decl.Type.Params)
	bindFields(fi.decl.Type.Results)

	// Range values are bound after everything else: their element type comes
	// from the ranged expression's own binding, which must be settled -- and
	// poisoned where ambiguous -- before it is read.
	type pendingRange struct {
		name string
		x    ast.Expr
	}
	var ranges []pendingRange

	ast.Inspect(fi.decl, func(n ast.Node) bool {
		switch stmt := n.(type) {
		case *ast.FuncLit:
			bindFields(stmt.Type.Params)
			bindFields(stmt.Type.Results)
		case *ast.AssignStmt:
			if stmt.Tok != token.DEFINE {
				break
			}
			if len(stmt.Rhs) == 1 && len(stmt.Lhs) > 1 {
				// d, err := newDonor(...): results map by position.
				types := r.callResultTypes(fi.pkgDir, stmt.Rhs[0])
				for i, lhs := range stmt.Lhs {
					if id, ok := lhs.(*ast.Ident); ok {
						typ := unknownType
						if i < len(types) {
							typ = types[i]
						}
						set(id.Name, typ)
					}
				}
				break
			}
			for i, lhs := range stmt.Lhs {
				if id, ok := lhs.(*ast.Ident); ok {
					typ := unknownType
					if i < len(stmt.Rhs) {
						typ = r.valueType(fi.pkgDir, stmt.Rhs[i])
					}
					set(id.Name, typ)
				}
			}
		case *ast.GenDecl:
			if stmt.Tok != token.VAR {
				break
			}
			for _, spec := range stmt.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for i, name := range vs.Names {
					typ := unknownType
					if vs.Type != nil {
						typ = namedType(vs.Type)
					} else if i < len(vs.Values) {
						typ = r.valueType(fi.pkgDir, vs.Values[i])
					}
					set(name.Name, typ)
				}
			}
		case *ast.RangeStmt:
			if stmt.Tok != token.DEFINE {
				break
			}
			if id, ok := stmt.Key.(*ast.Ident); ok {
				set(id.Name, unknownType)
			}
			if id, ok := stmt.Value.(*ast.Ident); ok {
				ranges = append(ranges, pendingRange{name: id.Name, x: stmt.X})
			}
		case *ast.TypeSwitchStmt:
			// switch v := x.(type): v has a different type in each case.
			if assign, ok := stmt.Assign.(*ast.AssignStmt); ok && len(assign.Lhs) == 1 {
				if id, ok := assign.Lhs[0].(*ast.Ident); ok {
					set(id.Name, unknownType)
				}
			}
		}
		return true
	})

	for _, p := range ranges {
		typ := unknownType
		switch x := p.x.(type) {
		case *ast.CompositeLit:
			if x.Type != nil {
				typ = elemType(namedType(x.Type))
			}
		case *ast.Ident:
			typ = elemType(binds[x.Name])
		}
		set(p.name, typ)
	}
	return binds
}

// isSkipStub reports an unconditional t.Skip: one that no condition guards, so
// the test can never run. A skip inside an if -- the TF_ACC pattern -- is a
// test that runs under the right conditions and is not reported.
func isSkipStub(fd *ast.FuncDecl) bool {
	if fd.Body == nil {
		return false
	}
	for _, stmt := range fd.Body.List {
		expr, ok := stmt.(*ast.ExprStmt)
		if !ok {
			continue
		}
		call, ok := expr.X.(*ast.CallExpr)
		if !ok {
			continue
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			continue
		}
		if sel.Sel.Name == "Skip" || sel.Sel.Name == "Skipf" || sel.Sel.Name == "SkipNow" {
			return true
		}
	}
	return false
}

// rangesOverEmptyTable reports a range over a slice literal with no
// elements, directly or through a variable that stays empty -- declared
// with := or with var, as a composite literal or (for var) a bare nil
// slice. "Declared empty" is not "empty": an accumulator declared empty and
// then filled elsewhere (census := map[string]int{} ... census[mode]++ ...
// range census) must not be reported, so this tracks whether the variable
// is ever written, not just its literal at declaration.
//
// A variable whose address is taken anywhere in the body -- fillIt(&table),
// the out-parameter idiom, or rec := recorder{log: &table}, a struct literal
// field -- also counts as written, not just a direct assignment or index
// write. Detecting that requires no type information: any &ident qualifies,
// regardless of where it appears, on the conservative bias every other case
// here shares -- an address taken for a read produces a false negative,
// never a false positive. A pointer-receiver method call on an
// already-addressable value (table.Fill(), no explicit &) is NOT detected
// -- that needs type information to know the receiver is a pointer, which
// this analyzer deliberately does not carry -- so it stays a named gap, not
// a silent one.
//
// Package-level var tables are out of scope: this only walks the function
// declaration passed in, never the file's top-level Decls.
func rangesOverEmptyTable(fd *ast.FuncDecl) bool {
	empty := map[string]bool{}
	written := map[string]bool{}

	ast.Inspect(fd, func(n ast.Node) bool {
		switch stmt := n.(type) {
		case *ast.AssignStmt:
			// x[k] = v, or a second assignment to x, is a write.
			for _, lhs := range stmt.Lhs {
				switch target := lhs.(type) {
				case *ast.IndexExpr:
					if id, ok := target.X.(*ast.Ident); ok {
						written[id.Name] = true
					}
				case *ast.Ident:
					if stmt.Tok == token.ASSIGN {
						written[target.Name] = true
					}
				}
			}
			for i, rhs := range stmt.Rhs {
				lit, ok := rhs.(*ast.CompositeLit)
				if !ok || len(lit.Elts) != 0 || i >= len(stmt.Lhs) {
					continue
				}
				if id, ok := stmt.Lhs[i].(*ast.Ident); ok {
					empty[id.Name] = true
				}
			}
		case *ast.GenDecl:
			// var cases = []T{} or var cases []T: the := form above only sees
			// short declarations, and gotests emits the var form just as often.
			if stmt.Tok != token.VAR {
				break
			}
			for _, spec := range stmt.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for i, name := range vs.Names {
					if i < len(vs.Values) {
						if lit, ok := vs.Values[i].(*ast.CompositeLit); ok && len(lit.Elts) == 0 {
							empty[name.Name] = true
						}
						continue
					}
					if arr, ok := vs.Type.(*ast.ArrayType); ok && arr.Len == nil {
						empty[name.Name] = true // var cases []T: a nil slice, empty until written.
					}
				}
			}
		case *ast.IncDecStmt:
			// census[mode]++ fills an accumulator.
			if index, ok := stmt.X.(*ast.IndexExpr); ok {
				if id, ok := index.X.(*ast.Ident); ok {
					written[id.Name] = true
				}
			}
		case *ast.UnaryExpr:
			// &ident anywhere -- call argument, struct-literal field value,
			// wherever -- is treated as a write. Position does not matter:
			// a caller that merely reads through the pointer produces a
			// false negative, never a false positive.
			if stmt.Op != token.AND {
				break
			}
			if id, ok := stmt.X.(*ast.Ident); ok {
				written[id.Name] = true
			}
		}
		return true
	})

	for name := range written {
		delete(empty, name)
	}

	found := false
	ast.Inspect(fd, func(n ast.Node) bool {
		rng, ok := n.(*ast.RangeStmt)
		if !ok {
			return true
		}
		switch x := rng.X.(type) {
		case *ast.Ident:
			if empty[x.Name] {
				found = true
			}
		case *ast.CompositeLit:
			if len(x.Elts) == 0 {
				found = true
			}
		}
		return !found
	})
	return found
}

func receiverName(e ast.Expr) string {
	switch t := e.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.StarExpr:
		return receiverName(t.X)
	case *ast.IndexExpr:
		return receiverName(t.X)
	}
	return ""
}
