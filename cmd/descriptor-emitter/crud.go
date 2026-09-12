package main

import (
	"bytes"
	"fmt"
	"text/template"

	"github.com/ubiquiti-community/terraform-provider-unifi/internal/sdkshape"
)

// crudTemplate is the whole descriptor a plain-CRUD resource needs: a Spec
// over the generated field list, the schema and timeouts wiring, a list
// filtered and displayed by name, and a Backend over the SDK's five
// site-scoped methods. A resource with a hook, a conditional write or a
// translated field is not plain CRUD and keeps its hand descriptor.
var crudTemplate = template.Must(template.New("crud").Parse(`
func {{.Stem}}KitSpec() resourcekit.Spec[{{.Model}}, {{.SDK}}] {
	return resourcekit.Spec[{{.Model}}, {{.SDK}}]{
		TypeName: "{{.Name}}",
		Subject:  "{{.Subject}}",
		IDWire:   "_id",
		New:      func() *{{.SDK}} { return &{{.SDK}}{} },
		ID:       func(m *{{.Model}}) *types.String { return &m.ID },
		Site:     func(m *{{.Model}}) *types.String { return &m.Site },
		Timeouts: func(m *{{.Model}}) *timeouts.Value { return &m.Timeouts },
		Fields:   {{.Stem}}GenFields(),
		// Seeded here as well as in {{.Stem}}KitBackend, because Configure
		// binds the real Backend and a unit test calling ToModel on an
		// unconfigured spec would otherwise dereference nil.
		Backend: resourcekit.Backend[{{.SDK}}]{
			GetID: func(s *{{.SDK}}) string { return s.ID },
			SetID: func(s *{{.SDK}}, id string) { s.ID = id },
		},
	}
}

func {{.Stem}}KitSchema() resourcekit.SchemaSpec {
	return resourcekit.SchemaSpec{
		Resource: {{.SchemaPackage}}.{{.Pascal}}ResourceSchema,
		Timeouts: timeouts.Opts{Create: true, Read: true, Update: true, Delete: true},
	}
}

func {{.Stem}}KitList() resourcekit.ListSpec[{{.SDK}}] {
	return resourcekit.ListSpec[{{.SDK}}]{
		ConfigSchema: {{.ListPackage}}.{{.Pascal}}ListResourceSchema,
		DisplayName: func(s *{{.SDK}}) string {
			if s.Name != "" {
				return s.Name
			}
			return s.ID
		},
		Filters: map[string]func(*{{.SDK}}) string{
			"name": func(s *{{.SDK}}) string { return s.Name },
		},
	}
}

func {{.Stem}}KitBackend(client *ui.ApiClient) resourcekit.Backend[{{.SDK}}] {
	return resourcekit.Backend[{{.SDK}}]{
		Create: func(ctx context.Context, site string, in *{{.SDK}}) (*{{.SDK}}, error) {
			return client.Create{{.Struct}}(ctx, site, in)
		},
		Read: func(ctx context.Context, site, id string) (*{{.SDK}}, error) {
			return client.Get{{.Struct}}(ctx, site, id)
		},
		UpdateFields: func(
			ctx context.Context, site string, in *{{.SDK}}, fields ...string,
		) (*{{.SDK}}, error) {
			return client.Update{{.Struct}}Fields(ctx, site, in, fields...)
		},
		Delete: func(ctx context.Context, site, id string) error {
			return client.Delete{{.Struct}}(ctx, site, id)
		},
		List: func(ctx context.Context, site string) ([]{{.SDK}}, error) {
			return client.List{{.Struct}}(ctx, site)
		},
		GetID: func(s *{{.SDK}}) string { return s.ID },
		SetID: func(s *{{.SDK}}, id string) { s.ID = id },
	}
}
`))

// renderCRUD writes the Spec/Schema/List/Backend quartet for a resource the
// artifacts determine whole. Two facts the template assumes are checked
// against the SDK rather than taken on faith: the client's Create and Get
// for this struct exist and operate on it, and the struct carries the name
// the list displays and filters by.
func renderCRUD(
	b *bytes.Buffer, s surface, doc *mapping, sdk *sdkshape.Package, model, sdkType string,
	imports map[string]string,
) error {
	for _, verb := range []string{"Create", "Get"} {
		if root, ok := sdk.RootFor([]string{verb + doc.SDKStruct}); !ok || root != doc.SDKStruct {
			return fmt.Errorf("the SDK has no %s%s operating on %s", verb, doc.SDKStruct, doc.SDKStruct)
		}
	}
	members, _ := sdk.Members(doc.SDKStruct)
	if _, ok := members["name"]; !ok {
		return fmt.Errorf("%s has no name member to list by", doc.SDKStruct)
	}

	pascal := camelNaive(s.name)
	imports["context"] = "context"
	imports[goUnifiPackage] = "ui"
	imports["github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"] = "timeouts"
	schemaPackage := "resource_" + s.name
	listPackage := "listresource_" + s.name
	imports["github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/"+schemaPackage] = schemaPackage
	imports["github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/"+listPackage] = listPackage

	return crudTemplate.Execute(b, struct {
		Stem, Name, Subject, Model, SDK, Struct, Pascal, SchemaPackage, ListPackage string
	}{
		Stem:          lowerCamelNaive(s.name),
		Name:          s.name,
		Subject:       s.subject,
		Model:         model,
		SDK:           sdkType,
		Struct:        doc.SDKStruct,
		Pascal:        pascal,
		SchemaPackage: schemaPackage,
		ListPackage:   listPackage,
	})
}
