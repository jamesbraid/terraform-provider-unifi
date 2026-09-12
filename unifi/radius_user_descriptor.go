package unifi

// The account's VLAN is derived: when vlan is unset and network_id is, the
// account inherits that network's VLAN via a controller lookup in BeforeSend
// (in radius_user_resource.go). That lookup is keyed on a model field rather
// than the site, so it can't be a Prefetch.

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/types"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/listresource_radius_user"
	resource_radius_user "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_radius_user"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/resourcekit"
)

func radiusUserKitSpec() resourcekit.Spec[radiusUserKitModel, ui.Account] {
	return resourcekit.Spec[radiusUserKitModel, ui.Account]{
		TypeName: "radius_user",
		Subject:  "RADIUS User",
		New:      func() *ui.Account { return &ui.Account{} },
		ID:       func(m *radiusUserKitModel) *types.String { return &m.ID },
		Site:     func(m *radiusUserKitModel) *types.String { return &m.Site },
		Timeouts: func(m *radiusUserKitModel) *timeouts.Value { return &m.Timeouts },

		// vlan is written by BeforeSend whether or not the plan names it, so
		// it has to be in the mask: a practitioner changing network_id alone
		// expects the derived VLAN to follow, and the plan mentions only
		// network_id.
		AlwaysWire: []string{"vlan"},

		Fields: resourcekit.Override(radiusUserGenFields(), []resourcekit.Field[radiusUserKitModel, ui.Account]{
			// OmitZero: Optional+Computed with UseStateForUnknown and no
			// schema default -- the dtim_6e shape exactly (R2-C Task 10b):
			// an unset plan value is genuinely Unknown on create, and
			// ValueInt64Pointer() would force-emit the zero the controller's
			// own pattern rejects.
			resourcekit.Int64PtrField[radiusUserKitModel, ui.Account]{
				Wire:  "vlan",
				Model: func(m *radiusUserKitModel) *types.Int64 { return &m.VLAN },
				SDK:   func(s *ui.Account) **int64 { return &s.VLAN },
				Elide: resourcekit.KeepZero, OmitZero: true,
			},
		}),
		// Seeded here as well as in radiusUserKitBackend, because Configure binds
		// the real Backend and a unit test calling ToModel on an unconfigured
		// spec would otherwise dereference nil.
		Backend: resourcekit.Backend[ui.Account]{
			GetID: func(s *ui.Account) string { return s.ID },
			SetID: func(s *ui.Account, id string) { s.ID = id },
		},
	}
}

func radiusUserKitSchema() resourcekit.SchemaSpec {
	return resourcekit.SchemaSpec{
		Resource: resource_radius_user.RadiusUserResourceSchema,
		Timeouts: timeouts.Opts{Create: true, Read: true, Update: true, Delete: true},
	}
}

func radiusUserKitList() resourcekit.ListSpec[ui.Account] {
	return resourcekit.ListSpec[ui.Account]{
		ConfigSchema: listresource_radius_user.RadiusUserListResourceSchema,
		DisplayName: func(s *ui.Account) string {
			if s.Name != "" {
				return s.Name
			}
			return s.ID
		},
		Filters: map[string]func(*ui.Account) string{
			"name": func(s *ui.Account) string { return s.Name },
		},
	}
}

func radiusUserKitBackend(client *ui.ApiClient) resourcekit.Backend[ui.Account] {
	return resourcekit.Backend[ui.Account]{
		Create: func(ctx context.Context, site string, in *ui.Account) (*ui.Account, error) {
			return client.CreateAccount(ctx, site, in)
		},
		Read: func(ctx context.Context, site, id string) (*ui.Account, error) {
			return client.GetAccount(ctx, site, id)
		},
		UpdateFields: func(ctx context.Context, site string, in *ui.Account, fields ...string) (*ui.Account, error) {
			return client.UpdateAccountFields(ctx, site, in, fields...)
		},
		Delete: func(ctx context.Context, site, id string) error {
			return client.DeleteAccount(ctx, site, id)
		},
		List: func(ctx context.Context, site string) ([]ui.Account, error) {
			return client.ListAccount(ctx, site)
		},
		GetID: func(s *ui.Account) string { return s.ID },
		SetID: func(s *ui.Account, id string) { s.ID = id },
	}
}
