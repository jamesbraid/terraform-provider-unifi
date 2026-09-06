package unifi

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
	resource_site "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_site"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/resourcekit"
)

// siteKitModel has no site attribute: sites are the things a site scopes, so
// the schema carries none. The unexported field below fills the kit's site
// slot -- Spec.Site is required by every whole resource -- and the resolved
// value parks there without ever reaching a schema attribute or the wire (the
// framework's reflection skips unexported fields).
type siteKitModel struct {
	ID          types.String   `tfsdk:"id"`
	Name        types.String   `tfsdk:"name"`
	Description types.String   `tfsdk:"description"`
	Timeouts    timeouts.Value `tfsdk:"timeouts"`

	site types.String
}

func siteKitSpec() resourcekit.Spec[siteKitModel, ui.Site] {
	return resourcekit.Spec[siteKitModel, ui.Site]{
		TypeName: "site",
		Subject:  "Site",
		IDWire:   "_id",
		New:      func() *ui.Site { return &ui.Site{} },
		ID:       func(m *siteKitModel) *types.String { return &m.ID },
		Site:     func(m *siteKitModel) *types.String { return &m.site },
		Timeouts: func(m *siteKitModel) *timeouts.Value { return &m.Timeouts },
		// Name is what opts the surface into import by a human handle:
		// "name=<site-name>" or any handle that isn't a 24-hex id lands here,
		// and the post-import read resolves it through Backend.ReadByName.
		Name: func(m *siteKitModel) *types.String { return &m.Name },
		Fields: []resourcekit.Field[siteKitModel, ui.Site]{
			resourcekit.StringField[siteKitModel, ui.Site]{
				Wire:  "desc",
				Model: func(m *siteKitModel) *types.String { return &m.Description },
				SDK:   func(s *ui.Site) *string { return &s.Description },
				Elide: resourcekit.KeepZero,
			},
			// The controller derives the name (the URL slug) at creation and
			// it never changes, so the resource only reads it. The write side
			// still needs it -- update-site is addressed by name -- which is
			// what the BeforeSend hook below carries.
			resourcekit.ReadOnly[siteKitModel, ui.Site](
				resourcekit.StringField[siteKitModel, ui.Site]{
					Wire:  "name",
					Model: func(m *siteKitModel) *types.String { return &m.Name },
					SDK:   func(s *ui.Site) *string { return &s.Name },
					Elide: resourcekit.KeepZero,
				}),
		},
		// BeforeSend puts the state's name on the object because
		// UpdateSiteFields addresses the update-site command by it; the mask
		// itself stays ["desc"], the one field that command writes.
		BeforeSend: func(
			_ context.Context,
			_, effective *siteKitModel,
			_ siteKitModel,
			sdk *ui.Site,
			_ any,
		) diag.Diagnostics {
			if !effective.Name.IsNull() && !effective.Name.IsUnknown() {
				sdk.Name = effective.Name.ValueString()
			}
			return nil
		},
		// Seeded here as well as in siteKitBackend, because Configure binds
		// the real Backend and a unit test calling ToModel on an unconfigured
		// spec would otherwise dereference nil.
		Backend: resourcekit.Backend[ui.Site]{
			GetID: func(s *ui.Site) string { return s.ID },
			SetID: func(s *ui.Site, id string) { s.ID = id },
		},
	}
}

func siteKitSchema() resourcekit.SchemaSpec {
	return resourcekit.SchemaSpec{
		Resource: resource_site.SiteResourceSchema,
		Timeouts: timeouts.Opts{Create: true, Read: true, Update: true, Delete: true},
	}
}

// siteKitBackend adapts the SDK's global site calls to the kit's site-scoped
// closure shapes. Every closure ignores the site argument: sites are not
// scoped by a site.
func siteKitBackend(client *ui.ApiClient) resourcekit.Backend[ui.Site] {
	return resourcekit.Backend[ui.Site]{
		// add-site takes only the description and answers with a list; the
		// hand-written resource trusted its first element to be the created
		// site, and this keeps that reading.
		Create: func(ctx context.Context, _ string, in *ui.Site) (*ui.Site, error) {
			sites, err := client.CreateSite(ctx, in.Description)
			if err != nil {
				return nil, err
			}
			if len(sites) == 0 {
				return nil, fmt.Errorf("no site returned from CreateSite call")
			}
			// The hand resource's guard, kept where it can still fire: an
			// element with neither id nor name is not a site to record.
			if sites[0].ID == "" && sites[0].Name == "" {
				return nil, fmt.Errorf("CreateSite returned a site with no ID or name")
			}
			return &sites[0], nil
		},
		Read: func(ctx context.Context, _, id string) (*ui.Site, error) {
			return client.GetSite(ctx, id)
		},
		ReadByName: func(ctx context.Context, _, name string) (*ui.Site, error) {
			return client.GetSiteByName(ctx, name)
		},
		UpdateFields: func(
			ctx context.Context, _ string, in *ui.Site, fields ...string,
		) (*ui.Site, error) {
			return client.UpdateSiteFields(ctx, in, fields...)
		},
		Delete: func(ctx context.Context, _, id string) error {
			_, err := client.DeleteSite(ctx, id)
			return err
		},
		List: func(ctx context.Context, _ string) ([]ui.Site, error) {
			return client.ListSites(ctx)
		},
		GetID: func(s *ui.Site) string { return s.ID },
		SetID: func(s *ui.Site, id string) { s.ID = id },
	}
}
