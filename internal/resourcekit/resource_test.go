package resourcekit

import (
	"context"
	"errors"
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
	// Nested is not in kitSchema: the object field's tests drive it directly
	// rather than through framework state, and adding it to the schema would
	// change every other test's state fixture.
	Nested types.Object `tfsdk:"-"`
}

type kitSDK struct {
	ID   string
	Name string
	// Unmanaged is deliberately NOT a Field in the spec: it stands for every
	// controller-owned value the provider does not model, which is what a
	// whole-object write built from the model resets.
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

// THE THREE HOOKS RAN ON CREATE ONLY, AND NOTHING SAID SO.
//
// Prefetch, BeforeSend and AfterReceive were declared, documented for
// port_profile's tagged-network inversion, and wired into Create alone. A
// surface using them would have derived its wire form correctly on the first
// apply and silently stopped on every update, and had its model populated on
// create and blanked on every refresh. That is worse than no hook: the first
// apply looks right and the second sends a different object.
//
// Nothing used them, so nothing failed -- which is why the asymmetry survived.
// These assert which hooks run where, so removing a call site fails rather than
// waiting for the first surface that needs one.
func hookSpy(t *testing.T) (Spec[kitModel, kitSDK], *map[string]int) {
	t.Helper()
	seen := map[string]int{}
	spec := Spec[kitModel, kitSDK]{
		Prefetch: func(context.Context, string) (any, diag.Diagnostics) {
			seen["prefetch"]++
			return "inventory", nil
		},
		BeforeSend: func(_ context.Context, _, _ *kitModel, _ *kitSDK, prefetched any) diag.Diagnostics {
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

// AlwaysWire is what carries a hook-derived value onto the wire. Without it a
// practitioner changes an attribute that IS in the plan while the attributes
// carrying the change are not, and the update writes nothing.
func TestWireFieldsCarriesTheFieldsAHookDerives(t *testing.T) {
	spec := kitResource(Backend[kitSDK]{}).Spec
	planWithNothingSet := &kitModel{Name: types.StringNull()}

	// The control: with nothing planned and nothing declared, there is no mask
	// at all -- so the case below cannot pass by the field being there anyway.
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

// BeforeSend's two models answer different questions, and on an update they
// differ: config is what the practitioner wrote, effective is what the SDK
// object was built from.
//
// This is not a stylistic distinction. radius_user derives an account's VLAN
// from network_id whenever vlan is not set; against the raw plan an unchanged
// vlan reads as unset, so a VLAN the practitioner had pinned would be silently
// re-derived on the next apply that touched any other attribute.
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
	r.Spec.BeforeSend = func(_ context.Context, config, effective *kitModel, sdk *kitSDK, _ any) diag.Diagnostics {
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

// TestWholeObjectUpdateStartsFromTheFetchedObject covers the path for the five
// SDK types with no Update<T>Fields -- BGPConfig, PowerSupervisor, Setting, Site
// and WireGuardPeer.
//
// The naive version of that adapter sends the struct ToSDK produced, which
// carries a Go zero for every field the schema does not declare. This asserts
// the object sent is the one that came back from Get, with only the masked
// fields applied onto it. That is the same question that has classified every
// surface correctly tonight: is the object passed to Update the one that came
// back from Get?
func TestWholeObjectUpdateStartsFromTheFetchedObject(t *testing.T) {
	var sent *kitSDK
	r := kitResource(Backend[kitSDK]{
		Read: func(_ context.Context, _, id string) (*kitSDK, error) {
			// What the controller holds: a value the provider never models.
			return &kitSDK{ID: id, Name: "before", Unmanaged: "controller-owned"}, nil
		},
		Update: func(_ context.Context, _ string, in *kitSDK) (*kitSDK, error) {
			sent = in
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
	r.Update(ctx, resource.UpdateRequest{State: state, Plan: tfsdk.Plan(plan)}, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("Update: %v", resp.Diagnostics)
	}
	if sent == nil {
		t.Fatal("the whole-object Update was never called, so this asserts nothing")
	}
	if sent.Unmanaged != "controller-owned" {
		t.Errorf("Unmanaged = %q, want the controller's value; a whole-object write "+
			"built from the model resets every field the schema does not declare",
			sent.Unmanaged)
	}
	// The control: the masked field IS applied, or the test above would pass for
	// an adapter that sent the fetched object untouched and wrote nothing at all.
	if sent.Name != "after" {
		t.Errorf("Name = %q, want the planned value; the mask is not being applied "+
			"and the update writes nothing", sent.Name)
	}
}

// TestUpdateRefusesABackendThatCannotWrite turns a nil-pointer panic into a
// diagnostic that names the descriptor rather than the kit.
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
		t.Fatal("a backend with neither UpdateFields nor Update was accepted")
	}
}

// TestBeforeSendRunsOnTheObjectThatIsActuallySent guards an ordering constraint
// that was discovered by accident while wiring the whole-object path, and an
// accident is not a guard.
//
// BeforeSend must run AFTER the body is built. On the whole-object path the
// object sent is the one fetched from the controller, not the one ToSDK
// produced -- so a hook running on the latter derives its wire form onto an
// object that is then discarded. Nothing else in the suite notices: with the
// hook moved back before buildUpdateBody, every other test in this package and
// in unifi/ still passes, because they all take the masked path where the two
// objects are the same object.
//
// port_profile is the surface that would break: it derives tagged_vlan_mgmt,
// excluded_networkconf_ids and forward in BeforeSend, and those are AlwaysWire
// precisely because no plan names them.
func TestBeforeSendRunsOnTheObjectThatIsActuallySent(t *testing.T) {
	var sent *kitSDK
	r := kitResource(Backend[kitSDK]{
		Read: func(_ context.Context, _, id string) (*kitSDK, error) {
			return &kitSDK{ID: id, Name: "before", Unmanaged: "controller-owned"}, nil
		},
		Update: func(_ context.Context, _ string, in *kitSDK) (*kitSDK, error) {
			sent = in
			return in, nil
		},
	})
	// A hook that derives a value no plan carries, which is what BeforeSend is
	// for. If it runs on the wrong object the derivation is silently dropped.
	r.Spec.BeforeSend = func(
		_ context.Context, _, _ *kitModel, sdk *kitSDK, _ any,
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
		t.Fatal("the whole-object Update was never called, so this asserts nothing")
	}
	if sent.Unmanaged != "derived-by-hook" {
		t.Errorf("Unmanaged = %q, want the hook's value. BeforeSend ran on the object "+
			"ToSDK produced rather than the one being sent, so everything it derived "+
			"was discarded -- move it after buildUpdateBody.", sent.Unmanaged)
	}
}

// TestBeforeSendSeesAnEmptyIDOnCreateAndTheRealOneOnUpdate pins the invariant a
// create/update asymmetry inside BeforeSend has to stand on.
//
// BeforeSend has ONE signature for BOTH writes, so a hook that must behave
// differently on the two has to tell them apart from its arguments. The only
// thing that separates them is the effective model's ID: Create passes the
// plan, whose Computed id is unknown and reads as "", and Update passes state,
// which carries the controller's id -- and Update refuses an empty id before
// the hook is ever reached, so the update direction cannot silently take a
// create branch.
//
// firewall_policy depends on exactly this. The controller rejects a policy
// whose schedule is null, so the field has to be on every write, and the value
// differs by operation: a literal on create, the controller's current schedule
// on update. Getting the branch backwards is destructive rather than noisy --
// it resets a practitioner's schedule with no diff to show for it -- which is
// why the invariant is pinned here instead of assumed in the descriptor.
func TestBeforeSendSeesAnEmptyIDOnCreateAndTheRealOneOnUpdate(t *testing.T) {
	ctx := context.Background()

	newHook := func(seen *[]string) func(context.Context, *kitModel, *kitModel, *kitSDK, any) diag.Diagnostics {
		return func(_ context.Context, _, effective *kitModel, _ *kitSDK, _ any) diag.Diagnostics {
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

	// THE CONTROL. Without it every assertion below passes for a hook that
	// never ran, which is the failure mode this file has hit before.
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

// AfterReceive'S PRIOR MODEL, ONE TEST PER OPERATION.
//
// WHY THE PARAMETER EXISTS AT ALL. Spec.ToModel writes into the SAME model the
// operation started with, so by the time any hook runs, every attribute a Field
// owns holds what the controller returned. An attribute NO field touches still
// holds its old value -- which is why device's port_override works through this
// hook -- and the two cases look identical from outside. vpn_client is where
// that mattered: five attributes have to be carried forward from what was there
// before, and without a prior a create ends in "provider produced inconsistent
// result after apply".
//
// EACH OPERATION HANDS A DIFFERENT THING and getting one wrong is silent, so
// each has its own case rather than one test asserting "prior is non-empty".
// The probe answers a name the plan did not ask for, so a prior that were
// really the post-decode model would carry the controller's value and fail.

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

// UPDATE HANDS THE EFFECTIVE MODEL, NOT THE RAW PLAN, and this is the case that
// would be silently wrong.
//
// An attribute the plan does not mention is null in the plan and present in the
// state. Handing the raw plan would make an apply that changed only some OTHER
// attribute look like one that cleared this one -- which is exactly the shape of
// the port_forward defect, where a block absent from the plan turned into a
// value being dropped. BeforeSend already takes both models for this reason and
// its comment says so.
func TestUpdateHandsAfterReceiveTheEffectiveModelNotTheRawPlan(t *testing.T) {
	ctx := context.Background()
	r := kitResource(Backend[kitSDK]{
		UpdateFields: func(_ context.Context, _ string, in *kitSDK, _ ...string) (*kitSDK, error) {
			out := *in
			out.Name = "what-the-controller-reports"
			return &out, nil
		},
	})
	// A SECOND FIELD, SO THE MASK IS NOT EMPTY. The kit refuses a patch that
	// names nothing, and this test needs an apply that changes SOMETHING while
	// leaving name alone -- which is the whole case. site carries it.
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
	// THE PLAN LEAVES name UNKNOWN, which is what the framework produces for an
	// attribute an apply does not change and the provider may recompute.
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
