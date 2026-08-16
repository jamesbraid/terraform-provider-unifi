// Package testaudit finds test functions that cannot fail.
//
// A test that cannot fail is worse than a missing one. A missing test is an
// absence a reader can see; a test named Test_wanResource_Create that passes
// without exercising create reads as coverage, and it reads that way to
// everyone -- the reviewer, the release checklist, the next person deciding
// where the risk is. Six of the seven behaviours we already know are broken
// have a test named after them, and every one of those tests is skipped or
// asserts nothing.
//
// Three shapes are detected, and they are not equally bad:
//
//	EmptyTable    a table-driven test whose table is empty, so the body never
//	              runs. This is the gotests scaffold, committed unfilled.
//	SkipStub      an unconditional t.Skip. It names a behaviour and declines to
//	              test it, permanently, with no condition that could ever make
//	              it run.
//	NoAssertion   a populated table that calls the code under test and checks
//	              nothing. The worst of the three: it EXECUTES, so it produces
//	              coverage and a green PASS while asserting nothing at all.
//
// Assertion reachability is transitive through same-package helpers. A test
// whose only assertion lives in checkFoo(t, x) does assert, and reporting it
// as unfailable would make the whole audit unusable -- one false positive in a
// list of a hundred and fifty is enough for a reader to stop trusting the list.
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

// String is the inventory line format: stable, sorted, one per line, with the
// defect this test is named after when there is one.
func (f Finding) String() string {
	line := fmt.Sprintf("%s\t%s\t%s", f.File, f.Name, f.Kind)
	if slot, ok := knownBugSlots[f.File+"\t"+f.Name]; ok {
		line += "\t" + slot
	}
	return line
}

// knownBugSlots names the defect a skipped test is already named after.
//
// These are not noise in the list, they are the most actionable entries in it.
// Every one of these bugs was found by a controller, a schema diff or a code
// read WHILE a test bearing its name sat green in CI, so when a fix lands the
// reproduction has a slot waiting with the right name. Without this
// cross-reference the next person to fix unifi_wan writes a new test beside
// Test_wanResource_Create rather than filling it, and the skip survives another
// round.
//
// Keyed on file and name rather than line, for the same reason the inventory
// carries no line numbers: an edit above a test must not rewrite the file.
var knownBugSlots = map[string]string{
	"unifi/wan_resource_test.go\tTest_wanResource_Create":                      "task 99: unifi_wan cannot create a static WAN at all",
	"unifi/wan_resource_test.go\tTest_wanResource_applyPlanToState":            "task 99: unifi_wan cannot create a static WAN at all",
	"unifi/wlan_resource_test.go\tTest_wlanFrameworkResource_Create":           "task 102: unifi_wlan cannot create",
	"unifi/wlan_resource_test.go\tTest_wlanFrameworkResource_Update":           "task 102: unifi_wlan cannot update",
	"unifi/wlan_resource_test.go\tTest_wlanFrameworkResource_applyPlanToState": "task 102: unifi_wlan cannot create or update",
	"unifi/wlan_resource_test.go\tTestAccWLANList_basic":                       "task 102: its own skip reason names the create bug",
	"unifi/vpn_server_resource_test.go\tTest_vpnServerResource_ImportState":    "task 116: vpn_server drops a third DNS server (SDK marshalUserVPN)",
}

// methods on *testing.T that can actually fail a test. Skip and Log are
// deliberately absent: a test that only logs cannot fail, and that is the
// point of the audit.
var failMethods = map[string]bool{
	"Error": true, "Errorf": true,
	"Fatal": true, "Fatalf": true,
	"Fail": true, "FailNow": true,
}

// Assertion helper packages. require.NoError(t, err) fails the test without
// ever naming a fail method.
var assertPackages = map[string]bool{"require": true, "assert": true}

// skipDirs are never descended into. testdata is here because this package's
// own fixtures are deliberately unfailable tests; scanning them would put them
// in the inventory and make the fixtures indistinguishable from findings.
var skipDirs = map[string]bool{".git": true, "vendor": true, "build": true, "testdata": true}

type funcInfo struct {
	decl    *ast.FuncDecl
	pkgDir  string
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

	byName := map[string][]*funcInfo{}
	for key, fi := range funcs {
		parts := strings.SplitN(key, "|", 3)
		byName[parts[0]+"|"+parts[2]] = append(byName[parts[0]+"|"+parts[2]], fi)
	}

	resolver := &assertResolver{byName: byName, visiting: map[*funcInfo]bool{}}

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
	byName   map[string][]*funcInfo
	visiting map[*funcInfo]bool
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
				found = true
				return false
			}
			if id, ok := fn.X.(*ast.Ident); ok && assertPackages[id.Name] {
				found = true
				return false
			}
			found = r.anyAsserts(fi, fn.Sel.Name)
		case *ast.Ident:
			found = r.anyAsserts(fi, fn.Name)
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

func (r *assertResolver) anyAsserts(from *funcInfo, name string) bool {
	for _, candidate := range r.byName[from.pkgDir+"|"+name] {
		if candidate != from && r.asserts(candidate) {
			return true
		}
	}
	return false
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

// rangesOverEmptyTable reports a range over a slice literal with no elements,
// directly or through a variable that stays empty.
//
// "Declared empty" is not "empty". An accumulator is declared empty and then
// filled -- census := map[string]int{} ... census[mode]++ ... range census --
// and reading only the declaration reports a test that runs and asserts as one
// whose body never executes. That was a real false positive here, in
// TestEvidenceModesCoverTheCommittedInventory, which asserts perfectly well.
//
// Two conditions rather than one, and both are needed. The literal must be a
// SLICE, because the table-driven shape this exists to find is always
// tests := []struct{...}{} and an empty map is far more often an accumulator.
// And the variable must never be written after, which is what separates a table
// nobody filled in from an accumulator the loop above fills.
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
				if _, isSlice := lit.Type.(*ast.ArrayType); !isSlice {
					continue
				}
				if id, ok := stmt.Lhs[i].(*ast.Ident); ok {
					empty[id.Name] = true
				}
			}
		case *ast.IncDecStmt:
			// census[mode]++ fills an accumulator.
			if index, ok := stmt.X.(*ast.IndexExpr); ok {
				if id, ok := index.X.(*ast.Ident); ok {
					written[id.Name] = true
				}
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
