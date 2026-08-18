package resourcekit

import (
	"context"
	"errors"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
)

// THE KIT'S OWN TESTS, DRIVING THE REAL METHODS.
//
// internal/resourcekit had no test files at all until this commit: 527 lines of
// CRUD serving four surfaces, with every assertion about its behaviour reaching
// it through some surface's test. That is how firewall_zone's cutover nearly
// deleted the only check of not-found tolerance -- the behaviour had moved into
// shared code while its only test stayed in a file being replaced.
//
// These drive Resource.Create/Read/Update/Delete through real framework state,
// which is the part a hand-rolled assertion about a Backend closure would miss:
// the plumbing between the model and the wire is most of what the kit IS.

type kitModel struct {
	ID       types.String   `tfsdk:"id"`
	Site     types.String   `tfsdk:"site"`
	Name     types.String   `tfsdk:"name"`
	Timeouts timeouts.Value `tfsdk:"timeouts"`
}

type kitSDK struct {
	ID   string
	Name string
}

func kitSchema(ctx context.Context) schema.Schema {
	return schema.Schema{Attributes: map[string]schema.Attribute{
		"id":       schema.StringAttribute{Computed: true},
		"site":     schema.StringAttribute{Optional: true, Computed: true},
		"name":     schema.StringAttribute{Required: true},
		"timeouts": timeouts.Attributes(ctx, timeouts.Opts{Create: true, Read: true, Update: true, Delete: true}),
	}}
}

func kitTimeoutTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"create": types.StringType, "read": types.StringType,
		"update": types.StringType, "delete": types.StringType,
	}
}

// kitResource builds a Resource whose Backend is entirely under the test's
// control, so what the kit sends and what it does with the answer are both
// observable.
func kitResource(backend Backend[kitSDK]) *Resource[kitModel, kitSDK] {
	r := &Resource[kitModel, kitSDK]{}
	r.Spec = Spec[kitModel, kitSDK]{
		TypeName: "kit_probe",
		Subject:  "Kit Probe",
		New:      func() *kitSDK { return &kitSDK{} },
		ID:       func(m *kitModel) *types.String { return &m.ID },
		Site:     func(m *kitModel) *types.String { return &m.Site },
		Timeouts: func(m *kitModel) *timeouts.Value { return &m.Timeouts },
		Fields: []Field[kitModel, kitSDK]{
			StringField[kitModel, kitSDK]{
				Wire:  "name",
				Model: func(m *kitModel) *types.String { return &m.Name },
				SDK:   func(s *kitSDK) *string { return &s.Name },
				Elide: KeepZero,
			},
		},
		Backend: backend,
	}
	r.Spec.Backend.GetID = func(s *kitSDK) string { return s.ID }
	r.Spec.Backend.SetID = func(s *kitSDK, id string) { s.ID = id }
	r.DefaultSite = "default"
	return r
}

// kitIdentity builds the resource identity the kit writes to on Create and
// Update. Supplying it is not test scaffolding: Create and Update call
// resp.Identity.SetAttribute unconditionally, so a surface that reaches them
// without an identity schema fails at runtime -- which is worth knowing and is
// why the harness makes it explicit rather than working around it.
func kitIdentity(t *testing.T) tfsdk.ResourceIdentity {
	t.Helper()
	ctx := context.Background()
	r := &Resource[kitModel, kitSDK]{}
	resp := &resource.IdentitySchemaResponse{}
	r.IdentitySchema(ctx, resource.IdentitySchemaRequest{}, resp)
	identity := tfsdk.ResourceIdentity{Schema: resp.IdentitySchema}
	identity.Raw = tftypes.NewValue(resp.IdentitySchema.Type().TerraformType(ctx), nil)
	return identity
}

func kitStateWith(t *testing.T, model kitModel) tfsdk.State {
	t.Helper()
	ctx := context.Background()
	state := tfsdk.State{Schema: kitSchema(ctx)}
	model.Timeouts = timeouts.Value{Object: types.ObjectNull(kitTimeoutTypes())}
	if diags := state.Set(ctx, &model); diags.HasError() {
		t.Fatalf("build state: %v", diags)
	}
	return state
}

// TestDeleteTreatsAnAbsentObjectAsSuccess is the assertion that firewall_zone
// owned. James decided the semantics -- deleting something already gone
// succeeds, on every resource, with no per-resource flag -- and before this the
// only test of it lived on one surface.
func TestDeleteTreatsAnAbsentObjectAsSuccess(t *testing.T) {
	for _, testCase := range []struct {
		name      string
		deleteErr error
		wantError bool
	}{
		{"already gone", &ui.NotFoundError{}, false},
		{"deleted cleanly", nil, false},
		{"transport failure", errors.New("connection reset"), true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			called := false
			r := kitResource(Backend[kitSDK]{
				Delete: func(context.Context, string, string) error {
					called = true
					return testCase.deleteErr
				},
			})
			state := kitStateWith(t, kitModel{
				ID: types.StringValue("id-1"), Site: types.StringValue("default"),
				Name: types.StringValue("probe"),
			})
			resp := &resource.DeleteResponse{State: state}
			r.Delete(context.Background(), resource.DeleteRequest{State: state}, resp)

			if !called {
				t.Fatal("the backend Delete was never reached, so this case asserts nothing")
			}
			if got := resp.Diagnostics.HasError(); got != testCase.wantError {
				t.Errorf("HasError() = %v, want %v: %v", got, testCase.wantError, resp.Diagnostics)
			}
		})
	}
}

// TestReadRemovesAnObjectTheControllerNoLongerHas is the other half of
// not-found handling, and it is the direction that loses a practitioner's state
// rather than failing loudly if it is wrong.
func TestReadRemovesAnObjectTheControllerNoLongerHas(t *testing.T) {
	r := kitResource(Backend[kitSDK]{
		Read: func(context.Context, string, string) (*kitSDK, error) {
			return nil, &ui.NotFoundError{}
		},
	})
	state := kitStateWith(t, kitModel{
		ID: types.StringValue("id-1"), Site: types.StringValue("default"),
		Name: types.StringValue("probe"),
	})
	resp := &resource.ReadResponse{State: state}
	r.Read(context.Background(), resource.ReadRequest{State: state}, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("Read errored on an absent object: %v", resp.Diagnostics)
	}
	if !resp.State.Raw.IsNull() {
		t.Error("state survived a not-found Read; the resource should have been removed " +
			"so a later apply recreates it")
	}
}

// TestReadKeepsStateWhenTheControllerIsUnreachable is the case the one above
// would otherwise license. A transport failure must NOT look like a deletion:
// removing state there would destroy and recreate a live object because a
// network blipped.
func TestReadKeepsStateWhenTheControllerIsUnreachable(t *testing.T) {
	r := kitResource(Backend[kitSDK]{
		Read: func(context.Context, string, string) (*kitSDK, error) {
			return nil, errors.New("connection reset")
		},
	})
	state := kitStateWith(t, kitModel{
		ID: types.StringValue("id-1"), Site: types.StringValue("default"),
		Name: types.StringValue("probe"),
	})
	resp := &resource.ReadResponse{State: state}
	r.Read(context.Background(), resource.ReadRequest{State: state}, resp)

	if !resp.Diagnostics.HasError() {
		t.Error("a transport failure was reported as success")
	}
	if resp.State.Raw.IsNull() {
		t.Error("state was removed on a transport failure; a network blip would " +
			"destroy and recreate a live object")
	}
}

// TestCreateSendsTheModelAndKeepsTheReturnedID.
func TestCreateSendsTheModelAndKeepsTheReturnedID(t *testing.T) {
	var sent *kitSDK
	r := kitResource(Backend[kitSDK]{
		Create: func(_ context.Context, site string, in *kitSDK) (*kitSDK, error) {
			if site != "default" {
				t.Errorf("site = %q, want default", site)
			}
			sent = in
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
	r.Create(context.Background(),
		resource.CreateRequest{Plan: tfsdk.Plan{Schema: plan.Schema, Raw: plan.Raw}}, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("Create: %v", resp.Diagnostics)
	}
	if sent == nil {
		t.Fatal("the backend Create was never reached")
	}
	if sent.Name != "probe" {
		t.Errorf("the name did not reach the controller: %q", sent.Name)
	}
	var stored kitModel
	if diags := resp.State.Get(context.Background(), &stored); diags.HasError() {
		t.Fatalf("read back state: %v", diags)
	}
	if stored.ID.ValueString() != "assigned-by-controller" {
		t.Errorf("the controller-assigned id was not stored: %q", stored.ID.ValueString())
	}
}

// TestUpdateSendsOnlyTheFieldsThePlanSet is the write-amplification guard, and
// it is the assertion the whole masked-update design exists for.
//
// The kit builds the wire mask from SetInPlan, so a field the practitioner
// never mentioned is not named and therefore not overwritten. A whole-object
// PUT would clobber attributes nobody asked to change -- which is #121's defect
// class, and the reason a plain Update must not be wrapped into this signature.
func TestUpdateSendsOnlyTheFieldsThePlanSet(t *testing.T) {
	var mask []string
	r := kitResource(Backend[kitSDK]{
		UpdateFields: func(_ context.Context, _ string, in *kitSDK, fields ...string) (*kitSDK, error) {
			mask = fields
			return in, nil
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
	resp := &resource.UpdateResponse{
		State:    state,
		Identity: func() *tfsdk.ResourceIdentity { id := kitIdentity(t); return &id }(),
	}
	r.Update(ctx, resource.UpdateRequest{
		State: state,
		Plan:  tfsdk.Plan{Schema: plan.Schema, Raw: plan.Raw},
	}, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("Update: %v", resp.Diagnostics)
	}
	if len(mask) == 0 {
		t.Fatal("the wire mask was empty, so this test asserts nothing about what was sent")
	}
	for _, name := range mask {
		if name != "name" {
			t.Errorf("the mask names %q; only fields the plan set may be sent", name)
		}
	}
}
