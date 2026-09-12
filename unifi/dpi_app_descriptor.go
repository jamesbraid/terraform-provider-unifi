package unifi

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/types"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/listresource_dpi_app"
	resource_dpi_app "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_dpi_app"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/resourcekit"
)

// dpiAppKitSpec: a DPI application rule matches DPI apps or whole categories
// and blocks or logs them. The three bools are non-omitempty on the wire, so
// their schema defaults keep the mask always naming them; the two qos rate
// caps carry a controller pattern that rejects a literal 0, so their fields
// omit a zero rather than let one reach the wire. The four attr_* controller
// internals stay unmapped.
func dpiAppKitSpec() resourcekit.Spec[dpiAppKitModel, ui.DpiApp] {
	return resourcekit.Spec[dpiAppKitModel, ui.DpiApp]{
		TypeName: "dpi_app",
		Subject:  "DPI Application",
		IDWire:   "_id",
		New:      func() *ui.DpiApp { return &ui.DpiApp{} },
		ID:       func(m *dpiAppKitModel) *types.String { return &m.ID },
		Site:     func(m *dpiAppKitModel) *types.String { return &m.Site },
		Timeouts: func(m *dpiAppKitModel) *timeouts.Value { return &m.Timeouts },
		Fields:   dpiAppGenFields(),
		// Seeded here as well as in dpiAppKitBackend, because Configure binds
		// the real Backend and a unit test calling ToModel on an unconfigured
		// spec would otherwise dereference nil.
		Backend: resourcekit.Backend[ui.DpiApp]{
			GetID: func(s *ui.DpiApp) string { return s.ID },
			SetID: func(s *ui.DpiApp, id string) { s.ID = id },
		},
	}
}

func dpiAppKitSchema() resourcekit.SchemaSpec {
	return resourcekit.SchemaSpec{
		Resource: resource_dpi_app.DpiAppResourceSchema,
		Timeouts: timeouts.Opts{Create: true, Read: true, Update: true, Delete: true},
	}
}

func dpiAppKitList() resourcekit.ListSpec[ui.DpiApp] {
	return resourcekit.ListSpec[ui.DpiApp]{
		ConfigSchema: listresource_dpi_app.DpiAppListResourceSchema,
		DisplayName: func(s *ui.DpiApp) string {
			if s.Name != "" {
				return s.Name
			}
			return s.ID
		},
		Filters: map[string]func(*ui.DpiApp) string{
			"name": func(s *ui.DpiApp) string { return s.Name },
		},
	}
}

func dpiAppKitBackend(client *ui.ApiClient) resourcekit.Backend[ui.DpiApp] {
	return resourcekit.Backend[ui.DpiApp]{
		Create: func(ctx context.Context, site string, in *ui.DpiApp) (*ui.DpiApp, error) {
			return client.CreateDpiApp(ctx, site, in)
		},
		Read: func(ctx context.Context, site, id string) (*ui.DpiApp, error) {
			return client.GetDpiApp(ctx, site, id)
		},
		UpdateFields: func(
			ctx context.Context, site string, in *ui.DpiApp, fields ...string,
		) (*ui.DpiApp, error) {
			return client.UpdateDpiAppFields(ctx, site, in, fields...)
		},
		Delete: func(ctx context.Context, site, id string) error {
			return client.DeleteDpiApp(ctx, site, id)
		},
		List: func(ctx context.Context, site string) ([]ui.DpiApp, error) {
			return client.ListDpiApp(ctx, site)
		},
		GetID: func(s *ui.DpiApp) string { return s.ID },
		SetID: func(s *ui.DpiApp, id string) { s.ID = id },
	}
}
