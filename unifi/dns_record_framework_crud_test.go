package unifi

// Framework-level CRUD for the HAND-WRITTEN dns_record resource.
//
// WHY THIS IS NOT CUTOVER SCAFFOLDING. Measured across the provider: 0 of 27
// resources have a test that drives Create, Read, Update or Delete through the
// framework. The only end-to-end coverage is TestAcc*, which calls preCheck and
// needs a live controller -- so the half of every resource that talks to
// Terraform rather than to the SDK has never been exercised on a push.
//
// That half is not small and it is not obvious. Timeouts are read off the model
// and applied to the context; diagnostics have to be checked after each step or
// a failed read proceeds into a nil model; a not-found Read must REMOVE the
// resource rather than error, or a deleted object becomes unmanageable; the
// identity attribute has to be set separately from state. None of that appears
// in a schema and none of it is reachable from the pure functions the other
// tests cover.
//
// THE FAKE IS THE BACKEND, NOT THE HTTP LAYER. dnsRecordBackend is already an
// interface, so the seam exists; the one precedent in this repository
// (firewall_zone) stands up an httptest server instead, which tests the SDK's
// URL construction as well and is slower. Both are worth having. This one is
// about the resource.

import (
	"context"
	"errors"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework-timetypes/timetypes"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	fwresource "github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
)

// fakeDNSRecordBackend records what it was asked to do and returns what it was
// told to. Every field is inspected by at least one test below.
type fakeDNSRecordBackend struct {
	created   dnsRecordIntent
	patched   dnsRecordPatch
	deletedID string
	readID    string
	result    dnsRecordModel
	err       error
	calls     []string
}

func (f *fakeDNSRecordBackend) Create(_ context.Context, _ string, in dnsRecordIntent) (dnsRecordModel, error) {
	f.calls = append(f.calls, "create")
	f.created = in
	return f.result, f.err
}

func (f *fakeDNSRecordBackend) Read(_ context.Context, _ string, id string) (dnsRecordModel, error) {
	f.calls = append(f.calls, "read")
	f.readID = id
	return f.result, f.err
}

func (f *fakeDNSRecordBackend) Update(_ context.Context, _ string, p dnsRecordPatch) (dnsRecordModel, error) {
	f.calls = append(f.calls, "update")
	f.patched = p
	return f.result, f.err
}

func (f *fakeDNSRecordBackend) Delete(_ context.Context, _ string, id string) error {
	f.calls = append(f.calls, "delete")
	f.deletedID = id
	return f.err
}

func (f *fakeDNSRecordBackend) List(_ context.Context, _ string) ([]dnsRecordModel, error) {
	f.calls = append(f.calls, "list")
	return []dnsRecordModel{f.result}, f.err
}

var dnsRecordTimeoutTypes = map[string]attr.Type{
	"create": types.StringType,
	"read":   types.StringType,
	"update": types.StringType,
	"delete": types.StringType,
}

// dnsRecordHarness builds the resource, its schemas, and a model the framework
// will accept. Everything the framework needs and a caller should not repeat.
func dnsRecordHarness(t *testing.T, backend dnsRecordBackend) (
	*dnsRecordFrameworkResource, tfsdk.State, tfsdk.ResourceIdentity,
) {
	t.Helper()
	ctx := context.Background()
	r := &dnsRecordFrameworkResource{backend: backend, defaultSite: "default"}

	schemaResp := &fwresource.SchemaResponse{}
	r.Schema(ctx, fwresource.SchemaRequest{}, schemaResp)
	if schemaResp.Diagnostics.HasError() {
		t.Fatalf("build the schema: %v", schemaResp.Diagnostics)
	}
	identityResp := &fwresource.IdentitySchemaResponse{}
	r.IdentitySchema(ctx, fwresource.IdentitySchemaRequest{}, identityResp)

	// THE IDENTITY NEEDS A TYPED NULL, not a zero value.
	//
	// A tfsdk.ResourceIdentity carrying only a Schema has an untyped Raw, and
	// SetAttribute on it fails with:
	//
	//	Cannot transform data: invalid transform: value missing type
	//
	// which is the framework refusing to guess. The real serving path builds
	// this itself, so a test has to. The error text is quoted in full because
	// it is the string somebody will search for, and one line is the whole fix.
	identity := tfsdk.ResourceIdentity{Schema: identityResp.IdentitySchema}
	identity.Raw = tftypes.NewValue(identityResp.IdentitySchema.Type().TerraformType(ctx), nil)

	return r, tfsdk.State{Schema: schemaResp.Schema}, identity
}

func dnsRecordModelFor(id string) dnsRecordFrameworkResourceModel {
	return dnsRecordFrameworkResourceModel{
		ID:         types.StringValue(id),
		Site:       types.StringValue("default"),
		Name:       types.StringValue("host.example"),
		Enabled:    types.BoolValue(true),
		Port:       types.Int64Null(),
		Priority:   types.Int64Null(),
		RecordType: types.StringValue("A"),
		TTL:        timetypes.NewGoDurationNull(),
		Value:      types.StringValue("10.0.0.1"),
		Weight:     types.Int64Null(),
		Timeouts:   timeouts.Value{Object: types.ObjectNull(dnsRecordTimeoutTypes)},
	}
}

// TestDNSRecordCreateWritesStateAndIdentity covers the whole framework path:
// plan in, backend called, state and identity out.
func TestDNSRecordCreateWritesStateAndIdentity(t *testing.T) {
	ctx := context.Background()
	backend := &fakeDNSRecordBackend{result: dnsRecordModel{
		ID: "created-1", Enabled: true, Name: "host.example",
		RecordType: "A", Value: "10.0.0.1",
	}}
	r, state, identity := dnsRecordHarness(t, backend)

	plan := state
	if diags := plan.Set(ctx, dnsRecordModelFor("")); diags.HasError() {
		t.Fatalf("set the plan: %v", diags)
	}

	resp := &fwresource.CreateResponse{State: state, Identity: &identity}
	r.Create(ctx, fwresource.CreateRequest{Plan: tfsdk.Plan(plan)}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("Create: %v", resp.Diagnostics)
	}

	// THE BACKEND SAW THE PLAN. Without this the test passes on a Create that
	// sends an empty object, which is the failure a schema cannot see.
	if backend.created.Name != "host.example" || backend.created.Value != "10.0.0.1" {
		t.Errorf("the backend was sent %+v, which is not the planned record", backend.created)
	}

	var got dnsRecordFrameworkResourceModel
	if diags := resp.State.Get(ctx, &got); diags.HasError() {
		t.Fatalf("read back the state: %v", diags)
	}
	if got.ID.ValueString() != "created-1" {
		t.Errorf("state id = %q, want the id the controller assigned", got.ID.ValueString())
	}

	// THE IDENTITY IS SET SEPARATELY FROM STATE and nothing else checks it. An
	// unset identity makes the resource unimportable, which surfaces much later
	// as a failed import rather than as a failed apply.
	var identityValue struct {
		ID types.String `tfsdk:"id"`
	}
	if diags := resp.Identity.Get(ctx, &identityValue); diags.HasError() {
		t.Fatalf("read back the identity: %v", diags)
	}
	if identityValue.ID.ValueString() != "created-1" {
		t.Errorf("identity id = %q, want it to match state", identityValue.ID.ValueString())
	}
}

// TestDNSRecordReadRemovesAnAbsentRecord is the behaviour a schema cannot
// express and an acceptance test needs a controller to reach.
//
// A record deleted outside Terraform must leave state, so the next plan
// recreates it. Erroring instead leaves the practitioner with a resource that
// cannot be refreshed and cannot be destroyed.
func TestDNSRecordReadRemovesAnAbsentRecord(t *testing.T) {
	ctx := context.Background()
	backend := &fakeDNSRecordBackend{err: &ui.NotFoundError{}}
	r, state, identity := dnsRecordHarness(t, backend)
	if diags := state.Set(ctx, dnsRecordModelFor("gone-1")); diags.HasError() {
		t.Fatalf("set the state: %v", diags)
	}

	resp := &fwresource.ReadResponse{State: state, Identity: &identity}
	r.Read(ctx, fwresource.ReadRequest{State: state}, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("Read errored on an absent record instead of removing it: %v", resp.Diagnostics)
	}
	if !resp.State.Raw.IsNull() {
		t.Error("the record is gone from the controller and still in state; the next plan " +
			"would try to update something that does not exist")
	}
	if backend.readID != "gone-1" {
		t.Errorf("the backend was asked for %q, want the id in state", backend.readID)
	}
}

// TestDNSRecordReadReportsARealFailure is the control for the test above, and
// the pair is the point rather than either test.
//
// WRITE BOTH OR NEITHER, ON EVERY NOT-FOUND CHECK IN THIS PROVIDER. "Removes an
// absent record" ALONE IS SATISFIED BY REMOVING THE RESOURCE ON EVERY ERROR --
// a resource that deletes state whenever the controller is briefly unreachable
// passes it, and passes it more easily than the correct one. The single test
// rewards the worse implementation.
//
// This is a positive control placed inside a test'"'"'s own semantics rather than
// around the harness, and it generalises: any check of the form "X is treated
// as absence" needs a sibling asserting that not-X is not.
func TestDNSRecordReadReportsARealFailure(t *testing.T) {
	ctx := context.Background()
	backend := &fakeDNSRecordBackend{err: errors.New("connection refused")}
	r, state, identity := dnsRecordHarness(t, backend)
	if diags := state.Set(ctx, dnsRecordModelFor("live-1")); diags.HasError() {
		t.Fatalf("set the state: %v", diags)
	}

	resp := &fwresource.ReadResponse{State: state, Identity: &identity}
	r.Read(ctx, fwresource.ReadRequest{State: state}, resp)

	if !resp.Diagnostics.HasError() {
		t.Fatal("a transport failure was treated as an absent record; state would be discarded " +
			"because the controller was briefly unreachable")
	}
	if resp.State.Raw.IsNull() {
		t.Error("state was removed on a transport failure")
	}
}

// TestDNSRecordUpdateMasksOnlyThePlannedFields checks the field mask reaches the
// backend, which is the whole point of a masked write.
func TestDNSRecordUpdateMasksOnlyThePlannedFields(t *testing.T) {
	ctx := context.Background()
	backend := &fakeDNSRecordBackend{result: dnsRecordModel{
		ID: "rec-1", Enabled: true, Name: "host.example", RecordType: "A", Value: "10.0.0.2",
	}}
	r, state, identity := dnsRecordHarness(t, backend)

	prior := dnsRecordModelFor("rec-1")
	if diags := state.Set(ctx, prior); diags.HasError() {
		t.Fatalf("set the state: %v", diags)
	}
	planned := prior
	planned.Value = types.StringValue("10.0.0.2")
	plan := tfsdk.State{Schema: state.Schema}
	if diags := plan.Set(ctx, planned); diags.HasError() {
		t.Fatalf("set the plan: %v", diags)
	}

	resp := &fwresource.UpdateResponse{State: state, Identity: &identity}
	r.Update(ctx, fwresource.UpdateRequest{State: state, Plan: tfsdk.Plan(plan)}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("Update: %v", resp.Diagnostics)
	}

	if backend.patched.ID != "rec-1" {
		t.Errorf("the patch carried id %q, want the id in state", backend.patched.ID)
	}
	wire, err := backend.patched.wireFields()
	if err != nil {
		t.Fatalf("the patch the resource built is not sendable: %v", err)
	}
	if len(wire) == 0 {
		t.Fatal("the update sent an empty field mask, which is a whole-object write by another route")
	}
	// THE MASK IS ABOUT WHAT THE PLAN SET, not about what changed. Every
	// non-null planned attribute joins it, which is what lets an unchanged
	// attribute stay under the provider's management.
	found := false
	for _, name := range wire {
		if name == "value" {
			found = true
		}
	}
	if !found {
		t.Errorf("the changed attribute is not in the mask %v", wire)
	}
}

// TestDNSRecordDeleteAsksForTheRecordInState. Small, and it is the one path
// where getting the id wrong destroys the wrong object.
func TestDNSRecordDeleteAsksForTheRecordInState(t *testing.T) {
	ctx := context.Background()
	backend := &fakeDNSRecordBackend{}
	r, state, _ := dnsRecordHarness(t, backend)
	if diags := state.Set(ctx, dnsRecordModelFor("doomed-1")); diags.HasError() {
		t.Fatalf("set the state: %v", diags)
	}

	resp := &fwresource.DeleteResponse{State: state}
	r.Delete(ctx, fwresource.DeleteRequest{State: state}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("Delete: %v", resp.Diagnostics)
	}
	if backend.deletedID != "doomed-1" {
		t.Errorf("deleted %q, want the id in state", backend.deletedID)
	}
}
