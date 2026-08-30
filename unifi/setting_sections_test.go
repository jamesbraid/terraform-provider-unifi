package unifi

import "testing"

// TestSettingKitSectionsOrder documents settingKitSectionTable's order: the
// Composite's actual Sections, in today's historical write order, all
// resourcekit.SpecSection kit constructor rows (see settingKitSectionTable's
// own comment). This subsumes what used to be a
// separate TestSettingSectionsOrder over the legacy-only settingSections
// var: that var is gone now that four more sections migrated onto the kit
// in the same table, so this is the sole order-authority test.
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
		"locale",
		"global_nat",
		"ssl_inspection",
		"ipsec",
		"dashboard",
		"ether_lighting",
		"global_network",
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
