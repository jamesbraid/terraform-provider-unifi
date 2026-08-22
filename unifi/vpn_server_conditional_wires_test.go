package unifi

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/resourcekit"
)

// EVERY CONDITIONAL WIRE, EXERCISED IN BOTH DIRECTIONS, AGAINST WHAT ENCODE
// ACTUALLY WRITES.
//
// The declaration is the protection. A wire Encode writes only sometimes must
// say so, or the mask carries it on an apply that did not write it and go-unifi
// sends its zero over whatever the controller holds. On this surface ten of
// those zeros would land on certificates and private keys, and because the
// three VPN types are mutually exclusive it would happen on every apply rather
// than on some edge.
//
// ConditionalWireProblems is fed OBJECTS, not assertions: it runs Encode and
// compares what moved against what each predicate claims, and it reports any
// wire no object drove both true and false rather than passing quietly on a
// half-exercised set.
func TestVPNServerConditionalWiresAgreeWithEncode(t *testing.T) {
	ctx := context.Background()
	spec := vpnServerKitSpec()

	// The encoder dispatches on Purpose and a zero Network cannot marshal at
	// all, so the discriminator is supplied the way maskedBody requires.
	seed := func(n *ui.Network) {
		n.Purpose = ui.PurposeUserVPN
		n.VPNType = strPtr("openvpn-server")
	}

	object := func(t *testing.T, attrTypes map[string]attr.Type, values map[string]attr.Value) types.Object {
		t.Helper()
		built, diags := types.ObjectValue(attrTypes, values)
		if diags.HasError() {
			t.Fatalf("building object: %v", diags)
		}
		return built
	}
	str := func(s string) types.String {
		if s == "" {
			return types.StringNull()
		}
		return types.StringValue(s)
	}
	dnsObject := func(t *testing.T, servers ...string) types.Object {
		t.Helper()
		list := types.ListNull(types.StringType)
		if servers != nil {
			values := make([]attr.Value, len(servers))
			for i, s := range servers {
				values[i] = types.StringValue(s)
			}
			built, diags := types.ListValue(types.StringType, values)
			if diags.HasError() {
				t.Fatalf("building dns list: %v", diags)
			}
			list = built
		}
		return object(t, vpnServerDNSModel{}.AttributeTypes(), map[string]attr.Value{
			"enabled": types.BoolValue(true),
			"servers": list,
		})
	}
	openVPNObject := func(t *testing.T, filled bool) types.Object {
		v := func(s string) types.String {
			if filled {
				return types.StringValue(s)
			}
			return types.StringNull()
		}
		return object(t, vpnServerOpenVPNModel{}.AttributeTypes(), map[string]attr.Value{
			"port":              types.Int64Null(),
			"mode":              v("site-to-site"),
			"encryption_cipher": v("aes-256-gcm"),
			"server_crt":        v("server-crt"),
			"server_key":        v("server-key"),
			"dh_key":            v("dh-key"),
			"shared_client_key": v("shared-client-key"),
			"shared_client_crt": v("shared-client-crt"),
			"auth_key":          v("auth-key"),
			"ca_crt":            v("ca-crt"),
			"ca_key":            v("ca-key"),
		})
	}
	l2tpObject := func(t *testing.T, psk string) types.Object {
		return object(t, vpnServerL2TPModel{}.AttributeTypes(), map[string]attr.Value{
			"allow_weak_ciphers": types.BoolValue(false),
			"pre_shared_key":     str(psk),
		})
	}

	cases := map[string][]types.Object{
		// none, one and two servers: the only shape that drives dhcpd_dns_1 and
		// dhcpd_dns_2 to different answers, which is the split a shared
		// predicate would have hidden.
		"dns":  {dnsObject(t), dnsObject(t, "1.1.1.1"), dnsObject(t, "1.1.1.1", "8.8.8.8")},
		"l2tp": {l2tpObject(t, ""), l2tpObject(t, "a-pre-shared-key")},
		// every certificate set, then none: ten predicates, both directions.
		"openvpn": {openVPNObject(t, false), openVPNObject(t, true)},
	}

	// Without this the loop below could match nothing and report success over a
	// descriptor whose conditional wires were never looked at.
	matched := 0
	for _, field := range spec.Fields {
		scattered, ok := field.(resourcekit.ScatteredObjectField[vpnServerKitModel, ui.Network])
		if !ok || len(scattered.ConditionalWires) == 0 {
			continue
		}
		var objects []types.Object
		switch {
		case scattered.Model(&vpnServerKitModel{}) != nil && len(scattered.Wires) > 0 && scattered.Wires[0] == "dhcpd_dns_enabled":
			objects = cases["dns"]
		case scattered.Wires[0] == "l2tp_allow_weak_ciphers":
			objects = cases["l2tp"]
		default:
			objects = cases["openvpn"]
		}
		matched++
		for _, problem := range resourcekit.ConditionalWireProblems(scattered, objects, seed) {
			t.Errorf("%v: %s", scattered.Wires, problem)
		}
	}
	if matched != 3 {
		t.Fatalf("exercised %d scattered fields with conditional wires, want 3; the descriptor changed shape and this test stopped seeing it", matched)
	}
	_ = ctx
}
