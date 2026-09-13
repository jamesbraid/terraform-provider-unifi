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

// The unifi_ospf_router descriptor is derived from the controller's OSPFRouter
// definition. The scalars are emitted into ospf_router_descriptor_gen.go, as
// are the areas and interfaces element models. The hand judgment kept here is
// the two list_nested objects' encode/decode, which the emitter leaves to the
// descriptor.
//
// areas, areas[].network_ids and router_id are required on create; that
// requiredness comes from behavior.json's writes.OSPFRouter, not from a hand
// decision. areas and router_id are forced required by the compiler (top-level
// wires); areas[].network_ids is declared required in the policy and
// cross-checked against the artifact, the treatment firewall_policy's
// source.zone_id relies on. The v2 OSPF endpoint validates a create for real,
// which is why network_ids carries KeepZero: an empty list is a value the
// controller must see, not an absence.

// ospfElideEmptyString maps a wire "" to null, the nested-member counterpart
// of a top-level StringField's NullZero: an optional member the practitioner
// never wrote comes back as null rather than a configured empty.
func ospfElideEmptyString(s string) types.String {
	if s == "" {
		return types.StringNull()
	}
	return types.StringValue(s)
}

// ospfAreaToAPI encodes one areas element.
func ospfAreaToAPI(ctx context.Context, object types.Object) (ui.OSPFRouterAreas, diag.Diagnostics) {
	var diags diag.Diagnostics
	var m ospfRouterAreasModel
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
		diags.Append(m.NetworkIDs.ElementsAs(ctx, &out.NetworkIDs, false)...)
	}
	return out, diags
}

// ospfAreaFromAPI decodes one areas element. network_ids is Required, so it
// comes back as a list value (empty when the controller sends none) rather
// than null.
func ospfAreaFromAPI(ctx context.Context, e ui.OSPFRouterAreas) (types.Object, diag.Diagnostics) {
	var diags diag.Diagnostics
	networkIDs, d := types.ListValueFrom(ctx, types.StringType, e.NetworkIDs)
	diags.Append(d...)
	object, d := types.ObjectValue(ospfRouterAreasAttrTypes, map[string]attr.Value{
		"area_id":     ospfElideEmptyString(e.AreaID),
		"area_type":   ospfElideEmptyString(e.AreaType),
		"name":        ospfElideEmptyString(e.Name),
		"network_ids": networkIDs,
	})
	diags.Append(d...)
	return object, diags
}

// ospfInterfaceToAPI encodes one interfaces element.
func ospfInterfaceToAPI(ctx context.Context, object types.Object) (ui.OSPFRouterInterfaces, diag.Diagnostics) {
	var diags diag.Diagnostics
	var m ospfRouterInterfacesModel
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

// ospfInterfaceFromAPI decodes one interfaces element. passive_interface is a
// concrete bool both ways: it carries a schema default, so a false is a value
// rather than an absence.
func ospfInterfaceFromAPI(_ context.Context, e ui.OSPFRouterInterfaces) (types.Object, diag.Diagnostics) {
	return types.ObjectValue(ospfRouterInterfacesAttrTypes, map[string]attr.Value{
		"authentication_type": ospfElideEmptyString(e.AuthenticationType),
		"cost":                ospfElideEmptyString(e.Cost),
		"dead_interval":       ospfElideEmptyString(e.DeadInterval),
		"hello_interval":      ospfElideEmptyString(e.HelloInterval),
		"network_id":          ospfElideEmptyString(e.NetworkID),
		"passive_interface":   types.BoolValue(e.PassiveInterface),
		"priority":            ospfElideEmptyString(e.Priority),
	})
}

func ospfRouterKitSpec() resourcekit.Spec[ospfRouterKitModel, ui.OSPFRouter] {
	return resourcekit.Spec[ospfRouterKitModel, ui.OSPFRouter]{
		TypeName: "ospf_router",
		Subject:  "OSPF Router",
		IDWire:   "_id",
		New:      func() *ui.OSPFRouter { return &ui.OSPFRouter{} },
		ID:       func(m *ospfRouterKitModel) *types.String { return &m.ID },
		Site:     func(m *ospfRouterKitModel) *types.String { return &m.Site },
		Timeouts: func(m *ospfRouterKitModel) *timeouts.Value { return &m.Timeouts },
		Fields: resourcekit.Override(ospfRouterGenFields(), []resourcekit.Field[ospfRouterKitModel, ui.OSPFRouter]{
			resourcekit.ObjectListField[ospfRouterKitModel, ui.OSPFRouter, ui.OSPFRouterAreas]{
				Wire:      "areas",
				Model:     func(m *ospfRouterKitModel) *types.List { return &m.Areas },
				SDK:       func(s *ui.OSPFRouter) *[]ui.OSPFRouterAreas { return &s.Areas },
				AttrTypes: ospfRouterAreasAttrTypes,
				Encode:    ospfAreaToAPI,
				Decode:    ospfAreaFromAPI,
				Elide:     resourcekit.KeepZero,
			},
			resourcekit.ObjectListField[ospfRouterKitModel, ui.OSPFRouter, ui.OSPFRouterInterfaces]{
				Wire:      "interfaces",
				Model:     func(m *ospfRouterKitModel) *types.List { return &m.Interfaces },
				SDK:       func(s *ui.OSPFRouter) *[]ui.OSPFRouterInterfaces { return &s.Interfaces },
				AttrTypes: ospfRouterInterfacesAttrTypes,
				Encode:    ospfInterfaceToAPI,
				Decode:    ospfInterfaceFromAPI,
				Elide:     resourcekit.NullZero,
			},
		}),
		// Seeded here as well as in ospfRouterKitBackend, because Configure
		// binds the real Backend and a unit test calling ToModel on an
		// unconfigured spec still needs the identity accessors.
		Backend: resourcekit.Backend[ui.OSPFRouter]{
			GetID: func(s *ui.OSPFRouter) string { return s.ID },
			SetID: func(s *ui.OSPFRouter, id string) { s.ID = id },
		},
	}
}

func ospfRouterKitSchema() resourcekit.SchemaSpec {
	return resourcekit.SchemaSpec{
		Resource: resource_ospf_router.OspfRouterResourceSchema,
		Timeouts: timeouts.Opts{Create: true, Read: true, Update: true, Delete: true},
	}
}

// ospfRouterKitBackend binds the spec to a client. The OSPF endpoints live
// under v2/api, like firewall_policy's.
func ospfRouterKitBackend(client *ui.ApiClient) resourcekit.Backend[ui.OSPFRouter] {
	return resourcekit.Backend[ui.OSPFRouter]{
		Create: func(ctx context.Context, site string, in *ui.OSPFRouter) (*ui.OSPFRouter, error) {
			return client.CreateOSPFRouter(ctx, site, in)
		},
		Read: func(ctx context.Context, site, id string) (*ui.OSPFRouter, error) {
			return client.GetOSPFRouter(ctx, site, id)
		},
		UpdateFields: func(
			ctx context.Context, site string, in *ui.OSPFRouter, fields ...string,
		) (*ui.OSPFRouter, error) {
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
