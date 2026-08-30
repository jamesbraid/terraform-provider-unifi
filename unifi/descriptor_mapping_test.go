package unifi

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/ubiquiti-community/terraform-provider-unifi/internal/sdkshape"
)

const (
	goUnifiPackage         = "github.com/ubiquiti-community/go-unifi/unifi"
	goUnifiSettingsPackage = goUnifiPackage + "/settings"
)

// loadSDK resolves the go-unifi package once. It is the slowest thing here by
// an order of magnitude and both tests need it. settings is folded in
// alongside the root package: unifi_setting's per-section descriptors (e.g.
// setting_mgmt) declare their SDK type argument from there.
var loadSDK = sync.OnceValues(func() (*sdkshape.Package, error) {
	return sdkshape.Load(goUnifiPackage, goUnifiSettingsPackage)
})

// The descriptors and the mapping artifacts must agree on which fields exist
// and what kind each one is. This is a conformance check, not a generator --
// cmd/provider-spec-compiler owns emitting descriptors.
//
// It reads descriptor source via go/ast rather than the running Spec: the
// field kind and SDK identifier aren't on resourcekit's Field interface (only
// WireName() is), so a Fields slice built outside the literal is invisible
// here. Elide values are deliberately not checked here --
// TestEveryDescriptorElideAgreesWithItsSchema owns that comparison against the
// generated schema. The per-surface tests (e.g.
// TestClientQosRateDescriptorCoversEveryManagedField) hardcode their own
// expectation rather than deriving one from the mapping, so neither subsumes
// the other -- keep both.

// descriptorField is one entry of a Spec's Fields slice, read off the source.
type descriptorField struct {
	Kind string // StringField, Int64PtrField, DurationField, ...
	Wire string
	// Wires is set instead of Wire by ScatteredObjectField, which maps one
	// model object onto several flat SDK attributes.
	Wires   []string
	Model   string // the model struct field the closure returns
	SDK     string // the SDK struct field the closure returns
	Wrapper string // ReadOnly, or empty
}

// wires is every SDK attribute this entry maps. Ask this, not .Wire: a
// ScatteredObjectField's five wires would otherwise report four as carried
// by nothing.
func (f descriptorField) wires() []string {
	if len(f.Wires) > 0 {
		return f.Wires
	}
	if f.Wire == "" {
		return nil
	}
	return []string{f.Wire}
}

// descriptor is one parsed *_descriptor.go.
type descriptor struct {
	TypeName string
	// AlwaysWire is the Spec's list of wires sent on every request. A managed
	// field can round-trip through it instead of through a Fields entry, which
	// is how a write-only secret and a translated set are carried.
	AlwaysWire map[string]bool
	// MappedElsewhere is the Spec's list of wires that round-trip through
	// something other than a Fields entry on this Spec: a sibling document --
	// an Extra sharing this section's model, with its own Spec and its own
	// SDK type (unifi_setting's usg and usg_geo share one mapping file) -- or
	// this same Spec writing the wire through a dedicated SDK method instead
	// of the general masked write (unifi_device's "port_overrides"). Either
	// way it's accounted for without the per-wire SDK-member check below,
	// which asks this descriptor's own SDKType and, for the sibling case,
	// would be the wrong struct.
	MappedElsewhere map[string]bool
	SDKType         string            // the Spec's second type argument, package-qualified, e.g. settings.Dashboard
	ModelTags       map[string]string // model Go field -> tfsdk tag
	Fields          []descriptorField
}

// mappingField is one entry of a *.mapping.json.
type mappingField struct {
	StructuralName string `json:"structural_name"`
	TerraformName  string `json:"terraform_name"`
	StructuralType string `json:"structural_type"`
	TerraformType  string `json:"terraform_type"`
	Disposition    string `json:"disposition"`
}

// TestEveryDescriptorAgreesWithItsSources checks both directions: a
// descriptor naming a wire the mapping doesn't have is a typo that compiles,
// and a mapping field with no descriptor entry silently stops round-tripping.
// Neither direction can see the other's failure.
func TestEveryDescriptorAgreesWithItsSources(t *testing.T) {
	descriptors := loadDescriptors(t)
	if len(descriptors) == 0 {
		t.Fatal("no descriptors were parsed, so every verdict below would be vacuous")
	}

	sdk, err := loadSDK()
	if err != nil {
		t.Fatalf("resolving %s: %v", goUnifiPackage, err)
	}

	for _, name := range sortedDescriptorNames(descriptors) {
		desc := descriptors[name]
		t.Run(name, func(t *testing.T) {
			mapping := loadMapping(t, name)
			sdkMembers, ok := sdk.Members(desc.SDKType)
			if !ok {
				t.Fatalf("the SDK has no struct %s, which this descriptor declares as its type "+
					"argument; every SDK comparison below would be vacuous", desc.SDKType)
			}

			// _id and site are the Spec's own ID and Site closures rather than
			// Fields entries, so they are not expected in the slice.
			expected := map[string]mappingField{}
			for _, f := range mapping {
				if f.Disposition != "managed" {
					continue
				}
				if f.TerraformName == "id" || f.TerraformName == "site" {
					continue
				}
				// An empty StructuralName names no wire at all (e.g. vpn_client's
				// parsed-only wireguard.configuration), so no descriptor field
				// can carry it.
				if f.StructuralName == "" {
					continue
				}
				expected[f.StructuralName] = f
			}

			got := map[string]descriptorField{}
			for _, f := range desc.Fields {
				for _, wire := range f.wires() {
					got[wire] = f
				}
			}

			// A wire is accounted for by any of three artifacts: managed in
			// the mapping, in AlwaysWire (a hook-derived value with no managed
			// attribute), or named by a claim (a wire that isn't one-to-one
			// with the schema).
			claimed := claimedStructuralNames(t, name)
			for wire := range got {
				if _, ok := expected[wire]; ok {
					continue
				}
				if desc.AlwaysWire[wire] || claimed[wire] {
					continue
				}
				t.Errorf("the descriptor carries wire %q, which is not a managed field of "+
					"%s.mapping.json, not in AlwaysWire and not named by a claim; a wire "+
					"name that matches nothing is sent to the controller and silently "+
					"ignored", wire, name)
			}
			// A managed field can also be carried by AlwaysWire alone, with no
			// Fields entry -- e.g. a write-only secret set by a BeforeSend
			// hook, or a field derived from another -- or by MappedElsewhere,
			// when a sibling document (an Extra sharing this section) carries
			// it under its own Spec instead.
			for wire, want := range expected {
				if _, ok := got[wire]; ok {
					continue
				}
				if desc.AlwaysWire[wire] || desc.MappedElsewhere[wire] {
					continue
				}
				t.Errorf("%s.mapping.json declares %q managed (terraform name %q) and no "+
					"descriptor field carries it, nor is it in AlwaysWire or MappedElsewhere, "+
					"so that attribute does not round-trip", name, wire, want.TerraformName)
			}

			for wire, f := range got {
				// A scattered field is checked only on SDK JSON membership: its
				// model field is the parent object, not any one wire, so the
				// per-wire mapping/model comparison below doesn't apply to it.
				if f.Kind == "ScatteredObjectField" {
					if _, ok := sdkMembers[wire]; !ok {
						t.Errorf("%s: the SDK struct %s has no JSON member of this name, so "+
							"this scattered entry puts a name on the mask that the controller "+
							"never reads", wire, desc.SDKType)
					}
					continue
				}
				want, ok := expected[wire]
				if !ok {
					continue
				}
				if kind := derivableKind(want); kind != "" && !kindAgrees(f.Kind, kind) {
					t.Errorf("%s: the mapping says %s -> %s, which is a %s, and the descriptor "+
						"uses %s", wire, want.StructuralType, want.TerraformType, kind, f.Kind)
				}

				// Checks the pairing, not just set membership: two same-typed
				// fields with their wires swapped would satisfy every check
				// above without this.
				member, ok := sdkMembers[wire]
				if !ok {
					t.Errorf("%s: the SDK struct %s has no JSON member of this name, so this "+
						"entry names a field the controller never reads", wire, desc.SDKType)
					continue
				}
				if f.SDK != member.GoName {
					t.Errorf("%s: the entry names SDK field %s, and %s's member %q is carried "+
						"by %s. Either this entry is paired with another field, or the SDK "+
						"renamed it", wire, f.SDK, desc.SDKType, wire, member.GoName)
				}
				if member.Pointer != kindCarriesAPointer(f.Kind) {
					t.Errorf("%s: the SDK field is pointer=%v and the descriptor uses %s; a "+
						"pointer distinguishes absent from zero and the two must agree",
						wire, member.Pointer, f.Kind)
				}
				if tag, ok := desc.ModelTags[f.Model]; !ok {
					t.Errorf("%s: the entry names model field %s, which the model struct does "+
						"not declare", wire, f.Model)
				} else if tag != want.TerraformName {
					t.Errorf("%s: the entry's model field %s is tfsdk:%q and the mapping's "+
						"terraform name is %q", wire, f.Model, tag, want.TerraformName)
				}
			}
		})
	}
}

// derivableKind is the field kind the mapping alone implies, or "" where the
// mapping cannot say. It always returns the non-pointer kind: pointer-ness is
// a property of the SDK struct, not the mapping, so Int64Field and
// Int64PtrField are treated as agreeing here.
func derivableKind(f mappingField) string {
	switch {
	case f.StructuralType == "int64" && f.TerraformType == "string":
		return "DurationField"
	case f.StructuralType == "array<string>" && f.TerraformType == "set":
		return "StringSetField"
	case f.StructuralType == "array<string>" && f.TerraformType == "list":
		return "StringListField"
	case f.StructuralType == "array<object>" && f.TerraformType == "list_nested":
		return "ObjectListField"
	case f.StructuralType == "string" && f.TerraformType == "string":
		return "StringField"
	case f.StructuralType == "bool" && f.TerraformType == "bool":
		return "BoolField"
	case f.StructuralType == "int64" && f.TerraformType == "int64":
		return "Int64Field"
	}
	return ""
}

// kindAgrees compares the kinds the mapping can actually distinguish. Two
// distinctions are dropped because the mapping itself can't make them:
// pointer-ness (checked separately against the SDK field) and StringLikeField
// vs. a plain string (the mapping records both as string -> string).
func kindAgrees(got, want string) bool {
	base := strings.Replace(strings.Replace(got, "Like", "", 1), "Ptr", "", 1)
	return base == want
}

// TestDescriptorDerivabilityIsReported asserts nothing about the numbers on
// purpose: how much of a descriptor a generator could emit isn't a property
// the tree should be held to. The numbers are only logged, for a person
// deciding whether to write the generator.
func TestDescriptorDerivabilityIsReported(t *testing.T) {
	descriptors := loadDescriptors(t)
	if len(descriptors) == 0 {
		t.Fatal("no descriptors were parsed, so the report below would describe nothing")
	}

	sdk, err := loadSDK()
	if err != nil {
		t.Fatalf("resolving %s: %v", goUnifiPackage, err)
	}

	var total, wireOK, kindOK, pointerNeedsSDK, identNeedsSDK int
	var needsSDK []string

	for _, name := range sortedDescriptorNames(descriptors) {
		desc := descriptors[name]
		byWire := map[string]mappingField{}
		for _, f := range loadMapping(t, name) {
			byWire[f.StructuralName] = f
		}
		members, _ := sdk.Members(desc.SDKType)
		for _, f := range desc.Fields {
			for _, wire := range f.wires() {
				total++
				want, ok := byWire[wire]
				if !ok {
					continue
				}
				wireOK++
				if k := derivableKind(want); k != "" && kindAgrees(f.Kind, k) {
					kindOK++
				}
				member, ok := members[wire]
				if !ok {
					continue
				}
				if member.Pointer {
					pointerNeedsSDK++
					needsSDK = append(needsSDK, fmt.Sprintf(
						"%s.%s is *T in the SDK and %s in the mapping", name, wire, want.StructuralType))
				}
				// Strip both separators before comparing: wire names use
				// underscores and, for static_route, hyphens too (e.g.
				// "static-route_distance" is StaticRouteDistance in Go).
				flattened := strings.NewReplacer("_", "", "-", "").Replace(wire)
				if !strings.EqualFold(flattened, member.GoName) {
					identNeedsSDK++
					needsSDK = append(needsSDK, fmt.Sprintf(
						"%s.%s is %s in the SDK", name, wire, member.GoName))
				}
			}
		}
	}

	sort.Strings(needsSDK)
	t.Logf("%d descriptor(s), %d field(s)", len(descriptors), total)
	t.Logf("  wire name from structural_name       %d/%d", wireOK, total)
	t.Logf("  field kind from the type pair        %d/%d  (pointer-ness excluded)", kindOK, total)
	t.Logf("  pointer-ness the mapping cannot say  %d/%d", pointerNeedsSDK, total)
	t.Logf("  Go identifier not a case-fold of the wire  %d/%d", identNeedsSDK, total)
	if len(needsSDK) > 0 {
		t.Logf("  facts only the SDK struct carries:\n    %s", strings.Join(needsSDK, "\n    "))
	}
}

func sortedDescriptorNames(m map[string]descriptor) []string {
	names := make([]string, 0, len(m))
	for name := range m {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func loadMapping(t *testing.T, surface string) []mappingField {
	t.Helper()
	path := filepath.Join("..", "provider-codegen", "generated", surface+".mapping.json")
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the mapping for %s: %v", surface, err)
	}
	var doc struct {
		Fields []mappingField `json:"fields"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		t.Fatalf("decoding %s: %v", path, err)
	}
	if len(doc.Fields) == 0 {
		t.Fatalf("%s carries no fields, so a comparison against it would assert nothing", path)
	}
	return doc.Fields
}

// loadDescriptors parses every *_descriptor.go in this package and returns the
// Fields entries keyed by the Spec's TypeName. It fails on an element shape
// it doesn't recognize rather than skipping it: a skipped entry would read
// as a missing field rather than a reader bug.
func loadDescriptors(t *testing.T) map[string]descriptor {
	t.Helper()
	paths, err := filepath.Glob("*_descriptor.go")
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]descriptor{}
	for _, path := range paths {
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Fatalf("parsing %s: %v", path, err)
		}
		modelTags := modelTagsIn(file)
		helpers := helpersIn(file)
		aliases := aliasesIn(file)
		ast.Inspect(file, func(n ast.Node) bool {
			lit, ok := n.(*ast.CompositeLit)
			if !ok {
				return true
			}
			if exprName(lit.Type) != "Spec" {
				return true
			}
			desc := descriptor{
				ModelTags:       map[string]string{},
				AlwaysWire:      map[string]bool{},
				MappedElsewhere: map[string]bool{},
			}
			if args, ok := lit.Type.(*ast.IndexListExpr); ok && len(args.Indices) == 2 {
				desc.SDKType = resolveAlias(aliases, qualifiedExprName(args.Indices[1]))
				desc.ModelTags = modelTags[resolveAlias(aliases, exprName(args.Indices[0]))]
			}
			for _, el := range lit.Elts {
				kv, ok := el.(*ast.KeyValueExpr)
				if !ok {
					continue
				}
				key, _ := kv.Key.(*ast.Ident)
				if key == nil {
					continue
				}
				switch key.Name {
				case "TypeName":
					if bl, ok := kv.Value.(*ast.BasicLit); ok {
						desc.TypeName, _ = strconv.Unquote(bl.Value)
					}
				case "AlwaysWire":
					lit, ok := kv.Value.(*ast.CompositeLit)
					if !ok {
						t.Fatalf("%s: AlwaysWire is %T, not a composite literal", path, kv.Value)
					}
					for _, item := range lit.Elts {
						bl, ok := item.(*ast.BasicLit)
						if !ok {
							t.Fatalf("%s: an AlwaysWire entry is %T, not a string literal; "+
								"skipping it would let a dropped field look accounted for",
								path, item)
						}
						wire, _ := strconv.Unquote(bl.Value)
						desc.AlwaysWire[wire] = true
					}
				case "MappedElsewhere":
					lit, ok := kv.Value.(*ast.CompositeLit)
					if !ok {
						t.Fatalf("%s: MappedElsewhere is %T, not a composite literal", path, kv.Value)
					}
					for _, item := range lit.Elts {
						bl, ok := item.(*ast.BasicLit)
						if !ok {
							t.Fatalf("%s: a MappedElsewhere entry is %T, not a string literal; "+
								"skipping it would let a dropped field look accounted for",
								path, item)
						}
						wire, _ := strconv.Unquote(bl.Value)
						desc.MappedElsewhere[wire] = true
					}
				case "Fields":
					slice, ok := kv.Value.(*ast.CompositeLit)
					if !ok {
						t.Fatalf("%s: Fields is %T, not a composite literal", path, kv.Value)
					}
					for _, item := range slice.Elts {
						desc.Fields = append(desc.Fields, parseField(t, path, item, helpers))
					}
				}
			}
			if desc.TypeName != "" {
				if desc.SDKType == "" {
					t.Fatalf("%s: could not read the Spec's SDK type argument", path)
				}
				out[desc.TypeName] = desc
			}
			return true
		})
	}
	return out
}

// modelTagsIn maps each struct type in the file to its Go field -> tfsdk tag.
func modelTagsIn(file *ast.File) map[string]map[string]string {
	out := map[string]map[string]string{}
	ast.Inspect(file, func(n ast.Node) bool {
		spec, ok := n.(*ast.TypeSpec)
		if !ok {
			return true
		}
		st, ok := spec.Type.(*ast.StructType)
		if !ok || st.Fields == nil {
			return true
		}
		tags := map[string]string{}
		for _, f := range st.Fields.List {
			if f.Tag == nil || len(f.Names) == 0 {
				continue
			}
			raw, err := strconv.Unquote(f.Tag.Value)
			if err != nil {
				continue
			}
			if tag := reflect.StructTag(raw).Get("tfsdk"); tag != "" {
				tags[f.Names[0].Name] = tag
			}
		}
		out[spec.Name.Name] = tags
		return true
	})
	return out
}

// helperSpec is a per-file constructor such as
//
//	str := func(wire string, model func(*M) *types.String,
//		sdk func(*S) *string, elide resourcekit.ElideZero,
//	) resourcekit.StringField[M, S] {
//		return resourcekit.StringField[M, S]{Wire: wire, Model: model, SDK: sdk, Elide: elide}
//	}
//
// Argument positions are read off each helper's own body, not assumed by
// position; a constructor that does more than forward its parameters isn't
// recognized, and fails.
type helperSpec struct {
	Kind  string
	Wire  int
	Model int
	SDK   int
}

// helpersIn finds the field constructors declared in a file, whether they are
// package-level functions or closures assigned inside the spec function.
func helpersIn(file *ast.File) map[string]helperSpec {
	out := map[string]helperSpec{}
	consider := func(name string, params *ast.FieldList, body *ast.BlockStmt) {
		if params == nil || body == nil || len(body.List) != 1 {
			return
		}
		ret, ok := body.List[0].(*ast.ReturnStmt)
		if !ok || len(ret.Results) != 1 {
			return
		}
		lit, ok := ret.Results[0].(*ast.CompositeLit)
		if !ok {
			return
		}
		kind := exprName(lit.Type)
		if !strings.HasSuffix(kind, "Field") {
			return
		}
		index := map[string]int{}
		position := 0
		for _, group := range params.List {
			for _, ident := range group.Names {
				index[ident.Name] = position
				position++
			}
		}
		spec := helperSpec{Kind: kind, Wire: -1, Model: -1, SDK: -1}
		for _, el := range lit.Elts {
			kv, ok := el.(*ast.KeyValueExpr)
			if !ok {
				continue
			}
			key, _ := kv.Key.(*ast.Ident)
			value, _ := kv.Value.(*ast.Ident)
			if key == nil || value == nil {
				continue
			}
			at, ok := index[value.Name]
			if !ok {
				continue
			}
			switch key.Name {
			case "Wire":
				spec.Wire = at
			case "Model":
				spec.Model = at
			case "SDK":
				spec.SDK = at
			}
		}
		if spec.Wire >= 0 && spec.Model >= 0 && spec.SDK >= 0 {
			out[name] = spec
		}
	}
	ast.Inspect(file, func(n ast.Node) bool {
		switch decl := n.(type) {
		case *ast.FuncDecl:
			consider(decl.Name.Name, decl.Type.Params, decl.Body)
		case *ast.AssignStmt:
			if len(decl.Lhs) != 1 || len(decl.Rhs) != 1 {
				return true
			}
			name, ok := decl.Lhs[0].(*ast.Ident)
			if !ok {
				return true
			}
			if fn, ok := decl.Rhs[0].(*ast.FuncLit); ok {
				consider(name.Name, fn.Type.Params, fn.Body)
			}
		}
		return true
	})
	return out
}

// aliasesIn resolves `type ppSDK = ui.PortProfile`, which two descriptors use to
// keep their generic instantiations readable. The target is recorded
// qualified (qualifiedExprName, not exprName) so resolveAlias hands back the
// same "pkg.Type" form a direct, unaliased reference would have produced.
func aliasesIn(file *ast.File) map[string]string {
	out := map[string]string{}
	ast.Inspect(file, func(n ast.Node) bool {
		spec, ok := n.(*ast.TypeSpec)
		if ok && spec.Assign.IsValid() {
			out[spec.Name.Name] = qualifiedExprName(spec.Type)
		}
		return true
	})
	return out
}

func resolveAlias(aliases map[string]string, name string) string {
	for range 8 {
		next, ok := aliases[name]
		if !ok {
			return name
		}
		name = next
	}
	return name
}

func parseField(t *testing.T, path string, el ast.Expr, helpers map[string]helperSpec) descriptorField {
	t.Helper()
	wrapper := ""
	for {
		call, ok := el.(*ast.CallExpr)
		if !ok {
			break
		}
		name := exprName(call.Fun)
		if spec, ok := helpers[name]; ok {
			return fieldFromHelper(t, path, name, spec, call, wrapper)
		}
		if len(call.Args) != 1 {
			t.Fatalf("%s: %s(...) takes %d arguments and is not a recognised field "+
				"constructor -- its body must return a resourcekit field literal whose Wire, "+
				"Model and SDK are its own parameters. Guessing the positions would "+
				"mis-attribute every field it builds", path, name, len(call.Args))
		}
		wrapper = name
		el = call.Args[0]
	}
	lit, ok := el.(*ast.CompositeLit)
	if !ok {
		t.Fatalf("%s: a Fields entry is %T, which this reader does not understand; it must fail "+
			"rather than skip, because a skipped entry reads as a missing field", path, el)
	}
	field := descriptorField{Kind: exprName(lit.Type), Wrapper: wrapper}
	for _, item := range lit.Elts {
		kv, ok := item.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		key, _ := kv.Key.(*ast.Ident)
		if key == nil {
			continue
		}
		switch key.Name {
		case "Wire":
			bl, ok := kv.Value.(*ast.BasicLit)
			if !ok {
				t.Fatalf("%s: a Wire value is %T, not a string literal", path, kv.Value)
			}
			field.Wire, _ = strconv.Unquote(bl.Value)
		case "Wires":
			slice, ok := kv.Value.(*ast.CompositeLit)
			if !ok {
				t.Fatalf("%s: a Wires value is %T, not a composite literal", path, kv.Value)
			}
			for _, item := range slice.Elts {
				bl, ok := item.(*ast.BasicLit)
				if !ok {
					t.Fatalf("%s: a Wires entry is %T, not a string literal; skipping it "+
						"would let a scattered field's dropped wire read as accounted for",
						path, item)
				}
				wire, _ := strconv.Unquote(bl.Value)
				field.Wires = append(field.Wires, wire)
			}
		case "Model":
			field.Model = returnedSelector(kv.Value)
		case "SDK":
			field.SDK = returnedSelector(kv.Value)
		}
	}
	if field.Wire == "" && len(field.Wires) == 0 {
		t.Fatalf("%s: a %s entry names no wire at all", path, field.Kind)
	}
	return field
}

// fieldFromHelper reads a call to a per-file constructor using the argument
// positions derived from that constructor's own signature.
func fieldFromHelper(
	t *testing.T, path, name string, spec helperSpec, call *ast.CallExpr, wrapper string,
) descriptorField {
	t.Helper()
	if spec.Wire >= len(call.Args) || spec.Model >= len(call.Args) || spec.SDK >= len(call.Args) {
		t.Fatalf("%s: %s takes arguments this call does not supply", path, name)
	}
	field := descriptorField{Kind: spec.Kind, Wrapper: wrapper}
	bl, ok := call.Args[spec.Wire].(*ast.BasicLit)
	if !ok {
		t.Fatalf("%s: %s's wire argument is %T, not a string literal", path, name, call.Args[spec.Wire])
	}
	field.Wire, _ = strconv.Unquote(bl.Value)
	field.Model = returnedSelector(call.Args[spec.Model])
	field.SDK = returnedSelector(call.Args[spec.SDK])
	return field
}

// returnedSelector reads the struct field a `func(m *M) *T { return &m.X }`
// closure names.
func returnedSelector(e ast.Expr) string {
	fn, ok := e.(*ast.FuncLit)
	if !ok || fn.Body == nil {
		return ""
	}
	var out string
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		if sel, ok := n.(*ast.SelectorExpr); ok && out == "" {
			out = sel.Sel.Name
		}
		return out == ""
	})
	return out
}

func exprName(e ast.Expr) string {
	switch t := e.(type) {
	case *ast.IndexListExpr:
		return exprName(t.X)
	case *ast.IndexExpr:
		return exprName(t.X)
	case *ast.SelectorExpr:
		return t.Sel.Name
	case *ast.Ident:
		return t.Name
	case *ast.StarExpr:
		return exprName(t.X)
	}
	return ""
}

// sdkImportAliases maps a descriptor file's own import identifier for
// go-unifi's root package to that package's real name, the one
// sdkshape.Load's qualified lookup is keyed on. Descriptor files import it
// as `ui "github.com/ubiquiti-community/go-unifi/unifi"`, but the package
// itself declares `package unifi`; settings needs no entry because
// descriptor files import it unaliased, so the identifier already matches.
var sdkImportAliases = map[string]string{"ui": "unifi"}

// qualifiedExprName is exprName's SDK-type-argument counterpart: it keeps a
// SelectorExpr's package qualifier instead of dropping it, translated
// through sdkImportAliases to the real package name. A Spec's SDK type
// argument needs this because go-unifi's root and settings packages both
// declare Dashboard, Setting and FieldConstraint -- bare "Dashboard" cannot
// say which one a descriptor means, and sdkshape.Package.Members resolves
// the ambiguity only when it is told (see that package's own comment). Every
// other use of exprName in this file (detecting the Spec composite literal,
// resolving helper-function calls, matching model struct names) wants the
// bare name and is untouched.
func qualifiedExprName(e ast.Expr) string {
	switch t := e.(type) {
	case *ast.IndexListExpr:
		return qualifiedExprName(t.X)
	case *ast.IndexExpr:
		return qualifiedExprName(t.X)
	case *ast.SelectorExpr:
		pkg, ok := t.X.(*ast.Ident)
		if !ok {
			return t.Sel.Name
		}
		name := pkg.Name
		if actual, known := sdkImportAliases[name]; known {
			name = actual
		}
		return name + "." + t.Sel.Name
	case *ast.Ident:
		return t.Name
	case *ast.StarExpr:
		return qualifiedExprName(t.X)
	}
	return ""
}

// kindCarriesAPointer reports whether a field kind's SDK accessor addresses a
// pointer, which is what decides whether it can express "absent" as distinct
// from "the zero value". ObjectField is a special case: its accessor is
// func(*S) **E, pointer-carrying by construction despite the name having no
// "Ptr" in it. ObjectListField needs no such case: its accessor is
// func(*S) *[]E over a slice member, so the JSON member isn't a pointer.
func kindCarriesAPointer(kind string) bool {
	if kind == "ObjectField" {
		return true
	}
	return strings.Contains(kind, "Ptr")
}

// claimedStructuralNames is every SDK attribute a claim in the surface's
// policy names -- a claim is the policy's word for a wire that isn't
// one-to-one with a schema attribute, which the mapping renders with an
// empty disposition rather than "managed".
func claimedStructuralNames(t *testing.T, surface string) map[string]bool {
	t.Helper()
	path := filepath.Join("..", "provider-codegen", "policy", surface+".json")
	raw, err := os.ReadFile(path)
	if err != nil {
		// A surface with no policy file has no claims. The policy's existence
		// is asserted by TestEveryKitSurfaceHasAPolicyEntry, not here.
		return nil
	}
	var policy struct {
		Claims []struct {
			StructuralNames []string `json:"structural_names"`
		} `json:"claims"`
	}
	if err := json.Unmarshal(raw, &policy); err != nil {
		t.Fatalf("parsing %s: %v", path, err)
	}
	out := map[string]bool{}
	for _, claim := range policy.Claims {
		for _, structural := range claim.StructuralNames {
			out[structural] = true
		}
	}
	return out
}
