package unifi

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// The list resource config schemas are hand-written and near-identical. The
// position recorded for them is assert-don't-generate, because the HashiCorp
// codegen toolchain has no list resource concept at any published version:
// terraform-plugin-codegen-spec's Specification carries only DataSources,
// Provider and Resources, and tfplugingen-framework has no generate
// list-resources subcommand. Emitting them would mean building Go source
// generation the pipeline does not otherwise have.
//
// That position is only safe while the bodies actually stay uniform, so this
// asserts the uniformity instead of assuming it. Without this the position is
// an assumption; with it, drift fails here and by name.
const (
	listScaffoldSurfaces  = 25
	listScaffoldCanonical = "site:StringAttribute:Optional"
)

// listScaffoldFilterBlock is the part that carries the bulk and is identical
// across every surface, including the three whose top-level attributes differ.
var listScaffoldFilterBlock = []string{
	"name:StringAttribute:Required",
	"value:StringAttribute:Required",
}

// listScaffoldOutliers are the surfaces whose top-level attributes deviate,
// each recorded exactly. A surface that drifts, or a fourth that appears,
// fails by name rather than being absorbed into the common shape.
var listScaffoldOutliers = map[string][]string{
	// Lists every site, so it takes no site selector.
	"siteFrameworkResource": {},
	// Adds an optional members-group selector.
	"clientResource": {"group:StringAttribute:Optional", listScaffoldCanonical},
	// Peers belong to one server network, so that selector is required.
	"wireguardPeerResource": {"network_id:StringAttribute:Required", listScaffoldCanonical},
}

func TestListResourceConfigSchemasMatchOneScaffold(t *testing.T) {
	schemas := parseListResourceConfigSchemas(t)

	if len(schemas) != listScaffoldSurfaces {
		t.Fatalf("found %d list resource config schemas, want %d", len(schemas), listScaffoldSurfaces)
	}

	canonical := 0
	for _, receiver := range sortedScaffoldReceivers(schemas) {
		schema := schemas[receiver]

		// The filter block is the scaffold's substance. It must be identical
		// everywhere, outliers included.
		if !equalStrings(schema.FilterAttributes, listScaffoldFilterBlock) {
			t.Errorf("%s filter block is %v, want %v",
				receiver, schema.FilterAttributes, listScaffoldFilterBlock)
		}
		if schema.Blocks != 1 {
			t.Errorf("%s declares %d blocks, want exactly the filter block", receiver, schema.Blocks)
		}

		want, isOutlier := listScaffoldOutliers[receiver]
		if !isOutlier {
			want = []string{listScaffoldCanonical}
		}
		if !equalStrings(schema.TopAttributes, want) {
			if isOutlier {
				t.Errorf("recorded outlier %s has attributes %v, want %v — its deviation changed",
					receiver, schema.TopAttributes, want)
			} else {
				t.Errorf("undeclared outlier %s has attributes %v, want %v — record it in listScaffoldOutliers or bring it back to the scaffold",
					receiver, schema.TopAttributes, want)
			}
			continue
		}
		if !isOutlier {
			canonical++
		}
	}

	if wantCanonical := listScaffoldSurfaces - len(listScaffoldOutliers); canonical != wantCanonical {
		t.Errorf("%d surfaces match the scaffold exactly, want %d", canonical, wantCanonical)
	}
}

// TestListScaffoldOutliersAreRealSurfaces keeps the outlier record honest in
// the other direction: an entry left behind after a surface is brought back to
// the scaffold would otherwise sit there unnoticed and excuse a real drift.
func TestListScaffoldOutliersAreRealSurfaces(t *testing.T) {
	schemas := parseListResourceConfigSchemas(t)
	for receiver := range listScaffoldOutliers {
		if _, found := schemas[receiver]; !found {
			t.Errorf("listScaffoldOutliers records %s, which declares no list resource config schema", receiver)
		}
	}
}

type listSchemaShape struct {
	TopAttributes    []string
	FilterAttributes []string
	Blocks           int
}

func parseListResourceConfigSchemas(t *testing.T) map[string]listSchemaShape {
	t.Helper()
	fileSet := token.NewFileSet()
	packages, err := parser.ParseDir(fileSet, ".", func(info fs.FileInfo) bool {
		return !strings.HasSuffix(info.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatal(err)
	}

	shapes := map[string]listSchemaShape{}
	for _, pkg := range packages {
		for _, file := range pkg.Files {
			for _, decl := range file.Decls {
				function, ok := decl.(*ast.FuncDecl)
				if !ok || function.Name.Name != "ListResourceConfigSchema" || function.Recv == nil {
					continue
				}
				receiver := receiverTypeName(function.Recv)
				literal := schemaLiteral(function)
				if literal == nil {
					t.Fatalf("%s assigns no listschema.Schema literal", receiver)
				}
				shapes[receiver] = shapeOf(t, receiver, literal)
			}
		}
	}
	return shapes
}

func receiverTypeName(fields *ast.FieldList) string {
	if len(fields.List) == 0 {
		return ""
	}
	expression := fields.List[0].Type
	if star, ok := expression.(*ast.StarExpr); ok {
		expression = star.X
	}
	if ident, ok := expression.(*ast.Ident); ok {
		return ident.Name
	}
	return ""
}

// schemaLiteral finds the `resp.Schema = listschema.Schema{...}` assignment.
func schemaLiteral(function *ast.FuncDecl) *ast.CompositeLit {
	var found *ast.CompositeLit
	ast.Inspect(function.Body, func(node ast.Node) bool {
		assign, ok := node.(*ast.AssignStmt)
		if !ok || len(assign.Lhs) != 1 || len(assign.Rhs) != 1 {
			return true
		}
		selector, ok := assign.Lhs[0].(*ast.SelectorExpr)
		if !ok || selector.Sel.Name != "Schema" {
			return true
		}
		if literal, ok := assign.Rhs[0].(*ast.CompositeLit); ok {
			found = literal
			return false
		}
		return true
	})
	return found
}

func shapeOf(t *testing.T, receiver string, literal *ast.CompositeLit) listSchemaShape {
	t.Helper()
	shape := listSchemaShape{TopAttributes: []string{}, FilterAttributes: []string{}}
	for _, element := range literal.Elts {
		pair, ok := element.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		key, ok := pair.Key.(*ast.Ident)
		if !ok {
			continue
		}
		switch key.Name {
		case "Attributes":
			shape.TopAttributes = attributeSignatures(pair.Value)
		case "Blocks":
			blocks, ok := pair.Value.(*ast.CompositeLit)
			if !ok {
				t.Fatalf("%s Blocks is not a composite literal", receiver)
			}
			shape.Blocks = len(blocks.Elts)
			shape.FilterAttributes = filterAttributeSignatures(t, receiver, blocks)
		}
	}
	return shape
}

// filterAttributeSignatures reads Blocks["filter"].NestedObject.Attributes.
func filterAttributeSignatures(t *testing.T, receiver string, blocks *ast.CompositeLit) []string {
	t.Helper()
	for _, element := range blocks.Elts {
		pair, ok := element.(*ast.KeyValueExpr)
		if !ok || literalString(pair.Key) != "filter" {
			continue
		}
		block, ok := pair.Value.(*ast.CompositeLit)
		if !ok {
			t.Fatalf("%s filter block is not a composite literal", receiver)
		}
		if name := typeName(block.Type); name != "ListNestedBlock" {
			t.Fatalf("%s filter block is a %s, want ListNestedBlock", receiver, name)
		}
		for _, blockElement := range block.Elts {
			nestedPair, ok := blockElement.(*ast.KeyValueExpr)
			if !ok {
				continue
			}
			if ident, ok := nestedPair.Key.(*ast.Ident); !ok || ident.Name != "NestedObject" {
				continue
			}
			nested, ok := nestedPair.Value.(*ast.CompositeLit)
			if !ok {
				t.Fatalf("%s filter NestedObject is not a composite literal", receiver)
			}
			for _, nestedElement := range nested.Elts {
				attributePair, ok := nestedElement.(*ast.KeyValueExpr)
				if !ok {
					continue
				}
				if ident, ok := attributePair.Key.(*ast.Ident); ok && ident.Name == "Attributes" {
					return attributeSignatures(attributePair.Value)
				}
			}
		}
	}
	return []string{}
}

// attributeSignatures renders each attribute as name:Type:flag, deliberately
// dropping MarkdownDescription. The prose is the only part expected to differ
// per surface; the shape is what has to stay uniform.
func attributeSignatures(value ast.Expr) []string {
	literal, ok := value.(*ast.CompositeLit)
	if !ok {
		return []string{}
	}
	signatures := make([]string, 0, len(literal.Elts))
	for _, element := range literal.Elts {
		pair, ok := element.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		attribute, ok := pair.Value.(*ast.CompositeLit)
		if !ok {
			continue
		}
		signatures = append(signatures, fmt.Sprintf("%s:%s:%s",
			literalString(pair.Key), typeName(attribute.Type), attributeFlag(attribute)))
	}
	sort.Strings(signatures)
	return signatures
}

func attributeFlag(attribute *ast.CompositeLit) string {
	flags := make([]string, 0, 2)
	for _, element := range attribute.Elts {
		pair, ok := element.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		ident, ok := pair.Key.(*ast.Ident)
		if !ok {
			continue
		}
		switch ident.Name {
		case "Required", "Optional", "Computed":
			if value, ok := pair.Value.(*ast.Ident); ok && value.Name == "true" {
				flags = append(flags, ident.Name)
			}
		}
	}
	sort.Strings(flags)
	if len(flags) == 0 {
		return "none"
	}
	return strings.Join(flags, "+")
}

func typeName(expression ast.Expr) string {
	if selector, ok := expression.(*ast.SelectorExpr); ok {
		return selector.Sel.Name
	}
	if ident, ok := expression.(*ast.Ident); ok {
		return ident.Name
	}
	return ""
}

func literalString(expression ast.Expr) string {
	basic, ok := expression.(*ast.BasicLit)
	if !ok || basic.Kind != token.STRING {
		return ""
	}
	unquoted, err := strconv.Unquote(basic.Value)
	if err != nil {
		return ""
	}
	return unquoted
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func sortedScaffoldReceivers(shapes map[string]listSchemaShape) []string {
	keys := make([]string, 0, len(shapes))
	for key := range shapes {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
