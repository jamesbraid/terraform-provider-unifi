package unifi

// Framework-level CRUD for the HAND-WRITTEN firewall_zone resource.
//
// A different shape from dns_record's: firewall_zone talks to the SDK
// client directly rather than through a backend seam, so the only seam is
// HTTP, and these tests also cover the SDK's URL construction and response
// decoding. Which shape a resource needs depends on whether it has an
// interface between itself and the SDK -- most do not, so this HTTP shape
// is the more common one.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/path"
	fwresource "github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
)

// zoneServer answers the four paths firewall_zone uses, recording what it
// saw. GET returns a list, not a single object: getFirewallZone lists every
// zone on the site and scans for the id, so a stub answering a
// single-object GET would test a route the provider never takes.
//
// Zones are raw JSON maps, not ui.FirewallZone, because
// FirewallZone.MarshalJSON deliberately drops the read-only fields
// (zone_key, default_zone, site_id, ...) via omitempty -- the controller
// rejects them on a write, so marshalling the SDK type here would silently
// produce a READ response missing every controller-owned field.
type zoneServer struct {
	zones    []map[string]any
	posted   *ui.FirewallZone
	deleted  string
	requests []string
	status   int
}

func (z *zoneServer) start(t *testing.T) *Client {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/proxy/network/status" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"meta":{"server_version":"10.4.57"}}`))
			return
		}
		z.requests = append(z.requests, req.Method+" "+req.URL.Path)
		if z.status != 0 {
			w.WriteHeader(z.status)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch req.Method {
		case http.MethodGet:
			_ = json.NewEncoder(w).Encode(z.zones)
		case http.MethodPost:
			var body ui.FirewallZone
			_ = json.NewDecoder(req.Body).Decode(&body)
			z.posted = &body
			created := body
			created.ID = "zone-created"
			_ = json.NewEncoder(w).Encode(created)
		case http.MethodDelete:
			z.deleted = req.URL.Path
			_, _ = w.Write([]byte(`{}`))
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	t.Cleanup(server.Close)

	api, err := ui.New(context.Background(), &ui.Config{BaseURL: server.URL, APIKey: "test-key"})
	if err != nil {
		t.Fatalf("create the API client: %v", err)
	}
	return &Client{ApiClient: api, Site: "default"}
}

// firewallZoneHarness builds the kit-served resource against a fake
// controller. Configure is called, not a client assigned directly, because
// the backend closures are built there.
func firewallZoneHarness(t *testing.T, client *Client) (
	*firewallZoneKitResource, tfsdk.State, tfsdk.ResourceIdentity,
) {
	t.Helper()
	ctx := context.Background()
	r := newFirewallZoneKitResource()
	configureResp := &fwresource.ConfigureResponse{}
	r.Configure(ctx, fwresource.ConfigureRequest{ProviderData: client}, configureResp)
	if configureResp.Diagnostics.HasError() {
		t.Fatalf("configure: %v", configureResp.Diagnostics)
	}

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

func firewallZoneModelFor(t *testing.T, id string, networks []string) firewallZoneKitModel {
	t.Helper()
	list := types.ListNull(types.StringType)
	if networks != nil {
		built, diags := types.ListValueFrom(context.Background(), types.StringType, networks)
		if diags.HasError() {
			t.Fatalf("build the network list: %v", diags)
		}
		list = built
	}
	return firewallZoneKitModel{
		ID:          types.StringValue(id),
		Site:        types.StringValue("default"),
		Name:        types.StringValue("Trusted"),
		NetworkIDs:  list,
		ZoneKey:     types.StringNull(),
		DefaultZone: types.BoolNull(),
		Timeouts:    timeouts.Value{Object: types.ObjectNull(dnsRecordTimeoutTypes)},
	}
}

// TestFirewallZoneCreateSendsTheZoneAndKeepsWhatComesBack.
//
// The POST body is inspected because a resource that sends an empty object
// still produces valid state from the response, and nothing else would notice.
func TestFirewallZoneCreateSendsTheZoneAndKeepsWhatComesBack(t *testing.T) {
	ctx := context.Background()
	server := &zoneServer{}
	r, state, identity := firewallZoneHarness(t, server.start(t))

	plan := state
	if diags := plan.Set(ctx, firewallZoneModelFor(t, "", []string{"net-a", "net-b"})); diags.HasError() {
		t.Fatalf("set the plan: %v", diags)
	}

	resp := &fwresource.CreateResponse{State: state, Identity: &identity}
	r.Create(ctx, fwresource.CreateRequest{Plan: tfsdk.Plan(plan)}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("Create: %v", resp.Diagnostics)
	}

	if server.posted == nil {
		t.Fatal("no zone reached the controller")
	}
	if server.posted.Name != "Trusted" {
		t.Errorf("the controller was sent name %q, want the planned one", server.posted.Name)
	}
	if len(server.posted.NetworkIDs) != 2 {
		t.Errorf("the controller was sent %d network ids, want the two in the plan",
			len(server.posted.NetworkIDs))
	}

	var got firewallZoneKitModel
	if diags := resp.State.Get(ctx, &got); diags.HasError() {
		t.Fatalf("read back the state: %v", diags)
	}
	if got.ID.ValueString() != "zone-created" {
		t.Errorf("state id = %q, want the id the controller assigned", got.ID.ValueString())
	}
}

// TestFirewallZoneReadRemovesAnAbsentZone. Same rule as dns_record and reached
// differently: the SDK returns NotFoundError when the LIST has no matching id,
// so an empty site is the absent case.
func TestFirewallZoneReadRemovesAnAbsentZone(t *testing.T) {
	ctx := context.Background()
	server := &zoneServer{zones: []map[string]any{}}
	r, state, identity := firewallZoneHarness(t, server.start(t))
	if diags := state.Set(ctx, firewallZoneModelFor(t, "gone-1", nil)); diags.HasError() {
		t.Fatalf("set the state: %v", diags)
	}

	resp := &fwresource.ReadResponse{State: state, Identity: &identity}
	r.Read(ctx, fwresource.ReadRequest{State: state}, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("Read errored on an absent zone instead of removing it: %v", resp.Diagnostics)
	}
	if !resp.State.Raw.IsNull() {
		t.Error("the zone is gone from the controller and still in state")
	}
}

// TestFirewallZoneReadKeepsStateOnATransportFailure is the control. Without it,
// the test above is satisfied by removing the resource on every error.
func TestFirewallZoneReadKeepsStateOnATransportFailure(t *testing.T) {
	ctx := context.Background()
	// 400 rather than 500, and this is a rule for every test of this shape:
	// the SDK retries a 5xx with backoff, so a 500 costs real wall-clock time
	// on every push. A 400 is not retried and exercises the same distinction
	// -- a failure that is not an absence must leave state alone.
	server := &zoneServer{status: http.StatusBadRequest}
	r, state, identity := firewallZoneHarness(t, server.start(t))
	if diags := state.Set(ctx, firewallZoneModelFor(t, "live-1", nil)); diags.HasError() {
		t.Fatalf("set the state: %v", diags)
	}

	resp := &fwresource.ReadResponse{State: state, Identity: &identity}
	r.Read(ctx, fwresource.ReadRequest{State: state}, resp)

	if !resp.Diagnostics.HasError() {
		t.Fatal("a request failure was treated as an absent zone; state would be discarded " +
			"because the controller rejected the request")
	}
	if resp.State.Raw.IsNull() {
		t.Error("state was removed on a transport failure")
	}
}

// TestFirewallZoneReadPopulatesTheControllerOwnedFields is what the ReadOnly
// wrapper in the descriptor is about, checked here on the hand-written path.
// zone_key and default_zone are assigned by the controller and the practitioner
// cannot set them; a Read that dropped them would show a permanent diff.
func TestFirewallZoneReadPopulatesTheControllerOwnedFields(t *testing.T) {
	ctx := context.Background()
	server := &zoneServer{zones: []map[string]any{{
		"_id": "zone-1", "name": "Trusted", "zone_key": "trusted",
		"default_zone": true, "network_ids": []string{"net-a"},
	}}}
	r, state, identity := firewallZoneHarness(t, server.start(t))
	if diags := state.Set(ctx, firewallZoneModelFor(t, "zone-1", nil)); diags.HasError() {
		t.Fatalf("set the state: %v", diags)
	}

	resp := &fwresource.ReadResponse{State: state, Identity: &identity}
	r.Read(ctx, fwresource.ReadRequest{State: state}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("Read: %v", resp.Diagnostics)
	}

	var got firewallZoneKitModel
	if diags := resp.State.Get(ctx, &got); diags.HasError() {
		t.Fatalf("read back the state: %v", diags)
	}
	if got.ZoneKey.ValueString() != "trusted" {
		t.Errorf("zone_key = %v, want the controller's value", got.ZoneKey)
	}
	if got.DefaultZone.IsNull() || !got.DefaultZone.ValueBool() {
		t.Errorf("default_zone = %v, want true; it is a *bool and its third state is "+
			"\"the controller did not say\"", got.DefaultZone)
	}
}

func TestFirewallZoneDeleteAsksForTheZoneInState(t *testing.T) {
	ctx := context.Background()
	server := &zoneServer{}
	r, state, _ := firewallZoneHarness(t, server.start(t))
	if diags := state.Set(ctx, firewallZoneModelFor(t, "doomed-1", nil)); diags.HasError() {
		t.Fatalf("set the state: %v", diags)
	}

	resp := &fwresource.DeleteResponse{State: state}
	r.Delete(ctx, fwresource.DeleteRequest{State: state}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("Delete: %v", resp.Diagnostics)
	}
	if server.deleted != "/proxy/network/v2/api/site/default/firewall/zone/doomed-1" {
		t.Errorf("deleted %q, want the path carrying the id in state", server.deleted)
	}
}

// TestFirewallZoneReadOnlyFieldsNeverReachTheController asserts a read-only
// field's behavior directly: it must write nothing to the SDK struct, yet
// still read back from it. A name-based contract check (comparing the
// mapping and descriptor's field names) can't see this -- dropping the
// ReadOnly wrapper from zone_key would leave every name identical while
// silently changing what the provider sends.
func TestFirewallZoneReadOnlyFieldsNeverReachTheController(t *testing.T) {
	ctx := context.Background()
	spec := firewallZoneKitSpec()

	model := firewallZoneKitModel{
		Name:        types.StringValue("Trusted"),
		NetworkIDs:  types.ListNull(types.StringType),
		ZoneKey:     types.StringValue("controller-assigned"),
		DefaultZone: types.BoolValue(true),
	}

	var sdk ui.FirewallZone
	for _, field := range spec.Fields {
		if d := field.ToSDK(ctx, &model, &sdk); d.HasError() {
			t.Fatalf("ToSDK(%s): %v", field.WireName(), d)
		}
	}
	if sdk.ZoneKey != "" {
		t.Errorf("zone_key reached the SDK struct as %q; it is controller-owned "+
			"and the descriptor must not offer it back", sdk.ZoneKey)
	}
	if sdk.DefaultZone != nil {
		t.Errorf("default_zone reached the SDK struct as %v; it is controller-owned", *sdk.DefaultZone)
	}
	if sdk.Name != "Trusted" {
		t.Errorf("the writable field did not reach the SDK struct: %q -- if this fails the "+
			"assertions above prove nothing, because nothing is being written at all", sdk.Name)
	}

	// And the read direction still populates them, which is the half a
	// send-only check would miss.
	back := firewallZoneKitModel{}
	fromAPI := ui.FirewallZone{Name: "Trusted", ZoneKey: "trusted", DefaultZone: boolPtrForTest(true)}
	for _, field := range spec.Fields {
		if d := field.ToModel(ctx, &fromAPI, &back); d.HasError() {
			t.Fatalf("ToModel(%s): %v", field.WireName(), d)
		}
	}
	if back.ZoneKey.ValueString() != "trusted" {
		t.Errorf("zone_key did not read back: %v", back.ZoneKey)
	}
	if back.DefaultZone.IsNull() || !back.DefaultZone.ValueBool() {
		t.Errorf("default_zone did not read back: %v", back.DefaultZone)
	}
}

func boolPtrForTest(b bool) *bool { return &b }

// firewallZoneModelForNameImport is firewallZoneModelFor with the id left
// null instead of set, which is the state a "name=" import leaves behind: the
// kit's generic ImportState writes the name attribute and nothing else, so
// the first Read is the one that must resolve it. types.String's zero value
// is null, not "", so this is not firewallZoneModelFor(t, "", nil) -- that
// would set id to the empty STRING, a different state than id unset.
func firewallZoneModelForNameImport(t *testing.T, name string) firewallZoneKitModel {
	t.Helper()
	return firewallZoneKitModel{
		Site:        types.StringValue("default"),
		Name:        types.StringValue(name),
		NetworkIDs:  types.ListNull(types.StringType),
		ZoneKey:     types.StringNull(),
		DefaultZone: types.BoolNull(),
		Timeouts:    timeouts.Value{Object: types.ObjectNull(dnsRecordTimeoutTypes)},
	}
}

// TestFirewallZoneReadByNameResolvesAnExactMatch is the case the ImportState
// wiring exists for: a "name=Hotspot" import lands here with no id, and the
// zone the controller holds under that name must come back.
func TestFirewallZoneReadByNameResolvesAnExactMatch(t *testing.T) {
	ctx := context.Background()
	server := &zoneServer{zones: []map[string]any{
		{"_id": "zone-1", "name": "Hotspot", "zone_key": "hotspot", "network_ids": []string{"net-a"}},
		{"_id": "zone-2", "name": "Internal", "zone_key": "internal", "network_ids": []string{}},
	}}
	r, state, identity := firewallZoneHarness(t, server.start(t))
	if diags := state.Set(ctx, firewallZoneModelForNameImport(t, "Hotspot")); diags.HasError() {
		t.Fatalf("set the state: %v", diags)
	}

	resp := &fwresource.ReadResponse{State: state, Identity: &identity}
	r.Read(ctx, fwresource.ReadRequest{State: state}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("Read by name: %v", resp.Diagnostics)
	}

	var got firewallZoneKitModel
	if diags := resp.State.Get(ctx, &got); diags.HasError() {
		t.Fatalf("read back the state: %v", diags)
	}
	if got.ID.ValueString() != "zone-1" {
		t.Errorf("id = %q, want the id of the zone named Hotspot", got.ID.ValueString())
	}
	if got.ZoneKey.ValueString() != "hotspot" {
		t.Errorf("zone_key = %v, want hotspot", got.ZoneKey)
	}
}

// TestFirewallZoneReadByNameReportsNoMatch guards the failure the generic
// kit test (TestReadReportsANameThatResolvesToNothing) covers with a stub
// resolver: here the resolver is the real list-and-scan, and an empty or
// non-matching zone list must still produce an error rather than a silently
// removed resource -- the practitioner typed the name moments ago.
func TestFirewallZoneReadByNameReportsNoMatch(t *testing.T) {
	ctx := context.Background()
	server := &zoneServer{zones: []map[string]any{
		{"_id": "zone-2", "name": "Internal", "zone_key": "internal", "network_ids": []string{}},
	}}
	r, state, identity := firewallZoneHarness(t, server.start(t))
	if diags := state.Set(ctx, firewallZoneModelForNameImport(t, "DoesNotExist")); diags.HasError() {
		t.Fatalf("set the state: %v", diags)
	}

	resp := &fwresource.ReadResponse{State: state, Identity: &identity}
	r.Read(ctx, fwresource.ReadRequest{State: state}, resp)
	if !resp.Diagnostics.HasError() {
		t.Fatal("a name that matched no zone was reported as success")
	}
	// An error leaves state alone, the same rule
	// TestFirewallZoneReadKeepsStateOnATransportFailure asserts for a
	// transport error: it is neither removed (that reads as "deleted, recreate
	// it") nor given a resolved id (that reads as "imported successfully").
	var id types.String
	if diags := resp.State.GetAttribute(ctx, path.Root("id"), &id); diags.HasError() {
		t.Fatalf("reading id: %v", diags)
	}
	if !id.IsNull() {
		t.Errorf("id = %v, want null; a name that matched nothing must not resolve one", id)
	}
}

// TestFirewallZoneReadByNameRejectsAnAmbiguousName covers a case go-unifi's
// own GetNetworkByName and GetWLANByName don't guard against (both return
// whichever match comes first); this resolver refuses an ambiguous name
// instead of guessing.
func TestFirewallZoneReadByNameRejectsAnAmbiguousName(t *testing.T) {
	ctx := context.Background()
	server := &zoneServer{zones: []map[string]any{
		{"_id": "zone-1", "name": "Duplicate", "network_ids": []string{}},
		{"_id": "zone-2", "name": "Duplicate", "network_ids": []string{}},
	}}
	r, state, identity := firewallZoneHarness(t, server.start(t))
	if diags := state.Set(ctx, firewallZoneModelForNameImport(t, "Duplicate")); diags.HasError() {
		t.Fatalf("set the state: %v", diags)
	}

	resp := &fwresource.ReadResponse{State: state, Identity: &identity}
	r.Read(ctx, fwresource.ReadRequest{State: state}, resp)
	if !resp.Diagnostics.HasError() {
		t.Fatal("an ambiguous name resolved to one of the matches instead of erroring")
	}
	found := false
	for _, d := range resp.Diagnostics {
		if strings.Contains(d.Detail(), "multiple firewall zones named") {
			found = true
		}
	}
	if !found {
		t.Errorf("no diagnostic named the ambiguity; got: %v", resp.Diagnostics)
	}
}

// TestFirewallZoneDeleteIgnoresNotFound drives the kit's Delete through a
// real state against a controller answering 404: nothing in the kit itself
// asserts that a 404 on delete is treated as success, and this is the one
// place that still checks the request path a descriptor could get wrong.
func TestFirewallZoneDeleteIgnoresNotFound(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/proxy/network/status" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"meta":{"server_version":"10.4.57"}}`))
			return
		}
		if req.Method != http.MethodDelete {
			t.Errorf("request method = %s, want DELETE", req.Method)
		}
		if req.URL.Path != "/proxy/network/v2/api/site/default/firewall/zone/missing-zone" {
			t.Errorf("request path = %s, want firewall zone delete path", req.URL.Path)
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(server.Close)

	apiClient, err := ui.New(
		context.Background(),
		&ui.Config{BaseURL: server.URL, APIKey: "test-key"},
	)
	if err != nil {
		t.Fatalf("create API client: %v", err)
	}

	r := newFirewallZoneKitResource()
	configureResp := &fwresource.ConfigureResponse{}
	r.Configure(context.Background(),
		fwresource.ConfigureRequest{ProviderData: &Client{ApiClient: apiClient, Site: "default"}},
		configureResp)
	if configureResp.Diagnostics.HasError() {
		t.Fatalf("configure: %v", configureResp.Diagnostics)
	}
	schemaResp := &fwresource.SchemaResponse{}
	r.Schema(context.Background(), fwresource.SchemaRequest{}, schemaResp)
	state := tfsdk.State{Schema: schemaResp.Schema}
	timeoutTypes := map[string]attr.Type{
		"create": types.StringType,
		"read":   types.StringType,
		"update": types.StringType,
		"delete": types.StringType,
	}
	diags := state.Set(context.Background(), &firewallZoneKitModel{
		ID:          types.StringValue("missing-zone"),
		Site:        types.StringValue("default"),
		Name:        types.StringValue("Missing Zone"),
		NetworkIDs:  types.ListNull(types.StringType),
		ZoneKey:     types.StringNull(),
		DefaultZone: types.BoolNull(),
		Timeouts:    timeouts.Value{Object: types.ObjectNull(timeoutTypes)},
	})
	if diags.HasError() {
		t.Fatalf("set delete state: %v", diags)
	}

	resp := &fwresource.DeleteResponse{State: state}
	r.Delete(
		context.Background(),
		fwresource.DeleteRequest{State: state},
		resp,
	)

	if resp.Diagnostics.HasError() {
		t.Fatalf("Delete returned error diagnostics for an absent zone: %v", resp.Diagnostics)
	}
}
