package unifi

import "testing"

// TestSettingSectionsOrder documents settingSections' order: writeSettings and
// readSettings loop over it, and it must match today's hand-written write
// order exactly. It is not a mapper test -- see setting_resource_test.go and
// setting_usg_mapper_test.go for those. mgmt is no longer here: it moved
// onto resourcekit.SpecSection (setting_mgmt_descriptor.go); see
// TestSettingKitSectionsOrder for the full, spliced order.
func TestSettingSectionsOrder(t *testing.T) {
	want := []string{
		"auto_speedtest",
		"country",
		"dpi",
		"lcm",
		"network_optimization",
		"ntp",
		"syslog",
		"doh",
		"ips",
		"radius",
		"usg",
		"igmp_snooping",
	}

	if len(settingSections) != len(want) {
		t.Fatalf("len(settingSections) = %d, want %d", len(settingSections), len(want))
	}

	for i, name := range want {
		ls, ok := settingSections[i].(legacySection)
		if !ok {
			t.Fatalf("settingSections[%d] (%s) is not a legacySection", i, name)
		}
		if got := ls.Name(); got != name {
			t.Errorf("settingSections[%d].Name() = %q, want %q", i, got, name)
		}
	}
}

// TestSettingKitSectionsOrder documents settingKitSections' spliced result:
// the Composite's actual Sections, in today's historical write order with
// mgmt back in its original position (index 9, right after ips).
func TestSettingKitSectionsOrder(t *testing.T) {
	want := []string{
		"auto_speedtest",
		"country",
		"dpi",
		"lcm",
		"network_optimization",
		"ntp",
		"syslog",
		"doh",
		"ips",
		"mgmt",
		"radius",
		"usg",
		"igmp_snooping",
	}

	r := &settingResource{client: &Client{}}
	sections := settingKitSections(r)
	if len(sections) != len(want) {
		t.Fatalf("len(settingKitSections(r)) = %d, want %d", len(sections), len(want))
	}
	for i, name := range want {
		if got := sections[i].Name(); got != name {
			t.Errorf("sections[%d].Name() = %q, want %q", i, got, name)
		}
	}
}
