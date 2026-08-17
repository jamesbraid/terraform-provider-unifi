package schemaparity

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// The numbers here are measured, not invented. Built the provider from
// v0.101.2 and from main, dumped both through terraform 1.15.8, and compared
// the digest artifacts: exactly two surface digests move,
// resource_schemas.unifi_network and resource_schemas.unifi_wlan, and the
// whole-schema hash moves with them.
//
// resource_schemas.unifi_radius_profile is the FALSIFIER and it is the useful
// half. That surface genuinely changed tonight -- use_usg_auth_server lost a
// static default -- and its digest does NOT move, because the protocol schema
// carries no defaults at all: zero occurrences of "default", zero of
// "plan_modifier", against 823 of "computed". A gate that flagged
// radius_profile would be reading something other than the canonical schema,
// and a gate that flagged nothing might be reading nothing.

func digestsFixture(network, wlan, radius, canonical string) Digests {
	return Digests{
		FormatVersion: 1, ProviderAddress: "registry.terraform.io/ubiquiti-community/unifi",
		CanonicalSHA256: canonical,
		SchemaSHA256: map[string]string{
			"resource_schemas.unifi_network":        network,
			"resource_schemas.unifi_wlan":           wlan,
			"resource_schemas.unifi_radius_profile": radius,
			"data_source_schemas.unifi_network":     "ds-network",
			"list_resource_schemas.unifi_wlan":      "list-wlan",
		},
	}
}

func twoSurfaceLedger() Ledger {
	e := func(surface, attribute string) Entry {
		return Entry{Surface: surface, Attribute: attribute, Field: "computed", Old: "false", New: "true",
			WhyNotBreaking: "still optional", Evidence: []Evidence{{Claim: "measured", Kind: Measured}}}
	}
	return Ledger{Entries: []Entry{e("unifi_network", "ipv6_static_subnet"), e("unifi_wlan", "ap_group_ids")}}
}

// TestDigestsAcceptExactlyTheDeclaredSurfaces is the pre-registered prediction.
func TestDigestsAcceptExactlyTheDeclaredSurfaces(t *testing.T) {
	released := digestsFixture("net-A", "wlan-A", "radius-A", "whole-A")
	candidate := digestsFixture("net-B", "wlan-B", "radius-A", "whole-B")
	if f := CheckDigests(released, candidate, twoSurfaceLedger()); len(f) != 0 {
		t.Fatalf("the two declared surfaces were refused: %v", f)
	}
}

// TestDigestsRefuseTheFalsifier is the half that proves the gate is reading.
// radius_profile did not change in the protocol schema, so a candidate whose
// radius digest moved is an undeclared change and must be named.
func TestDigestsRefuseTheFalsifier(t *testing.T) {
	released := digestsFixture("net-A", "wlan-A", "radius-A", "whole-A")
	candidate := digestsFixture("net-B", "wlan-B", "radius-MOVED", "whole-B")
	f := CheckDigests(released, candidate, twoSurfaceLedger())
	if len(f) != 1 || !strings.Contains(f[0].Detail, "resource_schemas.unifi_radius_profile") {
		t.Fatalf("findings = %v, want one naming radius_profile", f)
	}
}

// TestDigestsRefuseADeclaredSurfaceThatDidNotMove closes the other direction.
func TestDigestsRefuseADeclaredSurfaceThatDidNotMove(t *testing.T) {
	released := digestsFixture("net-A", "wlan-A", "radius-A", "whole-A")
	candidate := digestsFixture("net-B", "wlan-A", "radius-A", "whole-B")
	f := CheckDigests(released, candidate, twoSurfaceLedger())
	if len(f) != 1 || !strings.Contains(f[0].Detail, "did not\nmove") && !strings.Contains(f[0].Detail, "did not move") {
		t.Fatalf("findings = %v, want one naming the unmoved declared surface", f)
	}
}

// TestDigestsScopeLedgerEntriesToResourceSchemas pins the deliberate narrowing.
// A ledger entry names surface, attribute and field with no container, so an
// entry written about unifi_network the RESOURCE must not license a change to
// unifi_network the DATA SOURCE.
func TestDigestsScopeLedgerEntriesToResourceSchemas(t *testing.T) {
	released := digestsFixture("net-A", "wlan-A", "radius-A", "whole-A")
	candidate := digestsFixture("net-B", "wlan-B", "radius-A", "whole-B")
	candidate.SchemaSHA256["data_source_schemas.unifi_network"] = "ds-MOVED"
	f := CheckDigests(released, candidate, twoSurfaceLedger())
	if len(f) != 1 || !strings.Contains(f[0].Detail, "data_source_schemas.unifi_network") {
		t.Fatalf("findings = %v, want the data source flagged despite a resource entry", f)
	}
}

// TestWholeSchemaHashIsAssertedInTheInverseDirection. It cannot be scoped per
// surface, so with declared changes it MUST move: a declaration that leaves it
// untouched never reached the schema.
func TestWholeSchemaHashIsAssertedInTheInverseDirection(t *testing.T) {
	released := digestsFixture("net-A", "wlan-A", "radius-A", "whole-A")
	same := digestsFixture("net-B", "wlan-B", "radius-A", "whole-A")
	f := CheckDigests(released, same, twoSurfaceLedger())
	if len(f) != 1 || !strings.Contains(f[0].Detail, "never reached the schema") {
		t.Fatalf("findings = %v, want the whole-schema hash flagged for NOT moving", f)
	}
	moved := digestsFixture("net-A", "wlan-A", "radius-A", "whole-B")
	if f := CheckDigests(released, moved, Ledger{}); len(f) != 1 ||
		!strings.Contains(f[0].Detail, "with nothing declared") {
		t.Fatalf("findings = %v, want the hash flagged for moving with an empty ledger", f)
	}
}

// TestNoComparisonMixesCommittedAndRunOperands is the structural rule, and it
// is the one that does not depend on anybody classifying correctly.
//
// Three of the script's comparisons were left byte-exact deliberately:
// determinism, cross-CLI agreement, and the inverted control. All three compare
// two artifacts from THIS RUN. Every comparison that was wrong -- the schema
// contents, and then the digests a second time -- had one operand under
// ${repository_root}, because a committed operand means the comparison is
// asserting something about the released baseline.
//
// So the discriminator is provenance, not the file's name, and it is checkable
// without knowing what any file means. Both earlier misclassifications named
// the file and reasoned from that.
func TestNoComparisonMixesCommittedAndRunOperands(t *testing.T) {
	raw, err := os.ReadFile("../../.woodpecker/scripts/catalog-build-schema.sh")
	if err != nil {
		t.Fatalf("read the script: %v", err)
	}
	lines := strings.Split(string(raw), "\n")
	cmpLine := regexp.MustCompile(`^\s*(if\s+)?cmp\b`)
	var mixed []string
	for i, line := range lines {
		if !cmpLine.MatchString(line) {
			continue
		}
		operands := line
		for j := i; j < len(lines)-1 && strings.HasSuffix(strings.TrimSpace(operands), `\`); j++ {
			operands += lines[j+1]
		}
		committed := strings.Contains(operands, "${repository_root}")
		run := strings.Contains(operands, "${work_root}")
		if committed && run {
			mixed = append(mixed, strings.TrimSpace(line))
		}
	}
	if len(mixed) > 0 {
		t.Errorf(`%d comparison(s) in catalog-build-schema.sh put a committed artifact against one
built by this run:

  %s

A ${repository_root} operand means the comparison asserts something about the
RELEASED BASELINE, which is the claim the schema-change ledger now governs. Two
${work_root} operands mean it asserts something about this run -- determinism,
cross-CLI agreement, the inverted control -- and those stay byte-exact.

Both times this was got wrong, the line was classified by the file's name rather
than by where its operands came from.`, len(mixed), strings.Join(mixed, "\n  "))
	}
}
