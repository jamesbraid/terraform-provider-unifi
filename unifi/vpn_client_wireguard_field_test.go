package unifi

import (
	"context"
	"slices"
	"strconv"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/resourcekit"
)

func wireguardSpec(wires []string) resourcekit.Spec[vpnClientResourceModel, ui.Network] {
	field := vpnClientWireguardField()
	field.Wires = wires
	return resourcekit.Spec[vpnClientResourceModel, ui.Network]{
		TypeName: "unifi_vpn_client",
		Fields:   []resourcekit.Field[vpnClientResourceModel, ui.Network]{field},
	}
}

func wireguardPlan(t *testing.T) *vpnClientResourceModel {
	t.Helper()
	dns, diags := types.ListValueFrom(context.Background(), types.StringType,
		[]string{"10.0.0.1", "10.0.0.2"})
	if diags.HasError() {
		t.Fatal(diags)
	}
	value := wireguardModel{
		PrivateKey:          types.StringValue("privkey"),
		Configuration:       types.ObjectNull(wireguardConfigurationModel{}.AttributeTypes()),
		Peer:                types.ObjectNull(wireguardPeerModel{}.AttributeTypes()),
		PresharedKeyEnabled: types.BoolValue(false),
		PresharedKey:        types.StringNull(),
		Interface:           types.StringValue("wan"),
		DnsServers:          dns,
	}
	object, d := types.ObjectValueFrom(context.Background(), value.AttributeTypes(), value)
	if d.HasError() {
		t.Fatal(d)
	}
	return &vpnClientResourceModel{Wireguard: object}
}

// Checks names against the real unifi.Network's tags rather than a
// synthetic struct, so a wrong name is the one a descriptor author would
// actually have written.
func TestWireguardFieldNamesAreAttributesOfNetwork(t *testing.T) {
	if problems := resourcekit.WireNameProblems(wireguardSpec(vpnClientWireguardWires())); len(problems) != 0 {
		t.Errorf("the wireguard field names attributes unifi.Network does not have:\n  %v", problems)
	}

	// The trap, as a control: the Go field is WireguardPrivateKey, so a name
	// transcribed from the struct rather than the tag would be
	// wireguard_private_key. Without this, the assertion above would pass
	// against a check that approves anything.
	guessed := slices.Clone(vpnClientWireguardWires())
	guessed[slices.Index(guessed, "x_wireguard_private_key")] = "wireguard_private_key"
	problems := resourcekit.WireNameProblems(wireguardSpec(guessed))
	if len(problems) != 1 {
		t.Fatalf("wireguard_private_key produced %d problem(s), want 1: %v", len(problems), problems)
	}
}

// Encode writes only three wires unconditionally (private key, interface,
// preshared-key flag), so this needs a plan exercising every path, not a
// bare wireguard block.
func TestWireguardFieldPutsAllTenNamesInTheMask(t *testing.T) {
	fields, err := wireguardSpec(vpnClientWireguardWires()).WireFields(wireguardPlanWritingEverything(t))
	if err != nil {
		t.Fatalf("WireFields: %v", err)
	}
	if len(fields) != 10 {
		t.Errorf("the mask carries %d name(s), want 10: %v", len(fields), fields)
	}
	for _, name := range []string{"dhcpd_dns_1", "dhcpd_dns_2", "x_wireguard_private_key"} {
		if !slices.Contains(fields, name) {
			t.Errorf("%s is missing from the mask, so the value is written and never sent", name)
		}
	}
}

// An author who enumerates the wires by grepping the SDK for "Wireguard"
// gets eight, missing the two dhcpd_dns_ names -- Encode still writes them,
// so the values land on the struct, the mask omits them, and the apply
// succeeds having sent neither.
func TestWireguardFieldWithTheDNSNamesMissingWritesThemAndCannotSendThem(t *testing.T) {
	ctx := context.Background()
	incomplete := slices.DeleteFunc(slices.Clone(vpnClientWireguardWires()), func(name string) bool {
		return name == "dhcpd_dns_1" || name == "dhcpd_dns_2"
	})

	network := &ui.Network{}
	if diags := encodeVPNClientWireguard(ctx, wireguardPlanWritingEverything(t).Wireguard, network); diags.HasError() {
		t.Fatalf("Encode: %v", diags)
	}
	// Control: the value really is written, or the absence below proves nothing.
	if network.DHCPDDNS1 != "10.0.0.1" {
		t.Fatalf("Encode did not write dhcpd_dns_1 (%q), so its absence from the mask "+
			"is not a demonstration of anything", network.DHCPDDNS1)
	}

	fields, err := wireguardSpec(incomplete).WireFields(wireguardPlanWritingEverything(t))
	if err != nil {
		t.Fatalf("WireFields: %v", err)
	}
	if slices.Contains(fields, "dhcpd_dns_1") {
		t.Fatal("the incomplete list still masked dhcpd_dns_1; the case is not set up")
	}
	t.Logf("Encode wrote dhcpd_dns_1=%q and the mask of %d name(s) does not carry it: "+
		"the apply succeeds and the controller keeps its old DNS",
		network.DHCPDDNS1, len(fields))
}

func TestWireguardFieldRoundTripsWhatTheControllerReturns(t *testing.T) {
	ctx := context.Background()
	field := vpnClientWireguardField()

	network := &ui.Network{}
	model := wireguardPlan(t)
	if diags := field.ToSDK(ctx, model, network); diags.HasError() {
		t.Fatalf("ToSDK: %v", diags)
	}
	// Asserted on the struct fields: a symmetric pair writing everything to one
	// field would round trip and be wrong.
	if network.WireguardPrivateKey == nil || *network.WireguardPrivateKey != "privkey" {
		t.Error("private key did not land")
	}
	if network.WireguardInterface == nil || *network.WireguardInterface != "wan" {
		t.Error("interface did not land")
	}
	if network.DHCPDDNS1 != "10.0.0.1" || network.DHCPDDNS2 != "10.0.0.2" {
		t.Errorf("dns servers landed as %q/%q", network.DHCPDDNS1, network.DHCPDDNS2)
	}

	back := &vpnClientResourceModel{}
	if diags := field.ToModel(ctx, network, back); diags.HasError() {
		t.Fatalf("ToModel: %v", diags)
	}
	var decoded wireguardModel
	if d := back.Wireguard.As(ctx, &decoded, basetypes.ObjectAsOptions{}); d.HasError() {
		t.Fatal(d)
	}
	if decoded.Interface.ValueString() != "wan" {
		t.Errorf("interface came back as %q", decoded.Interface.ValueString())
	}
	// private_key is Required in the schema, so a Decode that nulled it would
	// disagree with the config on every refresh.
	if decoded.PrivateKey.ValueString() != "privkey" {
		t.Errorf("private key came back as %q, want %q", decoded.PrivateKey.ValueString(), "privkey")
	}
}

// The controller echoes back whatever private key it holds -- Encode sent
// it, so Decode sees it on network -- regardless of how the practitioner
// supplied it. prior.PrivateKey null is the only signal that the write-only
// path made this apply.
func TestDecodeVPNClientWireguardNeverLeaksAWriteOnlyKeyBackIntoState(t *testing.T) {
	ctx := context.Background()
	echoed := "must-not-enter-state"
	network := &ui.Network{WireguardPrivateKey: &echoed}

	prior := wireguardModel{
		PrivateKey:          types.StringNull(),
		PrivateKeyWO:        types.StringNull(),
		PrivateKeyWOVersion: types.Int64Value(7),
		Configuration:       types.ObjectNull(wireguardConfigurationModel{}.AttributeTypes()),
		Peer:                types.ObjectNull(wireguardPeerModel{}.AttributeTypes()),
		PresharedKeyEnabled: types.BoolValue(false),
		PresharedKey:        types.StringNull(),
		Interface:           types.StringValue("wan"),
		DnsServers:          types.ListNull(types.StringType),
	}
	priorObject, d := types.ObjectValueFrom(ctx, prior.AttributeTypes(), prior)
	if d.HasError() {
		t.Fatalf("building the prior object: %v", d)
	}

	object, diags := decodeVPNClientWireguard(ctx, network, priorObject)
	if diags.HasError() {
		t.Fatalf("Decode: %v", diags)
	}
	var got wireguardModel
	if d := object.As(ctx, &got, basetypes.ObjectAsOptions{}); d.HasError() {
		t.Fatalf("reading back the decoded object: %v", d)
	}
	if !got.PrivateKey.IsNull() {
		t.Errorf("private_key = %q, want null; the controller's echo of a write-only-managed "+
			"key must not enter state", got.PrivateKey.ValueString())
	}
	if !got.PrivateKeyWO.IsNull() {
		t.Error("private_key_wo decoded to a non-null value; write-only must never be read back")
	}
	// private_key_wo_version has no wire: it's a rotation counter the
	// provider invents, so a bare refresh can only get it from prior.
	if got.PrivateKeyWOVersion.ValueInt64() != 7 {
		t.Errorf("private_key_wo_version = %d, want 7 carried forward from prior state",
			got.PrivateKeyWOVersion.ValueInt64())
	}
}

// TestDecodeVPNClientWireguardKeepsRoundTrippingAConfigManagedKey is the
// control for the test above: a practitioner using plain private_key (prior
// non-null) must keep seeing the controller's echo, or the write-only guard
// is silently nulling every apply rather than only the write-only ones.
func TestDecodeVPNClientWireguardKeepsRoundTrippingAConfigManagedKey(t *testing.T) {
	ctx := context.Background()
	echoed := "privkey"
	network := &ui.Network{WireguardPrivateKey: &echoed}

	prior := wireguardModel{
		PrivateKey:          types.StringValue("privkey"),
		Configuration:       types.ObjectNull(wireguardConfigurationModel{}.AttributeTypes()),
		Peer:                types.ObjectNull(wireguardPeerModel{}.AttributeTypes()),
		PresharedKeyEnabled: types.BoolValue(false),
		PresharedKey:        types.StringNull(),
		Interface:           types.StringValue("wan"),
		DnsServers:          types.ListNull(types.StringType),
	}
	priorObject, d := types.ObjectValueFrom(ctx, prior.AttributeTypes(), prior)
	if d.HasError() {
		t.Fatalf("building the prior object: %v", d)
	}

	object, diags := decodeVPNClientWireguard(ctx, network, priorObject)
	if diags.HasError() {
		t.Fatalf("Decode: %v", diags)
	}
	var got wireguardModel
	if d := object.As(ctx, &got, basetypes.ObjectAsOptions{}); d.HasError() {
		t.Fatalf("reading back the decoded object: %v", d)
	}
	if got.PrivateKey.ValueString() != "privkey" {
		t.Errorf("private_key = %q, want %q; a config-managed key must keep round-tripping",
			got.PrivateKey.ValueString(), "privkey")
	}
}

// wireguardPlanWithout builds the same plan with one member cleared, so the
// conditional-wire cases differ from the ten-name case by exactly the thing
// under test.
func wireguardPlanWithoutDNS(t *testing.T) *vpnClientResourceModel {
	t.Helper()
	value := wireguardModel{
		PrivateKey:          types.StringValue("privkey"),
		Configuration:       types.ObjectNull(wireguardConfigurationModel{}.AttributeTypes()),
		Peer:                types.ObjectNull(wireguardPeerModel{}.AttributeTypes()),
		PresharedKeyEnabled: types.BoolValue(false),
		PresharedKey:        types.StringNull(),
		Interface:           types.StringValue("wan"),
		DnsServers:          types.ListNull(types.StringType),
	}
	object, d := types.ObjectValueFrom(context.Background(), value.AttributeTypes(), value)
	if d.HasError() {
		t.Fatal(d)
	}
	return &vpnClientResourceModel{Wireguard: object}
}

// go-unifi sends a masked field's zero when the object carries no value, so
// masking dhcpd_dns_1 on an apply with no dns_servers would blank the
// controller's DNS.
func TestWireguardFieldDropsTheDNSNamesWhenNothingWillWriteThem(t *testing.T) {
	ctx := context.Background()
	plan := wireguardPlanWithoutDNS(t)

	// Control first: Encode really does leave them empty, or their absence
	// from the mask below is protecting nothing.
	network := &ui.Network{}
	if diags := encodeVPNClientWireguard(ctx, plan.Wireguard, network); diags.HasError() {
		t.Fatalf("Encode: %v", diags)
	}
	if network.DHCPDDNS1 != "" || network.DHCPDDNS2 != "" {
		t.Fatalf("Encode wrote dhcpd_dns_1=%q dhcpd_dns_2=%q for a plan with no dns_servers; "+
			"this case is not the one it is named for", network.DHCPDDNS1, network.DHCPDDNS2)
	}

	fields, err := wireguardSpec(vpnClientWireguardWires()).WireFields(plan)
	if err != nil {
		t.Fatalf("WireFields: %v", err)
	}
	for _, name := range []string{"dhcpd_dns_1", "dhcpd_dns_2"} {
		if slices.Contains(fields, name) {
			t.Errorf("%s is in the mask although Encode left it empty; the update sends \"\" "+
				"and the controller's DNS is blanked", name)
		}
	}
	// The three unconditional wires still travel: a field masking nothing
	// looks identical to a correct narrowing from here. This plan sets no
	// configuration, no peer and no preshared key, so the five other
	// conditional wires are correctly absent too.
	want := []string{
		"x_wireguard_private_key",
		"wireguard_interface",
		"wireguard_client_preshared_key_enabled",
	}
	if !slices.Equal(fields, want) {
		t.Errorf("the mask carries %v, want exactly the unconditional %v", fields, want)
	}
}

// A configuration file supplies DNS only sometimes, and the mask has to
// follow which sometimes.
func TestWireguardFieldDNSNamesFollowWhatTheConfigurationSupplies(t *testing.T) {
	for _, testCase := range []struct {
		name    string
		content string
		masked  bool
	}{
		{"two DNS entries", "W0ludGVyZmFjZV0KUHJpdmF0ZUtleSA9IGFXUmxiblJwZEhsclpYbHBaR1Z1ZEdsMGVXdGxlV2xrWlc1MGFYUjVNREE9CkFkZHJlc3MgPSAxMC4wLjAuMi8zMgpETlMgPSAxLjEuMS4xLCA4LjguOC44CgpbUGVlcl0KUHVibGljS2V5ID0gY0dWbGNuQjFZbXhwWTJ0bGVYQmxaWEp3ZFdKc2FXTnJaWGx3WldWeU1EQT0KRW5kcG9pbnQgPSAyMDMuMC4xMTMuMTA6NTE4MjAK", true},
		{"a valid file with no DNS", "W0ludGVyZmFjZV0KUHJpdmF0ZUtleSA9IGFXUmxiblJwZEhsclpYbHBaR1Z1ZEdsMGVXdGxlV2xrWlc1MGFYUjVNREE9CkFkZHJlc3MgPSAxMC4wLjAuMi8zMgoKW1BlZXJdClB1YmxpY0tleSA9IGNHVmxjbkIxWW14cFkydGxlWEJsWlhKd2RXSnNhV05yWlhsd1pXVnlNREE9CkVuZHBvaW50ID0gMjAzLjAuMTEzLjEwOjUxODIwCg==", false},
		{"a file that does not parse", "Zm9v", false},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			configuration, d := types.ObjectValue(wireguardConfigurationModel{}.AttributeTypes(),
				map[string]attr.Value{
					"content":  types.StringValue(testCase.content),
					"filename": types.StringValue("wg0.conf"),
				})
			if d.HasError() {
				t.Fatal(d)
			}
			plan := wireguardPlanWithoutDNS(t)
			attributes := plan.Wireguard.Attributes()
			attributes["configuration"] = configuration
			object, d := types.ObjectValue(wireguardModel{}.AttributeTypes(), attributes)
			if d.HasError() {
				t.Fatal(d)
			}
			plan.Wireguard = object

			fields, err := wireguardSpec(vpnClientWireguardWires()).WireFields(plan)
			if err != nil {
				t.Fatalf("WireFields: %v", err)
			}
			// Control: a wire that is never conditional must be present in
			// every case, or an empty mask would satisfy the false rows.
			if !slices.Contains(fields, "wireguard_interface") {
				t.Fatalf("the unconditional wires are missing too; the mask is empty: %v", fields)
			}
			for _, name := range []string{"dhcpd_dns_1", "dhcpd_dns_2"} {
				if got := slices.Contains(fields, name); got != testCase.masked {
					t.Errorf("%s masked=%v, want %v", name, got, testCase.masked)
				}
			}
		})
	}
}

// wireguardPlanWritingEverything exercises every path through Encode, so that
// all ten wires are written and all ten must therefore be masked.
//
// peer rather than configuration, because the two are the arms of one switch
// and only one can run. Both write the same four wires; the peer arm needs no
// base64 fixture to parse.
func wireguardPlanWritingEverything(t *testing.T) *vpnClientResourceModel {
	t.Helper()
	ctx := context.Background()
	dns, diags := types.ListValueFrom(ctx, types.StringType, []string{"10.0.0.1", "10.0.0.2"})
	if diags.HasError() {
		t.Fatal(diags)
	}
	peer, d := types.ObjectValue(wireguardPeerModel{}.AttributeTypes(), map[string]attr.Value{
		"ip":         types.StringValue("198.51.100.7"),
		"port":       types.Int64Value(51820),
		"public_key": types.StringValue("pubkey"),
	})
	if d.HasError() {
		t.Fatal(d)
	}
	value := wireguardModel{
		PrivateKey:          types.StringValue("privkey"),
		Configuration:       types.ObjectNull(wireguardConfigurationModel{}.AttributeTypes()),
		Peer:                peer,
		PresharedKeyEnabled: types.BoolValue(true),
		PresharedKey:        types.StringValue("psk"),
		Interface:           types.StringValue("wan"),
		DnsServers:          dns,
	}
	object, d := types.ObjectValueFrom(ctx, value.AttributeTypes(), value)
	if d.HasError() {
		t.Fatal(d)
	}
	return &vpnClientResourceModel{Wireguard: object}
}

// wireguardWireValues reads the ten wires off an SDK object as comparable
// strings. All ten have a case, not just the conditional ones, because the
// derivation below would otherwise be blind to the missing one and it would
// read as unconditional.
func wireguardWireValues(network *ui.Network) map[string]string {
	text := func(p *string) string {
		if p == nil {
			return "<nil>"
		}
		return *p
	}
	number := func(p *int64) string {
		if p == nil {
			return "<nil>"
		}
		return strconv.FormatInt(*p, 10)
	}
	return map[string]string{
		"x_wireguard_private_key":                text(network.WireguardPrivateKey),
		"wireguard_interface":                    text(network.WireguardInterface),
		"wireguard_client_preshared_key_enabled": strconv.FormatBool(network.WireguardClientPresharedKeyEnabled),
		"wireguard_client_preshared_key":         text(network.WireguardClientPresharedKey),
		"wireguard_client_mode":                  text(network.WireguardClientMode),
		"wireguard_client_peer_public_key":       text(network.WireguardClientPeerPublicKey),
		"wireguard_client_peer_ip":               text(network.WireguardClientPeerIP),
		"wireguard_client_peer_port":             number(network.WireguardClientPeerPort),
		"dhcpd_dns_1":                            network.DHCPDDNS1,
		"dhcpd_dns_2":                            network.DHCPDDNS2,
	}
}

// writtenWires reports which wires Encode ASSIGNS -- not which are non-zero,
// since a deliberate write of the zero value looks like no write. Encode
// runs over two differently-seeded objects; an assigned field ends up the
// same in both, an untouched one keeps its seed.
func writtenWires(t *testing.T, plan *vpnClientResourceModel) []string {
	t.Helper()
	ctx := context.Background()
	encode := func(seed *ui.Network) map[string]string {
		if diags := encodeVPNClientWireguard(ctx, plan.Wireguard, seed); diags.HasError() {
			t.Fatalf("Encode: %v", diags)
		}
		return wireguardWireValues(seed)
	}
	sentinel := "SEEDED-NOT-WRITTEN"
	port := int64(65001)
	first := encode(&ui.Network{})
	second := encode(&ui.Network{
		WireguardPrivateKey:                &sentinel,
		WireguardInterface:                 &sentinel,
		WireguardClientPresharedKeyEnabled: true,
		WireguardClientPresharedKey:        &sentinel,
		WireguardClientMode:                &sentinel,
		WireguardClientPeerPublicKey:       &sentinel,
		WireguardClientPeerIP:              &sentinel,
		WireguardClientPeerPort:            &port,
		DHCPDDNS1:                          sentinel,
		DHCPDDNS2:                          sentinel,
	})

	var written []string
	for _, wire := range vpnClientWireguardWires() {
		before, ok := first[wire]
		if !ok {
			t.Fatalf("%s has no reader here, so the derivation cannot see it and it would "+
				"read as unconditional", wire)
		}
		if before == second[wire] {
			written = append(written, wire)
		}
	}
	slices.Sort(written)
	return written
}

// The conditional set is derived from Encode's behaviour rather than from
// the declaration -- taking the population from the declaration would let a
// missing entry simply stop being checked.
func TestTheDeclaredConditionalWiresAreTheOnesEncodeWritesConditionally(t *testing.T) {
	field := vpnClientWireguardField()
	always := writtenWires(t, wireguardPlanWithoutDNS(t))
	everything := writtenWires(t, wireguardPlanWritingEverything(t))

	var derived []string
	for _, wire := range everything {
		if !slices.Contains(always, wire) {
			derived = append(derived, wire)
		}
	}
	slices.Sort(derived)

	declared := conditionalWiresOf(t, field)
	if !slices.Equal(declared, derived) {
		t.Errorf("ConditionalWires declares %v and Encode writes %v conditionally.\n"+
			"A wire Encode writes only sometimes and the descriptor calls unconditional is "+
			"masked with nothing behind it, and go-unifi sends the zero -- the controller's "+
			"value is cleared. One the descriptor calls conditional and Encode always writes "+
			"leaves the mask and the write is silently dropped.", declared, derived)
	}
	if len(derived) == 0 {
		t.Fatal("nothing reads as conditional; the two plans do not differ and this proves nothing")
	}
}

// Each conditional wire is checked in both directions; a predicate that
// always returned false would pass one-way while masking nothing and
// silently dropping every write.
func TestEveryConditionalWireAgreesWithWhatEncodeWrites(t *testing.T) {
	field := vpnClientWireguardField()

	written := func(t *testing.T, plan *vpnClientResourceModel, wire string) bool {
		t.Helper()
		return slices.Contains(writtenWires(t, plan), wire)
	}

	// Derived, not listed, for the same reason as the test above.
	conditional := conditionalWiresOf(t, field)
	if len(conditional) == 0 {
		t.Fatal("no conditional wires found; this test cannot fail and is worthless")
	}
	t.Logf("checking %d conditional wire(s) of %d", len(conditional), len(vpnClientWireguardWires()))

	noPaths := wireguardPlanWithoutDNS(t) // no dns_servers, no configuration, no peer, psk off
	allPaths := wireguardPlanWritingEverything(t)

	for _, wire := range conditional {
		t.Run(wire, func(t *testing.T) {
			predicate := field.ConditionalWires[wire]

			if predicate(noPaths.Wireguard) {
				t.Errorf("the predicate says %s will be written for a plan that sets none of "+
					"its paths", wire)
			} else if written(t, noPaths, wire) {
				t.Errorf("the predicate says %s will NOT be written and Encode wrote it; "+
					"the wire leaves the mask and the value is silently dropped", wire)
			}

			if !predicate(allPaths.Wireguard) {
				t.Errorf("the predicate says %s will not be written for a plan that sets "+
					"every path", wire)
			} else if !written(t, allPaths, wire) {
				t.Errorf("the predicate says %s WILL be written and Encode did not; the wire "+
					"joins the mask with nothing behind it and the controller's value is cleared",
					wire)
			}
		})
	}
}

func conditionalWiresOf(
	t *testing.T,
	field resourcekit.ScatteredObjectField[vpnClientResourceModel, ui.Network],
) []string {
	t.Helper()
	names := make([]string, 0, len(field.ConditionalWires))
	for name := range field.ConditionalWires {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}
