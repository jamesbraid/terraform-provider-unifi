package unifi

import (
	"context"
	"reflect"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	fwlist "github.com/hashicorp/terraform-plugin-framework/list"
	fwresource "github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	"github.com/ubiquiti-community/go-unifi/unifi"
)

func ptrInt64(v int64) *int64 { return &v }

// TestFirewallPolicyEndpointSpecificPort is a unit round-trip for the SPECIFIC
// port match (#207): a `port` set on a firewall policy endpoint must reach the
// go-unifi source/destination struct. It guards the fix where the port value
// was previously unrepresentable and silently dropped.
//
// This is a unit test (model -> API conversion) rather than an acceptance test
// because exercising it end-to-end requires zone-based firewall and named
// firewall zones, which the dockerized acceptance controller does not provide.
func TestFirewallPolicyEndpointSpecificPort(t *testing.T) {
	ctx := context.Background()
	var diags diag.Diagnostics

	m := firewallPolicyEndpointModel{
		ZoneID:           types.StringValue("zone-1"),
		MatchingTarget:   types.StringValue("ANY"),
		NetworkIDs:       types.ListNull(types.StringType),
		ClientMACs:       types.ListNull(types.StringType),
		IPs:              types.ListNull(types.StringType),
		Port:             types.StringValue("443"),
		PortGroupID:      types.StringNull(),
		PortMatchingType: types.StringValue("SPECIFIC"),
	}

	src := endpointModelToSource(ctx, m, &diags)
	if diags.HasError() {
		t.Fatalf("source conversion errored: %v", diags)
	}
	if src.Port != "443" {
		t.Errorf("source Port = %q, want 443", src.Port)
	}
	if src.PortMatchingType != "SPECIFIC" {
		t.Errorf("source PortMatchingType = %q, want SPECIFIC", src.PortMatchingType)
	}

	m.Port = types.StringValue("8080")
	dst := endpointModelToDestination(ctx, m, &diags)
	if diags.HasError() {
		t.Fatalf("destination conversion errored: %v", diags)
	}
	if dst.Port != "8080" {
		t.Errorf("destination Port = %q, want 8080", dst.Port)
	}
	if dst.PortMatchingType != "SPECIFIC" {
		t.Errorf("destination PortMatchingType = %q, want SPECIFIC", dst.PortMatchingType)
	}
}

// TestFirewallPolicyPortStringHandling guards #288 and #286. A portless endpoint
// must serialize no port at all (an empty Go string the go-unifi marshaler drops)
// — not "0", which freezes the gateway firewall config (#288). A comma-separated
// list must survive (#286). On read, the controller's "" and the legacy "0" both
// map back to a null port so plans stay clean.
func TestFirewallPolicyPortStringHandling(t *testing.T) {
	ctx := context.Background()
	var diags diag.Diagnostics

	base := firewallPolicyEndpointModel{
		ZoneID:           types.StringValue("z1"),
		MatchingTarget:   types.StringValue("ANY"),
		NetworkIDs:       types.ListNull(types.StringType),
		ClientMACs:       types.ListNull(types.StringType),
		IPs:              types.ListNull(types.StringType),
		WebDomains:       types.ListNull(types.StringType),
		PortGroupID:      types.StringNull(),
		PortMatchingType: types.StringValue("ANY"),
	}

	// Portless endpoint: model -> API must produce an empty port (omitted, never "0").
	for _, port := range []types.String{types.StringNull(), types.StringValue("")} {
		m := base
		m.Port = port
		if got := endpointModelToSource(ctx, m, &diags).Port; got != "" {
			t.Errorf("portless source Port = %q, want empty", got)
		}
	}

	// Comma-separated list survives (#286).
	m := base
	m.Port = types.StringValue("80,443")
	m.PortMatchingType = types.StringValue("SPECIFIC")
	if got := endpointModelToDestination(ctx, m, &diags).Port; got != "80,443" {
		t.Errorf("multi-port destination Port = %q, want 80,443", got)
	}
	if diags.HasError() {
		t.Fatalf("conversion errored: %v", diags)
	}

	// API -> model: "" and the legacy "0" both become null; a real list survives.
	// PortMatchingType is SPECIFIC throughout: `port` is only owned (read back)
	// under that discriminator value (design doc §4.3); this test is about the
	// ""/"0"-vs-real-value null mapping, not the discriminator gating itself.
	cases := map[string]bool{"": true, "0": true, "161": false, "1812,1813": false}
	for apiPort, wantNull := range cases {
		got := apiSourceToEndpointModel(
			ctx,
			&unifi.FirewallPolicySource{
				ZoneID: "z1", MatchingTarget: "IP", PortMatchingType: "SPECIFIC", Port: apiPort,
			},
			&diags,
		)
		if got.Port.IsNull() != wantNull {
			t.Errorf("read port %q: IsNull = %v, want %v", apiPort, got.Port.IsNull(), wantNull)
		}
		if !wantNull && got.Port.ValueString() != apiPort {
			t.Errorf("read port %q: ValueString = %q", apiPort, got.Port.ValueString())
		}
	}
}

// TestFirewallPolicyPreservesFirmwareFields guards #220: the UCG Max firmware
// rejects a PUT that omits connection_state_type, icmp_typename, icmp_v6_typename
// or the source/destination matching_target_type. These fields are not
// user-settable, so the provider round-trips them through state. This test reads
// an API object into the model and converts it back, asserting nothing is dropped.
func TestFirewallPolicyPreservesFirmwareFields(t *testing.T) {
	ctx := context.Background()

	// A policy as the controller returns it, with all firmware-managed fields set.
	api := &unifi.FirewallPolicy{
		ID:                  "pol-1",
		Name:                "allow-vpn-to-nas-snmp",
		Action:              "ALLOW",
		Enabled:             true,
		Protocol:            "all",
		Version:             "BOTH",
		ConnectionStateType: "ALL",
		ICMPTypename:        "ANY",
		ICMPV6Typename:      "ANY",
		Source: &unifi.FirewallPolicySource{
			ZoneID:             "zone-vpn",
			MatchingTarget:     "IP",
			MatchingTargetType: "OBJECT",
		},
		Destination: &unifi.FirewallPolicyDestination{
			ZoneID:             "zone-internal",
			MatchingTarget:     "IP",
			MatchingTargetType: "OBJECT",
			PortMatchingType:   "SPECIFIC",
			Port:               "161",
		},
	}

	// Read API -> model (Read/Create response path).
	var model firewallPolicyModel
	if diags := firewallPolicyToModel(ctx, api, &model); diags.HasError() {
		t.Fatalf("firewallPolicyToModel errored: %v", diags)
	}
	if model.ConnectionStateType.ValueString() != "ALL" {
		t.Errorf("ConnectionStateType = %q, want ALL", model.ConnectionStateType.ValueString())
	}
	if model.ICMPTypename.ValueString() != "ANY" {
		t.Errorf("ICMPTypename = %q, want ANY", model.ICMPTypename.ValueString())
	}
	if model.ICMPV6Typename.ValueString() != "ANY" {
		t.Errorf("ICMPV6Typename = %q, want ANY", model.ICMPV6Typename.ValueString())
	}

	// Convert model -> API (Update PUT path) and assert the fields survive.
	out, diags := modelToFirewallPolicy(ctx, model)
	if diags.HasError() {
		t.Fatalf("modelToFirewallPolicy errored: %v", diags)
	}
	if out.ConnectionStateType != "ALL" {
		t.Errorf("PUT ConnectionStateType = %q, want ALL", out.ConnectionStateType)
	}
	if out.ICMPTypename != "ANY" {
		t.Errorf("PUT ICMPTypename = %q, want ANY", out.ICMPTypename)
	}
	if out.ICMPV6Typename != "ANY" {
		t.Errorf("PUT ICMPV6Typename = %q, want ANY", out.ICMPV6Typename)
	}
	if out.Source == nil || out.Source.MatchingTargetType != "OBJECT" {
		t.Errorf("PUT source MatchingTargetType not preserved: %+v", out.Source)
	}
	if out.Destination == nil || out.Destination.MatchingTargetType != "OBJECT" {
		t.Errorf("PUT destination MatchingTargetType not preserved: %+v", out.Destination)
	}
	if out.Destination == nil || out.Destination.Port != "161" {
		t.Errorf("PUT destination Port not preserved: %+v", out.Destination)
	}
}

// TestFirewallPolicyConnectionStatesRoundTrip guards #227: a policy whose
// connection_state_type is CUSTOM must round-trip its connection_states. The
// model->API conversion previously hard-coded an empty slice, so updates sent
// "connection_states": [] and the firmware rejected CUSTOM policies (HTTP 400).
func TestFirewallPolicyConnectionStatesRoundTrip(t *testing.T) {
	ctx := context.Background()
	fp := &unifi.FirewallPolicy{
		ID:                  "p1",
		Name:                "deny-vpn-to-lan",
		Action:              "BLOCK",
		Protocol:            "all",
		ConnectionStateType: "CUSTOM",
		ConnectionStates:    []string{"NEW", "ESTABLISHED"},
		Source: &unifi.FirewallPolicySource{
			ZoneID:           "z1",
			MatchingTarget:   "ANY",
			PortMatchingType: "ANY",
		},
		Destination: &unifi.FirewallPolicyDestination{
			ZoneID:           "z2",
			MatchingTarget:   "ANY",
			PortMatchingType: "ANY",
		},
	}

	var model firewallPolicyModel
	if d := firewallPolicyToModel(ctx, fp, &model); d.HasError() {
		t.Fatalf("firewallPolicyToModel: %v", d)
	}
	var states []string
	if d := model.ConnectionStates.ElementsAs(ctx, &states, false); d.HasError() {
		t.Fatalf("reading connection_states: %v", d)
	}
	if len(states) != 2 || states[0] != "NEW" || states[1] != "ESTABLISHED" {
		t.Errorf("read connection_states = %v, want [NEW ESTABLISHED]", states)
	}

	out, d := modelToFirewallPolicy(ctx, model)
	if d.HasError() {
		t.Fatalf("modelToFirewallPolicy: %v", d)
	}
	if len(out.ConnectionStates) != 2 || out.ConnectionStates[0] != "NEW" ||
		out.ConnectionStates[1] != "ESTABLISHED" {
		t.Errorf("PUT dropped connection_states: %v, want [NEW ESTABLISHED]", out.ConnectionStates)
	}
}

// TestFirewallPolicyEndpointListFieldsRoundTrip guards #242 and the wiring of the
// list-typed match fields. web_domains (FQDN matching, matching_target=WEB) is new;
// network_ids and client_macs were declared in the schema but never mapped to/from
// the API (model->API dropped them, API->model forced them to null). This asserts
// all three survive both conversion directions.
// Each selector is exercised under its own owning matching_target (WEB owns
// web_domains, NETWORK owns network_ids, CLIENT owns client_macs — design doc
// §4.3's ownership table). Asserting all three survive under one shared
// matching_target (as this test originally did, using WEB for all three) is
// itself the inconsistency the discriminator-gating fix (PR-C Task 1)
// resolves: only the active matching_target's own selector is sent/read, so
// the fixture must match that, not the production code.
func TestFirewallPolicyEndpointListFieldsRoundTrip(t *testing.T) {
	ctx := context.Background()
	var diags diag.Diagnostics

	webDomains, _ := types.ListValueFrom(
		ctx,
		types.StringType,
		[]string{"example.com", "ads.example.net"},
	)
	networkIDs, _ := types.ListValueFrom(ctx, types.StringType, []string{"net-1", "net-2"})
	clientMACs, _ := types.ListValueFrom(ctx, types.StringType, []string{"00:11:22:33:44:55"})

	webM := firewallPolicyEndpointModel{
		ZoneID:           types.StringValue("zone-1"),
		MatchingTarget:   types.StringValue("WEB"),
		NetworkIDs:       types.ListNull(types.StringType),
		ClientMACs:       types.ListNull(types.StringType),
		IPs:              types.ListNull(types.StringType),
		WebDomains:       webDomains,
		Port:             types.StringNull(),
		PortGroupID:      types.StringNull(),
		PortMatchingType: types.StringValue("ANY"),
	}
	networkM := webM
	networkM.MatchingTarget = types.StringValue("NETWORK")
	networkM.WebDomains = types.ListNull(types.StringType)
	networkM.NetworkIDs = networkIDs
	clientM := webM
	clientM.MatchingTarget = types.StringValue("CLIENT")
	clientM.WebDomains = types.ListNull(types.StringType)
	clientM.ClientMACs = clientMACs

	// model -> API (PUT path)
	webSrc := endpointModelToSource(ctx, webM, &diags)
	if diags.HasError() {
		t.Fatalf("source conversion errored: %v", diags)
	}
	if len(webSrc.WebDomains) != 2 || webSrc.WebDomains[0] != "example.com" {
		t.Errorf("source WebDomains = %v, want [example.com ads.example.net]", webSrc.WebDomains)
	}

	networkSrc := endpointModelToSource(ctx, networkM, &diags)
	if diags.HasError() {
		t.Fatalf("source conversion errored: %v", diags)
	}
	if len(networkSrc.NetworkIDs) != 2 || networkSrc.NetworkIDs[1] != "net-2" {
		t.Errorf("source NetworkIDs = %v, want [net-1 net-2]", networkSrc.NetworkIDs)
	}

	clientSrc := endpointModelToSource(ctx, clientM, &diags)
	if diags.HasError() {
		t.Fatalf("source conversion errored: %v", diags)
	}
	if len(clientSrc.ClientMACs) != 1 || clientSrc.ClientMACs[0] != "00:11:22:33:44:55" {
		t.Errorf("source ClientMACs = %v, want [00:11:22:33:44:55]", clientSrc.ClientMACs)
	}

	dst := endpointModelToDestination(ctx, webM, &diags)
	if diags.HasError() {
		t.Fatalf("destination conversion errored: %v", diags)
	}
	if len(dst.WebDomains) != 2 || dst.WebDomains[1] != "ads.example.net" {
		t.Errorf("destination WebDomains = %v, want [example.com ads.example.net]", dst.WebDomains)
	}

	// API -> model (read path)
	apiSrc := &unifi.FirewallPolicySource{
		ZoneID:         "zone-1",
		MatchingTarget: "WEB",
		WebDomains:     []string{"example.com"},
		NetworkIDs:     []string{"net-9"},             // inactive under WEB: must not be read back
		ClientMACs:     []string{"aa:bb:cc:dd:ee:ff"}, // inactive under WEB: must not be read back
	}
	got := apiSourceToEndpointModel(ctx, apiSrc, &diags)
	if diags.HasError() {
		t.Fatalf("apiSourceToEndpointModel errored: %v", diags)
	}
	var wd, nids, macs []string
	got.WebDomains.ElementsAs(ctx, &wd, false)
	got.NetworkIDs.ElementsAs(ctx, &nids, false)
	got.ClientMACs.ElementsAs(ctx, &macs, false)
	if len(wd) != 1 || wd[0] != "example.com" {
		t.Errorf("read WebDomains = %v, want [example.com]", wd)
	}
	if len(nids) != 0 {
		t.Errorf("read NetworkIDs = %v, want empty (inactive under WEB)", nids)
	}
	if len(macs) != 0 {
		t.Errorf("read ClientMACs = %v, want empty (inactive under WEB)", macs)
	}
}

// TestFirewallPolicyICMPProtocolRoundTrip guards #259: zone-based firewall ICMP
// policies (protocol "icmp"/"icmpv6") were rejected by the schema's OneOf
// validator even though the controller accepts and returns them. This asserts
// the protocol survives both conversion directions once the validator allows it.
func TestFirewallPolicyICMPProtocolRoundTrip(t *testing.T) {
	ctx := context.Background()
	for _, proto := range []string{"icmp", "icmpv6"} {
		fp := &unifi.FirewallPolicy{
			ID:       "p-icmp",
			Name:     "allow-internal-ping",
			Action:   "ALLOW",
			Protocol: proto,
			Source: &unifi.FirewallPolicySource{
				ZoneID:           "z1",
				MatchingTarget:   "IP",
				PortMatchingType: "ANY",
			},
			Destination: &unifi.FirewallPolicyDestination{
				ZoneID:           "z2",
				MatchingTarget:   "IP",
				PortMatchingType: "ANY",
			},
		}

		var model firewallPolicyModel
		if d := firewallPolicyToModel(ctx, fp, &model); d.HasError() {
			t.Fatalf("[%s] firewallPolicyToModel: %v", proto, d)
		}
		if model.Protocol.ValueString() != proto {
			t.Errorf("[%s] read Protocol = %q, want %q", proto, model.Protocol.ValueString(), proto)
		}

		out, d := modelToFirewallPolicy(ctx, model)
		if d.HasError() {
			t.Fatalf("[%s] modelToFirewallPolicy: %v", proto, d)
		}
		if out.Protocol != proto {
			t.Errorf("[%s] PUT dropped Protocol = %q, want %q", proto, out.Protocol, proto)
		}
	}
}

func TestNewFirewallPolicyResource(t *testing.T) {
	got := NewFirewallPolicyResource()
	if got == nil {
		t.Fatal("NewFirewallPolicyResource() returned nil")
	}
	if _, ok := got.(fwresource.ResourceWithImportState); !ok {
		t.Errorf("NewFirewallPolicyResource() does not implement resource.ResourceWithImportState")
	}
	if _, ok := got.(fwresource.ResourceWithIdentity); !ok {
		t.Errorf("NewFirewallPolicyResource() does not implement resource.ResourceWithIdentity")
	}
}

func TestNewFirewallPolicyListResource(t *testing.T) {
	got := NewFirewallPolicyListResource()
	if got == nil {
		t.Fatal("NewFirewallPolicyListResource() returned nil")
	}
	if _, ok := got.(fwlist.ListResourceWithConfigure); !ok {
		t.Errorf(
			"NewFirewallPolicyListResource() does not implement list.ListResourceWithConfigure",
		)
	}
}

func Test_firewallPolicyEndpointModel_AttributeTypes(t *testing.T) {
	tests := []struct {
		name string
		m    firewallPolicyEndpointModel
		want map[string]attr.Type
	}{
		{
			name: "returns expected attribute types",
			m:    firewallPolicyEndpointModel{},
			want: map[string]attr.Type{
				"zone_id":              types.StringType,
				"matching_target":      types.StringType,
				"network_ids":          types.ListType{ElemType: types.StringType},
				"client_macs":          types.ListType{ElemType: types.StringType},
				"ips":                  types.ListType{ElemType: types.StringType},
				"web_domains":          types.ListType{ElemType: types.StringType},
				"port":                 types.StringType,
				"port_group_id":        types.StringType,
				"ip_group_id":          types.StringType,
				"port_matching_type":   types.StringType,
				"matching_target_type": types.StringType,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.m.AttributeTypes(); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("firewallPolicyEndpointModel.AttributeTypes() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_firewallPolicyResource_Metadata(t *testing.T) {
	type args struct {
		ctx  context.Context
		req  fwresource.MetadataRequest
		resp *fwresource.MetadataResponse
	}
	tests := []struct {
		name string
		r    *firewallPolicyResource
		args args
	}{
		{
			name: "type name is unifi_firewall_policy",
			r:    &firewallPolicyResource{},
			args: args{
				ctx:  context.Background(),
				req:  fwresource.MetadataRequest{ProviderTypeName: "unifi"},
				resp: &fwresource.MetadataResponse{},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.r.Metadata(tt.args.ctx, tt.args.req, tt.args.resp)
			if tt.args.resp.TypeName != "unifi_firewall_policy" {
				t.Errorf("TypeName = %q, want unifi_firewall_policy", tt.args.resp.TypeName)
			}
		})
	}
}

func Test_firewallPolicyResource_IdentitySchema(t *testing.T) {
	type args struct {
		in0  context.Context
		in1  fwresource.IdentitySchemaRequest
		resp *fwresource.IdentitySchemaResponse
	}
	tests := []struct {
		name string
		r    *firewallPolicyResource
		args args
	}{
		{
			name: "has id attribute",
			r:    &firewallPolicyResource{},
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

func Test_firewallPolicyResource_Schema(t *testing.T) {
	type args struct {
		ctx  context.Context
		req  fwresource.SchemaRequest
		resp *fwresource.SchemaResponse
	}
	tests := []struct {
		name string
		r    *firewallPolicyResource
		args args
	}{
		{
			name: "schema has key attributes",
			r:    &firewallPolicyResource{},
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
			for _, key := range []string{"id", "name", "action", "source", "destination"} {
				if _, ok := tt.args.resp.Schema.Attributes[key]; !ok {
					t.Errorf("Schema missing %q attribute", key)
				}
			}
		})
	}
}

// TestFirewallPolicyConnectionStatesSettable guards #351: connection_state_type
// and connection_states must be author-settable (Optional+Computed) so a policy
// can be scoped to NEW-only / RESPOND_ONLY connections, not just read back.
func TestFirewallPolicyConnectionStatesSettable(t *testing.T) {
	r := &firewallPolicyResource{}
	resp := &fwresource.SchemaResponse{}
	r.Schema(context.Background(), fwresource.SchemaRequest{}, resp)

	for _, key := range []string{"connection_state_type", "connection_states"} {
		attr, ok := resp.Schema.Attributes[key]
		if !ok {
			t.Fatalf("Schema missing %q attribute", key)
		}
		if !attr.IsOptional() {
			t.Errorf("%q must be Optional (author-settable), got Optional=false", key)
		}
		if !attr.IsComputed() {
			t.Errorf("%q must stay Computed (round-trip), got Computed=false", key)
		}
	}
}

func Test_firewallPolicyResource_Configure(t *testing.T) {
	type args struct {
		ctx  context.Context
		req  fwresource.ConfigureRequest
		resp *fwresource.ConfigureResponse
	}
	tests := []struct {
		name string
		r    *firewallPolicyResource
		args args
	}{
		{
			name: "nil provider data",
			r:    &firewallPolicyResource{},
			args: args{
				ctx:  context.Background(),
				req:  fwresource.ConfigureRequest{ProviderData: nil},
				resp: &fwresource.ConfigureResponse{},
			},
		},
		{
			name: "wrong type",
			r:    &firewallPolicyResource{},
			args: args{
				ctx:  context.Background(),
				req:  fwresource.ConfigureRequest{ProviderData: "wrong"},
				resp: &fwresource.ConfigureResponse{},
			},
		},
		{
			name: "correct client",
			r:    &firewallPolicyResource{},
			args: args{
				ctx:  context.Background(),
				req:  fwresource.ConfigureRequest{ProviderData: &Client{}},
				resp: &fwresource.ConfigureResponse{},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.r.Configure(tt.args.ctx, tt.args.req, tt.args.resp)
			switch tt.name {
			case "nil provider data":
				if tt.args.resp.Diagnostics.HasError() {
					t.Error("nil ProviderData should not error")
				}
			case "wrong type":
				if !tt.args.resp.Diagnostics.HasError() {
					t.Error("wrong type should produce an error")
				}
			case "correct client":
				if tt.args.resp.Diagnostics.HasError() {
					t.Errorf("correct client should not error: %v", tt.args.resp.Diagnostics)
				}
				if tt.r.client == nil {
					t.Error("client should be set after Configure")
				}
			}
		})
	}
}

func Test_modelToFirewallPolicy(t *testing.T) {
	type args struct {
		ctx   context.Context
		model firewallPolicyModel
	}
	tests := []struct {
		name  string
		args  args
		want  *unifi.FirewallPolicy
		want1 diag.Diagnostics
	}{
		{
			name: "basic allow-lan policy",
			args: args{
				ctx: context.Background(),
				model: func() firewallPolicyModel {
					ctx := context.Background()
					srcEndpoint := firewallPolicyEndpointModel{
						ZoneID:             types.StringValue("z1"),
						MatchingTarget:     types.StringValue("ANY"),
						MatchingTargetType: types.StringNull(),
						NetworkIDs:         types.ListNull(types.StringType),
						ClientMACs:         types.ListNull(types.StringType),
						IPs:                types.ListNull(types.StringType),
						WebDomains:         types.ListNull(types.StringType),
						Port:               types.StringNull(),
						PortGroupID:        types.StringNull(),
						PortMatchingType:   types.StringValue("ANY"),
					}
					srcObj, _ := types.ObjectValueFrom(
						ctx,
						firewallPolicyEndpointModel{}.AttributeTypes(),
						srcEndpoint,
					)
					dstEndpoint := firewallPolicyEndpointModel{
						ZoneID:             types.StringValue("z2"),
						MatchingTarget:     types.StringValue("ANY"),
						MatchingTargetType: types.StringNull(),
						NetworkIDs:         types.ListNull(types.StringType),
						ClientMACs:         types.ListNull(types.StringType),
						IPs:                types.ListNull(types.StringType),
						WebDomains:         types.ListNull(types.StringType),
						Port:               types.StringNull(),
						PortGroupID:        types.StringNull(),
						PortMatchingType:   types.StringValue("ANY"),
					}
					dstObj, _ := types.ObjectValueFrom(
						ctx,
						firewallPolicyEndpointModel{}.AttributeTypes(),
						dstEndpoint,
					)
					return firewallPolicyModel{
						Name:                types.StringValue("allow-lan"),
						Action:              types.StringValue("ALLOW"),
						Enabled:             types.BoolValue(true),
						Protocol:            types.StringValue("all"),
						Description:         types.StringNull(),
						Logging:             types.BoolValue(false),
						Index:               types.Int64Null(),
						CreateAllowRespond:  types.BoolValue(false),
						IPVersion:           types.StringNull(),
						ConnectionStateType: types.StringNull(),
						ConnectionStates:    types.ListNull(types.StringType),
						ICMPTypename:        types.StringNull(),
						ICMPV6Typename:      types.StringNull(),
						Source:              srcObj,
						Destination:         dstObj,
						ID:                  types.StringNull(),
						Site:                types.StringNull(),
					}
				}(),
			},
			want: &unifi.FirewallPolicy{
				Name:             "allow-lan",
				Action:           "ALLOW",
				Enabled:          true,
				Protocol:         "all",
				ConnectionStates: []string{},
				Schedule:         &unifi.FirewallPolicySchedule{Mode: "ALWAYS"},
				Source: &unifi.FirewallPolicySource{
					ZoneID:           "z1",
					MatchingTarget:   "ANY",
					PortMatchingType: "ANY",
				},
				Destination: &unifi.FirewallPolicyDestination{
					ZoneID:           "z2",
					MatchingTarget:   "ANY",
					PortMatchingType: "ANY",
				},
			},
			want1: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, got1 := modelToFirewallPolicy(tt.args.ctx, tt.args.model)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("modelToFirewallPolicy() got = %+v, want %+v", got, tt.want)
			}
			if !reflect.DeepEqual(got1, tt.want1) {
				t.Errorf("modelToFirewallPolicy() got1 = %v, want %v", got1, tt.want1)
			}
		})
	}
}

// index is controller-assigned and read-only (#348): even when the model carries a
// value, modelToFirewallPolicy must never put it on the API struct, otherwise the
// controller's append-at-end behaviour produces an inconsistent-result/perpetual diff.
func TestFirewallPolicyIndexNotSent(t *testing.T) {
	ctx := context.Background()
	endpoint := func(zone string) types.Object {
		obj, _ := types.ObjectValueFrom(ctx, firewallPolicyEndpointModel{}.AttributeTypes(),
			firewallPolicyEndpointModel{
				ZoneID:             types.StringValue(zone),
				MatchingTarget:     types.StringValue("ANY"),
				MatchingTargetType: types.StringNull(),
				NetworkIDs:         types.ListNull(types.StringType),
				ClientMACs:         types.ListNull(types.StringType),
				IPs:                types.ListNull(types.StringType),
				WebDomains:         types.ListNull(types.StringType),
				Port:               types.StringNull(),
				PortGroupID:        types.StringNull(),
				PortMatchingType:   types.StringValue("ANY"),
			})
		return obj
	}
	model := firewallPolicyModel{
		Name:                types.StringValue("allow-lan"),
		Action:              types.StringValue("ALLOW"),
		Enabled:             types.BoolValue(true),
		Protocol:            types.StringValue("all"),
		Description:         types.StringNull(),
		Logging:             types.BoolValue(false),
		Index:               types.Int64Value(10010), // pinned by user, must be ignored
		CreateAllowRespond:  types.BoolValue(false),
		IPVersion:           types.StringNull(),
		ConnectionStateType: types.StringNull(),
		ConnectionStates:    types.ListNull(types.StringType),
		ICMPTypename:        types.StringNull(),
		ICMPV6Typename:      types.StringNull(),
		Source:              endpoint("z1"),
		Destination:         endpoint("z2"),
		ID:                  types.StringNull(),
		Site:                types.StringNull(),
	}
	got, diags := modelToFirewallPolicy(ctx, model)
	if diags.HasError() {
		t.Fatalf("modelToFirewallPolicy() unexpected diagnostics: %v", diags)
	}
	if got.Index != nil {
		t.Errorf(
			"modelToFirewallPolicy() sent Index = %d, want nil (index is read-only)",
			*got.Index,
		)
	}
}

func Test_endpointModelToSource(t *testing.T) {
	type args struct {
		ctx   context.Context
		m     firewallPolicyEndpointModel
		diags *diag.Diagnostics
	}
	tests := []struct {
		name string
		args args
		want *unifi.FirewallPolicySource
	}{
		{
			name: "source with IP matching",
			args: args{
				ctx:   context.Background(),
				diags: &diag.Diagnostics{},
				m: func() firewallPolicyEndpointModel {
					ctx := context.Background()
					ips, _ := types.ListValueFrom(ctx, types.StringType, []string{"10.0.0.1"})
					return firewallPolicyEndpointModel{
						ZoneID:             types.StringValue("z1"),
						MatchingTarget:     types.StringValue("IP"),
						MatchingTargetType: types.StringValue("OBJECT"),
						NetworkIDs:         types.ListNull(types.StringType),
						ClientMACs:         types.ListNull(types.StringType),
						IPs:                ips,
						WebDomains:         types.ListNull(types.StringType),
						Port:               types.StringNull(),
						PortGroupID:        types.StringNull(),
						PortMatchingType:   types.StringValue("ANY"),
					}
				}(),
			},
			want: &unifi.FirewallPolicySource{
				ZoneID:             "z1",
				MatchingTarget:     "IP",
				MatchingTargetType: "OBJECT",
				PortMatchingType:   "ANY",
				IPs:                []string{"10.0.0.1"},
			},
		},
		{
			name: "source with IP group ref and empty type derives OBJECT",
			args: args{
				ctx:   context.Background(),
				diags: &diag.Diagnostics{},
				m: firewallPolicyEndpointModel{
					ZoneID:             types.StringValue("z1"),
					MatchingTarget:     types.StringValue("IP"),
					MatchingTargetType: types.StringNull(),
					NetworkIDs:         types.ListNull(types.StringType),
					ClientMACs:         types.ListNull(types.StringType),
					IPs:                types.ListNull(types.StringType),
					WebDomains:         types.ListNull(types.StringType),
					Port:               types.StringNull(),
					PortGroupID:        types.StringNull(),
					IPGroupID:          types.StringValue("689ff798c4b72577507ae001"),
					PortMatchingType:   types.StringValue("ANY"),
				},
			},
			want: &unifi.FirewallPolicySource{
				ZoneID:             "z1",
				MatchingTarget:     "IP",
				MatchingTargetType: "OBJECT",
				IPGroupID:          "689ff798c4b72577507ae001",
				PortMatchingType:   "ANY",
			},
		},
		{
			name: "source with literal ips and empty type derives SPECIFIC",
			args: args{
				ctx:   context.Background(),
				diags: &diag.Diagnostics{},
				m: func() firewallPolicyEndpointModel {
					ctx := context.Background()
					ips, _ := types.ListValueFrom(ctx, types.StringType, []string{"10.0.0.1"})
					return firewallPolicyEndpointModel{
						ZoneID:             types.StringValue("z1"),
						MatchingTarget:     types.StringValue("IP"),
						MatchingTargetType: types.StringNull(),
						NetworkIDs:         types.ListNull(types.StringType),
						ClientMACs:         types.ListNull(types.StringType),
						IPs:                ips,
						WebDomains:         types.ListNull(types.StringType),
						Port:               types.StringNull(),
						PortGroupID:        types.StringNull(),
						PortMatchingType:   types.StringValue("ANY"),
					}
				}(),
			},
			want: &unifi.FirewallPolicySource{
				ZoneID:             "z1",
				MatchingTarget:     "IP",
				MatchingTargetType: "SPECIFIC",
				PortMatchingType:   "ANY",
				IPs:                []string{"10.0.0.1"},
			},
		},
		{
			name: "source with IP group ref overrides stale SPECIFIC to OBJECT",
			args: args{
				ctx:   context.Background(),
				diags: &diag.Diagnostics{},
				m: firewallPolicyEndpointModel{
					ZoneID:             types.StringValue("z1"),
					MatchingTarget:     types.StringValue("IP"),
					MatchingTargetType: types.StringValue("SPECIFIC"),
					NetworkIDs:         types.ListNull(types.StringType),
					ClientMACs:         types.ListNull(types.StringType),
					IPs:                types.ListNull(types.StringType),
					WebDomains:         types.ListNull(types.StringType),
					Port:               types.StringNull(),
					PortGroupID:        types.StringNull(),
					IPGroupID:          types.StringValue("689ff798c4b72577507ae001"),
					PortMatchingType:   types.StringValue("ANY"),
				},
			},
			want: &unifi.FirewallPolicySource{
				ZoneID:             "z1",
				MatchingTarget:     "IP",
				MatchingTargetType: "OBJECT",
				IPGroupID:          "689ff798c4b72577507ae001",
				PortMatchingType:   "ANY",
			},
		},
		{
			name: "source preserves controller-assigned LIST",
			args: args{
				ctx:   context.Background(),
				diags: &diag.Diagnostics{},
				m: firewallPolicyEndpointModel{
					ZoneID:             types.StringValue("z1"),
					MatchingTarget:     types.StringValue("IP"),
					MatchingTargetType: types.StringValue("LIST"),
					NetworkIDs:         types.ListNull(types.StringType),
					ClientMACs:         types.ListNull(types.StringType),
					IPs:                types.ListNull(types.StringType),
					WebDomains:         types.ListNull(types.StringType),
					Port:               types.StringNull(),
					PortGroupID:        types.StringNull(),
					IPGroupID:          types.StringValue("689ff798c4b72577507ae001"),
					PortMatchingType:   types.StringValue("ANY"),
				},
			},
			want: &unifi.FirewallPolicySource{
				ZoneID:             "z1",
				MatchingTarget:     "IP",
				MatchingTargetType: "LIST",
				IPGroupID:          "689ff798c4b72577507ae001",
				PortMatchingType:   "ANY",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := endpointModelToSource(
				tt.args.ctx,
				tt.args.m,
				tt.args.diags,
			); !reflect.DeepEqual(
				got,
				tt.want,
			) {
				t.Errorf("endpointModelToSource() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func Test_endpointModelToDestination(t *testing.T) {
	type args struct {
		ctx   context.Context
		m     firewallPolicyEndpointModel
		diags *diag.Diagnostics
	}
	tests := []struct {
		name string
		args args
		want *unifi.FirewallPolicyDestination
	}{
		{
			name: "destination with IP matching",
			args: args{
				ctx:   context.Background(),
				diags: &diag.Diagnostics{},
				m: func() firewallPolicyEndpointModel {
					ctx := context.Background()
					ips, _ := types.ListValueFrom(ctx, types.StringType, []string{"192.168.1.1"})
					return firewallPolicyEndpointModel{
						ZoneID:             types.StringValue("z2"),
						MatchingTarget:     types.StringValue("IP"),
						MatchingTargetType: types.StringValue("OBJECT"),
						NetworkIDs:         types.ListNull(types.StringType),
						ClientMACs:         types.ListNull(types.StringType),
						IPs:                ips,
						WebDomains:         types.ListNull(types.StringType),
						Port:               types.StringValue("80"),
						PortGroupID:        types.StringNull(),
						PortMatchingType:   types.StringValue("SPECIFIC"),
					}
				}(),
			},
			want: &unifi.FirewallPolicyDestination{
				ZoneID:             "z2",
				MatchingTarget:     "IP",
				MatchingTargetType: "OBJECT",
				Port:               "80",
				PortMatchingType:   "SPECIFIC",
				IPs:                []string{"192.168.1.1"},
			},
		},
		{
			name: "destination with IP group ref and empty type derives OBJECT",
			args: args{
				ctx:   context.Background(),
				diags: &diag.Diagnostics{},
				m: firewallPolicyEndpointModel{
					ZoneID:             types.StringValue("z2"),
					MatchingTarget:     types.StringValue("IP"),
					MatchingTargetType: types.StringNull(),
					NetworkIDs:         types.ListNull(types.StringType),
					ClientMACs:         types.ListNull(types.StringType),
					IPs:                types.ListNull(types.StringType),
					WebDomains:         types.ListNull(types.StringType),
					Port:               types.StringNull(),
					PortGroupID:        types.StringNull(),
					IPGroupID:          types.StringValue("689ff798c4b72577507ae001"),
					PortMatchingType:   types.StringValue("ANY"),
				},
			},
			want: &unifi.FirewallPolicyDestination{
				ZoneID:             "z2",
				MatchingTarget:     "IP",
				MatchingTargetType: "OBJECT",
				IPGroupID:          "689ff798c4b72577507ae001",
				PortMatchingType:   "ANY",
			},
		},
		{
			name: "destination with literal ips and empty type derives SPECIFIC",
			args: args{
				ctx:   context.Background(),
				diags: &diag.Diagnostics{},
				m: func() firewallPolicyEndpointModel {
					ctx := context.Background()
					ips, _ := types.ListValueFrom(ctx, types.StringType, []string{"192.168.1.1"})
					return firewallPolicyEndpointModel{
						ZoneID:             types.StringValue("z2"),
						MatchingTarget:     types.StringValue("IP"),
						MatchingTargetType: types.StringNull(),
						NetworkIDs:         types.ListNull(types.StringType),
						ClientMACs:         types.ListNull(types.StringType),
						IPs:                ips,
						WebDomains:         types.ListNull(types.StringType),
						Port:               types.StringNull(),
						PortGroupID:        types.StringNull(),
						PortMatchingType:   types.StringValue("ANY"),
					}
				}(),
			},
			want: &unifi.FirewallPolicyDestination{
				ZoneID:             "z2",
				MatchingTarget:     "IP",
				MatchingTargetType: "SPECIFIC",
				PortMatchingType:   "ANY",
				IPs:                []string{"192.168.1.1"},
			},
		},
		{
			name: "destination with IP group ref overrides stale SPECIFIC to OBJECT",
			args: args{
				ctx:   context.Background(),
				diags: &diag.Diagnostics{},
				m: firewallPolicyEndpointModel{
					ZoneID:             types.StringValue("z2"),
					MatchingTarget:     types.StringValue("IP"),
					MatchingTargetType: types.StringValue("SPECIFIC"),
					NetworkIDs:         types.ListNull(types.StringType),
					ClientMACs:         types.ListNull(types.StringType),
					IPs:                types.ListNull(types.StringType),
					WebDomains:         types.ListNull(types.StringType),
					Port:               types.StringNull(),
					PortGroupID:        types.StringNull(),
					IPGroupID:          types.StringValue("689ff798c4b72577507ae001"),
					PortMatchingType:   types.StringValue("ANY"),
				},
			},
			want: &unifi.FirewallPolicyDestination{
				ZoneID:             "z2",
				MatchingTarget:     "IP",
				MatchingTargetType: "OBJECT",
				IPGroupID:          "689ff798c4b72577507ae001",
				PortMatchingType:   "ANY",
			},
		},
		{
			name: "destination preserves controller-assigned LIST",
			args: args{
				ctx:   context.Background(),
				diags: &diag.Diagnostics{},
				m: firewallPolicyEndpointModel{
					ZoneID:             types.StringValue("z2"),
					MatchingTarget:     types.StringValue("IP"),
					MatchingTargetType: types.StringValue("LIST"),
					NetworkIDs:         types.ListNull(types.StringType),
					ClientMACs:         types.ListNull(types.StringType),
					IPs:                types.ListNull(types.StringType),
					WebDomains:         types.ListNull(types.StringType),
					Port:               types.StringNull(),
					PortGroupID:        types.StringNull(),
					IPGroupID:          types.StringValue("689ff798c4b72577507ae001"),
					PortMatchingType:   types.StringValue("ANY"),
				},
			},
			want: &unifi.FirewallPolicyDestination{
				ZoneID:             "z2",
				MatchingTarget:     "IP",
				MatchingTargetType: "LIST",
				IPGroupID:          "689ff798c4b72577507ae001",
				PortMatchingType:   "ANY",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := endpointModelToDestination(
				tt.args.ctx,
				tt.args.m,
				tt.args.diags,
			); !reflect.DeepEqual(
				got,
				tt.want,
			) {
				t.Errorf("endpointModelToDestination() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func Test_firewallPolicyToModel(t *testing.T) {
	type args struct {
		ctx   context.Context
		fp    *unifi.FirewallPolicy
		model *firewallPolicyModel
	}
	tests := []struct {
		name string
		args args
		want diag.Diagnostics
	}{
		{
			name: "basic policy to model",
			args: args{
				ctx: context.Background(),
				fp: &unifi.FirewallPolicy{
					ID:       "pol-1",
					Name:     "test-policy",
					Action:   "ALLOW",
					Enabled:  true,
					Protocol: "all",
					Source: &unifi.FirewallPolicySource{
						ZoneID:           "z1",
						MatchingTarget:   "ANY",
						PortMatchingType: "ANY",
					},
					Destination: &unifi.FirewallPolicyDestination{
						ZoneID:           "z2",
						MatchingTarget:   "ANY",
						PortMatchingType: "ANY",
					},
				},
				model: &firewallPolicyModel{},
			},
			want: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := firewallPolicyToModel(
				tt.args.ctx,
				tt.args.fp,
				tt.args.model,
			); !reflect.DeepEqual(
				got,
				tt.want,
			) {
				t.Errorf("firewallPolicyToModel() = %v, want %v", got, tt.want)
			}
			if tt.args.model.Name.ValueString() != tt.args.fp.Name {
				t.Errorf(
					"model.Name = %q, want %q",
					tt.args.model.Name.ValueString(),
					tt.args.fp.Name,
				)
			}
			if tt.args.model.ID.ValueString() != tt.args.fp.ID {
				t.Errorf("model.ID = %q, want %q", tt.args.model.ID.ValueString(), tt.args.fp.ID)
			}
		})
	}
}

func Test_apiSourceToEndpointModel(t *testing.T) {
	type args struct {
		ctx   context.Context
		src   *unifi.FirewallPolicySource
		diags *diag.Diagnostics
	}
	tests := []struct {
		name string
		args args
		want firewallPolicyEndpointModel
	}{
		{
			name: "source with IP and port",
			args: args{
				ctx:   context.Background(),
				diags: &diag.Diagnostics{},
				src: &unifi.FirewallPolicySource{
					ZoneID:             "z1",
					MatchingTarget:     "IP",
					MatchingTargetType: "OBJECT",
					IPs:                []string{"10.0.0.1"},
					PortMatchingType:   "SPECIFIC",
					Port:               "443",
				},
			},
			want: func() firewallPolicyEndpointModel {
				ctx := context.Background()
				ips, _ := types.ListValueFrom(ctx, types.StringType, []string{"10.0.0.1"})
				networkIDs, _ := types.ListValueFrom(ctx, types.StringType, ([]string)(nil))
				clientMACs, _ := types.ListValueFrom(ctx, types.StringType, ([]string)(nil))
				webDomains, _ := types.ListValueFrom(ctx, types.StringType, ([]string)(nil))
				return firewallPolicyEndpointModel{
					ZoneID:             types.StringValue("z1"),
					MatchingTarget:     types.StringValue("IP"),
					MatchingTargetType: types.StringValue("OBJECT"),
					IPs:                ips,
					NetworkIDs:         networkIDs,
					ClientMACs:         clientMACs,
					WebDomains:         webDomains,
					Port:               types.StringValue("443"),
					PortGroupID:        types.StringValue(""),
					IPGroupID:          types.StringValue(""),
					PortMatchingType:   types.StringValue("SPECIFIC"),
				}
			}(),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := apiSourceToEndpointModel(
				tt.args.ctx,
				tt.args.src,
				tt.args.diags,
			); !reflect.DeepEqual(
				got,
				tt.want,
			) {
				t.Errorf("apiSourceToEndpointModel() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func Test_apiDestinationToEndpointModel(t *testing.T) {
	type args struct {
		ctx   context.Context
		dst   *unifi.FirewallPolicyDestination
		diags *diag.Diagnostics
	}
	tests := []struct {
		name string
		args args
		want firewallPolicyEndpointModel
	}{
		{
			name: "destination with port",
			args: args{
				ctx:   context.Background(),
				diags: &diag.Diagnostics{},
				dst: &unifi.FirewallPolicyDestination{
					ZoneID:             "z2",
					MatchingTarget:     "ANY",
					MatchingTargetType: "OBJECT",
					PortMatchingType:   "SPECIFIC",
					Port:               "8080",
				},
			},
			want: func() firewallPolicyEndpointModel {
				ctx := context.Background()
				ips, _ := types.ListValueFrom(ctx, types.StringType, ([]string)(nil))
				networkIDs, _ := types.ListValueFrom(ctx, types.StringType, ([]string)(nil))
				clientMACs, _ := types.ListValueFrom(ctx, types.StringType, ([]string)(nil))
				webDomains, _ := types.ListValueFrom(ctx, types.StringType, ([]string)(nil))
				return firewallPolicyEndpointModel{
					ZoneID:             types.StringValue("z2"),
					MatchingTarget:     types.StringValue("ANY"),
					MatchingTargetType: types.StringValue("OBJECT"),
					IPs:                ips,
					NetworkIDs:         networkIDs,
					ClientMACs:         clientMACs,
					WebDomains:         webDomains,
					Port:               types.StringValue("8080"),
					PortGroupID:        types.StringValue(""),
					IPGroupID:          types.StringValue(""),
					PortMatchingType:   types.StringValue("SPECIFIC"),
				}
			}(),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := apiDestinationToEndpointModel(
				tt.args.ctx,
				tt.args.dst,
				tt.args.diags,
			); !reflect.DeepEqual(
				got,
				tt.want,
			) {
				t.Errorf("apiDestinationToEndpointModel() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func Test_firewallPolicyResource_firewallPolicyListToModel(t *testing.T) {
	type args struct {
		ctx   context.Context
		api   *unifi.FirewallPolicy
		model *firewallPolicyModel
		site  string
	}
	tests := []struct {
		name string
		r    *firewallPolicyResource
		args args
		want diag.Diagnostics
	}{
		{
			name: "sets site and populates model",
			r:    &firewallPolicyResource{},
			args: args{
				ctx: context.Background(),
				api: &unifi.FirewallPolicy{
					ID:       "pol-1",
					Name:     "list-test",
					Action:   "BLOCK",
					Protocol: "all",
					Source: &unifi.FirewallPolicySource{
						ZoneID:           "z1",
						MatchingTarget:   "ANY",
						PortMatchingType: "ANY",
					},
					Destination: &unifi.FirewallPolicyDestination{
						ZoneID:           "z2",
						MatchingTarget:   "ANY",
						PortMatchingType: "ANY",
					},
				},
				model: &firewallPolicyModel{},
				site:  "mysite",
			},
			want: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.r.firewallPolicyListToModel(
				tt.args.ctx,
				tt.args.api,
				tt.args.model,
				tt.args.site,
			); !reflect.DeepEqual(
				got,
				tt.want,
			) {
				t.Errorf(
					"firewallPolicyResource.firewallPolicyListToModel() = %v, want %v",
					got,
					tt.want,
				)
			}
			if tt.args.model.Site.ValueString() != tt.args.site {
				t.Errorf("model.Site = %q, want %q", tt.args.model.Site.ValueString(), tt.args.site)
			}
		})
	}
}

func Test_firewallPolicyResource_ListResourceConfigSchema(t *testing.T) {
	type args struct {
		in0  context.Context
		in1  fwlist.ListResourceSchemaRequest
		resp *fwlist.ListResourceSchemaResponse
	}
	tests := []struct {
		name string
		r    *firewallPolicyResource
		args args
	}{
		{
			name: "has site attribute",
			r:    &firewallPolicyResource{},
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

// TestFirewallPolicyMatchingTargetType guards #293: a specific (non-ANY) match
// must carry a concrete matching_target_type — the controller rejects an empty
// one (api.err.MissingFirewallPolicySourceMatchingTargetType) when a source is
// switched from ANY to e.g. IP. A controller-assigned type is preserved.
func TestFirewallPolicyMatchingTargetType(t *testing.T) {
	cases := []struct {
		matchingTarget, current, ipGroupID, want string
	}{
		{"IP", "", "", "SPECIFIC"},         // ANY -> IP, type was dropped
		{"IP", "ANY", "", "SPECIFIC"},      // ANY -> IP, stale "ANY" left over
		{"IP", "SPECIFIC", "", "SPECIFIC"}, // already correct
		{"IP", "OBJECT", "", "OBJECT"},     // controller-assigned object/group preserved
		{"NETWORK", "", "", "SPECIFIC"},
		{"ANY", "", "", ""}, // ANY source untouched
		{"ANY", "ANY", "", "ANY"},
		// ip_group_id set (#316): the group reference requires OBJECT. On create
		// the type is empty; on an update from literal ips a stale
		// ""/"ANY"/"SPECIFIC" may ride along — all must derive OBJECT.
		{"IP", "", "gid1", "OBJECT"},
		{"IP", "ANY", "gid1", "OBJECT"},
		{"IP", "SPECIFIC", "gid1", "OBJECT"},
		{"IP", "OBJECT", "gid1", "OBJECT"}, // already correct
		{"IP", "LIST", "gid1", "LIST"},     // controller-assigned LIST preserved
		// The group check ignores matching_target: a non-empty ip_group_id
		// derives OBJECT even for targets that shouldn't carry one (the schema
		// has no cross-field validation either way; pinned as documentation).
		{"ANY", "", "gid1", "OBJECT"},
		{"NETWORK", "", "gid1", "OBJECT"},
		// A stale "OBJECT" surviving a transition away from IP (ip_group_id
		// now correctly cleared by endpointOwnsSelector gating) must not be
		// preserved — OBJECT is only meaningful under matching_target == "IP"
		// (design doc §4.3's ip_group_id correctness note).
		{"NETWORK", "OBJECT", "", "SPECIFIC"},
		{"CLIENT", "OBJECT", "", "SPECIFIC"},
		{"ANY", "OBJECT", "", ""},
	}
	for _, c := range cases {
		got := firewallPolicyMatchingTargetType(c.matchingTarget, c.current, c.ipGroupID)
		if got != c.want {
			t.Errorf("matchingTargetType(%q,%q,%q) = %q, want %q",
				c.matchingTarget, c.current, c.ipGroupID, got, c.want)
		}
	}

	// End-to-end: an IP source whose type was lost serializes SPECIFIC.
	ctx := context.Background()
	var diags diag.Diagnostics
	ips, _ := types.ListValueFrom(ctx, types.StringType, []string{"10.0.40.138"})
	m := firewallPolicyEndpointModel{
		ZoneID:             types.StringValue("z1"),
		MatchingTarget:     types.StringValue("IP"),
		IPs:                ips,
		NetworkIDs:         types.ListNull(types.StringType),
		ClientMACs:         types.ListNull(types.StringType),
		WebDomains:         types.ListNull(types.StringType),
		Port:               types.StringNull(),
		PortGroupID:        types.StringNull(),
		PortMatchingType:   types.StringValue("ANY"),
		MatchingTargetType: types.StringValue("ANY"),
	}
	if got := endpointModelToSource(ctx, m, &diags).MatchingTargetType; got != "SPECIFIC" {
		t.Errorf("source MatchingTargetType = %q, want SPECIFIC", got)
	}
}

// TestFirewallPolicyPreserveMatchingTargetType guards #324: matching_target_type
// is firmware-derived and may change during an update PUT (e.g. "" -> "SPECIFIC"
// for a non-ANY match, via the controller or firewallPolicyMatchingTargetType).
// Since the attribute is Computed + UseStateForUnknown, the planned value is the
// prior-state value; the Update path captures it and re-asserts it on the
// post-apply state so Terraform's consistency check passes. This exercises the
// extract/replace helpers that implement that re-assertion.
func TestFirewallPolicyPreserveMatchingTargetType(t *testing.T) {
	ctx := context.Background()
	var diags diag.Diagnostics

	// A source object as it appears in the plan, with matching_target_type
	// carried over from prior state as "" (a legacy/empty state).
	planned := firewallPolicyEndpointModel{
		ZoneID:             types.StringValue("zone-1"),
		MatchingTarget:     types.StringValue("IP"),
		NetworkIDs:         types.ListNull(types.StringType),
		ClientMACs:         types.ListNull(types.StringType),
		IPs:                types.ListNull(types.StringType),
		WebDomains:         types.ListNull(types.StringType),
		Port:               types.StringValue("443"),
		PortGroupID:        types.StringNull(),
		PortMatchingType:   types.StringValue("SPECIFIC"),
		MatchingTargetType: types.StringValue(""),
	}
	plannedObj, d := types.ObjectValueFrom(
		ctx,
		firewallPolicyEndpointModel{}.AttributeTypes(),
		planned,
	)
	diags.Append(d...)

	if got := endpointMatchingTargetType(ctx, plannedObj, &diags); got.ValueString() != "" {
		t.Errorf("endpointMatchingTargetType = %q, want \"\"", got.ValueString())
	}

	// The controller re-derived matching_target_type to "SPECIFIC" on the apply
	// response. We must re-assert the planned "" to keep the result consistent.
	applied := planned
	applied.MatchingTargetType = types.StringValue("SPECIFIC")
	appliedObj, d2 := types.ObjectValueFrom(
		ctx,
		firewallPolicyEndpointModel{}.AttributeTypes(),
		applied,
	)
	diags.Append(d2...)

	fixed := withMatchingTargetType(ctx, appliedObj, types.StringValue(""), &diags)
	if diags.HasError() {
		t.Fatalf("helpers errored: %v", diags)
	}

	var out firewallPolicyEndpointModel
	fixed.As(ctx, &out, basetypes.ObjectAsOptions{})
	if out.MatchingTargetType.ValueString() != "" {
		t.Errorf("after withMatchingTargetType, matching_target_type = %q, want \"\"",
			out.MatchingTargetType.ValueString())
	}
	// Every other attribute must survive untouched.
	if out.Port.ValueString() != "443" || out.PortMatchingType.ValueString() != "SPECIFIC" {
		t.Errorf("withMatchingTargetType clobbered other fields: port=%q pmt=%q",
			out.Port.ValueString(), out.PortMatchingType.ValueString())
	}
}

// TestFirewallPolicyIPGroupIDRoundTrip guards #316: a source/destination may
// match an address-group firewall group via ip_group_id, returned by the
// controller alongside port_group_id. It must round-trip both directions.
func TestFirewallPolicyIPGroupIDRoundTrip(t *testing.T) {
	ctx := context.Background()
	var diags diag.Diagnostics

	m := firewallPolicyEndpointModel{
		ZoneID:             types.StringValue("zone-1"),
		MatchingTarget:     types.StringValue("IP"),
		NetworkIDs:         types.ListNull(types.StringType),
		ClientMACs:         types.ListNull(types.StringType),
		IPs:                types.ListNull(types.StringType),
		WebDomains:         types.ListNull(types.StringType),
		Port:               types.StringNull(),
		PortGroupID:        types.StringValue(""),
		IPGroupID:          types.StringValue("68945578bfcb5d2e51dd0f10"),
		PortMatchingType:   types.StringValue("ANY"),
		MatchingTargetType: types.StringValue("OBJECT"),
	}

	// model -> API (PUT path)
	src := endpointModelToSource(ctx, m, &diags)
	if diags.HasError() {
		t.Fatalf("source conversion errored: %v", diags)
	}
	if src.IPGroupID != "68945578bfcb5d2e51dd0f10" {
		t.Errorf("source IPGroupID = %q, want 68945578bfcb5d2e51dd0f10", src.IPGroupID)
	}
	dst := endpointModelToDestination(ctx, m, &diags)
	if dst.IPGroupID != "68945578bfcb5d2e51dd0f10" {
		t.Errorf("destination IPGroupID = %q, want 68945578bfcb5d2e51dd0f10", dst.IPGroupID)
	}

	// API -> model (read path)
	got := apiSourceToEndpointModel(
		ctx,
		&unifi.FirewallPolicySource{
			ZoneID:         "zone-1",
			MatchingTarget: "IP",
			IPGroupID:      "abc123",
		},
		&diags,
	)
	if diags.HasError() {
		t.Fatalf("apiSourceToEndpointModel errored: %v", diags)
	}
	if got.IPGroupID.ValueString() != "abc123" {
		t.Errorf("read IPGroupID = %q, want abc123", got.IPGroupID.ValueString())
	}
}

// TestFirewallPolicyEndpointListsUseStateForUnknown guards #338: the Computed
// match-list attributes (network_ids, client_macs, ips, web_domains) must carry
// a plan modifier so they keep their prior value when an unrelated field (index,
// protocol, …) changes, instead of churning to "known after apply".
func TestFirewallPolicyEndpointListsUseStateForUnknown(t *testing.T) {
	resp := &fwresource.SchemaResponse{}
	(&firewallPolicyResource{}).Schema(context.Background(), fwresource.SchemaRequest{}, resp)

	for _, ep := range []string{"source", "destination"} {
		nested, ok := resp.Schema.Attributes[ep].(schema.SingleNestedAttribute)
		if !ok {
			t.Fatalf("%s is not a SingleNestedAttribute", ep)
		}
		for _, key := range []string{"network_ids", "client_macs", "ips", "web_domains"} {
			la, ok := nested.Attributes[key].(schema.ListAttribute)
			if !ok {
				t.Errorf("%s.%s is not a ListAttribute", ep, key)
				continue
			}
			if len(la.PlanModifiers) == 0 {
				t.Errorf("%s.%s must have a plan modifier (UseStateForUnknown) (#338)", ep, key)
			}
		}
	}
}

// TestEndpointOwnsSelector guards the matching_target discriminator table
// from the design doc (§4.3): only the active matching_target's own
// selector field is "owned"; every other selector is inactive and must be
// cleared, regardless of what is left over in state. ip_group_id is owned
// by IP alongside ips (design doc's "ip_group_id is a second, previously-
// missed instance of the same bug" correctness note) — it is meaningless
// under every other matching_target.
func TestEndpointOwnsSelector(t *testing.T) {
	cases := []struct {
		matchingTarget, field string
		want                  bool
	}{
		{"NETWORK", "network_ids", true},
		{"NETWORK", "ips", false},
		{"NETWORK", "client_macs", false},
		{"NETWORK", "web_domains", false},
		{"NETWORK", "ip_group_id", false},
		{"CLIENT", "client_macs", true},
		{"CLIENT", "network_ids", false},
		{"CLIENT", "ip_group_id", false},
		{"IP", "ips", true},
		{"IP", "network_ids", false},
		{"IP", "ip_group_id", true},
		{"WEB", "web_domains", true},
		{"WEB", "ips", false},
		{"WEB", "ip_group_id", false},
		{"ANY", "network_ids", false},
		{"ANY", "ips", false},
		{"ANY", "ip_group_id", false},
		{"DEVICE", "network_ids", false},
		{"DEVICE", "ip_group_id", false},
		{"MAC", "client_macs", false}, // MAC target has no list selector today
	}
	for _, c := range cases {
		if got := endpointOwnsSelector(c.matchingTarget, c.field); got != c.want {
			t.Errorf("endpointOwnsSelector(%q, %q) = %v, want %v",
				c.matchingTarget, c.field, got, c.want)
		}
	}
}

// TestEndpointOwnsPortField mirrors TestEndpointOwnsSelector for the
// port_matching_type discriminator.
func TestEndpointOwnsPortField(t *testing.T) {
	cases := []struct {
		portMatchingType, field string
		want                    bool
	}{
		{"SPECIFIC", "port", true},
		{"SPECIFIC", "port_group_id", false},
		{"OBJECT", "port_group_id", true},
		{"OBJECT", "port", false},
		{"ANY", "port", false},
		{"ANY", "port_group_id", false},
	}
	for _, c := range cases {
		if got := endpointOwnsPortField(c.portMatchingType, c.field); got != c.want {
			t.Errorf("endpointOwnsPortField(%q, %q) = %v, want %v",
				c.portMatchingType, c.field, got, c.want)
		}
	}
}

// TestFirewallPolicyStaleSelectorClearedOnTransition guards #4.3: a policy
// endpoint whose matching_target changed (e.g. state carries a stale `ips`
// left over from a prior IP-targeted config, current config is NETWORK) must
// not send the stale ips in the outbound PUT.
func TestFirewallPolicyStaleSelectorClearedOnTransition(t *testing.T) {
	ctx := context.Background()
	var diags diag.Diagnostics

	networkIDs, _ := types.ListValueFrom(ctx, types.StringType, []string{"net-1"})
	staleIPs, _ := types.ListValueFrom(ctx, types.StringType, []string{"10.0.0.5"})

	m := firewallPolicyEndpointModel{
		ZoneID:             types.StringValue("z1"),
		MatchingTarget:     types.StringValue("NETWORK"), // config: switched to NETWORK
		NetworkIDs:         networkIDs,                   // config: the new selector
		ClientMACs:         types.ListNull(types.StringType),
		IPs:                staleIPs, // stale: left over from a prior IP-targeted state
		WebDomains:         types.ListNull(types.StringType),
		Port:               types.StringNull(),
		PortGroupID:        types.StringNull(),
		PortMatchingType:   types.StringValue("ANY"),
		MatchingTargetType: types.StringValue("SPECIFIC"),
	}

	src := endpointModelToSource(ctx, m, &diags)
	if diags.HasError() {
		t.Fatalf("source conversion errored: %v", diags)
	}
	if len(src.NetworkIDs) != 1 || src.NetworkIDs[0] != "net-1" {
		t.Errorf("source NetworkIDs = %v, want [net-1]", src.NetworkIDs)
	}
	if len(src.IPs) != 0 {
		t.Errorf("source IPs = %v, want empty (stale selector must be cleared)", src.IPs)
	}

	dst := endpointModelToDestination(ctx, m, &diags)
	if len(dst.IPs) != 0 {
		t.Errorf("destination IPs = %v, want empty (stale selector must be cleared)", dst.IPs)
	}
}

// TestFirewallPolicyStaleIPGroupIDClearedOnTransition guards the design
// doc's ip_group_id correctness note: a policy switched from an IP-group
// match (matching_target=IP, matching_target_type=OBJECT, ip_group_id set)
// to NETWORK must not leave the stale ip_group_id in the outbound PUT —
// otherwise firewallPolicyMatchingTargetType forces matching_target_type
// back to OBJECT even though the endpoint is now NETWORK-targeted.
func TestFirewallPolicyStaleIPGroupIDClearedOnTransition(t *testing.T) {
	ctx := context.Background()
	var diags diag.Diagnostics

	networkIDs, _ := types.ListValueFrom(ctx, types.StringType, []string{"net-1"})

	m := firewallPolicyEndpointModel{
		ZoneID:             types.StringValue("z1"),
		MatchingTarget:     types.StringValue("NETWORK"), // config: switched to NETWORK
		NetworkIDs:         networkIDs,
		ClientMACs:         types.ListNull(types.StringType),
		IPs:                types.ListNull(types.StringType),
		WebDomains:         types.ListNull(types.StringType),
		Port:               types.StringNull(),
		PortGroupID:        types.StringNull(),
		IPGroupID:          types.StringValue("aaaa0000000000000000f101"), // stale: left over from a prior IP-group state
		PortMatchingType:   types.StringValue("ANY"),
		MatchingTargetType: types.StringValue("OBJECT"), // stale: left over from the prior IP-group state
	}

	src := endpointModelToSource(ctx, m, &diags)
	if diags.HasError() {
		t.Fatalf("source conversion errored: %v", diags)
	}
	if src.IPGroupID != "" {
		t.Errorf("source IPGroupID = %q, want empty (stale ip_group_id must be cleared)", src.IPGroupID)
	}
	if src.MatchingTargetType != "SPECIFIC" {
		t.Errorf("source MatchingTargetType = %q, want SPECIFIC (must not be forced to OBJECT by a cleared ip_group_id)",
			src.MatchingTargetType)
	}

	dst := endpointModelToDestination(ctx, m, &diags)
	if dst.IPGroupID != "" {
		t.Errorf("destination IPGroupID = %q, want empty (stale ip_group_id must be cleared)", dst.IPGroupID)
	}
}

// TestFirewallPolicyStalePortFieldClearedOnTransition mirrors the above for
// the port_matching_type discriminator: switching from SPECIFIC to OBJECT
// must not send a stale `port` alongside the new `port_group_id`.
func TestFirewallPolicyStalePortFieldClearedOnTransition(t *testing.T) {
	ctx := context.Background()
	var diags diag.Diagnostics

	m := firewallPolicyEndpointModel{
		ZoneID:             types.StringValue("z1"),
		MatchingTarget:     types.StringValue("IP"),
		NetworkIDs:         types.ListNull(types.StringType),
		ClientMACs:         types.ListNull(types.StringType),
		IPs:                types.ListNull(types.StringType),
		WebDomains:         types.ListNull(types.StringType),
		Port:               types.StringValue("443"),  // stale: left over from SPECIFIC
		PortGroupID:        types.StringValue("pg-1"), // config: switched to OBJECT
		PortMatchingType:   types.StringValue("OBJECT"),
		MatchingTargetType: types.StringValue("SPECIFIC"),
	}

	dst := endpointModelToDestination(ctx, m, &diags)
	if diags.HasError() {
		t.Fatalf("destination conversion errored: %v", diags)
	}
	if dst.Port != "" {
		t.Errorf("destination Port = %q, want empty (stale port must be cleared)", dst.Port)
	}
	if dst.PortGroupID != "pg-1" {
		t.Errorf("destination PortGroupID = %q, want pg-1", dst.PortGroupID)
	}
}

// TestApiSourceToEndpointModelNormalizesInactiveSelectors guards the read-side
// half of #4.3: if the controller echoes back a value in a selector field the
// active matching_target does not own, it must not appear in state. Includes
// ip_group_id alongside ips, per the design doc's correctness note.
func TestApiSourceToEndpointModelNormalizesInactiveSelectors(t *testing.T) {
	ctx := context.Background()
	var diags diag.Diagnostics

	src := &unifi.FirewallPolicySource{
		ZoneID:         "z1",
		MatchingTarget: "NETWORK",
		NetworkIDs:     []string{"net-1"},
		IPs:            []string{"10.0.0.5"},       // controller echoed a stale value
		IPGroupID:      "aaaa0000000000000000f101", // controller echoed a stale value
	}
	got := apiSourceToEndpointModel(ctx, src, &diags)
	if diags.HasError() {
		t.Fatalf("apiSourceToEndpointModel errored: %v", diags)
	}
	var ips []string
	got.IPs.ElementsAs(ctx, &ips, false)
	if len(ips) != 0 {
		t.Errorf("read IPs = %v, want empty for a NETWORK-targeted endpoint", ips)
	}
	if got.IPGroupID.ValueString() != "" {
		t.Errorf("read IPGroupID = %q, want empty for a NETWORK-targeted endpoint", got.IPGroupID.ValueString())
	}
	var networkIDs []string
	got.NetworkIDs.ElementsAs(ctx, &networkIDs, false)
	if len(networkIDs) != 1 || networkIDs[0] != "net-1" {
		t.Errorf("read NetworkIDs = %v, want [net-1]", networkIDs)
	}
}

// TestEndpointDiscriminatorPlanModifierNullsInactiveChildren guards design
// doc §4.3 item 3: a planned object whose matching_target changed away from
// IP must have its stale `ips`/`ip_group_id` forced to null in the *planned*
// object, before the validator (Step 7) or Update/Read ever run. This is
// what UseStateForUnknown alone cannot do, since it resolves unknown
// attributes from prior state before this modifier or the codec functions
// execute.
func TestEndpointDiscriminatorPlanModifierNullsInactiveChildren(t *testing.T) {
	ctx := context.Background()
	attrTypes := firewallPolicyEndpointModel{}.AttributeTypes()

	staleIPs, _ := types.ListValueFrom(ctx, types.StringType, []string{"10.0.0.5"})
	networkIDs, _ := types.ListValueFrom(ctx, types.StringType, []string{"net-1"})

	// priorState/config mimic what UseStateForUnknown has already resolved:
	// matching_target moved to NETWORK, network_ids is the new selector, but
	// ips still carries the value UseStateForUnknown copied forward from
	// state because the user didn't touch it.
	planned, diags := types.ObjectValueFrom(ctx, attrTypes, firewallPolicyEndpointModel{
		ZoneID:             types.StringValue("z1"),
		MatchingTarget:     types.StringValue("NETWORK"),
		NetworkIDs:         networkIDs,
		ClientMACs:         types.ListNull(types.StringType),
		IPs:                staleIPs, // stale, resolved forward by UseStateForUnknown
		WebDomains:         types.ListNull(types.StringType),
		Port:               types.StringNull(),
		PortGroupID:        types.StringNull(),
		IPGroupID:          types.StringValue("aaaa0000000000000000f101"), // stale
		PortMatchingType:   types.StringValue("ANY"),
		MatchingTargetType: types.StringValue("OBJECT"),
	})
	if diags.HasError() {
		t.Fatalf("building planned object: %v", diags)
	}

	m := endpointDiscriminatorPlanModifier{}
	req := planmodifier.ObjectRequest{PlanValue: planned}
	resp := &planmodifier.ObjectResponse{PlanValue: planned}
	m.PlanModifyObject(ctx, req, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("plan modifier errored: %v", resp.Diagnostics)
	}

	var got firewallPolicyEndpointModel
	resp.Diagnostics.Append(resp.PlanValue.As(ctx, &got, basetypes.ObjectAsOptions{})...)
	if resp.Diagnostics.HasError() {
		t.Fatalf("decoding plan modifier result: %v", resp.Diagnostics)
	}

	if !got.IPs.IsNull() {
		t.Errorf("planned IPs = %v, want null (matching_target is NETWORK, ips is inactive)", got.IPs)
	}
	if !got.IPGroupID.IsNull() && got.IPGroupID.ValueString() != "" {
		t.Errorf("planned IPGroupID = %q, want null/empty (matching_target is NETWORK, ip_group_id is inactive)",
			got.IPGroupID.ValueString())
	}
	var gotNetworkIDs []string
	got.NetworkIDs.ElementsAs(ctx, &gotNetworkIDs, false)
	if len(gotNetworkIDs) != 1 || gotNetworkIDs[0] != "net-1" {
		t.Errorf("planned NetworkIDs = %v, want [net-1] (the active selector must survive)", gotNetworkIDs)
	}
}

// TestEndpointDiscriminatorPlanModifierSkipsUnknownDiscriminator guards the
// same null/unknown-defers-to-apply rule the Step 7 validator follows: if
// matching_target itself is unknown in the plan (e.g. it depends on an
// unresolved computed value), the plan modifier must not guess and must not
// null out any child — nulling children based on a guessed discriminator
// would itself introduce a plan/apply mismatch.
func TestEndpointDiscriminatorPlanModifierSkipsUnknownDiscriminator(t *testing.T) {
	ctx := context.Background()
	attrTypes := firewallPolicyEndpointModel{}.AttributeTypes()

	ips, _ := types.ListValueFrom(ctx, types.StringType, []string{"10.0.0.5"})
	planned, diags := types.ObjectValueFrom(ctx, attrTypes, firewallPolicyEndpointModel{
		ZoneID:             types.StringValue("z1"),
		MatchingTarget:     types.StringUnknown(), // depends on an unresolved computed value
		NetworkIDs:         types.ListNull(types.StringType),
		ClientMACs:         types.ListNull(types.StringType),
		IPs:                ips,
		WebDomains:         types.ListNull(types.StringType),
		Port:               types.StringNull(),
		PortGroupID:        types.StringNull(),
		PortMatchingType:   types.StringValue("ANY"),
		MatchingTargetType: types.StringValue("SPECIFIC"),
	})
	if diags.HasError() {
		t.Fatalf("building planned object: %v", diags)
	}

	m := endpointDiscriminatorPlanModifier{}
	req := planmodifier.ObjectRequest{PlanValue: planned}
	resp := &planmodifier.ObjectResponse{PlanValue: planned}
	m.PlanModifyObject(ctx, req, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("plan modifier errored: %v", resp.Diagnostics)
	}

	var got firewallPolicyEndpointModel
	resp.Diagnostics.Append(resp.PlanValue.As(ctx, &got, basetypes.ObjectAsOptions{})...)
	if resp.Diagnostics.HasError() {
		t.Fatalf("decoding plan modifier result: %v", resp.Diagnostics)
	}
	var gotIPs []string
	got.IPs.ElementsAs(ctx, &gotIPs, false)
	if len(gotIPs) != 1 || gotIPs[0] != "10.0.0.5" {
		t.Errorf("planned IPs = %v, want unchanged [10.0.0.5] when matching_target is unknown (must defer, not guess)",
			gotIPs)
	}
}

// TestFirewallPolicyEndpointRejectsInactiveSelectorConfig guards the
// plan-time half of #4.3: configuring a selector field the active
// matching_target does not own must be a validation error, not a silently
// dropped value.
func TestFirewallPolicyEndpointRejectsInactiveSelectorConfig(t *testing.T) {
	ctx := context.Background()

	ips, _ := types.ListValueFrom(ctx, types.StringType, []string{"10.0.0.5"})
	obj, diags := types.ObjectValueFrom(ctx, firewallPolicyEndpointModel{}.AttributeTypes(),
		firewallPolicyEndpointModel{
			ZoneID:             types.StringValue("z1"),
			MatchingTarget:     types.StringValue("NETWORK"),
			NetworkIDs:         types.ListNull(types.StringType),
			ClientMACs:         types.ListNull(types.StringType),
			IPs:                ips, // contradiction: NETWORK target, ips configured
			WebDomains:         types.ListNull(types.StringType),
			Port:               types.StringNull(),
			PortGroupID:        types.StringNull(),
			PortMatchingType:   types.StringValue("ANY"),
			MatchingTargetType: types.StringValue("SPECIFIC"),
		})
	if diags.HasError() {
		t.Fatalf("building test object: %v", diags)
	}

	v := endpointDiscriminatorValidator{}
	req := validator.ObjectRequest{ConfigValue: obj}
	resp := &validator.ObjectResponse{}
	v.ValidateObject(ctx, req, resp)
	if !resp.Diagnostics.HasError() {
		t.Fatal("expected a validation error for ips configured under matching_target=NETWORK")
	}
}

// TestFirewallPolicyEndpointAllowsActiveSelectorConfig is the negative case:
// the active selector configured under its own matching_target is not an
// error.
func TestFirewallPolicyEndpointAllowsActiveSelectorConfig(t *testing.T) {
	ctx := context.Background()

	networkIDs, _ := types.ListValueFrom(ctx, types.StringType, []string{"net-1"})
	obj, diags := types.ObjectValueFrom(ctx, firewallPolicyEndpointModel{}.AttributeTypes(),
		firewallPolicyEndpointModel{
			ZoneID:             types.StringValue("z1"),
			MatchingTarget:     types.StringValue("NETWORK"),
			NetworkIDs:         networkIDs,
			ClientMACs:         types.ListNull(types.StringType),
			IPs:                types.ListNull(types.StringType),
			WebDomains:         types.ListNull(types.StringType),
			Port:               types.StringNull(),
			PortGroupID:        types.StringNull(),
			PortMatchingType:   types.StringValue("ANY"),
			MatchingTargetType: types.StringValue("SPECIFIC"),
		})
	if diags.HasError() {
		t.Fatalf("building test object: %v", diags)
	}

	v := endpointDiscriminatorValidator{}
	req := validator.ObjectRequest{ConfigValue: obj}
	resp := &validator.ObjectResponse{}
	v.ValidateObject(ctx, req, resp)
	if resp.Diagnostics.HasError() {
		t.Errorf("unexpected validation error: %v", resp.Diagnostics)
	}
}

// TestFirewallPolicyEndpointValidatorDefersOnUnknownMatchingTarget guards
// design doc §4.3 item 4's correctness requirement: when matching_target
// itself is null or unknown, the validator must SKIP the selector-ownership
// checks entirely rather than treating the discriminator as "" — treating
// an unknown discriminator as "" would make every non-empty selector look
// unowned and reject a config that is actually valid once the discriminator
// resolves at apply time.
func TestFirewallPolicyEndpointValidatorDefersOnUnknownMatchingTarget(t *testing.T) {
	ctx := context.Background()

	ips, _ := types.ListValueFrom(ctx, types.StringType, []string{"10.0.0.5"})
	obj, diags := types.ObjectValueFrom(ctx, firewallPolicyEndpointModel{}.AttributeTypes(),
		firewallPolicyEndpointModel{
			ZoneID:             types.StringValue("z1"),
			MatchingTarget:     types.StringUnknown(), // depends on an unresolved computed value
			NetworkIDs:         types.ListNull(types.StringType),
			ClientMACs:         types.ListNull(types.StringType),
			IPs:                ips, // would be "inactive" under every guessed discriminator except IP
			WebDomains:         types.ListNull(types.StringType),
			Port:               types.StringNull(),
			PortGroupID:        types.StringNull(),
			PortMatchingType:   types.StringValue("ANY"),
			MatchingTargetType: types.StringValue("SPECIFIC"),
		})
	if diags.HasError() {
		t.Fatalf("building test object: %v", diags)
	}

	v := endpointDiscriminatorValidator{}
	req := validator.ObjectRequest{ConfigValue: obj}
	resp := &validator.ObjectResponse{}
	v.ValidateObject(ctx, req, resp)
	if resp.Diagnostics.HasError() {
		t.Errorf("matching_target unknown must defer selector validation to apply, got: %v", resp.Diagnostics)
	}
}

// TestFirewallPolicyEndpointValidatorDefersOnNullPortMatchingType mirrors
// the above for the port_matching_type discriminator and a null (not just
// unknown) value — null must defer identically to unknown, not be treated
// as ANY/"".
func TestFirewallPolicyEndpointValidatorDefersOnNullPortMatchingType(t *testing.T) {
	ctx := context.Background()

	obj, diags := types.ObjectValueFrom(ctx, firewallPolicyEndpointModel{}.AttributeTypes(),
		firewallPolicyEndpointModel{
			ZoneID:             types.StringValue("z1"),
			MatchingTarget:     types.StringValue("ANY"),
			NetworkIDs:         types.ListNull(types.StringType),
			ClientMACs:         types.ListNull(types.StringType),
			IPs:                types.ListNull(types.StringType),
			WebDomains:         types.ListNull(types.StringType),
			Port:               types.StringValue("443"), // would be "inactive" under a guessed ANY/""
			PortGroupID:        types.StringNull(),
			PortMatchingType:   types.StringNull(), // depends on an unresolved computed value
			MatchingTargetType: types.StringValue("SPECIFIC"),
		})
	if diags.HasError() {
		t.Fatalf("building test object: %v", diags)
	}

	v := endpointDiscriminatorValidator{}
	req := validator.ObjectRequest{ConfigValue: obj}
	resp := &validator.ObjectResponse{}
	v.ValidateObject(ctx, req, resp)
	if resp.Diagnostics.HasError() {
		t.Errorf("port_matching_type null must defer port-field validation to apply, got: %v", resp.Diagnostics)
	}
}

// TestFirewallPolicyPortSchemaRejectsSemanticallyInvalidPorts guards #4.5:
// the port field must carry PortRangeListValidator (full syntax-and-
// semantics validation) and nothing else — the old shape-gate regex is
// removed in the same change, not kept alongside it (design doc Open
// Question 4).
func TestFirewallPolicyPortSchemaRejectsSemanticallyInvalidPorts(t *testing.T) {
	resp := &fwresource.SchemaResponse{}
	(&firewallPolicyResource{}).Schema(context.Background(), fwresource.SchemaRequest{}, resp)

	source, ok := resp.Schema.Attributes["source"].(schema.SingleNestedAttribute)
	if !ok {
		t.Fatal("source is not a SingleNestedAttribute")
	}
	portAttr, ok := source.Attributes["port"].(schema.StringAttribute)
	if !ok {
		t.Fatal("source.port is not a StringAttribute")
	}
	if len(portAttr.Validators) != 1 {
		t.Fatalf("source.port must have exactly 1 validator (PortRangeListValidator, no separate shape regex), got %d",
			len(portAttr.Validators))
	}
	if _, ok := portAttr.Validators[0].(interface {
		ValidateString(context.Context, validator.StringRequest, *validator.StringResponse)
	}); !ok {
		t.Fatalf("source.port's validator does not implement validator.String")
	}
}
