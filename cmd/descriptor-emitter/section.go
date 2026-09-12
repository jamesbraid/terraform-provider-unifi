package main

import (
	"bytes"
	"text/template"
)

// mirrorSectionTemplate is the whole descriptor a plain-mirror settings
// section needs: a Spec over the generated field list, a Backend whose Read
// is the typed GetSetting and whose write is the masked UpdateSettingFields,
// and the Section binding the two to one settingResourceModel member. A
// section with a hook, a sibling document or a conditional write is not a
// mirror and keeps its hand descriptor.
var mirrorSectionTemplate = template.Must(template.New("mirror").Parse(`
// {{.Stem}}KitSpec is the {{.Section}} section's Spec: the generated field
// list over {{.SDK}}, with no hook and no conditional write.
func {{.Stem}}KitSpec() resourcekit.Spec[{{.Model}}, {{.SDK}}] {
	return resourcekit.Spec[{{.Model}}, {{.SDK}}]{
		TypeName: "{{.TypeName}}",
		Subject:  "{{.Subject}}",
		New:      func() *{{.SDK}} { return &{{.SDK}}{} },
		Fields:   {{.Fields}}(),
	}
}

// {{.Stem}}KitBackend binds {{.Stem}}KitSpec to a client: Read is
// GetSetting[*{{.SDK}}], UpdateFields is the masked UpdateSettingFields --
// naming only the fields the plan set.
func {{.Stem}}KitBackend(client *ui.ApiClient) resourcekit.Backend[{{.SDK}}] {
	return resourcekit.Backend[{{.SDK}}]{
		Read: func(ctx context.Context, site, _ string) (*{{.SDK}}, error) {
			_, doc, err := ui.GetSetting[*{{.SDK}}](client, ctx, site)
			if err != nil {
				return nil, err
			}
			return doc, nil
		},
		UpdateFields: func(
			ctx context.Context, site string, in *{{.SDK}}, fields ...string,
		) (*{{.SDK}}, error) {
			if err := client.UpdateSettingFields(ctx, site, in, fields...); err != nil {
				return nil, err
			}
			return in, nil
		},
	}
}

// {{.Stem}}KitSection builds the {{.Section}} entry for settingResource's
// Sections, bound to client via settingKitSections.
func {{.Stem}}KitSection(client *ui.ApiClient) resourcekit.Section[settingResourceModel] {
	spec := {{.Stem}}KitSpec()
	spec.Backend = {{.Stem}}KitBackend(client)
	return resourcekit.SpecSection[settingResourceModel, {{.Model}}, {{.SDK}}]{
		SectionName: "{{.Section}}",
		Get:         func(m *settingResourceModel) *types.Object { return &m.{{.Member}} },
		Set:         func(m *settingResourceModel, o types.Object) { m.{{.Member}} = o },
		AttrTypes:   {{.Stem}}AttrTypes,
		Spec:        spec,
	}
}
`))

// renderMirrorSection writes the Spec/Backend/Section trio for a section
// whose every varying token the pipeline already holds: the SDK struct from
// the mapping artifact, the model and attr-type map from the emitted half,
// the settingResourceModel member from the section name, and Subject from
// the registry -- the one string a schema cannot spell ("DoH", "NetFlow").
func renderMirrorSection(b *bytes.Buffer, s surface, model, sdkType string, imports map[string]string) error {
	imports["context"] = "context"
	imports[goUnifiPackage] = "ui"
	return mirrorSectionTemplate.Execute(b, struct {
		Stem, Section, TypeName, Subject, Model, SDK, Fields, Member string
	}{
		Stem:     lowerCamelNaive(s.section),
		Section:  s.section,
		TypeName: s.name,
		Subject:  s.subject,
		Model:    model,
		SDK:      sdkType,
		Fields:   lowerCamelNaive(s.name) + "GenFields",
		Member:   camelNaive(s.section),
	})
}
