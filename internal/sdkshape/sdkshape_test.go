package sdkshape

import (
	"sort"
	"testing"
)

const sdkPackage = "github.com/ubiquiti-community/go-unifi/unifi"

// These cases were established by hand against the SDK source, and exist
// to judge the tool rather than the SDK.
func TestBestReproducesHandVerifiedClassifications(t *testing.T) {
	pkg := load(t)

	tests := map[string]struct {
		root      string
		want      []string
		structure string // expected origin, empty when the shape is not derivable
		derivable bool
		byOrigin  string // attribute name, when the SDK field disambiguates
	}{
		// FirewallPolicyDestination carries all the wanted members, plus
		// some the provider omits.
		"firewall policy destination is a subset of its own struct": {
			root: "FirewallPolicy",
			want: []string{
				"client_macs", "ip_group_id", "ips", "matching_target", "matching_target_type",
				"network_ids", "port", "port_group_id", "port_matching_type", "web_domains", "zone_id",
			},
			structure: "FirewallPolicyDestination",
			derivable: true,
		},
		// The counterpart: source and destination have identical member
		// sets, so member matching alone cannot tell them apart -- only
		// the field each hangs off can. Origin is asserted here rather
		// than Best for that reason.
		"firewall policy source resolves to its own struct not destination": {
			root: "FirewallPolicy",
			want: []string{
				"client_macs", "ip_group_id", "ips", "matching_target", "matching_target_type",
				"network_ids", "port", "port_group_id", "port_matching_type", "web_domains", "zone_id",
			},
			structure: "FirewallPolicySource",
			derivable: true,
			byOrigin:  "source",
		},
		// These three members are grouped out of flat SDK fields
		// (PfwdInterface, DestinationIP, DstPort); no struct carries
		// them together.
		"port forward wan is invented from flat fields": {
			root:      "PortForward",
			want:      []string{"interface", "ip_address", "port"},
			derivable: false,
		},
		"port forward destination_ips mirrors its struct": {
			root:      "PortForward",
			want:      []string{"destination_ip", "interface"},
			structure: "PortForwardDestinationIPs",
			derivable: true,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			candidate, found := pkg.Best(test.root, test.want)
			if test.byOrigin != "" {
				candidate, found = pkg.Origin(test.root, test.byOrigin, test.want)
			}
			if !found {
				t.Fatalf("no candidate found for %q", test.root)
			}
			if candidate.Derivable() != test.derivable {
				t.Fatalf("derivable = %v (missing %v, via %q), want %v",
					candidate.Derivable(), candidate.Missing, candidate.Struct, test.derivable)
			}
			if test.structure != "" && candidate.Struct != test.structure {
				t.Fatalf("origin = %q, want %q", candidate.Struct, test.structure)
			}
		})
	}
}

// A renamed member is not an invented shape. wlan's private_preshared_keys is
// one-to-one with its struct except that networkconf_id is exposed as
// network_id, so the tool must report a near miss rather than a total one —
// that difference is what separates a rename, which policy already expresses,
// from a regrouping, which nothing can derive.
func TestBestDistinguishesARenameFromAnInvention(t *testing.T) {
	pkg := load(t)

	renamed, found := pkg.Best("WLAN", []string{"network_id", "password"})
	if !found {
		t.Fatal("Best(WLAN) found no candidate")
	}
	if renamed.Derivable() {
		t.Fatalf("expected a near miss, got a clean match via %q", renamed.Struct)
	}
	if len(renamed.Missing) != 1 || renamed.Missing[0] != "network_id" {
		t.Fatalf("missing = %v, want exactly [network_id]", renamed.Missing)
	}

	invented, found := pkg.Best("PortForward", []string{"interface", "ip_address", "port"})
	if !found {
		t.Fatal("Best(PortForward) found no candidate")
	}
	if len(invented.Missing) < 2 {
		t.Fatalf("an invented shape should miss most members, missing = %v", invented.Missing)
	}
}

// No type is named after unifi_wan, unifi_vpn_client or unifi_vpn_server, which
// is why deriving a root from the resource name classified all three as having
// no SDK type at all. They do: RootFor resolves each to Network from the
// methods they call. This asserts the absence that makes name derivation fail,
// so the two facts stay recorded together and the absence is not mistaken for
// the resource having no backing type.
func TestNoTypeIsNamedAfterTheNetworkViewResources(t *testing.T) {
	pkg := load(t)
	for _, absent := range []string{"WAN", "VPNClient", "VPNServer", "Wan", "VpnClient", "VpnServer"} {
		if pkg.Has(absent) {
			t.Errorf("package unexpectedly defines %q", absent)
		}
		if _, found := pkg.Best(absent, []string{"anything"}); found {
			t.Errorf("Best(%q) returned a candidate for a type that does not exist", absent)
		}
	}
	// The resources are nonetheless backed, via the operations they perform.
	if root, ok := pkg.RootFor([]string{"GetNetwork"}); !ok || root != "Network" {
		t.Fatalf("the same resources resolve to %q, %v; want Network", root, ok)
	}
}

func TestReachabilityExcludesUnrelatedStructs(t *testing.T) {
	pkg := load(t)
	reachable := pkg.Reachable("PortForward")
	if !reachable["PortForwardDestinationIPs"] {
		t.Fatal("PortForwardDestinationIPs is a field of PortForward and must be reachable")
	}
	// Coincidentally matching member names must not make these reachable.
	for _, unrelated := range []string{"RADIUSProfileAcctServers", "DNSRecord", "DeviceNutServer"} {
		if reachable[unrelated] {
			t.Errorf("%q is not referenced by PortForward and must not be reachable", unrelated)
		}
	}
}

func TestBestIsDeterministic(t *testing.T) {
	pkg := load(t)
	want := []string{"destination_ip", "interface"}
	first, _ := pkg.Best("PortForward", want)
	for range 5 {
		again, _ := pkg.Best("PortForward", want)
		if again.Struct != first.Struct {
			t.Fatalf("Best is not deterministic: %q then %q", first.Struct, again.Struct)
		}
	}
	sorted := append([]string(nil), first.Missing...)
	sort.Strings(sorted)
	if len(first.Missing) != 0 {
		t.Fatalf("expected a clean match, missing %v", sorted)
	}
}

// Every case here is one that deriving the root from the resource name got
// wrong. The SDK states what each method operates on, so asking it is not a
// better heuristic, it is the end of heuristics.
func TestRootForResolvesWhatNameDerivationCouldNot(t *testing.T) {
	pkg := load(t)

	tests := map[string]struct {
		methods []string
		want    string
	}{
		// Name derivation produced "Bgp", which does not exist.
		"bgp operates on BGPConfig": {
			methods: []string{"GetBGPConfig", "CreateBGPConfig"},
			want:    "BGPConfig",
		},
		// These three have no type of their own at all. They are three
		// Terraform views over one struct, told apart by a purpose field.
		"wan operates on Network": {
			methods: []string{"GetNetwork", "CreateNetwork"},
			want:    "Network",
		},
		// Data sources are no different: neither name predicts its type.
		"radius_user reads Account": {
			methods: []string{"GetAccount"},
			want:    "Account",
		},
		"client_qos_rate reads ClientGroup": {
			methods: []string{"GetClientGroup"},
			want:    "ClientGroup",
		},
		// A caller's preferred operation may not exist; later ones still answer.
		"falls through to an operation that exists": {
			methods: []string{"GetNoSuchThing", "GetPortForward"},
			want:    "PortForward",
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			root, ok := pkg.RootFor(test.methods)
			if !ok {
				t.Fatalf("RootFor(%v) resolved nothing", test.methods)
			}
			if root != test.want {
				t.Fatalf("RootFor(%v) = %q, want %q", test.methods, root, test.want)
			}
		})
	}

	if _, ok := pkg.RootFor([]string{"GetNoSuchThing"}); ok {
		t.Error("RootFor resolved a method the SDK does not define")
	}
}

// The three Network views are the case that motivated this: same root, and
// nothing about their names says so.
func TestNetworkViewsShareOneRoot(t *testing.T) {
	pkg := load(t)
	for _, methods := range [][]string{
		{"GetNetwork"}, {"CreateNetwork"}, {"UpdateNetwork"},
	} {
		root, ok := pkg.RootFor(methods)
		if !ok || root != "Network" {
			t.Fatalf("RootFor(%v) = %q, %v; want Network", methods, root, ok)
		}
	}
}

// TestLoadFoldsInAnExtraPackageWithoutDisturbingTheRoot is the positive
// control for Load's extra parameter: settings.Mgmt is real and must
// resolve once settings is folded in, and Dashboard -- declared by both the
// root package and settings, with different shapes -- must still resolve to
// the root's own version, per Load's documented "path always wins" rule.
func TestLoadFoldsInAnExtraPackageWithoutDisturbingTheRoot(t *testing.T) {
	pkg, err := Load(sdkPackage, sdkPackage+"/settings")
	if err != nil {
		t.Skipf("SDK package unavailable: %v", err)
	}

	mgmt, ok := pkg.Members("Mgmt")
	if !ok {
		t.Fatal(`Members("Mgmt") not found; settings was not folded in`)
	}
	if _, ok := mgmt["x_ssh_enabled"]; !ok {
		t.Errorf(`Mgmt has no "x_ssh_enabled" member; got %v`, mgmt)
	}

	dashboard, ok := pkg.Members("Dashboard")
	if !ok {
		t.Fatal(`Members("Dashboard") not found`)
	}
	if _, ok := dashboard["attr_hidden"]; !ok {
		t.Errorf(`Dashboard has no "attr_hidden" member (the root package's own shape); `+
			"got %v -- settings' Dashboard overrode the root's", dashboard)
	}
	if _, ok := dashboard["layout_preference"]; ok {
		t.Error(`Dashboard has a "layout_preference" member; that's settings' shape, ` +
			"which must not win over the root package's")
	}
}

func load(t *testing.T) *Package {
	t.Helper()
	pkg, err := Load(sdkPackage)
	if err != nil {
		t.Skipf("SDK package unavailable: %v", err)
	}
	return pkg
}
