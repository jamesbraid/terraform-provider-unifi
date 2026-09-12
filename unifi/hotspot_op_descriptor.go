package unifi

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/types"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/listresource_hotspot_op"
	resource_hotspot_op "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_hotspot_op"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/resourcekit"
)

// hotspotOpKitSpec: a hotspot operator is a guest-portal login account. name
// and the password are required; note is optional. The controller calls the
// password x_password on the wire, which only the wire-name check would catch
// if this used the Terraform spelling. The four attr_* controller internals
// stay unmapped.
func hotspotOpKitSpec() resourcekit.Spec[hotspotOpKitModel, ui.HotspotOp] {
	return resourcekit.Spec[hotspotOpKitModel, ui.HotspotOp]{
		TypeName: "hotspot_op",
		Subject:  "Hotspot Operator",
		IDWire:   "_id",
		New:      func() *ui.HotspotOp { return &ui.HotspotOp{} },
		ID:       func(m *hotspotOpKitModel) *types.String { return &m.ID },
		Site:     func(m *hotspotOpKitModel) *types.String { return &m.Site },
		Timeouts: func(m *hotspotOpKitModel) *timeouts.Value { return &m.Timeouts },
		Fields:   hotspotOpGenFields(),
		// Seeded here as well as in hotspotOpKitBackend, because Configure binds
		// the real Backend and a unit test calling ToModel on an unconfigured
		// spec would otherwise dereference nil.
		Backend: resourcekit.Backend[ui.HotspotOp]{
			GetID: func(s *ui.HotspotOp) string { return s.ID },
			SetID: func(s *ui.HotspotOp, id string) { s.ID = id },
		},
	}
}

func hotspotOpKitSchema() resourcekit.SchemaSpec {
	return resourcekit.SchemaSpec{
		Resource: resource_hotspot_op.HotspotOpResourceSchema,
		Timeouts: timeouts.Opts{Create: true, Read: true, Update: true, Delete: true},
	}
}

func hotspotOpKitList() resourcekit.ListSpec[ui.HotspotOp] {
	return resourcekit.ListSpec[ui.HotspotOp]{
		ConfigSchema: listresource_hotspot_op.HotspotOpListResourceSchema,
		DisplayName: func(s *ui.HotspotOp) string {
			if s.Name != "" {
				return s.Name
			}
			return s.ID
		},
		Filters: map[string]func(*ui.HotspotOp) string{
			"name": func(s *ui.HotspotOp) string { return s.Name },
		},
	}
}

func hotspotOpKitBackend(client *ui.ApiClient) resourcekit.Backend[ui.HotspotOp] {
	return resourcekit.Backend[ui.HotspotOp]{
		Create: func(ctx context.Context, site string, in *ui.HotspotOp) (*ui.HotspotOp, error) {
			return client.CreateHotspotOp(ctx, site, in)
		},
		Read: func(ctx context.Context, site, id string) (*ui.HotspotOp, error) {
			return client.GetHotspotOp(ctx, site, id)
		},
		UpdateFields: func(
			ctx context.Context, site string, in *ui.HotspotOp, fields ...string,
		) (*ui.HotspotOp, error) {
			return client.UpdateHotspotOpFields(ctx, site, in, fields...)
		},
		Delete: func(ctx context.Context, site, id string) error {
			return client.DeleteHotspotOp(ctx, site, id)
		},
		List: func(ctx context.Context, site string) ([]ui.HotspotOp, error) {
			return client.ListHotspotOp(ctx, site)
		},
		GetID: func(s *ui.HotspotOp) string { return s.ID },
		SetID: func(s *ui.HotspotOp, id string) { s.ID = id },
	}
}
