package unifi

// Framework-level CRUD for the HAND-WRITTEN firewall_zone resource.
//
// THE SECOND SHAPE, AND IT IS NOT THE SAME AS dns_record's. That resource has a
// backend seam through the kit, so its fake is thirty lines and the test is about
// the resource alone. firewall_zone talks to the SDK client directly, so the
// only seam is HTTP -- which means these tests also cover the SDK's URL
// construction and its response decoding, and are slower for it.
//
// BOTH SHAPES ARE WORTH HAVING AND THEY ANSWER DIFFERENT QUESTIONS. Nobody had
// noticed that, because the one existing precedent in this repository used the
// HTTP shape for a resource that has no other option and it read as the only
// way. Which shape a resource needs is decided by whether it has an interface
// between itself and the SDK, and most do not.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	fwresource "github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
)

// zoneServer answers the four paths firewall_zone uses, recording what it saw.
//
// GET IS A LIST, NOT A FETCH, and that is the SDK's doing rather than a
// simplification here: getFirewallZone lists every zone on the site and scans
// for the id, returning NotFoundError when the list is empty or has no match.
// A test that stubbed a single-object GET would be testing a route the provider
// never takes.
// THE FAKE EMITS RAW JSON RATHER THAN MARSHALLING ui.FirewallZone, and finding
// out why cost an hour that is worth writing down.
//
// FirewallZone.MarshalJSON DELIBERATELY DROPS THE READ-ONLY FIELDS -- zone_key,
// default_zone, site_id, the attr_ set, cloud_template, external_id -- by
// shadowing each with a *struct{} and omitempty. Its own comment says why: the
// controller reports them and REJECTS them on a write, so an update after a read
// would fail on the server-assigned fields the read filled in.
//
// So a fake that builds its response by marshalling the SDK's own type is using
// the WRITE shape to produce a READ, and silently returns an object with every
// controller-owned field missing. The SDK is right and the fake was lying. The
// first version of this test read zone_key as "" and concluded the resource had
// dropped it.
//
// AND IT IS INDEPENDENT CONFIRMATION OF SOMETHING ELSE. The SDK marks exactly
// the fields the policy dispositions "computed" and the hand-written
// modelToFirewallZone declines to send. Three authorities, separately authored,
// agreeing on the same set.
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

// firewallZoneHarness builds the KIT-SERVED resource against a fake controller.
//
// The surface moved onto internal/resourcekit and these tests came with it
// unchanged in substance: they drive Create, Read and Delete against an
// httptest server and assert what goes on the wire and what comes back, which
// is exactly the behaviour a cutover can silently alter. Configure is called
// rather than a client being assigned, because the backend closures are built
// there.
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
	// 400 RATHER THAN 500, AND THIS IS A RULE FOR EVERY TEST OF THIS SHAPE.
	//
	// The SDK wraps its transport in retryablehttp, which retries a 5xx with
	// backoff. This test took FIFTEEN SECONDS on a 500, in a suite fast-loop
	// runs on every push. Twenty-five more resources need this same control, so
	// reaching for 500 as "the obvious server failure" would add six minutes to
	// every push and buy nothing: a 400 is not retried and exercises exactly the
	// same distinction, that a failure which is not an absence must leave state
	// alone.
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

// TestFirewallZoneReadOnlyFieldsNeverReachTheController REPLACES THE COVERAGE
// THE DIFFERENTIAL TOOK WITH IT.
//
// firewall_zone_kit_differential_test.go carried its own instruction to be
// deleted in the commit that deletes the hand-written resource, and it has
// been. But it also said why it existed: the contract check compares field
// NAMES between the mapping and the descriptor, so dropping the ReadOnly
// wrapper from zone_key leaves every name identical and silently changes what
// the provider sends. Only a comparison against the hand-written path caught
// that -- and there is no hand-written path any more.
//
// So the property is asserted directly instead: a read-only field must write
// nothing to the SDK struct, and must still read back from it. That needs no
// second implementation to compare against, which is why it outlives the one
// that did.
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

// TestFirewallZoneDeleteIgnoresNotFound SURVIVES THE CUTOVER, ADAPTED.
//
// It was this surface's own test of a behaviour that has since moved into the
// kit -- and NOTHING IN THE KIT ASSERTS IT. Deleting it with the hand-written
// Delete would have removed the only check of a live behaviour at the moment
// the behaviour became shared. It drives the kit's Delete through a real state
// against a controller answering 404, and it still asserts the request path,
// which is the part a descriptor could silently get wrong.
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
