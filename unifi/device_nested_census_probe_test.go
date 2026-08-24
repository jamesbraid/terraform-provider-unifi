package unifi

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	ui "github.com/ubiquiti-community/go-unifi/unifi"
)

// TestTheDeviceNestedCensus counts unifi_device's force-emitting nested
// fields by syntax, not by a regex over the Go field name: device_resource.go
// is 3,260 lines and its nested types reuse common field names (Name,
// Enabled, Index, Type), so a name-only match can't tell a real assignment
// from a coincidence elsewhere in the file. A field counts as assigned only
// when it appears as a key in a composite literal of its own type.
func TestTheDeviceNestedCensus(t *testing.T) {
	// Sources, plural, because the literals move: a surface served from the
	// resource kit builds its nested SDK types in its descriptor, not the
	// resource file that used to hold its CRUD. Naming only the resource
	// file would make this instrument quietly stop seeing a cut-over
	// surface.
	surfaces := []struct {
		name    string
		sources []string
		subject any
	}{
		{"unifi_device", []string{
			"unifi/device_resource.go", "unifi/device_descriptor.go",
		}, ui.Device{}},
		// The control surfaces exist to make the device answer mean
		// something, not to be reported themselves: a zero for device is
		// only informative if the same instrument returns non-zero elsewhere.
		{"unifi_network", []string{
			"unifi/network_resource.go", "unifi/network_descriptor.go",
		}, ui.Network{}},
		{"unifi_wlan", []string{"unifi/wlan_resource.go"}, ui.WLAN{}},
		{"unifi_firewall_policy", []string{
			"unifi/firewall_policy_resource.go",
			"unifi/firewall_policy_descriptor.go",
		}, ui.FirewallPolicy{}},
	}

	totalPopulation, totalManaged, totalAtRisk := 0, 0, 0
	classes := map[string][]string{}
	for _, surface := range surfaces {
		assigned := map[string]map[string]bool{}
		for _, source := range surface.sources {
			for typeName, keys := range compositeLiteralKeys(t, source) {
				if assigned[typeName] == nil {
					assigned[typeName] = map[string]bool{}
				}
				for key := range keys {
					assigned[typeName][key] = true
				}
			}
		}
		if len(assigned) == 0 {
			t.Errorf("%s: no composite literals parsed from %v, so every field would read "+
				"as unassigned", surface.name, surface.sources)
			continue
		}

		parent := reflect.TypeOf(surface.subject)
		var population, managed, atRisk []string
		var nestedTypes, builtTypes, bareParents []string

		for i := range parent.NumField() {
			outer := parent.Field(i)
			tag := outer.Tag.Get("json")
			key := strings.Split(tag, ",")[0]
			if key == "" || key == "-" {
				continue
			}
			nested := outer.Type
			for nested.Kind() == reflect.Ptr || nested.Kind() == reflect.Slice {
				nested = nested.Elem()
			}
			if nested.Kind() != reflect.Struct {
				continue
			}
			nestedTypes = append(nestedTypes, nested.Name())
			if !strings.Contains(tag, ",omitempty") {
				bareParents = append(bareParents, key)
			}
			keys, built := assigned[nested.Name()]
			if built {
				builtTypes = append(builtTypes, nested.Name())
			}

			// A nested key the provider never sends destroys nothing: the parent
			// is a nil pointer or an empty slice under omitempty. A bare parent
			// tag is sent whatever it holds.
			parentAlwaysSent := !strings.Contains(tag, ",omitempty")

			for j := range nested.NumField() {
				inner := nested.Field(j)
				innerTag := inner.Tag.Get("json")
				innerKey := strings.Split(innerTag, ",")[0]
				if innerKey == "" || innerKey == "-" {
					continue
				}
				if strings.Contains(innerTag, ",omitempty") {
					continue // a zero here is dropped, so it destroys nothing
				}
				// Population is counted before the parent filter: a final
				// count of zero means nothing until this is known to be
				// non-zero, since an empty class and a blind probe produce
				// the same last line.
				entry := surface.name + " " + key + "." + innerKey
				population = append(population, entry)
				if !parentAlwaysSent && !built {
					continue
				}
				if keys[inner.Name] {
					managed = append(managed, entry)
				} else {
					atRisk = append(atRisk, entry)
				}
			}
		}
		sort.Strings(population)
		sort.Strings(managed)
		sort.Strings(atRisk)
		totalPopulation += len(population)
		totalManaged += len(managed)
		totalAtRisk += len(atRisk)
		classes["managed"] = append(classes["managed"], managed...)
		classes["at risk"] = append(classes["at risk"], atRisk...)

		t.Logf("%s: %d nested struct type(s), %d built by literal; %d force-emitting "+
			"nested field(s) in the population",
			surface.name, len(nestedTypes), len(builtTypes), len(population))
		sort.Strings(bareParents)
		// A bare parent tag is sent on every write whatever it holds, so an
		// empty one overwrites what the controller had (a device with no
		// port_override block loses every controller-side override) -- a
		// different defect from the nested-field class above, so reporting
		// only that one would leave device looking untouched.
		t.Logf("   nested parents with a BARE tag, sent on every write (%d): %v",
			len(bareParents), bareParents)
		if len(population) == 0 {
			t.Logf("   every nested field carries omitempty, so no nested Go zero can " +
				"reach the controller from this surface at all")
			continue
		}
		t.Logf("   MANAGED %d: %v", len(managed), managed)
		t.Logf("   AT RISK %d: %v", len(atRisk), atRisk)
	}

	// Zero population across every surface is the signature of a probe that
	// can't see, which is why the control surfaces are here at all.
	if totalPopulation == 0 {
		t.Fatal("no force-emitting nested field found on ANY surface; the reflection walk " +
			"is not reaching nested types and every zero below is meaningless")
	}
	// The at-risk class being empty here is a result, not a control:
	// asserting it non-empty would only pass while the defect it describes
	// is still open, going red the day it's fixed.
	// TestTheNestedCensusCanTellTheClassesApart is the actual control, on a
	// fixture whose answer is known by construction.
	if len(classes["managed"]) == 0 {
		t.Error("nothing came out managed on any surface, and firewall_policy assigns all " +
			"eight of its nested flags; the literal keys are not being read")
	}

	t.Logf("across %d surfaces: population %d, managed %d, at risk %d",
		len(surfaces), totalPopulation, totalManaged, totalAtRisk)
	t.Log("AT RISK means the parent reaches the controller and the resource never sets this " +
		"member, so every write sends its Go zero. It is a candidate, not a defect: the " +
		"controller still has to hold a non-zero, which only a controller run settles.")
}

// compositeLiteralKeys returns, per SDK type name, the fields the file
// names as keys when it builds one. Keyed by the literal's own type, so
// DevicePortOverrides.Name and DeviceRadioTable.Name are counted separately.
func compositeLiteralKeys(t *testing.T, path string) map[string]map[string]bool {
	t.Helper()
	src, err := os.ReadFile(filepath.Join("..", path))
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	return compositeLiteralKeysIn(t, path, src)
}

// compositeLiteralKeysIn is the half that takes source rather than a path, so
// the classifier control below can feed it a tree whose answer is known.
func compositeLiteralKeysIn(t *testing.T, path string, src []byte) map[string]map[string]bool {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, src, 0)
	if err != nil {
		t.Fatalf("parsing %s: %v", path, err)
	}

	keys := map[string]map[string]bool{}
	ast.Inspect(file, func(node ast.Node) bool {
		literal, ok := node.(*ast.CompositeLit)
		if !ok {
			return true
		}
		selector, ok := literal.Type.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		name := selector.Sel.Name
		if keys[name] == nil {
			keys[name] = map[string]bool{}
		}
		for _, element := range literal.Elts {
			pair, ok := element.(*ast.KeyValueExpr)
			if !ok {
				continue
			}
			if key, ok := pair.Key.(*ast.Ident); ok {
				keys[name][key.Name] = true
			}
		}
		return true
	})
	return keys
}

// TestTheNestedCensusCanTellTheClassesApart is the control the survey above
// can't carry itself: asserting its at-risk class is non-empty would only
// hold while the missing-nested-flags defect it once caught was still open.
//
// So the instrument is pointed at a fixture instead: one nested type, two
// members, one named in the literal and one not -- known by construction
// and stable however the provider changes.
func TestTheNestedCensusCanTellTheClassesApart(t *testing.T) {
	fixture := []byte(`package fixture

func build() *unifi.FirewallPolicySource {
	return &unifi.FirewallPolicySource{
		ZoneID:           "z",
		MatchOppositeIPs: true,
	}
}
`)
	keys := compositeLiteralKeysIn(t, "fixture.go", fixture)
	members, built := keys["FirewallPolicySource"]
	if !built {
		t.Fatal("the walk did not see a FirewallPolicySource literal at all, so neither " +
			"class in the survey above means anything")
	}
	if !members["MatchOppositeIPs"] {
		t.Error("a member named in the literal did not come out assigned; the survey's " +
			"MANAGED class is unreliable")
	}
	if members["MatchMAC"] {
		t.Error("a member the literal never names came out assigned; the survey's AT RISK " +
			"class is unreliable, and this is the failure mode of the regex census it replaces")
	}

	// The specific way the old probe failed: a name that appears SOMEWHERE in
	// the file, in a literal of a different type, must not count.
	other := []byte(`package fixture

func build() {
	_ = &unifi.DevicePortOverrides{Name: "port 1"}
	_ = &unifi.DeviceRadioTable{Channel: 6}
}
`)
	keys = compositeLiteralKeysIn(t, "other.go", other)
	if keys["DeviceRadioTable"]["Name"] {
		t.Error("Name leaked from DevicePortOverrides into DeviceRadioTable; the keys are " +
			"not separated by type and the census is the old regex again")
	}
	if !keys["DevicePortOverrides"]["Name"] {
		t.Error("Name was not recorded for the type that does declare it")
	}
}
