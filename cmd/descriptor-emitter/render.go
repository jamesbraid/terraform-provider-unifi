package main

import (
	"bytes"
	"context"
	"fmt"
	"go/format"
	"reflect"
	"sort"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/sdkshape"
)

// constraintPatterns projects a constraint table to wire -> pattern, the
// one column OmitZero derivation reads; the projection lets the SDK's root
// and settings tables share deriveOmitZero despite their distinct
// FieldConstraint struct types.
func constraintPatterns[C any](table map[string]C, pattern func(C) string) map[string]string {
	out := make(map[string]string, len(table))
	for wire, constraint := range table {
		out[wire] = pattern(constraint)
	}
	return out
}

const basetypesPackage = "github.com/hashicorp/terraform-plugin-framework/types/basetypes"

// modelMember is one field of the emitted model struct.
type modelMember struct {
	Name string
	Type string
	Tag  string
}

// modelTypeFor renders the model member type an attribute wants: the plain
// types.X for a framework value, the custom type itself (with its import)
// for anything else.
func modelTypeFor(ctx context.Context, t attr.Type, imports map[string]string) string {
	rt := reflect.TypeOf(t.ValueType(ctx))
	if rt.PkgPath() == basetypesPackage {
		return "types." + strings.TrimSuffix(rt.Name(), "Value")
	}
	alias := rt.PkgPath()[strings.LastIndex(rt.PkgPath(), "/")+1:]
	imports[rt.PkgPath()] = alias
	return alias + "." + rt.Name()
}

// deriveModel lists the model struct's members: id and site first where the
// schema serves them, then the mapping's managed attributes in artifact
// order, then whatever else the schema declares (sorted), then blocks
// (sorted), then timeouts on a managed resource.
func deriveModel(
	ctx context.Context, s surface, built schema.Schema, doc *mapping, imports map[string]string,
) []modelMember {
	var out []modelMember
	seen := map[string]bool{}
	add := func(name string) {
		if seen[name] {
			return
		}
		attribute, ok := built.Attributes[name]
		if !ok {
			return
		}
		seen[name] = true
		out = append(out, modelMember{
			Name: camel(name),
			Type: modelTypeFor(ctx, attribute.GetType(), imports),
			Tag:  name,
		})
	}
	add("id")
	add("site")
	for _, f := range doc.Fields {
		if f.Disposition == "managed" && f.TerraformName != "" {
			add(f.TerraformName)
		}
	}
	rest := make([]string, 0, len(built.Attributes))
	for name := range built.Attributes {
		if !seen[name] {
			rest = append(rest, name)
		}
	}
	sort.Strings(rest)
	for _, name := range rest {
		add(name)
	}
	blocks := make([]string, 0, len(built.Blocks))
	for name := range built.Blocks {
		blocks = append(blocks, name)
	}
	sort.Strings(blocks)
	for _, name := range blocks {
		out = append(out, modelMember{
			Name: camel(name),
			Type: modelTypeFor(ctx, built.Blocks[name].Type(), imports),
			Tag:  name,
		})
	}
	if s.section == "" {
		imports["github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"] = "timeouts"
		out = append(out, modelMember{Name: "Timeouts", Type: "timeouts.Value", Tag: "timeouts"})
	}
	return out
}

func render(
	ctx context.Context, s surface, doc *mapping, sdk *sdkshape.Package, hand handFacts,
) ([]byte, error) {
	structName := doc.SDKStruct
	patterns := constraintPatterns(ui.FieldConstraints[structName],
		func(c ui.FieldConstraint) string { return c.Pattern })
	if s.section != "" {
		var err error
		if structName, err = sectionStruct(s.section, sdk); err != nil {
			return nil, err
		}
		patterns = constraintPatterns(settings.FieldConstraints["Setting"+structName],
			func(c settings.FieldConstraint) string { return c.Pattern })
	}
	sdkType, sdkLookup := "ui."+structName, structName
	if s.section != "" {
		sdkType, sdkLookup = "settings."+structName, "settings."+structName
	}
	members, ok := sdk.Members(sdkLookup)
	if !ok {
		return nil, fmt.Errorf("the SDK has no struct %s", sdkLookup)
	}
	built := s.built(ctx)
	fields, err := deriveFields(built, doc, members, patterns, hand)
	if err != nil {
		return nil, err
	}

	imports := map[string]string{
		"github.com/hashicorp/terraform-plugin-framework/types":                       "types",
		"github.com/ubiquiti-community/terraform-provider-unifi/internal/resourcekit": "resourcekit",
	}
	if s.section != "" {
		imports[goUnifiSettingsPackage] = "settings"
	} else {
		imports[goUnifiPackage] = "ui"
	}
	var model []modelMember
	if !s.handModel {
		model = deriveModel(ctx, s, built, doc, imports)
	}
	modelName := lowerCamelNaive(s.name) + "KitModel"
	if s.section != "" {
		modelName = lowerCamelNaive(s.name) + "Model"
	}

	var b bytes.Buffer
	if !s.handModel {
		fmt.Fprintf(&b, "type %s struct {\n", modelName)
		for _, m := range model {
			fmt.Fprintf(&b, "\t%s %s `tfsdk:%q`\n", m.Name, m.Type, m.Tag)
		}
		fmt.Fprintf(&b, "}\n\n")
	}

	if !s.handNested {
		renderNested(ctx, &b, s, built, doc, hand, imports)
	}

	fmt.Fprintf(&b, "// %s is every %s attribute whose mapping the pipeline's\n", lowerCamelNaive(s.name)+"GenFields", surfaceLabel(s))
	fmt.Fprintf(&b, "// artifacts fully determine. The hand descriptor lays its judgment fields\n")
	fmt.Fprintf(&b, "// over these with resourcekit.Override.\n")
	fmt.Fprintf(&b, "func %sGenFields() []resourcekit.Field[%s, %s] {\n", lowerCamelNaive(s.name), modelName, sdkType)
	fmt.Fprintf(&b, "\treturn []resourcekit.Field[%s, %s]{\n", modelName, sdkType)
	for _, f := range fields {
		fmt.Fprintf(&b, "\t\tresourcekit.%s[%s, %s]{\n", f.Kind, modelName, sdkType)
		fmt.Fprintf(&b, "\t\t\tWire:  %q,\n", f.Wire)
		fmt.Fprintf(&b, "\t\t\tModel: func(m *%s) %s { return &m.%s },\n", modelName, modelValueType[f.Kind], f.Model)
		fmt.Fprintf(&b, "\t\t\tSDK:   func(s *%s) %s { return &s.%s },\n", sdkType, sdkValueType[f.Kind], f.SDK)
		if f.Elide != "" {
			fmt.Fprintf(&b, "\t\t\tElide: resourcekit.%s,\n", f.Elide)
		}
		if f.OmitZero {
			fmt.Fprintf(&b, "\t\t\tOmitZero: true,\n")
		}
		fmt.Fprintf(&b, "\t\t},\n")
	}
	fmt.Fprintf(&b, "\t}\n}\n")

	if s.mirror {
		if err := renderMirrorSection(&b, s, modelName, sdkType, imports); err != nil {
			return nil, err
		}
	}
	if s.crud {
		if err := renderCRUD(&b, s, doc, sdk, modelName, sdkType, imports); err != nil {
			return nil, err
		}
	}

	// The import block is rendered last, once the body has declared every
	// package it needs, then spliced ahead of the body.
	var file bytes.Buffer
	fmt.Fprintf(&file, "// Code generated by cmd/descriptor-emitter. DO NOT EDIT.\n\npackage unifi\n\n")
	renderImports(&file, imports)
	b.WriteTo(&file) //nolint:errcheck // bytes.Buffer writes cannot fail

	source, err := format.Source(file.Bytes())
	if err != nil {
		return nil, fmt.Errorf("formatting: %w\n%s", err, file.String())
	}
	return source, nil
}

func surfaceLabel(s surface) string {
	if s.section != "" {
		return "unifi_setting " + s.section
	}
	return "unifi_" + s.name
}

// renderImports writes the file's import block in the repo's gci grouping:
// standard library (none here), then everything else alphabetically.
func renderImports(b *bytes.Buffer, imports map[string]string) {
	paths := make([]string, 0, len(imports))
	for path := range imports {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	fmt.Fprintf(b, "import (\n")
	for _, path := range paths {
		alias := imports[path]
		if alias == path[strings.LastIndex(path, "/")+1:] {
			fmt.Fprintf(b, "\t%q\n", path)
			continue
		}
		fmt.Fprintf(b, "\t%s %q\n", alias, path)
	}
	fmt.Fprintf(b, ")\n\n")
}
