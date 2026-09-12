package main

import (
	"context"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
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
}

func scanHandDescriptor(path string) (handFacts, error) {
	facts := handFacts{AlwaysWire: map[string]bool{}, MappedElsewhere: map[string]bool{}}
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		return facts, err
	}
	var bad error
	ast.Inspect(file, func(n ast.Node) bool {
		kv, ok := n.(*ast.KeyValueExpr)
		if !ok {
			return true
		}
		key, ok := kv.Key.(*ast.Ident)
		if !ok || (key.Name != "AlwaysWire" && key.Name != "MappedElsewhere") {
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
	constraints map[string]ui.FieldConstraint,
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
		if hand.AlwaysWire[f.StructuralName] || hand.MappedElsewhere[f.StructuralName] {
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
			omit, err := deriveOmitZero(constraints, f.StructuralName, attribute)
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
	constraints map[string]ui.FieldConstraint, wire string, attribute schema.Attribute,
) (bool, error) {
	constraint, ok := constraints[wire]
	if !ok || constraint.Pattern == "" {
		return false, nil
	}
	re, err := regexp.Compile(controllerregex.Anchored(constraint.Pattern))
	if err != nil {
		return false, fmt.Errorf("%s: constraint pattern %q does not compile: %w", wire, constraint.Pattern, err)
	}
	return !re.MatchString("0") && !attribute.IsRequired(), nil
}
