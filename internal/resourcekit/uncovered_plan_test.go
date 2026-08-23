package resourcekit

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// AN ATTRIBUTE NO FIELD CLAIMS STILL BELONGS TO THE PLAN. network's vlan is
// the case: BeforeSend derives two wires (vlan, vlan_enabled) from the one
// released attribute, so no Field carries it -- and ApplyPlanToState, which
// walks Fields, never moved the plan's new number onto the effective state.
// An update whose only change was the vlan sent the state's old value,
// measured live: the apply planned 81 and the controller kept 76.

type uncoveredModel struct {
	ID   types.String `tfsdk:"id"`
	Name types.String `tfsdk:"name"`
	Vlan types.Int64  `tfsdk:"vlan"`
}

type uncoveredSDK struct {
	Name string
}

func uncoveredSpec() Spec[uncoveredModel, uncoveredSDK] {
	return Spec[uncoveredModel, uncoveredSDK]{
		TypeName: "probe",
		ID:       func(m *uncoveredModel) *types.String { return &m.ID },
		Fields: []Field[uncoveredModel, uncoveredSDK]{
			StringField[uncoveredModel, uncoveredSDK]{
				Wire:  "name",
				Model: func(m *uncoveredModel) *types.String { return &m.Name },
				SDK:   func(s *uncoveredSDK) *string { return &s.Name },
				Elide: KeepZero,
			},
		},
	}
}

func TestApplyPlanToStateReachesTheAttributesNoFieldClaims(t *testing.T) {
	spec := uncoveredSpec()
	state := uncoveredModel{
		ID:   types.StringValue("id-1"),
		Name: types.StringValue("old"),
		Vlan: types.Int64Value(76),
	}
	plan := uncoveredModel{
		ID:   types.StringValue("id-1"),
		Name: types.StringValue("new"),
		Vlan: types.Int64Value(81),
	}
	spec.ApplyPlanToState(&plan, &state)
	if state.Name.ValueString() != "new" {
		t.Errorf("the Field-covered attribute was not applied: %v", state.Name)
	}
	if state.Vlan.ValueInt64() != 81 {
		t.Errorf("vlan = %v, want 81; the plan's change to an attribute no Field claims "+
			"never reached the effective state, so the write carried the old value", state.Vlan)
	}
}

func TestApplyPlanToStateLeavesUnsetUncoveredAttributesAlone(t *testing.T) {
	spec := uncoveredSpec()
	state := uncoveredModel{
		ID:   types.StringValue("id-1"),
		Name: types.StringValue("old"),
		Vlan: types.Int64Value(76),
	}
	plan := uncoveredModel{
		ID:   types.StringValue("id-1"),
		Name: types.StringValue("new"),
		Vlan: types.Int64Null(),
	}
	spec.ApplyPlanToState(&plan, &state)
	if state.Vlan.ValueInt64() != 76 {
		t.Errorf("vlan = %v, want the state's 76; a null plan value is an absence, "+
			"not an instruction", state.Vlan)
	}

	plan.Vlan = types.Int64Unknown()
	spec.ApplyPlanToState(&plan, &state)
	if state.Vlan.IsUnknown() || state.Vlan.ValueInt64() != 76 {
		t.Errorf("vlan = %v, want the state's 76; an unknown must never overwrite a "+
			"known value", state.Vlan)
	}
}

// THE RESPONSE DOES NOT OUTRANK THE PLAN FOR A VALUE THE PLAN SET. A vlan-only
// network's encoder omits 54 of the surface's 67 wires, so the controller
// echoes none of them back -- and a create that let the response win recorded
// null for gateway_type and false for auto_scale against a plan that said
// "default" and true. Terraform refuses that as an inconsistent result, and it
// is: the practitioner's values are not the controller's to unset by silence.
func TestCreateKeepsAPlanValueTheResponseOmits(t *testing.T) {
	r := kitResource(Backend[kitSDK]{
		Create: func(_ context.Context, _ string, in *kitSDK) (*kitSDK, error) {
			return &kitSDK{ID: "made-1"}, nil // echoes nothing but the id
		},
	})
	ctx := context.Background()
	plan := kitStateWith(t, kitModel{
		Site: types.StringValue("default"), Name: types.StringValue("keep-me"),
	})
	identity := kitIdentity(t)
	resp := &resource.CreateResponse{State: kitStateWith(t, kitModel{}), Identity: &identity}
	r.Create(ctx, resource.CreateRequest{
		Plan:   tfsdk.Plan(plan),
		Config: tfsdk.Config(plan),
	}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("Create: %v", resp.Diagnostics)
	}
	var name types.String
	if diags := resp.State.GetAttribute(ctx, path.Root("name"), &name); diags.HasError() {
		t.Fatalf("reading name: %v", diags)
	}
	if name.ValueString() != "keep-me" {
		t.Errorf("name = %v, want keep-me; the response's silence unset a planned value", name)
	}
}

func TestUpdateKeepsAPlanValueTheResponseOmits(t *testing.T) {
	r := kitResource(Backend[kitSDK]{
		UpdateFields: func(_ context.Context, _ string, in *kitSDK, _ ...string) (*kitSDK, error) {
			return &kitSDK{ID: in.ID}, nil // echoes nothing but the id
		},
	})
	ctx := context.Background()
	state := kitStateWith(t, kitModel{
		ID: types.StringValue("id-1"), Site: types.StringValue("default"),
		Name: types.StringValue("before"),
	})
	plan := kitStateWith(t, kitModel{
		ID: types.StringValue("id-1"), Site: types.StringValue("default"),
		Name: types.StringValue("after"),
	})
	identity := kitIdentity(t)
	resp := &resource.UpdateResponse{State: state, Identity: &identity}
	r.Update(ctx, resource.UpdateRequest{
		State:  state,
		Plan:   tfsdk.Plan(plan),
		Config: tfsdk.Config(plan),
	}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("Update: %v", resp.Diagnostics)
	}
	var name types.String
	if diags := resp.State.GetAttribute(ctx, path.Root("name"), &name); diags.HasError() {
		t.Fatalf("reading name: %v", diags)
	}
	if name.ValueString() != "after" {
		t.Errorf("name = %v, want after; the response's silence unset a planned value", name)
	}
}
