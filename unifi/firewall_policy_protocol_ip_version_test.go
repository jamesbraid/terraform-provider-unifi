package unifi

import (
	"context"
	"testing"

	fwresource "github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// TestFirewallPolicyProtocolMatrixMatchesTheMeasuredSets pins the three
// buckets to the matrix measured against UniFi Network 10.6.101,
// 2026-08-28: 23 universal names/numbers, 29 IPv4-only, 6 IPv6-only, plus
// the one name ("ipv6-icmp") the controller never accepts under any
// ip_version. A change to any set is a new measurement, not a refactor --
// this test fails loudly if one drifts.
func TestFirewallPolicyProtocolMatrixMatchesTheMeasuredSets(t *testing.T) {
	if got, want := len(firewallPolicyUniversalProtocols), 23; got != want {
		t.Errorf("universal set has %d entries, want %d", got, want)
	}
	if got, want := len(firewallPolicyIPv4OnlyProtocols), 29; got != want {
		t.Errorf("IPv4-only set has %d entries, want %d", got, want)
	}
	if got, want := len(firewallPolicyIPv6OnlyProtocols), 6; got != want {
		t.Errorf("IPv6-only set has %d entries, want %d", got, want)
	}

	for _, name := range []string{"all", "tcp", "udp", "tcp_udp", "6", "58", "ospf"} {
		if !firewallPolicyUniversalProtocols[name] {
			t.Errorf("%q missing from the universal set", name)
		}
	}
	for _, name := range []string{"icmp", "igmp", "ip"} {
		if !firewallPolicyIPv4OnlyProtocols[name] {
			t.Errorf("%q missing from the IPv4-only set", name)
		}
	}
	for _, name := range []string{"icmpv6", "ipv6", "ipv6-route"} {
		if !firewallPolicyIPv6OnlyProtocols[name] {
			t.Errorf("%q missing from the IPv6-only set", name)
		}
	}
	if firewallPolicyUniversalProtocols["ipv6-icmp"] ||
		firewallPolicyIPv4OnlyProtocols["ipv6-icmp"] ||
		firewallPolicyIPv6OnlyProtocols["ipv6-icmp"] {
		t.Error(`"ipv6-icmp" must not appear in any of the three sets -- ` +
			"it is measured unsupported under every ip_version")
	}
}

func Test_firewallPolicyKitResource_ConfigValidators(t *testing.T) {
	r := newFirewallPolicyKitResource()
	validators := r.ConfigValidators(context.Background())
	if len(validators) == 0 {
		t.Error("expected at least one config validator")
	}
}

func Test_firewallPolicyProtocolIPVersionConfigValidator_Description(t *testing.T) {
	v := &firewallPolicyProtocolIPVersionConfigValidator{}
	want := "protocol must be valid for the declared ip_version"
	if got := v.Description(context.Background()); got != want {
		t.Errorf("Description() = %q, want %q", got, want)
	}
}

func Test_firewallPolicyProtocolIPVersionConfigValidator_MarkdownDescription(t *testing.T) {
	v := &firewallPolicyProtocolIPVersionConfigValidator{}
	want := "protocol must be valid for the declared ip_version"
	if got := v.MarkdownDescription(context.Background()); got != want {
		t.Errorf("MarkdownDescription() = %q, want %q", got, want)
	}
}

// Test_firewallPolicyProtocolIPVersionConfigValidator_ValidateResource builds
// a real schema-backed config, the same shape
// Test_staticRouteIPVersionValidator_ValidateResource and
// Test_siteToSiteVPNRemoteSubnetsConfigValidator_ValidateResource use, and
// exercises both directions of every bucket in the matrix, including the
// numeric/name asymmetry ("58" vs "icmpv6") and the always-unsupported name.
func Test_firewallPolicyProtocolIPVersionConfigValidator_ValidateResource(t *testing.T) {
	ctx := context.Background()
	schemaResp := &fwresource.SchemaResponse{}
	newFirewallPolicyKitResource().Schema(ctx, fwresource.SchemaRequest{}, schemaResp)
	if schemaResp.Diagnostics.HasError() {
		t.Fatalf("build the schema: %v", schemaResp.Diagnostics)
	}

	endpoint := func(t *testing.T, zoneID string) types.Object {
		t.Helper()
		obj, diags := types.ObjectValueFrom(ctx, firewallPolicyEndpointModel{}.AttributeTypes(),
			firewallPolicyEndpointModel{
				ZoneID:             types.StringValue(zoneID),
				MatchingTarget:     types.StringValue("ANY"),
				MatchingTargetType: types.StringNull(),
				NetworkIDs:         types.ListNull(types.StringType),
				ClientMACs:         types.ListNull(types.StringType),
				IPs:                types.ListNull(types.StringType),
				WebDomains:         types.ListNull(types.StringType),
				Port:               types.StringNull(),
				PortGroupID:        types.StringNull(),
				IPGroupID:          types.StringNull(),
				PortMatchingType:   types.StringValue("ANY"),
			})
		if diags.HasError() {
			t.Fatalf("building an endpoint: %v", diags)
		}
		return obj
	}

	configFor := func(t *testing.T, protocol, ipVersion types.String) tfsdk.Config {
		t.Helper()
		model := firewallPolicyKitModel{
			ID:                  types.StringNull(),
			Site:                types.StringNull(),
			Name:                types.StringValue("probe"),
			Action:              types.StringValue("ALLOW"),
			Enabled:             types.BoolValue(true),
			Protocol:            protocol,
			Description:         types.StringNull(),
			Logging:             types.BoolValue(false),
			Index:               types.Int64Null(),
			CreateAllowRespond:  types.BoolValue(false),
			IPVersion:           ipVersion,
			ConnectionStateType: types.StringNull(),
			ConnectionStates:    types.ListNull(types.StringType),
			ICMPTypename:        types.StringNull(),
			ICMPV6Typename:      types.StringNull(),
			Schedule:            types.ObjectNull(firewallPolicyScheduleAttrTypes()),
			Source:              endpoint(t, "z1"),
			Destination:         endpoint(t, "z2"),
			Timeouts:            timeoutsNullValue(),
		}
		staging := tfsdk.State{Schema: schemaResp.Schema}
		if diags := staging.Set(ctx, model); diags.HasError() {
			t.Fatalf("set the config: %v", diags)
		}
		return tfsdk.Config{Schema: schemaResp.Schema, Raw: staging.Raw}
	}

	tests := []struct {
		name      string
		protocol  types.String
		ipVersion types.String
		wantError bool
	}{
		{"universal_under_ipv4", types.StringValue("tcp"), types.StringValue("IPV4"), false},
		{"universal_under_ipv6", types.StringValue("tcp"), types.StringValue("IPV6"), false},
		{"universal_under_both", types.StringValue("all"), types.StringValue("BOTH"), false},
		{"ipv4_only_under_ipv4", types.StringValue("icmp"), types.StringValue("IPV4"), false},
		{"ipv4_only_under_ipv6", types.StringValue("icmp"), types.StringValue("IPV6"), true},
		{"ipv4_only_under_both", types.StringValue("icmp"), types.StringValue("BOTH"), true},
		{"ipv6_only_under_ipv6", types.StringValue("icmpv6"), types.StringValue("IPV6"), false},
		{"ipv6_only_under_ipv4", types.StringValue("icmpv6"), types.StringValue("IPV4"), true},
		{"ipv6_only_under_both", types.StringValue("icmpv6"), types.StringValue("BOTH"), true},
		// The numeric/name asymmetry: "58" is icmpv6's protocol number, and
		// unlike the name, the number is accepted under IPV4.
		{"numeric_form_under_ipv4", types.StringValue("58"), types.StringValue("IPV4"), false},
		{"name_form_under_ipv4", types.StringValue("icmpv6"), types.StringValue("IPV4"), true},
		// Always unsupported, regardless of ip_version.
		{"never_under_ipv4", types.StringValue("ipv6-icmp"), types.StringValue("IPV4"), true},
		{"never_under_ipv6", types.StringValue("ipv6-icmp"), types.StringValue("IPV6"), true},
		{"never_under_both", types.StringValue("ipv6-icmp"), types.StringValue("BOTH"), true},
		// Unset ip_version resolves to the schema default, IPV4.
		{"unset_ip_version_with_ipv4_only", types.StringValue("icmp"), types.StringNull(), false},
		{"unset_ip_version_with_ipv6_only", types.StringValue("icmpv6"), types.StringNull(), true},
		// An unmeasured protocol name makes no claim either way.
		{"unmeasured_protocol", types.StringValue("not-a-real-protocol"), types.StringValue("IPV4"), false},
		// protocol left unset: nothing to validate yet.
		{"unset_protocol", types.StringNull(), types.StringValue("IPV6"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := &firewallPolicyProtocolIPVersionConfigValidator{}
			resp := &fwresource.ValidateConfigResponse{}
			v.ValidateResource(ctx, fwresource.ValidateConfigRequest{
				Config: configFor(t, tt.protocol, tt.ipVersion),
			}, resp)
			if got := resp.Diagnostics.HasError(); got != tt.wantError {
				t.Errorf("protocol=%v ip_version=%v: got error=%v, want %v (diags: %v)",
					tt.protocol, tt.ipVersion, got, tt.wantError, resp.Diagnostics)
			}
		})
	}
}
