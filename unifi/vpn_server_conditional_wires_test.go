package unifi

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/resourcekit"
)

// TestVPNServerConditionalWiresAgreeWithEncode exercises every conditional
// wire in both directions against what Encode actually writes. A wire Encode
// writes only sometimes must declare so, or the mask carries it on an apply
// that did not write it and go-unifi sends its zero over whatever the
// controller holds -- here that would land on certificates and private keys.
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
			// The port varies with `filled` so local_port is exercised in both
			// directions here too. Leaving it null in both objects would let
			// the check pass on a wire it never got to judge.
			"port": func() attr.Value {
				if filled {
					return types.Int64Value(1194)
				}
				return types.Int64Null()
			}(),
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

	wanObject := func(t *testing.T, filled bool) types.Object {
		v := func(x string) types.String {
			if filled {
				return types.StringValue(x)
			}
			return types.StringNull()
		}
		return object(t, vpnServerWANModel{}.AttributeTypes(), map[string]attr.Value{
			"ip":        v("203.0.113.10"),
			"interface": v("wan"),
		})
	}
	wireguardObject := func(t *testing.T, filled bool) types.Object {
		key := types.StringNull()
		port := types.Int64Null()
		if filled {
			key = types.StringValue("a-private-key")
			port = types.Int64Value(51820)
		}
		return object(t, vpnServerWireguardModel{}.AttributeTypes(), map[string]attr.Value{
			"private_key": key,
			"public_key":  types.StringNull(),
			"port":        port,
		})
	}

	// Every scattered field, not only the ones declaring predicates: the
	// check's population is field.Wires, so an UNDECLARED conditional wire is
	// reported by name -- the only case that destroys anything.
	byLeadWire := map[string][]types.Object{
		"dhcpd_dns_enabled":       {dnsObject(t), dnsObject(t, "1.1.1.1"), dnsObject(t, "1.1.1.1", "8.8.8.8")},
		"l2tp_allow_weak_ciphers": {l2tpObject(t, ""), l2tpObject(t, "a-pre-shared-key")},
		"local_port":              {openVPNObject(t, false), openVPNObject(t, true)},
		"x_wireguard_private_key": {wireguardObject(t, false), wireguardObject(t, true)},
		"wireguard_local_wan_ip":  {wanObject(t, false), wanObject(t, true)},
	}

	matched := 0
	for _, field := range spec.Fields {
		scattered, ok := field.(resourcekit.ScatteredObjectField[vpnServerKitModel, ui.Network])
		if !ok {
			continue
		}
		objects, known := byLeadWire[scattered.Wires[0]]
		if !known {
			t.Fatalf("scattered field leading with %q has no objects here; a new field must be exercised, not skipped", scattered.Wires[0])
		}
		matched++
		for _, problem := range resourcekit.ConditionalWireProblems(scattered, objects, seed) {
			t.Errorf("%s: %s", scattered.Wires[0], problem)
		}
	}
	if matched != 5 {
		t.Fatalf("exercised %d scattered fields, want 5; the descriptor changed shape and this test stopped seeing it", matched)
	}
	_ = ctx
}
