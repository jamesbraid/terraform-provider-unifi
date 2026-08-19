package resourcekit

import (
	"context"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
	fwp "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_firewall_policy"
)

// THE CHECK IS ITS OWN REPRODUCTION, and that is the point of building it
// before fixing anything.
//
// unifi.FirewallPolicySource carries four bools with no omitempty that the
// provider does not model. Pointing the kind at the real type reports all four
// by name -- so the capability that would have spread this defect across eleven
// surfaces is the same code that finds it.
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
	field := ObjectField[kitModel, kitSDK, ui.FirewallPolicySource, types.Object]{
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
	// The control: a member the object DOES declare must not be reported, or
	// the check would be flagging everything and the assertion above would pass
	// for a check that cannot distinguish.
	for _, problem := range problems {
		if strings.HasPrefix(problem, "source.zone_id") {
			t.Errorf("zone_id is declared and was reported anyway: %s", problem)
		}
	}
}

// TestUnmodelledSilencesOnlyWhatItNames is the property that makes the
// exemption a record rather than an off switch.
func TestUnmodelledSilencesOnlyWhatItNames(t *testing.T) {
	field := ObjectField[kitModel, kitSDK, ui.FirewallPolicySource, types.Object]{
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

	// A MEMBER THE LIST DOES NOT NAME STILL FAILS. This is the case a blanket
	// "preserve what I do not model" would absorb in silence, and it is what a
	// later SDK regeneration looks like: one more member, nobody's decision.
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

// TestNestedProblemsIgnoresOmitemptyMembers is the other half: the check must
// not report a member the wire simply drops, or every nested type in the SDK
// becomes a wall of false positives and the real four get lost in it.
func TestNestedProblemsIgnoresOmitemptyMembers(t *testing.T) {
	field := ObjectField[kitModel, kitSDK, ui.FirewallPolicySource, types.Object]{
		Wire:      "source",
		AttrTypes: map[string]attr.Type{},
		Unmodelled: []string{
			"match_mac", "match_opposite_ips", "match_opposite_networks",
			"match_opposite_ports",
		},
	}
	// FirewallPolicySource has eleven omitempty members and four force-emitted
	// ones. With the four named, nothing is left -- which says the check is
	// keyed on omitempty rather than on being undeclared.
	if problems := field.nestedProblems(); len(problems) != 0 {
		t.Fatalf("omitempty members were reported: %s", strings.Join(problems, "\n"))
	}
}

// TestObjectFieldRoundTripsThroughTheDescriptorsConverters covers the carrying
// part: the kind moves a types.Object to a *E and back using what the
// descriptor supplies, and a null object leaves the SDK pointer nil so an
// omitempty key drops out rather than being sent as an empty object.
func TestObjectFieldRoundTripsThroughTheDescriptorsConverters(t *testing.T) {
	ctx := context.Background()
	attrTypes := map[string]attr.Type{"zone_id": types.StringType}
	field := ObjectField[kitModel, kitSDK, ui.FirewallPolicySource, types.Object]{
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

// TestNestedProblemsReachesFieldsThroughTheSpecAndThroughReadOnly exercises the
// exported entry point, which the tests above bypass by calling the unexported
// method directly.
//
// The ReadOnly case matters for the same reason ElideProblems needed its
// unwrap: a wrapper that forwards ToModel but not ToSDK still carries the
// nested type, and a check that stopped at the wrapper would report a
// read-only nested object as clean. It is not automatically clean -- ReadOnly
// stops the field being WRITTEN from the model, but the SDK struct is still
// sent whole when its key is masked, so its force-emitted members still go.
func TestNestedProblemsReachesFieldsThroughTheSpecAndThroughReadOnly(t *testing.T) {
	field := ObjectField[kitModel, kitSDK, ui.FirewallPolicySource, types.Object]{
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
			t.Fatalf("NestedProblems reported %d through ReadOnly, want 4; the wrapper "+
				"is hiding the nested type and a read-only object would read as clean", got)
		}
	})

	t.Run("a spec with no object fields is clean", func(t *testing.T) {
		spec := kitResource(Backend[kitSDK]{}).Spec
		if got := NestedProblems(spec); len(got) != 0 {
			t.Fatalf("a spec of scalar fields reported %d problem(s): %v", len(got), got)
		}
	})
}

// TestObjectFieldCarriesAGeneratedValueType is the typed checkpoint, run
// against firewall_policy's REAL generated SourceValue rather than a stand-in.
//
// The decision in #195 is that the nested capability produces values of the
// generated CustomTypes, because plain objects leave eleven surfaces
// permanently unable to regenerate -- which would make the self-delete
// tripwire on graftPreservedCollections a guard that can never fire.
//
// What this proves is narrow and worth stating exactly: the kind can carry a
// generated value type, because SourceValue satisfies basetypes.ObjectValuable
// and the constraint is written against that interface rather than against
// types.Object. types.Object satisfies it too, so a descriptor that does not
// need a typed object is not forced into one.
//
// It does NOT prove the regenerate goes clean. That needs the schema binding
// and the model field changed together, and both are currently forbidden by
// TestServedSchemaAgreesWithItsRuntimeModel, which asserts the plain-object
// invariant in two directions.
func TestObjectFieldCarriesAGeneratedValueType(t *testing.T) {
	ctx := context.Background()

	field := ObjectField[typedModel, kitSDK, ui.FirewallPolicySource, fwp.SourceValue]{
		Wire:      "source",
		AttrTypes: fwp.SourceValue{}.AttributeTypes(ctx),
		Model:     func(m *typedModel) *fwp.SourceValue { return &m.Source },
		SDK:       func(s *kitSDK) **ui.FirewallPolicySource { return &s.Nested },
		Null:      fwp.NewSourceValueNull,
		Encode: func(_ context.Context, v fwp.SourceValue) (*ui.FirewallPolicySource, diag.Diagnostics) {
			return &ui.FirewallPolicySource{ZoneID: v.ZoneId.ValueString()}, nil
		},
		Decode: func(c context.Context, s *ui.FirewallPolicySource) (fwp.SourceValue, diag.Diagnostics) {
			return fwp.NewSourceValue(fwp.SourceValue{}.AttributeTypes(c), map[string]attr.Value{
				"client_macs":          types.ListNull(types.StringType),
				"ip_group_id":          types.StringNull(),
				"ips":                  types.ListNull(types.StringType),
				"matching_target":      types.StringNull(),
				"matching_target_type": types.StringNull(),
				"network_ids":          types.ListNull(types.StringType),
				"port":                 types.StringNull(),
				"port_group_id":        types.StringNull(),
				"port_matching_type":   types.StringNull(),
				"web_domains":          types.ListNull(types.StringType),
				"zone_id":              types.StringValue(s.ZoneID),
			})
		},
		// The four this type force-emits and the schema does not declare.
		Unmodelled: []string{
			"match_mac", "match_opposite_ips", "match_opposite_networks",
			"match_opposite_ports",
		},
	}

	t.Run("the controller's object becomes a typed value", func(t *testing.T) {
		var model typedModel
		sdk := kitSDK{Nested: &ui.FirewallPolicySource{ZoneID: "zone-9"}}
		if diags := field.ToModel(ctx, &sdk, &model); diags.HasError() {
			t.Fatalf("ToModel: %v", diags)
		}
		if model.Source.ZoneId.ValueString() != "zone-9" {
			t.Errorf("ZoneId = %q, want zone-9", model.Source.ZoneId.ValueString())
		}
		// The value is the generated type, not a plain object -- which is the
		// whole point of the decision.
		if _, ok := any(model.Source).(fwp.SourceValue); !ok {
			t.Error("the model does not carry a SourceValue")
		}
	})

	t.Run("an absent object becomes the typed null", func(t *testing.T) {
		model := typedModel{Source: fwp.NewSourceValueMust(
			fwp.SourceValue{}.AttributeTypes(ctx), map[string]attr.Value{
				"client_macs": types.ListNull(types.StringType), "ip_group_id": types.StringNull(),
				"ips": types.ListNull(types.StringType), "matching_target": types.StringNull(),
				"matching_target_type": types.StringNull(),
				"network_ids":          types.ListNull(types.StringType), "port": types.StringNull(),
				"port_group_id": types.StringNull(), "port_matching_type": types.StringNull(),
				"web_domains": types.ListNull(types.StringType), "zone_id": types.StringValue("stale"),
			})}
		var sdk kitSDK
		if diags := field.ToModel(ctx, &sdk, &model); diags.HasError() {
			t.Fatalf("ToModel: %v", diags)
		}
		if !model.Source.IsNull() {
			t.Errorf("Source = %v, want the typed null; the kind cannot build one "+
				"itself because V is an interface constraint and its zero value is "+
				"not its null value", model.Source)
		}
	})

	t.Run("the exemption still holds for a typed field", func(t *testing.T) {
		if problems := field.nestedProblems(); len(problems) != 0 {
			t.Fatalf("typed field reported %d problem(s): %s",
				len(problems), strings.Join(problems, "\n"))
		}
	})
}

// typedModel stands for a descriptor model whose nested field is the generated
// value type rather than a plain object.
type typedModel struct {
	Source fwp.SourceValue `tfsdk:"source"`
}
