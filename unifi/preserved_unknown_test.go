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

// planNames are the identifiers this package uses for a model read out of the
// plan. A function handed one of these can be reached on the create path.
var planNames = []string{"plandata", "plan", "planmodel"}

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

// planReceivers returns the names of functions that some call site passes a
// plan-derived model to. Those are the ones whose "previous" argument is not
// prior state on every path.
func (p *parsedPackage) planReceivers() map[string]bool {
	out := map[string]bool{}
	for _, f := range p.files {
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			for _, arg := range call.Args {
				if u, ok := arg.(*ast.UnaryExpr); ok {
					arg = u.X
				}
				ident, ok := arg.(*ast.Ident)
				if !ok || !matchesAny(ident.Name, planNames) {
					continue
				}
				switch fn := call.Fun.(type) {
				case *ast.Ident:
					out[fn.Name] = true
				case *ast.SelectorExpr:
					out[fn.Sel.Name] = true
				}
			}
			return true
		})
	}
	return out
}

// planCopies finds `dst.Field = src.Field` inside functions that receive a
// plan, and reports whether each sits under an IsUnknown guard on that field.
func (p *parsedPackage) planCopies() []planCopy {
	receivers := p.planReceivers()

	var out []planCopy
	for name, f := range p.files {
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil || !receivers[fn.Name.Name] {
				continue
			}

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
