// Package schemamodel compares the schema a surface SERVES against the runtime
// model that surface's code actually carries values in.
//
// WHY THIS EXISTS, and why nothing else could have caught what it catches.
// Every referee on this project compares the CANDIDATE against the RELEASED
// provider: the baseline projection, the behaviour inventory, the docs, the
// contract. All of them are cross-version. Nobody ever compared the two halves
// of the SAME provider against each other, and that is the gap fifty-four
// controller regressions came through -- they are not a divergence from the old
// provider, they are an internal inconsistency in the new one, so a
// cross-version referee is blind to them by construction.
//
// It is also not bounded by test coverage, which is the reason it is worth
// having rather than relying on a controller run. The run found the defect on
// nine surfaces; it could not find it on unifi_firewall_policy, which has no
// managed acceptance test and sixteen uses on the real fleet.
//
// RESOLVING A MODEL IS DONE BY TAG SET, PACKAGE-WIDE, AND BOTH HALVES OF THAT
// ARE LOAD-BEARING.
//
// Package-wide because models are shared. dhcpServerModel is declared in
// unifi/network_resource.go and used by unifi/network_data_source.go, so a
// lookup scoped to the file that serves a schema reports every shared model's
// members as missing. That mistake was made and caught during this work: it
// produced a confident "the data source has no field for boot or wins" about
// code that assigns to exactly those fields and compiles.
//
// By tag set rather than by name because there is no reliable name link. A
// nested attribute is populated through types.ObjectValueFrom with whatever
// struct the conversion code happened to build, and the struct's identifier is
// a convention, not a contract. What IS a contract is the set of tfsdk tags:
// the framework requires it to match the attribute set exactly, so an exact
// match identifies the model and a failure to match is itself the finding.
package schemamodel

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
)

// Model is one runtime struct that carries a surface's values, keyed by the
// tfsdk tags it declares.
type Model struct {
	// Name and File are for the failure message: a finding that cannot say
	// which struct it means sends the reader hunting.
	Name string
	File string
	// Fields maps a tfsdk tag to the Go type expression the field declares,
	// rendered as written -- "types.Object", "hwtypes.MACAddress",
	// "DhcpRelayValue". The text is what matters here, not the resolved type:
	// the question is what the author wrote.
	Fields map[string]string
	// Restated is the same shape as this model's own AttributeTypes() method
	// declares it, or nil if the model has no such method. It is NOT a second
	// model -- see restatements below for why keeping the distinction matters.
	Restated map[string]string
}

// Tags returns the model's tfsdk tags, sorted.
func (m Model) Tags() []string {
	tags := make([]string, 0, len(m.Fields))
	for tag := range m.Fields {
		tags = append(tags, tag)
	}
	sort.Strings(tags)
	return tags
}

// Index is every tfsdk-tagged struct in one Go package.
type Index struct {
	Models []Model
}

// IndexModels reads every non-test Go file in each dir.
//
// It takes several directories because a shape can live outside the package
// that serves it: unifi/models declares the client_info element shape that
// unifi/client_info_list_data_source.go builds its list from. Indexing only the
// serving package was this referee's third bug of the same family as the first
// two -- a search narrower than the claim it supports. Each of the three
// reported real, working code as missing.
//
// Test files are excluded deliberately: a fixture struct in a _test.go file
// with the same tag set as a real model would make the lookup ambiguous, and a
// referee that reports "two candidate models" because of a test helper is one
// people learn to ignore.
func IndexModels(dirs ...string) (*Index, error) {
	paths := make([]string, 0)
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			name := entry.Name()
			if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
				continue
			}
			paths = append(paths, filepath.Join(dir, name))
		}
	}
	sort.Strings(paths)

	index := &Index{Models: make([]Model, 0, len(paths))}
	restated := map[string]map[string]string{}
	fileSet := token.NewFileSet()
	for _, path := range paths {
		file, err := parser.ParseFile(fileSet, path, nil, 0)
		if err != nil {
			return nil, err
		}
		for _, declaration := range file.Decls {
			general, ok := declaration.(*ast.GenDecl)
			if !ok || general.Tok != token.TYPE {
				continue
			}
			for _, spec := range general.Specs {
				typeSpec, ok := spec.(*ast.TypeSpec)
				if !ok {
					continue
				}
				structType, ok := typeSpec.Type.(*ast.StructType)
				if !ok {
					continue
				}
				fields := taggedFields(fileSet, structType)
				if len(fields) == 0 {
					continue
				}
				index.Models = append(index.Models, Model{
					Name:   typeSpec.Name.Name,
					File:   filepath.Base(path),
					Fields: fields,
				})
			}
		}
		standalone, methods := attrTypeMaps(file, filepath.Base(path))
		index.Models = append(index.Models, standalone...)
		for receiver, shape := range methods {
			restated[receiver] = shape
		}
	}
	for i, model := range index.Models {
		if shape, ok := restated[model.Name]; ok {
			index.Models[i].Restated = shape
		}
	}
	return index, nil
}

// Disagreements returns every model whose own AttributeTypes() method declares
// a different member set than its tfsdk tags do.
//
// This is a check the tag/schema comparison cannot make, and it exists because
// leaving these indexed as separate shapes made the whole referee unable to
// fail. See restatements.
func (i *Index) Disagreements() []Model {
	out := make([]Model, 0)
	for _, model := range i.Models {
		if model.Restated == nil {
			continue
		}
		restatedTags := make([]string, 0, len(model.Restated))
		for tag := range model.Restated {
			restatedTags = append(restatedTags, tag)
		}
		sort.Strings(restatedTags)
		if !reflect.DeepEqual(model.Tags(), restatedTags) {
			out = append(out, model)
		}
	}
	return out
}

// GeneratedTypes returns every type name declared under the generated tree,
// both bare ("DhcpRelayValue") and package-qualified ("resource_network.
// DhcpRelayValue").
//
// This exists because the obvious spelling of the check is wrong in both
// directions. Matching a "Value" suffix catches timeouts.Value, which is the
// framework's own type and appears in thirty-nine runtime models; skipping
// anything package-qualified misses the form the defect ACTUALLY takes here,
// because the unifi package imports thirty-nine generated packages and would
// name their types qualified. Membership in the generated tree is the fact the
// check is about, so it is the fact the check reads.
func GeneratedTypes(root string) (map[string]struct{}, error) {
	names := map[string]struct{}{}
	fileSet := token.NewFileSet()
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".go") {
			return err
		}
		file, err := parser.ParseFile(fileSet, path, nil, 0)
		if err != nil {
			return err
		}
		pkg := filepath.Base(filepath.Dir(path))
		for _, declaration := range file.Decls {
			general, ok := declaration.(*ast.GenDecl)
			if !ok || general.Tok != token.TYPE {
				continue
			}
			for _, spec := range general.Specs {
				typeSpec, ok := spec.(*ast.TypeSpec)
				if !ok {
					continue
				}
				names[typeSpec.Name.Name] = struct{}{}
				names[pkg+"."+typeSpec.Name.Name] = struct{}{}
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return names, nil
}

// RestatedTags returns the member set the model's AttributeTypes() method
// declares, sorted.
func (m Model) RestatedTags() []string {
	tags := make([]string, 0, len(m.Restated))
	for tag := range m.Restated {
		tags = append(tags, tag)
	}
	sort.Strings(tags)
	return tags
}

// attrTypeMaps indexes the OTHER way this package declares an object shape: a
// map[string]attr.Type returned from a function, with no struct behind it at
// all. unifi_power_supervisor's power_sources and the two client-list element
// shapes are declared this way, nine functions in total.
//
// Indexing only tagged structs was this referee's second bug, and a worse one
// than the tag options: it reported six perfectly good shapes as having no
// model. A referee that does not know every way the codebase expresses a thing
// reports the codebase as wrong, and the reader learns to disbelieve it.
//
// A METHOD IS NOT A SHAPE, and getting that wrong made this referee unable to
// fail at all. Thirty-eight models carry an AttributeTypes() method that
// restates the same members their tfsdk tags already declare. Indexing those
// restatements as independent candidates means every one of those models has a
// twin: rename a tag on the struct and the method still matches the schema, so
// Resolve finds its one model and the referee stays green over a live
// mismatch. That was measured, not reasoned -- renaming dhcpServerModel's wins
// tag left the whole suite passing.
//
// So a map[string]attr.Type in a method named AttributeTypes is returned
// separately, keyed by its receiver type, to be checked AGAINST that struct
// rather than offered as an alternative to it. Maps in plain functions have no
// struct behind them and stay real shapes.
func attrTypeMaps(file *ast.File, fileName string) ([]Model, map[string]map[string]string) {
	models := make([]Model, 0)
	methods := map[string]map[string]string{}
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Body == nil {
			continue
		}
		receiver := receiverTypeName(function)
		ast.Inspect(function.Body, func(node ast.Node) bool {
			literal, ok := node.(*ast.CompositeLit)
			if !ok {
				return true
			}
			mapType, ok := literal.Type.(*ast.MapType)
			if !ok || exprString(nil, mapType.Key) != "string" || exprString(nil, mapType.Value) != "attr.Type" {
				return true
			}
			fields := map[string]string{}
			for _, element := range literal.Elts {
				pair, ok := element.(*ast.KeyValueExpr)
				if !ok {
					continue
				}
				key, ok := pair.Key.(*ast.BasicLit)
				if !ok || key.Kind != token.STRING {
					continue
				}
				fields[strings.Trim(key.Value, `"`)] = exprString(nil, pair.Value)
			}
			if len(fields) == 0 {
				return true
			}
			if receiver != "" && function.Name.Name == "AttributeTypes" {
				methods[receiver] = fields
				return true
			}
			models = append(models, Model{
				Name:   function.Name.Name + "()",
				File:   fileName,
				Fields: fields,
			})
			return true
		})
	}
	return models, methods
}

// receiverTypeName returns the receiver's type name with any pointer stripped,
// or "" for a plain function.
func receiverTypeName(function *ast.FuncDecl) string {
	if function.Recv == nil || len(function.Recv.List) == 0 {
		return ""
	}
	return strings.TrimPrefix(exprString(nil, function.Recv.List[0].Type), "*")
}

func taggedFields(fileSet *token.FileSet, structType *ast.StructType) map[string]string {
	fields := map[string]string{}
	for _, field := range structType.Fields.List {
		if field.Tag == nil {
			continue
		}
		tag := reflect.StructTag(strings.Trim(field.Tag.Value, "`")).Get("tfsdk")
		// The tag can carry options -- `tfsdk:"ip_address_pool,omitempty"` --
		// and the attribute name is the part before the first comma. Reading
		// the whole value was this indexer's first bug: natOutboundIPAddresses
		// declares all three of its members with an option, so the model never
		// matched and the referee reported a struct that is right there in
		// network_resource.go as missing.
		if comma := strings.IndexByte(tag, ','); comma >= 0 {
			tag = tag[:comma]
		}
		if tag == "" || tag == "-" {
			continue
		}
		fields[tag] = exprString(fileSet, field.Type)
	}
	return fields
}

func exprString(fileSet *token.FileSet, expr ast.Expr) string {
	switch typed := expr.(type) {
	case *ast.Ident:
		return typed.Name
	case *ast.SelectorExpr:
		return exprString(fileSet, typed.X) + "." + typed.Sel.Name
	case *ast.StarExpr:
		return "*" + exprString(fileSet, typed.X)
	default:
		return fmt.Sprintf("%T", expr)
	}
}

// Resolve finds the model whose tfsdk tags are exactly attributes.
//
// It returns every candidate rather than the first, because more than one match
// means the tag set does not identify a model and the caller must say so
// instead of picking.
func (i *Index) Resolve(attributes []string) []Model {
	want := append([]string(nil), attributes...)
	sort.Strings(want)
	matches := make([]Model, 0, 1)
	for _, model := range i.Models {
		if reflect.DeepEqual(model.Tags(), want) {
			matches = append(matches, model)
		}
	}
	return matches
}

// Nearest returns the model sharing the most tags with attributes, so a
// no-match failure can say which struct it probably meant and exactly how the
// two differ. A referee that reports "no model found" and stops has told the
// reader to go and do the search again by hand.
func (i *Index) Nearest(attributes []string) (Model, []string, []string) {
	want := map[string]struct{}{}
	for _, attribute := range attributes {
		want[attribute] = struct{}{}
	}
	var best Model
	bestScore := -1
	for _, model := range i.Models {
		score := 0
		for tag := range model.Fields {
			if _, ok := want[tag]; ok {
				score++
			}
		}
		if score > bestScore {
			best, bestScore = model, score
		}
	}
	if bestScore <= 0 {
		return Model{}, nil, nil
	}
	missing := make([]string, 0)
	for _, attribute := range attributes {
		if _, ok := best.Fields[attribute]; !ok {
			missing = append(missing, attribute)
		}
	}
	extra := make([]string, 0)
	for tag := range best.Fields {
		if _, ok := want[tag]; !ok {
			extra = append(extra, tag)
		}
	}
	sort.Strings(missing)
	sort.Strings(extra)
	return best, missing, extra
}
