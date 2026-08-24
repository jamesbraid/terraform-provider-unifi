package unifi

import (
	"context"
	"fmt"

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

// firewallZoneKitSpec: every value is read off the mapping artifact joined to
// the bootstrap on structural_name, except the SDK method names.
//
// The three read-only fields are the point of this descriptor: _id, zone_key
// and default_zone are computed in the policy, and marking them ReadOnly is
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
		// The documented import handle also accepts "name=<zone name>" (and
		// "site:name=<zone name>"), for the built-in zones -- Hotspot, Internal,
		// ... -- whose id is controller-assigned and not known up front; see
		// Spec.Name and firewallZoneKitBackend's ReadByName.
		Name: func(m *firewallZoneKitModel) *types.String { return &m.Name },
		Fields: []resourcekit.Field[firewallZoneKitModel, ui.FirewallZone]{
			resourcekit.StringField[firewallZoneKitModel, ui.FirewallZone]{
				Wire:  "name",
				Model: func(m *firewallZoneKitModel) *types.String { return &m.Name },
				SDK:   func(s *ui.FirewallZone) *string { return &s.Name },
				Elide: resourcekit.KeepZero,
			},
			// The slice is emptied rather than left nil, inside the field kind:
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
			// Pointer bool, and the bootstrap is what says so: default_zone is
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

// firewallZoneKitBackend binds the spec to a client. The masked
// UpdateFirewallZoneFields sends only the fields the plan set, so the three
// computed attributes -- _id, zone_key, default_zone -- are never offered
// back to the controller that authored them.
func firewallZoneKitBackend(client *ui.ApiClient) resourcekit.Backend[ui.FirewallZone] {
	return resourcekit.Backend[ui.FirewallZone]{
		Create: func(ctx context.Context, site string, in *ui.FirewallZone) (*ui.FirewallZone, error) {
			return client.CreateFirewallZone(ctx, site, in)
		},
		Read: func(ctx context.Context, site, id string) (*ui.FirewallZone, error) {
			return client.GetFirewallZone(ctx, site, id)
		},
		// ReadByName resolves a name-based import: go-unifi has no
		// GetFirewallZoneByName (unlike GetNetworkByName/GetWLANByName), so
		// this scans ListFirewallZone instead, and refuses an ambiguous name
		// rather than returning whichever match came first.
		ReadByName: func(ctx context.Context, site, name string) (*ui.FirewallZone, error) {
			zones, err := client.ListFirewallZone(ctx, site)
			if err != nil {
				return nil, err
			}
			var found *ui.FirewallZone
			for i := range zones {
				if zones[i].Name != name {
					continue
				}
				if found != nil {
					return nil, fmt.Errorf(
						"multiple firewall zones named %q on site %q; import by id instead",
						name, site)
				}
				found = &zones[i]
			}
			if found == nil {
				return nil, &ui.NotFoundError{Type: "FirewallZone", Attr: "Name", Value: name}
			}
			return found, nil
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
