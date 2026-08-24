package unifi

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/resourcekit"
)

// fullProbeObject populates every member of an object type, through the type
// itself so a string-valuable custom type gets a value that fits.
func fullProbeObject(t *testing.T, attrTypes map[string]attr.Type) types.Object {
	t.Helper()
	values := make(map[string]attr.Value, len(attrTypes))
	for name, attrType := range attrTypes {
		values[name] = populatedAttr(t, attrType)
	}
	object, diags := types.ObjectValue(attrTypes, values)
	if diags.HasError() {
		t.Fatalf("building the full probe object: %v", diags)
	}
	return object
}

// reportBenignAlwaysAssigned fails on every AlwaysAssigned problem except one
// naming a wire in benign, which it logs instead and marks seen. Whether a
// pinned wire disappeared is left for the caller to check once every field
// has been walked.
func reportBenignAlwaysAssigned(
	t *testing.T,
	surface string,
	problems []string,
	benign map[string]string,
	seen map[string]bool,
) {
	t.Helper()
	for _, problem := range problems {
		matched := false
		for name, reason := range benign {
			if strings.Contains(problem, fmt.Sprintf("%q is ASSIGNED", name)) {
				seen[name] = true
				matched = true
				t.Logf("%s: %s -- pinned benign: %s", surface, problem, reason)
				break
			}
		}
		if !matched {
			t.Errorf("%s: %s", surface, problem)
		}
	}
}

// descriptorsDeclaringScatteredObjectField globs every *_descriptor.go file
// and returns the base names of the ones declaring at least one
// ScatteredObjectField literal, so a walk built from a hand-picked list of
// surfaces has something to check itself against.
func descriptorsDeclaringScatteredObjectField(t *testing.T) []string {
	t.Helper()
	descriptors, err := filepath.Glob(filepath.Join("..", "unifi", "*_descriptor.go"))
	if err != nil {
		t.Fatal(err)
	}
	if len(descriptors) == 0 {
		t.Fatal("no descriptor files found; the glob is wrong and this asserts nothing")
	}
	var have []string
	for _, path := range descriptors {
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("reading %s: %v", path, err)
		}
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, path, src, 0)
		if err != nil {
			t.Fatalf("parsing %s: %v", path, err)
		}
		found := false
		ast.Inspect(file, func(n ast.Node) bool {
			if lit, ok := n.(*ast.CompositeLit); ok && isScatteredObjectField(lit) {
				found = true
			}
			return true
		})
		if found {
			have = append(have, filepath.Base(path))
		}
	}
	sort.Strings(have)
	return have
}

// TestNoSurfaceMasksAZeroForAPartlyFilledBlock walks the surfaces whose
// descriptors carry a scattered object. A partly filled block (e.g.
// wan { port = "8080" }) is set, so every wire it spans joins the mask --
// including ones whose members are null and whose zero then overwrites
// whatever the controller holds.
//
// The unmeasured ones are named, not skipped: a surface whose members can't
// take a generated value reports here rather than joining a silent list, and
// is pinned below so a new one fails instead of going unnoticed.
//
// The walk below is a hand-picked list of six surfaces, checked against the
// descriptor files themselves before walking anything, so a seventh
// ScatteredObjectField fails here rather than shipping unmeasured.
func TestNoSurfaceMasksAZeroForAPartlyFilledBlock(t *testing.T) {
	wantScattered := []string{
		"network_descriptor.go",
		"port_forward_descriptor.go",
		"traffic_route_descriptor.go",
		"vpn_client_descriptor.go",
		"vpn_server_descriptor.go",
		"wlan_descriptor.go",
	}
	haveScattered := descriptorsDeclaringScatteredObjectField(t)
	mismatch := len(haveScattered) != len(wantScattered)
	if !mismatch {
		for i, name := range haveScattered {
			if name != wantScattered[i] {
				mismatch = true
				break
			}
		}
	}
	if mismatch {
		t.Fatalf("descriptor files declaring a ScatteredObjectField = %v, want %v; the walk "+
			"below is a hand-picked list built to match this set exactly, and a mismatch "+
			"means a surface is either walked here without still declaring one, or declares "+
			"one without being walked", haveScattered, wantScattered)
	}

	var unmeasured []string
	note := func(surface string, err error) {
		unmeasured = append(unmeasured, surface)
		t.Logf("%s: not measured -- %v", surface, err)
	}
	// network's always-assigned wires are pinned in
	// TestNetworkNarrowingStaysSafeWhileWiresAreUnclassified, not here, so
	// failing on them in both places wouldn't catch either going stale.
	// Every other surface must have none.
	alwaysAssigned := map[string]int{}

	for _, field := range scatteredFieldsOf(t, networkKitSpec()) {
		report, err := resourcekit.MaskedZeroProblems(t.Context(), field,
			fullProbeObject(t, field.AttrTypes), func(n *ui.Network) {
				n.Purpose = ui.PurposeCorporate
				subnet := "10.0.0.0/24"
				n.IPSubnet = &subnet
			})
		if err != nil {
			note("network/"+field.Wires[0], err)
			continue
		}
		for _, problem := range report.Guarded {
			t.Errorf("network: %s", problem)
		}
		alwaysAssigned["network"] += len(report.AlwaysAssigned)
	}

	for _, field := range portForwardKitSpec().Fields {
		scattered, ok := field.(resourcekit.ScatteredObjectField[portForwardKitModel, ui.PortForward])
		if !ok {
			continue
		}
		report, err := resourcekit.MaskedZeroProblems(t.Context(), scattered,
			fullProbeObject(t, scattered.AttrTypes), nil)
		if err != nil {
			note("port_forward/"+scattered.Wires[0], err)
			continue
		}
		for _, problem := range report.Guarded {
			t.Errorf("port_forward: %s", problem)
		}
		for _, problem := range report.AlwaysAssigned {
			t.Errorf("port_forward: %s", problem)
		}
	}

	for _, field := range vpnClientKitSpec().Fields {
		scattered, ok := field.(resourcekit.ScatteredObjectField[vpnClientResourceModel, ui.Network])
		if !ok {
			continue
		}
		report, err := resourcekit.MaskedZeroProblems(t.Context(), scattered,
			fullProbeObject(t, scattered.AttrTypes), func(n *ui.Network) {
				n.Purpose = ui.PurposeVPNClient
			})
		if err != nil {
			note("vpn_client/"+scattered.Wires[0], err)
			continue
		}
		for _, problem := range report.Guarded {
			t.Errorf("vpn_client: %s", problem)
		}
		for _, problem := range report.AlwaysAssigned {
			t.Errorf("vpn_client: %s", problem)
		}
	}

	for _, field := range trafficRouteKitSpec().Fields {
		scattered, ok := field.(resourcekit.ScatteredObjectField[trafficRouteKitModel, ui.TrafficRoute])
		if !ok {
			continue
		}
		report, err := resourcekit.MaskedZeroProblems(t.Context(), scattered,
			fullProbeObject(t, scattered.AttrTypes), nil)
		if err != nil {
			note("traffic_route/"+scattered.Wires[0], err)
			continue
		}
		for _, problem := range report.Guarded {
			t.Errorf("traffic_route: %s", problem)
		}
		for _, problem := range report.AlwaysAssigned {
			t.Errorf("traffic_route: %s", problem)
		}
	}

	wlanBenignAlwaysAssigned := map[string]string{
		"mac_filter_enabled": `carries the schema default false, so a plan never leaves it null`,
		"mac_filter_policy":  `carries the schema default "deny", so a plan never leaves it null`,
		"mac_filter_list":    `Optional-only; sending the zero keeps the apply consistent`,
	}
	seenWlanBenign := map[string]bool{}
	for _, field := range wlanKitSpec().Fields {
		scattered, ok := field.(resourcekit.ScatteredObjectField[wlanKitModel, ui.WLAN])
		if !ok {
			continue
		}
		report, err := resourcekit.MaskedZeroProblems(t.Context(), scattered,
			fullProbeObject(t, scattered.AttrTypes), nil)
		if err != nil {
			note("wlan/"+scattered.Wires[0], err)
			continue
		}
		for _, problem := range report.Guarded {
			t.Errorf("wlan: %s", problem)
		}
		reportBenignAlwaysAssigned(t, "wlan", report.AlwaysAssigned, wlanBenignAlwaysAssigned, seenWlanBenign)
	}
	for name := range wlanBenignAlwaysAssigned {
		if !seenWlanBenign[name] {
			t.Errorf("wlan: %q no longer reports as always-assigned; if it was fixed, remove "+
				"it from the pinned benign set in the same commit", name)
		}
	}

	vpnServerBenignAlwaysAssigned := map[string]string{
		"l2tp_allow_weak_ciphers": `carries the schema default false, so a plan never leaves it null`,
	}
	seenVPNServerBenign := map[string]bool{}
	for _, field := range vpnServerKitSpec().Fields {
		scattered, ok := field.(resourcekit.ScatteredObjectField[vpnServerKitModel, ui.Network])
		if !ok {
			continue
		}
		report, err := resourcekit.MaskedZeroProblems(t.Context(), scattered,
			fullProbeObject(t, scattered.AttrTypes), nil)
		if err != nil {
			note("vpn_server/"+scattered.Wires[0], err)
			continue
		}
		for _, problem := range report.Guarded {
			t.Errorf("vpn_server: %s", problem)
		}
		reportBenignAlwaysAssigned(t, "vpn_server", report.AlwaysAssigned, vpnServerBenignAlwaysAssigned, seenVPNServerBenign)
	}
	for name := range vpnServerBenignAlwaysAssigned {
		if !seenVPNServerBenign[name] {
			t.Errorf("vpn_server: %q no longer reports as always-assigned; if it was fixed, "+
				"remove it from the pinned benign set in the same commit", name)
		}
	}

	// network's own pin is the other half of this check, and it has to be
	// non-empty or the two have drifted apart.
	if alwaysAssigned["network"] == 0 {
		t.Error("network reports no always-assigned wires here, but " +
			"TestNetworkNarrowingStaysSafeWhileWiresAreUnclassified pins twenty-three " +
			"of them; one of the two instruments has stopped seeing its subject")
	}

	sort.Strings(unmeasured)
	// Pinned, so the silence is a measurement: each of these needs a fixture
	// its members will accept before this check can say anything about it.
	wantUnmeasured := []string{
		"traffic_route/domains",
		"vpn_client/x_wireguard_private_key",
	}
	if len(unmeasured) != len(wantUnmeasured) {
		t.Errorf("unmeasured = %v, pinned as %v", unmeasured, wantUnmeasured)
	}
	for index, name := range unmeasured {
		if index < len(wantUnmeasured) && name != wantUnmeasured[index] {
			t.Errorf("unmeasured[%d] = %s, pinned as %s", index, name, wantUnmeasured[index])
		}
	}
}

// TestPortForwardConditionalWiresAgreeWithEncode exists because a declared
// ConditionalWires predicate that no longer matches Encode is invisible to
// TestNoSurfaceMasksAZeroForAPartlyFilledBlock -- a declared wire is skipped
// there, taking the declaration as the answer. The full object and each
// partial derived from it exercise every predicate in both directions.
func TestPortForwardConditionalWiresAgreeWithEncode(t *testing.T) {
	checked := 0
	for _, field := range portForwardKitSpec().Fields {
		scattered, ok := field.(resourcekit.ScatteredObjectField[portForwardKitModel, ui.PortForward])
		if !ok {
			continue
		}
		full := fullProbeObject(t, scattered.AttrTypes)
		objects := []types.Object{full}
		for name := range scattered.AttrTypes {
			objects = append(objects, withoutProbeMember(t, full, name))
		}
		for _, problem := range resourcekit.ConditionalWireProblems(scattered, objects, nil) {
			t.Errorf("port_forward/%s: %s", scattered.Wires[0], problem)
		}
		checked++
	}
	if checked != 3 {
		t.Errorf("checked %d scattered field(s) on port_forward, want 3; a field the walk "+
			"missed is a field nothing here compares against its Encode", checked)
	}
}

// withoutProbeMember nulls one member of an object and leaves the rest.
func withoutProbeMember(t *testing.T, full types.Object, unset string) types.Object {
	t.Helper()
	attrTypes := full.AttributeTypes(t.Context())
	values := make(map[string]attr.Value, len(attrTypes))
	for name, value := range full.Attributes() {
		values[name] = value
	}
	values[unset] = nullAttr(attrTypes[unset])
	object, diags := types.ObjectValue(attrTypes, values)
	if diags.HasError() {
		t.Fatalf("building a probe object without %q: %v", unset, diags)
	}
	return object
}
