package unifi

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-nettypes/iptypes"
	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/listresource_static_route"
	resource_static_route "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_static_route"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/resourcekit"
)

type staticRouteKitModel struct {
	ID            types.String      `tfsdk:"id"`
	Site          types.String      `tfsdk:"site"`
	Name          types.String      `tfsdk:"name"`
	Network       types.String      `tfsdk:"network"`
	Type          types.String      `tfsdk:"type"`
	Distance      types.Int64       `tfsdk:"distance"`
	NextHop       iptypes.IPAddress `tfsdk:"next_hop"`
	Interface     types.String      `tfsdk:"interface"`
	Enabled       types.Bool        `tfsdk:"enabled"`
	GatewayDevice types.String      `tfsdk:"gateway_device"`
	GatewayType   types.String      `tfsdk:"gateway_type"`
	Timeouts      timeouts.Value    `tfsdk:"timeouts"`
}

// isRouteType builds the write predicate for a route-type-specific field.
// Nothing else suppresses them: the ConfigValidators check that network and
// next_hop share an IP version, not which field belongs to which route type,
// so without this the kit would send whatever the plan happened to hold.
func isRouteType(want string) func(*staticRouteKitModel) bool {
	return func(m *staticRouteKitModel) bool { return m.Type.ValueString() == want }
}

func staticRouteKitSpec() resourcekit.Spec[staticRouteKitModel, ui.Routing] {
	return resourcekit.Spec[staticRouteKitModel, ui.Routing]{
		TypeName: "static_route",
		Subject:  "Static Route",
		New:      func() *ui.Routing { return &ui.Routing{Type: "static-route"} },
		ID:       func(m *staticRouteKitModel) *types.String { return &m.ID },
		Site:     func(m *staticRouteKitModel) *types.String { return &m.Site },
		Timeouts: func(m *staticRouteKitModel) *timeouts.Value { return &m.Timeouts },
		Fields: []resourcekit.Field[staticRouteKitModel, ui.Routing]{
			resourcekit.StringField[staticRouteKitModel, ui.Routing]{
				Wire:  "name",
				Model: func(m *staticRouteKitModel) *types.String { return &m.Name },
				SDK:   func(s *ui.Routing) *string { return &s.Name },
				Elide: resourcekit.KeepZero,
			},
			resourcekit.StringField[staticRouteKitModel, ui.Routing]{
				Wire:  "static-route_network",
				Model: func(m *staticRouteKitModel) *types.String { return &m.Network },
				SDK:   func(s *ui.Routing) *string { return &s.StaticRouteNetwork },
				Elide: resourcekit.KeepZero,
			},
			resourcekit.StringField[staticRouteKitModel, ui.Routing]{
				Wire:  "static-route_type",
				Model: func(m *staticRouteKitModel) *types.String { return &m.Type },
				SDK:   func(s *ui.Routing) *string { return &s.StaticRouteType },
				Elide: resourcekit.KeepZero,
			},
			resourcekit.Int64PtrField[staticRouteKitModel, ui.Routing]{
				Wire:  "static-route_distance",
				Model: func(m *staticRouteKitModel) *types.Int64 { return &m.Distance },
				SDK:   func(s *ui.Routing) **int64 { return &s.StaticRouteDistance },
				Elide: resourcekit.KeepZero,
			},
			// Custom-typed and conditional: the only field that is both.
			resourcekit.StringLikeField[staticRouteKitModel, ui.Routing, iptypes.IPAddress]{
				Wire:  "static-route_nexthop",
				Model: func(m *staticRouteKitModel) *iptypes.IPAddress { return &m.NextHop },
				SDK:   func(s *ui.Routing) *string { return &s.StaticRouteNexthop },
				New: func(v basetypes.StringValue) iptypes.IPAddress {
					return iptypes.IPAddress{StringValue: v}
				},
				Elide:     resourcekit.NullZero,
				WriteWhen: isRouteType("nexthop-route"),
			},
			resourcekit.StringField[staticRouteKitModel, ui.Routing]{
				Wire:      "static-route_interface",
				Model:     func(m *staticRouteKitModel) *types.String { return &m.Interface },
				SDK:       func(s *ui.Routing) *string { return &s.StaticRouteInterface },
				Elide:     resourcekit.NullZero,
				WriteWhen: isRouteType("interface-route"),
			},
			resourcekit.BoolField[staticRouteKitModel, ui.Routing]{
				Wire:  "enabled",
				Model: func(m *staticRouteKitModel) *types.Bool { return &m.Enabled },
				SDK:   func(s *ui.Routing) *bool { return &s.Enabled },
			},
			resourcekit.StringField[staticRouteKitModel, ui.Routing]{
				Wire:  "gateway_device",
				Model: func(m *staticRouteKitModel) *types.String { return &m.GatewayDevice },
				SDK:   func(s *ui.Routing) *string { return &s.GatewayDevice },
				Elide: resourcekit.NullZero,
			},
			// No Elide, because the default replaces it: an empty read
			// reports "default" (ReadDefault below), so there's no zero left
			// for an elision to judge.
			resourcekit.StringField[staticRouteKitModel, ui.Routing]{
				Wire:        "gateway_type",
				Model:       func(m *staticRouteKitModel) *types.String { return &m.GatewayType },
				SDK:         func(s *ui.Routing) *string { return &s.GatewayType },
				ReadDefault: "default",
			},
		},
		// Seeded here as well as in staticRouteKitBackend, because Configure binds
		// the real Backend and a unit test calling ToModel on an unconfigured
		// spec would otherwise dereference nil.
		Backend: resourcekit.Backend[ui.Routing]{
			GetID: func(s *ui.Routing) string { return s.ID },
			SetID: func(s *ui.Routing, id string) { s.ID = id },
		},
	}
}

func staticRouteKitSchema() resourcekit.SchemaSpec {
	return resourcekit.SchemaSpec{
		Resource: resource_static_route.StaticRouteResourceSchema,
		Timeouts: timeouts.Opts{Create: true, Read: true, Update: true, Delete: true},
	}
}

func staticRouteKitList() resourcekit.ListSpec[ui.Routing] {
	return resourcekit.ListSpec[ui.Routing]{
		ConfigSchema: listresource_static_route.StaticRouteListResourceSchema,
		DisplayName: func(s *ui.Routing) string {
			if s.Name != "" {
				return s.Name
			}
			return s.ID
		},
		Filters: map[string]func(*ui.Routing) string{
			"name": func(s *ui.Routing) string { return s.Name },
			"type": func(s *ui.Routing) string { return s.StaticRouteType },
		},
	}
}

func staticRouteKitBackend(client *ui.ApiClient) resourcekit.Backend[ui.Routing] {
	return resourcekit.Backend[ui.Routing]{
		Create: func(ctx context.Context, site string, in *ui.Routing) (*ui.Routing, error) {
			return client.CreateRouting(ctx, site, in)
		},
		Read: func(ctx context.Context, site, id string) (*ui.Routing, error) {
			return client.GetRouting(ctx, site, id)
		},
		UpdateFields: func(ctx context.Context, site string, in *ui.Routing, fields ...string) (*ui.Routing, error) {
			return client.UpdateRoutingFields(ctx, site, in, fields...)
		},
		Delete: func(ctx context.Context, site, id string) error {
			return client.DeleteRouting(ctx, site, id)
		},
		List: func(ctx context.Context, site string) ([]ui.Routing, error) {
			return client.ListRouting(ctx, site)
		},
		GetID: func(s *ui.Routing) string { return s.ID },
		SetID: func(s *ui.Routing, id string) { s.ID = id },
	}
}
