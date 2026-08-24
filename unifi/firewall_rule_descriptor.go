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

type firewallRuleKitModel struct {
	ID                  types.String       `tfsdk:"id"`
	Site                types.String       `tfsdk:"site"`
	Name                types.String       `tfsdk:"name"`
	Action              types.String       `tfsdk:"action"`
	Ruleset             types.String       `tfsdk:"ruleset"`
	RuleIndex           types.Int64        `tfsdk:"rule_index"`
	Protocol            types.String       `tfsdk:"protocol"`
	ProtocolV6          types.String       `tfsdk:"protocol_v6"`
	ICMPTypename        types.String       `tfsdk:"icmp_typename"`
	ICMPV6Typename      types.String       `tfsdk:"icmp_v6_typename"`
	Enabled             types.Bool         `tfsdk:"enabled"`
	SrcNetworkID        types.String       `tfsdk:"src_network_id"`
	SrcNetworkType      types.String       `tfsdk:"src_network_type"`
	SrcFirewallGroupIDs types.Set          `tfsdk:"src_firewall_group_ids"`
	SrcAddress          types.String       `tfsdk:"src_address"`
	SrcAddressIPv6      types.String       `tfsdk:"src_address_ipv6"`
	SrcPort             types.String       `tfsdk:"src_port"`
	SrcMac              hwtypes.MACAddress `tfsdk:"src_mac"`
	DstNetworkID        types.String       `tfsdk:"dst_network_id"`
	DstNetworkType      types.String       `tfsdk:"dst_network_type"`
	DstFirewallGroupIDs types.Set          `tfsdk:"dst_firewall_group_ids"`
	DstAddress          types.String       `tfsdk:"dst_address"`
	DstAddressIPv6      types.String       `tfsdk:"dst_address_ipv6"`
	DstPort             types.String       `tfsdk:"dst_port"`
	Logging             types.Bool         `tfsdk:"logging"`
	StateEstablished    types.Bool         `tfsdk:"state_established"`
	StateInvalid        types.Bool         `tfsdk:"state_invalid"`
	StateNew            types.Bool         `tfsdk:"state_new"`
	StateRelated        types.Bool         `tfsdk:"state_related"`
	IPSec               types.String       `tfsdk:"ip_sec"`
	SettingPreference   types.String       `tfsdk:"setting_preference"`
	ProtocolMatchExcept types.Bool         `tfsdk:"protocol_match_excepted"`
	Timeouts            timeouts.Value     `tfsdk:"timeouts"`
}

func firewallRuleKitSpec() resourcekit.Spec[firewallRuleKitModel, ui.FirewallRule] {
	str := func(
		wire string,
		model func(*firewallRuleKitModel) *types.String,
		sdk func(*ui.FirewallRule) *string,
		elide resourcekit.ElideZero,
	) resourcekit.StringField[firewallRuleKitModel, ui.FirewallRule] {
		return resourcekit.StringField[firewallRuleKitModel, ui.FirewallRule]{
			Wire: wire, Model: model, SDK: sdk, Elide: elide,
		}
	}
	boolean := func(
		wire string,
		model func(*firewallRuleKitModel) *types.Bool,
		sdk func(*ui.FirewallRule) *bool,
	) resourcekit.BoolField[firewallRuleKitModel, ui.FirewallRule] {
		return resourcekit.BoolField[firewallRuleKitModel, ui.FirewallRule]{
			Wire: wire, Model: model, SDK: sdk,
		}
	}

	return resourcekit.Spec[firewallRuleKitModel, ui.FirewallRule]{
		TypeName: "firewall_rule",
		Subject:  "Firewall Rule",
		New:      func() *ui.FirewallRule { return &ui.FirewallRule{} },
		ID:       func(m *firewallRuleKitModel) *types.String { return &m.ID },
		Site:     func(m *firewallRuleKitModel) *types.String { return &m.Site },
		Timeouts: func(m *firewallRuleKitModel) *timeouts.Value { return &m.Timeouts },
		Fields: []resourcekit.Field[firewallRuleKitModel, ui.FirewallRule]{
			str("name", func(m *firewallRuleKitModel) *types.String { return &m.Name },
				func(s *ui.FirewallRule) *string { return &s.Name }, resourcekit.KeepZero),
			str("action", func(m *firewallRuleKitModel) *types.String { return &m.Action },
				func(s *ui.FirewallRule) *string { return &s.Action }, resourcekit.KeepZero),
			str("ruleset", func(m *firewallRuleKitModel) *types.String { return &m.Ruleset },
				func(s *ui.FirewallRule) *string { return &s.Ruleset }, resourcekit.KeepZero),
			resourcekit.Int64PtrField[firewallRuleKitModel, ui.FirewallRule]{
				Wire:  "rule_index",
				Model: func(m *firewallRuleKitModel) *types.Int64 { return &m.RuleIndex },
				SDK:   func(s *ui.FirewallRule) **int64 { return &s.RuleIndex },
				Elide: resourcekit.KeepZero,
			},
			str("protocol", func(m *firewallRuleKitModel) *types.String { return &m.Protocol },
				func(s *ui.FirewallRule) *string { return &s.Protocol }, resourcekit.NullZero),
			str("protocol_v6", func(m *firewallRuleKitModel) *types.String { return &m.ProtocolV6 },
				func(s *ui.FirewallRule) *string { return &s.ProtocolV6 }, resourcekit.NullZero),
			str(
				"icmp_typename",
				func(m *firewallRuleKitModel) *types.String { return &m.ICMPTypename },
				func(s *ui.FirewallRule) *string { return &s.ICMPTypename },
				resourcekit.NullZero,
			),
			// The wire name is the SDK's, not Terraform's: icmp_v6_typename in
			// the schema is icmpv6_typename on the controller.
			str(
				"icmpv6_typename",
				func(m *firewallRuleKitModel) *types.String { return &m.ICMPV6Typename },
				func(s *ui.FirewallRule) *string { return &s.ICMPv6Typename },
				resourcekit.NullZero,
			),
			boolean("enabled", func(m *firewallRuleKitModel) *types.Bool { return &m.Enabled },
				func(s *ui.FirewallRule) *bool { return &s.Enabled }),

			str(
				"src_networkconf_id",
				func(m *firewallRuleKitModel) *types.String { return &m.SrcNetworkID },
				func(s *ui.FirewallRule) *string { return &s.SrcNetworkID },
				resourcekit.NullZero,
			),
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
			resourcekit.StringSetField[firewallRuleKitModel, ui.FirewallRule]{
				Wire:  "src_firewallgroup_ids",
				Model: func(m *firewallRuleKitModel) *types.Set { return &m.SrcFirewallGroupIDs },
				SDK:   func(s *ui.FirewallRule) *[]string { return &s.SrcFirewallGroupIDs },
				Elide: resourcekit.NullZero,
			},
			str("src_address", func(m *firewallRuleKitModel) *types.String { return &m.SrcAddress },
				func(s *ui.FirewallRule) *string { return &s.SrcAddress }, resourcekit.NullZero),
			str(
				"src_address_ipv6",
				func(m *firewallRuleKitModel) *types.String { return &m.SrcAddressIPv6 },
				func(s *ui.FirewallRule) *string { return &s.SrcAddressIPV6 },
				resourcekit.NullZero,
			),
			str("src_port", func(m *firewallRuleKitModel) *types.String { return &m.SrcPort },
				func(s *ui.FirewallRule) *string { return &s.SrcPort }, resourcekit.NullZero),
			resourcekit.StringLikeField[firewallRuleKitModel, ui.FirewallRule, hwtypes.MACAddress]{
				Wire:  "src_mac_address",
				Model: func(m *firewallRuleKitModel) *hwtypes.MACAddress { return &m.SrcMac },
				SDK:   func(s *ui.FirewallRule) *string { return &s.SrcMACAddress },
				New: func(v basetypes.StringValue) hwtypes.MACAddress {
					return hwtypes.MACAddress{StringValue: v}
				},
				Elide: resourcekit.NullZero,
			},

			str(
				"dst_networkconf_id",
				func(m *firewallRuleKitModel) *types.String { return &m.DstNetworkID },
				func(s *ui.FirewallRule) *string { return &s.DstNetworkID },
				resourcekit.NullZero,
			),
			resourcekit.StringField[firewallRuleKitModel, ui.FirewallRule]{
				Wire:        "dst_networkconf_type",
				Model:       func(m *firewallRuleKitModel) *types.String { return &m.DstNetworkType },
				SDK:         func(s *ui.FirewallRule) *string { return &s.DstNetworkType },
				ReadDefault: "NETv4",
			},
			resourcekit.StringSetField[firewallRuleKitModel, ui.FirewallRule]{
				Wire:  "dst_firewallgroup_ids",
				Model: func(m *firewallRuleKitModel) *types.Set { return &m.DstFirewallGroupIDs },
				SDK:   func(s *ui.FirewallRule) *[]string { return &s.DstFirewallGroupIDs },
				Elide: resourcekit.NullZero,
			},
			str("dst_address", func(m *firewallRuleKitModel) *types.String { return &m.DstAddress },
				func(s *ui.FirewallRule) *string { return &s.DstAddress }, resourcekit.NullZero),
			str(
				"dst_address_ipv6",
				func(m *firewallRuleKitModel) *types.String { return &m.DstAddressIPv6 },
				func(s *ui.FirewallRule) *string { return &s.DstAddressIPV6 },
				resourcekit.NullZero,
			),
			str("dst_port", func(m *firewallRuleKitModel) *types.String { return &m.DstPort },
				func(s *ui.FirewallRule) *string { return &s.DstPort }, resourcekit.NullZero),

			boolean("logging", func(m *firewallRuleKitModel) *types.Bool { return &m.Logging },
				func(s *ui.FirewallRule) *bool { return &s.Logging }),
			boolean(
				"state_established",
				func(m *firewallRuleKitModel) *types.Bool { return &m.StateEstablished },
				func(s *ui.FirewallRule) *bool { return &s.StateEstablished },
			),
			boolean(
				"state_invalid",
				func(m *firewallRuleKitModel) *types.Bool { return &m.StateInvalid },
				func(s *ui.FirewallRule) *bool { return &s.StateInvalid },
			),
			boolean("state_new", func(m *firewallRuleKitModel) *types.Bool { return &m.StateNew },
				func(s *ui.FirewallRule) *bool { return &s.StateNew }),
			boolean(
				"state_related",
				func(m *firewallRuleKitModel) *types.Bool { return &m.StateRelated },
				func(s *ui.FirewallRule) *bool { return &s.StateRelated },
			),

			str("ipsec", func(m *firewallRuleKitModel) *types.String { return &m.IPSec },
				func(s *ui.FirewallRule) *string { return &s.IPSec }, resourcekit.NullZero),
			// Optional+Computed and still NullZero: its schema says
			// OneOf("auto","manual"), so an empty is not a value the
			// attribute accepts, it's an absence.
			str(
				"setting_preference",
				func(m *firewallRuleKitModel) *types.String { return &m.SettingPreference },
				func(s *ui.FirewallRule) *string { return &s.SettingPreference },
				resourcekit.NullZero,
			),
			boolean(
				"protocol_match_excepted",
				func(m *firewallRuleKitModel) *types.Bool { return &m.ProtocolMatchExcept },
				func(s *ui.FirewallRule) *bool { return &s.ProtocolMatchExcepted },
			),
		},
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
