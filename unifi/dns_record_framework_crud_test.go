package unifi

// Framework-level CRUD tests for the hand-written dns_record resource -- the
// only resource with tests that drive Create, Read, Update or Delete through
// the framework rather than through TestAcc against a live controller. They
// were written against the hand-written implementation and inherited
// unchanged by the kit, which is what makes them a regression check rather
// than the kit merely agreeing with itself.

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
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/resourcekit"
)

// fakeDNSRecordBackend records what it was asked to do and returns what it was
// told to. Every field is inspected by at least one test below.
type fakeDNSRecordBackend struct {
	created   *ui.DNSRecord
	updated   *ui.DNSRecord
	fields    []string
	deletedID string
	readID    string
	result    *ui.DNSRecord
	err       error
}

// backend renders the fake as the closures the kit takes. GetID and SetID are
// the real ones, not fakes: a lying identity accessor would make every
// assertion below about the fake, not the resource.
func (f *fakeDNSRecordBackend) backend() resourcekit.Backend[ui.DNSRecord] {
	return resourcekit.Backend[ui.DNSRecord]{
		Create: func(_ context.Context, _ string, in *ui.DNSRecord) (*ui.DNSRecord, error) {
			f.created = in
			return f.result, f.err
		},
		Read: func(_ context.Context, _, id string) (*ui.DNSRecord, error) {
			f.readID = id
			return f.result, f.err
		},
		UpdateFields: func(_ context.Context, _ string, in *ui.DNSRecord, fields ...string) (*ui.DNSRecord, error) {
			f.updated, f.fields = in, fields
			return f.result, f.err
		},
		Delete: func(_ context.Context, _, id string) error {
			f.deletedID = id
			return f.err
		},
		List: func(_ context.Context, _ string) ([]ui.DNSRecord, error) {
			if f.result == nil {
				return nil, f.err
			}
			return []ui.DNSRecord{*f.result}, f.err
		},
		GetID: func(s *ui.DNSRecord) string { return s.ID },
		SetID: func(s *ui.DNSRecord, id string) { s.ID = id },
	}
}

var dnsRecordTimeoutTypes = map[string]attr.Type{
	"create": types.StringType,
	"read":   types.StringType,
	"update": types.StringType,
	"delete": types.StringType,
}

// dnsRecordHarness builds the resource, its schemas, and a model the framework
// will accept. Everything the framework needs and a caller should not repeat.
func dnsRecordHarness(t *testing.T, fake *fakeDNSRecordBackend) (
	*dnsRecordKitResource, tfsdk.State, tfsdk.ResourceIdentity,
) {
	t.Helper()
	ctx := context.Background()
	r := newDNSRecordKitResource()
	r.Spec.Backend = fake.backend()
	r.DefaultSite = "default"

	schemaResp := &fwresource.SchemaResponse{}
	r.Schema(ctx, fwresource.SchemaRequest{}, schemaResp)
	if schemaResp.Diagnostics.HasError() {
		t.Fatalf("build the schema: %v", schemaResp.Diagnostics)
	}
	identityResp := &fwresource.IdentitySchemaResponse{}
	r.IdentitySchema(ctx, fwresource.IdentitySchemaRequest{}, identityResp)

	// The identity needs a typed null, not a zero value: a
	// tfsdk.ResourceIdentity carrying only a Schema has an untyped Raw, and
	// SetAttribute on it fails with "Cannot transform data: invalid
	// transform: value missing type".
	identity := tfsdk.ResourceIdentity{Schema: identityResp.IdentitySchema}
	identity.Raw = tftypes.NewValue(identityResp.IdentitySchema.Type().TerraformType(ctx), nil)

	return r, tfsdk.State{Schema: schemaResp.Schema}, identity
}

func dnsRecordModelFor(id string) dnsRecordKitModel {
	return dnsRecordKitModel{
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
	backend := &fakeDNSRecordBackend{result: &ui.DNSRecord{
		ID: "created-1", Enabled: true, Key: "host.example",
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

	// Confirms the backend saw the plan: without this the test would pass
	// even if Create sent an empty object.
	if backend.created == nil || backend.created.Key != "host.example" ||
		backend.created.Value != "10.0.0.1" {
		t.Errorf("the backend was sent %+v, which is not the planned record", backend.created)
	}

	var got dnsRecordKitModel
	if diags := resp.State.Get(ctx, &got); diags.HasError() {
		t.Fatalf("read back the state: %v", diags)
	}
	if got.ID.ValueString() != "created-1" {
		t.Errorf("state id = %q, want the id the controller assigned", got.ID.ValueString())
	}

	// The identity is set separately from state and nothing else checks it:
	// an unset identity makes the resource unimportable.
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

// TestDNSRecordReadReportsARealFailure is the control for the test above: a
// "removes an absent record" test alone would be satisfied by removing the
// resource on every error, and pass more easily than the correct
// implementation.
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
	backend := &fakeDNSRecordBackend{result: &ui.DNSRecord{
		ID: "rec-1", Enabled: true, Key: "host.example", RecordType: "A", Value: "10.0.0.2",
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

	if backend.updated == nil || backend.updated.ID != "rec-1" {
		t.Fatalf("the update carried the wrong object: %+v", backend.updated)
	}
	wire := backend.fields
	if len(wire) == 0 {
		t.Fatal("the update sent an empty field mask, which is a whole-object write by another route")
	}
	// The mask reflects what the plan set, not what changed: every non-null
	// planned attribute joins it, keeping an unchanged attribute under the
	// provider's management.
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
