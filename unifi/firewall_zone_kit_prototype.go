package unifi

// PROTOTYPE, NOT GENERATED YET, and the SECOND descriptor rather than a repeat
// of the first.
//
// firewall_zone was chosen over another simple surface for three reasons, each
// a thing dns_record cannot exercise: it is BOOTSTRAP-compiled, so go_name and
// pointer-ness exist for it and the contract check has real subjects; it has a
// LIST of strings and a POINTER bool, which are field kinds dns_record has no
// instance of; and three of its fields are computed, so it is the first
// descriptor that has anything to mark read-only.

import (
	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/types"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
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
