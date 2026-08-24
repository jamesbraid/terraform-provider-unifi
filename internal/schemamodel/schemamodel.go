// Package schemamodel compares the schema a surface serves against the
// runtime model that surface's code actually carries values in.
//
// Resolution is by tfsdk tag set, package-wide, not by struct name: models
// are shared across files, and there is no reliable name link between a
// nested attribute and the struct that populates it.
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
	// Name and File identify the struct in failure messages.
	Name string
	File string
	// Fields maps a tfsdk tag to the Go type expression as written
	// ("types.Object", "hwtypes.MACAddress"), not the resolved type.
	Fields map[string]string
	// Restated is the shape the model's own AttributeTypes() method declares,
	// or nil if it has none. It is not a second, rival model.
	Restated map[string]string
	// UpgradeOnly marks a model reachable only from the state-upgrade path;
	// it cannot be the model serving the current schema.
	UpgradeOnly bool
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

// IndexModels reads every non-test Go file in each dir. Several directories
// may be needed because a shape can live outside the package that serves it
// (unifi/models declares element shapes unifi's data sources build lists
// from). Test files are excluded: a fixture struct with the same tag set as
// a real model would make resolution ambiguous.
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
	functions := make([]declaredFunction, 0)
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
				fields := taggedFields(structType)
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
		functions = append(functions, declaredFunctions(file)...)
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
	index.foldFunctionRestatements()
	index.markUpgradeOnly(functions)
	return index, nil
}

// declaredFunction is one function body reduced to the two things reachability
// needs: the identifiers it mentions, and the functions it calls.
type declaredFunction struct {
	name       string
	references map[string]bool
	calls      map[string]bool
}

// markUpgradeOnly flags every model reached only from functions reachable
// from UpgradeState: such a model describes a schema version no longer
// served, even if its name carries no "V0"-style marker. A model mentioned
// anywhere outside the upgrade path stays a candidate.
func (i *Index) markUpgradeOnly(functions []declaredFunction) {
	reachable := map[string]bool{"UpgradeState": true}
	queue := make([]string, 0)
	for _, function := range functions {
		if function.name != "UpgradeState" {
			continue
		}
		for called := range function.calls {
			queue = append(queue, called)
		}
	}
	for len(queue) > 0 {
		name := queue[0]
		queue = queue[1:]
		if reachable[name] {
			continue
		}
		reachable[name] = true
		for _, function := range functions {
			if function.name != name {
				continue
			}
			for called := range function.calls {
				queue = append(queue, called)
			}
		}
	}

	for at, model := range i.Models {
		mentioned := false
		onlyFromUpgrade := true
		for _, function := range functions {
			if !function.references[model.Name] {
				continue
			}
			mentioned = true
			if !reachable[function.name] {
				onlyFromUpgrade = false
			}
		}
		i.Models[at].UpgradeOnly = mentioned && onlyFromUpgrade
	}
}

// foldFunctionRestatements attaches a function-declared shape to the struct
// it restates, instead of leaving the two indexed as rivals. A function is
// folded only when exactly one struct carries its member set; an ambiguous
// match stays an independent shape rather than being attached arbitrarily.
func (i *Index) foldFunctionRestatements() {
	dropped := make(map[int]bool)
	for at, model := range i.Models {
		if !strings.HasSuffix(model.Name, "()") {
			continue
		}
		matches := make([]int, 0, 1)
		for candidateAt, candidate := range i.Models {
			if strings.HasSuffix(candidate.Name, "()") {
				continue
			}
			if reflect.DeepEqual(candidate.Tags(), model.Tags()) {
				matches = append(matches, candidateAt)
			}
		}
		if len(matches) != 1 {
			continue
		}
		// Don't clobber an existing method restatement with this one.
		if i.Models[matches[0]].Restated == nil {
			i.Models[matches[0]].Restated = model.Fields
		}
		dropped[at] = true
	}
	kept := make([]Model, 0, len(i.Models))
	for at, model := range i.Models {
		if !dropped[at] {
			kept = append(kept, model)
		}
	}
	i.Models = kept
}

// Disagreements returns every model whose own AttributeTypes() method
// declares a different member set than its tfsdk tags do.
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
// both bare ("DhcpRelayValue") and package-qualified
// ("resource_network.DhcpRelayValue").
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

// attrTypeMaps indexes the other way this package declares an object shape:
// a map[string]attr.Type returned from a function, with no struct behind it.
// A map returned from a method named AttributeTypes is kept separately, keyed
// by receiver, to be checked against that struct rather than indexed as a
// rival shape; maps in plain functions stay real, independent shapes.
func attrTypeMaps(file *ast.File, fileName string) ([]Model, map[string]map[string]string) {
	models := make([]Model, 0)
	methods := map[string]map[string]string{}
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Body == nil {
			continue
		}
		receiver := receiverTypeName(function)
		// Only a single-statement body counts as a declaration; a longer body
		// that builds an attr.Type map inline is a use of a shape, not one
		// declaring it.
		if function.Body != nil && len(function.Body.List) != 1 {
			continue
		}
		ast.Inspect(function.Body, func(node ast.Node) bool {
			literal, ok := node.(*ast.CompositeLit)
			if !ok {
				return true
			}
			mapType, ok := literal.Type.(*ast.MapType)
			if !ok || exprString(mapType.Key) != "string" || exprString(mapType.Value) != "attr.Type" {
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
				fields[strings.Trim(key.Value, `"`)] = exprString(pair.Value)
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

// declaredFunctions reduces every function in a file to the identifiers it
// mentions and the functions it calls, which is all markUpgradeOnly needs.
// Calls are matched by name, not resolved symbol: a collision can only make
// more functions look reachable, never exclude a model that's actually live.
func declaredFunctions(file *ast.File) []declaredFunction {
	out := make([]declaredFunction, 0)
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Body == nil {
			continue
		}
		current := declaredFunction{
			name:       function.Name.Name,
			references: map[string]bool{},
			calls:      map[string]bool{},
		}
		ast.Inspect(function.Body, func(node ast.Node) bool {
			switch typed := node.(type) {
			case *ast.Ident:
				current.references[typed.Name] = true
			case *ast.CallExpr:
				switch callee := typed.Fun.(type) {
				case *ast.Ident:
					current.calls[callee.Name] = true
				case *ast.SelectorExpr:
					current.calls[callee.Sel.Name] = true
				}
			}
			return true
		})
		out = append(out, current)
	}
	return out
}

// receiverTypeName returns the receiver's type name with any pointer stripped,
// or "" for a plain function.
func receiverTypeName(function *ast.FuncDecl) string {
	if function.Recv == nil || len(function.Recv.List) == 0 {
		return ""
	}
	return strings.TrimPrefix(exprString(function.Recv.List[0].Type), "*")
}

func taggedFields(structType *ast.StructType) map[string]string {
	fields := map[string]string{}
	for _, field := range structType.Fields.List {
		if field.Tag == nil {
			continue
		}
		tag := reflect.StructTag(strings.Trim(field.Tag.Value, "`")).Get("tfsdk")
		// A tag can carry options (`tfsdk:"ip_address_pool,omitempty"`); the
		// attribute name is only the part before the first comma.
		if comma := strings.IndexByte(tag, ','); comma >= 0 {
			tag = tag[:comma]
		}
		if tag == "" || tag == "-" {
			continue
		}
		fields[tag] = exprString(field.Type)
	}
	return fields
}

func exprString(expr ast.Expr) string {
	switch typed := expr.(type) {
	case *ast.Ident:
		return typed.Name
	case *ast.SelectorExpr:
		return exprString(typed.X) + "." + typed.Sel.Name
	case *ast.StarExpr:
		return "*" + exprString(typed.X)
	default:
		return fmt.Sprintf("%T", expr)
	}
}

// Resolve finds the model whose tfsdk tags are exactly attributes. It
// returns every candidate rather than the first, since more than one match
// means the tag set does not identify a model.
func (i *Index) Resolve(attributes []string) []Model {
	want := append([]string(nil), attributes...)
	sort.Strings(want)
	matches := make([]Model, 0, 1)
	for _, model := range i.Models {
		if model.UpgradeOnly {
			continue
		}
		if reflect.DeepEqual(model.Tags(), want) {
			matches = append(matches, model)
		}
	}
	return matches
}

// Nearest returns the model sharing the most tags with attributes, so a
// no-match failure can say which struct it probably meant and how the two
// differ.
func (i *Index) Nearest(attributes []string) (Model, []string, []string) {
	want := map[string]struct{}{}
	for _, attribute := range attributes {
		want[attribute] = struct{}{}
	}
	var best Model
	bestScore := -1
	for _, model := range i.Models {
		// Same exclusion as Resolve, so an excluded model can't be offered
		// here as the nearest miss to an exact match Resolve just refused.
		if model.UpgradeOnly {
			continue
		}
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
