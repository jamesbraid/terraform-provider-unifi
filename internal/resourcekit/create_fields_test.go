package resourcekit

import (
	"context"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestCreateFieldsSendsOnlyTheFieldsThePlanSet(t *testing.T) {
	var mask []string
	var sent *kitSDK
	r := kitResource(Backend[kitSDK]{
		CreateFields: func(_ context.Context, _ string, in *kitSDK, fields ...string) (*kitSDK, error) {
			mask, sent = fields, in
			return &kitSDK{ID: "assigned-by-controller", Name: in.Name}, nil
		},
	})
	plan := kitStateWith(t, kitModel{
		ID: types.StringNull(), Site: types.StringValue("default"),
		Name: types.StringValue("probe"),
	})
	resp := &resource.CreateResponse{
		State:    tfsdk.State{Schema: kitSchema(context.Background())},
		Identity: func() *tfsdk.ResourceIdentity { id := kitIdentity(t); return &id }(),
	}
	r.Create(context.Background(), resource.CreateRequest{Plan: tfsdk.Plan(plan)}, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("Create: %v", resp.Diagnostics)
	}
	if sent == nil {
		t.Fatal("Backend.CreateFields was never reached, so this asserts nothing")
	}
	if len(mask) == 0 {
		t.Fatal("the wire mask was empty, so this test asserts nothing about what was sent")
	}
	for _, name := range mask {
		if name != "name" {
			t.Errorf("the mask names %q; only fields the plan set may be created", name)
		}
	}
}

func TestCreateRefusesADescriptorThatDeclaresNoWriter(t *testing.T) {
	r := kitResource(Backend[kitSDK]{})
	plan := kitStateWith(t, kitModel{
		ID: types.StringNull(), Site: types.StringValue("default"),
		Name: types.StringValue("probe"),
	})
	resp := &resource.CreateResponse{
		State:    tfsdk.State{Schema: kitSchema(context.Background())},
		Identity: func() *tfsdk.ResourceIdentity { id := kitIdentity(t); return &id }(),
	}
	r.Create(context.Background(), resource.CreateRequest{Plan: tfsdk.Plan(plan)}, resp)

	if !resp.Diagnostics.HasError() {
		t.Fatal("a descriptor with neither Create nor CreateFields was accepted")
	}
	if !strings.Contains(resp.Diagnostics.Errors()[0].Detail(), "kit_probe") {
		t.Errorf("the error does not name the descriptor: %q",
			resp.Diagnostics.Errors()[0].Detail())
	}
}

func TestCreateRefusesADescriptorThatDeclaresBothWriters(t *testing.T) {
	reached := 0
	r := kitResource(Backend[kitSDK]{
		Create: func(_ context.Context, _ string, in *kitSDK) (*kitSDK, error) {
			reached++
			return in, nil
		},
		CreateFields: func(_ context.Context, _ string, in *kitSDK, _ ...string) (*kitSDK, error) {
			reached++
			return in, nil
		},
	})
	plan := kitStateWith(t, kitModel{
		ID: types.StringNull(), Site: types.StringValue("default"),
		Name: types.StringValue("probe"),
	})
	resp := &resource.CreateResponse{
		State:    tfsdk.State{Schema: kitSchema(context.Background())},
		Identity: func() *tfsdk.ResourceIdentity { id := kitIdentity(t); return &id }(),
	}
	r.Create(context.Background(), resource.CreateRequest{Plan: tfsdk.Plan(plan)}, resp)

	if !resp.Diagnostics.HasError() {
		t.Fatal("a descriptor declaring both writers was accepted")
	}
	if reached != 0 {
		t.Errorf("a writer ran %d time(s); neither may run when the descriptor is ambiguous", reached)
	}
}

func TestBeforeDeleteCanRefuseToDeleteTheObject(t *testing.T) {
	reached := 0
	r := kitResource(Backend[kitSDK]{
		Delete: func(context.Context, string, string) error {
			reached++
			return nil
		},
	})
	r.Spec.BeforeDelete = func(context.Context, *kitModel) (bool, diag.Diagnostics) {
		return false, nil
	}
	state := kitStateWith(t, kitModel{
		ID: types.StringValue("id-1"), Site: types.StringValue("default"),
		Name: types.StringValue("probe"),
	})
	resp := &resource.DeleteResponse{State: state}
	r.Delete(context.Background(), resource.DeleteRequest{State: state}, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("Delete: %v", resp.Diagnostics)
	}
	if reached != 0 {
		t.Errorf("Backend.Delete ran %d time(s) after BeforeDelete refused -- a "+
			"resource like a physical device can only be forgotten, never "+
			"deleted, and that has to stay opt-in", reached)
	}
}

func TestBeforeDeleteProceedsWhenItReturnsTrue(t *testing.T) {
	reached := 0
	r := kitResource(Backend[kitSDK]{
		Delete: func(context.Context, string, string) error {
			reached++
			return nil
		},
	})
	r.Spec.BeforeDelete = func(context.Context, *kitModel) (bool, diag.Diagnostics) {
		return true, nil
	}
	state := kitStateWith(t, kitModel{
		ID: types.StringValue("id-1"), Site: types.StringValue("default"),
		Name: types.StringValue("probe"),
	})
	resp := &resource.DeleteResponse{State: state}
	r.Delete(context.Background(), resource.DeleteRequest{State: state}, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("Delete: %v", resp.Diagnostics)
	}
	if reached != 1 {
		t.Errorf("Backend.Delete ran %d time(s), want exactly 1", reached)
	}
}

func TestBeforeDeleteErrorStopsTheDelete(t *testing.T) {
	reached := 0
	r := kitResource(Backend[kitSDK]{
		Delete: func(context.Context, string, string) error {
			reached++
			return nil
		},
	})
	r.Spec.BeforeDelete = func(context.Context, *kitModel) (bool, diag.Diagnostics) {
		var diags diag.Diagnostics
		diags.AddError("Cannot Delete", "the hook could not decide")
		return true, diags
	}
	state := kitStateWith(t, kitModel{
		ID: types.StringValue("id-1"), Site: types.StringValue("default"),
		Name: types.StringValue("probe"),
	})
	resp := &resource.DeleteResponse{State: state}
	r.Delete(context.Background(), resource.DeleteRequest{State: state}, resp)

	if !resp.Diagnostics.HasError() {
		t.Fatal("a failing BeforeDelete was ignored")
	}
	if reached != 0 {
		t.Errorf("Backend.Delete ran %d time(s) after BeforeDelete errored", reached)
	}
}

func TestUnwritableWiresIsSubtractedFromTheMask(t *testing.T) {
	var mask []string
	r := kitResource(Backend[kitSDK]{
		UpdateFields: func(_ context.Context, _ string, in *kitSDK, fields ...string) (*kitSDK, error) {
			mask = fields
			return in, nil
		},
	})
	r.Spec.UnwritableWires = func(*kitSDK) []string { return []string{"nonexistent", "name"} }

	state := kitStateWith(t, kitModel{
		ID: types.StringValue("id-1"), Site: types.StringValue("default"),
		Name: types.StringValue("before"),
	})
	plan := kitStateWith(t, kitModel{
		ID: types.StringValue("id-1"), Site: types.StringValue("default"),
		Name: types.StringValue("after"),
	})
	resp := &resource.UpdateResponse{
		State:    state,
		Identity: func() *tfsdk.ResourceIdentity { id := kitIdentity(t); return &id }(),
	}
	r.Update(context.Background(), resource.UpdateRequest{
		State: state, Plan: tfsdk.Plan(plan),
	}, resp)

	// "name" was the only field the plan set and the hook reported it
	// unwritable, so the mask empties and the write must be refused.
	if !resp.Diagnostics.HasError() {
		t.Fatalf("an update whose whole mask was reported unwritable was sent as %v", mask)
	}
	if !strings.Contains(resp.Diagnostics.Errors()[0].Detail(), "kit_probe") {
		t.Errorf("the error does not name the descriptor: %q",
			resp.Diagnostics.Errors()[0].Detail())
	}
}

func TestUnwritableWiresLeavesTheRestOfTheMaskAlone(t *testing.T) {
	var mask []string
	r := kitResource(Backend[kitSDK]{
		UpdateFields: func(_ context.Context, _ string, in *kitSDK, fields ...string) (*kitSDK, error) {
			mask = fields
			return in, nil
		},
	})
	// Reports a name that is not in this plan's mask at all: nothing to remove.
	r.Spec.UnwritableWires = func(*kitSDK) []string { return []string{"not_in_the_mask"} }

	state := kitStateWith(t, kitModel{
		ID: types.StringValue("id-1"), Site: types.StringValue("default"),
		Name: types.StringValue("before"),
	})
	plan := kitStateWith(t, kitModel{
		ID: types.StringValue("id-1"), Site: types.StringValue("default"),
		Name: types.StringValue("after"),
	})
	resp := &resource.UpdateResponse{
		State:    state,
		Identity: func() *tfsdk.ResourceIdentity { id := kitIdentity(t); return &id }(),
	}
	r.Update(context.Background(), resource.UpdateRequest{
		State: state, Plan: tfsdk.Plan(plan),
	}, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("Update: %v", resp.Diagnostics)
	}
	if len(mask) != 1 || mask[0] != "name" {
		t.Errorf("mask = %v, want just [name]; the hook named nothing that was in it", mask)
	}
}
