package unifi

// Framework-level CRUD for the HAND-WRITTEN firewall_zone resource.
//
// THE SECOND SHAPE, AND IT IS NOT THE SAME AS dns_record's. That resource has a
// dnsRecordBackend interface, so its fake is thirty lines and the test is about
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

func firewallZoneHarness(t *testing.T, client *Client) (
	*firewallZoneResource, tfsdk.State, tfsdk.ResourceIdentity,
) {
	t.Helper()
	ctx := context.Background()
	r := &firewallZoneResource{client: client}

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

func firewallZoneModelFor(t *testing.T, id string, networks []string) firewallZoneResourceModel {
	t.Helper()
	list := types.ListNull(types.StringType)
	if networks != nil {
		built, diags := types.ListValueFrom(context.Background(), types.StringType, networks)
		if diags.HasError() {
			t.Fatalf("build the network list: %v", diags)
		}
		list = built
	}
	return firewallZoneResourceModel{
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

	var got firewallZoneResourceModel
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
	// 400 RATHER THAN 500, AND THE REASON IS THE FAST LOOP. The SDK wraps its
	// transport in retryablehttp, which retries a 5xx with backoff -- this test
	// took FIFTEEN SECONDS on a 500, in a suite that runs on every push. A 400
	// is a client error, is not retried, and exercises the same distinction: a
	// failure that is not an absence must leave state alone.
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

	var got firewallZoneResourceModel
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
