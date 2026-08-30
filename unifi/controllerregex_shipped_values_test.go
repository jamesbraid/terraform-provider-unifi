package unifi

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclparse"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	dschema "github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	fwpath "github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	rschema "github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/zclconf/go-cty/cty"
)

// controllerregexPkgPath identifies a validator.String built by
// controllerregex.Matches -- the derived-pattern validator this task's
// compiler change now emits -- by its concrete type's package path, without
// needing an exported accessor onto the unexported matchesValidator struct.
const controllerregexPkgPath = "github.com/ubiquiti-community/terraform-provider-unifi/internal/controllerregex"

// TestControllerregexShippedValuesAreAccepted is Task 2's step 4: for every
// attribute whose validator this task's compiler change touched, run every
// real value this repository ships for it -- examples/resources,
// examples/data-sources, and (best effort) acceptance test Config templates
// -- through the actual validator now compiled into the schema, and fail
// loudly if any one of them is now rejected.
//
// The regex-engine-study.md measurement found zero behavioural disagreement
// between RE2 and regexp2 across a synthetic corpus of several thousand
// pattern x input checks; this test is the same question asked of the real
// corpus that measurement could not see -- the values this provider actually
// ships -- because a synthetic corpus proves nothing about values it never
// tried.
//
// Coverage is necessarily partial: examples/ is exhaustive (every .tf file
// under examples/resources and examples/data-sources is parsed), but the
// acceptance-test half only recovers Config values built from backtick raw
// string literals containing literal HCL text with no unresolved
// interpolation or Sprintf placeholder (a value containing "%" is dropped,
// since it is very likely an unexpanded format verb, not a real value) --
// Config values built through more elaborate string composition are not
// recovered. What is recovered is real, not synthetic.
func TestControllerregexShippedValuesAreAccepted(t *testing.T) {
	ctx := context.Background()

	resourceValidators := collectResourcePatternValidators(ctx, t)
	dataSourceValidators := collectDataSourcePatternValidators(ctx, t)

	resourceExamples, dataSourceExamples := collectExampleValues(t)
	resourceAcc, dataSourceAcc := collectAcceptanceTestConfigValues(t)

	checked := 0
	rejected := 0

	type sourcedValue struct{ value, source string }

	checkSurface := func(kind string, byType map[string]map[string][]validator.String, examples, acc map[string]map[string][]string) {
		for typeName, byPath := range byType {
			for path, validators := range byPath {
				var values []sourcedValue
				for _, v := range examples[typeName][path] {
					values = append(values, sourcedValue{v, "examples/"})
				}
				for _, v := range acc[typeName][path] {
					values = append(values, sourcedValue{v, "acceptance-test config"})
				}
				for _, sv := range values {
					checked++
					for _, v := range validators {
						resp := &validator.StringResponse{}
						v.ValidateString(ctx, validator.StringRequest{
							Path:        fwpath.Root(path),
							ConfigValue: types.StringValue(sv.value),
						}, resp)
						if resp.Diagnostics.HasError() {
							rejected++
							t.Errorf("%s %s.%s: shipped value %q (from %s) is now rejected: %s",
								kind, typeName, path, sv.value, sv.source, resp.Diagnostics.Errors()[0].Detail())
						}
					}
				}
			}
		}
	}

	checkSurface("resource", resourceValidators, resourceExamples, resourceAcc)
	checkSurface("data source", dataSourceValidators, dataSourceExamples, dataSourceAcc)

	t.Logf("checked %d shipped value(s) against their controller-pattern validator(s); %d rejected", checked, rejected)
}

// isControllerregexValidator reports whether v is a validator.String built
// by controllerregex.Matches, identified by its concrete type's package path
// -- matchesValidator is unexported, so this package cannot type-assert to
// it directly.
func isControllerregexValidator(v validator.String) bool {
	t := reflect.TypeOf(v)
	return t != nil && t.PkgPath() == controllerregexPkgPath
}

// collectResourcePatternValidators walks every registered resource's schema
// and returns, for every leaf StringAttribute (at any nesting depth, through
// both nested attributes and nested blocks) that carries at least one
// controllerregex validator, the dotted attribute path and that validator
// set.
func collectResourcePatternValidators(ctx context.Context, t *testing.T) map[string]map[string][]validator.String {
	t.Helper()
	out := map[string]map[string][]validator.String{}
	for _, newResource := range (&unifiProvider{}).Resources(ctx) {
		res := newResource()

		var meta resource.MetadataResponse
		res.Metadata(ctx, resource.MetadataRequest{ProviderTypeName: "unifi"}, &meta)

		var got resource.SchemaResponse
		res.Schema(ctx, resource.SchemaRequest{}, &got)
		if got.Diagnostics.HasError() {
			t.Fatalf("schema for %s: %v", meta.TypeName, got.Diagnostics)
		}

		attrs := map[string][]validator.String{}
		collectResourceAttrPatternValidators(got.Schema.Attributes, "", attrs)
		collectResourceBlockPatternValidators(got.Schema.Blocks, "", attrs)
		if len(attrs) > 0 {
			out[meta.TypeName] = attrs
		}
	}
	return out
}

func collectResourceAttrPatternValidators(attrs map[string]rschema.Attribute, prefix string, out map[string][]validator.String) {
	for name, a := range attrs {
		path := prefix + name
		switch at := a.(type) {
		case rschema.StringAttribute:
			var matched []validator.String
			for _, v := range at.Validators {
				if isControllerregexValidator(v) {
					matched = append(matched, v)
				}
			}
			if len(matched) > 0 {
				out[path] = matched
			}
		case rschema.SingleNestedAttribute:
			collectResourceAttrPatternValidators(at.Attributes, path+".", out)
		case rschema.ListNestedAttribute:
			collectResourceAttrPatternValidators(at.NestedObject.Attributes, path+".", out)
		case rschema.SetNestedAttribute:
			collectResourceAttrPatternValidators(at.NestedObject.Attributes, path+".", out)
		case rschema.MapNestedAttribute:
			collectResourceAttrPatternValidators(at.NestedObject.Attributes, path+".", out)
		}
	}
}

func collectResourceBlockPatternValidators(blocks map[string]rschema.Block, prefix string, out map[string][]validator.String) {
	for name, b := range blocks {
		path := prefix + name
		switch bt := b.(type) {
		case rschema.ListNestedBlock:
			collectResourceAttrPatternValidators(bt.NestedObject.Attributes, path+".", out)
			collectResourceBlockPatternValidators(bt.NestedObject.Blocks, path+".", out)
		case rschema.SetNestedBlock:
			collectResourceAttrPatternValidators(bt.NestedObject.Attributes, path+".", out)
			collectResourceBlockPatternValidators(bt.NestedObject.Blocks, path+".", out)
		case rschema.SingleNestedBlock:
			collectResourceAttrPatternValidators(bt.Attributes, path+".", out)
			collectResourceBlockPatternValidators(bt.Blocks, path+".", out)
		}
	}
}

// collectDataSourcePatternValidators mirrors collectResourcePatternValidators
// over datasource/schema, which shares no interface with resource/schema's.
func collectDataSourcePatternValidators(ctx context.Context, t *testing.T) map[string]map[string][]validator.String {
	t.Helper()
	out := map[string]map[string][]validator.String{}
	for _, newDataSource := range (&unifiProvider{}).DataSources(ctx) {
		ds := newDataSource()

		var meta datasource.MetadataResponse
		ds.Metadata(ctx, datasource.MetadataRequest{ProviderTypeName: "unifi"}, &meta)

		var got datasource.SchemaResponse
		ds.Schema(ctx, datasource.SchemaRequest{}, &got)
		if got.Diagnostics.HasError() {
			t.Fatalf("schema for %s: %v", meta.TypeName, got.Diagnostics)
		}

		attrs := map[string][]validator.String{}
		collectDataSourceAttrPatternValidators(got.Schema.Attributes, "", attrs)
		collectDataSourceBlockPatternValidators(got.Schema.Blocks, "", attrs)
		if len(attrs) > 0 {
			out[meta.TypeName] = attrs
		}
	}
	return out
}

func collectDataSourceAttrPatternValidators(attrs map[string]dschema.Attribute, prefix string, out map[string][]validator.String) {
	for name, a := range attrs {
		path := prefix + name
		switch at := a.(type) {
		case dschema.StringAttribute:
			var matched []validator.String
			for _, v := range at.Validators {
				if isControllerregexValidator(v) {
					matched = append(matched, v)
				}
			}
			if len(matched) > 0 {
				out[path] = matched
			}
		case dschema.SingleNestedAttribute:
			collectDataSourceAttrPatternValidators(at.Attributes, path+".", out)
		case dschema.ListNestedAttribute:
			collectDataSourceAttrPatternValidators(at.NestedObject.Attributes, path+".", out)
		case dschema.SetNestedAttribute:
			collectDataSourceAttrPatternValidators(at.NestedObject.Attributes, path+".", out)
		case dschema.MapNestedAttribute:
			collectDataSourceAttrPatternValidators(at.NestedObject.Attributes, path+".", out)
		}
	}
}

func collectDataSourceBlockPatternValidators(blocks map[string]dschema.Block, prefix string, out map[string][]validator.String) {
	for name, b := range blocks {
		path := prefix + name
		switch bt := b.(type) {
		case dschema.ListNestedBlock:
			collectDataSourceAttrPatternValidators(bt.NestedObject.Attributes, path+".", out)
			collectDataSourceBlockPatternValidators(bt.NestedObject.Blocks, path+".", out)
		case dschema.SetNestedBlock:
			collectDataSourceAttrPatternValidators(bt.NestedObject.Attributes, path+".", out)
			collectDataSourceBlockPatternValidators(bt.NestedObject.Blocks, path+".", out)
		case dschema.SingleNestedBlock:
			collectDataSourceAttrPatternValidators(bt.Attributes, path+".", out)
			collectDataSourceBlockPatternValidators(bt.Blocks, path+".", out)
		}
	}
}

// collectExampleValues parses every .tf file under examples/resources and
// examples/data-sources (relative to this package's directory), keyed by
// Terraform type name (the example subdirectory name) and dotted attribute
// path, the same path shape collectResourcePatternValidators produces.
func collectExampleValues(t *testing.T) (resources, dataSources map[string]map[string][]string) {
	t.Helper()
	resources = map[string]map[string][]string{}
	dataSources = map[string]map[string][]string{}
	walkExampleDir(t, filepath.Join("..", "examples", "resources"), "resource", resources)
	walkExampleDir(t, filepath.Join("..", "examples", "data-sources"), "data", dataSources)
	return resources, dataSources
}

func walkExampleDir(t *testing.T, dir, keyword string, sink map[string]map[string][]string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading %s: %v", dir, err)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		files, err := filepath.Glob(filepath.Join(dir, entry.Name(), "*.tf"))
		if err != nil {
			t.Fatalf("globbing %s: %v", filepath.Join(dir, entry.Name()), err)
		}
		for _, f := range files {
			src, err := os.ReadFile(f)
			if err != nil {
				t.Fatalf("reading %s: %v", f, err)
			}
			extractHCLValues(t, src, f, keyword, sink, true)
		}
	}
}

// collectAcceptanceTestConfigValues does the same extraction over every
// backtick-delimited raw string literal found anywhere in this package's
// _test.go sources -- the shape every Config template in this repository
// uses (see e.g. testAccNetworkFrameworkConfig_basic). This is a lexical
// scan, not a resolution of which literal a given resource.TestStep.Config
// actually references, so it is deliberately over-inclusive (it also visits
// backtick blobs that are not Terraform config at all, such as multi-line
// doc comments quoted in a test fixture) and relies on extractHCLValues's
// non-strict parse failure to silently skip anything that is not valid HCL,
// and on the "%" filter in walkHCLExpr to drop unexpanded Sprintf
// placeholders (e.g. `name = "%s-test"`) rather than report them as real
// values.
func collectAcceptanceTestConfigValues(t *testing.T) (resources, dataSources map[string]map[string][]string) {
	t.Helper()
	resources = map[string]map[string][]string{}
	dataSources = map[string]map[string][]string{}

	files, err := filepath.Glob("*_test.go")
	if err != nil {
		t.Fatalf("globbing *_test.go: %v", err)
	}
	backtick := regexp.MustCompile("(?s)`([^`]*)`")
	for _, f := range files {
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("reading %s: %v", f, err)
		}
		for i, m := range backtick.FindAllSubmatch(src, -1) {
			blob := m[1]
			if !bytes.Contains(blob, []byte(`resource "`)) && !bytes.Contains(blob, []byte(`data "`)) {
				continue // cheap prefilter: not a Terraform config blob
			}
			label := fmt.Sprintf("%s:blob#%d", f, i)
			extractHCLValues(t, blob, label, "resource", resources, false)
			extractHCLValues(t, blob, label, "data", dataSources, false)
		}
	}
	return resources, dataSources
}

// extractHCLValues parses src as HCL and, for every top-level block whose
// type matches keyword (e.g. "resource" or "data"), walks its body and
// records every literal string value found, keyed by the block's first
// label (the Terraform type name) and the dotted attribute path within the
// block. strict controls what happens when src is not valid HCL at all:
// true fails the test by name (an examples/ file must always parse), false
// silently skips (used for the acceptance-test lexical scan, where most
// backtick blobs are not Terraform config).
func extractHCLValues(t *testing.T, src []byte, filename, keyword string, sink map[string]map[string][]string, strict bool) {
	t.Helper()
	parser := hclparse.NewParser()
	file, diags := parser.ParseHCL(src, filename)
	if diags.HasErrors() {
		if strict {
			t.Fatalf("parsing %s: %s", filename, diags.Error())
		}
		return
	}
	body, ok := file.Body.(*hclsyntax.Body)
	if !ok {
		return
	}
	for _, block := range body.Blocks {
		if block.Type != keyword || len(block.Labels) == 0 {
			continue
		}
		typeName := block.Labels[0]
		if sink[typeName] == nil {
			sink[typeName] = map[string][]string{}
		}
		walkHCLBody(block.Body, "", sink[typeName])
	}
}

// walkHCLBody records every literal string attribute value in body (at any
// nesting depth, through both nested blocks -- native HCL block syntax, e.g.
// port_override { ... } -- and single/list/set-nested attributes expressed
// as object-constructor values, e.g. dhcp_server = { ... }, the syntax the
// framework's own modern schema types use).
func walkHCLBody(body *hclsyntax.Body, prefix string, out map[string][]string) {
	for name, attr := range body.Attributes {
		walkHCLExpr(attr.Expr, prefix+name, out)
	}
	for _, block := range body.Blocks {
		walkHCLBody(block.Body, prefix+block.Type+".", out)
	}
}

// walkHCLExpr records path's value if expr is (or reduces to) a literal
// string, and recurses into object-constructor and tuple-constructor
// expressions so a nested attribute path is built up the same way
// collectResourcePatternValidators builds one from the schema. Anything that
// does not evaluate statically with no EvalContext -- a variable reference
// like unifi_network.lan.id, a function call, string interpolation of a
// traversal -- is silently skipped: this is a corpus of real, static,
// already-known values, not a general Terraform evaluator. A value
// containing "%" is also skipped: in this repository's acceptance tests that
// is always an unexpanded fmt.Sprintf verb, not a real value, and reporting
// it as a rejection would be a false positive.
func walkHCLExpr(expr hclsyntax.Expression, path string, out map[string][]string) {
	switch e := expr.(type) {
	case *hclsyntax.ObjectConsExpr:
		for _, item := range e.Items {
			key := hcl.ExprAsKeyword(item.KeyExpr)
			if key == "" {
				if v, diags := item.KeyExpr.Value(nil); !diags.HasErrors() && v.Type() == cty.String {
					key = v.AsString()
				}
			}
			if key == "" {
				continue
			}
			walkHCLExpr(item.ValueExpr, path+"."+key, out)
		}
		return
	case *hclsyntax.TupleConsExpr:
		for _, elem := range e.Exprs {
			walkHCLExpr(elem, path, out)
		}
		return
	}

	v, diags := expr.Value(nil)
	if diags.HasErrors() || v.IsNull() || !v.IsKnown() || v.Type() != cty.String {
		return
	}
	value := v.AsString()
	if strings.ContainsRune(value, '%') {
		return
	}
	out[path] = append(out[path], value)
}
