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

type radiusUserKitModel struct {
	ID               types.String   `tfsdk:"id"`
	Site             types.String   `tfsdk:"site"`
	Name             types.String   `tfsdk:"name"`
	Password         types.String   `tfsdk:"password"`
	TunnelType       types.Int64    `tfsdk:"tunnel_type"`
	TunnelMediumType types.Int64    `tfsdk:"tunnel_medium_type"`
	NetworkID        types.String   `tfsdk:"network_id"`
	VLAN             types.Int64    `tfsdk:"vlan"`
	TunnelConfigType types.String   `tfsdk:"tunnel_config_type"`
	Timeouts         timeouts.Value `tfsdk:"timeouts"`
}

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

		Fields: []resourcekit.Field[radiusUserKitModel, ui.Account]{
			resourcekit.StringField[radiusUserKitModel, ui.Account]{
				Wire:  "name",
				Model: func(m *radiusUserKitModel) *types.String { return &m.Name },
				SDK:   func(s *ui.Account) *string { return &s.Name },
				Elide: resourcekit.KeepZero,
			},
			// The controller calls it x_password. The Terraform name is
			// password, and nothing but the wire-name check would catch a
			// descriptor that used the Terraform spelling here -- the mask
			// would name an attribute the controller does not have and the
			// password would never change.
			resourcekit.StringField[radiusUserKitModel, ui.Account]{
				Wire:  "x_password",
				Model: func(m *radiusUserKitModel) *types.String { return &m.Password },
				SDK:   func(s *ui.Account) *string { return &s.Password },
				Elide: resourcekit.KeepZero,
			},
			resourcekit.Int64PtrField[radiusUserKitModel, ui.Account]{
				Wire:  "tunnel_type",
				Model: func(m *radiusUserKitModel) *types.Int64 { return &m.TunnelType },
				SDK:   func(s *ui.Account) **int64 { return &s.TunnelType },
				Elide: resourcekit.KeepZero,
			},
			resourcekit.Int64PtrField[radiusUserKitModel, ui.Account]{
				Wire:  "tunnel_medium_type",
				Model: func(m *radiusUserKitModel) *types.Int64 { return &m.TunnelMediumType },
				SDK:   func(s *ui.Account) **int64 { return &s.TunnelMediumType },
				Elide: resourcekit.KeepZero,
			},
			resourcekit.StringField[radiusUserKitModel, ui.Account]{
				Wire:  "networkconf_id",
				Model: func(m *radiusUserKitModel) *types.String { return &m.NetworkID },
				SDK:   func(s *ui.Account) *string { return &s.NetworkID },
				Elide: resourcekit.NullZero,
			},
			resourcekit.Int64PtrField[radiusUserKitModel, ui.Account]{
				Wire:  "vlan",
				Model: func(m *radiusUserKitModel) *types.Int64 { return &m.VLAN },
				SDK:   func(s *ui.Account) **int64 { return &s.VLAN },
				Elide: resourcekit.KeepZero,
			},
			resourcekit.StringField[radiusUserKitModel, ui.Account]{
				Wire:  "tunnel_config_type",
				Model: func(m *radiusUserKitModel) *types.String { return &m.TunnelConfigType },
				SDK:   func(s *ui.Account) *string { return &s.TunnelConfigType },
				Elide: resourcekit.NullZero,
			},
		},
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
