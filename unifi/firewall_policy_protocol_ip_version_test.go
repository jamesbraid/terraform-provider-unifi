package unifi

import (
	"context"
	"testing"

	fwresource "github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/firewallcapability"
)

// TestFirewallPolicyCapabilityMatrixIsDerivedAndSane checks the generated
// matrix (firewallcapability.Matrix, derived from the behaviour artifact)
// carries the controller's measured verdicts: a universal name accepted under
// every ip_version, an IPv4-only name refused under IPV6 and BOTH, icmpv6
// IPv6-only, the numeric/name asymmetry (58 accepted under IPV4 where the name
// icmpv6 is not), and BOTH behaving as the intersection. It also confirms the
// non-existent name "ipv6-icmp" has no entry -- the vocabulary validator, not
// this matrix, is what rejects a name the controller does not have.
func TestFirewallPolicyCapabilityMatrixIsDerivedAndSane(t *testing.T) {
	m := firewallcapability.Matrix
	if len(m) == 0 {
		t.Fatal("the generated capability matrix is empty")
	}
	cases := []struct {
		key  string
		want bool
	}{
		{"IPV4|tcp", true}, {"IPV6|tcp", true}, {"BOTH|tcp", true},
		{"IPV4|icmp", true}, {"IPV6|icmp", false}, {"BOTH|icmp", false},
		{"IPV6|icmpv6", true}, {"IPV4|icmpv6", false}, {"BOTH|icmpv6", false},
		{"IPV4|58", true}, // the numeric form escapes the ip_version gate the name is under
	}
	for _, c := range cases {
		got, measured := m[c.key]
		if !measured {
			t.Errorf("%q: absent from the matrix, expected a measured verdict", c.key)
			continue
		}
		if got != c.want {
			t.Errorf("%q = %v, want %v", c.key, got, c.want)
		}
	}
	for _, v := range []string{"IPV4", "IPV6", "BOTH"} {
		if _, present := m[v+"|ipv6-icmp"]; present {
			t.Errorf("%s|ipv6-icmp is in the matrix; that name does not exist on the controller", v)
		}
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
		// "ipv6-icmp" is not a controller protocol name (the name is icmpv6),
		// so the matrix has no row and this cross-field validator stays silent;
		// the protocol vocabulary validator rejects the name instead.
		{"nonexistent_name_under_ipv4", types.StringValue("ipv6-icmp"), types.StringValue("IPV4"), false},
		{"nonexistent_name_under_ipv6", types.StringValue("ipv6-icmp"), types.StringValue("IPV6"), false},
		{"nonexistent_name_under_both", types.StringValue("ipv6-icmp"), types.StringValue("BOTH"), false},
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
