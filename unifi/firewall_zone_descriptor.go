package unifi

// The firewall_zone descriptor.
//
// WAS A PROTOTYPE UNTIL THIS COMMIT. It was written to find the kit's gaps
// rather than to serve traffic, and it did: read-only fields, a pointer bool
// and a string list all arrived because this surface needed them. The spec
// below is unchanged from the prototype; what is added is the schema, list and
// backend halves that turn it into a live descriptor.
//
// It was the SECOND descriptor rather than a repeat of the first.
//
// firewall_zone was chosen over another simple surface for three reasons, each
// a thing dns_record cannot exercise: it is BOOTSTRAP-compiled, so go_name and
// pointer-ness exist for it and the contract check has real subjects; it has a
// LIST of strings and a POINTER bool, which are field kinds dns_record has no
// instance of; and three of its fields are computed, so it is the first
// descriptor that has anything to mark read-only.

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/types"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/listresource_firewall_zone"
	resource_firewall_zone "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_firewall_zone"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/resourcekit"
)

type firewallZoneKitModel struct {
	ID          types.String   `tfsdk:"id"`
	Site        types.String   `tfsdk:"site"`
	Name        types.String   `tfsdk:"name"`
	NetworkIDs  types.List     `tfsdk:"network_ids"`
	ZoneKey     types.String   `tfsdk:"zone_key"`
	DefaultZone types.Bool     `tfsdk:"default_zone"`
	Timeouts    timeouts.Value `tfsdk:"timeouts"`
}

// firewallZoneKitSpec. Every value is read off the mapping artifact joined to
// the bootstrap on structural_name, except the SDK method names.
//
// THE THREE READ-ONLY FIELDS ARE THE POINT OF THIS DESCRIPTOR. _id, zone_key
// and default_zone are computed in the policy, and the hand-written
// modelToFirewallZone sends exactly the other two. Marking them ReadOnly is
// what stops the generated path asking the controller to accept values it is
// itself the author of.
func firewallZoneKitSpec() resourcekit.Spec[firewallZoneKitModel, ui.FirewallZone] {
	return resourcekit.Spec[firewallZoneKitModel, ui.FirewallZone]{
		TypeName: "firewall_zone",
		Subject:  "Firewall Zone",
		IDWire:   "_id",
		New:      func() *ui.FirewallZone { return &ui.FirewallZone{} },
		ID:       func(m *firewallZoneKitModel) *types.String { return &m.ID },
		Site:     func(m *firewallZoneKitModel) *types.String { return &m.Site },
		Timeouts: func(m *firewallZoneKitModel) *timeouts.Value { return &m.Timeouts },
		Fields: []resourcekit.Field[firewallZoneKitModel, ui.FirewallZone]{
			resourcekit.StringField[firewallZoneKitModel, ui.FirewallZone]{
				Wire:  "name",
				Model: func(m *firewallZoneKitModel) *types.String { return &m.Name },
				SDK:   func(s *ui.FirewallZone) *string { return &s.Name },
				Elide: resourcekit.KeepZero,
			},
			// THE SLICE IS EMPTIED RATHER THAN LEFT NIL, inside the field kind.
			// network_ids is the SDK's only field without omitempty, so an empty
			// list is sent as [] and a nil one would be sent as null -- which the
			// controller reads as a different request.
			resourcekit.StringListField[firewallZoneKitModel, ui.FirewallZone]{
				Wire:  "network_ids",
				Model: func(m *firewallZoneKitModel) *types.List { return &m.NetworkIDs },
				SDK:   func(s *ui.FirewallZone) *[]string { return &s.NetworkIDs },
			},
			resourcekit.ReadOnly[firewallZoneKitModel, ui.FirewallZone](
				resourcekit.StringField[firewallZoneKitModel, ui.FirewallZone]{
					Wire:  "zone_key",
					Model: func(m *firewallZoneKitModel) *types.String { return &m.ZoneKey },
					SDK:   func(s *ui.FirewallZone) *string { return &s.ZoneKey },
					Elide: resourcekit.KeepZero,
				}),
			// POINTER BOOL, and the bootstrap is what says so: default_zone is
			// the one field of the twelve marked pointer. Read through BoolField
			// it would turn "the controller did not say" into "the controller
			// said false".
			resourcekit.ReadOnly[firewallZoneKitModel, ui.FirewallZone](
				resourcekit.BoolPtrField[firewallZoneKitModel, ui.FirewallZone]{
					Wire:  "default_zone",
					Model: func(m *firewallZoneKitModel) *types.Bool { return &m.DefaultZone },
					SDK:   func(s *ui.FirewallZone) **bool { return &s.DefaultZone },
				}),
		},
		Backend: resourcekit.Backend[ui.FirewallZone]{
			GetID: func(s *ui.FirewallZone) string { return s.ID },
			SetID: func(s *ui.FirewallZone, id string) { s.ID = id },
		},
	}
}

// firewallZoneKitSchema is the schema half. No version and no upgraders: this
// surface has never migrated its state shape.
func firewallZoneKitSchema() resourcekit.SchemaSpec {
	return resourcekit.SchemaSpec{
		Resource: resource_firewall_zone.FirewallZoneResourceSchema,
		Timeouts: timeouts.Opts{Create: true, Read: true, Update: true, Delete: true},
	}
}

func firewallZoneKitList() resourcekit.ListSpec[ui.FirewallZone] {
	return resourcekit.ListSpec[ui.FirewallZone]{
		ConfigSchema: listresource_firewall_zone.FirewallZoneListResourceSchema,
		DisplayName: func(s *ui.FirewallZone) string {
			if s.Name != "" {
				return s.Name
			}
			return s.ID
		},
		Filters: map[string]func(*ui.FirewallZone) string{
			"name": func(s *ui.FirewallZone) string { return s.Name },
		},
	}
}

// firewallZoneKitBackend binds the spec to a client.
//
// THE HAND-WRITTEN RESOURCE USED THE WHOLE-OBJECT UpdateFirewallZone; this uses
// the masked UpdateFirewallZoneFields. That is not a translation but a
// narrowing: the kit sends only the fields the plan set, so the three computed
// attributes this surface has -- _id, zone_key, default_zone -- are never
// offered back to the controller that authored them.
func firewallZoneKitBackend(client *ui.ApiClient) resourcekit.Backend[ui.FirewallZone] {
	return resourcekit.Backend[ui.FirewallZone]{
		Create: func(ctx context.Context, site string, in *ui.FirewallZone) (*ui.FirewallZone, error) {
			return client.CreateFirewallZone(ctx, site, in)
		},
		Read: func(ctx context.Context, site, id string) (*ui.FirewallZone, error) {
			return client.GetFirewallZone(ctx, site, id)
		},
		UpdateFields: func(ctx context.Context, site string, in *ui.FirewallZone, fields ...string) (*ui.FirewallZone, error) {
			return client.UpdateFirewallZoneFields(ctx, site, in, fields...)
		},
		Delete: func(ctx context.Context, site, id string) error {
			return client.DeleteFirewallZone(ctx, site, id)
		},
		List: func(ctx context.Context, site string) ([]ui.FirewallZone, error) {
			return client.ListFirewallZone(ctx, site)
		},
		GetID: func(s *ui.FirewallZone) string { return s.ID },
		SetID: func(s *ui.FirewallZone, id string) { s.ID = id },
	}
}
