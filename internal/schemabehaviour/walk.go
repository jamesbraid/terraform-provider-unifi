package schemabehaviour

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"sort"
	"strconv"
	"strings"
)

// derive walks one resource's assigned schema expression.
func (p *parsedFile) derive(typeName string, assigned ast.Expr, pkg map[string]symbol) (Surface, error) {
	surface := Surface{TypeName: typeName, File: p.path}

	literal, from, ok := resolveComposite(assigned, p, pkg)
	if !ok {
		rendered, _, err := p.render(assigned)
		if err != nil {
			rendered = "<unprintable>"
		}
		surface.Delegated = true
		surface.DelegatedTo = rendered
		return surface, nil
	}

	walker := &walker{file: from, pkg: pkg}
	walker.descend("", literal, from)
	if walker.err != nil {
		return Surface{}, walker.err
	}

	sort.Slice(walker.found, func(i, j int) bool {
		left, right := walker.found[i], walker.found[j]
		if left.Path != right.Path {
			return left.Path < right.Path
		}
		if left.Kind != right.Kind {
			return left.Kind < right.Kind
		}
		return left.Expression < right.Expression
	})
	sort.Slice(walker.unread, func(i, j int) bool { return walker.unread[i].Path < walker.unread[j].Path })
	sort.Strings(walker.seen)

	surface.Behaviours = walker.found
	surface.Attributes = walker.seen
	surface.Opaque = walker.unread
	return surface, nil
}

type walker struct {
	file   *parsedFile
	pkg    map[string]symbol
	found  []Behaviour
	seen   []string
	unread []Opaque
	err    error
}

func (w *walker) opaque(path, reason string) {
	w.unread = append(w.unread, Opaque{Path: path, Reason: reason})
}

// descend walks a schema or nested-object literal: its attributes, then its
// blocks.
//
// A block holds its own validators and plan modifiers and contains attributes
// that hold theirs, so it is walked exactly as an attribute is -- reading only
// Attributes is what left the estate's four blocks and their fifty-six
// attributes unseen in the inventory.
func (w *walker) descend(prefix string, literal *ast.CompositeLit, from *parsedFile) {
	held := false
	for _, member := range []string{"Attributes", "Blocks"} {
		value := field_(literal, member)
		if value == nil {
			continue
		}
		held = true
		nested, in, ok := resolveComposite(value, from, w.pkg)
		if !ok || !isMap(nested) {
			// A nesting field that is there but is not a map this package can
			// reach is NAMED. Descending nowhere and saying nothing is how a
			// shared attribute map costs twenty-four behaviours without a
			// single message -- which is what firewall_policy's endpointAttrs
			// did before this.
			w.opaque(strings.TrimSuffix(prefix, "."), fmt.Sprintf(
				"%s is %s, which is not a map literal this package can follow, so the "+
					"attributes under it are not read", member, w.describe(value, from)))
			continue
		}
		w.attributes(prefix, nested, in)
	}
	if !held && prefix == "" {
		w.opaque("", "the schema literal has neither an Attributes nor a Blocks field")
	}
}

// attributes walks a map[string]schema.Attribute or map[string]schema.Block
// literal.
//
// Nesting is followed by looking for the fields that hold it rather than by
// switching on the concrete attribute type, for the reason the behaviour
// inventory gives for using reflection: a switch silently ignores a type added
// later, which is the hole this is meant to close.
func (w *walker) attributes(prefix string, attributes *ast.CompositeLit, from *parsedFile) {
	for _, element := range attributes.Elts {
		name, value, ok := namedElement(element)
		if !ok {
			w.opaque(prefix+"<unnamed>", "map entry has no string literal key")
			continue
		}
		path := prefix + name
		// Recorded before the value is resolved, because the schema has this
		// attribute whether or not its literal can be read. The name is the
		// only thing that separates an attribute carrying no behaviour from
		// one a policy renamed onto nothing.
		w.seen = append(w.seen, path)
		literal, in, ok := resolveComposite(value, from, w.pkg)
		if !ok {
			w.opaque(path, "value is "+w.describe(value, from)+
				", not a literal, so its behaviour is not in this source")
			continue
		}

		w.behaviours(path, literal, in)

		// SingleNestedAttribute and SingleNestedBlock hold their children
		// directly; the List, Set and Map nested forms hold them under
		// NestedObject.
		if field_(literal, "Attributes") != nil || field_(literal, "Blocks") != nil {
			w.descend(path+".", literal, in)
		}
		if object := field_(literal, "NestedObject"); object != nil {
			nested, objectFile, ok := resolveComposite(object, in, w.pkg)
			if !ok {
				w.opaque(path, "NestedObject is "+w.describe(object, in)+
					", not a literal, so the attributes under it are not read")
				continue
			}
			w.descend(path+".", nested, objectFile)
		}
	}
}

// behaviours reads one attribute literal's four behaviour fields.
func (w *walker) behaviours(path string, literal *ast.CompositeLit, from *parsedFile) {
	for _, field := range []struct{ name, kind string }{
		{"Validators", KindValidator},
		{"PlanModifiers", KindPlanModifier},
		{"Default", KindDefault},
		{"CustomType", KindCustomType},
	} {
		value := field_(literal, field.name)
		if value == nil {
			continue
		}

		switch field.kind {
		case KindValidator, KindPlanModifier:
			slice, in, ok := resolveComposite(value, from, w.pkg)
			if !ok {
				w.opaque(path, fmt.Sprintf(
					"%s is %s, not a slice literal, so the behaviours in it are not read",
					field.name, w.describe(value, from)))
				continue
			}
			for _, element := range slice.Elts {
				w.record(path, field.kind, element, in)
			}
		default:
			w.record(path, field.kind, value, from)
		}
	}
}

// describe renders an expression for a message, so an attribute reported as
// unreadable says what was there rather than only that something was.
func (w *walker) describe(expr ast.Expr, from *parsedFile) string {
	rendered, _, err := from.render(expr)
	if err != nil {
		return "an expression that could not be printed"
	}
	return rendered
}

func (w *walker) record(path, kind string, expr ast.Expr, from *parsedFile) {
	rendered, imports, err := from.render(expr)
	if err != nil {
		w.err = fmt.Errorf("%s %s: %w", path, kind, err)
		return
	}
	behaviour := Behaviour{Path: path, Kind: kind, Expression: rendered, Imports: imports}
	switch kind {
	case KindDefault:
		behaviour.Static = from.staticOf(expr)
	case KindCustomType:
		behaviour.ValueType = customTypeValue(rendered)
	}
	w.found = append(w.found, behaviour)
}

// render prints an expression back out as a single line and returns the import
// paths its selectors resolve to.
//
// The result is checked for idempotence: printed, re-parsed, re-printed and
// compared. An expression that does not survive that round trip is refused
// here, where the attribute and the source file are still in hand, rather than
// reaching the generator as a schema_definition that does not compile.
func (p *parsedFile) render(expr ast.Expr) (string, []string, error) {
	rendered, err := printExpr(foldStrings(expr))
	if err != nil {
		return "", nil, err
	}
	reparsed, err := parser.ParseExpr(rendered)
	if err != nil {
		return "", nil, fmt.Errorf("printed expression does not parse: %q: %w", rendered, err)
	}
	again, err := printExpr(reparsed)
	if err != nil {
		return "", nil, err
	}
	if again != rendered {
		return "", nil, fmt.Errorf(
			"printing is not idempotent for this expression, so what would be written is not "+
				"what was read: %q became %q", rendered, again)
	}

	seen := map[string]bool{}
	ast.Inspect(expr, func(node ast.Node) bool {
		selector, ok := node.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		ident, ok := selector.X.(*ast.Ident)
		if !ok {
			return true
		}
		if path, ok := p.imports[ident.Name]; ok {
			seen[path] = true
		}
		return true
	})
	paths := make([]string, 0, len(seen))
	for path := range seen {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return rendered, paths, nil
}

// foldStrings joins adjacent string literals into one, returning a new tree and
// leaving the parsed source untouched.
//
// A validator message written as "one part " + `another part` is one string to
// Go and to everyone reading the provider's output, and the hand transcription
// it is being compared against wrote it as one. Folding makes the two exactly
// equal rather than equal once someone squints, which is the difference between
// a check and an opinion.
//
// This is the one place a derived expression is not a character-for-character
// copy, so it is kept as narrow as it can be: both operands must already be
// string literals, and the joined value is computed by unquoting them rather
// than by manipulating their text. A concatenation involving anything else --
// a constant, a variable, a call -- is left exactly as written.
func foldStrings(expr ast.Expr) ast.Expr {
	switch node := expr.(type) {
	case *ast.BinaryExpr:
		left, right := foldStrings(node.X), foldStrings(node.Y)
		if node.Op == token.ADD {
			if joined, ok := joinLiterals(left, right); ok {
				return joined
			}
		}
		return &ast.BinaryExpr{X: left, Op: node.Op, Y: right}
	case *ast.CallExpr:
		args := make([]ast.Expr, len(node.Args))
		for index, arg := range node.Args {
			args[index] = foldStrings(arg)
		}
		return &ast.CallExpr{Fun: node.Fun, Args: args, Ellipsis: node.Ellipsis}
	case *ast.ParenExpr:
		return &ast.ParenExpr{X: foldStrings(node.X)}
	default:
		return expr
	}
}

func joinLiterals(left, right ast.Expr) (ast.Expr, bool) {
	leftLiteral, ok := left.(*ast.BasicLit)
	if !ok || leftLiteral.Kind != token.STRING {
		return nil, false
	}
	rightLiteral, ok := right.(*ast.BasicLit)
	if !ok || rightLiteral.Kind != token.STRING {
		return nil, false
	}
	leftValue, err := strconv.Unquote(leftLiteral.Value)
	if err != nil {
		return nil, false
	}
	rightValue, err := strconv.Unquote(rightLiteral.Value)
	if err != nil {
		return nil, false
	}
	return &ast.BasicLit{Kind: token.STRING, Value: strconv.Quote(leftValue + rightValue)}, true
}

// printExpr prints an expression with no position information, so the result
// does not carry the source's line breaks.
func printExpr(expr ast.Expr) (string, error) {
	var buffer bytes.Buffer
	if err := printer.Fprint(&buffer, token.NewFileSet(), expr); err != nil {
		return "", fmt.Errorf("print expression: %w", err)
	}
	return flatten(buffer.String()), nil
}

// flatten joins a printed expression onto one line.
//
// A raw string literal is the one place a newline can be part of the value
// rather than part of the layout, so an expression containing a backtick is
// left exactly as printed. It then fails the idempotence check if the layout
// mattered, which is the outcome to want: refused loudly rather than corrupted
// quietly.
func flatten(rendered string) string {
	if !strings.Contains(rendered, "\n") {
		return rendered
	}
	if strings.Contains(rendered, "`") {
		return rendered
	}
	var out []string
	for _, line := range strings.Split(rendered, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" {
			out = append(out, trimmed)
		}
	}
	joined := strings.Join(out, " ")
	// A trailing comma before a closing bracket is how gofmt writes a
	// multi-line call; on one line it is legal but not how the source would
	// have been written, so it goes.
	joined = strings.ReplaceAll(joined, ", )", ")")
	joined = strings.ReplaceAll(joined, ", }", "}")
	joined = strings.ReplaceAll(joined, ", ]", "]")
	return joined
}

const defaultsPackagePrefix = frameworkModule + "/resource/schema/"

// staticOf reduces a default to a literal value when the specification can
// carry it as one.
//
// Only the Static* constructors from the framework's own default packages
// qualify, and only with a literal argument. Anything else -- a default built
// from a variable, a computed value, a collection -- returns nil and keeps its
// expression, so the emitter writes a custom default rather than inventing a
// value it cannot see.
func (p *parsedFile) staticOf(expr ast.Expr) *Static {
	call, ok := expr.(*ast.CallExpr)
	if !ok || len(call.Args) != 1 {
		return nil
	}
	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || !strings.HasPrefix(selector.Sel.Name, "Static") {
		return nil
	}
	ident, ok := selector.X.(*ast.Ident)
	if !ok {
		return nil
	}
	path, ok := p.imports[ident.Name]
	if !ok || !strings.HasPrefix(path, defaultsPackagePrefix) || !strings.HasSuffix(path, "default") {
		return nil
	}

	switch argument := call.Args[0].(type) {
	case *ast.BasicLit:
		switch argument.Kind {
		case token.STRING:
			value, err := strconv.Unquote(argument.Value)
			if err != nil {
				return nil
			}
			return &Static{String: &value}
		case token.INT:
			value, err := strconv.ParseInt(argument.Value, 0, 64)
			if err != nil {
				return nil
			}
			return &Static{Int64: &value}
		case token.FLOAT:
			value, err := strconv.ParseFloat(argument.Value, 64)
			if err != nil {
				return nil
			}
			return &Static{Float64: &value}
		}
	case *ast.Ident:
		switch argument.Name {
		case "true":
			value := true
			return &Static{Bool: &value}
		case "false":
			value := false
			return &Static{Bool: &value}
		}
	}
	return nil
}

// customTypeValue derives the value type that accompanies a custom type in a
// specification: timetypes.GoDurationType{} is served by timetypes.GoDuration.
//
// This is a naming convention, not something the source states, so it is
// derived here and CHECKED elsewhere -- against the policies three migrated
// surfaces already carry, and finally by the generated code compiling. A custom
// type whose name is only "Type" yields nothing rather than a bare package
// name, which is how timeouts.Type reports that it needs a decision.
func customTypeValue(rendered string) string {
	name := strings.TrimSuffix(strings.TrimSpace(rendered), "{}")
	if name == "" || !strings.HasSuffix(name, "Type") {
		return ""
	}
	trimmed := strings.TrimSuffix(name, "Type")
	if trimmed == "" || strings.HasSuffix(trimmed, ".") {
		return ""
	}
	return trimmed
}

// namedElement splits a map literal entry into its string key and its value.
func namedElement(element ast.Expr) (string, ast.Expr, bool) {
	pair, ok := element.(*ast.KeyValueExpr)
	if !ok {
		return "", nil, false
	}
	literal, ok := pair.Key.(*ast.BasicLit)
	if !ok || literal.Kind != token.STRING {
		return "", nil, false
	}
	name, err := strconv.Unquote(literal.Value)
	if err != nil {
		return "", nil, false
	}
	return name, pair.Value, true
}

// compositeOf unwraps &T{...} and returns the literal, or nil when the
// expression is not one.
func compositeOf(expr ast.Expr) *ast.CompositeLit {
	if unary, ok := expr.(*ast.UnaryExpr); ok && unary.Op == token.AND {
		expr = unary.X
	}
	literal, _ := expr.(*ast.CompositeLit)
	return literal
}

// resolveComposite reduces an expression to a literal, following a reference to
// a package-level variable when it is one.
//
// Sharing a map between two attributes is ordinary Go and firewall_policy did
// it: its source and destination endpoints both said `Attributes: endpointAttrs`.
// Following the name is what makes twenty-four behaviours readable instead of
// absent. The file the literal came FROM travels with it, because the imports
// its expressions resolve against are that file's, not the caller's.
//
// The chain is bounded: a variable defined as another variable is followed, a
// cycle stops. Nothing else is evaluated -- a function call or a value built at
// run time is reported unreadable rather than guessed at.
func resolveComposite(expr ast.Expr, from *parsedFile, pkg map[string]symbol) (*ast.CompositeLit, *parsedFile, bool) {
	for range len(pkg) + 1 {
		if literal := compositeOf(expr); literal != nil {
			return literal, from, true
		}
		ident, ok := expr.(*ast.Ident)
		if !ok {
			return nil, nil, false
		}
		defined, ok := pkg[ident.Name]
		if !ok {
			return nil, nil, false
		}
		expr, from = defined.expr, defined.file
	}
	return nil, nil, false
}

func isMap(literal *ast.CompositeLit) bool {
	if literal == nil {
		return false
	}
	_, ok := literal.Type.(*ast.MapType)
	return ok
}

// field_ returns the value of a named field in a struct literal.
func field_(literal *ast.CompositeLit, name string) ast.Expr {
	for _, element := range literal.Elts {
		pair, ok := element.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		if ident, ok := pair.Key.(*ast.Ident); ok && ident.Name == name {
			return pair.Value
		}
	}
	return nil
}
