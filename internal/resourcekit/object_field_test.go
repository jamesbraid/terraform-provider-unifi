package resourcekit

import (
	"context"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
)

func TestNestedProblemsNamesTheUnmodelledForceEmittedMembers(t *testing.T) {
	// What firewall_policy's endpoint object actually declares today.
	declared := map[string]attr.Type{
		"client_macs":          types.ListType{ElemType: types.StringType},
		"ip_group_id":          types.StringType,
		"ips":                  types.ListType{ElemType: types.StringType},
		"matching_target":      types.StringType,
		"matching_target_type": types.StringType,
		"network_ids":          types.ListType{ElemType: types.StringType},
		"port":                 types.StringType,
		"port_group_id":        types.StringType,
		"port_matching_type":   types.StringType,
		"web_domains":          types.ListType{ElemType: types.StringType},
		"zone_id":              types.StringType,
	}
	field := ObjectField[kitModel, kitSDK, ui.FirewallPolicySource]{
		Wire:      "source",
		AttrTypes: declared,
	}

	problems := field.nestedProblems()
	want := []string{
		"source.match_mac",
		"source.match_opposite_ips",
		"source.match_opposite_networks",
		"source.match_opposite_ports",
	}
	if len(problems) != len(want) {
		t.Fatalf("got %d problem(s), want %d:\n%s", len(problems), len(want),
			strings.Join(problems, "\n"))
	}
	for _, name := range want {
		found := false
		for _, problem := range problems {
			if strings.HasPrefix(problem, name+" ") {
				found = true
			}
		}
		if !found {
			t.Errorf("no problem names %s; it is force-emitted and undeclared", name)
		}
	}
	// The control: a declared member must not be reported, or the check
	// would flag everything and the assertion above would prove nothing.
	for _, problem := range problems {
		if strings.HasPrefix(problem, "source.zone_id") {
			t.Errorf("zone_id is declared and was reported anyway: %s", problem)
		}
	}
}

func TestUnmodelledSilencesOnlyWhatItNames(t *testing.T) {
	field := ObjectField[kitModel, kitSDK, ui.FirewallPolicySource]{
		Wire:      "source",
		AttrTypes: map[string]attr.Type{},
		Unmodelled: []string{
			"match_mac", "match_opposite_ips", "match_opposite_networks",
			"match_opposite_ports",
		},
	}
	// Everything force-emitted is either declared or named, so the field is
	// clean -- the descriptor has recorded the decision.
	if problems := field.nestedProblems(); len(problems) != 0 {
		t.Fatalf("an exemption naming every force-emitted member still reported %d "+
			"problem(s): %s", len(problems), strings.Join(problems, "\n"))
	}

	partial := field
	partial.Unmodelled = []string{"match_mac"}
	problems := partial.nestedProblems()
	if len(problems) != 3 {
		t.Fatalf("exempting one of four left %d problem(s), want 3: %s",
			len(problems), strings.Join(problems, "\n"))
	}
	for _, problem := range problems {
		if strings.HasPrefix(problem, "source.match_mac") {
			t.Error("match_mac was named in the exemption and reported anyway")
		}
	}
}

func TestNestedProblemsIgnoresOmitemptyMembers(t *testing.T) {
	field := ObjectField[kitModel, kitSDK, ui.FirewallPolicySource]{
		Wire:      "source",
		AttrTypes: map[string]attr.Type{},
		Unmodelled: []string{
			"match_mac", "match_opposite_ips", "match_opposite_networks",
			"match_opposite_ports",
		},
	}
	// FirewallPolicySource has eleven omitempty members and four
	// force-emitted ones; with the four named, nothing should be left.
	if problems := field.nestedProblems(); len(problems) != 0 {
		t.Fatalf("omitempty members were reported: %s", strings.Join(problems, "\n"))
	}
}

func TestObjectFieldRoundTripsThroughTheDescriptorsConverters(t *testing.T) {
	ctx := context.Background()
	attrTypes := map[string]attr.Type{"zone_id": types.StringType}
	field := ObjectField[kitModel, kitSDK, ui.FirewallPolicySource]{
		Wire:      "source",
		AttrTypes: attrTypes,
		Model:     func(m *kitModel) *types.Object { return &m.Nested },
		SDK:       func(s *kitSDK) **ui.FirewallPolicySource { return &s.Nested },
		Encode: func(_ context.Context, o types.Object) (*ui.FirewallPolicySource, diag.Diagnostics) {
			var diags diag.Diagnostics
			zone, ok := o.Attributes()["zone_id"].(types.String)
			if !ok {
				diags.AddError("encode", "zone_id missing")
				return nil, diags
			}
			return &ui.FirewallPolicySource{ZoneID: zone.ValueString()}, diags
		},
		Decode: func(_ context.Context, s *ui.FirewallPolicySource) (types.Object, diag.Diagnostics) {
			return types.ObjectValue(attrTypes, map[string]attr.Value{
				"zone_id": types.StringValue(s.ZoneID),
			})
		},
	}

	t.Run("a configured object reaches the SDK", func(t *testing.T) {
		object, d := types.ObjectValue(attrTypes,
			map[string]attr.Value{"zone_id": types.StringValue("zone-1")})
		if d.HasError() {
			t.Fatal(d)
		}
		model := kitModel{Nested: object}
		var sdk kitSDK
		if diags := field.ToSDK(ctx, &model, &sdk); diags.HasError() {
			t.Fatalf("ToSDK: %v", diags)
		}
		if sdk.Nested == nil || sdk.Nested.ZoneID != "zone-1" {
			t.Fatalf("SDK object = %+v, want ZoneID zone-1", sdk.Nested)
		}
	})

	t.Run("a null object leaves the SDK pointer nil", func(t *testing.T) {
		model := kitModel{Nested: types.ObjectNull(attrTypes)}
		sdk := kitSDK{Nested: &ui.FirewallPolicySource{ZoneID: "stale"}}
		if diags := field.ToSDK(ctx, &model, &sdk); diags.HasError() {
			t.Fatalf("ToSDK: %v", diags)
		}
		if sdk.Nested != nil {
			t.Errorf("SDK object = %+v, want nil so the omitempty key drops out "+
				"instead of sending an empty object", sdk.Nested)
		}
	})

	t.Run("what the controller returned reaches the model", func(t *testing.T) {
		var model kitModel
		sdk := kitSDK{Nested: &ui.FirewallPolicySource{ZoneID: "zone-2"}}
		if diags := field.ToModel(ctx, &sdk, &model); diags.HasError() {
			t.Fatalf("ToModel: %v", diags)
		}
		zone, ok := model.Nested.Attributes()["zone_id"].(types.String)
		if !ok || zone.ValueString() != "zone-2" {
			t.Errorf("model object = %v, want zone_id zone-2", model.Nested)
		}
	})
}

func TestNestedProblemsReachesFieldsThroughTheSpecAndThroughReadOnly(t *testing.T) {
	field := ObjectField[kitModel, kitSDK, ui.FirewallPolicySource]{
		Wire:      "source",
		AttrTypes: map[string]attr.Type{},
	}

	t.Run("through the spec", func(t *testing.T) {
		spec := Spec[kitModel, kitSDK]{Fields: []Field[kitModel, kitSDK]{field}}
		if got := len(NestedProblems(spec)); got != 4 {
			t.Fatalf("NestedProblems reported %d, want 4", got)
		}
	})

	t.Run("through ReadOnly", func(t *testing.T) {
		spec := Spec[kitModel, kitSDK]{
			Fields: []Field[kitModel, kitSDK]{ReadOnly[kitModel, kitSDK](field)},
		}
		if got := len(NestedProblems(spec)); got != 4 {
			t.Fatalf("NestedProblems reported %d through ReadOnly, want 4; ReadOnly "+
				"only stops the field being written from the model, the SDK struct "+
				"still goes out whole when its key is masked", got)
		}
	})

	t.Run("a spec with no object fields is clean", func(t *testing.T) {
		spec := kitResource(Backend[kitSDK]{}).Spec
		if got := NestedProblems(spec); len(got) != 0 {
			t.Fatalf("a spec of scalar fields reported %d problem(s): %v", len(got), got)
		}
	})
}

// radiusServer stands in for RADIUSProfileAcctServers: three members, all
// omitempty, which is what a clean nested type looks like.
type radiusServer struct {
	IP     string `json:"ip,omitempty"`
	Port   *int64 `json:"port,omitempty"`
	Secret string `json:"x_secret,omitempty"`
}

type listModel struct {
	Servers types.List `tfsdk:"servers"`
}

type listSDK struct {
	Servers []radiusServer
}

func serverListField() ObjectListField[listModel, listSDK, radiusServer] {
	attrTypes := map[string]attr.Type{"ip": types.StringType, "x_secret": types.StringType}
	return ObjectListField[listModel, listSDK, radiusServer]{
		Wire:      "acct_servers",
		Model:     func(m *listModel) *types.List { return &m.Servers },
		SDK:       func(s *listSDK) *[]radiusServer { return &s.Servers },
		AttrTypes: attrTypes,
		Encode: func(_ context.Context, o types.Object) (radiusServer, diag.Diagnostics) {
			var diags diag.Diagnostics
			ip, _ := o.Attributes()["ip"].(types.String)
			secret, _ := o.Attributes()["x_secret"].(types.String)
			return radiusServer{IP: ip.ValueString(), Secret: secret.ValueString()}, diags
		},
		Decode: func(_ context.Context, e radiusServer) (types.Object, diag.Diagnostics) {
			return types.ObjectValue(attrTypes, map[string]attr.Value{
				"ip":       types.StringValue(e.IP),
				"x_secret": types.StringValue(e.Secret),
			})
		},
		Elide: KeepZero,
	}
}

func TestObjectListFieldCarriesEveryElementBothWays(t *testing.T) {
	ctx := context.Background()
	field := serverListField()
	attrTypes := field.AttrTypes

	t.Run("the controller's list reaches the model", func(t *testing.T) {
		var model listModel
		sdk := listSDK{Servers: []radiusServer{
			{IP: "10.0.0.1", Secret: "a"}, {IP: "10.0.0.2", Secret: "b"},
		}}
		if diags := field.ToModel(ctx, &sdk, &model); diags.HasError() {
			t.Fatalf("ToModel: %v", diags)
		}
		if n := len(model.Servers.Elements()); n != 2 {
			t.Fatalf("model carries %d element(s), want 2", n)
		}
	})

	t.Run("the model's list reaches the SDK", func(t *testing.T) {
		one, d := types.ObjectValue(attrTypes, map[string]attr.Value{
			"ip": types.StringValue("10.0.0.9"), "x_secret": types.StringValue("z"),
		})
		if d.HasError() {
			t.Fatal(d)
		}
		list, d := types.ListValue(types.ObjectType{AttrTypes: attrTypes}, []attr.Value{one})
		if d.HasError() {
			t.Fatal(d)
		}
		model := listModel{Servers: list}
		var sdk listSDK
		if diags := field.ToSDK(ctx, &model, &sdk); diags.HasError() {
			t.Fatalf("ToSDK: %v", diags)
		}
		if len(sdk.Servers) != 1 || sdk.Servers[0].IP != "10.0.0.9" {
			t.Fatalf("SDK carries %+v, want one server with IP 10.0.0.9", sdk.Servers)
		}
	})

	t.Run("an empty configured list stays an allocated empty slice", func(t *testing.T) {
		list, d := types.ListValue(types.ObjectType{AttrTypes: attrTypes}, []attr.Value{})
		if d.HasError() {
			t.Fatal(d)
		}
		model := listModel{Servers: list}
		var sdk listSDK
		if diags := field.ToSDK(ctx, &model, &sdk); diags.HasError() {
			t.Fatalf("ToSDK: %v", diags)
		}
		if sdk.Servers == nil {
			t.Error("an empty configured list produced a nil slice; where the wire field " +
				"has no omitempty that is null rather than [], which the controller reads " +
				"as a different request")
		}
	})

	t.Run("a null model list leaves the SDK slice nil", func(t *testing.T) {
		model := listModel{Servers: types.ListNull(types.ObjectType{AttrTypes: attrTypes})}
		sdk := listSDK{Servers: []radiusServer{{IP: "stale"}}}
		if diags := field.ToSDK(ctx, &model, &sdk); diags.HasError() {
			t.Fatalf("ToSDK: %v", diags)
		}
		if sdk.Servers != nil {
			t.Errorf("SDK slice = %+v, want nil so an omitempty key drops out", sdk.Servers)
		}
	})
}

func TestObjectListFieldChecksItsElementType(t *testing.T) {
	clean := serverListField()
	if problems := clean.nestedProblems(); len(problems) != 0 {
		t.Errorf("radiusServer has no force-emitted members and reported %d problem(s): %s",
			len(problems), strings.Join(problems, "\n"))
	}

	// The control: the same kind pointed at an element type that DOES
	// force-emit must still report it, or the clean result above would be a
	// fact about the check rather than about radiusServer.
	hazardous := ObjectListField[listModel, listSDK, ui.FirewallPolicySource]{
		Wire:      "endpoints",
		AttrTypes: map[string]attr.Type{},
	}
	if problems := hazardous.nestedProblems(); len(problems) != 4 {
		t.Errorf("an element type with four force-emitted members reported %d problem(s): %s",
			len(problems), strings.Join(problems, "\n"))
	}
}

func TestObjectFieldKeepsAComputedMemberOutOfState(t *testing.T) {
	attrTypes := map[string]attr.Type{
		"zone_id":              types.StringType,
		"matching_target_type": types.StringType,
	}
	field := ObjectField[kitModel, kitSDK, ui.FirewallPolicySource]{
		Wire:      "source",
		AttrTypes: attrTypes,
		Model:     func(m *kitModel) *types.Object { return &m.Nested },
	}

	planned, d := types.ObjectValue(attrTypes, map[string]attr.Value{
		"zone_id":              types.StringValue("zone-new"),
		"matching_target_type": types.StringUnknown(),
	})
	if d.HasError() {
		t.Fatal(d)
	}
	current, d := types.ObjectValue(attrTypes, map[string]attr.Value{
		"zone_id":              types.StringValue("zone-old"),
		"matching_target_type": types.StringValue("SPECIFIC"),
	})
	if d.HasError() {
		t.Fatal(d)
	}

	plan := kitModel{Nested: planned}
	state := kitModel{Nested: current}
	field.CopyPlanToState(&plan, &state)

	got := state.Nested.Attributes()
	if value, _ := got["matching_target_type"].(types.String); value.IsUnknown() {
		t.Error("matching_target_type is unknown in state; Terraform rejects an unknown " +
			"after apply, and a wholesale object copy is how it got there")
	} else if value.ValueString() != "SPECIFIC" {
		t.Errorf("matching_target_type = %q, want the value the read produced", value.ValueString())
	}
	// The control: a member the plan sets must still win, or the merge would
	// be preserving everything and the assertion above would prove nothing.
	if value, _ := got["zone_id"].(types.String); value.ValueString() != "zone-new" {
		t.Errorf("zone_id = %q, want the plan's; a set member must win",
			value.ValueString())
	}
}
