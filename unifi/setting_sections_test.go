package unifi

import "testing"

// TestSettingSectionsOrder documents settingSections' order: writeSettings and
// readSettings loop over it, and it must match today's hand-written write
// order exactly. It is not a mapper test -- see setting_resource_test.go and
// setting_usg_mapper_test.go for those.
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
		"mgmt",
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
