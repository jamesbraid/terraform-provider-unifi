package unifi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework-nettypes/cidrtypes"
	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	fwresource "github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
)

// This alias is the cutover's seam: repointing these lines is the only edit
// the cutover may make to this file, so a behaviour that changes has to
// change a test rather than be absorbed by one rewritten alongside the code.
type (
	vpnClientCRUD      = *vpnClientResource
	vpnClientCRUDModel = vpnClientResourceModel
)

func newVPNClientCRUD() vpnClientCRUD { return newVPNClientKitResource() }

// wireguardConfigFixture is a real WireGuard configuration. The keys are not
// keys -- they are the right shape and nothing else -- and the file is here in
// plain text so that what it supplies is readable rather than encoded.
const wireguardConfigFixture = `[Interface]
PrivateKey = aFakePrivateKeyForTestsOnlyNotRealAAAAAAAAAA=
Address = 10.7.0.2/32
DNS = 10.7.0.1, 10.7.0.53

[Peer]
PublicKey = aFakePublicKeyForTestsOnlyNotRealBBBBBBBBBB=
PresharedKey = aFakePresharedKeyForTestsOnlyNotRealCCCCCC=
Endpoint = 198.51.100.7:51820
AllowedIPs = 0.0.0.0/0
`

func encodedWireguardConfig(t *testing.T) string {
	t.Helper()
	encoded := base64.StdEncoding.EncodeToString([]byte(wireguardConfigFixture))
	// Checked before use: a configuration that doesn't parse makes Encode
	// write nothing, and every assertion downstream would then describe the
	// failure path while reading as though it described the feature.
	parsed, err := parseWireGuardBase64Config(encoded)
	if err != nil {
		t.Fatalf("the fixture configuration does not parse: %v", err)
	}
	if parsed.PublicKey == "" || parsed.EndpointIP == "" || len(parsed.DNS) != 2 {
		t.Fatalf("the fixture parses but supplies %+v, not the peer and two DNS servers "+
			"these tests are written against", parsed)
	}
	return encoded
}

// networkServer answers the networkconf paths and keeps the raw body of
// every write, for the reason port_forward's fake does: decoding would lose
// which keys were sent at all.
type networkServer struct {
	networks []map[string]any
	bodies   []map[string]json.RawMessage
	requests []string
	deleted  string
}

func (n *networkServer) start(t *testing.T) *Client {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/proxy/network/status" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"meta":{"server_version":"10.4.57"}}`))
			return
		}
		n.requests = append(n.requests, req.Method+" "+req.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		switch req.Method {
		case http.MethodGet:
			_ = json.NewEncoder(w).Encode(map[string]any{"data": n.networks})
		case http.MethodPost, http.MethodPut:
			raw, _ := io.ReadAll(req.Body)
			var keyed map[string]json.RawMessage
			if err := json.Unmarshal(raw, &keyed); err != nil {
				t.Errorf("the provider sent a body that is not an object: %v", err)
			}
			n.bodies = append(n.bodies, keyed)
			// Echoed as a map, not ui.Network: a zero Network can't marshal at
			// all (MarshalJSON switches on Purpose and errors on unknown), so
			// echoing what arrived keeps the discriminator the provider's own.
			echoed := map[string]any{}
			for key, value := range keyed {
				echoed[key] = value
			}
			if _, has := echoed["_id"]; !has {
				echoed["_id"] = "net-created"
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{echoed}})
		case http.MethodDelete:
			n.deleted = req.URL.Path
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

func (n *networkServer) lastBody(t *testing.T) map[string]json.RawMessage {
	t.Helper()
	if len(n.bodies) == 0 {
		t.Fatal("nothing reached the controller")
	}
	return n.bodies[len(n.bodies)-1]
}

func (n *networkServer) sent(t *testing.T, key string) string {
	t.Helper()
	raw, present := n.lastBody(t)[key]
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

func vpnClientHarness(t *testing.T, client *Client) (
	vpnClientCRUD, tfsdk.State, tfsdk.ResourceIdentity,
) {
	t.Helper()
	ctx := context.Background()
	r := newVPNClientCRUD()
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

type wireguardBlock struct {
	privateKey          types.String
	privateKeyWO        types.String
	privateKeyWOVersion types.Int64
	configuration       types.Object
	peer                types.Object
	presharedOn         bool
	presharedKey        types.String
	iface               types.String
	dnsServers          types.List
}

func emptyWireguardBlock() wireguardBlock {
	return wireguardBlock{
		privateKey:          types.StringNull(),
		privateKeyWO:        types.StringNull(),
		privateKeyWOVersion: types.Int64Null(),
		configuration:       types.ObjectNull(wireguardConfigurationModel{}.AttributeTypes()),
		peer:                types.ObjectNull(wireguardPeerModel{}.AttributeTypes()),
		presharedKey:        types.StringNull(),
		iface:               types.StringNull(),
		dnsServers:          types.ListNull(types.StringType),
	}
}

func (b wireguardBlock) object(t *testing.T) types.Object {
	t.Helper()
	value := wireguardModel{
		PrivateKey:          b.privateKey,
		PrivateKeyWO:        b.privateKeyWO,
		PrivateKeyWOVersion: b.privateKeyWOVersion,
		Configuration:       b.configuration,
		Peer:                b.peer,
		PresharedKeyEnabled: types.BoolValue(b.presharedOn),
		PresharedKey:        b.presharedKey,
		Interface:           b.iface,
		DnsServers:          b.dnsServers,
	}
	object, diags := types.ObjectValueFrom(context.Background(), value.AttributeTypes(), value)
	if diags.HasError() {
		t.Fatalf("build the wireguard block: %v", diags)
	}
	return object
}

func vpnClientModel(t *testing.T, id string, wireguard types.Object) vpnClientCRUDModel {
	t.Helper()
	identifier := types.StringNull()
	if id != "" {
		identifier = types.StringValue(id)
	}
	return vpnClientCRUDModel{
		ID:           identifier,
		Site:         types.StringValue("default"),
		Name:         types.StringValue("tunnel"),
		Enabled:      types.BoolValue(true),
		Subnet:       cidrtypes.NewIPv4PrefixValue("10.7.0.0/24"),
		DefaultRoute: types.BoolValue(true),
		PullDNS:      types.BoolValue(false),
		Wireguard:    wireguard,
		Timeouts:     timeouts.Value{Object: types.ObjectNull(dnsRecordTimeoutTypes)},
	}
}

func configurationObject(t *testing.T) types.Object {
	t.Helper()
	object, diags := types.ObjectValue(wireguardConfigurationModel{}.AttributeTypes(),
		map[string]attr.Value{
			"content":  types.StringValue(encodedWireguardConfig(t)),
			"filename": types.StringValue("tunnel.conf"),
		})
	if diags.HasError() {
		t.Fatalf("build the configuration block: %v", diags)
	}
	return object
}

func createVPNClient(t *testing.T, r vpnClientCRUD, state tfsdk.State,
	identity tfsdk.ResourceIdentity, model vpnClientCRUDModel,
) {
	t.Helper()
	ctx := context.Background()
	plan := state
	if diags := plan.Set(ctx, model); diags.HasError() {
		t.Fatalf("set the plan: %v", diags)
	}
	resp := &fwresource.CreateResponse{State: state, Identity: &identity}
	// Config mirrors the plan deliberately: vpnClientBeforeSend reads
	// req.Config unconditionally, so a zero Config would panic (Data.Get on
	// a nil Schema). Tests needing plan and config to differ build the
	// request by hand instead of through this helper.
	r.Create(ctx, fwresource.CreateRequest{
		Plan:   tfsdk.Plan(plan),
		Config: tfsdk.Config(plan),
	}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("Create: %v", resp.Diagnostics)
	}
}

// purpose and vpn_type are constants with no attribute of their own: purpose
// selects which of go-unifi's seven alias structs serialises the object, and
// vpn_type is what makes the controller treat this as a wireguard client at all.
func TestVPNClientCreateSendsTheConstantsAndTheAttributes(t *testing.T) {
	server := &networkServer{}
	r, state, identity := vpnClientHarness(t, server.start(t))
	block := emptyWireguardBlock()
	block.privateKey = types.StringValue("privkey")
	block.iface = types.StringValue("wan")
	createVPNClient(t, r, state, identity, vpnClientModel(t, "", block.object(t)))

	for key, want := range map[string]string{
		"purpose":                 "vpn-client",
		"vpn_type":                "wireguard-client",
		"name":                    "tunnel",
		"ip_subnet":               "10.7.0.0/24",
		"x_wireguard_private_key": "privkey",
		"wireguard_interface":     "wan",
	} {
		if got := server.sent(t, key); got != want {
			t.Errorf("the controller was sent %s = %q, want %q", key, got, want)
		}
	}
	for _, key := range []string{"enabled", "vpn_client_default_route", "vpn_client_pull_dns"} {
		if _, present := server.lastBody(t)[key]; !present {
			t.Errorf("the controller was sent no %s; it carries no omitempty and the "+
				"practitioner set it", key)
		}
	}
}

// private_key_wo overrides whatever Encode already put on the wire, and can
// only do that from req.Config: a write-only attribute's plan value is null
// by the time Create runs, which is why this builds Plan and Config from two
// different models rather than through createVPNClient's one-model helper.
// decodeVPNClientWireguard's guard reads prior (the plan) to tell a
// write-only apply from a config-managed one, which is why the controller's
// echo must not land in state either.
func TestVPNClientCreateSendsTheWriteOnlyKeyInsteadOfTheConfiguredOne(t *testing.T) {
	ctx := context.Background()
	server := &networkServer{}
	r, state, identity := vpnClientHarness(t, server.start(t))

	planned := emptyWireguardBlock()
	planned.iface = types.StringValue("wan")
	planned.privateKeyWOVersion = types.Int64Value(1)
	plan := state
	if diags := plan.Set(ctx, vpnClientModel(t, "", planned.object(t))); diags.HasError() {
		t.Fatalf("set the plan: %v", diags)
	}

	configured := planned
	configured.privateKeyWO = types.StringValue("the-write-only-key")
	config := state
	if diags := config.Set(ctx, vpnClientModel(t, "", configured.object(t))); diags.HasError() {
		t.Fatalf("set the config: %v", diags)
	}

	resp := &fwresource.CreateResponse{State: state, Identity: &identity}
	r.Create(ctx, fwresource.CreateRequest{
		Plan: tfsdk.Plan(plan), Config: tfsdk.Config(config),
	}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("Create: %v", resp.Diagnostics)
	}

	if got := server.sent(t, "x_wireguard_private_key"); got != "the-write-only-key" {
		t.Errorf("x_wireguard_private_key = %q, want the write-only value", got)
	}

	var stored vpnClientCRUDModel
	if diags := resp.State.Get(ctx, &stored); diags.HasError() {
		t.Fatalf("read back state: %v", diags)
	}
	var wireguard wireguardModel
	if diags := stored.Wireguard.As(ctx, &wireguard, basetypes.ObjectAsOptions{}); diags.HasError() {
		t.Fatalf("read the wireguard block: %v", diags)
	}
	if !wireguard.PrivateKey.IsNull() {
		t.Errorf("private_key = %q, want null; the controller echoes the write-only key back "+
			"and it must not enter state", wireguard.PrivateKey.ValueString())
	}
	if !wireguard.PrivateKeyWO.IsNull() {
		t.Error("private_key_wo is in state; write-only must never be read back")
	}
	if wireguard.PrivateKeyWOVersion.ValueInt64() != 1 {
		t.Errorf("private_key_wo_version = %d, want 1", wireguard.PrivateKeyWOVersion.ValueInt64())
	}
}

// TestVPNClientUpdateRotatesTheWriteOnlyKeyOnAVersionBump is the update half:
// bumping private_key_wo_version alongside a new private_key_wo value is the
// only way write-only ever changes anything, since Terraform cannot compare
// its old and new values the way it compares a stored attribute's.
func TestVPNClientUpdateRotatesTheWriteOnlyKeyOnAVersionBump(t *testing.T) {
	ctx := context.Background()
	server := &networkServer{networks: manualModeNetwork()}
	r, state, identity := vpnClientHarness(t, server.start(t))

	before := emptyWireguardBlock()
	before.iface = types.StringValue("wan")
	before.privateKeyWOVersion = types.Int64Value(1)
	held := vpnClientModel(t, "net-1", before.object(t))
	if diags := state.Set(ctx, held); diags.HasError() {
		t.Fatalf("set the state: %v", diags)
	}

	rotated := before
	rotated.privateKeyWOVersion = types.Int64Value(2)
	planned := held
	planned.Wireguard = rotated.object(t)
	plan := state
	if diags := plan.Set(ctx, planned); diags.HasError() {
		t.Fatalf("set the plan: %v", diags)
	}

	configured := rotated
	configured.privateKeyWO = types.StringValue("rotated-key")
	config := held
	config.Wireguard = configured.object(t)
	configState := state
	if diags := configState.Set(ctx, config); diags.HasError() {
		t.Fatalf("set the config: %v", diags)
	}

	resp := &fwresource.UpdateResponse{State: state, Identity: &identity}
	r.Update(ctx, fwresource.UpdateRequest{
		Plan: tfsdk.Plan(plan), State: state, Config: tfsdk.Config(configState),
	}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("Update: %v", resp.Diagnostics)
	}

	if got := server.sent(t, "x_wireguard_private_key"); got != "rotated-key" {
		t.Errorf("x_wireguard_private_key = %q, want the rotated write-only value", got)
	}

	var stored vpnClientCRUDModel
	if diags := resp.State.Get(ctx, &stored); diags.HasError() {
		t.Fatalf("read back state: %v", diags)
	}
	var wireguard wireguardModel
	if diags := stored.Wireguard.As(ctx, &wireguard, basetypes.ObjectAsOptions{}); diags.HasError() {
		t.Fatalf("read the wireguard block: %v", diags)
	}
	if wireguard.PrivateKeyWOVersion.ValueInt64() != 2 {
		t.Errorf("private_key_wo_version = %d, want 2", wireguard.PrivateKeyWOVersion.ValueInt64())
	}
	if !wireguard.PrivateKey.IsNull() {
		t.Errorf("private_key = %q, want null", wireguard.PrivateKey.ValueString())
	}
}

// The controller's own file mode is not consistently supported, so the
// provider parses the configuration and writes manual mode with the peer
// fields it extracted -- which is why the read path needs its own prior-state
// branch (see TestVPNClientReadKeepsTheConfigurationBlockPriorStateHeld).
func TestVPNClientCreateConvertsAConfigurationFileToManualMode(t *testing.T) {
	server := &networkServer{}
	r, state, identity := vpnClientHarness(t, server.start(t))
	block := emptyWireguardBlock()
	block.configuration = configurationObject(t)
	createVPNClient(t, r, state, identity, vpnClientModel(t, "", block.object(t)))

	for key, want := range map[string]string{
		"wireguard_client_mode":            "manual",
		"wireguard_client_peer_public_key": "aFakePublicKeyForTestsOnlyNotRealBBBBBBBBBB=",
		"wireguard_client_peer_ip":         "198.51.100.7",
		"x_wireguard_private_key":          "aFakePrivateKeyForTestsOnlyNotRealAAAAAAAAAA=",
		"wireguard_client_preshared_key":   "aFakePresharedKeyForTestsOnlyNotRealCCCCCC=",
		"dhcpd_dns_1":                      "10.7.0.1",
		"dhcpd_dns_2":                      "10.7.0.53",
	} {
		if got := server.sent(t, key); got != want {
			t.Errorf("the controller was sent %s = %q, want %q from the configuration file",
				key, got, want)
		}
	}
	if got := server.sent(t, "wireguard_client_peer_port"); got != "51820" {
		t.Errorf("wireguard_client_peer_port = %s, want the endpoint's port", got)
	}
	// The file itself must not go: the alias has members for it, and sending
	// them is the file mode the provider deliberately doesn't use.
	for _, key := range []string{
		"wireguard_client_configuration_file",
		"wireguard_client_configuration_filename",
	} {
		if _, present := server.lastBody(t)[key]; present {
			t.Errorf("%s reached the controller; the provider converts to manual mode "+
				"rather than using the controller's file mode", key)
		}
	}
}

func readVPNClient(t *testing.T, r vpnClientCRUD, state tfsdk.State,
	identity tfsdk.ResourceIdentity, prior vpnClientCRUDModel,
) vpnClientCRUDModel {
	t.Helper()
	ctx := context.Background()
	if diags := state.Set(ctx, prior); diags.HasError() {
		t.Fatalf("set the state: %v", diags)
	}
	resp := &fwresource.ReadResponse{State: state, Identity: &identity}
	r.Read(ctx, fwresource.ReadRequest{State: state, Identity: &identity}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("Read: %v", resp.Diagnostics)
	}
	var got vpnClientCRUDModel
	if diags := resp.State.Get(ctx, &got); diags.HasError() {
		t.Fatalf("read back the state: %v", diags)
	}
	return got
}

// manualModeNetwork is what the controller reports for a client the provider
// configured from a file: manual mode, the peer fields, and NOTHING that says
// a file was ever involved.
func manualModeNetwork() []map[string]any {
	return []map[string]any{{
		"_id": "net-1", "name": "tunnel", "purpose": "vpn-client",
		"vpn_type": "wireguard-client", "enabled": true,
		"ip_subnet": "10.7.0.0/24", "vpn_client_default_route": true,
		"wireguard_client_mode":            "manual",
		"wireguard_client_peer_public_key": "aFakePublicKeyForTestsOnlyNotRealBBBBBBBBBB=",
		"wireguard_client_peer_ip":         "198.51.100.7",
		"wireguard_client_peer_port":       51820,
		"wireguard_interface":              "wan",
	}}
}

// The controller reports manual mode whichever the practitioner wrote, so a
// read that didn't ask prior state would replace the configuration block
// with a peer block on every refresh -- a permanent diff. This is why
// AfterReceive is given the prior model: Spec.ToModel overwrites the model
// before any other hook runs.
func TestVPNClientReadKeepsTheConfigurationBlockPriorStateHeld(t *testing.T) {
	server := &networkServer{networks: manualModeNetwork()}
	r, state, identity := vpnClientHarness(t, server.start(t))

	block := emptyWireguardBlock()
	block.configuration = configurationObject(t)
	block.privateKey = types.StringValue("the-key-the-controller-never-returns")
	got := readVPNClient(t, r, state, identity, vpnClientModel(t, "net-1", block.object(t)))

	var wireguard wireguardModel
	if diags := got.Wireguard.As(context.Background(), &wireguard,
		basetypes.ObjectAsOptions{}); diags.HasError() {
		t.Fatalf("read the wireguard block: %v", diags)
	}
	if wireguard.Configuration.IsNull() {
		t.Error("the configuration block is gone after a refresh; the controller reports " +
			"manual mode whichever the practitioner wrote, so this is a permanent diff")
	}
	if !wireguard.Peer.IsNull() {
		t.Error("a peer block appeared beside the configuration the practitioner wrote; " +
			"the two are alternatives and the schema accepts only one")
	}
	// The write-only secret comes from prior state or from nowhere; the
	// controller never returns it.
	if wireguard.PrivateKey.ValueString() != "the-key-the-controller-never-returns" {
		t.Errorf("private_key = %q after a refresh, want what state held; the controller "+
			"does not report it and there is nowhere else to get it",
			wireguard.PrivateKey.ValueString())
	}
}

// The control for the test above: without this, that test would also pass
// for a read that ignores the controller entirely. A practitioner who wrote
// a peer block must get the controller's peer back, not a preserved anything.
func TestVPNClientReadReportsThePeerWhenPriorStateHeldNoConfiguration(t *testing.T) {
	server := &networkServer{networks: manualModeNetwork()}
	r, state, identity := vpnClientHarness(t, server.start(t))

	got := readVPNClient(t, r, state, identity,
		vpnClientModel(t, "net-1", emptyWireguardBlock().object(t)))

	var wireguard wireguardModel
	if diags := got.Wireguard.As(context.Background(), &wireguard,
		basetypes.ObjectAsOptions{}); diags.HasError() {
		t.Fatalf("read the wireguard block: %v", diags)
	}
	if wireguard.Peer.IsNull() {
		t.Fatal("no peer block after a refresh although the controller reports manual mode " +
			"with peer fields; the read is not reading the controller")
	}
	var peer wireguardPeerModel
	if diags := wireguard.Peer.As(context.Background(), &peer,
		basetypes.ObjectAsOptions{}); diags.HasError() {
		t.Fatalf("read the peer block: %v", diags)
	}
	if peer.IP.ValueString() != "198.51.100.7" || peer.Port.ValueInt64() != 51820 {
		t.Errorf("peer = %s:%d, want the controller's endpoint",
			peer.IP.ValueString(), peer.Port.ValueInt64())
	}
	if !wireguard.Configuration.IsNull() {
		t.Error("a configuration block appeared for a practitioner who wrote none")
	}
}

// The mask must name only what the object carries: the mapper assigns seven
// wireguard wires conditionally, and networkMaskFor then drops any name the
// vpn-client encoder leaves out of the encoding. A name that survives either
// filter with nothing behind it would clear the controller's value, since
// go-unifi sends a masked field's zero.
func TestVPNClientUpdateMasksOnlyWhatTheObjectCarries(t *testing.T) {
	ctx := context.Background()
	server := &networkServer{networks: manualModeNetwork()}
	r, state, identity := vpnClientHarness(t, server.start(t))

	// A practitioner with a private key and an interface and nothing else:
	// no dns_servers, no configuration, no peer, no preshared key.
	block := emptyWireguardBlock()
	block.privateKey = types.StringValue("privkey")
	block.iface = types.StringValue("wan")
	held := vpnClientModel(t, "net-1", block.object(t))
	if diags := state.Set(ctx, held); diags.HasError() {
		t.Fatalf("set the state: %v", diags)
	}
	planned := held
	planned.Name = types.StringValue("tunnel-renamed")
	plan := state
	if diags := plan.Set(ctx, planned); diags.HasError() {
		t.Fatalf("set the plan: %v", diags)
	}

	resp := &fwresource.UpdateResponse{State: state, Identity: &identity}
	// Config mirrors the plan for the same reason createVPNClient's does: no
	// write-only value is under test here, and vpnClientBeforeSend now makes
	// Update read req.Config unconditionally.
	r.Update(ctx, fwresource.UpdateRequest{
		Plan:   tfsdk.Plan(plan),
		State:  state,
		Config: tfsdk.Config(plan),
	}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("Update: %v", resp.Diagnostics)
	}

	body := server.lastBody(t)
	for _, unwritten := range []string{
		"dhcpd_dns_1",
		"dhcpd_dns_2",
		"wireguard_client_mode",
		"wireguard_client_peer_public_key",
		"wireguard_client_peer_ip",
		"wireguard_client_peer_port",
		"wireguard_client_preshared_key",
	} {
		if _, sent := body[unwritten]; sent {
			t.Errorf("%s reached the controller although nothing wrote it; go-unifi sends "+
				"the masked zero and the controller's value is cleared", unwritten)
		}
	}
	// The other half, and not optional: everything above is also absent from
	// a write that sends nothing at all, which is the write-drop pointing
	// the other way.
	for _, written := range []string{"name", "x_wireguard_private_key", "wireguard_interface"} {
		if _, sent := body[written]; !sent {
			t.Errorf("%s did not reach the controller although the plan sets it", written)
		}
	}
	// purpose and vpn_type are constants no attribute carries, so a masked
	// update deriving its names from the plan alone would omit both; they
	// reach the mask only by being declared in AlwaysWire.
	for _, constant := range []string{"purpose", "vpn_type"} {
		if _, sent := body[constant]; !sent {
			t.Errorf("the update did not send %s; no attribute carries it, so it reaches "+
				"the mask only by being declared", constant)
		}
	}
	if got := server.sent(t, "purpose"); got != "vpn-client" {
		t.Errorf("purpose = %q, want vpn-client: it selects the encoder", got)
	}
	if got := server.sent(t, "name"); got != "tunnel-renamed" {
		t.Errorf("name = %q, want the planned one", got)
	}
}

func TestVPNClientDeleteAsksForTheNetworkById(t *testing.T) {
	ctx := context.Background()
	server := &networkServer{networks: manualModeNetwork()}
	r, state, identity := vpnClientHarness(t, server.start(t))
	_ = identity
	if diags := state.Set(ctx, vpnClientModel(t, "net-1",
		emptyWireguardBlock().object(t))); diags.HasError() {
		t.Fatalf("set the state: %v", diags)
	}
	resp := &fwresource.DeleteResponse{State: state}
	r.Delete(ctx, fwresource.DeleteRequest{State: state}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("Delete: %v", resp.Diagnostics)
	}
	if server.deleted != "/proxy/network/api/s/default/rest/networkconf/net-1" {
		t.Errorf("deleted %q, want the network's own path", server.deleted)
	}
}
