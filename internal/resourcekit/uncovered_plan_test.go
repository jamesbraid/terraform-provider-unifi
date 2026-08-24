package resourcekit

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

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
