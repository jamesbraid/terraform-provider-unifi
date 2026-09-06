package unifi

// The scoped-backend seam is the one piece of machinery this surface has
// that no other kit resource exercises: Read, Delete and List must resolve
// the parent network out of state or config and bind the backend to it
// before the kit runs. Nothing else covers it -- the kit's own tests bind a
// backend directly, and the acceptance tests need a controller -- so these
// drive the overrides through the framework with a factory that records
// which network each operation asked for.

import (
	"context"
	"testing"

	fwlist "github.com/hashicorp/terraform-plugin-framework/list"
	fwresource "github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/resourcekit"
)

// fakeWireguardPeerScopedBackend records every scope the factory was asked
// for and what each closure was asked to do.
type fakeWireguardPeerScopedBackend struct {
	networkIDs []string
	readID     string
	deletedID  string
	result     *ui.WireGuardPeer
	err        error
}

func (f *fakeWireguardPeerScopedBackend) factory() func(string) resourcekit.Backend[ui.WireGuardPeer] {
	return func(networkID string) resourcekit.Backend[ui.WireGuardPeer] {
		f.networkIDs = append(f.networkIDs, networkID)
		return resourcekit.Backend[ui.WireGuardPeer]{
			Read: func(_ context.Context, _, id string) (*ui.WireGuardPeer, error) {
				f.readID = id
				return f.result, f.err
			},
			Delete: func(_ context.Context, _, id string) error {
				f.deletedID = id
				return f.err
			},
			List: func(_ context.Context, _ string) ([]ui.WireGuardPeer, error) {
				if f.result == nil {
					return nil, f.err
				}
				return []ui.WireGuardPeer{*f.result}, f.err
			},
			GetID: func(s *ui.WireGuardPeer) string { return s.ID },
			SetID: func(s *ui.WireGuardPeer, id string) { s.ID = id },
		}
	}
}

// lastScope is what the operation under test bound: the harness itself binds
// once with "" the way Configure does, so the interesting scope is the last.
func (f *fakeWireguardPeerScopedBackend) lastScope() string {
	if len(f.networkIDs) == 0 {
		return "<never bound>"
	}
	return f.networkIDs[len(f.networkIDs)-1]
}

func wireguardPeerHarness(t *testing.T, fake *fakeWireguardPeerScopedBackend) (
	*wireguardPeerKitResource, tfsdk.State, tfsdk.ResourceIdentity,
) {
	t.Helper()
	ctx := context.Background()
	r := newWireguardPeerKitResource()
	r.scopedBackend = fake.factory()
	r.Spec.Backend = r.scopedBackend("")
	r.DefaultSite = "default"

	schemaResp := &fwresource.SchemaResponse{}
	r.Schema(ctx, fwresource.SchemaRequest{}, schemaResp)
	if schemaResp.Diagnostics.HasError() {
		t.Fatalf("build the schema: %v", schemaResp.Diagnostics)
	}
	identityResp := &fwresource.IdentitySchemaResponse{}
	r.IdentitySchema(ctx, fwresource.IdentitySchemaRequest{}, identityResp)

	identity := tfsdk.ResourceIdentity{Schema: identityResp.IdentitySchema}
	identity.Raw = tftypes.NewValue(identityResp.IdentitySchema.Type().TerraformType(ctx), nil)

	return r, tfsdk.State{Schema: schemaResp.Schema}, identity
}

func wireguardPeerModelFor(id string) wireguardPeerKitModel {
	return wireguardPeerKitModel{
		ID:          types.StringValue(id),
		Site:        types.StringValue("default"),
		NetworkID:   types.StringValue("net-1"),
		Name:        types.StringValue("peer"),
		InterfaceIP: types.StringValue("192.0.2.10"),
		PublicKey:   types.StringValue("k=="),
		AllowedIPs:  types.ListValueMust(types.StringType, nil),
		Timeouts:    timeoutsNullValue(),
	}
}

func TestWireguardPeerReadBindsTheNetworkInState(t *testing.T) {
	ctx := context.Background()
	fake := &fakeWireguardPeerScopedBackend{result: &ui.WireGuardPeer{
		ID: "peer-1", NetworkID: "net-1", Name: "peer",
		InterfaceIP: "192.0.2.10", PublicKey: "k==", AllowedIPs: []string{},
	}}
	r, state, identity := wireguardPeerHarness(t, fake)
	if diags := state.Set(ctx, wireguardPeerModelFor("peer-1")); diags.HasError() {
		t.Fatalf("set the state: %v", diags)
	}

	resp := &fwresource.ReadResponse{State: state, Identity: &identity}
	r.Read(ctx, fwresource.ReadRequest{State: state}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("Read: %v", resp.Diagnostics)
	}
	if fake.lastScope() != "net-1" {
		t.Errorf("Read bound the backend to %q, want the network_id state holds", fake.lastScope())
	}
	if fake.readID != "peer-1" {
		t.Errorf("the backend was asked for %q, want the id in state", fake.readID)
	}
	var got wireguardPeerKitModel
	if diags := resp.State.Get(ctx, &got); diags.HasError() {
		t.Fatalf("read back the state: %v", diags)
	}
	if got.InterfaceIP.ValueString() != "192.0.2.10" {
		t.Errorf("state interface_ip = %q, want the controller's value", got.InterfaceIP.ValueString())
	}
}

// TestWireguardPeerReadRemovesAnAbsentPeer pins that the override still
// reaches the kit's not-found handling: a peer deleted outside Terraform
// leaves state so the next plan recreates it.
func TestWireguardPeerReadRemovesAnAbsentPeer(t *testing.T) {
	ctx := context.Background()
	fake := &fakeWireguardPeerScopedBackend{err: &ui.NotFoundError{}}
	r, state, identity := wireguardPeerHarness(t, fake)
	if diags := state.Set(ctx, wireguardPeerModelFor("gone-1")); diags.HasError() {
		t.Fatalf("set the state: %v", diags)
	}

	resp := &fwresource.ReadResponse{State: state, Identity: &identity}
	r.Read(ctx, fwresource.ReadRequest{State: state}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("Read errored on an absent peer instead of removing it: %v", resp.Diagnostics)
	}
	if !resp.State.Raw.IsNull() {
		t.Error("the peer is gone from the controller and still in state")
	}
}

func TestWireguardPeerDeleteBindsTheNetworkInState(t *testing.T) {
	ctx := context.Background()
	fake := &fakeWireguardPeerScopedBackend{}
	r, state, identity := wireguardPeerHarness(t, fake)
	if diags := state.Set(ctx, wireguardPeerModelFor("peer-2")); diags.HasError() {
		t.Fatalf("set the state: %v", diags)
	}

	resp := &fwresource.DeleteResponse{}
	r.Delete(ctx, fwresource.DeleteRequest{State: state, Identity: &identity}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("Delete: %v", resp.Diagnostics)
	}
	if fake.lastScope() != "net-1" {
		t.Errorf("Delete bound the backend to %q, want the network_id state holds", fake.lastScope())
	}
	if fake.deletedID != "peer-2" {
		t.Errorf("the backend was asked to delete %q, want the id in state", fake.deletedID)
	}
}

// wireguardPeerListRequest builds a list request whose config carries the
// parent network and, optionally, one name filter.
func wireguardPeerListRequest(t *testing.T, r *wireguardPeerKitResource, filterName, filterValue string) fwlist.ListRequest {
	t.Helper()
	ctx := context.Background()

	schemaResp := &fwlist.ListResourceSchemaResponse{}
	r.ListResourceConfigSchema(ctx, fwlist.ListResourceSchemaRequest{}, schemaResp)
	configSchema := schemaResp.Schema
	config := tfsdk.Config{Schema: configSchema}
	schemaObject, ok := configSchema.Type().TerraformType(ctx).(tftypes.Object)
	if !ok {
		t.Fatalf("the list config schema is not an object: %T",
			configSchema.Type().TerraformType(ctx))
	}
	filterType := schemaObject.AttributeTypes["filter"]
	filterValues := tftypes.NewValue(filterType, nil)
	if filterName != "" {
		filterList, ok := filterType.(tftypes.List)
		if !ok {
			t.Fatalf("the filter attribute is not a list: %T", filterType)
		}
		element := filterList.ElementType
		filterValues = tftypes.NewValue(filterType, []tftypes.Value{
			tftypes.NewValue(element, map[string]tftypes.Value{
				"name":  tftypes.NewValue(tftypes.String, filterName),
				"value": tftypes.NewValue(tftypes.String, filterValue),
			}),
		})
	}
	config.Raw = tftypes.NewValue(configSchema.Type().TerraformType(ctx), map[string]tftypes.Value{
		"site":       tftypes.NewValue(tftypes.String, "default"),
		"network_id": tftypes.NewValue(tftypes.String, "net-1"),
		"filter":     filterValues,
	})

	resourceSchemaResp := &fwresource.SchemaResponse{}
	r.Schema(ctx, fwresource.SchemaRequest{}, resourceSchemaResp)
	identityResp := &fwresource.IdentitySchemaResponse{}
	r.IdentitySchema(ctx, fwresource.IdentitySchemaRequest{}, identityResp)

	return fwlist.ListRequest{
		Config:                 config,
		ResourceSchema:         resourceSchemaResp.Schema,
		ResourceIdentitySchema: identityResp.IdentitySchema,
		IncludeResource:        true,
	}
}

func TestWireguardPeerListBindsTheNetworkInConfig(t *testing.T) {
	ctx := context.Background()
	fake := &fakeWireguardPeerScopedBackend{result: &ui.WireGuardPeer{
		ID: "peer-3", NetworkID: "net-1", Name: "peer",
		InterfaceIP: "192.0.2.10", PublicKey: "k==", AllowedIPs: []string{},
	}}
	r, _, _ := wireguardPeerHarness(t, fake)

	stream := &fwlist.ListResultsStream{}
	r.List(ctx, wireguardPeerListRequest(t, r, "name", "peer"), stream)
	var results []fwlist.ListResult
	stream.Results(func(result fwlist.ListResult) bool {
		results = append(results, result)
		return true
	})

	if fake.lastScope() != "net-1" {
		t.Errorf("List bound the backend to %q, want the network_id the config names", fake.lastScope())
	}
	if len(results) != 1 {
		t.Fatalf("got %d result(s), want the one matching peer", len(results))
	}
	if results[0].Diagnostics.HasError() {
		t.Fatalf("the result carries errors: %v", results[0].Diagnostics)
	}
	if results[0].DisplayName != "peer" {
		t.Errorf("display name = %q, want the peer's name", results[0].DisplayName)
	}
}

// TestWireguardPeerListRefusesAnUnknownFilter pins the refusal this List
// shares with the kit's: a filter naming no field must be an error, not a
// silent match-everything.
func TestWireguardPeerListRefusesAnUnknownFilter(t *testing.T) {
	ctx := context.Background()
	fake := &fakeWireguardPeerScopedBackend{}
	r, _, _ := wireguardPeerHarness(t, fake)

	stream := &fwlist.ListResultsStream{}
	r.List(ctx, wireguardPeerListRequest(t, r, "mac", "aa:bb"), stream)
	errored := false
	stream.Results(func(result fwlist.ListResult) bool {
		if result.Diagnostics.HasError() {
			errored = true
		}
		return true
	})
	if !errored {
		t.Fatal("a filter naming no field was accepted; it would match everything")
	}
}
