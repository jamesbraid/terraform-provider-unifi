package unifi

// WHICH WRITE PATHS BYPASS THE MASK.
//
// The force-emitted and cannot-clear counts have been reported as ceilings all
// along, because a masked write drops the names nothing set and an unmasked one
// does not -- and nobody had said which writes are which. This says it, and
// pins it, so a surface that quietly loses its mask fails here.
//
// READ OFF THE BACKEND, NOT THE SOURCE. Each constructor is called and its
// closures inspected. A nil client is fine: the closures are built regardless
// and only their presence is read, which is the same reason the spec's own
// GetID seeding works in a unit test.
//
// THE PICTURE IT DRAWS:
//
//	update   masked on all nineteen kit surfaces
//	create   unmasked on eighteen -- device alone patches, because a device is
//	         ADOPTED rather than made and a whole-object create would assert a
//	         zero for every attribute the plan did not set
//
// AN UNMASKED CREATE IS ONLY DANGEROUS WHERE CREATE MEANS ADOPT, and both such
// paths are covered: device through CreateFields, and wan's adoptExistingWAN
// through UpdateNetworkFields. site looks a network up by name in Read, not in
// Create, so its POST is a genuinely new object.
//
// THE REMAINING EXPOSURE IS THE HAND-WRITTEN SURFACES, listed below, whose
// updates carry no mask at all -- so every field the resource does not assign
// goes out as its Go zero on every apply.

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	ui "github.com/ubiquiti-community/go-unifi/unifi"
)

func TestEveryKitWritePathIsClassified(t *testing.T) {
	var api *ui.ApiClient
	maskedCreate := map[string]bool{}
	maskedUpdate := map[string]bool{}
	var seen []string
	record := func(name string, create, createFields, update, updateFields bool) {
		seen = append(seen, name)
		if create == createFields {
			t.Errorf("%s supplies %v for Create and %v for CreateFields; exactly one is "+
				"the contract, and both or neither is a surface whose create path nobody "+
				"can name", name, create, createFields)
		}
		if update == updateFields {
			t.Errorf("%s supplies %v for Update and %v for UpdateFields; exactly one is "+
				"the contract", name, update, updateFields)
		}
		maskedCreate[name] = createFields
		maskedUpdate[name] = updateFields
	}

	{
		backend := apGroupKitBackend(api)
		record("ap_group", backend.Create != nil, backend.CreateFields != nil,
			backend.Update != nil, backend.UpdateFields != nil)
	}
	{
		backend := clientQosRateKitBackend(api)
		record("client_qos_rate", backend.Create != nil, backend.CreateFields != nil,
			backend.Update != nil, backend.UpdateFields != nil)
	}
	{
		backend := deviceKitBackend(api)
		record("device", backend.Create != nil, backend.CreateFields != nil,
			backend.Update != nil, backend.UpdateFields != nil)
	}
	{
		backend := dnsRecordKitBackend(api)
		record("dns_record", backend.Create != nil, backend.CreateFields != nil,
			backend.Update != nil, backend.UpdateFields != nil)
	}
	{
		backend := firewallGroupKitBackend(api)
		record("firewall_group", backend.Create != nil, backend.CreateFields != nil,
			backend.Update != nil, backend.UpdateFields != nil)
	}
	{
		backend := firewallPolicyKitBackend(api)
		record("firewall_policy", backend.Create != nil, backend.CreateFields != nil,
			backend.Update != nil, backend.UpdateFields != nil)
	}
	{
		backend := firewallRuleKitBackend(api)
		record("firewall_rule", backend.Create != nil, backend.CreateFields != nil,
			backend.Update != nil, backend.UpdateFields != nil)
	}
	{
		backend := firewallZoneKitBackend(api)
		record("firewall_zone", backend.Create != nil, backend.CreateFields != nil,
			backend.Update != nil, backend.UpdateFields != nil)
	}
	{
		backend := networkKitBackend(api)
		record("network", backend.Create != nil, backend.CreateFields != nil,
			backend.Update != nil, backend.UpdateFields != nil)
	}
	{
		backend := portForwardKitBackend(api)
		record("port_forward", backend.Create != nil, backend.CreateFields != nil,
			backend.Update != nil, backend.UpdateFields != nil)
	}
	{
		backend := portProfileKitBackend(api)
		record("port_profile", backend.Create != nil, backend.CreateFields != nil,
			backend.Update != nil, backend.UpdateFields != nil)
	}
	{
		backend := radiusProfileKitBackend(api)
		record("radius_profile", backend.Create != nil, backend.CreateFields != nil,
			backend.Update != nil, backend.UpdateFields != nil)
	}
	{
		backend := radiusUserKitBackend(api)
		record("radius_user", backend.Create != nil, backend.CreateFields != nil,
			backend.Update != nil, backend.UpdateFields != nil)
	}
	{
		backend := siteToSiteVPNKitBackend(api)
		record("site_to_site_vpn", backend.Create != nil, backend.CreateFields != nil,
			backend.Update != nil, backend.UpdateFields != nil)
	}
	{
		backend := staticRouteKitBackend(api)
		record("static_route", backend.Create != nil, backend.CreateFields != nil,
			backend.Update != nil, backend.UpdateFields != nil)
	}
	{
		backend := trafficRouteKitBackend(api)
		record("traffic_route", backend.Create != nil, backend.CreateFields != nil,
			backend.Update != nil, backend.UpdateFields != nil)
	}
	{
		backend := vpnClientKitBackend(api)
		record("vpn_client", backend.Create != nil, backend.CreateFields != nil,
			backend.Update != nil, backend.UpdateFields != nil)
	}
	{
		backend := vpnServerKitBackend(api)
		record("vpn_server", backend.Create != nil, backend.CreateFields != nil,
			backend.Update != nil, backend.UpdateFields != nil)
	}
	{
		backend := wlanKitBackend(api)
		record("wlan", backend.Create != nil, backend.CreateFields != nil,
			backend.Update != nil, backend.UpdateFields != nil)
	}

	sort.Strings(seen)
	served := kitServedSurfaces(t)
	if len(seen) != len(served) {
		t.Errorf("classified %d write path(s) against %d kit-served surface(s); a surface "+
			"missing from this walk is one whose writes nobody has classified",
			len(seen), len(served))
	}

	// EVERY UPDATE IS MASKED, and that is the property the force-emitted
	// ceiling rests on. One surface losing it puts every field it does not
	// model back on the wire as a zero.
	for _, name := range seen {
		if !maskedUpdate[name] {
			t.Errorf("%s updates with a whole-object write; every field it does not model "+
				"goes out as its Go zero on every apply", name)
		}
	}

	// CREATE IS THE OTHER WAY ROUND AND device IS THE ONLY EXCEPTION. Pinned by
	// name rather than counted: a second surface adopting rather than making is
	// a decision someone has to state, and a first surface LOSING its patch is
	// the device defect returning.
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

// THE HAND-WRITTEN SURFACES WITH NO MASK ON ANY WRITE.
//
// Pinned so the list shrinks deliberately. Each of these sends a whole object
// on every apply, so every field its mapper does not assign travels as its Go
// zero -- and unlike the kit surfaces there is nothing that would notice.
//
// wan is NOT among them: its update and its adoptExistingWAN both go through
// UpdateNetworkFields. It is the one hand-written surface that already has the
// protection, which is why it reads as the model for the rest.
func TestTheUnmaskedHandWrittenSurfacesAreTheOnesWeThinkTheyAre(t *testing.T) {
	want := []string{
		"bgp",
		"client",
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

// unmaskedHandWrittenSurfaces derives the list rather than restating it.
//
// A SURFACE IS UNMASKED WHEN ITS UPDATE IS A WHOLE-OBJECT CALL, which is the
// exposure that runs on every apply. A whole-object CREATE is not the same
// claim: for a genuinely new object there is nothing to overwrite, and the two
// surfaces where create means adopt are covered elsewhere.
//
// wan has both -- CreateNetwork whole-object and UpdateNetworkFields masked --
// so keying on the update is what puts it on the right side.
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
