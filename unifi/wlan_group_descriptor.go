package unifi

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/types"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/listresource_wlan_group"
	resource_wlan_group "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_wlan_group"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/resourcekit"
)

// wlanGroupKitSpec: the smallest descriptor in the tree. The SDK struct
// carries one practitioner-facing field -- name -- and the four attr_*
// controller internals stay unmapped, so the derived mask can never offer
// them back on a write.
func wlanGroupKitSpec() resourcekit.Spec[wlanGroupKitModel, ui.WLANGroup] {
	return resourcekit.Spec[wlanGroupKitModel, ui.WLANGroup]{
		TypeName: "wlan_group",
		Subject:  "WLAN Group",
		IDWire:   "_id",
		New:      func() *ui.WLANGroup { return &ui.WLANGroup{} },
		ID:       func(m *wlanGroupKitModel) *types.String { return &m.ID },
		Site:     func(m *wlanGroupKitModel) *types.String { return &m.Site },
		Timeouts: func(m *wlanGroupKitModel) *timeouts.Value { return &m.Timeouts },
		Fields:   wlanGroupGenFields(),
		// Seeded here as well as in wlanGroupKitBackend, because Configure
		// binds the real Backend and a unit test calling ToModel on an
		// unconfigured spec would otherwise dereference nil.
		Backend: resourcekit.Backend[ui.WLANGroup]{
			GetID: func(s *ui.WLANGroup) string { return s.ID },
			SetID: func(s *ui.WLANGroup, id string) { s.ID = id },
		},
	}
}

func wlanGroupKitSchema() resourcekit.SchemaSpec {
	return resourcekit.SchemaSpec{
		Resource: resource_wlan_group.WlanGroupResourceSchema,
		Timeouts: timeouts.Opts{Create: true, Read: true, Update: true, Delete: true},
	}
}

func wlanGroupKitList() resourcekit.ListSpec[ui.WLANGroup] {
	return resourcekit.ListSpec[ui.WLANGroup]{
		ConfigSchema: listresource_wlan_group.WlanGroupListResourceSchema,
		DisplayName: func(s *ui.WLANGroup) string {
			if s.Name != "" {
				return s.Name
			}
			return s.ID
		},
		Filters: map[string]func(*ui.WLANGroup) string{
			"name": func(s *ui.WLANGroup) string { return s.Name },
		},
	}
}

func wlanGroupKitBackend(client *ui.ApiClient) resourcekit.Backend[ui.WLANGroup] {
	return resourcekit.Backend[ui.WLANGroup]{
		Create: func(ctx context.Context, site string, in *ui.WLANGroup) (*ui.WLANGroup, error) {
			return client.CreateWLANGroup(ctx, site, in)
		},
		Read: func(ctx context.Context, site, id string) (*ui.WLANGroup, error) {
			return client.GetWLANGroup(ctx, site, id)
		},
		UpdateFields: func(
			ctx context.Context, site string, in *ui.WLANGroup, fields ...string,
		) (*ui.WLANGroup, error) {
			return client.UpdateWLANGroupFields(ctx, site, in, fields...)
		},
		Delete: func(ctx context.Context, site, id string) error {
			return client.DeleteWLANGroup(ctx, site, id)
		},
		List: func(ctx context.Context, site string) ([]ui.WLANGroup, error) {
			return client.ListWLANGroup(ctx, site)
		},
		GetID: func(s *ui.WLANGroup) string { return s.ID },
		SetID: func(s *ui.WLANGroup, id string) { s.ID = id },
	}
}
