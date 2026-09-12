package main

import (
	"bytes"
	"context"
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
)

// nestedDef is one object-valued attribute's element shape: the struct and
// attr-type map emitted for it, named by the path that reaches it.
type nestedDef struct {
	stem  string // mdnsCustomServices
	attrs map[string]schema.Attribute
	// attribute is the surface's own top-level attribute this def hangs
	// under, empty for anything deeper. Only a top-level def can be skipped:
	// a deeper one's attr-type map is named by its parent's.
	attribute string
}

// collectNested walks the attribute tree depth-first and returns every
// nested object element, children before parents, so an emitted attr-type
// map can reference its child's by name.
func collectNested(stem string, attrs map[string]schema.Attribute, top bool) []nestedDef {
	var out []nestedDef
	for _, name := range sortedNames(attrs) {
		members, ok := nestedAttrsOf(attrs[name])
		if !ok {
			continue
		}
		child := nestedDef{
			stem:  stem + camelNaive(name),
			attrs: members,
		}
		if top {
			child.attribute = name
		}
		out = append(out, collectNested(child.stem, child.attrs, false)...)
		out = append(out, child)
	}
	return out
}

// nestedAttrsOf returns an attribute's nested object members, through the
// concrete schema types: the interface route hands back the framework's
// internal fwschema maps, which cannot be named here.
func nestedAttrsOf(attribute schema.Attribute) (map[string]schema.Attribute, bool) {
	switch typed := attribute.(type) {
	case schema.ListNestedAttribute:
		return typed.NestedObject.Attributes, true
	case schema.SetNestedAttribute:
		return typed.NestedObject.Attributes, true
	case schema.MapNestedAttribute:
		return typed.NestedObject.Attributes, true
	case schema.SingleNestedAttribute:
		return typed.Attributes, true
	}
	return nil, false
}

func sortedNames(attrs map[string]schema.Attribute) []string {
	names := make([]string, 0, len(attrs))
	for name := range attrs {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// attrTypeExpr renders one attribute's attr.Type as source. childStems maps
// a nested attribute's name (within the map being rendered) to the emitted
// element stem its object type is declared under.
func attrTypeExpr(attribute schema.Attribute, childStem string, imports map[string]string) string {
	object := "types.ObjectType{AttrTypes: " + childStem + "AttrTypes}"
	switch attribute.(type) {
	case schema.ListNestedAttribute:
		return "types.ListType{ElemType: " + object + "}"
	case schema.SetNestedAttribute:
		return "types.SetType{ElemType: " + object + "}"
	case schema.MapNestedAttribute:
		return "types.MapType{ElemType: " + object + "}"
	case schema.SingleNestedAttribute:
		return object
	}
	return plainTypeExpr(attribute.GetType(), imports)
}

// plainTypeExpr renders a non-nested attr.Type: framework types by their
// types.X names, element types recursively, custom types by their own
// zero-value literal.
func plainTypeExpr(t attr.Type, imports map[string]string) string {
	switch typed := t.(type) {
	case basetypes.ListType:
		return "types.ListType{ElemType: " + plainTypeExpr(typed.ElemType, imports) + "}"
	case basetypes.SetType:
		return "types.SetType{ElemType: " + plainTypeExpr(typed.ElemType, imports) + "}"
	case basetypes.MapType:
		return "types.MapType{ElemType: " + plainTypeExpr(typed.ElemType, imports) + "}"
	case basetypes.ObjectType:
		members := typed.AttributeTypes()
		names := make([]string, 0, len(members))
		for name := range members {
			names = append(names, name)
		}
		sort.Strings(names)
		var b strings.Builder
		b.WriteString("types.ObjectType{AttrTypes: map[string]attr.Type{")
		for i, name := range names {
			if i > 0 {
				b.WriteString(", ")
			}
			fmt.Fprintf(&b, "%q: %s", name, plainTypeExpr(members[name], imports))
		}
		b.WriteString("}}")
		return b.String()
	}
	rt := reflect.TypeOf(t)
	if rt.PkgPath() == basetypesPackage {
		return "types." + rt.Name()
	}
	alias := rt.PkgPath()[strings.LastIndex(rt.PkgPath(), "/")+1:]
	imports[rt.PkgPath()] = alias
	return alias + "." + rt.Name() + "{}"
}

// supersededNested reports which of a def's two declarations the hand
// descriptor has already replaced, so the emitter does not write a second
// copy nothing can name.
//
// Only a top-level def is ever a candidate: a deeper one's attr-type map is
// named by its parent's, and on a section every top-level one is named by
// the section's own map. Mentions is the guard in both directions -- a name
// the hand file uses is emitted whatever else that file does.
func supersededNested(s surface, def nestedDef, doc *mapping, hand handFacts) (model, attrTypes bool) {
	if def.attribute == "" || s.section != "" {
		return false, false
	}
	if hand.AttributeTypesMethods[def.stem+"Model"] {
		attrTypes = !hand.Mentions[def.stem+"AttrTypes"]
	}
	// A hand entry that claims the wire owns the element shape end to end:
	// power_supervisor's power_sources carries its own AttrTypes and its own
	// Decode, and reaches for neither generated declaration.
	for _, f := range doc.Fields {
		if f.TerraformName != def.attribute || !hand.Claimed[f.StructuralName] {
			continue
		}
		model = !hand.Mentions[def.stem+"Model"]
		attrTypes = attrTypes || !hand.Mentions[def.stem+"AttrTypes"]
	}
	return model, attrTypes
}

// renderNested writes the element model structs and attr-type maps for
// every nested object under the surface, and -- for a section -- the
// section's own attr-type map and NestedSchema helper.
func renderNested(
	ctx context.Context, b *bytes.Buffer, s surface, built schema.Schema,
	doc *mapping, hand handFacts, imports map[string]string,
) {
	stem := lowerCamelNaive(s.name)
	if s.section != "" {
		stem = lowerCamelNaive(s.section)
	}
	defs := collectNested(stem, built.Attributes, true)
	for _, def := range defs {
		skipModel, skipAttrTypes := supersededNested(s, def, doc, hand)
		if !skipModel {
			fmt.Fprintf(b, "type %sModel struct {\n", def.stem)
			for _, name := range sortedNames(def.attrs) {
				fmt.Fprintf(b, "\t%s %s `tfsdk:%q`\n",
					camel(name), modelTypeFor(ctx, def.attrs[name].GetType(), imports), name)
			}
			fmt.Fprintf(b, "}\n\n")
		}
		if skipAttrTypes {
			continue
		}
		imports["github.com/hashicorp/terraform-plugin-framework/attr"] = "attr"
		fmt.Fprintf(b, "var %sAttrTypes = map[string]attr.Type{\n", def.stem)
		for _, name := range sortedNames(def.attrs) {
			fmt.Fprintf(b, "\t%q: %s,\n", name, attrTypeExpr(def.attrs[name], def.stem+camelNaive(name), imports))
		}
		fmt.Fprintf(b, "}\n\n")
	}
	if s.section == "" {
		return
	}
	imports["github.com/hashicorp/terraform-plugin-framework/attr"] = "attr"
	fmt.Fprintf(b, "var %sAttrTypes = map[string]attr.Type{\n", stem)
	for _, name := range sortedNames(built.Attributes) {
		fmt.Fprintf(b, "\t%q: %s,\n", name, attrTypeExpr(built.Attributes[name], stem+camelNaive(name), imports))
	}
	fmt.Fprintf(b, "}\n\n")

	imports["context"] = "context"
	imports["github.com/hashicorp/terraform-plugin-framework/resource/schema"] = "schema"
	imports["github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_setting"] = "resource_setting"
	fmt.Fprintf(b, "// %sNestedSchema is the %s SingleNestedAttribute's own Attributes,\n", stem, s.section)
	fmt.Fprintf(b, "// wrapped as a schema.Schema so resourcekit's conformance checks -- built\n")
	fmt.Fprintf(b, "// for a whole resource's top-level schema -- can run against one section\n")
	fmt.Fprintf(b, "// of unifi_setting.\n")
	fmt.Fprintf(b, "func %sNestedSchema(ctx context.Context) schema.Schema {\n", stem)
	fmt.Fprintf(b, "\tbuilt := resource_setting.SettingResourceSchema(ctx)\n")
	fmt.Fprintf(b, "\tsection := built.Attributes[%q].(schema.SingleNestedAttribute)\n", s.section)
	fmt.Fprintf(b, "\treturn schema.Schema{Attributes: section.Attributes}\n}\n\n")
}
