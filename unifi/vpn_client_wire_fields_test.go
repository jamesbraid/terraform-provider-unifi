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

// A DECLARED LIST IS ONLY SAFE IF SOMETHING CHECKS IT. vpnClientWireFields is
// hand-written, and a hand-kept enumeration that nothing compares against the
// thing it enumerates goes stale in silence -- which is the disease this whole
// area has. So the same set is derived here from the source and the two must
// agree.
//
// Both failure directions matter and they fail differently. A field added to
// the mapper but not to the list stops being written: the practitioner sets an
// attribute and nothing happens. A field in the list but not the mapper names
// something the object never carries, which go-unifi's maskedBody refuses
// outright.
func TestVPNClientWireFieldsMatchTheMapper(t *testing.T) {
	assigned := networkFieldsAssignedBy(t, "unifi/vpn_client_resource.go", "modelToNetwork")
	if len(assigned) == 0 {
		t.Fatal("no assignments found in modelToNetwork; the parse failed and this test " +
			"would pass for an empty list")
	}

	tags := networkJSONTags(t)
	emitted := purposeEncoderEmits(t, "marshalVPNClient")
	if len(emitted) == 0 {
		t.Fatal("the vpn-client encoder emitted nothing; the parse failed")
	}

	var want []string
	for _, field := range assigned {
		tag, ok := tags[field]
		if !ok {
			t.Errorf("modelToNetwork assigns Network.%s, which carries no json tag", field)
			continue
		}
		if emitted[tag] {
			want = append(want, tag)
		}
	}
	sort.Strings(want)

	got := append([]string(nil), vpnClientWireFields()...)
	sort.Strings(got)

	if !reflect.DeepEqual(got, want) {
		t.Errorf("vpnClientWireFields disagrees with the mapper.\n  declared: %v\n  derived:  %v",
			got, want)
	}
}

// THE DEFECT ITSELF, asserted rather than described: the field that was being
// reset must not be in the mask, and a field the resource does manage must be.
func TestVPNClientMaskExcludesWhatTheResourceDoesNotManage(t *testing.T) {
	mask := vpnClientWireFields()

	if slices.Contains(mask, "dhcpd_dns_enabled") {
		t.Error("dhcpd_dns_enabled is in the mask; it is the field the whole-object write " +
			"was resetting on every apply and this resource does not manage it")
	}
	if !slices.Contains(mask, "wireguard_client_peer_ip") {
		t.Error("wireguard_client_peer_ip is missing from the mask, so a practitioner setting " +
			"it would see nothing happen")
	}

	// The control that gives the first assertion meaning: the encoder DOES emit
	// dhcpd_dns_enabled for this purpose, so excluding it from the mask is the
	// thing preventing the write rather than the encoder doing it for us.
	if !purposeEncoderEmits(t, "marshalVPNClient")["dhcpd_dns_enabled"] {
		t.Fatal("the vpn-client encoder does not emit dhcpd_dns_enabled at all, so excluding " +
			"it from the mask proves nothing")
	}
}

// EVERY MASKED NAME MUST BE ONE THE ENCODER EMITS, or go-unifi refuses the
// write. Inert for this surface and asserted anyway, because the next three
// surfaces reuse this shape and there it is not inert.
func TestVPNClientMaskNamesOnlyEmittedFields(t *testing.T) {
	emitted := purposeEncoderEmits(t, "marshalVPNClient")
	for _, name := range vpnClientWireFields() {
		if !emitted[name] {
			t.Errorf("the mask names %q, which marshalVPNClient does not emit; go-unifi "+
				"refuses a mask naming a dropped field", name)
		}
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
