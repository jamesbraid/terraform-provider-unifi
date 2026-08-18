package unifi

// The firewall_group descriptor.
//
// THE FIRST SURFACE WHERE WIRE AND TERRAFORM NAMES DIVERGE ON MORE THAN ONE
// FIELD, which is what the Wire/Model split is for: the controller calls them
// group_members and group_type, the practitioner writes members and type. The
// field-masked update puts the WIRE name on the wire, so a descriptor keyed on
// either name alone gets one direction wrong.

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/types"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/listresource_firewall_group"
	resource_firewall_group "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_firewall_group"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/resourcekit"
)

type firewallGroupKitModel struct {
	ID       types.String   `tfsdk:"id"`
	Site     types.String   `tfsdk:"site"`
	Name     types.String   `tfsdk:"name"`
	Type     types.String   `tfsdk:"type"`
	Members  types.Set      `tfsdk:"members"`
	Timeouts timeouts.Value `tfsdk:"timeouts"`
}

// firewallGroupKitSpec is the whole of what varies.
//
// ALL THREE FIELDS ARE Required, SO ALL THREE ARE KeepZero. members is the
// interesting one: the hand-written code nulled the set when the API returned
// no members, and a test named "empty members produces null set" asserted it.
// That is the record_type defect in another costume -- a Required attribute
// whose state goes null while its config holds a value is an
// inconsistent-result-after-apply waiting for a controller that returns an
// empty list. `members = []` is a legal configuration, so the trigger is
// reachable rather than hypothetical. The behaviour changes here deliberately,
// and the test changes with it rather than being deleted.
//
// THE MASKED UPDATE SUPERSEDES THIS SURFACE'S READ-MODIFY-WRITE. The
// hand-written Update read the current group, overlaid the planned fields and
// PUT the whole object back, preserving server-managed fields by carrying them
// through. Naming only the fields the plan set preserves them by never sending
// them, and closes the window between the GET and the PUT in which a concurrent
// change is read and written back stale.
func firewallGroupKitSpec() resourcekit.Spec[firewallGroupKitModel, ui.FirewallGroup] {
	return resourcekit.Spec[firewallGroupKitModel, ui.FirewallGroup]{
		TypeName: "firewall_group",
		Subject:  "Firewall Group",
		New:      func() *ui.FirewallGroup { return &ui.FirewallGroup{} },
		ID:       func(m *firewallGroupKitModel) *types.String { return &m.ID },
		Site:     func(m *firewallGroupKitModel) *types.String { return &m.Site },
		Timeouts: func(m *firewallGroupKitModel) *timeouts.Value { return &m.Timeouts },
		Fields: []resourcekit.Field[firewallGroupKitModel, ui.FirewallGroup]{
			resourcekit.StringField[firewallGroupKitModel, ui.FirewallGroup]{
				Wire:  "name",
				Model: func(m *firewallGroupKitModel) *types.String { return &m.Name },
				SDK:   func(s *ui.FirewallGroup) *string { return &s.Name },
				Elide: resourcekit.KeepZero,
			},
			// group_type on the wire, type in the configuration.
			resourcekit.StringField[firewallGroupKitModel, ui.FirewallGroup]{
				Wire:  "group_type",
				Model: func(m *firewallGroupKitModel) *types.String { return &m.Type },
				SDK:   func(s *ui.FirewallGroup) *string { return &s.GroupType },
				Elide: resourcekit.KeepZero,
			},
			// group_members on the wire, members in the configuration. A set,
			// because membership is unordered: a controller returning the same
			// members in another sequence must not read as a change.
			resourcekit.StringSetField[firewallGroupKitModel, ui.FirewallGroup]{
				Wire:  "group_members",
				Model: func(m *firewallGroupKitModel) *types.Set { return &m.Members },
				SDK:   func(s *ui.FirewallGroup) *[]string { return &s.GroupMembers },
				Elide: resourcekit.KeepZero,
			},
		},
		Backend: resourcekit.Backend[ui.FirewallGroup]{
			GetID: func(s *ui.FirewallGroup) string { return s.ID },
			SetID: func(s *ui.FirewallGroup, id string) { s.ID = id },
		},
	}
}

// firewallGroupKitSchema is the schema half. No version and no upgraders: this
// surface has never migrated its state shape.
func firewallGroupKitSchema() resourcekit.SchemaSpec {
	return resourcekit.SchemaSpec{
		Resource: resource_firewall_group.FirewallGroupResourceSchema,
		Timeouts: timeouts.Opts{Create: true, Read: true, Update: true, Delete: true},
	}
}

func firewallGroupKitList() resourcekit.ListSpec[ui.FirewallGroup] {
	return resourcekit.ListSpec[ui.FirewallGroup]{
		ConfigSchema: listresource_firewall_group.FirewallGroupListResourceSchema,
		DisplayName: func(s *ui.FirewallGroup) string {
			if s.Name != "" {
				return s.Name
			}
			return s.ID
		},
		Filters: map[string]func(*ui.FirewallGroup) string{
			"name": func(s *ui.FirewallGroup) string { return s.Name },
			"type": func(s *ui.FirewallGroup) string { return s.GroupType },
		},
	}
}

func firewallGroupKitBackend(client *ui.ApiClient) resourcekit.Backend[ui.FirewallGroup] {
	return resourcekit.Backend[ui.FirewallGroup]{
		Create: func(ctx context.Context, site string, in *ui.FirewallGroup) (*ui.FirewallGroup, error) {
			return client.CreateFirewallGroup(ctx, site, in)
		},
		Read: func(ctx context.Context, site, id string) (*ui.FirewallGroup, error) {
			return client.GetFirewallGroup(ctx, site, id)
		},
		UpdateFields: func(ctx context.Context, site string, in *ui.FirewallGroup, fields ...string) (*ui.FirewallGroup, error) {
			return client.UpdateFirewallGroupFields(ctx, site, in, fields...)
		},
		Delete: func(ctx context.Context, site, id string) error {
			return client.DeleteFirewallGroup(ctx, site, id)
		},
		List: func(ctx context.Context, site string) ([]ui.FirewallGroup, error) {
			return client.ListFirewallGroup(ctx, site)
		},
		GetID: func(s *ui.FirewallGroup) string { return s.ID },
		SetID: func(s *ui.FirewallGroup, id string) { s.ID = id },
	}
}
