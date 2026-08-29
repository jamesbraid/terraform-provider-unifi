package resourcekit

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
)

type kitModel struct {
	ID       types.String   `tfsdk:"id"`
	Site     types.String   `tfsdk:"site"`
	Name     types.String   `tfsdk:"name"`
	Timeouts timeouts.Value `tfsdk:"timeouts"`
	// Nested is not in kitSchema: the object field's tests drive it directly
	// rather than through framework state, and adding it to the schema would
	// change every other test's state fixture.
	Nested types.Object `tfsdk:"-"`
}

type kitSDK struct {
	ID   string
	Name string
	// Unmanaged is deliberately NOT a Field in the spec: it stands for a
	// controller-owned value the provider does not model, useful for proving
	// a hook's mutation survives to what's actually sent.
	Unmanaged string
	// Nested carries a real SDK nested type, so the object field's tests run
	// against the same struct firewall_policy sends rather than a stand-in.
	Nested *ui.FirewallPolicySource
}

func kitSchema(ctx context.Context) schema.Schema {
	return schema.Schema{Attributes: map[string]schema.Attribute{
		"id":   schema.StringAttribute{Computed: true},
		"site": schema.StringAttribute{Optional: true, Computed: true},
		"name": schema.StringAttribute{Required: true},
		"timeouts": timeouts.Attributes(
			ctx,
			timeouts.Opts{Create: true, Read: true, Update: true, Delete: true},
		),
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
// Update. Not test scaffolding: Create and Update call
// resp.Identity.SetAttribute unconditionally, so a test resource needs one to
// avoid a runtime panic.
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
		resource.CreateRequest{Plan: tfsdk.Plan(plan)}, resp)

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
		Plan:  tfsdk.Plan(plan),
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

func hookSpy(t *testing.T) (Spec[kitModel, kitSDK], *map[string]int) {
	t.Helper()
	seen := map[string]int{}
	spec := Spec[kitModel, kitSDK]{
		Prefetch: func(context.Context, string) (any, diag.Diagnostics) {
			seen["prefetch"]++
			return "inventory", nil
		},
		BeforeSend: func(_ context.Context, _, _ *kitModel, _ kitModel, _ *kitSDK, prefetched any) diag.Diagnostics {
			seen["beforeSend"]++
			if prefetched != "inventory" {
				t.Errorf(
					"BeforeSend got prefetched = %v; the hooks are wired but not connected",
					prefetched,
				)
			}
			return nil
		},
		AfterReceive: func(_ context.Context, _ *kitSDK, _ *kitModel, _ kitModel, prefetched any) diag.Diagnostics {
			seen["afterReceive"]++
			if prefetched != "inventory" {
				t.Errorf("AfterReceive got prefetched = %v", prefetched)
			}
			return nil
		},
	}
	return spec, &seen
}

func withHooks(
	t *testing.T,
	backend Backend[kitSDK],
) (*Resource[kitModel, kitSDK], *map[string]int) {
	t.Helper()
	r := kitResource(backend)
	hooks, seen := hookSpy(t)
	r.Spec.Prefetch, r.Spec.BeforeSend, r.Spec.AfterReceive = hooks.Prefetch, hooks.BeforeSend, hooks.AfterReceive
	return r, seen
}

func TestUpdateRunsAllThreeHooks(t *testing.T) {
	ctx := context.Background()
	r, seen := withHooks(t, Backend[kitSDK]{
		UpdateFields: func(_ context.Context, _ string, in *kitSDK, _ ...string) (*kitSDK, error) {
			return in, nil
		},
	})
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
		State:  state,
		Plan:   tfsdk.Plan(plan),
		Config: tfsdk.Config(plan),
	}, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("Update: %v", resp.Diagnostics)
	}
	for _, hook := range []string{"prefetch", "beforeSend", "afterReceive"} {
		if (*seen)[hook] == 0 {
			t.Errorf("Update never called %s; a surface deriving part of its wire form "+
				"would create correctly and then silently stop", hook)
		}
	}
}

func TestReadRunsPrefetchAndAfterReceive(t *testing.T) {
	ctx := context.Background()
	r, seen := withHooks(t, Backend[kitSDK]{
		Read: func(context.Context, string, string) (*kitSDK, error) {
			return &kitSDK{ID: "id-1", Name: "probe"}, nil
		},
	})
	state := kitStateWith(t, kitModel{
		ID: types.StringValue("id-1"), Site: types.StringValue("default"),
		Name: types.StringValue("probe"),
	})
	resp := &resource.ReadResponse{
		State:    state,
		Identity: func() *tfsdk.ResourceIdentity { id := kitIdentity(t); return &id }(),
	}
	r.Read(ctx, resource.ReadRequest{State: state}, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("Read: %v", resp.Diagnostics)
	}
	if (*seen)["prefetch"] == 0 || (*seen)["afterReceive"] == 0 {
		t.Errorf("Read called prefetch=%d afterReceive=%d; a model attribute the field "+
			"list cannot express would be populated on create and blank on refresh",
			(*seen)["prefetch"], (*seen)["afterReceive"])
	}
	// BeforeSend must NOT run on Read: there is nothing being sent.
	if (*seen)["beforeSend"] != 0 {
		t.Error("Read called BeforeSend, which sends nothing")
	}
}

func TestWireFieldsCarriesTheFieldsAHookDerives(t *testing.T) {
	spec := kitResource(Backend[kitSDK]{}).Spec
	planWithNothingSet := &kitModel{Name: types.StringNull()}

	if _, err := spec.WireFields(planWithNothingSet); err == nil {
		t.Fatal("an empty plan produced a mask, so the assertion below proves nothing")
	}

	spec.AlwaysWire = []string{"name"}
	fields, err := spec.WireFields(planWithNothingSet)
	if err != nil {
		t.Fatalf("WireFields: %v", err)
	}
	if len(fields) != 1 || fields[0] != "name" {
		t.Fatalf("mask = %v, want [name] from AlwaysWire alone", fields)
	}

	// And it must not duplicate a field the plan already set, because
	// WireFields refuses a mask naming anything twice.
	planWithNameSet := &kitModel{Name: types.StringValue("x")}
	fields, err = spec.WireFields(planWithNameSet)
	if err != nil {
		t.Fatalf("a field both planned and declared produced an error: %v", err)
	}
	if len(fields) != 1 {
		t.Errorf("mask = %v, want the field named once", fields)
	}
}

func TestBeforeSendGetsTheModelTheObjectWasBuiltFrom(t *testing.T) {
	ctx := context.Background()

	var sawConfig, sawEffective string
	r := kitResource(Backend[kitSDK]{
		Read: func(context.Context, string, string) (*kitSDK, error) {
			return &kitSDK{ID: "id-1", Name: "from-state"}, nil
		},
		UpdateFields: func(_ context.Context, _ string, in *kitSDK, _ ...string) (*kitSDK, error) {
			return in, nil
		},
	})
	// The plan sets nothing, so the mask would be empty and Update would fail
	// before reaching the hook. AlwaysWire keeps the write legal without
	// putting a value in the plan, which is exactly the case under test.
	r.Spec.AlwaysWire = []string{"name"}
	r.Spec.BeforeSend = func(_ context.Context, config, effective *kitModel, _ kitModel, sdk *kitSDK, _ any) diag.Diagnostics {
		sawConfig = config.Name.ValueString()
		sawEffective = effective.Name.ValueString()
		return nil
	}

	// The plan leaves name alone; state carries it. ApplyPlanToState therefore
	// keeps the state value, and that is what ToSDK sends.
	state := kitStateWith(t, kitModel{
		ID: types.StringValue("id-1"), Site: types.StringValue("default"),
		Name: types.StringValue("from-state"),
	})
	plan := kitStateWith(t, kitModel{
		ID: types.StringValue("id-1"), Site: types.StringValue("default"),
		Name: types.StringNull(),
	})
	identity := kitIdentity(t)
	resp := &resource.UpdateResponse{
		State:    tfsdk.State{Schema: kitSchema(ctx)},
		Identity: &identity,
	}
	r.Update(ctx, resource.UpdateRequest{
		State:  state,
		Plan:   tfsdk.Plan(plan),
		Config: tfsdk.Config(plan),
	}, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("update failed, so the hook may never have run: %v", resp.Diagnostics)
	}
	if sawEffective == "" && sawConfig == "" {
		t.Fatal(
			"BeforeSend did not run at all; the assertions below would pass for the wrong reason",
		)
	}
	if sawEffective != "from-state" {
		t.Errorf("effective.Name = %q, want %q -- the hook must see what ToSDK sent, "+
			"or a derived value is recomputed from an attribute the plan left alone",
			sawEffective, "from-state")
	}
	// The control: config still reports the absence, or the two arguments
	// would be the same thing and the distinction would be untested.
	if sawConfig != "" {
		t.Errorf("config.Name = %q, want empty -- config is what the practitioner wrote", sawConfig)
	}
}

func TestUpdateRefusesABackendThatCannotWrite(t *testing.T) {
	r := kitResource(Backend[kitSDK]{})
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
	r.Update(ctx, resource.UpdateRequest{State: state, Plan: tfsdk.Plan(plan)}, resp)

	if !resp.Diagnostics.HasError() {
		t.Fatal("a backend with no UpdateFields was accepted")
	}
}

func TestBeforeSendRunsOnTheObjectThatIsActuallySent(t *testing.T) {
	var sent *kitSDK
	r := kitResource(Backend[kitSDK]{
		UpdateFields: func(_ context.Context, _ string, in *kitSDK, _ ...string) (*kitSDK, error) {
			sent = in
			return in, nil
		},
	})
	r.Spec.BeforeSend = func(
		_ context.Context, _, _ *kitModel, _ kitModel, sdk *kitSDK, _ any,
	) diag.Diagnostics {
		sdk.Unmanaged = "derived-by-hook"
		return nil
	}

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
		State: state, Plan: tfsdk.Plan(plan), Config: tfsdk.Config(plan),
	}, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("Update: %v", resp.Diagnostics)
	}
	if sent == nil {
		t.Fatal("UpdateFields was never called, so this asserts nothing")
	}
	if sent.Unmanaged != "derived-by-hook" {
		t.Errorf("Unmanaged = %q, want the hook's value. BeforeSend must run on the "+
			"object ToSDK produced and before it is sent, or everything it derives "+
			"is discarded", sent.Unmanaged)
	}
}

// BeforeSend has one signature for both writes; the only way a hook tells
// them apart is the effective model's ID: empty on create (a Computed id
// reads as unknown), the controller's real id on update. firewall_policy's
// schedule hook depends on exactly this, and getting it backwards silently
// resets a practitioner's schedule with no diff to show for it.
func TestBeforeSendSeesAnEmptyIDOnCreateAndTheRealOneOnUpdate(t *testing.T) {
	ctx := context.Background()

	newHook := func(seen *[]string) func(context.Context, *kitModel, *kitModel, kitModel, *kitSDK, any) diag.Diagnostics {
		return func(_ context.Context, _, effective *kitModel, _ kitModel, _ *kitSDK, _ any) diag.Diagnostics {
			*seen = append(*seen, effective.ID.ValueString())
			return nil
		}
	}

	var onCreate []string
	create := kitResource(Backend[kitSDK]{
		Create: func(_ context.Context, _ string, in *kitSDK) (*kitSDK, error) {
			return &kitSDK{ID: "assigned-by-controller", Name: in.Name}, nil
		},
	})
	create.Spec.BeforeSend = newHook(&onCreate)
	plan := kitStateWith(t, kitModel{
		ID: types.StringNull(), Site: types.StringValue("default"),
		Name: types.StringValue("probe"),
	})
	createIdentity := kitIdentity(t)
	createResp := &resource.CreateResponse{
		State: tfsdk.State{Schema: kitSchema(ctx)}, Identity: &createIdentity,
	}
	create.Create(ctx, resource.CreateRequest{
		Plan: tfsdk.Plan(plan), Config: tfsdk.Config(plan),
	}, createResp)
	if createResp.Diagnostics.HasError() {
		t.Fatalf("Create: %v", createResp.Diagnostics)
	}

	var onUpdate []string
	update := kitResource(Backend[kitSDK]{
		Read: func(context.Context, string, string) (*kitSDK, error) {
			return &kitSDK{ID: "id-1", Name: "from-state"}, nil
		},
		UpdateFields: func(_ context.Context, _ string, in *kitSDK, _ ...string) (*kitSDK, error) {
			return in, nil
		},
	})
	update.Spec.BeforeSend = newHook(&onUpdate)
	updateState := kitStateWith(t, kitModel{
		ID: types.StringValue("id-1"), Site: types.StringValue("default"),
		Name: types.StringValue("from-state"),
	})
	updatePlan := kitStateWith(t, kitModel{
		ID: types.StringValue("id-1"), Site: types.StringValue("default"),
		Name: types.StringValue("renamed"),
	})
	updateIdentity := kitIdentity(t)
	updateResp := &resource.UpdateResponse{
		State: tfsdk.State{Schema: kitSchema(ctx)}, Identity: &updateIdentity,
	}
	update.Update(ctx, resource.UpdateRequest{
		State: updateState, Plan: tfsdk.Plan(updatePlan), Config: tfsdk.Config(updatePlan),
	}, updateResp)
	if updateResp.Diagnostics.HasError() {
		t.Fatalf("Update: %v", updateResp.Diagnostics)
	}

	if len(onCreate) != 1 || len(onUpdate) != 1 {
		t.Fatalf("BeforeSend ran %d time(s) on create and %d on update, want 1 each; "+
			"the assertions below would otherwise pass vacuously", len(onCreate), len(onUpdate))
	}
	if onCreate[0] != "" {
		t.Errorf("BeforeSend saw id %q on create, want empty. A hook that reads the id "+
			"to mean \"this is an update\" would fetch an object that does not exist yet",
			onCreate[0])
	}
	if onUpdate[0] != "id-1" {
		t.Errorf("BeforeSend saw id %q on update, want id-1. A hook that carries a "+
			"controller-owned field forward could not find the object to carry it from",
			onUpdate[0])
	}
}

// TestBeforeSendSeesThePriorOnUpdateAndTheZeroValueOnCreate pins prior the
// same way TestBeforeSendSeesAnEmptyIDOnCreateAndTheRealOneOnUpdate pins
// effective's id: on update, prior is state as it stood BEFORE the plan was
// applied, not effective (which already has the plan merged in) -- a mapper
// that needs to know what the controller held before this write, to clear
// something the new plan dropped, has nowhere else to read it. On create
// there is no prior state, so prior is the zero model, matching
// AfterReceive's own zero-prior case.
func TestBeforeSendSeesThePriorOnUpdateAndTheZeroValueOnCreate(t *testing.T) {
	ctx := context.Background()

	newHook := func(seen *[]kitModel) func(context.Context, *kitModel, *kitModel, kitModel, *kitSDK, any) diag.Diagnostics {
		return func(_ context.Context, _, _ *kitModel, prior kitModel, _ *kitSDK, _ any) diag.Diagnostics {
			*seen = append(*seen, prior)
			return nil
		}
	}

	var onCreate []kitModel
	create := kitResource(Backend[kitSDK]{
		Create: func(_ context.Context, _ string, in *kitSDK) (*kitSDK, error) {
			return &kitSDK{ID: "assigned-by-controller", Name: in.Name}, nil
		},
	})
	create.Spec.BeforeSend = newHook(&onCreate)
	createPlan := kitStateWith(t, kitModel{
		ID: types.StringNull(), Site: types.StringValue("default"),
		Name: types.StringValue("what-the-plan-said"),
	})
	createIdentity := kitIdentity(t)
	createResp := &resource.CreateResponse{
		State: tfsdk.State{Schema: kitSchema(ctx)}, Identity: &createIdentity,
	}
	create.Create(ctx, resource.CreateRequest{
		Plan: tfsdk.Plan(createPlan), Config: tfsdk.Config(createPlan),
	}, createResp)
	if createResp.Diagnostics.HasError() {
		t.Fatalf("Create: %v", createResp.Diagnostics)
	}

	var onUpdate []kitModel
	update := kitResource(Backend[kitSDK]{
		UpdateFields: func(_ context.Context, _ string, in *kitSDK, _ ...string) (*kitSDK, error) {
			return in, nil
		},
	})
	update.Spec.BeforeSend = newHook(&onUpdate)
	updateState := kitStateWith(t, kitModel{
		ID: types.StringValue("id-1"), Site: types.StringValue("default"),
		Name: types.StringValue("what-state-held-before"),
	})
	updatePlan := kitStateWith(t, kitModel{
		ID: types.StringValue("id-1"), Site: types.StringValue("default"),
		Name: types.StringValue("what-the-plan-changed-it-to"),
	})
	updateIdentity := kitIdentity(t)
	updateResp := &resource.UpdateResponse{
		State: tfsdk.State{Schema: kitSchema(ctx)}, Identity: &updateIdentity,
	}
	update.Update(ctx, resource.UpdateRequest{
		State: updateState, Plan: tfsdk.Plan(updatePlan), Config: tfsdk.Config(updatePlan),
	}, updateResp)
	if updateResp.Diagnostics.HasError() {
		t.Fatalf("Update: %v", updateResp.Diagnostics)
	}

	if len(onCreate) != 1 || len(onUpdate) != 1 {
		t.Fatalf("BeforeSend ran %d time(s) on create and %d on update, want 1 each; "+
			"the assertions below would otherwise pass vacuously", len(onCreate), len(onUpdate))
	}
	if !onCreate[0].Name.IsNull() {
		t.Errorf("prior.name on create = %q, want null (the zero model) -- there is no "+
			"prior state to report before the object exists", onCreate[0].Name.ValueString())
	}
	if got := onUpdate[0].Name.ValueString(); got != "what-state-held-before" {
		t.Errorf("prior.name on update = %q, want %q -- prior must be state as it stood "+
			"before ApplyPlanToState merged the plan in, or a mapper reading it to see "+
			"what the controller held before this write sees the new value instead",
			got, "what-state-held-before")
	}
}

func captureAfterReceivePrior(r *Resource[kitModel, kitSDK]) *kitModel {
	var captured kitModel
	r.Spec.AfterReceive = func(
		_ context.Context, _ *kitSDK, _ *kitModel, prior kitModel, _ any,
	) diag.Diagnostics {
		captured = prior
		return nil
	}
	return &captured
}

func TestCreateHandsAfterReceiveThePlan(t *testing.T) {
	ctx := context.Background()
	r := kitResource(Backend[kitSDK]{
		Create: func(_ context.Context, _ string, in *kitSDK) (*kitSDK, error) {
			out := *in
			out.ID = "id-1"
			out.Name = "what-the-controller-chose"
			return &out, nil
		},
	})
	captured := captureAfterReceivePrior(r)

	plan := kitStateWith(t, kitModel{
		Site: types.StringValue("default"), Name: types.StringValue("what-the-plan-said"),
	})
	identity := kitIdentity(t)
	resp := &resource.CreateResponse{
		State: tfsdk.State{Schema: kitSchema(ctx)}, Identity: &identity,
	}
	r.Create(ctx, resource.CreateRequest{Plan: tfsdk.Plan(plan)}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("Create: %v", resp.Diagnostics)
	}
	if got := captured.Name.ValueString(); got != "what-the-plan-said" {
		t.Errorf("prior.name = %q, want the planned value; a hook carrying a value "+
			"forward from the configuration has nowhere else to read it", got)
	}
}

func TestReadHandsAfterReceiveThePriorState(t *testing.T) {
	ctx := context.Background()
	r := kitResource(Backend[kitSDK]{
		Read: func(context.Context, string, string) (*kitSDK, error) {
			return &kitSDK{ID: "id-1", Name: "what-the-controller-reports"}, nil
		},
	})
	captured := captureAfterReceivePrior(r)

	state := kitStateWith(t, kitModel{
		ID: types.StringValue("id-1"), Site: types.StringValue("default"),
		Name: types.StringValue("what-state-recorded"),
	})
	identity := kitIdentity(t)
	resp := &resource.ReadResponse{State: state, Identity: &identity}
	r.Read(ctx, resource.ReadRequest{State: state}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("Read: %v", resp.Diagnostics)
	}
	if got := captured.Name.ValueString(); got != "what-state-recorded" {
		t.Errorf("prior.name = %q, want what state held before the refresh", got)
	}
}

func TestUpdateHandsAfterReceiveTheEffectiveModelNotTheRawPlan(t *testing.T) {
	ctx := context.Background()
	r := kitResource(Backend[kitSDK]{
		UpdateFields: func(_ context.Context, _ string, in *kitSDK, _ ...string) (*kitSDK, error) {
			out := *in
			out.Name = "what-the-controller-reports"
			return &out, nil
		},
	})
	// A second field, so the mask is not empty: the kit refuses a patch that
	// names nothing, and this test needs an apply that changes something
	// while leaving name alone. site carries it.
	r.Spec.Fields = append(r.Spec.Fields, StringField[kitModel, kitSDK]{
		Wire:  "unmanaged",
		Model: func(m *kitModel) *types.String { return &m.Site },
		SDK:   func(s *kitSDK) *string { return &s.Unmanaged },
		Elide: KeepZero,
	})
	captured := captureAfterReceivePrior(r)

	state := kitStateWith(t, kitModel{
		ID: types.StringValue("id-1"), Site: types.StringValue("default"),
		Name: types.StringValue("what-state-recorded"),
	})
	// The plan leaves name unknown, which is what the framework produces for
	// an attribute an apply does not change and the provider may recompute.
	plan := kitStateWith(t, kitModel{
		ID: types.StringValue("id-1"), Site: types.StringValue("default"),
		Name: types.StringUnknown(),
	})
	identity := kitIdentity(t)
	resp := &resource.UpdateResponse{
		State: tfsdk.State{Schema: kitSchema(ctx)}, Identity: &identity,
	}
	r.Update(ctx, resource.UpdateRequest{
		Plan: tfsdk.Plan(plan), State: state,
	}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("Update: %v", resp.Diagnostics)
	}
	switch got := captured.Name; {
	case got.IsUnknown():
		t.Error("prior.name is unknown, so Update handed the RAW PLAN; an attribute " +
			"the apply did not mention reads as cleared and a hook carrying it " +
			"forward drops it")
	case got.ValueString() != "what-state-recorded":
		t.Errorf("prior.name = %q, want what the object was actually built from",
			got.ValueString())
	}
}

// sectionSpec stands in for a composite's per-section Spec
// (r2b-settings-composite, task 1): no ID, Site or Timeouts, since the
// composite owns id = site and there is no per-section timeouts block.
func sectionSpec() Spec[kitModel, kitSDK] {
	return Spec[kitModel, kitSDK]{
		TypeName: "kit_probe",
		Subject:  "Kit Probe",
		New:      func() *kitSDK { return &kitSDK{} },
		Fields: []Field[kitModel, kitSDK]{
			StringField[kitModel, kitSDK]{
				Wire:  "name",
				Model: func(m *kitModel) *types.String { return &m.Name },
				SDK:   func(s *kitSDK) *string { return &s.Name },
				Elide: KeepZero,
			},
		},
	}
}

func TestSpecWithNoIDSiteOrTimeoutsRoundTripsWithoutPanicking(t *testing.T) {
	spec := sectionSpec()
	ctx := context.Background()

	plan := &kitModel{Name: types.StringValue("configured")}
	sdk, diags := spec.ToSDK(ctx, plan)
	if diags.HasError() {
		t.Fatalf("ToSDK: %v", diags)
	}
	fields, err := spec.WireFields(plan)
	if err != nil {
		t.Fatalf("WireFields: %v", err)
	}
	if len(fields) != 1 || fields[0] != "name" {
		t.Fatalf("WireFields = %v, want [name]", fields)
	}

	state := &kitModel{}
	diags = spec.ToModel(ctx, sdk, state, "unused-site")
	if diags.HasError() {
		t.Fatalf("ToModel: %v", diags)
	}
	if state.Name.ValueString() != "configured" {
		t.Errorf("name = %v, want configured; a section Spec's Fields must still decode", state.Name)
	}

	spec.ApplyPlanToState(plan, state)
	if state.Name.ValueString() != "configured" {
		t.Errorf("name = %v after ApplyPlanToState, want configured", state.Name)
	}
}

func TestToModelLeavesIDAndSiteAloneWhenTheSpecOwnsNeither(t *testing.T) {
	spec := sectionSpec()
	sdk := &kitSDK{ID: "controller-id", Name: "from-controller"}
	model := &kitModel{
		ID:   types.StringValue("untouched-id"),
		Site: types.StringValue("untouched-site"),
		Name: types.StringValue("stale"),
	}
	diags := spec.ToModel(context.Background(), sdk, model, "some-site")
	if diags.HasError() {
		t.Fatalf("ToModel: %v", diags)
	}
	if model.Name.ValueString() != "from-controller" {
		t.Errorf("name = %v, want from-controller; the Field-decoded attribute must still land",
			model.Name)
	}
	if model.ID.ValueString() != "untouched-id" {
		t.Errorf("id = %v, want untouched-id; a Spec with no ID accessor has nowhere to write "+
			"one and must not touch it", model.ID)
	}
	if model.Site.ValueString() != "untouched-site" {
		t.Errorf("site = %v, want untouched-site; a Spec with no Site accessor has nowhere to "+
			"write one and must not touch it", model.Site)
	}
}

// TestToModelWritesIDAndSiteWhenTheSpecOwnsBoth is the positive control: a
// whole-resource Spec must keep writing id and site exactly as before.
func TestToModelWritesIDAndSiteWhenTheSpecOwnsBoth(t *testing.T) {
	spec := sectionSpec()
	spec.ID = func(m *kitModel) *types.String { return &m.ID }
	spec.Site = func(m *kitModel) *types.String { return &m.Site }
	spec.Backend.GetID = func(s *kitSDK) string { return s.ID }

	sdk := &kitSDK{ID: "controller-id", Name: "from-controller"}
	model := &kitModel{}
	diags := spec.ToModel(context.Background(), sdk, model, "some-site")
	if diags.HasError() {
		t.Fatalf("ToModel: %v", diags)
	}
	if model.ID.ValueString() != "controller-id" {
		t.Errorf("id = %v, want controller-id", model.ID)
	}
	if model.Site.ValueString() != "some-site" {
		t.Errorf("site = %v, want some-site", model.Site)
	}
}

// TestResourceRefusesASpecMissingIDSiteOrTimeouts is Resource's side of the
// same change: a section Spec may omit these, but Resource (the
// whole-resource server) dereferences all three directly, so a nil one is a
// descriptor defect that must fail with a clear message rather than panic.
// Mirrors TestCreateRefusesADescriptorThatDeclaresNoWriter's style.
func TestResourceRefusesASpecMissingIDSiteOrTimeouts(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		nilOut func(*Resource[kitModel, kitSDK])
	}{
		{"ID", func(r *Resource[kitModel, kitSDK]) { r.Spec.ID = nil }},
		{"Site", func(r *Resource[kitModel, kitSDK]) { r.Spec.Site = nil }},
		{"Timeouts", func(r *Resource[kitModel, kitSDK]) { r.Spec.Timeouts = nil }},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			r := kitResource(Backend[kitSDK]{
				Create: func(_ context.Context, _ string, in *kitSDK) (*kitSDK, error) {
					return in, nil
				},
			})
			testCase.nilOut(r)
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
				t.Fatalf("a Resource with a nil Spec.%s was accepted", testCase.name)
			}
			if !strings.Contains(resp.Diagnostics.Errors()[0].Detail(), "kit_probe") {
				t.Errorf("the error does not name the descriptor: %q",
					resp.Diagnostics.Errors()[0].Detail())
			}
			if !strings.Contains(resp.Diagnostics.Errors()[0].Detail(), testCase.name) {
				t.Errorf("the error does not name the missing accessor %q: %q",
					testCase.name, resp.Diagnostics.Errors()[0].Detail())
			}
		})
	}
}

// TestReadUpdateDeleteRefuseASpecMissingTimeouts proves the same guard
// applies to every operation, not only Create: Timeouts is the first of the
// three accessors each of them dereferences.
func TestReadUpdateDeleteRefuseASpecMissingTimeouts(t *testing.T) {
	state := kitStateWith(t, kitModel{
		ID: types.StringValue("id-1"), Site: types.StringValue("default"),
		Name: types.StringValue("probe"),
	})

	t.Run("Read", func(t *testing.T) {
		r := kitResource(Backend[kitSDK]{
			Read: func(context.Context, string, string) (*kitSDK, error) {
				return &kitSDK{ID: "id-1", Name: "probe"}, nil
			},
		})
		r.Spec.Timeouts = nil
		resp := &resource.ReadResponse{State: state}
		r.Read(context.Background(), resource.ReadRequest{State: state}, resp)
		if !resp.Diagnostics.HasError() {
			t.Fatal("a Resource with a nil Spec.Timeouts was accepted by Read")
		}
	})

	t.Run("Update", func(t *testing.T) {
		r := kitResource(Backend[kitSDK]{
			UpdateFields: func(_ context.Context, _ string, in *kitSDK, _ ...string) (*kitSDK, error) {
				return in, nil
			},
		})
		r.Spec.Timeouts = nil
		identity := kitIdentity(t)
		resp := &resource.UpdateResponse{State: state, Identity: &identity}
		r.Update(context.Background(), resource.UpdateRequest{State: state, Plan: tfsdk.Plan(state)}, resp)
		if !resp.Diagnostics.HasError() {
			t.Fatal("a Resource with a nil Spec.Timeouts was accepted by Update")
		}
	})

	t.Run("Delete", func(t *testing.T) {
		r := kitResource(Backend[kitSDK]{
			Delete: func(context.Context, string, string) error { return nil },
		})
		r.Spec.Timeouts = nil
		resp := &resource.DeleteResponse{State: state}
		r.Delete(context.Background(), resource.DeleteRequest{State: state}, resp)
		if !resp.Diagnostics.HasError() {
			t.Fatal("a Resource with a nil Spec.Timeouts was accepted by Delete")
		}
	})
}
