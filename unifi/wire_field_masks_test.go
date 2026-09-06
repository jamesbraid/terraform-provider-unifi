package unifi

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"

	ui "github.com/ubiquiti-community/go-unifi/unifi"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/resourcekit"
)

// --- derivations from source, shared by the surfaces that follow ---

// networkJSONTags maps each Network field to its json name.
func networkJSONTags(t *testing.T) map[string]string {
	t.Helper()
	out := map[string]string{}
	typ := reflect.TypeOf(ui.Network{})
	for i := range typ.NumField() {
		field := typ.Field(i)
		tag := field.Tag.Get("json")
		if tag == "" || tag == "-" {
			continue
		}
		name, _, _ := strings.Cut(tag, ",")
		if name != "" {
			out[field.Name] = name
		}
	}
	if len(out) == 0 {
		t.Fatal("Network has no json tags")
	}
	return out
}

// unifi_network gets its own tests rather than a row above: it writes THREE
// purposes, which the table's single-encoder shape can't express.

// A mask naming a field the purpose doesn't encode fails the apply outright
// (maskedBody refuses it) rather than silently no-op-ing; vlan-only and
// corporate encode very different field sets.
func TestNetworkMaskNamesOnlyWhatThePurposeEncodes(t *testing.T) {
	name := "probe"
	for _, purpose := range []string{
		ui.PurposeCorporate, ui.PurposeGuest, ui.PurposeVLANOnly,
	} {
		t.Run(purpose, func(t *testing.T) {
			network := &ui.Network{Purpose: purpose, Name: &name, Enabled: true}
			raw, err := json.Marshal(network)
			if err != nil {
				t.Fatalf("encoding a %s network: %v", purpose, err)
			}
			var encoded map[string]json.RawMessage
			if err := json.Unmarshal(raw, &encoded); err != nil {
				t.Fatalf("reading back: %v", err)
			}

			mask := networkWireFields(network)
			if len(mask) == 0 {
				t.Fatal("the mask is empty, so every assertion below would pass vacuously")
			}
			for _, field := range mask {
				if _, carried := encoded[field]; !carried {
					t.Errorf("the mask names %q, which a %s network does not encode; "+
						"go-unifi refuses a mask naming a dropped field", field, purpose)
				}
			}
		})
	}
}

// These fields must never be masked, on any purpose: they are what the
// whole-object write was resetting, and the resource assigns none of them.
func TestNetworkMaskExcludesTheFieldsItDoesNotManage(t *testing.T) {
	unmanaged := []string{
		"dhcpd_mac_1", "dhcpd_mac_2", "dhcpd_mac_3",
		// Spelled with one P on purpose: that's the controller's own spelling
		// (via go-unifi's json tag), while the Go field IGMPSuppression has two.
		"igmp_fastleave", "igmp_flood_unknown_multicast", "igmp_supression", //nolint:misspell // the controller's spelling
		"mac_override_enabled", "upnp_lan_enabled",
	}
	name := "probe"
	for _, purpose := range []string{
		ui.PurposeCorporate, ui.PurposeGuest, ui.PurposeVLANOnly,
	} {
		t.Run(purpose, func(t *testing.T) {
			network := &ui.Network{Purpose: purpose, Name: &name, Enabled: true}
			mask := networkWireFields(network)
			for _, field := range unmanaged {
				if slices.Contains(mask, field) {
					t.Errorf("%s is in the %s mask; the resource does not assign it and the "+
						"whole-object write was resetting it", field, purpose)
				}
			}
			// The positive control: a field the resource DOES manage is masked,
			// or the assertions above would hold for an empty mask.
			if !slices.Contains(mask, "name") {
				t.Errorf("name is missing from the %s mask, so a rename would not be written",
					purpose)
			}
		})
	}
}

// unifi_network must use the masked call. Separate from the table's version
// because its call passes networkWireFields(network) rather than a bare list.
func TestNetworkUpdateUsesTheMaskedCall(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "unifi", "network_descriptor.go"))
	if err != nil {
		t.Fatalf("reading the descriptor: %v", err)
	}
	src := string(raw)
	// The kit builds the mask from Fields and hands it to Backend.UpdateFields,
	// so what this can still check is that the masked call is the one wired up
	// and the whole-object one is not.
	if !strings.Contains(src, "UpdateNetworkFields(ctx, site, in, fields...)") {
		t.Error("Backend.UpdateFields does not call UpdateNetworkFields with the mask")
	}
	if regexp.MustCompile(`UpdateNetwork\(ctx`).MatchString(src) {
		t.Error("a whole-object UpdateNetwork( call remains in network_descriptor.go")
	}
	if !strings.Contains(src, "func networkKitBackend(") {
		t.Fatal("the file read is not network_descriptor.go; the assertions above prove nothing")
	}
}

// wanWireFields answers what the surface can write on ANY plan -- the
// descriptor's Fields wires plus AlwaysWire -- narrowed to what this
// object's encoding carries, the same shape networkWireFields has.
func wanWireFields(network *ui.Network) []string {
	spec := wanKitSpec()
	declared := map[string]bool{}
	for _, name := range spec.WireNames() {
		declared[name] = true
	}
	for _, name := range spec.AlwaysWire {
		declared[name] = true
	}
	out := make([]string, 0, len(declared))
	for name := range declared {
		out = append(out, name)
	}
	sort.Strings(out)
	return networkMaskFor(out, network)
}

// unifi_wan has TWO masked call sites -- Backend.UpdateFields (every apply)
// and adoptExistingWAN (once, on a create conflict) -- so this counts call
// sites rather than checking that a masked call exists somewhere, which a
// fix to only one site would still pass.
func TestWANUsesTheMaskedCallAtEverySite(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "unifi", "wan_descriptor.go"))
	if err != nil {
		t.Fatalf("reading the descriptor: %v", err)
	}
	src := string(raw)

	if n := strings.Count(src, "UpdateNetworkFields("); n != 2 {
		t.Errorf("found %d masked calls, want 2 -- Backend.UpdateFields and adoptExistingWAN", n)
	}
	if regexp.MustCompile(`UpdateNetwork\(ctx`).MatchString(src) {
		t.Error("a whole-object UpdateNetwork( call remains in wan_descriptor.go")
	}
	if !strings.Contains(src, "func adoptExistingWAN(") {
		t.Fatal("the file read is not wan_descriptor.go; the assertions above prove nothing")
	}
}

// Every wire the descriptor can put on a mask must be one the WAN purpose's
// encoder can emit: go-unifi refuses a mask naming a field the purpose
// drops, so a wire failing this check fails every apply that plans it. A
// ReadOnly field never reaches a mask, which is exactly why
// single_network_lan and mac_override_enabled are ReadOnly -- the WAN
// encoder drops both -- so those are excluded rather than special-cased.
func TestWANDeclaredWiresAreAllWANEncodable(t *testing.T) {
	spec := wanKitSpec()
	readOnly := map[string]bool{}
	for _, field := range spec.Fields {
		if _, ok := field.(interface {
			Unwrap() resourcekit.Field[wanKitModel, ui.Network]
		}); ok {
			readOnly[field.WireName()] = true
		}
	}
	names := append(spec.WireNames(), spec.AlwaysWire...)
	if len(names) == 0 {
		t.Fatal("the descriptor declares no wires, so this would check nothing")
	}
	if len(readOnly) != 2 {
		t.Errorf("found %d ReadOnly wires, want 2 (single_network_lan, mac_override_enabled); "+
			"a new one is a new claim that the WAN encoder drops it", len(readOnly))
	}
	for _, wire := range names {
		if readOnly[wire] {
			continue
		}
		if !ui.NetworkEncodesField(ui.PurposeWAN, wire) {
			t.Errorf("the descriptor declares %q, which a WAN's encoder never emits; "+
				"a mask naming it is refused outright", wire)
		}
	}
}

// The seven fields unifi_wan was blanking, two of them credentials.
func TestWANMaskExcludesTheFieldsItDoesNotManage(t *testing.T) {
	unmanaged := []string{
		"x_wan_password", "wan_username",
		"wan_pppoe_password_enabled", "wan_pppoe_username_enabled",
		"interface_mtu_enabled", "wan_ipv6", "wan_gateway_v6",
	}
	name := "probe"
	network := &ui.Network{Purpose: ui.PurposeWAN, Name: &name, Enabled: true}
	mask := wanWireFields(network)
	if len(mask) == 0 {
		t.Fatal("the mask is empty, so the assertions below would pass vacuously")
	}

	raw, err := json.Marshal(network)
	if err != nil {
		t.Fatalf("encoding a WAN: %v", err)
	}
	var encoded map[string]json.RawMessage
	if err := json.Unmarshal(raw, &encoded); err != nil {
		t.Fatalf("reading back: %v", err)
	}

	for _, field := range unmanaged {
		if slices.Contains(mask, field) {
			t.Errorf("%s is in the mask; the resource does not assign it and the "+
				"whole-object write was blanking it", field)
		}
		// The control: the encoder DOES send it for this purpose, so excluding
		// it from the mask is what stops the write.
		if _, carried := encoded[field]; !carried {
			t.Errorf("marshalWAN does not emit %s at all, so excluding it proves nothing", field)
		}
	}
	if !slices.Contains(mask, "name") {
		t.Error("name is missing from the mask, so a rename would not be written")
	}
}

// A mask may name only what the purpose encodes, or go-unifi refuses the write.
func TestWANMaskNamesOnlyWhatThePurposeEncodes(t *testing.T) {
	name := "probe"
	network := &ui.Network{Purpose: ui.PurposeWAN, Name: &name, Enabled: true}
	raw, err := json.Marshal(network)
	if err != nil {
		t.Fatalf("encoding: %v", err)
	}
	var encoded map[string]json.RawMessage
	if err := json.Unmarshal(raw, &encoded); err != nil {
		t.Fatalf("reading back: %v", err)
	}
	mask := wanWireFields(network)
	if len(mask) == 0 {
		t.Fatal("the mask is empty")
	}
	for _, field := range mask {
		if _, carried := encoded[field]; !carried {
			t.Errorf("the mask names %q, which a WAN does not encode", field)
		}
	}
}

// Several purpose encoders strip omitempty, so unmanaged list fields emit []
// unconditionally -- and for a list, [] means "clear this", not silence.
// maskedBody marshals first and copies only the named keys out, so a key the
// mask doesn't name never reaches the body; that ordering is why masking
// stops it. A managed list stays in the mask, since a practitioner clearing
// it really should send [].
func TestUnconditionalEmptySlicesAreMaskedOnlyWhereManaged(t *testing.T) {
	name := "probe"
	for _, testCase := range []struct {
		purpose  string
		mask     func(*ui.Network) []string
		managed  []string // emitted as [] AND assigned by the resource: stay
		excluded []string // emitted as [] and NOT assigned: must not be masked
	}{
		{
			purpose: ui.PurposeCorporate, mask: networkWireFields,
			managed: []string{
				"ip_aliases", "ipv6_aliases", "nat_outbound_ip_addresses", "dhcp_relay_servers",
			},
			excluded: nil,
		},
		{
			purpose: ui.PurposeWAN, mask: wanWireFields,
			managed:  []string{"wan_ip_aliases"},
			excluded: nil,
		},
	} {
		t.Run(testCase.purpose, func(t *testing.T) {
			network := &ui.Network{Purpose: testCase.purpose, Name: &name}
			raw, err := json.Marshal(network)
			if err != nil {
				t.Fatalf("encoding: %v", err)
			}
			var encoded map[string]json.RawMessage
			if err := json.Unmarshal(raw, &encoded); err != nil {
				t.Fatalf("reading back: %v", err)
			}
			mask := testCase.mask(network)

			for _, field := range append(append([]string{}, testCase.managed...), testCase.excluded...) {
				// The control for every case below: the encoder really does
				// emit this key from an object whose slice is nil.
				value, emitted := encoded[field]
				if !emitted {
					t.Errorf("%s is not emitted for %s at all, so this case proves nothing",
						field, testCase.purpose)
					continue
				}
				if string(value) != "[]" {
					t.Errorf("%s is emitted as %s, not []; the premise of this test is wrong",
						field, value)
				}
			}
			for _, field := range testCase.managed {
				if !slices.Contains(mask, field) {
					t.Errorf("%s is assigned by the resource but missing from the mask, so "+
						"clearing it would never be written", field)
				}
			}
			for _, field := range testCase.excluded {
				if slices.Contains(mask, field) {
					t.Errorf("%s is in the mask but the resource never assigns it; the "+
						"encoder's unconditional [] would then reach the controller and "+
						"empty a list the practitioner never mentioned", field)
				}
			}
		})
	}
}

// unifi_wlan is the largest instance of the class and has no purpose
// discriminator, so its mask is the assigned set with no runtime filter.
func TestWLANMaskExcludesTheFieldsItDoesNotManage(t *testing.T) {
	unmanaged := []string{
		"dpi_enabled", "rrm_enabled", "bc_filter_enabled", "auth_cache",
		"p2p", "p2p_cross_connect", "tdls_prohibit", "radius_das_enabled",
		"iot_channel_lock", "sae_psk_vlan_required", "dpigroup_id",
	}
	mask := wlanManagedWireFields()
	if len(mask) == 0 {
		t.Fatal("the mask is empty, so the assertions below would pass vacuously")
	}

	// The control: every one of these IS force-emitted by a zero WLAN, so
	// excluding it from the mask is what stops the write.
	raw, err := json.Marshal(&ui.WLAN{})
	if err != nil {
		t.Fatalf("encoding a zero WLAN: %v", err)
	}
	var encoded map[string]json.RawMessage
	if err := json.Unmarshal(raw, &encoded); err != nil {
		t.Fatalf("reading back: %v", err)
	}

	for _, field := range unmanaged {
		if slices.Contains(mask, field) {
			t.Errorf("%s is in the mask; the resource does not assign it and the "+
				"whole-object write was resetting it", field)
		}
		if _, emitted := encoded[field]; !emitted {
			t.Errorf("a zero WLAN does not emit %s at all, so excluding it proves nothing", field)
		}
	}
	// enabled is force-emitted here as on every network purpose, and the
	// resource does assign it -- so it belongs in the mask.
	if !slices.Contains(mask, "enabled") {
		t.Error("enabled is missing from the mask; the resource assigns it and a " +
			"disable would never be written")
	}
}

// The declared list must match what the resource assigns.
func TestWLANManagedWireFieldsMatchTheResource(t *testing.T) {
	assigned := wlanFieldsAssignedBy(t)
	if len(assigned) == 0 {
		t.Fatal("no assignments found; the parse failed")
	}
	tags := map[string]string{}
	typ := reflect.TypeOf(ui.WLAN{})
	for i := range typ.NumField() {
		field := typ.Field(i)
		tag := field.Tag.Get("json")
		if tag == "" || tag == "-" {
			continue
		}
		name, _, _ := strings.Cut(tag, ",")
		tags[field.Name] = name
	}
	declared := map[string]bool{}
	for _, name := range wlanManagedWireFields() {
		declared[name] = true
	}
	for _, field := range assigned {
		tag, ok := tags[field]
		if !ok || tag == "_id" || tag == "site_id" {
			continue
		}
		if !declared[tag] {
			t.Errorf("the resource assigns %s (WLAN.%s) but it is not in "+
				"wlanManagedWireFields, so it would never be written", tag, field)
		}
	}
}

// wlanFieldsAssignedBy reads assignments across the whole file rather than
// one function, since planToWLAN delegates the way modelToNetwork does and a
// function-scoped regex would miss delegated assignments.
func wlanFieldsAssignedBy(t *testing.T) []string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "unifi", "wlan_resource.go"))
	if err != nil {
		t.Fatalf("reading the resource: %v", err)
	}
	seen := map[string]bool{}
	for _, m := range regexp.MustCompile(`\bwlan\.([A-Z]\w*)\s*=`).FindAllStringSubmatch(string(raw), -1) {
		seen[m[1]] = true
	}
	for _, m := range regexp.MustCompile(`(?m)^\t\t([A-Z]\w*):\s`).FindAllStringSubmatch(string(raw), -1) {
		seen[m[1]] = true
	}
	out := make([]string, 0, len(seen))
	for name := range seen {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// Compares two derived sets rather than grepping source: the kit's mask
// (from the descriptor's Fields/AlwaysWire) against wlanManagedWireFields,
// what the hand-written resource used to send. Equality is the claim that
// migrating the surface didn't quietly add or drop a wire.
func TestWLANUsesTheMaskedCall(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "unifi", "wlan_descriptor.go"))
	if err != nil {
		t.Fatalf("reading the descriptor: %v", err)
	}
	src := string(raw)
	if !strings.Contains(src, "UpdateWLANFields(ctx, site, in, fields...)") {
		t.Error("the kit Backend does not bind UpdateWLANFields")
	}
	if regexp.MustCompile(`UpdateWLAN\(ctx`).MatchString(src) {
		t.Error("a whole-object UpdateWLAN( call remains")
	}

	spec := wlanKitSpec()
	got := map[string]bool{}
	for _, name := range spec.WireNames() {
		got[name] = true
	}
	for _, name := range spec.AlwaysWire {
		got[name] = true
	}
	// The identity is reached through Backend.GetID and is never a masked
	// field; Spec.IDWire exists to say so.
	delete(got, spec.IDWire)

	want := map[string]bool{}
	for _, name := range wlanManagedWireFields() {
		want[name] = true
	}

	// Without this the two maps could both be empty and agree.
	if len(want) == 0 {
		t.Fatal("wlanManagedWireFields is empty, so this comparison would pass having checked nothing")
	}

	for name := range want {
		if !got[name] {
			t.Errorf("the hand-written mask sent %q and the descriptor does not; a practitioner setting it would see the apply silently drop it", name)
		}
	}
	for name := range got {
		if !want[name] {
			t.Errorf("the descriptor sends %q and the hand-written mask did not; the migration widened what this surface writes", name)
		}
	}
}

// networkWireFields answers what the surface can write on ANY plan -- read
// from the descriptor's Fields, Wires and AlwaysWire, the widest case each
// caller wants. The purpose argument is deliberately unused: narrowing by
// purpose is what TestNetworkMaskNamesOnlyWhatThePurposeEncodes checks,
// against the encoder itself rather than against this.
func networkWireFields(network *ui.Network) []string {
	declared, _ := networkDescriptorWiresAndHooks(&testing.T{})
	out := make([]string, 0, len(declared))
	for name := range declared {
		out = append(out, name)
	}
	sort.Strings(out)
	// Narrowed the way the surface narrows it: Spec.NarrowMask runs
	// networkMaskFor before the write, so skipping this would report names
	// the resource never actually sends.
	return networkMaskFor(out, network)
}
