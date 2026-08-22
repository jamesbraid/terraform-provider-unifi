package unifi

// Framework-level CRUD for vpn_client, written against the HAND-WRITTEN
// resource and required to pass unchanged after the cutover.
//
// THIS SURFACE'S BEHAVIOUR IS MOSTLY PRIOR-STATE CARRY-FORWARD, which is the
// part a migration silently loses. The practitioner supplies a wireguard
// CONFIG FILE, the provider parses it and sends the controller manual mode, and
// the controller reports manual mode forever -- so five attributes come from
// what was there before rather than from the wire. None of it was asserted
// anywhere.
//
// THE FIXTURE IS READABLE IN THE SOURCE AND ENCODED AT TEST TIME, deliberately.
// A base64 literal was used here once as a stand-in for a configuration file
// without anyone decoding it: "Zm9v" is "foo", it does not parse, Encode raises
// a diagnostic and writes nothing, and the test that used it ended up asserting
// the destructive behaviour while reading as protective. A literal standing in
// for a measurement.

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

// THE ALIAS IS THE CUTOVER'S SEAM. Repointing these three lines is the only
// edit the cutover may make to this file, so a behaviour that changes has to
// change a test rather than be absorbed by one rewritten alongside the code.
type (
	vpnClientCRUD      = *vpnClientResource
	vpnClientCRUDModel = vpnClientResourceModel
)

func newVPNClientCRUD() vpnClientCRUD { return &vpnClientResource{} }

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
	// THE FIXTURE IS CHECKED BEFORE IT IS USED. A configuration that does not
	// parse makes Encode write nothing, and every assertion downstream then
	// describes the failure path while reading as though it describes the
	// feature.
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

// networkServer answers the networkconf paths and keeps the raw body of every
// write, for the reason port_forward's fake does: decoding answers which values
// went out and loses which keys went out at all, and the second is the whole
// difference between a whole-object write and a masked one.
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
			// ECHOED AS A MAP, NOT AS ui.Network. A zero Network cannot marshal
			// at all -- MarshalJSON switches on Purpose and errors on an unknown
			// one -- so a fake that round-trips through the SDK type has to seed
			// the discriminator. Echoing what arrived keeps the seed the
			// provider's own.
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
	privateKey    types.String
	configuration types.Object
	peer          types.Object
	presharedOn   bool
	presharedKey  types.String
	iface         types.String
	dnsServers    types.List
}

func emptyWireguardBlock() wireguardBlock {
	return wireguardBlock{
		privateKey:    types.StringNull(),
		configuration: types.ObjectNull(wireguardConfigurationModel{}.AttributeTypes()),
		peer:          types.ObjectNull(wireguardPeerModel{}.AttributeTypes()),
		presharedKey:  types.StringNull(),
		iface:         types.StringNull(),
		dnsServers:    types.ListNull(types.StringType),
	}
}

func (b wireguardBlock) object(t *testing.T) types.Object {
	t.Helper()
	value := wireguardModel{
		PrivateKey:          b.privateKey,
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

// TWO OF THE SEVEN TOP-LEVEL WIRES ARE CONSTANTS AND CARRY NO ATTRIBUTE.
// purpose selects the encoder, so getting it wrong changes which of go-unifi's
// seven alias structs serialises the object -- and vpn_type is what makes the
// controller treat the network as a wireguard client at all.
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

// FILE MODE IS NOT SENT AS FILE MODE. The controller's own file mode is not
// consistently supported, so the provider parses the configuration and writes
// manual mode with the peer fields it extracted. Everything downstream --
// including the read path's whole prior-state branch -- exists because of this
// conversion, so it is asserted against a fixture whose contents are visible.
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
	// THE FILE ITSELF MUST NOT GO. The alias has members for it and sending
	// them is the mode the provider deliberately does not use.
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

// THE READ CANNOT TELL FILE MODE FROM MANUAL MODE AND HAS TO ASK PRIOR STATE.
//
// The provider converts a configuration file to manual mode on the way out, so
// the controller reports manual mode whichever the practitioner wrote. A read
// that believed the controller would replace the `configuration` block with a
// `peer` block on every refresh -- a permanent diff against a configuration
// that has not changed.
//
// This is the behaviour that has no seam on the kit's path: Spec.ToModel
// overwrites the model before any hook runs. It is the reason AfterReceive was
// given the prior model.
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
	// THE WRITE-ONLY SECRET COMES FROM PRIOR STATE OR FROM NOWHERE. The
	// controller never returns it.
	if wireguard.PrivateKey.ValueString() != "the-key-the-controller-never-returns" {
		t.Errorf("private_key = %q after a refresh, want what state held; the controller "+
			"does not report it and there is nowhere else to get it",
			wireguard.PrivateKey.ValueString())
	}
}

// THE CONTROL, AND WITHOUT IT THE TEST ABOVE PASSES FOR A READ THAT IGNORES THE
// CONTROLLER ENTIRELY. A practitioner who wrote a peer block rather than a file
// must get the controller's peer back, not a preserved anything.
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

// THE UPDATE MASK MUST NAME ONLY WHAT THE OBJECT CARRIES, and on this surface
// two separate filters make that true today: the mapper assigns seven of the
// wireguard wires conditionally, and networkMaskFor then drops any name the
// vpn-client encoder leaves out of the encoding.
//
// NOTHING HAS EVER ASSERTED IT. The declared mask has fifteen names and an
// empty object reduces it to five, so the filter is doing the work rather than
// the list. Whatever replaces it inherits that, and go-unifi sends a masked
// field's ZERO -- so a name that survives with nothing behind it clears the
// controller's value.
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
	r.Update(ctx, fwresource.UpdateRequest{Plan: tfsdk.Plan(plan), State: state}, resp)
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
	// THE OTHER HALF, AND IT IS NOT OPTIONAL. Everything above is also absent
	// from a write that sends nothing at all, which is the silent write-drop
	// pointing the other way.
	for _, written := range []string{"name", "x_wireguard_private_key", "wireguard_interface"} {
		if _, sent := body[written]; !sent {
			t.Errorf("%s did not reach the controller although the plan sets it", written)
		}
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
