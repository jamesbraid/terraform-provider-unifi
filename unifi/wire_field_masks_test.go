package unifi

import (
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
}

func maskedSurfaces() []maskedSurface {
	return []maskedSurface{
		{
			name: "vpn_client", file: "unifi/vpn_client_resource.go",
			mapper: "modelToNetwork", encoder: "marshalVPNClient",
			declared:  vpnClientWireFields,
			unmanaged: "dhcpd_dns_enabled",
			managed:   "wireguard_client_peer_ip",
		},
		{
			name: "vpn_server", file: "unifi/vpn_server_resource.go",
			mapper: "modelToNetwork", encoder: "marshalUserVPN",
			declared:  vpnServerWireFields,
			unmanaged: "require_mschapv2",
			managed:   "openvpn_mode",
		},
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
				if emitted[tag] {
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
	for _, surface := range maskedSurfaces() {
		t.Run(surface.name, func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join("..", surface.file))
			if err != nil {
				t.Fatalf("reading the resource: %v", err)
			}
			src := string(raw)
			if !strings.Contains(src, "UpdateNetworkFields(ctx, site, network,") {
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
	seen := map[string]bool{}
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name.Name != method {
			continue
		}
		ast.Inspect(fn, func(n ast.Node) bool {
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

// THE MASK MUST ACTUALLY BE USED, and nothing else checks that.
//
// Reverting the resource to UpdateNetwork leaves every test above passing: the
// declared list still matches the mapper, still names only emitted fields, and
// still excludes dhcpd_dns_enabled -- it is simply no longer consulted. Go does
// not complain about an unused package-level function, so the whole fix can be
// undone in one line with nothing failing.
//
// This is the same shape as the ValidateConfig interface assertion: a wiring
// that can be silently removed needs something asserting the wiring, not just
// the thing being wired.
func TestVPNClientUpdateUsesTheMaskedCall(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "unifi", "vpn_client_resource.go"))
	if err != nil {
		t.Fatalf("reading the resource: %v", err)
	}
	src := string(raw)

	if !strings.Contains(src, "UpdateNetworkFields(ctx, site, network, vpnClientWireFields()...)") {
		t.Error("the update does not call UpdateNetworkFields with vpnClientWireFields; " +
			"a whole-object write resets every field this resource does not manage")
	}
	// The whole-object call must be gone entirely, not merely joined.
	if regexp.MustCompile(`UpdateNetwork\(ctx`).MatchString(src) {
		t.Error("a whole-object UpdateNetwork( call remains in vpn_client_resource.go")
	}
	// The control: this file really is the one under test, or both assertions
	// above would pass for an empty read.
	if !strings.Contains(src, "func (r *vpnClientResource) modelToNetwork(") {
		t.Fatal("the file read does not contain vpnClientResource.modelToNetwork; " +
			"the path is wrong and the assertions above prove nothing")
	}
}
