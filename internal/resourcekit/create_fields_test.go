package resourcekit

import (
	"context"
	"strings"
	"testing"

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
