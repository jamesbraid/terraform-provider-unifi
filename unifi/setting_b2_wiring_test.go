package unifi

import "testing"

// TestPRB2Sections_AllRegistered proves all 4 PR-B2 sections are present in
// the settingSections registry with key()==attrName() for each — a
// property PR-B2's own sections hold (unlike the pre-existing "syslog"
// section, whose wire key "rsyslogd" legitimately diverges from its
// attrName "syslog"; the invariant checked here is scoped to PR-B2's own
// sections, not the whole registry, for exactly that reason).
func TestPRB2Sections_AllRegistered(t *testing.T) {
	want := []string{
		"mdns", "teleport", "magic_site_to_site_vpn", "radio_ai",
	}

	byKey := make(map[string]settingSection, len(settingSections))
	for _, s := range settingSections {
		byKey[s.key()] = s
	}

	for _, name := range want {
		s, ok := byKey[name]
		if !ok {
			t.Errorf("PR-B2 section %q not found in settingSections registry", name)
			continue
		}
		if s.key() != s.attrName() {
			t.Errorf("section key()=%q attrName()=%q: PR-B2 sections must have key()==attrName()", s.key(), s.attrName())
		}
	}
}
