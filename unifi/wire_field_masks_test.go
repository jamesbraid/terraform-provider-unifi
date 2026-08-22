package unifi

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"

	ui "github.com/ubiquiti-community/go-unifi/unifi"
)

// THE SURFACES WHOSE UPDATE IS MASKED, and everything asserted about them.
//
// One table rather than a set of near-identical tests per surface: the checks
// are the same and only the four facts differ, so a new surface is a row. That
// also means a surface cannot be added to the fix without being added here --
// the alternative, copying three tests each time, is how one of them ends up
// quietly not copied.
type maskedSurface struct {
	name     string // the resource file, for the failure message
	file     string // where its mapper lives
	mapper   string // the method building the SDK object
	encoder  string // go-unifi's per-purpose marshaller
	declared func() []string
	// unmanaged names a field the encoder emits for this purpose that the
	// resource does NOT manage -- the thing the whole-object write was
	// resetting. Empty where none was measured.
	unmanaged string
	// managed names a field the resource does manage, as the positive control.
	managed string
	// conditionallyAssigned names fields the mapper assigns ONLY under a
	// condition, and which must therefore stay OUT of the mask.
	//
	// THE DISTINCTION THE PARSER CANNOT SEE. It reports a field as assigned
	// whether the assignment is unconditional or guarded, and for a guarded one
	// the two are opposite: go-unifi sends a masked field's ZERO when the
	// object does not carry a value, so naming a field the mapper leaves unset
	// clears whatever the controller holds. vpnServerDNSServersToNetwork only
	// assigns dhcpd_dns_1 and _2 when the practitioner supplied servers, so
	// masking them would blank the controller's DNS on every apply that omits
	// the block -- the destruction this whole area exists to stop, arriving
	// through the fix for the opposite defect.
	//
	// Declared per field rather than skipped, so the exclusion carries its
	// reason and a new one has to be argued for.
	conditionallyAssigned []string
}

// EVERY SURFACE HERE IS HAND-WRITTEN, and a row outlives its subject the same
// way a blocker record does. vpn_server's row was still present after its
// cutover, so the parser looked for modelToNetwork in a file that no longer has
// one -- and the test said so rather than passing on an empty list, which is
// the guard doing its job.
// EMPTY BECAUSE EVERY HAND-WRITTEN MASKED SURFACE HAS BEEN MIGRATED, and that
// is a state this table has to be able to express out loud.
//
// It carried vpn_client and vpn_server this morning. Both are now served from
// the kit, which derives its mask from Fields plus AlwaysWire and checks it with
// ScatteredObjectField.ConditionalWires and resourcekit.ConditionalWireProblems.
// So the four tests below have nothing left to walk.
//
// AN EMPTY POPULATION IS HOW A CHECK STOPS CHECKING WITHOUT FAILING. Ranging
// over nothing passes, four times, in a file whose own comment says a hand-kept
// enumeration that nothing compares against its subject goes stale in silence.
// Each caller therefore skips with the reason rather than passing quietly, and a
// new hand-written masked surface puts its row back here and turns them on
// again.
func maskedSurfaces() []maskedSurface {
	return []maskedSurface{}
}

// skipIfNoMaskedSurfaces reports that the run measured nothing, rather than
// letting a range over an empty slice read as a pass.
func skipIfNoMaskedSurfaces(t *testing.T) {
	t.Helper()
	if len(maskedSurfaces()) == 0 {
		t.Skip("no hand-written masked surfaces remain; every one has been migrated " +
			"to the kit, whose mask is derived and checked by ConditionalWireProblems")
	}
}

// A DECLARED LIST IS ONLY SAFE IF SOMETHING CHECKS IT. Each mask is
// hand-written, and a hand-kept enumeration that nothing compares against the
// thing it enumerates goes stale in silence -- the disease this whole area has.
//
// Both failure directions matter and they fail differently. A field added to
// the mapper but not the list stops being written: the practitioner sets an
// attribute and nothing happens. A field in the list but not the mapper names
// something the object never carries, which go-unifi's maskedBody refuses.
func TestWireFieldMasksMatchTheirMappers(t *testing.T) {
	tags := networkJSONTags(t)
	skipIfNoMaskedSurfaces(t)
	for _, surface := range maskedSurfaces() {
		t.Run(surface.name, func(t *testing.T) {
			assigned := networkFieldsAssignedBy(t, surface.file, surface.mapper)
			if len(assigned) == 0 {
				t.Fatal("no assignments found; the parse failed and this would pass for an empty list")
			}
			emitted := purposeEncoderEmits(t, surface.encoder)
			if len(emitted) == 0 {
				t.Fatal("the encoder emitted nothing; the parse failed")
			}

			var want []string
			for _, field := range assigned {
				tag, ok := tags[field]
				if !ok {
					t.Errorf("%s assigns Network.%s, which carries no json tag", surface.mapper, field)
					continue
				}
				if emitted[tag] && !slices.Contains(surface.conditionallyAssigned, tag) {
					want = append(want, tag)
				}
			}
			sort.Strings(want)
			got := append([]string(nil), surface.declared()...)
			sort.Strings(got)
			if !reflect.DeepEqual(got, want) {
				t.Errorf("the declared mask disagrees with the mapper.\n  declared: %v\n  derived:  %v",
					got, want)
			}
		})
	}
}

// THE DEFECT ITSELF, asserted rather than described.
func TestWireFieldMasksExcludeWhatTheResourceDoesNotManage(t *testing.T) {
	skipIfNoMaskedSurfaces(t)
	for _, surface := range maskedSurfaces() {
		t.Run(surface.name, func(t *testing.T) {
			mask := surface.declared()
			emitted := purposeEncoderEmits(t, surface.encoder)

			if surface.unmanaged != "" {
				if slices.Contains(mask, surface.unmanaged) {
					t.Errorf("%s is in the mask; the whole-object write was resetting it on "+
						"every apply and this resource does not manage it", surface.unmanaged)
				}
				// The control: the encoder DOES emit it, so excluding it from
				// the mask is what prevents the write rather than the encoder
				// doing it for us.
				if !emitted[surface.unmanaged] {
					t.Fatalf("%s does not emit %s at all, so excluding it proves nothing",
						surface.encoder, surface.unmanaged)
				}
			}
			if !slices.Contains(mask, surface.managed) {
				t.Errorf("%s is missing from the mask, so a practitioner setting it would "+
					"see nothing happen", surface.managed)
			}
		})
	}
}

// EVERY MASKED NAME MUST BE ONE THE ENCODER EMITS, or go-unifi refuses the
// write. Inert on both surfaces here and asserted anyway, because network
// reuses this shape and there the filter is load-bearing.
func TestWireFieldMasksNameOnlyEmittedFields(t *testing.T) {
	skipIfNoMaskedSurfaces(t)
	for _, surface := range maskedSurfaces() {
		t.Run(surface.name, func(t *testing.T) {
			emitted := purposeEncoderEmits(t, surface.encoder)
			for _, name := range surface.declared() {
				if !emitted[name] {
					t.Errorf("the mask names %q, which %s does not emit; go-unifi refuses "+
						"a mask naming a dropped field", name, surface.encoder)
				}
			}
		})
	}
}

// THE MASK MUST ACTUALLY BE USED, and nothing else checks that.
//
// Reverting a resource to UpdateNetwork leaves every test above passing: the
// declared list still matches its mapper, still names only emitted fields, and
// still excludes the unmanaged field -- it is simply no longer consulted, and
// Go does not complain about an unused package-level function. The whole fix
// reverts in one line with nothing failing.
func TestMaskedSurfacesUseTheMaskedCall(t *testing.T) {
	wholeObject := regexp.MustCompile(`UpdateNetwork\(ctx`)
	skipIfNoMaskedSurfaces(t)
	for _, surface := range maskedSurfaces() {
		t.Run(surface.name, func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join("..", surface.file))
			if err != nil {
				t.Fatalf("reading the resource: %v", err)
			}
			src := string(raw)
			// MATCHED ACROSS LINES, because the argument list now wraps: the
			// mask is narrowed by networkMaskFor before it is passed, and gofmt
			// splits the call. A literal one-line substring made this assertion
			// fail for a formatting change while the property it guards held.
			if !maskedUpdateCall.MatchString(src) {
				t.Error("the update does not call UpdateNetworkFields; a whole-object write " +
					"resets every field this resource does not manage")
			}
			if wholeObject.MatchString(src) {
				t.Error("a whole-object UpdateNetwork( call remains")
			}
			// The control: this really is the file under test.
			if !strings.Contains(src, surface.mapper+"(") {
				t.Fatalf("the file read does not contain %s; the path is wrong and the "+
					"assertions above prove nothing", surface.mapper)
			}
		})
	}
}

// --- derivations from source, shared by the surfaces that follow ---

// networkFieldsAssignedBy returns the Network fields a named method assigns.
func networkFieldsAssignedBy(t *testing.T, path, method string) []string {
	t.Helper()
	src, err := os.ReadFile(filepath.Join("..", path))
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, src, 0)
	if err != nil {
		t.Fatalf("parsing %s: %v", path, err)
	}
	// FUNCTIONS THE MAPPER CALLS COUNT AS THE MAPPER, and leaving them out is
	// how vpn_server lost local_port. The port a practitioner sets as
	// wireguard.port reaches network.LocalPort through
	// vpnServerLocalPortToNetwork -- a helper, not a line in modelToNetwork --
	// so this parser never saw the assignment, the derived set never demanded
	// the wire name, and the mask omitting it agreed with a check that could
	// not look. The port change was accepted at plan and never written.
	//
	// ONE LEVEL, and only within this file. That is enough for every helper the
	// mappers actually use, and it keeps the walk bounded: a transitive crawl
	// would follow into go-unifi and start reporting fields no mapper touches.
	// If a mapper ever assigns through two hops this reports too few again, and
	// the honest place to find that out is a controller, which is what found
	// this one.
	bodies := map[string]*ast.FuncDecl{}
	for _, decl := range file.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok {
			bodies[fn.Name.Name] = fn
		}
	}
	seen := map[string]bool{}
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name.Name != method {
			continue
		}
		targets := []*ast.FuncDecl{fn}
		ast.Inspect(fn, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			name, ok := call.Fun.(*ast.Ident)
			if !ok {
				return true
			}
			if helper, ok := bodies[name.Name]; ok && helper != fn {
				targets = append(targets, helper)
			}
			return true
		})
		for _, target := range targets {
			ast.Inspect(target, func(n ast.Node) bool {
				switch node := n.(type) {
				case *ast.AssignStmt:
					for _, lhs := range node.Lhs {
						if sel, ok := lhs.(*ast.SelectorExpr); ok {
							seen[sel.Sel.Name] = true
						}
					}
				case *ast.KeyValueExpr:
					if key, ok := node.Key.(*ast.Ident); ok {
						seen[key.Name] = true
					}
				}
				return true
			})
		}
	}
	out := make([]string, 0, len(seen))
	for name := range seen {
		if name != "" && strings.ToUpper(name[:1]) == name[:1] {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

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

var encoderTagPattern = regexp.MustCompile("`json:\"([^\"]+)\"`")

// purposeEncoderEmits reports the json names one purpose encoder sends, read
// from go-unifi's source because the alias structs are local to their methods.
func purposeEncoderEmits(t *testing.T, method string) map[string]bool {
	t.Helper()
	src := readGoUnifiEncoder(t)
	start := strings.Index(src, "func (n *Network) "+method+"()")
	if start < 0 {
		t.Fatalf("%s not found in network_encode.go", method)
	}
	end := strings.Index(src[start:], "\n}\n")
	if end < 0 {
		end = len(src) - start
	}
	out := map[string]bool{}
	for _, m := range encoderTagPattern.FindAllStringSubmatch(src[start:start+end], -1) {
		name, _, _ := strings.Cut(m[1], ",")
		out[name] = true
	}
	return out
}

func readGoUnifiEncoder(t *testing.T) string {
	t.Helper()
	// Located through the build cache rather than a hardcoded version, so a pin
	// bump does not silently point this at the old SDK.
	raw, err := os.ReadFile(goUnifiSourcePath(t, "network_encode.go"))
	if err != nil {
		t.Fatalf("reading go-unifi's network_encode.go: %v", err)
	}
	return string(raw)
}

func goUnifiSourcePath(t *testing.T, name string) string {
	t.Helper()
	// Tests run with the package directory as the working directory.
	out, err := os.ReadFile(filepath.Join("..", "go.mod"))
	if err != nil {
		t.Fatalf("reading go.mod: %v", err)
	}
	m := regexp.MustCompile(`github\.com/ubiquiti-community/go-unifi (v\S+)`).FindSubmatch(out)
	if m == nil {
		t.Fatal("go.mod does not pin go-unifi")
	}
	cache := os.Getenv("GOMODCACHE")
	if cache == "" {
		home, _ := os.UserHomeDir()
		cache = filepath.Join(home, "go", "pkg", "mod")
	}
	return filepath.Join(cache, "github.com/ubiquiti-community/go-unifi@"+string(m[1]), "unifi", name)
}

// unifi_network gets its own tests rather than a row in the table above,
// because it writes THREE purposes and the table's single-encoder shape cannot
// express that. Forcing it in would mean weakening the table's assertions for
// the two surfaces where they are exact.

// THE MASK MUST BE PURPOSE-CORRECT, and this is the surface where getting it
// wrong is a failed apply rather than a silent no-op: maskedBody refuses a mask
// naming a field the encoder drops, and vlan-only encodes 15 fields against
// corporate's 40.
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

// THE NINE UNMANAGED FIELDS MUST NEVER BE MASKED, on any purpose. They are what
// the whole-object write was resetting, and the resource assigns none of them.
func TestNetworkMaskExcludesTheFieldsItDoesNotManage(t *testing.T) {
	unmanaged := []string{
		"dhcpd_mac_1", "dhcpd_mac_2", "dhcpd_mac_3",
		// THE THIRD NAME BELOW IS SPELLED WITH ONE P ON PURPOSE. That is the
		// controller's own spelling, carried through go-unifi's json tag, while
		// the Go field is IGMPSuppression with two -- so the struct and its own
		// tag disagree about the same word, and a grep keyed off either misses
		// the other. Correcting it here would name a field that does not exist.
		"igmp_fastleave", "igmp_flood_unknown_multicast", "igmp_supression", //nolint:misspell // the controller's spelling
		"ipv6_aliases", "mac_override_enabled", "upnp_lan_enabled",
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

// TestNetworkManagedWireFieldsMatchTheResource IS GONE, replaced rather than
// dropped. It compared what modelToNetwork assigned against a hand-maintained
// networkManagedWireFields list. unifi_network is served from the kit now: the
// mask is DERIVED from the descriptor's Fields, so a flat field's wire name is
// its declaration and the two cannot disagree. What can still disagree is a
// hand-written hook -- a ScatteredObjectField Encode against its own Wires, and
// BeforeSend against everything -- and those are checked in
// scattered_encode_wires_test.go.

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

// unifi_wan has TWO call sites and both were whole-object. Update runs on every
// apply that touches a WAN; adoptExistingWAN runs once, when a create finds the
// interface already there. The frequent one is the one that went unnamed in the
// first census, which is why this test counts call sites rather than checking
// that a masked call exists somewhere.
func TestWANUsesTheMaskedCallAtEverySite(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "unifi", "wan_resource.go"))
	if err != nil {
		t.Fatalf("reading the resource: %v", err)
	}
	src := string(raw)

	if n := strings.Count(src, "UpdateNetworkFields("); n != 2 {
		t.Errorf("found %d masked calls, want 2 -- Update and adoptExistingWAN", n)
	}
	if regexp.MustCompile(`UpdateNetwork\(ctx`).MatchString(src) {
		t.Error("a whole-object UpdateNetwork( call remains in wan_resource.go")
	}
	if !strings.Contains(src, "func (r *wanResource) adoptExistingWAN(") {
		t.Fatal("the file read is not wan_resource.go; the assertions above prove nothing")
	}
}

// THE SEVEN FIELDS unifi_wan was blanking, two of them credentials.
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

// The declared list must match what the resource assigns, same as network's.
func TestWANManagedWireFieldsMatchTheResource(t *testing.T) {
	assigned := networkFieldsAssignedBy(t, "unifi/wan_resource.go", "modelToNetwork")
	if len(assigned) == 0 {
		t.Fatal("no assignments found; the parse failed")
	}
	tags := networkJSONTags(t)
	declared := map[string]bool{}
	for _, name := range wanManagedWireFields() {
		declared[name] = true
	}
	for _, field := range assigned {
		tag, ok := tags[field]
		if !ok || tag == "_id" || tag == "site_id" {
			continue
		}
		if !declared[tag] {
			t.Errorf("modelToNetwork assigns %s (Network.%s) but it is not in "+
				"wanManagedWireFields, so it would never be written", tag, field)
		}
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

// THE UNCONDITIONAL EMPTY SLICES, and why the mask is the right place to stop
// them.
//
// Several purpose encoders strip the omitempty the base Network struct carries,
// so those keys are emitted as [] whatever the object holds -- and for a list,
// [] is not silence, it is "make this empty". go-unifi does that deliberately;
// network_encode_test.go asserts the empty array for ip_aliases.
//
// The mask stops it because maskedBody marshals FIRST and then copies only the
// named keys out of the encoding: a key the encoder produced but the mask does
// not name never reaches the body. That ordering is the whole reason a mask can
// fix this, so it is asserted here rather than assumed.
//
// A managed list stays in the mask, and that is correct: if a practitioner
// clears ip_aliases, [] is exactly what should be sent.
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
			managed:  []string{"ip_aliases", "nat_outbound_ip_addresses", "dhcp_relay_servers"},
			excluded: []string{"ipv6_aliases"},
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

// wlanFieldsAssignedBy reads assignments onto a *unifi.WLAN across the whole
// file, because planToWLAN delegates the way modelToNetwork does -- which is
// how a regex scoped to one function missed wan_networkgroup twice.
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

// unifi_wlan must use the masked call, and the mask must be the one the
// hand-written resource used.
//
// THIS USED TO GREP wlan_resource.go FOR A LITERAL CALL STRING. That checked a
// declaration rather than a state, and the cutover to the kit made the string
// vanish while the behaviour stayed correct -- the test would have failed on a
// change that broke nothing, which is the same fault as passing on a change
// that breaks something.
//
// It now compares two derived sets. The kit builds the update mask from the
// descriptor's Fields plus AlwaysWire; wlanManagedWireFields is what the
// hand-written resource sent. Equality is the real claim -- that migrating the
// surface did not quietly add or drop a wire -- and it is checked against the
// spec that runs rather than against source text.
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

// maskedUpdateCall matches the masked update however its arguments are laid
// out. The call takes a narrowed mask now -- networkMaskFor(...) around the
// surface's field list -- so gofmt wraps it, and a literal substring would
// report a whole-object write on a surface that does not do one.
var maskedUpdateCall = regexp.MustCompile(`(?s)UpdateNetworkFields\(\s*ctx,\s*site,\s*network,`)

// networkWireFields is now derived from the descriptor rather than maintained
// beside it, and it lives here because only tests ask the question.
//
// The production list it replaces was hand-written: a name added to one and not
// the other was a field the provider either could not write or claimed it
// could. unifi_network is served from the kit, so the mask comes from Fields,
// Wires and AlwaysWire -- this reads those three and answers what the surface
// can write on ANY plan, which is the worst case each caller wants.
//
// The purpose argument is ignored. The old function narrowed the mask by what a
// given purpose encodes; the checks that mattered are about the widest set, and
// TestNetworkMaskNamesOnlyWhatThePurposeEncodes is the one that pins the
// narrowing -- against the encoder itself rather than against another list.
func networkWireFields(network *ui.Network) []string {
	declared, _ := networkDescriptorWiresAndHooks(&testing.T{})
	out := make([]string, 0, len(declared))
	for name := range declared {
		out = append(out, name)
	}
	sort.Strings(out)
	// NARROWED THE WAY THE SURFACE NARROWS IT. Spec.NarrowMask runs
	// networkMaskFor before the write, so a check that skipped it would report
	// names the resource never sends.
	return networkMaskFor(out, network)
}
