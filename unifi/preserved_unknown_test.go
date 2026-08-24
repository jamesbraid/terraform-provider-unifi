package unifi

import (
	"context"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	fwresource "github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
)

// Test_preservedValuesCannotBeUnknown fails when a resource copies a value
// straight out of the plan into an attribute the plan carries as unknown --
// which Terraform rejects at apply with "Provider returned invalid result
// object". Nothing else here can see it: the Terraform protocol carries no
// defaults, so no schema comparison notices one going missing.
func Test_preservedValuesCannotBeUnknown(t *testing.T) {
	pkg := parsePackage(t)

	unknownable := attributesThatCanBeUnknown(t)
	tags := pkg.tfsdkTags()
	copies := pkg.planCopies()

	// A test that finds nothing to examine passes for the wrong reason. The
	// idiom it keys on is a naming convention, so a rename would silence it.
	if len(copies) == 0 {
		t.Fatal("found no copies out of a plan-derived model, so this proved nothing.\n" +
			"    It looks for `dst.Field = src.Field` inside a function that some\n" +
			"    call site passes a plan to. If that idiom changed, teach this test\n" +
			"    the new spelling rather than leaving it green.")
	}

	var found []string
	for _, c := range copies {
		if c.guarded {
			continue
		}
		resource, ok := pkg.resourceOf(c.file)
		if !ok {
			continue
		}
		for _, tag := range tags[c.field] {
			path := resource + "." + tag
			if !unknownable[path] {
				continue
			}
			found = append(found, fmt.Sprintf("%s:%d  %s  (copied from %s, via %s)",
				c.file, c.line, path, c.source, c.fn))
		}
	}
	sort.Strings(found)

	if len(found) > 0 {
		t.Errorf("%d unguarded cop(ies) of a value the plan can carry as unknown:\n    %s\n\n"+
			"    Each attribute is Computed with no Default, so on Create the plan\n"+
			"    carries it as unknown. Copying it into the result leaves it unknown\n"+
			"    after apply, which Terraform rejects outright.\n\n"+
			"    Resolve it from the controller instead, as these functions already\n"+
			"    do for their other attributes:\n\n"+
			"        if previousModel.X.IsUnknown() {\n"+
			"            model.X = <the controller's value>\n"+
			"        } else {\n"+
			"            model.X = previousModel.X\n"+
			"        }\n",
			len(found), strings.Join(found, "\n    "))
	}
}

// funcKey identifies one function declaration by file and receiver, not bare
// name: networkToModel is a method on three different resources, and keying
// by name alone would conflate them.
type funcKey struct {
	file string
	recv string
	name string
}

// modelNames are the identifiers this package assigns a terraform model to.
// Requiring the destination to be one keeps SDK-to-SDK copies out: those share
// field names with the models (Type, ID) and are not schema values at all.
var modelNames = []string{"model", "data", "state", "result"}

type planCopy struct {
	file    string
	line    int
	fn      string
	field   string
	source  string
	guarded bool
}

type parsedPackage struct {
	fset  *token.FileSet
	files map[string]*ast.File
}

func parsePackage(t *testing.T) *parsedPackage {
	t.Helper()

	names, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("listing package files: %v", err)
	}
	p := &parsedPackage{fset: token.NewFileSet(), files: map[string]*ast.File{}}
	for _, name := range names {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(p.fset, name, nil, 0)
		if err != nil {
			t.Fatalf("parsing %s: %v", name, err)
		}
		p.files[name] = f
	}
	if len(p.files) == 0 {
		t.Fatal("no non-test .go files found in the package directory")
	}
	return p
}

// planCarriers returns, per function, the parameters and locals that hold a
// value read out of the PLAN, propagated by dataflow from req.Plan.Get(ctx,
// &v) rather than by matching a conventional argument name -- a rename would
// otherwise silently remove coverage of the function this exists to check.
func (p *parsedPackage) planCarriers() map[funcKey]map[string]bool {
	decls, byName := p.declarations()
	carriers := map[funcKey]map[string]bool{}

	mark := func(key funcKey, name string) bool {
		if name == "" || name == "_" {
			return false
		}
		if carriers[key] == nil {
			carriers[key] = map[string]bool{}
		}
		if carriers[key][name] {
			return false
		}
		carriers[key][name] = true
		return true
	}

	// A plan reaches this package through req.Plan.Get(ctx, &v) and nowhere
	// else; anything else called a plan is a copy of one.
	for key, fn := range decls {
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || len(call.Args) < 2 {
				return true
			}
			get, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || get.Sel.Name != "Get" {
				return true
			}
			source, ok := get.X.(*ast.SelectorExpr)
			if !ok || source.Sel.Name != "Plan" {
				return true
			}
			if ident, ok := identOf(call.Args[len(call.Args)-1]); ok {
				mark(key, ident)
			}
			return true
		})
	}

	// Propagate by argument position to a fixpoint: a parameter is
	// plan-derived when some caller passes it a plan-derived value, whatever
	// either side calls it.
	for changed := true; changed; {
		changed = false
		for key, fn := range decls {
			held := carriers[key]
			if len(held) == 0 {
				continue
			}
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				callee, ok := p.resolveCall(key, call, byName)
				if !ok {
					return true
				}
				params := parameterNames(decls[callee])
				for i, arg := range call.Args {
					ident, ok := identOf(arg)
					if !ok || !held[ident] || i >= len(params) {
						continue
					}
					if mark(callee, params[i]) {
						changed = true
					}
				}
				return true
			})
		}
	}
	return carriers
}

// declarations indexes every function in the package by key, and by bare name
// for call resolution.
func (p *parsedPackage) declarations() (map[funcKey]*ast.FuncDecl, map[string][]funcKey) {
	decls := map[funcKey]*ast.FuncDecl{}
	byName := map[string][]funcKey{}
	for file, f := range p.files {
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			key := funcKey{file: file, recv: receiverType(fn), name: fn.Name.Name}
			decls[key] = fn
			byName[fn.Name.Name] = append(byName[fn.Name.Name], key)
		}
	}
	return decls, byName
}

// resolveCall picks the declaration a call refers to, preferring the
// caller's own file (each resource keeps its methods in one file), falling
// back to a package-level helper when there's no local match. Anything
// still ambiguous across several same-named methods is left unresolved
// rather than guessed.
func (p *parsedPackage) resolveCall(
	from funcKey, call *ast.CallExpr, byName map[string][]funcKey,
) (funcKey, bool) {
	var name string
	switch fun := call.Fun.(type) {
	case *ast.Ident:
		name = fun.Name
	case *ast.SelectorExpr:
		name = fun.Sel.Name
	default:
		return funcKey{}, false
	}
	candidates := byName[name]
	for _, candidate := range candidates {
		if candidate.file == from.file {
			return candidate, true
		}
	}
	if len(candidates) == 1 {
		return candidates[0], true
	}
	return funcKey{}, false
}

func receiverType(fn *ast.FuncDecl) string {
	if fn.Recv == nil || len(fn.Recv.List) == 0 {
		return ""
	}
	expr := fn.Recv.List[0].Type
	if star, ok := expr.(*ast.StarExpr); ok {
		expr = star.X
	}
	if ident, ok := expr.(*ast.Ident); ok {
		return ident.Name
	}
	return ""
}

func parameterNames(fn *ast.FuncDecl) []string {
	if fn == nil || fn.Type.Params == nil {
		return nil
	}
	var out []string
	for _, field := range fn.Type.Params.List {
		if len(field.Names) == 0 {
			out = append(out, "")
			continue
		}
		for _, name := range field.Names {
			out = append(out, name.Name)
		}
	}
	return out
}

// identOf reads the identifier out of x, &x or *x.
func identOf(expr ast.Expr) (string, bool) {
	switch typed := expr.(type) {
	case *ast.Ident:
		return typed.Name, true
	case *ast.UnaryExpr:
		return identOf(typed.X)
	case *ast.StarExpr:
		return identOf(typed.X)
	}
	return "", false
}

// planCopies finds `dst.Field = src.Field` inside functions that receive a
// plan, and reports whether each sits under an IsUnknown guard on that field.
func (p *parsedPackage) planCopies() []planCopy {
	carriers := p.planCarriers()
	decls, _ := p.declarations()

	var out []planCopy
	for key, fn := range decls {
		held := carriers[key]
		if len(held) == 0 {
			continue
		}
		name := key.file
		{

			var guards []ast.Expr
			var walk func(n ast.Node)
			walk = func(n ast.Node) {
				switch v := n.(type) {
				case *ast.IfStmt:
					guards = append(guards, v.Cond)
					if v.Init != nil {
						walk(v.Init)
					}
					walk(v.Body)
					if v.Else != nil {
						walk(v.Else)
					}
					guards = guards[:len(guards)-1]
					return
				case *ast.AssignStmt:
					for i, lhs := range v.Lhs {
						if i >= len(v.Rhs) {
							break
						}
						dst, ok := fieldSelector(lhs)
						if !ok {
							continue
						}
						src, ok := fieldSelector(v.Rhs[i])
						if !ok || src.field != dst.field || src.recv == dst.recv {
							continue
						}
						if !matchesAny(dst.recv, modelNames) {
							continue
						}
						// The source must itself carry the plan, not merely
						// sit in a function that receives one -- a copy from
						// prior state is safe.
						if !held[src.recv] {
							continue
						}
						pos := p.fset.Position(v.Pos())
						out = append(out, planCopy{
							file:    name,
							line:    pos.Line,
							fn:      fn.Name.Name,
							field:   dst.field,
							source:  src.recv,
							guarded: guardsField(guards, dst.field),
						})
					}
				}
				ast.Inspect(n, func(c ast.Node) bool {
					if c == nil || c == n {
						return true
					}
					switch c.(type) {
					case *ast.IfStmt, *ast.AssignStmt:
						walk(c)
						return false
					}
					return true
				})
			}
			walk(fn.Body)
		}
	}
	return out
}

// resourceOf returns the terraform type name the given file implements, read
// from its Metadata method, so an attribute is only checked against the
// resource that actually serves it.
func (p *parsedPackage) resourceOf(file string) (string, bool) {
	f, ok := p.files[file]
	if !ok {
		return "", false
	}
	var suffix string
	ast.Inspect(f, func(n ast.Node) bool {
		bin, ok := n.(*ast.BinaryExpr)
		if !ok {
			return true
		}
		lit, ok := bin.Y.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return true
		}
		v := strings.Trim(lit.Value, `"`)
		if strings.HasPrefix(v, "_") && suffix == "" {
			suffix = v
		}
		return true
	})
	if suffix == "" {
		return "", false
	}
	return "unifi" + suffix, true
}

// tfsdkTags maps a Go field name to the attribute names it is tagged with.
func (p *parsedPackage) tfsdkTags() map[string][]string {
	out := map[string]map[string]bool{}
	for _, f := range p.files {
		ast.Inspect(f, func(n ast.Node) bool {
			st, ok := n.(*ast.StructType)
			if !ok {
				return true
			}
			for _, field := range st.Fields.List {
				if field.Tag == nil || len(field.Names) == 0 {
					continue
				}
				tag := reflect.StructTag(strings.Trim(field.Tag.Value, "`"))
				name, ok := tag.Lookup("tfsdk")
				if !ok || name == "" || name == "-" {
					continue
				}
				key := field.Names[0].Name
				if out[key] == nil {
					out[key] = map[string]bool{}
				}
				out[key][name] = true
			}
			return true
		})
	}
	tags := make(map[string][]string, len(out))
	for field, set := range out {
		for name := range set {
			tags[field] = append(tags[field], name)
		}
		sort.Strings(tags[field])
	}
	return tags
}

type selector struct{ recv, field string }

func fieldSelector(e ast.Expr) (selector, bool) {
	sel, ok := e.(*ast.SelectorExpr)
	if !ok {
		return selector{}, false
	}
	ident, ok := sel.X.(*ast.Ident)
	if !ok {
		return selector{}, false
	}
	return selector{recv: ident.Name, field: sel.Sel.Name}, true
}

func matchesAny(name string, candidates []string) bool {
	lower := strings.ToLower(name)
	for _, c := range candidates {
		if lower == c {
			return true
		}
	}
	return false
}

// guardsField reports whether an enclosing condition establishes that the
// field's plan value is known. Two spellings do that: IsUnknown resolves
// from the controller when unknown; IsNull (used throughout unifi_wan)
// copies only when the practitioner set a literal, which the plan can't
// render unknown.
func guardsField(guards []ast.Expr, field string) bool {
	for _, cond := range guards {
		var hit bool
		ast.Inspect(cond, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			fn, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || (fn.Sel.Name != "IsUnknown" && fn.Sel.Name != "IsNull") {
				return true
			}
			if inner, ok := fn.X.(*ast.SelectorExpr); ok && inner.Sel.Name == field {
				hit = true
			}
			return true
		})
		if hit {
			return true
		}
	}
	return false
}

// attributesThatCanBeUnknown returns the set of "<resource>.<path>" that are
// Computed and carry no Default, which is exactly the set a plan carries as
// unknown when the configuration leaves them out.
func attributesThatCanBeUnknown(t *testing.T) map[string]bool {
	t.Helper()
	ctx := context.Background()

	out := map[string]bool{}

	var walk func(prefix string, attrs map[string]schema.Attribute)
	walk = func(prefix string, attrs map[string]schema.Attribute) {
		for name, a := range attrs {
			path := prefix + name

			var hasDefault bool
			switch v := a.(type) {
			case schema.BoolAttribute:
				hasDefault = v.Default != nil
			case schema.StringAttribute:
				hasDefault = v.Default != nil
			case schema.Int64Attribute:
				hasDefault = v.Default != nil
			case schema.Float64Attribute:
				hasDefault = v.Default != nil
			case schema.NumberAttribute:
				hasDefault = v.Default != nil
			case schema.ListAttribute:
				hasDefault = v.Default != nil
			case schema.SetAttribute:
				hasDefault = v.Default != nil
			case schema.MapAttribute:
				hasDefault = v.Default != nil
			case schema.ObjectAttribute:
				hasDefault = v.Default != nil
			case schema.SingleNestedAttribute:
				hasDefault = v.Default != nil
				walk(path+".", v.Attributes)
			case schema.ListNestedAttribute:
				hasDefault = v.Default != nil
				walk(path+".", v.NestedObject.Attributes)
			case schema.SetNestedAttribute:
				hasDefault = v.Default != nil
				walk(path+".", v.NestedObject.Attributes)
			case schema.MapNestedAttribute:
				hasDefault = v.Default != nil
				walk(path+".", v.NestedObject.Attributes)
			}

			if a.IsComputed() && !hasDefault {
				out[path] = true
			}
		}
	}

	for _, fn := range New().Resources(ctx) {
		r := fn()

		var meta fwresource.MetadataResponse
		r.Metadata(ctx, fwresource.MetadataRequest{ProviderTypeName: "unifi"}, &meta)

		var resp fwresource.SchemaResponse
		r.Schema(ctx, fwresource.SchemaRequest{}, &resp)
		if resp.Diagnostics.HasError() {
			t.Fatalf("schema for %s: %v", meta.TypeName, resp.Diagnostics)
		}

		walk(meta.TypeName+".", resp.Schema.Attributes)
	}

	if len(out) == 0 {
		t.Fatal("no Computed attribute without a Default was found in any served\n" +
			"    schema, which cannot be true and means this test measures nothing")
	}
	return out
}

// TestPlanScopeSurvivesARename is the regression test for planCarriers'
// dataflow-based scope, run against a fixture with deliberately
// non-conventional names ("carried", not "plan") so a scope that reverted to
// matching names would fail here rather than staying green by accident.
func TestPlanScopeSurvivesARename(t *testing.T) {
	p := parseSources(t, map[string]string{
		"resource.go": `package unifi

func (r *thing) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var desired thingModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &desired)...)
	r.toModel(ctx, &result, &desired)
}

func (r *thing) toModel(ctx context.Context, model *thingModel, carried *thingModel) {
	model.Field = carried.Field
}
`,
	})
	carriers := p.planCarriers()

	var toModel map[string]bool
	for key, held := range carriers {
		if key.name == "toModel" {
			toModel = held
		}
	}
	if toModel == nil {
		t.Fatal("toModel is not in the carrier map at all; the plan did not propagate " +
			"through the call, so the check has no scope and every verdict it gives is empty")
	}
	// "carried" is deliberately not spelled plan, plandata or planmodel.
	if !toModel["carried"] {
		t.Errorf("the parameter holding the plan was not recognised: %v.\n"+
			"    The scope is following names again rather than the value, and a rename "+
			"will silently remove coverage the way it did before.", toModel)
	}
}

// parseSources parses an in-memory package, so a scope test can name its
// variables badly on purpose.
func parseSources(t *testing.T, sources map[string]string) *parsedPackage {
	t.Helper()
	p := &parsedPackage{fset: token.NewFileSet(), files: map[string]*ast.File{}}
	for name, src := range sources {
		f, err := parser.ParseFile(p.fset, name, src, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parsing %s: %v", name, err)
		}
		p.files[name] = f
	}
	return p
}
