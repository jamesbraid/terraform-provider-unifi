package unifi

// Framework-level CRUD for port_forward, written against the HAND-WRITTEN
// resource and kept across the cutover.
//
// THE POINT IS THE WIRE, NOT THE STATE. Every behaviour this surface can lose
// in a migration is a decision about which json keys leave the provider and
// which nested object a read produces, and both are invisible to a test that
// only checks the model round-trips. So the fake records the raw request body
// and the assertions name keys.
//
// port_forward talks to the SDK client directly, so the seam is HTTP and these
// also cover the SDK's URL construction and decoding -- the firewall_zone shape
// rather than the dns_record one, for the reason recorded there.

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

// THE ALIAS IS THE CUTOVER'S SEAM. Everything below is a conformance test
// against the HAND-WRITTEN resource, written before the migration and required
// to pass unchanged after it. Repointing these three lines is the only edit the
// cutover is allowed to make to this file, so a behaviour that changes has to
// change a test rather than be absorbed by one rewritten alongside the code.
type (
	portForwardCRUD      = *portForwardResource
	portForwardCRUDModel = portForwardKitModel
)

func newPortForwardCRUD() portForwardCRUD { return newPortForwardKitResource() }

// forwardServer answers the four paths port_forward uses and keeps the raw body
// of every write.
//
// RAW, NOT DECODED INTO ui.PortForward. Decoding would answer "what values did
// the provider send" and lose "which keys did it send at all" -- and the second
// is the whole difference between a whole-object write and a masked one. A
// field the provider omits and one it sends as its Go zero decode identically.
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
		"firewall_group_id": portForwardStringOrNull(group),
		"enabled":           types.BoolValue(enabled),
		"type":              kind,
	})
}

func portForwardModel(t *testing.T, id string) portForwardCRUDModel {
	t.Helper()
	return portForwardCRUDModel{
		ID:             portForwardStringOrNull(id),
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
) *fwresource.CreateResponse {
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
	return resp
}

// THE MAPPER WRITES FOURTEEN WIRES AND THE NAMES DO NOT LINE UP WITH THE
// ATTRIBUTES. wan.ip_address is destination_ip -- SINGULAR -- while the
// separate destination_ips list is the multi-WAN one, so a census taken from
// the SDK's field names pairs them and gets both wrong. wan.port is dst_port
// and wan.interface is pfwd_interface; nothing in either name says wan.
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

// SOURCE LIMITING'S TYPE IS INFERRED WHEN THE PRACTITIONER DOES NOT SET IT, and
// the inference is the behaviour, not the assignment: an explicit type wins, a
// firewall group id means firewall_group, and anything else means ip.
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

// A PLAN WITH NO SOURCE LIMITING MUST NOT INVENT ONE. The four wires travel
// together and none of them is written unless the block is present.
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

// THE CONTROLLER'S OWN DEFAULT MUST NOT PRODUCE A BLOCK. It answers src "any"
// with limiting disabled on every rule, so treating that as configuration makes
// an omitted block plan as null and apply as an object -- "inconsistent result
// after apply", and the reason the read side has a predicate rather than a
// zero check.
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

// SOURCE LIMITING THE CONTROLLER GENUINELY HOLDS COMES BACK.
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

// THE MULTI-WAN LIST ROUND-TRIPS AS ITSELF, and is null rather than empty when
// the controller reports none -- the nil-versus-empty distinction that costs a
// permanent diff.
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

// AN EMPTY NAME IS AN ABSENT NAME. The controller returns "" for a rule with no
// name and the attribute is optional, so a value of "" would plan as a change
// on every apply.
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

// A DELETED RULE IS REMOVED FROM STATE RATHER THAN REPORTED. The SDK returns a
// NotFoundError when the controller's data array is empty, and the resource has
// to answer that by dropping the resource rather than by raising a diagnostic --
// otherwise the practitioner has to remove it by hand.
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

// WHAT AN UPDATE PUTS ON THE WIRE IS THE BEHAVIOUR MOST AT RISK IN A CUTOVER,
// so it is asserted as a key set rather than as values.
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

func portForwardStringOrNull(s string) types.String {
	if s == "" {
		return types.StringNull()
	}
	return types.StringValue(s)
}

// SOURCE LIMITING SET OUTSIDE TERRAFORM SURVIVES THE NEXT APPLY -- and it did
// not before the cutover, which is why this test exists in both forms.
//
// THE DEFECT WAS TWO CORRECT PIECES. applyPlanToState tracked the plan exactly
// for this block including a null, deliberately, because a stale non-null state
// would re-send limiting the practitioner had removed. And src_limiting_enabled
// carries no omitempty on ui.PortForward, so the whole-object write put `false`
// on the wire whether or not anything set it. Together: a rule whose source
// limiting was configured in the controller UI had it turned off by an apply
// that changed only the name. The three companion wires DO carry omitempty and
// were omitted, which was the worst version -- the configuration survived in
// the controller and stopped taking effect.
//
// THE MASKED UPDATE CLOSES IT. A block absent from the plan puts none of the
// four names in the mask, so the controller keeps what it holds.
//
// THE SECOND HALF IS THE CONTROL AND IT IS NOT OPTIONAL. "None of the four is
// sent" is also what a descriptor that forgot to declare them produces, and
// that is the silent write-drop ScatteredObjectField exists to prevent. So the
// same wires must appear the moment the plan does declare the block.
// NAMED HERE RATHER THAN READ OFF THE DESCRIPTOR. A test that asked the
// descriptor which wires it declares would agree with it by construction, and
// the property under test is exactly that the descriptor declares all four.
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

	// THE CONTROL. Same resource, same server, a plan that DOES declare the
	// block: every one of the four must travel.
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

// A BLOCK THE CONTROLLER HAS EMPTIED NOW READS AS ABSENT, and before the
// cutover it read as an object whose members were all null.
//
// THE HAND-WRITTEN READ CONSULTED PRIOR MODEL STATE, in three places: `wan` and
// `forward` kept a non-null object when state already held one, and
// source_limiting's elision had the same clause. The kit hands Decode the SDK
// object and nothing else, and its AfterReceive hook runs AFTER ToModel has
// overwritten the model -- so there is nowhere on the kit's path to read the
// prior value from. vpn_client's wireguard field records the kit's answer as
// "express prior-state carry-forward in AfterReceive"; that answer does not
// work, for this reason, and this surface is where it was tried.
//
// SOURCE LIMITING KEEPS ITS PREDICATE AND LOSES ONLY THE STATE CLAUSE, so the
// controller-default case above still elides and a configured one still comes
// back. What changed is confined to the two blocks whose whole content the
// controller can empty, and only when it empties ALL of it: a port forward with
// any of pfwd_interface, destination_ip or dst_port set is unaffected, which is
// every rule a controller will accept.
//
// WHY THE NEW BEHAVIOUR IS THE BETTER ONE, rather than merely the reachable
// one: an object with every member null is a thing the controller does not have,
// manufactured by the provider. Both forms produce a diff against a
// configuration that declares the block. Only one of them says what is true.
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

	// THE CONTROL, so that "null" is a decision and not a decoder that stopped
	// working. One member is enough to bring the whole block back.
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
