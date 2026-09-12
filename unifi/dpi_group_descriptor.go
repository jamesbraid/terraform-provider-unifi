package unifi

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/types"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/listresource_dpi_group"
	resource_dpi_group "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_dpi_group"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/resourcekit"
)

// dpiGroupKitSpec: a DPI group gathers DPI application rules under one name so
// the controller can apply them together. enabled is non-omitempty on the
// wire and takes a schema default; dpiapp_ids is an optional id list; the four
// attr_* controller internals stay unmapped.
func dpiGroupKitSpec() resourcekit.Spec[dpiGroupKitModel, ui.DpiGroup] {
	return resourcekit.Spec[dpiGroupKitModel, ui.DpiGroup]{
		TypeName: "dpi_group",
		Subject:  "DPI Group",
		IDWire:   "_id",
		New:      func() *ui.DpiGroup { return &ui.DpiGroup{} },
		ID:       func(m *dpiGroupKitModel) *types.String { return &m.ID },
		Site:     func(m *dpiGroupKitModel) *types.String { return &m.Site },
		Timeouts: func(m *dpiGroupKitModel) *timeouts.Value { return &m.Timeouts },
		Fields:   dpiGroupGenFields(),
		// Seeded here as well as in dpiGroupKitBackend, because Configure binds
		// the real Backend and a unit test calling ToModel on an unconfigured
		// spec would otherwise dereference nil.
		Backend: resourcekit.Backend[ui.DpiGroup]{
			GetID: func(s *ui.DpiGroup) string { return s.ID },
			SetID: func(s *ui.DpiGroup, id string) { s.ID = id },
		},
	}
}

func dpiGroupKitSchema() resourcekit.SchemaSpec {
	return resourcekit.SchemaSpec{
		Resource: resource_dpi_group.DpiGroupResourceSchema,
		Timeouts: timeouts.Opts{Create: true, Read: true, Update: true, Delete: true},
	}
}

func dpiGroupKitList() resourcekit.ListSpec[ui.DpiGroup] {
	return resourcekit.ListSpec[ui.DpiGroup]{
		ConfigSchema: listresource_dpi_group.DpiGroupListResourceSchema,
		DisplayName: func(s *ui.DpiGroup) string {
			if s.Name != "" {
				return s.Name
			}
			return s.ID
		},
		Filters: map[string]func(*ui.DpiGroup) string{
			"name": func(s *ui.DpiGroup) string { return s.Name },
		},
	}
}

func dpiGroupKitBackend(client *ui.ApiClient) resourcekit.Backend[ui.DpiGroup] {
	return resourcekit.Backend[ui.DpiGroup]{
		Create: func(ctx context.Context, site string, in *ui.DpiGroup) (*ui.DpiGroup, error) {
			return client.CreateDpiGroup(ctx, site, in)
		},
		Read: func(ctx context.Context, site, id string) (*ui.DpiGroup, error) {
			return client.GetDpiGroup(ctx, site, id)
		},
		UpdateFields: func(
			ctx context.Context, site string, in *ui.DpiGroup, fields ...string,
		) (*ui.DpiGroup, error) {
			return client.UpdateDpiGroupFields(ctx, site, in, fields...)
		},
		Delete: func(ctx context.Context, site, id string) error {
			return client.DeleteDpiGroup(ctx, site, id)
		},
		List: func(ctx context.Context, site string) ([]ui.DpiGroup, error) {
			return client.ListDpiGroup(ctx, site)
		},
		GetID: func(s *ui.DpiGroup) string { return s.ID },
		SetID: func(s *ui.DpiGroup, id string) { s.ID = id },
	}
}
