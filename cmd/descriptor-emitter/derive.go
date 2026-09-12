package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/controllerregex"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/resourcekit"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/sdkshape"
)

// mappingField is one entry of a *.mapping.json.
type mappingField struct {
	StructuralName string `json:"structural_name"`
	TerraformName  string `json:"terraform_name"`
	StructuralType string `json:"structural_type"`
	TerraformType  string `json:"terraform_type"`
	Disposition    string `json:"disposition"`
}

type mapping struct {
	SurfaceKind string         `json:"surface_kind"`
	SDKStruct   string         `json:"sdk_struct"`
	Fields      []mappingField `json:"fields"`
}

func loadMapping(path string) (*mapping, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var doc mapping
	if err := json.Unmarshal(body, &doc); err != nil {
		return nil, fmt.Errorf("decoding %s: %w", path, err)
	}
	if len(doc.Fields) == 0 {
		return nil, fmt.Errorf("%s carries no fields", path)
	}
	return &doc, nil
}

// handFacts is what the emitter reads off the hand descriptor: the wires the
// Spec routes around its field list. A wire in either set must not surface
// as a generated field -- AlwaysWire is a hook-carried value (a write-only
// secret, typically) and MappedElsewhere belongs to a sibling document or a
// dedicated SDK method.
type handFacts struct {
	AlwaysWire      map[string]bool
	MappedElsewhere map[string]bool
	// Claimed is every wire a hand Fields entry names, read from Wire and
	// Wires values and from per-file constructor calls (firewall_rule's
	// str/boolean, port_profile's ppInt). A claimed wire is judgment by
	// definition -- the hand file kept it -- so the emitter never emits a
	// twin for it. This is also what keeps the emitter honest about SDK
	// fields with defined types (device's state is a ui.DeviceState, not an
	// int64): every such wire is hand-claimed, so no generated accessor
	// asserts the primitive type the struct does not have.
	Claimed map[string]bool
}

func scanHandDescriptor(path string) (handFacts, error) {
	facts := handFacts{
		AlwaysWire:      map[string]bool{},
		MappedElsewhere: map[string]bool{},
		Claimed:         map[string]bool{},
	}
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, 0)
	// A surface the emitter serves whole keeps no hand descriptor: an absent
	// file claims nothing, which is what the zero facts already say.
	if errors.Is(err, os.ErrNotExist) {
		return facts, nil
	}
	if err != nil {
		return facts, err
	}
	helperWireArg := helperWirePositions(file)
	var bad error
	ast.Inspect(file, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok {
			name := ""
			if id, ok := call.Fun.(*ast.Ident); ok {
				name = id.Name
			}
			if at, ok := helperWireArg[name]; ok && at < len(call.Args) {
				if bl, ok := call.Args[at].(*ast.BasicLit); ok {
					wire, _ := strconv.Unquote(bl.Value)
					facts.Claimed[wire] = true
				}
			}
			return true
		}
		kv, ok := n.(*ast.KeyValueExpr)
		if !ok {
			return true
		}
		key, ok := kv.Key.(*ast.Ident)
		if !ok {
			return true
		}
		switch key.Name {
		case "Wire":
			if bl, ok := kv.Value.(*ast.BasicLit); ok {
				wire, _ := strconv.Unquote(bl.Value)
				facts.Claimed[wire] = true
			}
			return true
		case "Wires":
			lit, ok := kv.Value.(*ast.CompositeLit)
			if !ok {
				return true
			}
			for _, item := range lit.Elts {
				if bl, ok := item.(*ast.BasicLit); ok {
					wire, _ := strconv.Unquote(bl.Value)
					facts.Claimed[wire] = true
				}
			}
			return true
		case "AlwaysWire", "MappedElsewhere":
		default:
			return true
		}
		into := facts.AlwaysWire
		if key.Name == "MappedElsewhere" {
			into = facts.MappedElsewhere
		}
		lit, ok := kv.Value.(*ast.CompositeLit)
		if !ok {
			bad = fmt.Errorf("%s: %s is %T, not a composite literal", path, key.Name, kv.Value)
			return false
		}
		for _, item := range lit.Elts {
			bl, ok := item.(*ast.BasicLit)
			if !ok {
				bad = fmt.Errorf("%s: a %s entry is %T, not a string literal", path, key.Name, item)
				return false
			}
			wire, _ := strconv.Unquote(bl.Value)
			into[wire] = true
		}
		return true
	})
	return facts, bad
}

// helperWirePositions finds the per-file field constructors -- functions or
// closures whose single-return body is a resourcekit field literal with its
// Wire forwarded from a parameter -- and reports which argument position
// carries the wire name.
func helperWirePositions(file *ast.File) map[string]int {
	out := map[string]int{}
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
		if sel, ok := lit.Type.(*ast.IndexListExpr); ok {
			_ = sel
		}
		index := map[string]int{}
		position := 0
		for _, group := range params.List {
			for _, ident := range group.Names {
				index[ident.Name] = position
				position++
			}
		}
		for _, el := range lit.Elts {
			kv, ok := el.(*ast.KeyValueExpr)
			if !ok {
				continue
			}
			key, _ := kv.Key.(*ast.Ident)
			value, _ := kv.Value.(*ast.Ident)
			if key == nil || value == nil || key.Name != "Wire" {
				continue
			}
			if at, ok := index[value.Name]; ok {
				out[name] = at
			}
		}
	}
	ast.Inspect(file, func(n ast.Node) bool {
		switch decl := n.(type) {
		case *ast.FuncDecl:
			consider(decl.Name.Name, decl.Type.Params, decl.Body)
		case *ast.AssignStmt:
			if len(decl.Lhs) == 1 && len(decl.Rhs) == 1 {
				if name, ok := decl.Lhs[0].(*ast.Ident); ok {
					if fn, ok := decl.Rhs[0].(*ast.FuncLit); ok {
						consider(name.Name, fn.Type.Params, fn.Body)
					}
				}
			}
		}
		return true
	})
	return out
}

// genField is one derivable Fields entry.
type genField struct {
	Kind     string // StringField, BoolField, ...
	Wire     string
	Model    string // model struct member
	SDK      string // SDK struct member
	Elide    string // "KeepZero", "NullZero", or "" for kinds without one
	OmitZero bool
}

// sdkValueType is the SDK accessor's pointee per kind; the compiler holds
// the emitted closure to it, so a struct whose field is anything else fails
// the build instead of masking a type change.
var sdkValueType = map[string]string{
	"StringField":     "*string",
	"BoolField":       "*bool",
	"BoolPtrField":    "**bool",
	"Int64Field":      "*int64",
	"Int64PtrField":   "**int64",
	"StringListField": "*[]string",
	"StringSetField":  "*[]string",
	"Int64ListField":  "*[]int64",
}

// modelValueType is the model accessor's pointee per kind.
var modelValueType = map[string]string{
	"StringField":     "*types.String",
	"BoolField":       "*types.Bool",
	"BoolPtrField":    "*types.Bool",
	"Int64Field":      "*types.Int64",
	"Int64PtrField":   "*types.Int64",
	"StringListField": "*types.List",
	"StringSetField":  "*types.Set",
	"Int64ListField":  "*types.List",
}

// deriveKind names the field kind the mapping's type pair and the SDK
// field's pointer-ness imply, or "" where the pair needs judgment (custom
// types, durations, nested objects) and the hand descriptor owns the wire.
func deriveKind(f mappingField, pointer bool) string {
	switch {
	case f.StructuralType == "string" && f.TerraformType == "string" && !pointer:
		return "StringField"
	case f.StructuralType == "bool" && f.TerraformType == "bool" && !pointer:
		return "BoolField"
	case f.StructuralType == "bool" && f.TerraformType == "bool" && pointer:
		return "BoolPtrField"
	case f.StructuralType == "int64" && f.TerraformType == "int64" && !pointer:
		return "Int64Field"
	case f.StructuralType == "int64" && f.TerraformType == "int64" && pointer:
		return "Int64PtrField"
	case f.StructuralType == "array<string>" && f.TerraformType == "list" && !pointer:
		return "StringListField"
	case f.StructuralType == "array<string>" && f.TerraformType == "set" && !pointer:
		return "StringSetField"
	case f.StructuralType == "array<int64>" && f.TerraformType == "list" && !pointer:
		return "Int64ListField"
	}
	return ""
}

// deriveFields walks the mapping's managed fields in artifact order and
// keeps every one the artifacts fully determine.
func deriveFields(
	built schema.Schema,
	doc *mapping,
	members map[string]sdkshape.Member,
	patterns map[string]string,
	hand handFacts,
) ([]genField, error) {
	var out []genField
	for _, f := range doc.Fields {
		if f.Disposition != "managed" || f.StructuralName == "" || f.StructuralName == "_id" {
			continue
		}
		if f.TerraformName == "id" || f.TerraformName == "site" {
			continue
		}
		if hand.AlwaysWire[f.StructuralName] || hand.MappedElsewhere[f.StructuralName] ||
			hand.Claimed[f.StructuralName] {
			continue
		}
		member, ok := members[f.StructuralName]
		if !ok {
			continue // carried by a sibling struct; the hand Spec owns it
		}
		attribute, ok := built.Attributes[f.TerraformName]
		if !ok {
			continue // a block, or invented; judgment either way
		}
		if writeOnly, ok := attribute.(interface{ IsWriteOnly() bool }); ok && writeOnly.IsWriteOnly() {
			continue
		}
		if !plainTyped(attribute) {
			continue // a custom type wants a StringLike kind and its constructor
		}
		kind := deriveKind(f, member.Pointer)
		if kind == "" {
			continue
		}
		field := genField{
			Kind:  kind,
			Wire:  f.StructuralName,
			Model: camel(f.TerraformName),
			SDK:   member.GoName,
		}
		switch kind {
		case "BoolField", "BoolPtrField":
			// No elision claim; a false is a value. See the kinds' own doc.
		default:
			elide, ok := resourcekit.DerivedElide(built, f.TerraformName, kind == "StringField")
			if !ok {
				return nil, fmt.Errorf("%s: schema declares no attribute", f.TerraformName)
			}
			field.Elide = "KeepZero"
			if elide == resourcekit.NullZero {
				field.Elide = "NullZero"
			}
		}
		if kind == "Int64PtrField" {
			omit, err := deriveOmitZero(patterns, f.StructuralName, attribute)
			if err != nil {
				return nil, err
			}
			field.OmitZero = omit
		}
		out = append(out, field)
	}
	return out, nil
}

// plainTyped reports whether the attribute's value is a plain framework
// type. A custom type (GoDuration, an iptypes address) needs a field kind
// carrying the type and its constructor, which is the hand descriptor's
// call.
func plainTyped(attribute schema.Attribute) bool {
	valueType := fmt.Sprintf("%T", attribute.GetType().ValueType(context.Background()))
	return strings.HasPrefix(valueType, "basetypes.")
}

// deriveOmitZero says whether an Int64PtrField must omit an unset value
// from the wire: the controller's own pattern rejects a literal zero, and
// the attribute is not Required (a Required attribute is never legitimately
// unset, so omission is the wrong tool for it -- the reasoning
// omit_zero_census_test.go's pins record for rule_index and distance).
func deriveOmitZero(
	patterns map[string]string, wire string, attribute schema.Attribute,
) (bool, error) {
	pattern := patterns[wire]
	if pattern == "" {
		return false, nil
	}
	re, err := regexp.Compile(controllerregex.Anchored(pattern))
	if err != nil {
		return false, fmt.Errorf("%s: constraint pattern %q does not compile: %w", wire, pattern, err)
	}
	return !re.MatchString("0") && !attribute.IsRequired(), nil
}

// sectionStruct names the settings struct a section descriptor's Spec is
// generic over. The settings package almost always spells it as the naive
// camel of the section name (Mdns, GuestAccess, RadioAi); where it does not
// (syslog's struct is Rsyslogd), setting.mapping.json's own qualified
// structural names carry the stem, and the unique stem the settings package
// declares is the answer.
func sectionStruct(section string, sdk *sdkshape.Package) (string, error) {
	name := camelNaive(section)
	if _, ok := sdk.Members("settings." + name); ok {
		return name, nil
	}
	stems, err := settingMappingStems(section)
	if err != nil {
		return "", err
	}
	var declared []string
	for _, stem := range stems {
		if _, ok := sdk.Members("settings." + stem); ok {
			declared = append(declared, stem)
		}
	}
	if len(declared) != 1 {
		return "", fmt.Errorf("section %s: cannot resolve its settings struct (candidates %v)", section, declared)
	}
	return declared[0], nil
}

// settingMappingStems reads the SDK struct stems setting.mapping.json's
// qualified structural names carry for one section.
func settingMappingStems(section string) ([]string, error) {
	doc, err := loadMapping(settingMappingPath)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var stems []string
	for _, f := range doc.Fields {
		structural, terraform := f.StructuralName, f.TerraformName
		si, ti := strings.IndexByte(structural, '.'), strings.IndexByte(terraform, '.')
		if si <= 0 || ti <= 0 || terraform[:ti] != section {
			continue
		}
		if stem := structural[:si]; !seen[stem] {
			seen[stem] = true
			stems = append(stems, stem)
		}
	}
	sort.Strings(stems)
	return stems, nil
}
