package unifi

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework-nettypes/hwtypes"
	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/listresource_firewall_rule"
	resource_firewall_rule "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_firewall_rule"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/resourcekit"
)

func firewallRuleKitSpec() resourcekit.Spec[firewallRuleKitModel, ui.FirewallRule] {
	return resourcekit.Spec[firewallRuleKitModel, ui.FirewallRule]{
		TypeName: "firewall_rule",
		Subject:  "Firewall Rule",
		New:      func() *ui.FirewallRule { return &ui.FirewallRule{} },
		ID:       func(m *firewallRuleKitModel) *types.String { return &m.ID },
		Site:     func(m *firewallRuleKitModel) *types.String { return &m.Site },
		Timeouts: func(m *firewallRuleKitModel) *timeouts.Value { return &m.Timeouts },
		Fields: resourcekit.Override(firewallRuleGenFields(), []resourcekit.Field[firewallRuleKitModel, ui.FirewallRule]{
			// NETv4 on an empty read, the capability static_route produced.
			// The schema also carries Default: "NETv4", but that fills a plan
			// when the config omits the attribute; this fills state when the
			// controller reports nothing, which is the refresh and import path
			// a schema default never reaches.
			resourcekit.StringField[firewallRuleKitModel, ui.FirewallRule]{
				Wire:        "src_networkconf_type",
				Model:       func(m *firewallRuleKitModel) *types.String { return &m.SrcNetworkType },
				SDK:         func(s *ui.FirewallRule) *string { return &s.SrcNetworkType },
				ReadDefault: "NETv4",
			},
			resourcekit.StringLikeField[firewallRuleKitModel, ui.FirewallRule, hwtypes.MACAddress]{
				Wire:  "src_mac_address",
				Model: func(m *firewallRuleKitModel) *hwtypes.MACAddress { return &m.SrcMAC },
				SDK:   func(s *ui.FirewallRule) *string { return &s.SrcMACAddress },
				New: func(v basetypes.StringValue) hwtypes.MACAddress {
					return hwtypes.MACAddress{StringValue: v}
				},
				Elide: resourcekit.NullZero,
			},
			resourcekit.StringField[firewallRuleKitModel, ui.FirewallRule]{
				Wire:        "dst_networkconf_type",
				Model:       func(m *firewallRuleKitModel) *types.String { return &m.DstNetworkType },
				SDK:         func(s *ui.FirewallRule) *string { return &s.DstNetworkType },
				ReadDefault: "NETv4",
			},
		}),
		// Seeded here as well as in firewallRuleKitBackend, because Configure binds
		// the real Backend and a unit test calling ToModel on an unconfigured
		// spec would otherwise dereference nil.
		Backend: resourcekit.Backend[ui.FirewallRule]{
			GetID: func(s *ui.FirewallRule) string { return s.ID },
			SetID: func(s *ui.FirewallRule, id string) { s.ID = id },
		},
	}
}

func firewallRuleKitSchema() resourcekit.SchemaSpec {
	return resourcekit.SchemaSpec{
		Resource: resource_firewall_rule.FirewallRuleResourceSchema,
		Timeouts: timeouts.Opts{Create: true, Read: true, Update: true, Delete: true},
	}
}

func firewallRuleKitList() resourcekit.ListSpec[ui.FirewallRule] {
	return resourcekit.ListSpec[ui.FirewallRule]{
		ConfigSchema: listresource_firewall_rule.FirewallRuleListResourceSchema,
		DisplayName: func(s *ui.FirewallRule) string {
			if s.Name != "" {
				return s.Name
			}
			return s.ID
		},
		Filters: map[string]func(*ui.FirewallRule) string{
			"name":    func(s *ui.FirewallRule) string { return s.Name },
			"ruleset": func(s *ui.FirewallRule) string { return s.Ruleset },
			"action":  func(s *ui.FirewallRule) string { return s.Action },
			"enabled": func(s *ui.FirewallRule) string { return fmt.Sprintf("%t", s.Enabled) },
		},
	}
}

func firewallRuleKitBackend(client *ui.ApiClient) resourcekit.Backend[ui.FirewallRule] {
	return resourcekit.Backend[ui.FirewallRule]{
		Create: func(ctx context.Context, site string, in *ui.FirewallRule) (*ui.FirewallRule, error) {
			return client.CreateFirewallRule(ctx, site, in)
		},
		Read: func(ctx context.Context, site, id string) (*ui.FirewallRule, error) {
			return client.GetFirewallRule(ctx, site, id)
		},
		UpdateFields: func(ctx context.Context, site string, in *ui.FirewallRule, fields ...string) (*ui.FirewallRule, error) {
			return client.UpdateFirewallRuleFields(ctx, site, in, fields...)
		},
		Delete: func(ctx context.Context, site, id string) error {
			return client.DeleteFirewallRule(ctx, site, id)
		},
		List: func(ctx context.Context, site string) ([]ui.FirewallRule, error) {
			return client.ListFirewallRule(ctx, site)
		},
		GetID: func(s *ui.FirewallRule) string { return s.ID },
		SetID: func(s *ui.FirewallRule, id string) { s.ID = id },
	}
}
