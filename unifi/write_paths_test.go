package unifi

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	ui "github.com/ubiquiti-community/go-unifi/unifi"
)

// TestEveryKitWritePathIsClassified pins, per kit-served surface, whether
// its create masks unset fields (CreateFields) or sends a whole object.
// Update has only ever had one shape since Backend.Update (the whole-object
// write) was removed: every kit surface updates through UpdateFields, a
// structural guarantee now rather than a convention to check. device is the
// only create that patches rather than creates, since it adopts an existing
// object rather than making one.
func TestEveryKitWritePathIsClassified(t *testing.T) {
	// nil is fine: only the closures' presence is inspected below, not called.
	var api *ui.ApiClient
	maskedCreate := map[string]bool{}
	var seen []string
	record := func(name string, create, createFields bool) {
		seen = append(seen, name)
		if create == createFields {
			t.Errorf("%s supplies %v for Create and %v for CreateFields; exactly one is "+
				"the contract, and both or neither is a surface whose create path nobody "+
				"can name", name, create, createFields)
		}
		maskedCreate[name] = createFields
	}

	{
		backend := apGroupKitBackend(api)
		record("ap_group", backend.Create != nil, backend.CreateFields != nil)
	}
	{
		backend := clientKitBackend(api)
		record("client", backend.Create != nil, backend.CreateFields != nil)
	}
	{
		backend := clientQosRateKitBackend(api)
		record("client_qos_rate", backend.Create != nil, backend.CreateFields != nil)
	}
	{
		backend := deviceKitBackend(api)
		record("device", backend.Create != nil, backend.CreateFields != nil)
	}
	{
		backend := dnsRecordKitBackend(api)
		record("dns_record", backend.Create != nil, backend.CreateFields != nil)
	}
	{
		backend := firewallGroupKitBackend(api)
		record("firewall_group", backend.Create != nil, backend.CreateFields != nil)
	}
	{
		backend := firewallPolicyKitBackend(api)
		record("firewall_policy", backend.Create != nil, backend.CreateFields != nil)
	}
	{
		backend := firewallRuleKitBackend(api)
		record("firewall_rule", backend.Create != nil, backend.CreateFields != nil)
	}
	{
		backend := firewallZoneKitBackend(api)
		record("firewall_zone", backend.Create != nil, backend.CreateFields != nil)
	}
	{
		backend := networkKitBackend(api)
		record("network", backend.Create != nil, backend.CreateFields != nil)
	}
	{
		backend := portForwardKitBackend(api)
		record("port_forward", backend.Create != nil, backend.CreateFields != nil)
	}
	{
		backend := portProfileKitBackend(api)
		record("port_profile", backend.Create != nil, backend.CreateFields != nil)
	}
	{
		backend := radiusProfileKitBackend(api)
		record("radius_profile", backend.Create != nil, backend.CreateFields != nil)
	}
	{
		backend := radiusUserKitBackend(api)
		record("radius_user", backend.Create != nil, backend.CreateFields != nil)
	}
	{
		backend := siteToSiteVPNKitBackend(api)
		record("site_to_site_vpn", backend.Create != nil, backend.CreateFields != nil)
	}
	{
		backend := staticRouteKitBackend(api)
		record("static_route", backend.Create != nil, backend.CreateFields != nil)
	}
	{
		backend := trafficRouteKitBackend(api)
		record("traffic_route", backend.Create != nil, backend.CreateFields != nil)
	}
	{
		backend := vpnClientKitBackend(api)
		record("vpn_client", backend.Create != nil, backend.CreateFields != nil)
	}
	{
		backend := vpnServerKitBackend(api)
		record("vpn_server", backend.Create != nil, backend.CreateFields != nil)
	}
	{
		backend := wlanKitBackend(api)
		record("wlan", backend.Create != nil, backend.CreateFields != nil)
	}

	sort.Strings(seen)
	served := kitServedSurfaces(t)
	if len(seen) != len(served) {
		t.Errorf("classified %d write path(s) against %d kit-served surface(s); a surface "+
			"missing from this walk is one whose writes nobody has classified",
			len(seen), len(served))
	}

	// Pinned by name, not just count: a new adopting surface is a decision to
	// state, and device losing its patch is a regression either way.
	var patchingCreates []string
	for _, name := range seen {
		if maskedCreate[name] {
			patchingCreates = append(patchingCreates, name)
		}
	}
	sort.Strings(patchingCreates)
	if len(patchingCreates) != 1 || patchingCreates[0] != "device" {
		t.Errorf("surfaces whose create is a patch = %v, want exactly [device]; a create "+
			"that patches is one whose object the controller already holds, and that is a "+
			"claim about the surface rather than a style", patchingCreates)
	}
}

// TestTheUnmaskedHandWrittenSurfacesAreTheOnesWeThinkTheyAre pins the
// hand-written surfaces whose writes carry no mask at all, so every field
// their mapper doesn't assign goes out as a Go zero on every apply -- unlike
// the kit surfaces, nothing else would notice. wan is not among them: both
// its update and its adopt path go through the masked UpdateNetworkFields.
func TestTheUnmaskedHandWrittenSurfacesAreTheOnesWeThinkTheyAre(t *testing.T) {
	want := []string{
		"bgp",
		"dynamic_dns",
		"power_supervisor",
		"setting",
		"site",
		"wireguard_peer",
	}
	got := unmaskedHandWrittenSurfaces(t)
	if len(got) != len(want) {
		t.Errorf("unmasked hand-written surfaces = %v, pinned as %v", got, want)
		return
	}
	for index, name := range got {
		if name != want[index] {
			t.Errorf("unmasked[%d] = %s, pinned as %s", index, name, want[index])
		}
	}
}

// unmaskedHandWrittenSurfaces derives the list from source: a surface is
// unmasked when its update is a whole-object call. A whole-object create
// isn't the same claim (a genuinely new object has nothing to overwrite), so
// only Update calls are matched -- which is what keeps wan, whose
// CreateNetwork is whole-object but UpdateNetworkFields is masked, off this list.
func unmaskedHandWrittenSurfaces(t *testing.T) []string {
	t.Helper()
	files, err := filepath.Glob("*_resource.go")
	if err != nil {
		t.Fatalf("listing resources: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("no resource files found; a comparison with an empty left side always agrees")
	}
	whole := regexp.MustCompile(`client\.Update([A-Za-z]+)\(`)
	var surfaces []string
	sawAnyCall := false
	for _, file := range files {
		source, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("reading %s: %v", file, err)
		}
		unmasked := false
		for _, match := range whole.FindAllStringSubmatch(string(source), -1) {
			sawAnyCall = true
			if !strings.HasSuffix(match[1], "Fields") {
				unmasked = true
			}
		}
		if unmasked {
			surfaces = append(surfaces, strings.TrimSuffix(filepath.Base(file), "_resource.go"))
		}
	}
	if !sawAnyCall {
		t.Fatal("no client Update call found in any resource file; the pattern is wrong " +
			"and this would report an empty list as a clean estate")
	}
	sort.Strings(surfaces)
	return surfaces
}
