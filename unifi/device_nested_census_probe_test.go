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

// TestTheDeviceNestedCensus replaces a zero that was an instrument failure.
//
// An earlier census reported unifi_device as 0 nested fields at risk and 0
// awake, across a surface where #191 had already proved a real defect -- a
// device with no port_override block lost every controller-side override. A
// zero from an instrument that cannot discriminate is worse than no number,
// because it occupies the slot a measurement would fill.
//
// THE MECHANISM WAS THE PROBE, NOT THE SURFACE. That census asked "does the
// resource assign this field" with a regex for the Go field name over the whole
// source. device_resource.go is 3,260 lines and the nested types carry members
// called Name, Enabled, Index and Type, so the pattern matched somewhere for
// every field and every one was skipped as managed. Both counts came out zero
// for the same reason, which is why neither looked like an error.
//
// This asks the syntax instead: a field is assigned when it appears as a key in
// a composite literal of its own type. That cannot match a coincidence in an
// unrelated function, and it fails loudly rather than quietly -- a nested type
// the resource never constructs by literal has no keys at all, which the
// coverage check below reports rather than passing off as "nothing assigned".
func TestTheDeviceNestedCensus(t *testing.T) {
	surfaces := []struct {
		name    string
		source  string
		subject any
	}{
		{"unifi_device", "unifi/device_resource.go", ui.Device{}},
		// THE CONTROL SURFACES, and they are here to make the device answer
		// mean something rather than to be reported. A census that returns zero
		// for device is only informative if the same instrument returns
		// non-zero somewhere -- otherwise it is the earlier regex failure again,
		// wearing a different implementation.
		{"unifi_network", "unifi/network_resource.go", ui.Network{}},
		{"unifi_wlan", "unifi/wlan_resource.go", ui.WLAN{}},
		{"unifi_firewall_policy", "unifi/firewall_policy_resource.go", ui.FirewallPolicy{}},
	}

	totalPopulation, totalManaged, totalAtRisk := 0, 0, 0
	classes := map[string][]string{}
	for _, surface := range surfaces {
		assigned := compositeLiteralKeys(t, surface.source)
		if len(assigned) == 0 {
			t.Errorf("%s: no composite literals parsed from %s, so every field would read "+
				"as unassigned", surface.name, surface.source)
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
			// is a nil pointer or an empty slice under omitempty. A BARE parent
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
				// THE POPULATION, counted BEFORE the parent filter. A final
				// count of zero means nothing until this is known to be
				// non-zero: an empty class and a blind probe produce the same
				// last line, which is exactly how the earlier census read as a
				// measurement.
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
		// WHERE THE RISK ACTUALLY IS ON THIS SURFACE. A bare parent tag is sent
		// on every write whatever it holds, so an empty one overwrites what the
		// controller had -- that is task 191, a device with no port_override
		// block losing every controller-side override. The nested-field class
		// and the parent class are different defects, and reporting only the
		// first would leave device looking untouched.
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

	// THE CLASSIFIER CONTROL: a member of each class, from anywhere in the
	// control set. Zero in both is the signature of a probe that cannot see,
	// which is the failure this test exists to replace -- and it is the reason
	// the control surfaces are here rather than the device alone.
	if totalPopulation == 0 {
		t.Fatal("no force-emitting nested field found on ANY surface; the reflection walk " +
			"is not reaching nested types and every zero below is meaningless")
	}
	// The at-risk class being empty is a RESULT here, not a control. It was not
	// empty before task 198 landed, and a control that requires a member of it
	// can only pass while the defect it describes is still open -- which is a
	// check that goes red when the code is fixed. TestTheNestedCensusCanTellThe
	// ClassesApart proves the instrument discriminates, on a tree whose answer
	// is known by construction and does not change when this one is repaired.
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

// compositeLiteralKeys returns, per SDK type name, the fields the file names as
// keys when it builds one.
//
// Keyed by the literal's own type, so DevicePortOverrides.Name and
// DeviceRadioTable.Name are counted separately. That separation is the whole
// difference from the regex it replaces, which could only ask whether the string
// "Name" appeared anywhere in the file.
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
// cannot carry itself.
//
// Its at-risk class is empty today because task 198's fix landed: the resource
// now assigns all eight nested flags. A control asserting that class is
// non-empty would therefore go red the day the defect was repaired, which is a
// check that measures the codebase's health rather than the instrument's
// eyesight -- the shape this campaign keeps finding.
//
// So the instrument is pointed at a fixture instead. One nested type, two
// members, one of them named in the literal and one not. The classification is
// known by construction and stays known however the provider changes.
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
