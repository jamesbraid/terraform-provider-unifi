package unifi

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework-nettypes/hwtypes"
	fwlist "github.com/hashicorp/terraform-plugin-framework/list"
	fwresource "github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/querycheck"
	"github.com/hashicorp/terraform-plugin-testing/tfversion"
	"github.com/ubiquiti-community/go-unifi/unifi"
)

func TestAccFirewallRule_basic(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccFirewallRuleConfig_basic(),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet("unifi_firewall_rule.test", "id"),
					resource.TestCheckResourceAttr(
						"unifi_firewall_rule.test",
						"name",
						"tfacc-firewall-rule",
					),
					resource.TestCheckResourceAttr("unifi_firewall_rule.test", "action", "drop"),
					resource.TestCheckResourceAttr("unifi_firewall_rule.test", "ruleset", "LAN_IN"),
					resource.TestCheckResourceAttr(
						"unifi_firewall_rule.test",
						"rule_index",
						"2000",
					),
					resource.TestCheckResourceAttr("unifi_firewall_rule.test", "enabled", "true"),
					resource.TestCheckResourceAttr("unifi_firewall_rule.test", "logging", "false"),
					resource.TestCheckResourceAttr(
						"unifi_firewall_rule.test",
						"state_established",
						"false",
					),
					resource.TestCheckResourceAttr(
						"unifi_firewall_rule.test",
						"state_invalid",
						"false",
					),
					resource.TestCheckResourceAttr(
						"unifi_firewall_rule.test",
						"state_new",
						"false",
					),
					resource.TestCheckResourceAttr(
						"unifi_firewall_rule.test",
						"state_related",
						"false",
					),
				),
			},
			{
				ResourceName:      "unifi_firewall_rule.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

func TestAccFirewallRule_accept(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccFirewallRuleConfig_accept(),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet("unifi_firewall_rule.test", "id"),
					resource.TestCheckResourceAttr("unifi_firewall_rule.test", "action", "accept"),
					resource.TestCheckResourceAttr("unifi_firewall_rule.test", "ruleset", "WAN_IN"),
					resource.TestCheckResourceAttr(
						"unifi_firewall_rule.test",
						"rule_index",
						"2010",
					),
				),
			},
			{
				ResourceName:      "unifi_firewall_rule.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

func TestAccFirewallRule_reject(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccFirewallRuleConfig_reject(),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet("unifi_firewall_rule.test", "id"),
					resource.TestCheckResourceAttr("unifi_firewall_rule.test", "action", "reject"),
					resource.TestCheckResourceAttr("unifi_firewall_rule.test", "ruleset", "LAN_IN"),
					resource.TestCheckResourceAttr(
						"unifi_firewall_rule.test",
						"rule_index",
						"2020",
					),
				),
			},
		},
	})
}

func TestAccFirewallRule_disabled(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccFirewallRuleConfig_disabled(),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet("unifi_firewall_rule.test", "id"),
					resource.TestCheckResourceAttr("unifi_firewall_rule.test", "enabled", "false"),
				),
			},
			{
				ResourceName:      "unifi_firewall_rule.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

func TestAccFirewallRule_withLogging(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccFirewallRuleConfig_withLogging(),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet("unifi_firewall_rule.test", "id"),
					resource.TestCheckResourceAttr("unifi_firewall_rule.test", "logging", "true"),
				),
			},
			{
				ResourceName:      "unifi_firewall_rule.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

func TestAccFirewallRule_withStateMatching(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccFirewallRuleConfig_withStateMatching(),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet("unifi_firewall_rule.test", "id"),
					resource.TestCheckResourceAttr(
						"unifi_firewall_rule.test",
						"state_established",
						"true",
					),
					resource.TestCheckResourceAttr(
						"unifi_firewall_rule.test",
						"state_related",
						"true",
					),
					resource.TestCheckResourceAttr(
						"unifi_firewall_rule.test",
						"state_new",
						"false",
					),
					resource.TestCheckResourceAttr(
						"unifi_firewall_rule.test",
						"state_invalid",
						"false",
					),
				),
			},
			{
				ResourceName:      "unifi_firewall_rule.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

func TestAccFirewallRule_withProtocol(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccFirewallRuleConfig_withProtocol(),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet("unifi_firewall_rule.test", "id"),
					resource.TestCheckResourceAttr("unifi_firewall_rule.test", "protocol", "tcp"),
					resource.TestCheckResourceAttr("unifi_firewall_rule.test", "dst_port", "443"),
				),
			},
			{
				ResourceName:      "unifi_firewall_rule.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

func TestAccFirewallRule_withSrcAddress(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccFirewallRuleConfig_withSrcAddress(),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet("unifi_firewall_rule.test", "id"),
					resource.TestCheckResourceAttr(
						"unifi_firewall_rule.test",
						"src_address",
						"10.0.0.0/8",
					),
					resource.TestCheckResourceAttr(
						"unifi_firewall_rule.test",
						"src_network_type",
						"NETv4",
					),
				),
			},
			{
				ResourceName:      "unifi_firewall_rule.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

func TestAccFirewallRule_withDstAddress(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccFirewallRuleConfig_withDstAddress(),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet("unifi_firewall_rule.test", "id"),
					resource.TestCheckResourceAttr(
						"unifi_firewall_rule.test",
						"dst_address",
						"192.168.0.0/16",
					),
				),
			},
			{
				ResourceName:      "unifi_firewall_rule.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

func TestAccFirewallRule_withFirewallGroups(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccFirewallRuleConfig_withFirewallGroups(),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet("unifi_firewall_rule.test", "id"),
					resource.TestCheckResourceAttr(
						"unifi_firewall_rule.test",
						"src_firewall_group_ids.#",
						"1",
					),
					resource.TestCheckResourceAttr(
						"unifi_firewall_rule.test",
						"dst_firewall_group_ids.#",
						"1",
					),
				),
			},
			{
				ResourceName:      "unifi_firewall_rule.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

func TestAccFirewallRule_update(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccFirewallRuleConfig_basic(),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"unifi_firewall_rule.test",
						"name",
						"tfacc-firewall-rule",
					),
					resource.TestCheckResourceAttr("unifi_firewall_rule.test", "action", "drop"),
					resource.TestCheckResourceAttr("unifi_firewall_rule.test", "enabled", "true"),
				),
			},
			{
				Config: testAccFirewallRuleConfig_updated(),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"unifi_firewall_rule.test",
						"name",
						"tfacc-firewall-rule-updated",
					),
					resource.TestCheckResourceAttr("unifi_firewall_rule.test", "action", "accept"),
					resource.TestCheckResourceAttr("unifi_firewall_rule.test", "enabled", "false"),
					resource.TestCheckResourceAttr(
						"unifi_firewall_rule.test",
						"rule_index",
						"2001",
					),
				),
			},
		},
	})
}

func TestAccFirewallRule_highRuleIndex(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccFirewallRuleConfig_highRuleIndex(),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet("unifi_firewall_rule.test", "id"),
					resource.TestCheckResourceAttr(
						"unifi_firewall_rule.test",
						"rule_index",
						"4000",
					),
				),
			},
			{
				ResourceName:      "unifi_firewall_rule.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

func TestAccFirewallRule_guestRuleset(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccFirewallRuleConfig_guestRuleset(),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet("unifi_firewall_rule.test", "id"),
					resource.TestCheckResourceAttr(
						"unifi_firewall_rule.test",
						"ruleset",
						"GUEST_IN",
					),
				),
			},
		},
	})
}

func TestAccFirewallRule_withSrcMac(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccFirewallRuleConfig_withSrcMac(),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet("unifi_firewall_rule.test", "id"),
					resource.TestCheckResourceAttr(
						"unifi_firewall_rule.test",
						"src_mac",
						"00:11:22:33:44:55",
					),
					resource.TestCheckResourceAttr(
						"unifi_firewall_rule.test",
						"protocol_match_excepted",
						"true",
					),
				),
			},
			{
				ResourceName:      "unifi_firewall_rule.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

func testAccFirewallRuleConfig_withSrcMac() string {
	return `
resource "unifi_firewall_rule" "test" {
  name       = "tfacc-firewall-rule-src-mac"
  action     = "drop"
  ruleset    = "LAN_IN"
  rule_index = 2020

  protocol                = "tcp"
  protocol_match_excepted = true
  src_mac                 = "00:11:22:33:44:55"
}
`
}

func testAccFirewallRuleConfig_basic() string {
	return `
resource "unifi_firewall_rule" "test" {
  name       = "tfacc-firewall-rule"
  action     = "drop"
  ruleset    = "LAN_IN"
  rule_index = 2000

  logging           = false
  state_established = false
  state_invalid     = false
  state_new         = false
  state_related     = false
}
`
}

func testAccFirewallRuleConfig_accept() string {
	return `
resource "unifi_firewall_rule" "test" {
  name       = "tfacc-firewall-rule-accept"
  action     = "accept"
  ruleset    = "WAN_IN"
  rule_index = 2010

  logging           = false
  state_established = false
  state_invalid     = false
  state_new         = false
  state_related     = false
}
`
}

func testAccFirewallRuleConfig_reject() string {
	return `
resource "unifi_firewall_rule" "test" {
  name       = "tfacc-firewall-rule-reject"
  action     = "reject"
  ruleset    = "LAN_IN"
  rule_index = 2020

  logging           = false
  state_established = false
  state_invalid     = false
  state_new         = false
  state_related     = false
}
`
}

func testAccFirewallRuleConfig_disabled() string {
	return `
resource "unifi_firewall_rule" "test" {
  name       = "tfacc-firewall-rule-disabled"
  action     = "drop"
  ruleset    = "LAN_IN"
  rule_index = 2030
  enabled    = false

  logging           = false
  state_established = false
  state_invalid     = false
  state_new         = false
  state_related     = false
}
`
}

func testAccFirewallRuleConfig_withLogging() string {
	return `
resource "unifi_firewall_rule" "test" {
  name       = "tfacc-firewall-rule-logging"
  action     = "drop"
  ruleset    = "LAN_IN"
  rule_index = 2040
  logging    = true

  state_established = false
  state_invalid     = false
  state_new         = false
  state_related     = false
}
`
}

func testAccFirewallRuleConfig_withStateMatching() string {
	return `
resource "unifi_firewall_rule" "test" {
  name       = "tfacc-firewall-rule-state"
  action     = "accept"
  ruleset    = "WAN_IN"
  rule_index = 2050

  logging           = false
  state_established = true
  state_related     = true
  state_new         = false
  state_invalid     = false
}
`
}

func testAccFirewallRuleConfig_withProtocol() string {
	return `
resource "unifi_firewall_rule" "test" {
  name       = "tfacc-firewall-rule-proto"
  action     = "accept"
  ruleset    = "WAN_IN"
  rule_index = 2060
  protocol   = "tcp"
  dst_port   = "443"

  logging           = false
  state_established = false
  state_invalid     = false
  state_new         = false
  state_related     = false
}
`
}

func testAccFirewallRuleConfig_withSrcAddress() string {
	return `
resource "unifi_firewall_rule" "test" {
  name            = "tfacc-firewall-rule-src"
  action          = "drop"
  ruleset         = "LAN_IN"
  rule_index      = 2070
  src_address     = "10.0.0.0/8"
  src_network_type = "NETv4"

  logging           = false
  state_established = false
  state_invalid     = false
  state_new         = false
  state_related     = false
}
`
}

func testAccFirewallRuleConfig_withDstAddress() string {
	return `
resource "unifi_firewall_rule" "test" {
  name       = "tfacc-firewall-rule-dst"
  action     = "drop"
  ruleset    = "LAN_IN"
  rule_index = 2080
  dst_address = "192.168.0.0/16"

  logging           = false
  state_established = false
  state_invalid     = false
  state_new         = false
  state_related     = false
}
`
}

func testAccFirewallRuleConfig_withFirewallGroups() string {
	return `
resource "unifi_firewall_group" "src" {
  name    = "tfacc-fwrule-src-group"
  type    = "address-group"
  members = ["10.0.0.1"]
}

resource "unifi_firewall_group" "dst" {
  name    = "tfacc-fwrule-dst-group"
  type    = "address-group"
  members = ["192.168.1.1"]
}

resource "unifi_firewall_rule" "test" {
  name       = "tfacc-firewall-rule-groups"
  action     = "drop"
  ruleset    = "LAN_IN"
  rule_index = 2090

  src_firewall_group_ids = [unifi_firewall_group.src.id]
  dst_firewall_group_ids = [unifi_firewall_group.dst.id]

  logging           = false
  state_established = false
  state_invalid     = false
  state_new         = false
  state_related     = false
}
`
}

func testAccFirewallRuleConfig_updated() string {
	return `
resource "unifi_firewall_rule" "test" {
  name       = "tfacc-firewall-rule-updated"
  action     = "accept"
  ruleset    = "LAN_IN"
  rule_index = 2001
  enabled    = false

  logging           = false
  state_established = false
  state_invalid     = false
  state_new         = false
  state_related     = false
}
`
}

func testAccFirewallRuleConfig_highRuleIndex() string {
	return `
resource "unifi_firewall_rule" "test" {
  name       = "tfacc-firewall-rule-high-idx"
  action     = "drop"
  ruleset    = "LAN_IN"
  rule_index = 4000

  logging           = false
  state_established = false
  state_invalid     = false
  state_new         = false
  state_related     = false
}
`
}

func testAccFirewallRuleConfig_guestRuleset() string {
	return `
resource "unifi_firewall_rule" "test" {
  name       = "tfacc-firewall-rule-guest"
  action     = "drop"
  ruleset    = "GUEST_IN"
  rule_index = 2000

  logging           = false
  state_established = false
  state_invalid     = false
  state_new         = false
  state_related     = false
}
`
}

func TestNewFirewallRuleResource(t *testing.T) {
	r := NewFirewallRuleResource()
	if r == nil {
		t.Fatal("NewFirewallRuleResource() returned nil")
	}
	if _, ok := r.(fwresource.ResourceWithImportState); !ok {
		t.Error("resource does not implement ResourceWithImportState")
	}
	if _, ok := r.(fwresource.ResourceWithIdentity); !ok {
		t.Error("resource does not implement ResourceWithIdentity")
	}
}

func TestNewFirewallRuleListResource(t *testing.T) {
	r := NewFirewallRuleListResource()
	if r == nil {
		t.Fatal("NewFirewallRuleListResource() returned nil")
	}
}

func Test_firewallRuleResource_IdentitySchema(t *testing.T) {
	type args struct {
		in0  context.Context
		in1  fwresource.IdentitySchemaRequest
		resp *fwresource.IdentitySchemaResponse
	}
	tests := []struct {
		name string
		r    *firewallRuleKitResource
		args args
	}{
		{
			name: "id attribute exists",
			r:    newFirewallRuleKitResource(),
			args: args{
				in0:  context.Background(),
				in1:  fwresource.IdentitySchemaRequest{},
				resp: &fwresource.IdentitySchemaResponse{},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.r.IdentitySchema(tt.args.in0, tt.args.in1, tt.args.resp)
			if _, ok := tt.args.resp.IdentitySchema.Attributes["id"]; !ok {
				t.Error("IdentitySchema missing 'id' attribute")
			}
		})
	}
}

func Test_firewallRuleResource_Schema(t *testing.T) {
	type args struct {
		ctx  context.Context
		req  fwresource.SchemaRequest
		resp *fwresource.SchemaResponse
	}
	tests := []struct {
		name string
		r    *firewallRuleKitResource
		args args
	}{
		{
			name: "key attributes exist with correct configurability",
			r:    newFirewallRuleKitResource(),
			args: args{
				ctx:  context.Background(),
				req:  fwresource.SchemaRequest{},
				resp: &fwresource.SchemaResponse{},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.r.Schema(tt.args.ctx, tt.args.req, tt.args.resp)
			s := tt.args.resp.Schema

			checks := []struct {
				attr     string
				required bool
				optional bool
				computed bool
			}{
				{"id", false, false, true},
				{"name", true, false, false},
				{"action", true, false, false},
				{"ruleset", true, false, false},
				{"rule_index", true, false, false},
				{"enabled", false, true, true},
			}
			for _, c := range checks {
				a, ok := s.Attributes[c.attr]
				if !ok {
					t.Errorf("missing attribute %q", c.attr)
					continue
				}
				if a.IsRequired() != c.required {
					t.Errorf("%s: Required = %v, want %v", c.attr, a.IsRequired(), c.required)
				}
				if a.IsOptional() != c.optional {
					t.Errorf("%s: Optional = %v, want %v", c.attr, a.IsOptional(), c.optional)
				}
				if a.IsComputed() != c.computed {
					t.Errorf("%s: Computed = %v, want %v", c.attr, a.IsComputed(), c.computed)
				}
			}
		})
	}
}

func Test_firewallRule_specApplyPlanToState(t *testing.T) {
	type args struct {
		in0   context.Context
		plan  *firewallRuleKitModel
		state *firewallRuleKitModel
	}
	tests := []struct {
		name string
		r    *firewallRuleKitResource
		args args
	}{
		{
			name: "non-null plan fields overwrite state, null fields preserve state",
			r:    newFirewallRuleKitResource(),
			args: args{
				in0: context.Background(),
				plan: &firewallRuleKitModel{
					Name:     types.StringValue("new"),
					Protocol: types.StringNull(),
				},
				state: &firewallRuleKitModel{
					Name:     types.StringValue("old"),
					Protocol: types.StringValue("tcp"),
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			firewallRuleKitSpec().ApplyPlanToState(tt.args.plan, tt.args.state)
			if tt.args.state.Name.ValueString() != "new" {
				t.Errorf("Name = %q, want %q", tt.args.state.Name.ValueString(), "new")
			}
			if tt.args.state.Protocol.ValueString() != "tcp" {
				t.Errorf(
					"Protocol = %q, want %q (should be preserved)",
					tt.args.state.Protocol.ValueString(),
					"tcp",
				)
			}
		})
	}
}

func Test_firewallRule_specToSDK(t *testing.T) {
	type args struct {
		ctx   context.Context
		model *firewallRuleKitModel
	}
	ruleIndex2000 := int64(2000)
	ruleIndex3000 := int64(3000)
	tests := []struct {
		name string
		r    *firewallRuleKitResource
		args args
		want *unifi.FirewallRule
	}{
		{
			name: "basic fields",
			r:    newFirewallRuleKitResource(),
			args: args{
				ctx: context.Background(),
				model: &firewallRuleKitModel{
					Name:                types.StringValue("drop-rule"),
					Action:              types.StringValue("drop"),
					Ruleset:             types.StringValue("LAN_IN"),
					RuleIndex:           types.Int64Value(2000),
					Enabled:             types.BoolValue(true),
					Protocol:            types.StringNull(),
					ProtocolV6:          types.StringNull(),
					ICMPTypename:        types.StringNull(),
					ICMPV6Typename:      types.StringNull(),
					SrcNetworkID:        types.StringNull(),
					SrcNetworkType:      types.StringNull(),
					SrcFirewallGroupIDs: types.SetNull(types.StringType),
					SrcAddress:          types.StringNull(),
					SrcAddressIPv6:      types.StringNull(),
					SrcPort:             types.StringNull(),
					SrcMac:              hwtypes.NewMACAddressNull(),
					DstNetworkID:        types.StringNull(),
					DstNetworkType:      types.StringNull(),
					DstFirewallGroupIDs: types.SetNull(types.StringType),
					DstAddress:          types.StringNull(),
					DstAddressIPv6:      types.StringNull(),
					DstPort:             types.StringNull(),
					Logging:             types.BoolNull(),
					StateEstablished:    types.BoolNull(),
					StateInvalid:        types.BoolNull(),
					StateNew:            types.BoolNull(),
					StateRelated:        types.BoolNull(),
					IPSec:               types.StringNull(),
					SettingPreference:   types.StringNull(),
					ProtocolMatchExcept: types.BoolValue(false),
				},
			},
			want: &unifi.FirewallRule{
				Name:      "drop-rule",
				Action:    "drop",
				Ruleset:   "LAN_IN",
				RuleIndex: &ruleIndex2000,
				Enabled:   true,
			},
		},
		{
			name: "with protocol src dst",
			r:    newFirewallRuleKitResource(),
			args: args{
				ctx: context.Background(),
				model: &firewallRuleKitModel{
					Name:                types.StringValue("allow-https"),
					Action:              types.StringValue("accept"),
					Ruleset:             types.StringValue("WAN_IN"),
					RuleIndex:           types.Int64Value(3000),
					Enabled:             types.BoolValue(true),
					Protocol:            types.StringValue("tcp"),
					ProtocolV6:          types.StringNull(),
					ICMPTypename:        types.StringNull(),
					ICMPV6Typename:      types.StringNull(),
					SrcNetworkID:        types.StringNull(),
					SrcNetworkType:      types.StringNull(),
					SrcFirewallGroupIDs: types.SetNull(types.StringType),
					SrcAddress:          types.StringValue("10.0.0.1"),
					SrcAddressIPv6:      types.StringNull(),
					SrcPort:             types.StringNull(),
					SrcMac:              hwtypes.NewMACAddressNull(),
					DstNetworkID:        types.StringNull(),
					DstNetworkType:      types.StringNull(),
					DstFirewallGroupIDs: types.SetNull(types.StringType),
					DstAddress:          types.StringNull(),
					DstAddressIPv6:      types.StringNull(),
					DstPort:             types.StringValue("443"),
					Logging:             types.BoolNull(),
					StateEstablished:    types.BoolNull(),
					StateInvalid:        types.BoolNull(),
					StateNew:            types.BoolNull(),
					StateRelated:        types.BoolNull(),
					IPSec:               types.StringNull(),
					SettingPreference:   types.StringNull(),
					ProtocolMatchExcept: types.BoolValue(false),
				},
			},
			want: &unifi.FirewallRule{
				Name:       "allow-https",
				Action:     "accept",
				Ruleset:    "WAN_IN",
				RuleIndex:  &ruleIndex3000,
				Enabled:    true,
				Protocol:   "tcp",
				SrcAddress: "10.0.0.1",
				DstPort:    "443",
			},
		},
		{
			name: "minimal required fields only",
			r:    newFirewallRuleKitResource(),
			args: args{
				ctx: context.Background(),
				model: &firewallRuleKitModel{
					Name:                types.StringValue("min"),
					Action:              types.StringValue("drop"),
					Ruleset:             types.StringValue("LAN_IN"),
					RuleIndex:           types.Int64Value(2000),
					Enabled:             types.BoolValue(false),
					Protocol:            types.StringNull(),
					ProtocolV6:          types.StringNull(),
					ICMPTypename:        types.StringNull(),
					ICMPV6Typename:      types.StringNull(),
					SrcNetworkID:        types.StringNull(),
					SrcNetworkType:      types.StringNull(),
					SrcFirewallGroupIDs: types.SetNull(types.StringType),
					SrcAddress:          types.StringNull(),
					SrcAddressIPv6:      types.StringNull(),
					SrcPort:             types.StringNull(),
					SrcMac:              hwtypes.NewMACAddressNull(),
					DstNetworkID:        types.StringNull(),
					DstNetworkType:      types.StringNull(),
					DstFirewallGroupIDs: types.SetNull(types.StringType),
					DstAddress:          types.StringNull(),
					DstAddressIPv6:      types.StringNull(),
					DstPort:             types.StringNull(),
					Logging:             types.BoolNull(),
					StateEstablished:    types.BoolNull(),
					StateInvalid:        types.BoolNull(),
					StateNew:            types.BoolNull(),
					StateRelated:        types.BoolNull(),
					IPSec:               types.StringNull(),
					SettingPreference:   types.StringNull(),
					ProtocolMatchExcept: types.BoolNull(),
				},
			},
			want: &unifi.FirewallRule{
				Name:      "min",
				Action:    "drop",
				Ruleset:   "LAN_IN",
				RuleIndex: &ruleIndex2000,
				Enabled:   false,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, diags := firewallRuleKitSpec().ToSDK(tt.args.ctx, tt.args.model)
			if diags.HasError() {
				t.Fatalf("ToSDK: %v", diags)
			}
			// Compared as the JSON the controller receives, not as Go
			// structs: reflect.DeepEqual separates a nil slice from an
			// empty one, and both firewall group fields are tagged
			// omitempty so the two produce identical requests -- while it
			// also prints *int64 fields as addresses, so a genuine
			// rule_index mismatch and an irrelevant one look the same in
			// the failure. Marshalling asserts what the controller is
			// actually told.
			gotJSON, err := json.Marshal(got)
			if err != nil {
				t.Fatalf("marshalling the built object: %v", err)
			}
			wantJSON, err := json.Marshal(tt.want)
			if err != nil {
				t.Fatalf("marshalling the expectation: %v", err)
			}
			if string(gotJSON) != string(wantJSON) {
				t.Errorf("ToSDK() sends\n  %s\nwant\n  %s", gotJSON, wantJSON)
			}
		})
	}
}

func Test_firewallRule_specToModel(t *testing.T) {
	type args struct {
		ctx          context.Context
		firewallRule *unifi.FirewallRule
		model        *firewallRuleKitModel
		site         string
	}
	ruleIndex3000 := int64(3000)
	ruleIndex2000 := int64(2000)
	tests := []struct {
		name      string
		r         *firewallRuleKitResource
		args      args
		checkFunc func(t *testing.T, model *firewallRuleKitModel)
	}{
		{
			name: "basic API struct to model",
			r:    newFirewallRuleKitResource(),
			args: args{
				ctx: context.Background(),
				firewallRule: &unifi.FirewallRule{
					ID:        "r1",
					Name:      "test",
					Action:    "accept",
					Ruleset:   "WAN_IN",
					RuleIndex: &ruleIndex3000,
					Enabled:   true,
				},
				model: &firewallRuleKitModel{},
				site:  "default",
			},
			checkFunc: func(t *testing.T, m *firewallRuleKitModel) {
				if m.ID.ValueString() != "r1" {
					t.Errorf("ID = %q, want %q", m.ID.ValueString(), "r1")
				}
				if m.Name.ValueString() != "test" {
					t.Errorf("Name = %q, want %q", m.Name.ValueString(), "test")
				}
				if m.Action.ValueString() != "accept" {
					t.Errorf("Action = %q, want %q", m.Action.ValueString(), "accept")
				}
				if m.Ruleset.ValueString() != "WAN_IN" {
					t.Errorf("Ruleset = %q, want %q", m.Ruleset.ValueString(), "WAN_IN")
				}
				if m.RuleIndex.ValueInt64() != 3000 {
					t.Errorf("RuleIndex = %d, want %d", m.RuleIndex.ValueInt64(), 3000)
				}
				if m.Enabled.ValueBool() != true {
					t.Error("Enabled should be true")
				}
				if m.Site.ValueString() != "default" {
					t.Errorf("Site = %q, want %q", m.Site.ValueString(), "default")
				}
			},
		},
		{
			name: "empty optional fields become null",
			r:    newFirewallRuleKitResource(),
			args: args{
				ctx: context.Background(),
				firewallRule: &unifi.FirewallRule{
					ID:        "r2",
					Name:      "empty-opts",
					Action:    "drop",
					Ruleset:   "LAN_IN",
					RuleIndex: &ruleIndex2000,
				},
				model: &firewallRuleKitModel{},
				site:  "default",
			},
			checkFunc: func(t *testing.T, m *firewallRuleKitModel) {
				if !m.Protocol.IsNull() {
					t.Error("Protocol should be null")
				}
				if !m.SrcAddress.IsNull() {
					t.Error("SrcAddress should be null")
				}
				if !m.DstPort.IsNull() {
					t.Error("DstPort should be null")
				}
				if !m.IPSec.IsNull() {
					t.Error("IPSec should be null")
				}
				if !m.SrcFirewallGroupIDs.IsNull() {
					t.Error("SrcFirewallGroupIDs should be null")
				}
			},
		},
		{
			name: "SrcNetworkType defaults to NETv4 when empty",
			r:    newFirewallRuleKitResource(),
			args: args{
				ctx: context.Background(),
				firewallRule: &unifi.FirewallRule{
					ID:             "r3",
					Name:           "netv4-default",
					Action:         "drop",
					Ruleset:        "LAN_IN",
					RuleIndex:      &ruleIndex2000,
					SrcNetworkType: "",
				},
				model: &firewallRuleKitModel{},
				site:  "default",
			},
			checkFunc: func(t *testing.T, m *firewallRuleKitModel) {
				if m.SrcNetworkType.ValueString() != "NETv4" {
					t.Errorf(
						"SrcNetworkType = %q, want %q",
						m.SrcNetworkType.ValueString(),
						"NETv4",
					)
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if diags := firewallRuleKitSpec().ToModel(
				tt.args.ctx, tt.args.firewallRule, tt.args.model, tt.args.site); diags.HasError() {
				t.Fatalf("ToModel: %v", diags)
			}
			if tt.checkFunc != nil {
				tt.checkFunc(t, tt.args.model)
			}
		})
	}
}

func Test_firewallRuleResource_ListResourceConfigSchema(t *testing.T) {
	type args struct {
		in0  context.Context
		in1  fwlist.ListResourceSchemaRequest
		resp *fwlist.ListResourceSchemaResponse
	}
	tests := []struct {
		name string
		r    *firewallRuleKitResource
		args args
	}{
		{
			name: "schema has site attribute",
			r:    newFirewallRuleKitResource(),
			args: args{
				in0:  context.Background(),
				in1:  fwlist.ListResourceSchemaRequest{},
				resp: &fwlist.ListResourceSchemaResponse{},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.r.ListResourceConfigSchema(tt.args.in0, tt.args.in1, tt.args.resp)
			if _, ok := tt.args.resp.Schema.Attributes["site"]; !ok {
				t.Error("ListResourceConfigSchema missing 'site' attribute")
			}
		})
	}
}

func TestAccFirewallRuleList_basic(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		TerraformVersionChecks: []tfversion.TerraformVersionCheck{
			tfversion.SkipBelow(tfversion.Version1_14_0),
		},
		Steps: []resource.TestStep{
			{
				Config: testAccFirewallRuleConfig_basic(),
			},
			{
				Query: true,
				Config: `
					provider "unifi" {}
					list "unifi_firewall_rule" "test" {
						provider = unifi
						config {
							filter {
								name  = "name"
								value = "tfacc-firewall-rule"
						  }
					  }
					}
				`,
				QueryResultChecks: []querycheck.QueryResultCheck{
					querycheck.ExpectLengthAtLeast("unifi_firewall_rule.test", 1),
				},
			},
		},
	})
}
