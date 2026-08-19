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
// straight out of the plan for an attribute the plan carries as unknown.
//
// The shape it catches shipped once and cost a release. unifi_network's
// post-write read preserves fields the controller does not return for
// vlan-only networks:
//
//	if isVLANOnly && previousModel != nil {
//	    model.SettingPreference = previousModel.SettingPreference
//
// On Create that last argument is the PLAN, not prior state -- there is no
// prior state on a create. The copy was safe only while setting_preference
// carried a schema Default, which guaranteed the plan value was already known.
// Removing that default, in a different file, in a change that looked
// self-contained, turned the copy into "Provider returned invalid result object
// after apply" on every create that left the attribute out.
//
// Nothing else we own could see it. The Terraform protocol carries no defaults,
// so no schema comparison notices one leaving; the copy itself did not change.
// One acceptance fixture out of seventeen was the entire defence.
//
// The guarded form is what the same function already uses ten times over:
//
//	if previousModel.MulticastDNS.IsUnknown() {
//	    model.MulticastDNS = types.BoolValue(network.MdnsEnabled)
//	} else {
//	    model.MulticastDNS = previousModel.MulticastDNS
//	}
//
// Ten authors each rediscovered that reasoning by hand and wrote it out in a
// comment. This is so the eleventh does not have to.
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

// funcKey identifies one function declaration.
//
// KEYED BY FILE AND RECEIVER, NOT BY BARE NAME, and that is a correction rather
// than a refinement. networkToModel is a method on networkResource,
// vpnClientResource and vpnServerResource. A single map keyed by "networkToModel"
// made those one entry, so a plan-carrying call on any one of them put all three
// in scope -- and renaming the variable in one file left the name in scope
// through the other two, which is how a mutation test nearly recorded "renames
// are safe".
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
// value read out of the PLAN.
//
// IT FOLLOWS THE VALUE RATHER THAN THE NAME. The previous version collected any
// function called with an argument spelled "plan", "plandata" or "planmodel",
// compared exactly and lowercased. That made the check's scope a naming
// convention: renaming planData to anything else -- the commonest refactor there
// is -- silently removed its coverage of the function it was written for, whose
// own comment says the shape "shipped once and cost a release". Measured rather
// than supposed: with the variable renamed in the three files that call a
// networkToModel, an unguarded multicast_dns copy the check had just caught
// three times became invisible.
//
// So the seed is the only place a plan can enter -- req.Plan.Get(ctx, &v) -- and
// it propagates by ARGUMENT POSITION to a fixpoint.
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

	// THE SEED. A plan reaches this package through req.Plan.Get(ctx, &v) and
	// nowhere else; anything else called a plan is a copy of one.
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

	// PROPAGATE BY POSITION to a fixpoint. A parameter is plan-derived when some
	// caller passes it a plan-derived value, whatever either side calls it.
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

// resolveCall picks the declaration a call refers to, preferring the caller's
// own file.
//
// Without go/types the receiver's type is not known here, and the file is the
// best available proxy: each resource keeps its methods in one file, so a method
// call inside network_resource.go means that file's method. A call with no local
// declaration falls through to a package-level helper. Anything still ambiguous
// is left unresolved rather than unioned -- guessing across three same-named
// methods is the over-reach this rewrite removes.
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
						// THE SOURCE MUST BE THE PLAN, not merely sit in a
						// function that receives one. The old form flagged any
						// same-field copy inside a plan-receiving function,
						// which is wider than the claim and narrower at the same
						// time: wider because a copy from prior state is safe,
						// narrower because the function only qualified when a
						// caller happened to spell its argument "plan".
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
// field's plan value is known. Two spellings do that and both are in use here:
//
//	IsUnknown  the mechanical one -- resolve from the controller when unknown
//	IsNull     the semantic one -- the user set it in config, so it is known
//
// unifi_wan uses the second throughout: `if !config.X.IsNull() { state.X =
// plan.X }` copies only when the practitioner wrote a literal, which the plan
// cannot render unknown. Treating only IsUnknown as a guard reported seven
// correct lines as defects.
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

// TestPlanScopeSurvivesARename is the regression test for the rewrite above,
// and it is deliberately not pointed at the package.
//
// The defect it guards is that the check's SCOPE used to be a naming
// convention: a function entered it only when some caller spelled an argument
// "plan", "plandata" or "planmodel". A pure rename -- the commonest refactor
// there is -- removed coverage of the function the check was written for, and
// nothing went red. Measured on the tree before this change: with planData
// renamed, an unguarded copy the check had just caught three times became
// invisible.
//
// A fixture rather than the package, for the same reason the device census uses
// one: an assertion about the real tree passes or fails for whatever the tree
// happens to contain today, and would go green the moment someone renamed a
// variable back. Here the names are chosen to be wrong on purpose and stay
// wrong.
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
	// NOT ONE OF THE OLD NAMES. "carried" is the parameter and "desired" the
	// local, and neither is plan, plandata or planmodel. If this passes only
	// because a name matched, the rewrite achieved nothing.
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
