package unifi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	fwresource "github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
)

// This alias is the cutover's seam: everything below is a conformance test
// against the hand-written resource, required to pass unchanged after a
// migration. Repointing these lines is the only edit a cutover may make here.
type (
	portForwardCRUD      = *portForwardResource
	portForwardCRUDModel = portForwardKitModel
)

func newPortForwardCRUD() portForwardCRUD { return newPortForwardKitResource() }

// forwardServer answers the four paths port_forward uses and keeps the raw
// body of every write -- undecoded, since decoding would lose which keys
// were sent at all (an omitted field and one sent as its Go zero decode
// identically).
type forwardServer struct {
	rules    []map[string]any
	bodies   []map[string]json.RawMessage
	requests []string
	deleted  string
	status   int
}

func (f *forwardServer) start(t *testing.T) *Client {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/proxy/network/status" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"meta":{"server_version":"10.4.57"}}`))
			return
		}
		f.requests = append(f.requests, req.Method+" "+req.URL.Path)
		if f.status != 0 {
			w.WriteHeader(f.status)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch req.Method {
		case http.MethodGet:
			_ = json.NewEncoder(w).Encode(map[string]any{"data": f.rules})
		case http.MethodPost, http.MethodPut:
			raw, _ := io.ReadAll(req.Body)
			var keyed map[string]json.RawMessage
			if err := json.Unmarshal(raw, &keyed); err != nil {
				t.Errorf("the provider sent a body that is not an object: %v", err)
			}
			f.bodies = append(f.bodies, keyed)
			var decoded ui.PortForward
			_ = json.Unmarshal(raw, &decoded)
			if decoded.ID == "" {
				decoded.ID = "pf-created"
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"data": []ui.PortForward{decoded}})
		case http.MethodDelete:
			f.deleted = req.URL.Path
			_, _ = w.Write([]byte(`{"data":[]}`))
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

// lastBody is the write under test. A test that read bodies[0] would keep
// passing after a change that added a second request.
func (f *forwardServer) lastBody(t *testing.T) map[string]json.RawMessage {
	t.Helper()
	if len(f.bodies) == 0 {
		t.Fatal("nothing reached the controller")
	}
	return f.bodies[len(f.bodies)-1]
}

func (f *forwardServer) sent(t *testing.T, key string) string {
	t.Helper()
	raw, present := f.lastBody(t)[key]
	if !present {
		t.Errorf("the controller was sent no %s at all", key)
		return ""
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatalf("reread %s: %v", key, err)
	}
	if text, ok := value.(string); ok {
		return text
	}
	return string(raw)
}

func portForwardHarness(t *testing.T, client *Client) (
	portForwardCRUD, tfsdk.State, tfsdk.ResourceIdentity,
) {
	t.Helper()
	ctx := context.Background()
	r := newPortForwardCRUD()
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

func objectOf(t *testing.T, types_ map[string]attr.Type, values map[string]attr.Value) types.Object {
	t.Helper()
	object, diags := types.ObjectValue(types_, values)
	if diags.HasError() {
		t.Fatalf("build an object: %v", diags)
	}
	return object
}

func portForwardWan(t *testing.T, iface, ip, port string) types.Object {
	t.Helper()
	return objectOf(t, portForwardWanModel{}.AttributeTypes(), map[string]attr.Value{
		"interface":  types.StringValue(iface),
		"ip_address": types.StringValue(ip),
		"port":       types.StringValue(port),
	})
}

func portForwardForward(t *testing.T, ip, port string) types.Object {
	t.Helper()
	return objectOf(t, portForwardForwardModel{}.AttributeTypes(), map[string]attr.Value{
		"ip":   types.StringValue(ip),
		"port": types.StringValue(port),
	})
}

func portForwardSourceLimiting(t *testing.T, ip, group string, enabled bool, kind attr.Value) types.Object {
	t.Helper()
	return objectOf(t, portForwardSourceLimitingModel{}.AttributeTypes(), map[string]attr.Value{
		"ip":                types.StringValue(ip),
		"firewall_group_id": portForwardStringOrNullValue(group),
		"enabled":           types.BoolValue(enabled),
		"type":              kind,
	})
}

func portForwardModel(t *testing.T, id string) portForwardCRUDModel {
	t.Helper()
	return portForwardCRUDModel{
		ID:             portForwardStringOrNullValue(id),
		Site:           types.StringValue("default"),
		Name:           types.StringValue("web"),
		Wan:            portForwardWan(t, "wan", "203.0.113.9", "8080"),
		Forward:        portForwardForward(t, "10.0.0.5", "80"),
		SourceLimiting: types.ObjectNull(portForwardSourceLimitingModel{}.AttributeTypes()),
		DestinationIPs: types.ListNull(types.ObjectType{
			AttrTypes: portForwardDestinationIPModel{}.AttributeTypes(),
		}),
		Protocol: types.StringValue("tcp"),
		Logging:  types.BoolValue(true),
		Enabled:  types.BoolValue(true),
		Timeouts: timeouts.Value{Object: types.ObjectNull(dnsRecordTimeoutTypes)},
	}
}

func createPortForward(t *testing.T, r portForwardCRUD, state tfsdk.State,
	identity tfsdk.ResourceIdentity, model portForwardCRUDModel,
) {
	t.Helper()
	ctx := context.Background()
	plan := state
	if diags := plan.Set(ctx, model); diags.HasError() {
		t.Fatalf("set the plan: %v", diags)
	}
	resp := &fwresource.CreateResponse{State: state, Identity: &identity}
	r.Create(ctx, fwresource.CreateRequest{Plan: tfsdk.Plan(plan)}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("Create: %v", resp.Diagnostics)
	}
}

// wan.ip_address writes as destination_ip (singular) -- not destination_ips,
// the separate multi-WAN list -- and wan.port/wan.interface write as
// dst_port/pfwd_interface; none of the wire names say "wan".
func TestPortForwardCreateSendsTheWiresTheMapperWrites(t *testing.T) {
	server := &forwardServer{}
	r, state, identity := portForwardHarness(t, server.start(t))
	createPortForward(t, r, state, identity, portForwardModel(t, ""))

	for key, want := range map[string]string{
		"name":           "web",
		"proto":          "tcp",
		"pfwd_interface": "wan",
		"destination_ip": "203.0.113.9",
		"dst_port":       "8080",
		"fwd":            "10.0.0.5",
		"fwd_port":       "80",
	} {
		if got := server.sent(t, key); got != want {
			t.Errorf("the controller was sent %s = %q, want %q", key, got, want)
		}
	}
	if _, present := server.lastBody(t)["destination_ips"]; present {
		t.Error("destination_ips reached the controller from a plan that set none; " +
			"it is the multi-WAN list, not wan.ip_address")
	}
}

// The type is inferred when not set explicitly: an explicit type wins, a
// firewall group id implies firewall_group, anything else implies ip.
func TestPortForwardSourceLimitingTypeIsInferredFromWhatIsSet(t *testing.T) {
	for _, tc := range []struct {
		name  string
		group string
		kind  attr.Value
		want  string
	}{
		{"an explicit type wins", "grp-1", types.StringValue("ip"), "ip"},
		{"a firewall group implies firewall_group", "grp-1", types.StringNull(), "firewall_group"},
		{"neither implies ip", "", types.StringNull(), "ip"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := &forwardServer{}
			r, state, identity := portForwardHarness(t, server.start(t))
			model := portForwardModel(t, "")
			model.SourceLimiting = portForwardSourceLimiting(t, "198.51.100.0/24", tc.group, true, tc.kind)
			createPortForward(t, r, state, identity, model)

			if got := server.sent(t, "src_limiting_type"); got != tc.want {
				t.Errorf("src_limiting_type = %q, want %q", got, tc.want)
			}
			if got := server.sent(t, "src"); got != "198.51.100.0/24" {
				t.Errorf("src = %q, want the configured address", got)
			}
		})
	}
}

// The four source_limiting wires travel together; none is written unless
// the block is present in the plan.
func TestPortForwardSendsNoSourceLimitingWhenTheBlockIsAbsent(t *testing.T) {
	server := &forwardServer{}
	r, state, identity := portForwardHarness(t, server.start(t))
	createPortForward(t, r, state, identity, portForwardModel(t, ""))

	body := server.lastBody(t)
	for _, key := range []string{"src", "src_firewall_group_id", "src_limiting_type"} {
		if _, present := body[key]; present {
			t.Errorf("%s reached the controller from a plan with no source_limiting block", key)
		}
	}
}

// The controller reports src "any" with limiting disabled on every rule,
// even absent config; treating that as configured would plan an omitted
// block as null and apply as an object -- "inconsistent result after apply".
func TestPortForwardReadElidesTheSourceLimitingTheControllerAlwaysReturns(t *testing.T) {
	ctx := context.Background()
	server := &forwardServer{rules: []map[string]any{{
		"_id": "pf-1", "name": "web", "proto": "tcp",
		"dst_port": "8080", "fwd": "10.0.0.5", "fwd_port": "80",
		"src": "any", "src_limiting_enabled": false, "enabled": true,
	}}}
	r, state, identity := portForwardHarness(t, server.start(t))
	if diags := state.Set(ctx, portForwardModel(t, "pf-1")); diags.HasError() {
		t.Fatalf("set the state: %v", diags)
	}

	resp := &fwresource.ReadResponse{State: state, Identity: &identity}
	r.Read(ctx, fwresource.ReadRequest{State: state, Identity: &identity}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("Read: %v", resp.Diagnostics)
	}
	var got portForwardCRUDModel
	if diags := resp.State.Get(ctx, &got); diags.HasError() {
		t.Fatalf("read back the state: %v", diags)
	}
	if !got.SourceLimiting.IsNull() {
		t.Errorf("source_limiting = %v, want null: the controller reported its own default, not configuration",
			got.SourceLimiting)
	}
}

// TestPortForwardReadKeepsConfiguredSourceLimiting checks that source
// limiting the controller genuinely holds comes back from a read.
func TestPortForwardReadKeepsConfiguredSourceLimiting(t *testing.T) {
	ctx := context.Background()
	server := &forwardServer{rules: []map[string]any{{
		"_id": "pf-1", "name": "web", "proto": "tcp", "enabled": true,
		"src": "198.51.100.0/24", "src_limiting_enabled": true, "src_limiting_type": "ip",
	}}}
	r, state, identity := portForwardHarness(t, server.start(t))
	if diags := state.Set(ctx, portForwardModel(t, "pf-1")); diags.HasError() {
		t.Fatalf("set the state: %v", diags)
	}
	resp := &fwresource.ReadResponse{State: state, Identity: &identity}
	r.Read(ctx, fwresource.ReadRequest{State: state, Identity: &identity}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("Read: %v", resp.Diagnostics)
	}
	var got portForwardCRUDModel
	if diags := resp.State.Get(ctx, &got); diags.HasError() {
		t.Fatalf("read back the state: %v", diags)
	}
	if got.SourceLimiting.IsNull() {
		t.Fatal("source_limiting came back null although the controller reported it configured")
	}
	ip, ok := got.SourceLimiting.Attributes()["ip"].(types.String)
	if !ok {
		t.Fatalf("source_limiting.ip is %T, not a string", got.SourceLimiting.Attributes()["ip"])
	}
	if ip.ValueString() != "198.51.100.0/24" {
		t.Errorf("source_limiting.ip = %q, want the controller's value", ip.ValueString())
	}
}

// destination_ips is null rather than empty when the controller reports
// none -- the nil-vs-empty distinction Terraform would otherwise diff on
// forever.
func TestPortForwardDestinationIPsRoundTrip(t *testing.T) {
	ctx := context.Background()
	server := &forwardServer{rules: []map[string]any{{
		"_id": "pf-1", "name": "web", "proto": "tcp", "enabled": true,
		"destination_ips": []map[string]any{
			{"destination_ip": "203.0.113.9", "interface": "wan"},
			{"destination_ip": "203.0.113.10", "interface": "wan2"},
		},
	}}}
	r, state, identity := portForwardHarness(t, server.start(t))
	if diags := state.Set(ctx, portForwardModel(t, "pf-1")); diags.HasError() {
		t.Fatalf("set the state: %v", diags)
	}
	resp := &fwresource.ReadResponse{State: state, Identity: &identity}
	r.Read(ctx, fwresource.ReadRequest{State: state, Identity: &identity}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("Read: %v", resp.Diagnostics)
	}
	var got portForwardCRUDModel
	if diags := resp.State.Get(ctx, &got); diags.HasError() {
		t.Fatalf("read back the state: %v", diags)
	}
	if got.DestinationIPs.IsNull() || len(got.DestinationIPs.Elements()) != 2 {
		t.Fatalf("destination_ips = %v, want the two the controller reported", got.DestinationIPs)
	}

	server.rules[0]["destination_ips"] = []map[string]any{}
	resp = &fwresource.ReadResponse{State: state, Identity: &identity}
	r.Read(ctx, fwresource.ReadRequest{State: state, Identity: &identity}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("Read: %v", resp.Diagnostics)
	}
	if diags := resp.State.Get(ctx, &got); diags.HasError() {
		t.Fatalf("read back the state: %v", diags)
	}
	if !got.DestinationIPs.IsNull() {
		t.Errorf("destination_ips = %v, want null when the controller reports none", got.DestinationIPs)
	}
}

// The controller returns "" for a rule with no name; reading that back as ""
// rather than null would plan a change on every apply.
func TestPortForwardEmptyNameReadsAsNull(t *testing.T) {
	ctx := context.Background()
	server := &forwardServer{rules: []map[string]any{{
		"_id": "pf-1", "proto": "tcp", "enabled": true,
	}}}
	r, state, identity := portForwardHarness(t, server.start(t))
	if diags := state.Set(ctx, portForwardModel(t, "pf-1")); diags.HasError() {
		t.Fatalf("set the state: %v", diags)
	}
	resp := &fwresource.ReadResponse{State: state, Identity: &identity}
	r.Read(ctx, fwresource.ReadRequest{State: state, Identity: &identity}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("Read: %v", resp.Diagnostics)
	}
	var got portForwardCRUDModel
	if diags := resp.State.Get(ctx, &got); diags.HasError() {
		t.Fatalf("read back the state: %v", diags)
	}
	if !got.Name.IsNull() {
		t.Errorf("name = %q, want null for a rule the controller reports with no name",
			got.Name.ValueString())
	}
}

// The SDK returns NotFoundError when the controller's data array is empty;
// Read must drop the resource rather than raise a diagnostic.
func TestPortForwardReadRemovesARuleTheControllerNoLongerHas(t *testing.T) {
	ctx := context.Background()
	server := &forwardServer{rules: []map[string]any{}}
	r, state, identity := portForwardHarness(t, server.start(t))
	if diags := state.Set(ctx, portForwardModel(t, "pf-1")); diags.HasError() {
		t.Fatalf("set the state: %v", diags)
	}
	resp := &fwresource.ReadResponse{State: state, Identity: &identity}
	r.Read(ctx, fwresource.ReadRequest{State: state, Identity: &identity}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("Read: %v", resp.Diagnostics)
	}
	if !resp.State.Raw.IsNull() {
		t.Error("the rule stayed in state although the controller no longer has it")
	}
}

func TestPortForwardDeleteAsksForTheRuleById(t *testing.T) {
	ctx := context.Background()
	server := &forwardServer{}
	r, state, _ := portForwardHarness(t, server.start(t))
	if diags := state.Set(ctx, portForwardModel(t, "pf-1")); diags.HasError() {
		t.Fatalf("set the state: %v", diags)
	}
	resp := &fwresource.DeleteResponse{State: state}
	r.Delete(ctx, fwresource.DeleteRequest{State: state}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("Delete: %v", resp.Diagnostics)
	}
	if server.deleted != "/proxy/network/api/s/default/rest/portforward/pf-1" {
		t.Errorf("deleted %q, want the rule's own path", server.deleted)
	}
}

func TestPortForwardUpdateSendsTheseKeys(t *testing.T) {
	ctx := context.Background()
	server := &forwardServer{rules: []map[string]any{{
		"_id": "pf-1", "name": "web", "proto": "tcp", "enabled": true,
	}}}
	r, state, identity := portForwardHarness(t, server.start(t))
	if diags := state.Set(ctx, portForwardModel(t, "pf-1")); diags.HasError() {
		t.Fatalf("set the state: %v", diags)
	}
	planned := portForwardModel(t, "pf-1")
	planned.Name = types.StringValue("web-renamed")
	plan := state
	if diags := plan.Set(ctx, planned); diags.HasError() {
		t.Fatalf("set the plan: %v", diags)
	}

	resp := &fwresource.UpdateResponse{State: state, Identity: &identity}
	r.Update(ctx, fwresource.UpdateRequest{
		Plan:  tfsdk.Plan(plan),
		State: state,
	}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("Update: %v", resp.Diagnostics)
	}
	if got := server.sent(t, "name"); got != "web-renamed" {
		t.Errorf("name = %q, want the planned one", got)
	}

	var keys []string
	for key := range server.lastBody(t) {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	t.Logf("update sent %d keys: %v", len(keys), keys)
	for _, required := range []string{"name", "proto", "dst_port", "fwd", "fwd_port"} {
		if !slices.Contains(keys, required) {
			t.Errorf("the update did not send %s, which the plan sets", required)
		}
	}
}

// sourceLimitingWires is named here rather than read off the descriptor: the
// property under test is exactly that the descriptor declares all four, so
// deriving the list from it would make the test agree with itself by
// construction.
var sourceLimitingWires = []string{
	"src",
	"src_limiting_enabled",
	"src_firewall_group_id",
	"src_limiting_type",
}

func TestPortForwardUpdateLeavesSourceLimitingThePlanDoesNotDeclare(t *testing.T) {
	ctx := context.Background()
	server := &forwardServer{rules: []map[string]any{{
		"_id": "pf-1", "name": "web", "proto": "tcp", "enabled": true,
		"src": "198.51.100.0/24", "src_limiting_enabled": true, "src_limiting_type": "ip",
	}}}
	r, state, identity := portForwardHarness(t, server.start(t))

	held := portForwardModel(t, "pf-1")
	held.SourceLimiting = portForwardSourceLimiting(t, "198.51.100.0/24", "", true,
		types.StringValue("ip"))
	if diags := state.Set(ctx, held); diags.HasError() {
		t.Fatalf("set the state: %v", diags)
	}

	planned := portForwardModel(t, "pf-1") // no source_limiting block
	planned.Name = types.StringValue("web-renamed")
	plan := state
	if diags := plan.Set(ctx, planned); diags.HasError() {
		t.Fatalf("set the plan: %v", diags)
	}

	resp := &fwresource.UpdateResponse{State: state, Identity: &identity}
	r.Update(ctx, fwresource.UpdateRequest{Plan: tfsdk.Plan(plan), State: state}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("Update: %v", resp.Diagnostics)
	}
	body := server.lastBody(t)
	for _, wire := range sourceLimitingWires {
		if _, sent := body[wire]; sent {
			t.Errorf("%s reached the controller although the plan declares no "+
				"source_limiting block; the mask is carrying a name the plan did not set", wire)
		}
	}

	// Now with a plan that does declare the block: every one of the four must travel.
	declared := portForwardModel(t, "pf-1")
	declared.SourceLimiting = portForwardSourceLimiting(t, "203.0.113.0/24", "grp-9", true,
		types.StringValue("firewall_group"))
	plan = state
	if diags := plan.Set(ctx, declared); diags.HasError() {
		t.Fatalf("set the plan: %v", diags)
	}
	resp = &fwresource.UpdateResponse{State: state, Identity: &identity}
	r.Update(ctx, fwresource.UpdateRequest{Plan: tfsdk.Plan(plan), State: state}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("Update: %v", resp.Diagnostics)
	}
	body = server.lastBody(t)
	for _, wire := range sourceLimitingWires {
		if _, sent := body[wire]; !sent {
			t.Errorf("%s did not reach the controller although the plan declares the block; "+
				"a name missing from the mask is a value the practitioner set and the apply drops", wire)
		}
	}
}

// A block (wan/forward) whose every member the controller reports empty
// reads as absent rather than an object with all-null members -- but only
// when ALL of pfwd_interface, destination_ip and dst_port are unset; any one
// present brings the whole block back.
func TestPortForwardReadReportsABlockTheControllerEmptiedAsAbsent(t *testing.T) {
	ctx := context.Background()
	server := &forwardServer{rules: []map[string]any{{
		"_id": "pf-1", "name": "web", "proto": "tcp", "enabled": true,
	}}}
	r, state, identity := portForwardHarness(t, server.start(t))
	if diags := state.Set(ctx, portForwardModel(t, "pf-1")); diags.HasError() {
		t.Fatalf("set the state: %v", diags)
	}
	resp := &fwresource.ReadResponse{State: state, Identity: &identity}
	r.Read(ctx, fwresource.ReadRequest{State: state, Identity: &identity}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("Read: %v", resp.Diagnostics)
	}
	var got portForwardCRUDModel
	if diags := resp.State.Get(ctx, &got); diags.HasError() {
		t.Fatalf("read back the state: %v", diags)
	}
	for name, object := range map[string]types.Object{"wan": got.Wan, "forward": got.Forward} {
		if !object.IsNull() {
			t.Errorf("%s = %v, want null: the controller reported none of its members", name, object)
		}
	}

	// Confirms "null" is a decision, not a decoder that stopped working: one
	// member is enough to bring the whole block back.
	server.rules[0]["dst_port"] = "8080"
	resp = &fwresource.ReadResponse{State: state, Identity: &identity}
	r.Read(ctx, fwresource.ReadRequest{State: state, Identity: &identity}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("Read: %v", resp.Diagnostics)
	}
	if diags := resp.State.Get(ctx, &got); diags.HasError() {
		t.Fatalf("read back the state: %v", diags)
	}
	if got.Wan.IsNull() {
		t.Fatal("wan is null although the controller reported a dst_port")
	}
	port, ok := got.Wan.Attributes()["port"].(types.String)
	if !ok || port.ValueString() != "8080" {
		t.Errorf("wan.port = %v, want the controller's value", got.Wan.Attributes()["port"])
	}
}
