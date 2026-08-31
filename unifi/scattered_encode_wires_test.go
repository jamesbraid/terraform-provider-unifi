package unifi

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"

	ui "github.com/ubiquiti-community/go-unifi/unifi"
)

// A ScatteredObjectField's Wires list and its Encode closure are maintained
// by hand, separately: a field Encode assigns but Wires doesn't name is
// accepted at plan, written into the SDK object, and then filtered straight
// back out by the mask -- the practitioner's change silently doesn't apply.
// The reverse direction (a name in Wires that isn't a real json tag) is
// covered by WireNameProblems.
func TestScatteredEncodeAssignsOnlyItsDeclaredWires(t *testing.T) {
	tags := networkJSONTags(t)
	sdkTag := func(field string) (string, bool) {
		tag, ok := tags[field]
		return tag, ok
	}

	descriptors, err := filepath.Glob(filepath.Join("..", "unifi", "*_descriptor.go"))
	if err != nil {
		t.Fatal(err)
	}
	if len(descriptors) == 0 {
		t.Fatal("no descriptor files found; the glob is wrong and this asserts nothing")
	}

	checked := 0
	for _, path := range descriptors {
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("reading %s: %v", path, err)
		}
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, path, src, 0)
		if err != nil {
			t.Fatalf("parsing %s: %v", path, err)
		}
		ast.Inspect(file, func(n ast.Node) bool {
			lit, ok := n.(*ast.CompositeLit)
			if !ok || !isScatteredObjectField(lit) {
				return true
			}
			wires, encode := scatteredWiresAndEncode(lit)
			if encode == nil {
				return true
			}
			checked++
			declared := map[string]bool{}
			for _, w := range wires {
				declared[w] = true
			}
			for _, field := range assignedSDKFields(encode) {
				tag, ok := sdkTag(field)
				if !ok {
					// Not a Network field: this descriptor is over another SDK
					// type, and the tag map cannot judge it.
					continue
				}
				if !declared[tag] {
					t.Errorf("%s: a ScatteredObjectField Encode assigns Network.%s (%q) "+
						"but its Wires list does not name it, so the mask filters it back "+
						"out and the practitioner's value is never written",
						filepath.Base(path), field, tag)
				}
			}
			return true
		})
	}
	if checked == 0 {
		t.Fatal("no ScatteredObjectField with an Encode was found; this asserts nothing")
	}
	t.Logf("checked %d ScatteredObjectField encoder(s)", checked)
}

func isScatteredObjectField(lit *ast.CompositeLit) bool {
	idx, ok := lit.Type.(*ast.IndexListExpr)
	if !ok {
		return false
	}
	sel, ok := idx.X.(*ast.SelectorExpr)
	return ok && sel.Sel.Name == "ScatteredObjectField"
}

func scatteredWiresAndEncode(lit *ast.CompositeLit) ([]string, ast.Node) {
	var wires []string
	var encode ast.Node
	for _, elt := range lit.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		key, ok := kv.Key.(*ast.Ident)
		if !ok {
			continue
		}
		switch key.Name {
		case "Wires":
			slice, ok := kv.Value.(*ast.CompositeLit)
			if !ok {
				continue
			}
			for _, item := range slice.Elts {
				if bl, ok := item.(*ast.BasicLit); ok {
					if v, err := strconv.Unquote(bl.Value); err == nil {
						wires = append(wires, v)
					}
				}
			}
		case "Encode":
			encode = kv.Value
		}
	}
	sort.Strings(wires)
	return wires, encode
}

// assignedSDKFields collects `sdk.Field = ...` targets, including through
// helper calls the Encode delegates to -- one level deep, only within this
// package, so the walk doesn't crawl into go-unifi and report fields no
// encoder touches.
func assignedSDKFields(encode ast.Node) []string {
	seen := map[string]bool{}
	collect := func(n ast.Node) {
		ast.Inspect(n, func(node ast.Node) bool {
			assign, ok := node.(*ast.AssignStmt)
			if !ok {
				return true
			}
			for _, lhs := range assign.Lhs {
				sel, ok := lhs.(*ast.SelectorExpr)
				if !ok {
					continue
				}
				if ident, ok := sel.X.(*ast.Ident); ok && ident.Name == "sdk" {
					seen[sel.Sel.Name] = true
				}
			}
			return true
		})
	}
	collect(encode)

	// Follow the package-level helpers the encoder calls, one level.
	helperNames := map[string]bool{}
	ast.Inspect(encode, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		if ident, ok := call.Fun.(*ast.Ident); ok {
			helperNames[ident.Name] = true
		}
		return true
	})
	for name := range helperNames {
		if body := packageFuncBody(name); body != nil {
			ast.Inspect(body, func(node ast.Node) bool {
				assign, ok := node.(*ast.AssignStmt)
				if !ok {
					return true
				}
				for _, lhs := range assign.Lhs {
					sel, ok := lhs.(*ast.SelectorExpr)
					if !ok {
						continue
					}
					// The helpers name their parameter `network`, not `sdk`.
					if ident, ok := sel.X.(*ast.Ident); ok &&
						(ident.Name == "network" || ident.Name == "sdk") {
						seen[sel.Sel.Name] = true
					}
				}
				return true
			})
		}
	}

	out := make([]string, 0, len(seen))
	for name := range seen {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

var packageFuncBodies map[string]*ast.FuncDecl

func packageFuncBody(name string) ast.Node {
	if packageFuncBodies == nil {
		packageFuncBodies = map[string]*ast.FuncDecl{}
		files, _ := filepath.Glob(filepath.Join("..", "unifi", "*.go"))
		fset := token.NewFileSet()
		for _, path := range files {
			if strings.HasSuffix(path, "_test.go") {
				continue
			}
			src, err := os.ReadFile(path)
			if err != nil {
				continue
			}
			file, err := parser.ParseFile(fset, path, src, 0)
			if err != nil {
				continue
			}
			for _, decl := range file.Decls {
				if fn, ok := decl.(*ast.FuncDecl); ok && fn.Recv == nil {
					packageFuncBodies[fn.Name.Name] = fn
				}
			}
		}
	}
	fn, ok := packageFuncBodies[name]
	if !ok {
		return nil
	}
	return fn
}

var _ = reflect.TypeOf(ui.Network{})

// A BeforeSend hook can assign a field no wire name declares -- the same
// defect as a ScatteredObjectField Encode, one level up: the hook derives
// what no Field can express (network's purpose/vlan/networkgroup), but
// still has to reach the mask built from Fields plus AlwaysWire. This is
// what checks a hand-written hook now that the mask itself is derived from
// the descriptor rather than a maintained list.
//
// device's port_overrides used to be the same shape (a BeforeSend-derived
// value reaching the mask through AlwaysWire) but no longer is: it is
// written through its own call (updateDevicePortOverridesGrouped) and is
// never part of the general masked write at all, so it is the
// counter-example now, not an instance -- this test only covers network.
func TestBeforeSendAssignsOnlyDeclaredWires(t *testing.T) {
	tags := networkJSONTags(t)

	declared, hooks := networkDescriptorWiresAndHooks(t)
	if len(declared) == 0 {
		t.Fatal("no wire names parsed from network_descriptor.go; this asserts nothing")
	}
	if len(hooks) == 0 {
		t.Fatal("no BeforeSend hook found; this asserts nothing")
	}

	for _, hook := range hooks {
		for _, field := range assignedSDKFields(hook) {
			tag, ok := tags[field]
			if !ok {
				continue
			}
			if !declared[tag] {
				t.Errorf("BeforeSend assigns Network.%s (%q) but no Field, no Wires "+
					"list and no AlwaysWire entry names it, so the mask filters it "+
					"straight back out", field, tag)
			}
		}
	}
}

// networkDescriptorWiresAndHooks collects every wire name the descriptor
// declares -- Wire, Wires and AlwaysWire alike -- and the BeforeSend bodies.
func networkDescriptorWiresAndHooks(t *testing.T) (map[string]bool, []ast.Node) {
	t.Helper()
	path := filepath.Join("..", "unifi", "network_descriptor.go")
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, src, 0)
	if err != nil {
		t.Fatalf("parsing %s: %v", path, err)
	}

	declared := map[string]bool{}
	addLit := func(v ast.Expr) {
		if bl, ok := v.(*ast.BasicLit); ok {
			if s, err := strconv.Unquote(bl.Value); err == nil {
				declared[s] = true
			}
		}
	}
	ast.Inspect(file, func(n ast.Node) bool {
		kv, ok := n.(*ast.KeyValueExpr)
		if !ok {
			return true
		}
		key, ok := kv.Key.(*ast.Ident)
		if !ok {
			return true
		}
		switch key.Name {
		case "Wire":
			addLit(kv.Value)
		case "Wires", "AlwaysWire":
			if slice, ok := kv.Value.(*ast.CompositeLit); ok {
				for _, item := range slice.Elts {
					addLit(item)
				}
			}
		}
		return true
	})
	// The netPtr/netBool helpers pass the wire name as their first argument.
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || len(call.Args) == 0 {
			return true
		}
		if ident, ok := call.Fun.(*ast.Ident); ok &&
			(ident.Name == "netPtr" || ident.Name == "netBool") {
			addLit(call.Args[0])
		}
		return true
	})

	var hooks []ast.Node
	for _, decl := range file.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok &&
			strings.HasSuffix(fn.Name.Name, "BeforeSend") {
			hooks = append(hooks, fn)
		}
	}
	return declared, hooks
}
