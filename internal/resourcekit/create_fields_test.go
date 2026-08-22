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

// A SURFACE WHOSE CREATE IS A PATCH NEEDS THE MASK, AND CREATE NEVER GOT ONE.
//
// Backend.Create takes the whole object because for every surface here create
// means POST: the controller has nothing yet, and an unset field takes a
// default rather than overwriting a live value. unifi_device breaks that. A
// device is ADOPTED, so the object already carries its full config when
// Terraform first names it, and ToSDK builds from the PLAN -- every attribute
// the practitioner left out is elided to its zero value. A whole-object create
// would asserts those zeros over the config the device already has.
//
// These pin the mask to the same rule Update uses, so a create cannot quietly
// widen into a whole-object write.
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

// A descriptor with neither would nil-panic at the send: a stack trace pointing
// at the kit rather than a diagnostic pointing at the descriptor that is wrong.
// The update path has guarded this since it grew a second writer; create only
// needed it once CreateFields existed.
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

// Both set is ambiguous about what the provider may overwrite, which is exactly
// the decision the masked/unmasked split exists to make explicit. Refuse rather
// than pick.
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

// DESTROYING A RESOURCE IS NOT ALWAYS DESTROYING A THING.
//
// unifi_device is physical: the provider cannot delete one, only forget it,
// which unadopts real hardware. The schema makes that opt-in through
// forget_on_destroy, and Backend.Delete takes site and id so it cannot see the
// attribute that decides. Without the hook every destroy would forget the
// device -- these pin that a refusal reaches the backend as silence.
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
		t.Errorf("Backend.Delete ran %d time(s) after BeforeDelete refused", reached)
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

// A hook that fails must not fall through to the destructive call.
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
