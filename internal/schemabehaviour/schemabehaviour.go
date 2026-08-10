// Package schemabehaviour derives a hand-written schema's validators, plan
// modifiers, defaults and custom types from Go source.
//
// Migrating a surface to a generated schema means restating all four in its
// policy, because the compiler cannot infer them from a catalog and no schema
// comparison can see that they are gone. Restating them by hand is the largest
// remaining per-surface cost -- wlan carries eighty-three -- and it is the last
// step of the migration recipe still resting on someone being careful.
//
// It does not need to. The hand-written schema is Go source and the expressions
// are right there: stringvalidator.OneOf("all", "groups"),
// booldefault.StaticBool(true), timetypes.GoDurationType{}. This package reads
// them with go/ast and copies them out verbatim, which is both cheaper than
// typing them and unable to make the mistakes typing makes.
//
// Two properties are load-bearing.
//
// Expressions are COPIED, never synthesised. A derived expression is the source
// node printed back out, so it cannot say OneOf("all") where the source said
// OneOf("all", "groups"). Rendering is checked for idempotence -- printed,
// re-parsed, re-printed and compared -- so a mangled expression fails here
// rather than at the generator.
//
// An attribute this package cannot read is NAMED, not skipped. Attributes are
// discovered by looking for fields, the way the behaviour inventory's
// reflection does, rather than by switching on the ten concrete attribute
// types: a switch silently ignores anything added later, which is the hole the
// inventory exists to close. Where a value is not a literal at all --
// timeouts.Attributes(ctx, ...) is the estate's example -- the attribute is
// recorded as opaque, so "this attribute has no behaviour" and "this attribute
// keeps its behaviour somewhere this package cannot look" stay distinguishable.
package schemabehaviour

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// The four behaviours a schema carries that Terraform's protocol does not.
// These are the inventory's kind names, so a derived fact and an observed fact
// are comparable without translating between two vocabularies.
const (
	KindValidator    = "validator"
	KindPlanModifier = "plan_modifier"
	KindDefault      = "default"
	KindCustomType   = "custom_type"
)

// A Behaviour is one validator, plan modifier, default or custom type, with the
// Go expression that produces it and the imports that expression needs.
type Behaviour struct {
	// Path is the attribute path within the schema, dot separated, matching
	// the behaviour inventory's paths without their resource prefix.
	Path string `json:"path"`
	Kind string `json:"kind"`
	// Expression is the source expression, printed back out. It is what a
	// policy's schema_definition carries.
	Expression string `json:"expression"`
	// Imports are the package paths Expression's selectors resolve to, sorted.
	Imports []string `json:"imports,omitempty"`
	// Static is set only for a default the specification can express as a
	// value rather than as code. A default this package cannot reduce to a
	// value keeps its expression and no Static, which the emitter turns into a
	// custom default rather than guessing.
	Static *Static `json:"static,omitempty"`
	// ValueType is set only for a custom type: the value type that goes
	// alongside it in a specification. See customTypeValue for why it is
	// derived by convention and what checks it.
	ValueType string `json:"value_type,omitempty"`
}

// A Static is a default reduced to a literal value.
//
// Exactly one member is set. A struct of pointers rather than an `any` so that
// a JSON round trip cannot turn an int64 default into a float, which is what
// encoding/json does to an any holding a number.
type Static struct {
	Bool    *bool    `json:"bool,omitempty"`
	String  *string  `json:"string,omitempty"`
	Int64   *int64   `json:"int64,omitempty"`
	Float64 *float64 `json:"float64,omitempty"`
}

// A Surface is one managed resource's derived behaviour.
type Surface struct {
	// TypeName is the resource type name as the provider registers it, e.g.
	// unifi_wlan, so a Surface pairs with the behaviour inventory directly.
	TypeName string `json:"type_name"`
	File     string `json:"file"`
	// Delegated says the Schema method assigns something other than a literal
	// -- a surface already serving a generated schema. Nothing is derived from
	// one, and that is reported rather than passed over silently.
	Delegated bool `json:"delegated,omitempty"`
	// DelegatedTo is the expression the Schema method assigns, when Delegated.
	DelegatedTo string      `json:"delegated_to,omitempty"`
	Behaviours  []Behaviour `json:"behaviours"`
	// Attributes names every attribute path the schema declares, whether or not
	// it carries behaviour.
	//
	// A merge needs the difference. An attribute with no validators, plan
	// modifiers, defaults or custom type is ordinary, and most attributes are
	// one; an attribute the schema does not declare at all is the shape a wrong
	// rename takes. Indexing behaviour alone cannot tell those apart, and
	// reported every correct rename onto a plain attribute as a suspect one --
	// on three consecutive surfaces, which is how a check stops being read.
	Attributes []string `json:"attributes,omitempty"`
	// Opaque names attribute paths whose value this package could not read,
	// with what stopped it. A behaviour under one of these is invisible here
	// and must not be mistaken for a behaviour that does not exist.
	Opaque []Opaque `json:"opaque,omitempty"`
}

// An Opaque is an attribute whose behaviour could not be read, and why.
type Opaque struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

// DeriveDir derives every managed resource's behaviour from the Go source in
// dir, sorted by resource type name.
//
// Test files are excluded: they build schemas to exercise the walkers, and a
// fixture is not a schema the provider serves.
func DeriveDir(dir string) ([]Surface, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", dir, err)
	}

	fset := token.NewFileSet()
	var files []*parsedFile
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		path := filepath.Join(dir, name)
		file, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", path, err)
		}
		files = append(files, &parsedFile{path: path, file: file, imports: importsByName(file)})
	}

	prefix, err := providerTypeName(files)
	if err != nil {
		return nil, err
	}

	// Metadata and Schema are paired by receiver type rather than by file, so
	// a resource whose two methods live apart is still resolved.
	suffixes := map[string]string{}
	for _, parsed := range files {
		for receiver, suffix := range parsed.metadataSuffixes() {
			suffixes[receiver] = suffix
		}
	}

	// Package-level values are collected so that a schema built partly out of
	// shared pieces is still read whole. firewall_policy shared one map
	// between its two endpoints, which is twenty-four behaviours a walker that
	// only follows literals never sees.
	pkg := map[string]symbol{}
	for _, parsed := range files {
		for name, expr := range parsed.packageValues() {
			pkg[name] = symbol{file: parsed, expr: expr}
		}
	}

	sources := map[string]schemaSource{}
	for _, parsed := range files {
		for receiver, source := range parsed.resourceSchemas() {
			sources[receiver] = source
		}
	}

	var surfaces []Surface
	for receiver, source := range sources {
		suffix, ok := suffixes[receiver]
		if !ok {
			return nil, fmt.Errorf(
				"%s: %s has a resource Schema method and no Metadata method, so its "+
					"resource type name cannot be resolved", source.file.path, receiver)
		}
		resolved, err := resolve(sources, receiver)
		if err != nil {
			return nil, err
		}
		// A value declared inside the Schema method shadows a package-level one
		// of the same name, as it does in Go.
		scope := make(map[string]symbol, len(pkg))
		for name, defined := range pkg {
			scope[name] = defined
		}
		for name, expr := range localValues(resolved.body) {
			scope[name] = symbol{file: resolved.file, expr: expr}
		}

		surface, err := resolved.file.derive(prefix+suffix, resolved.assigned, scope)
		if err != nil {
			return nil, fmt.Errorf("%s: %s: %w", resolved.file.path, receiver, err)
		}
		surfaces = append(surfaces, surface)
	}

	sort.Slice(surfaces, func(i, j int) bool { return surfaces[i].TypeName < surfaces[j].TypeName })
	return surfaces, nil
}

type parsedFile struct {
	path    string
	file    *ast.File
	imports map[string]string
}

// A symbol is a package-level value and the file that defines it. The file
// travels with the expression because the imports its selectors resolve
// against are that file's, not the file that referred to it.
type symbol struct {
	file *parsedFile
	expr ast.Expr
}

// packageValues returns the file's package-level var and const initialisers, by
// name. Only single-name single-value specs are taken: `var a, b = f()` does
// not say which value is which without evaluating the call, and this package
// evaluates nothing.
func (p *parsedFile) packageValues() map[string]ast.Expr {
	values := map[string]ast.Expr{}
	for _, decl := range p.file.Decls {
		general, ok := decl.(*ast.GenDecl)
		if !ok || (general.Tok != token.VAR && general.Tok != token.CONST) {
			continue
		}
		for _, spec := range general.Specs {
			value, ok := spec.(*ast.ValueSpec)
			if !ok || len(value.Names) != 1 || len(value.Values) != 1 {
				continue
			}
			values[value.Names[0].Name] = value.Values[0]
		}
	}
	return values
}

// importsByName maps the identifier an import is referred to by onto its path.
//
// The alias when there is one, otherwise the last path segment. That is a
// convention rather than a guarantee -- a package may declare a name unlike its
// directory -- so a selector that resolves to no import contributes no import
// path, and the generated code fails to build rather than importing something
// wrong.
func importsByName(file *ast.File) map[string]string {
	names := map[string]string{}
	for _, spec := range file.Imports {
		path, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			continue
		}
		name := path[strings.LastIndex(path, "/")+1:]
		if spec.Name != nil {
			name = spec.Name.Name
		}
		if name == "_" || name == "." {
			continue
		}
		names[name] = path
	}
	return names
}

// providerTypeName reads the provider's own type name, which every resource's
// name is built from. Reading it rather than assuming "unifi" keeps the derived
// names tied to the source: renaming the provider renames these too.
func providerTypeName(files []*parsedFile) (string, error) {
	for _, parsed := range files {
		for _, decl := range parsed.file.Decls {
			function, ok := decl.(*ast.FuncDecl)
			if !ok || function.Name.Name != "Metadata" || function.Recv == nil {
				continue
			}
			if !parsed.paramIsPointerTo(function, 2, "provider", "MetadataResponse") {
				continue
			}
			for _, assigned := range assignmentsTo(function.Body, "TypeName") {
				if literal, ok := assigned.(*ast.BasicLit); ok && literal.Kind == token.STRING {
					return strconv.Unquote(literal.Value)
				}
			}
		}
	}
	return "", fmt.Errorf(
		"no provider Metadata method assigns a literal TypeName, so resource type " +
			"names cannot be built from the source")
}

// metadataSuffixes returns, per receiver type, the surface name suffix its
// Metadata method appends to the provider type name.
//
// Actions are accepted alongside managed resources, for the same reason
// resourceSchemas accepts them: an action whose schema is read but whose name
// cannot be resolved is not a partial success, it is an error that stops the
// whole derivation.
func (p *parsedFile) metadataSuffixes() map[string]string {
	suffixes := map[string]string{}
	for _, decl := range p.file.Decls {
		function, ok := decl.(*ast.FuncDecl)
		if !ok || function.Name.Name != "Metadata" || function.Recv == nil {
			continue
		}
		if !p.paramIsPointerTo(function, 2, "resource", "MetadataResponse") &&
			!p.paramIsPointerTo(function, 2, "action", "MetadataResponse") {
			continue
		}
		receiver := receiverType(function)
		if receiver == "" {
			continue
		}
		for _, assigned := range assignmentsTo(function.Body, "TypeName") {
			// resp.TypeName = req.ProviderTypeName + "_wlan" is the only form
			// accepted. A name built any other way is not derived by guessing.
			binary, ok := assigned.(*ast.BinaryExpr)
			if !ok || binary.Op != token.ADD {
				continue
			}
			if selector, ok := binary.X.(*ast.SelectorExpr); !ok || selector.Sel.Name != "ProviderTypeName" {
				continue
			}
			literal, ok := binary.Y.(*ast.BasicLit)
			if !ok || literal.Kind != token.STRING {
				continue
			}
			if suffix, err := strconv.Unquote(literal.Value); err == nil {
				suffixes[receiver] = suffix
			}
		}
	}
	return suffixes
}

// A schemaSource is where one receiver's schema comes from: an expression it
// assigns, or another receiver it hands the response to.
type schemaSource struct {
	file     *parsedFile
	assigned ast.Expr
	// delegate is the embedded receiver type whose Schema method fills the
	// response instead. unifi_account is the estate's case: it embeds
	// radiusUserResource, calls its Schema and adds a deprecation message, so
	// it serves that schema and its behaviour is that schema's behaviour.
	delegate string
	// body is the Schema method's body, kept so that values declared inside it
	// can be followed. firewall_policy's shared endpoint map is a local, not a
	// package-level var.
	body *ast.BlockStmt
}

// localValues returns the values a function body declares by name, from both
// `x := expr` and `var x = expr`.
//
// Only single-name single-value declarations are taken, and a name declared
// more than once is dropped rather than guessed at: this package does not
// evaluate anything, so it cannot know which assignment reached the use.
func localValues(body *ast.BlockStmt) map[string]ast.Expr {
	values := map[string]ast.Expr{}
	ambiguous := map[string]bool{}
	if body == nil {
		return values
	}
	remember := func(name string, expr ast.Expr) {
		if _, seen := values[name]; seen {
			ambiguous[name] = true
			return
		}
		values[name] = expr
	}
	ast.Inspect(body, func(node ast.Node) bool {
		switch declared := node.(type) {
		case *ast.AssignStmt:
			if declared.Tok != token.DEFINE || len(declared.Lhs) != 1 || len(declared.Rhs) != 1 {
				return true
			}
			if ident, ok := declared.Lhs[0].(*ast.Ident); ok {
				remember(ident.Name, declared.Rhs[0])
			}
		case *ast.DeclStmt:
			general, ok := declared.Decl.(*ast.GenDecl)
			if !ok || general.Tok != token.VAR {
				return true
			}
			for _, spec := range general.Specs {
				value, ok := spec.(*ast.ValueSpec)
				if !ok || len(value.Names) != 1 || len(value.Values) != 1 {
					continue
				}
				remember(value.Names[0].Name, value.Values[0])
			}
		}
		return true
	})
	for name := range ambiguous {
		delete(values, name)
	}
	return values
}

// resourceSchemas returns, per receiver type, where its schema comes from.
//
// The response parameter's type is what separates a managed resource from a
// data source, a list resource or an ephemeral resource, all of which have a
// method called Schema. This covers the same set as the behaviour inventory,
// which is managed resources AND actions.
//
// Actions were added when the inventory grew to read them. They had been
// invisible to both, so nothing noticed that the estate's one action carries a
// hwtypes.MACAddressType on device_mac -- a custom type that IS the validation
// on that attribute, and that a policy written by this tool would have omitted
// without saying so. The derivability test is what tied the two together: it
// compares what the provider applies against what this reads, so widening the
// inventory alone turned a silent gap into a failure.
func (p *parsedFile) resourceSchemas() map[string]schemaSource {
	sources := map[string]schemaSource{}
	for _, decl := range p.file.Decls {
		function, ok := decl.(*ast.FuncDecl)
		if !ok || function.Name.Name != "Schema" || function.Recv == nil {
			continue
		}
		if !p.paramIsPointerTo(function, 2, "resource", "SchemaResponse") &&
			!p.paramIsPointerTo(function, 2, "action", "SchemaResponse") {
			continue
		}
		receiver := receiverType(function)
		if receiver == "" {
			continue
		}
		source := schemaSource{file: p, body: function.Body}
		for _, value := range assignmentsTo(function.Body, "Schema") {
			source.assigned = value
		}
		if source.assigned == nil {
			source.delegate = embeddedSchemaCall(function.Body)
		}
		sources[receiver] = source
	}
	return sources
}

// embeddedSchemaCall finds `r.embedded.Schema(...)`, the form a wrapping
// resource uses to serve another's schema, and returns the embedded type name.
func embeddedSchemaCall(body *ast.BlockStmt) string {
	var embedded string
	if body == nil {
		return ""
	}
	ast.Inspect(body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		outer, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || outer.Sel.Name != "Schema" {
			return true
		}
		inner, ok := outer.X.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		embedded = inner.Sel.Name
		return false
	})
	return embedded
}

// resolve follows delegation to the receiver that actually holds the schema.
//
// The chain is bounded by the number of receivers, so a cycle is reported
// rather than spun on -- a wrapper that ends up delegating to itself would
// otherwise hang a code generator with no output at all.
func resolve(sources map[string]schemaSource, receiver string) (schemaSource, error) {
	seen := map[string]bool{}
	for {
		source, ok := sources[receiver]
		if !ok {
			return schemaSource{}, fmt.Errorf(
				"%s serves another receiver's schema and that receiver has no resource "+
					"Schema method", receiver)
		}
		if source.delegate == "" {
			return source, nil
		}
		if seen[receiver] {
			return schemaSource{}, fmt.Errorf(
				"%s delegates its schema in a cycle, so no schema is ever built", receiver)
		}
		seen[receiver] = true
		receiver = source.delegate
	}
}

// paramIsPointerTo reports whether a function's index-th parameter is a pointer
// to pkg.name, with pkg resolved through this file's imports so an aliased
// import is read correctly.
func (p *parsedFile) paramIsPointerTo(function *ast.FuncDecl, index int, pkg, name string) bool {
	params := function.Type.Params
	if params == nil {
		return false
	}
	var flat []ast.Expr
	for _, field := range params.List {
		count := len(field.Names)
		if count == 0 {
			count = 1
		}
		for range count {
			flat = append(flat, field.Type)
		}
	}
	if index >= len(flat) {
		return false
	}
	star, ok := flat[index].(*ast.StarExpr)
	if !ok {
		return false
	}
	selector, ok := star.X.(*ast.SelectorExpr)
	if !ok || selector.Sel.Name != name {
		return false
	}
	ident, ok := selector.X.(*ast.Ident)
	if !ok {
		return false
	}
	path := p.imports[ident.Name]
	return path == frameworkModule+"/"+pkg || path == frameworkModule+"/"+pkg+"/schema"
}

const frameworkModule = "github.com/hashicorp/terraform-plugin-framework"

func receiverType(function *ast.FuncDecl) string {
	if function.Recv == nil || len(function.Recv.List) == 0 {
		return ""
	}
	expr := function.Recv.List[0].Type
	if star, ok := expr.(*ast.StarExpr); ok {
		expr = star.X
	}
	if ident, ok := expr.(*ast.Ident); ok {
		return ident.Name
	}
	return ""
}

// assignmentsTo finds every `x.field = value` in a body and returns the values.
func assignmentsTo(body *ast.BlockStmt, field string) []ast.Expr {
	var values []ast.Expr
	if body == nil {
		return nil
	}
	ast.Inspect(body, func(node ast.Node) bool {
		assign, ok := node.(*ast.AssignStmt)
		if !ok {
			return true
		}
		for index, target := range assign.Lhs {
			selector, ok := target.(*ast.SelectorExpr)
			if !ok || selector.Sel.Name != field || index >= len(assign.Rhs) {
				continue
			}
			values = append(values, assign.Rhs[index])
		}
		return true
	})
	return values
}
