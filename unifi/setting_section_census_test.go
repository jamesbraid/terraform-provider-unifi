package unifi

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	resource_setting "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_setting"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/resourcekit"
)

// TestEverySettingSectionIsServedByExactlyOneSpecSection is the gate Task 6
// replaces settingKitSectionTable's own hand-maintained order assertion
// with: instead of a literal []string a reviewer must keep in sync by eye,
// this walks the generated schema itself. Every SingleNestedAttribute
// resource_setting.SettingResourceSchema declares -- except timeouts, which
// is a SingleNestedAttribute too (terraform-plugin-framework-timeouts'
// own Attributes helper) but belongs to Composite, not to a settings
// section -- must be served by exactly one Section settingKitSections
// returns, and no Section may name an attribute the schema doesn't have.
// A row dropped from settingKitSectionTable, a row duplicated in it, or a
// SectionName typo all fail this loudly, by the schema attribute's or the
// Section's own name -- not by a diff against a second hand-written list
// that could drift the same way the table itself could.
func TestEverySettingSectionIsServedByExactlyOneSpecSection(t *testing.T) {
	ctx := context.Background()
	built := resource_setting.SettingResourceSchema(ctx)
	r := &settingResource{client: &Client{}}
	sections := settingKitSections(r)

	counts := make(map[string]int, len(sections))
	for _, s := range sections {
		counts[s.Name()]++
	}

	sawNested := 0
	for name, a := range built.Attributes {
		if name == "timeouts" {
			continue
		}
		if _, ok := a.(schema.SingleNestedAttribute); !ok {
			continue
		}
		sawNested++
		if got := counts[name]; got != 1 {
			t.Errorf("schema attribute %q is served by %d Section(s) in settingKitSections, want exactly 1",
				name, got)
		}
	}
	// A schema that stopped declaring any nested section (a generator
	// regression, or built.Attributes silently coming back empty) would
	// otherwise pass the loop above with nothing to check -- this pins that
	// the walk actually saw the twenty-four sections it's meant to.
	if sawNested != 25 {
		t.Errorf("resource_setting.SettingResourceSchema declares %d non-timeouts SingleNestedAttribute(s), want 25",
			sawNested)
	}

	for name := range counts {
		if _, ok := built.Attributes[name]; !ok {
			t.Errorf("settingKitSections has a Section named %q, which the generated schema has no attribute for",
				name)
		}
	}
}

// settingSectionConformanceCase is one row of
// TestEverySettingSectionPassesTheConformanceInstruments: a name to report
// failures under, plus a closure over the concretely-typed Spec[M, S] and
// nested schema.Schema pair that name owns. A closure, not a Spec value
// directly, because every section's Spec is generic over a different model
// and SDK type -- there is no single field type a slice of rows could share
// across settingRadiusModel/settings.Radius, settingUSGModel/settings.Usg,
// and the rest.
type settingSectionConformanceCase struct {
	name string
	run  func(t *testing.T)
}

// checkSectionConformance runs the same four instruments every individual
// descriptor's own KitSpecConformance test applies (e.g.
// TestRadiusKitSpecConformance in setting_radius_descriptor_test.go),
// against one section's Spec and its own nested schema rather than a whole
// resource's. Kept generic so every case in the table below -- every
// section plus ips_suppression and usg_geo, ips's and usg's own second
// Spec each -- can share it instead of repeating the four loops once per
// row.
func checkSectionConformance[M any, S any](t *testing.T, spec resourcekit.Spec[M, S], built schema.Schema) {
	t.Helper()
	for _, problem := range resourcekit.WireNameProblems(spec) {
		t.Error(problem)
	}
	for _, problem := range resourcekit.NestedProblems(spec) {
		t.Error(problem)
	}
	for _, problem := range resourcekit.ElideProblems(spec, built) {
		t.Error(problem)
	}
	for _, problem := range resourcekit.ZeroReadProblems(spec, built) {
		t.Error(problem)
	}
}

// TestEverySettingSectionPassesTheConformanceInstruments is the census half
// of Task 6's gate: every section descriptor already carries its own
// KitSpecConformance test (kept in place -- this is the gate, those are the
// locality when one fails), but nothing before this forced the set of
// descriptors under test to match settingKitSectionTable. A descriptor
// deleted, renamed, or never wired into the table would leave its own test
// file passing (or gone) without this failing; this table is keyed off the
// same names settingKitSectionTable uses, plus the two Extra documents, so
// a missing row here is as visible as a missing row there.
func TestEverySettingSectionPassesTheConformanceInstruments(t *testing.T) {
	ctx := context.Background()

	cases := []settingSectionConformanceCase{
		{"auto_speedtest", func(t *testing.T) {
			checkSectionConformance(t, autoSpeedtestKitSpec(), autoSpeedtestNestedSchema(ctx))
		}},
		{"country", func(t *testing.T) {
			checkSectionConformance(t, countryKitSpec(), countryNestedSchema(ctx))
		}},
		{"dpi", func(t *testing.T) {
			checkSectionConformance(t, dpiKitSpec(), dpiNestedSchema(ctx))
		}},
		{"lcm", func(t *testing.T) {
			checkSectionConformance(t, lcmKitSpec(), lcmNestedSchema(ctx))
		}},
		{"network_optimization", func(t *testing.T) {
			checkSectionConformance(t, networkOptimizationKitSpec(), networkOptimizationNestedSchema(ctx))
		}},
		{"ntp", func(t *testing.T) {
			checkSectionConformance(t, ntpKitSpec(), ntpNestedSchema(ctx))
		}},
		{"syslog", func(t *testing.T) {
			checkSectionConformance(t, syslogKitSpec(), syslogNestedSchema(ctx))
		}},
		{"doh", func(t *testing.T) {
			checkSectionConformance(t, dohKitSpec(), dohNestedSchema(ctx))
		}},
		{"ips", func(t *testing.T) {
			checkSectionConformance(t, ipsKitSpec(), ipsNestedSchema(ctx))
		}},
		{"ips_suppression", func(t *testing.T) {
			checkSectionConformance(t, ipsSuppressionKitSpec(), ipsSuppressionNestedSchema(ctx))
		}},
		{"mgmt", func(t *testing.T) {
			checkSectionConformance(t, mgmtKitSpec(), mgmtNestedSchema(ctx))
		}},
		{"radius", func(t *testing.T) {
			checkSectionConformance(t, radiusKitSpec(), radiusNestedSchema(ctx))
		}},
		{"usg", func(t *testing.T) {
			checkSectionConformance(t, usgKitSpec(), usgNestedSchema(ctx))
		}},
		{"usg_geo", func(t *testing.T) {
			checkSectionConformance(t, usgGeoKitSpec(), usgGeoNestedSchema(ctx))
		}},
		{"igmp_snooping", func(t *testing.T) {
			checkSectionConformance(t, igmpSnoopingKitSpec(), igmpSnoopingNestedSchema(ctx))
		}},
		{"locale", func(t *testing.T) {
			checkSectionConformance(t, localeKitSpec(), localeNestedSchema(ctx))
		}},
		{"global_nat", func(t *testing.T) {
			checkSectionConformance(t, globalNatKitSpec(), globalNatNestedSchema(ctx))
		}},
		{"ssl_inspection", func(t *testing.T) {
			checkSectionConformance(t, sslInspectionKitSpec(), sslInspectionNestedSchema(ctx))
		}},
		{"ipsec", func(t *testing.T) {
			checkSectionConformance(t, ipsecKitSpec(), ipsecNestedSchema(ctx))
		}},
		{"dashboard", func(t *testing.T) {
			checkSectionConformance(t, dashboardKitSpec(), dashboardNestedSchema(ctx))
		}},
		{"ether_lighting", func(t *testing.T) {
			checkSectionConformance(t, etherLightingKitSpec(), etherLightingNestedSchema(ctx))
		}},
		{"global_network", func(t *testing.T) {
			checkSectionConformance(t, globalNetworkKitSpec(), globalNetworkNestedSchema(ctx))
		}},
		{"traffic_flow", func(t *testing.T) {
			checkSectionConformance(t, trafficFlowKitSpec(), trafficFlowNestedSchema(ctx))
		}},
		{"mdns", func(t *testing.T) {
			checkSectionConformance(t, mdnsKitSpec(), mdnsNestedSchema(ctx))
		}},
		{"teleport", func(t *testing.T) {
			checkSectionConformance(t, teleportKitSpec(), teleportNestedSchema(ctx))
		}},
		{"magic_site_to_site_vpn", func(t *testing.T) {
			checkSectionConformance(t, magicSiteToSiteVpnKitSpec(), magicSiteToSiteVpnNestedSchema(ctx))
		}},
		{"global_switch", func(t *testing.T) {
			checkSectionConformance(t, globalSwitchKitSpec(), globalSwitchNestedSchema(ctx))
		}},
	}
	if len(cases) != 27 {
		t.Fatalf("settingSectionConformanceCase table has %d row(s), want 27 (twenty-five sections plus "+
			"ips_suppression and usg_geo)", len(cases))
	}

	for _, tc := range cases {
		t.Run(tc.name, tc.run)
	}
}
