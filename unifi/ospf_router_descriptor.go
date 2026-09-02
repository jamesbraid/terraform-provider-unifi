package unifi

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
	resource_ospf_router "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_ospf_router"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/resourcekit"
)

// ospfAreaModel describes one nested areas entry.
type ospfAreaModel struct {
	AreaID     types.String `tfsdk:"area_id"`
	AreaType   types.String `tfsdk:"area_type"`
	Name       types.String `tfsdk:"name"`
	NetworkIDs types.List   `tfsdk:"network_ids"`
}

func (m ospfAreaModel) AttributeTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"area_id":     types.StringType,
		"area_type":   types.StringType,
		"name":        types.StringType,
		"network_ids": types.ListType{ElemType: types.StringType},
	}
}

// ospfInterfaceModel describes one nested interfaces entry.
type ospfInterfaceModel struct {
	AuthenticationType types.String `tfsdk:"authentication_type"`
	Cost               types.String `tfsdk:"cost"`
	DeadInterval       types.String `tfsdk:"dead_interval"`
	HelloInterval      types.String `tfsdk:"hello_interval"`
	NetworkID          types.String `tfsdk:"network_id"`
	PassiveInterface   types.Bool   `tfsdk:"passive_interface"`
	Priority           types.String `tfsdk:"priority"`
}

func (m ospfInterfaceModel) AttributeTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"authentication_type": types.StringType,
		"cost":                types.StringType,
		"dead_interval":       types.StringType,
		"hello_interval":      types.StringType,
		"network_id":          types.StringType,
		"passive_interface":   types.BoolType,
		"priority":            types.StringType,
	}
}

type ospfRouterKitModel struct {
	ID                                    types.String   `tfsdk:"id"`
	Site                                  types.String   `tfsdk:"site"`
	AnnounceDefaultRoute                  types.Bool     `tfsdk:"announce_default_route"`
	Areas                                 types.List     `tfsdk:"areas"`
	Enabled                               types.Bool     `tfsdk:"enabled"`
	Interfaces                            types.List     `tfsdk:"interfaces"`
	RedistributeBgpRoutes                 types.Bool     `tfsdk:"redistribute_bgp_routes"`
	RedistributeConnectedRoutes           types.Bool     `tfsdk:"redistribute_connected_routes"`
	RedistributeConnectedRoutesMetricType types.String   `tfsdk:"redistribute_connected_routes_metric_type"`
	RedistributeStaticRoutes              types.Bool     `tfsdk:"redistribute_static_routes"`
	RedistributeStaticRoutesMetricType    types.String   `tfsdk:"redistribute_static_routes_metric_type"`
	RouterID                              types.String   `tfsdk:"router_id"`
	Timeouts                              timeouts.Value `tfsdk:"timeouts"`
}

// ospfRouterKitSpec is the whole of what varies. The v2 endpoint validates
// creates for real: it refuses an empty router_id and an empty areas list
// with NotEmpty violations (probed live), which is why both are Required in
// the policy and carry KeepZero here.
func ospfRouterKitSpec() resourcekit.Spec[ospfRouterKitModel, ui.OSPFRouter] {
	return resourcekit.Spec[ospfRouterKitModel, ui.OSPFRouter]{
		TypeName: "ospf_router",
		Subject:  "OSPF Router",
		New:      func() *ui.OSPFRouter { return &ui.OSPFRouter{} },
		ID:       func(m *ospfRouterKitModel) *types.String { return &m.ID },
		Site:     func(m *ospfRouterKitModel) *types.String { return &m.Site },
		Timeouts: func(m *ospfRouterKitModel) *timeouts.Value { return &m.Timeouts },
		Fields: []resourcekit.Field[ospfRouterKitModel, ui.OSPFRouter]{
			resourcekit.BoolField[ospfRouterKitModel, ui.OSPFRouter]{
				Wire:  "announce_default_route",
				Model: func(m *ospfRouterKitModel) *types.Bool { return &m.AnnounceDefaultRoute },
				SDK:   func(s *ui.OSPFRouter) *bool { return &s.AnnounceDefaultRoute },
			},
			resourcekit.ObjectListField[ospfRouterKitModel, ui.OSPFRouter, ui.OSPFRouterAreas]{
				Wire:      "areas",
				Model:     func(m *ospfRouterKitModel) *types.List { return &m.Areas },
				SDK:       func(s *ui.OSPFRouter) *[]ui.OSPFRouterAreas { return &s.Areas },
				AttrTypes: ospfAreaModel{}.AttributeTypes(),
				Encode:    ospfAreaToAPI,
				Decode:    ospfAreaFromAPI,
				Elide:     resourcekit.KeepZero,
			},
			resourcekit.BoolField[ospfRouterKitModel, ui.OSPFRouter]{
				Wire:  "enabled",
				Model: func(m *ospfRouterKitModel) *types.Bool { return &m.Enabled },
				SDK:   func(s *ui.OSPFRouter) *bool { return &s.Enabled },
			},
			resourcekit.ObjectListField[ospfRouterKitModel, ui.OSPFRouter, ui.OSPFRouterInterfaces]{
				Wire:      "interfaces",
				Model:     func(m *ospfRouterKitModel) *types.List { return &m.Interfaces },
				SDK:       func(s *ui.OSPFRouter) *[]ui.OSPFRouterInterfaces { return &s.Interfaces },
				AttrTypes: ospfInterfaceModel{}.AttributeTypes(),
				Encode:    ospfInterfaceToAPI,
				Decode:    ospfInterfaceFromAPI,
				Elide:     resourcekit.NullZero,
			},
			resourcekit.BoolField[ospfRouterKitModel, ui.OSPFRouter]{
				Wire:  "redistribute_bgp_routes",
				Model: func(m *ospfRouterKitModel) *types.Bool { return &m.RedistributeBgpRoutes },
				SDK:   func(s *ui.OSPFRouter) *bool { return &s.RedistributeBgpRoutes },
			},
			resourcekit.BoolField[ospfRouterKitModel, ui.OSPFRouter]{
				Wire:  "redistribute_connected_routes",
				Model: func(m *ospfRouterKitModel) *types.Bool { return &m.RedistributeConnectedRoutes },
				SDK:   func(s *ui.OSPFRouter) *bool { return &s.RedistributeConnectedRoutes },
			},
			resourcekit.StringField[ospfRouterKitModel, ui.OSPFRouter]{
				Wire: "redistribute_connected_routes_metric_type",
				Model: func(m *ospfRouterKitModel) *types.String {
					return &m.RedistributeConnectedRoutesMetricType
				},
				SDK:   func(s *ui.OSPFRouter) *string { return &s.RedistributeConnectedRoutesMetricType },
				Elide: resourcekit.NullZero,
			},
			resourcekit.BoolField[ospfRouterKitModel, ui.OSPFRouter]{
				Wire:  "redistribute_static_routes",
				Model: func(m *ospfRouterKitModel) *types.Bool { return &m.RedistributeStaticRoutes },
				SDK:   func(s *ui.OSPFRouter) *bool { return &s.RedistributeStaticRoutes },
			},
			resourcekit.StringField[ospfRouterKitModel, ui.OSPFRouter]{
				Wire: "redistribute_static_routes_metric_type",
				Model: func(m *ospfRouterKitModel) *types.String {
					return &m.RedistributeStaticRoutesMetricType
				},
				SDK:   func(s *ui.OSPFRouter) *string { return &s.RedistributeStaticRoutesMetricType },
				Elide: resourcekit.NullZero,
			},
			resourcekit.StringField[ospfRouterKitModel, ui.OSPFRouter]{
				Wire:  "router_id",
				Model: func(m *ospfRouterKitModel) *types.String { return &m.RouterID },
				SDK:   func(s *ui.OSPFRouter) *string { return &s.RouterID },
				Elide: resourcekit.KeepZero,
			},
		},
		// Seeded here as well as in ospfRouterKitBackend, because Configure
		// binds the real Backend and a unit test calling ToModel on an
		// unconfigured spec still needs the identity accessors.
		Backend: resourcekit.Backend[ui.OSPFRouter]{
			GetID: func(s *ui.OSPFRouter) string { return s.ID },
			SetID: func(s *ui.OSPFRouter, id string) { s.ID = id },
		},
	}
}

// ospfAreaToAPI encodes one areas element. An optional member left null
// stays a Go zero, which omitempty then keeps off the wire.
func ospfAreaToAPI(ctx context.Context, object types.Object) (ui.OSPFRouterAreas, diag.Diagnostics) {
	var diags diag.Diagnostics
	var m ospfAreaModel
	diags.Append(object.As(ctx, &m, basetypes.ObjectAsOptions{})...)
	if diags.HasError() {
		return ui.OSPFRouterAreas{}, diags
	}
	out := ui.OSPFRouterAreas{
		AreaID:   m.AreaID.ValueString(),
		AreaType: m.AreaType.ValueString(),
		Name:     m.Name.ValueString(),
	}
	if !m.NetworkIDs.IsNull() && !m.NetworkIDs.IsUnknown() {
		var ids []string
		diags.Append(m.NetworkIDs.ElementsAs(ctx, &ids, false)...)
		out.NetworkIDs = ids
	}
	return out, diags
}

// ospfAreaFromAPI decodes one areas element. Optional strings come back as
// "" when unset (omitempty on the wire), which reads as null so an attribute
// the practitioner never wrote does not show up as a configured empty.
func ospfAreaFromAPI(_ context.Context, e ui.OSPFRouterAreas) (types.Object, diag.Diagnostics) {
	networkIDs := types.ListNull(types.StringType)
	if len(e.NetworkIDs) > 0 {
		values := make([]attr.Value, len(e.NetworkIDs))
		for i, id := range e.NetworkIDs {
			values[i] = types.StringValue(id)
		}
		var diags diag.Diagnostics
		networkIDs, diags = types.ListValue(types.StringType, values)
		if diags.HasError() {
			return types.ObjectNull(ospfAreaModel{}.AttributeTypes()), diags
		}
	}
	return types.ObjectValue(ospfAreaModel{}.AttributeTypes(), map[string]attr.Value{
		"area_id":     types.StringValue(e.AreaID),
		"area_type":   elideEmptyString(e.AreaType),
		"name":        elideEmptyString(e.Name),
		"network_ids": networkIDs,
	})
}

// ospfInterfaceToAPI encodes one interfaces element, the same shape as
// ospfAreaToAPI.
func ospfInterfaceToAPI(ctx context.Context, object types.Object) (ui.OSPFRouterInterfaces, diag.Diagnostics) {
	var diags diag.Diagnostics
	var m ospfInterfaceModel
	diags.Append(object.As(ctx, &m, basetypes.ObjectAsOptions{})...)
	if diags.HasError() {
		return ui.OSPFRouterInterfaces{}, diags
	}
	return ui.OSPFRouterInterfaces{
		AuthenticationType: m.AuthenticationType.ValueString(),
		Cost:               m.Cost.ValueString(),
		DeadInterval:       m.DeadInterval.ValueString(),
		HelloInterval:      m.HelloInterval.ValueString(),
		NetworkID:          m.NetworkID.ValueString(),
		PassiveInterface:   m.PassiveInterface.ValueBool(),
		Priority:           m.Priority.ValueString(),
	}, diags
}

// ospfInterfaceFromAPI decodes one interfaces element. passive_interface is
// a concrete bool both ways: it carries a schema default, so a false is a
// value rather than an absence.
func ospfInterfaceFromAPI(_ context.Context, e ui.OSPFRouterInterfaces) (types.Object, diag.Diagnostics) {
	return types.ObjectValue(ospfInterfaceModel{}.AttributeTypes(), map[string]attr.Value{
		"authentication_type": elideEmptyString(e.AuthenticationType),
		"cost":                elideEmptyString(e.Cost),
		"dead_interval":       elideEmptyString(e.DeadInterval),
		"hello_interval":      elideEmptyString(e.HelloInterval),
		"network_id":          types.StringValue(e.NetworkID),
		"passive_interface":   types.BoolValue(e.PassiveInterface),
		"priority":            elideEmptyString(e.Priority),
	})
}

// elideEmptyString maps a wire "" to null, the nested-member counterpart of
// a top-level StringField's NullZero.
func elideEmptyString(s string) types.String {
	if s == "" {
		return types.StringNull()
	}
	return types.StringValue(s)
}

func ospfRouterKitSchema() resourcekit.SchemaSpec {
	return resourcekit.SchemaSpec{
		Resource: resource_ospf_router.OspfRouterResourceSchema,
		Timeouts: timeouts.Opts{Create: true, Read: true, Update: true, Delete: true},
	}
}

// ospfRouterKitBackend binds the spec to a client. The OSPF endpoints live
// under v2/api, like firewall_policy's; nothing about the wiring differs.
func ospfRouterKitBackend(client *ui.ApiClient) resourcekit.Backend[ui.OSPFRouter] {
	return resourcekit.Backend[ui.OSPFRouter]{
		Create: func(ctx context.Context, site string, in *ui.OSPFRouter) (*ui.OSPFRouter, error) {
			return client.CreateOSPFRouter(ctx, site, in)
		},
		Read: func(ctx context.Context, site, id string) (*ui.OSPFRouter, error) {
			return client.GetOSPFRouter(ctx, site, id)
		},
		UpdateFields: func(ctx context.Context, site string, in *ui.OSPFRouter, fields ...string) (*ui.OSPFRouter, error) {
			return client.UpdateOSPFRouterFields(ctx, site, in, fields...)
		},
		Delete: func(ctx context.Context, site, id string) error {
			return client.DeleteOSPFRouter(ctx, site, id)
		},
		List: func(ctx context.Context, site string) ([]ui.OSPFRouter, error) {
			return client.ListOSPFRouter(ctx, site)
		},
		GetID: func(s *ui.OSPFRouter) string { return s.ID },
		SetID: func(s *ui.OSPFRouter, id string) { s.ID = id },
	}
}
