package unifi

// The controller's own sensitivity declaration and the served schemas must
// agree: every wire the SDK's SensitiveFieldsByCollection declares for a
// served collection must be Sensitive in the schema snapshot wherever policy
// exposes it, and every Sensitive attribute a compiled surface serves must
// trace back either to that declaration or to a recorded provider opinion.
// The compiler derives the flag at generation time; this closes the loop
// from the other end, over committed artifacts in their own terms -- the
// mapping reports (which record each surface's collection), the policy
// files (which relate wire names to schema paths) and the schema snapshot
// -- rather than by asking the compiler, which would agree with itself by
// construction. The declaration itself is read from the pinned SDK, the
// same module the bootstraps are derived from.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	ui "github.com/ubiquiti-community/go-unifi/unifi"
)

// unservedSensitiveCollections are collections the controller declares
// secrets in that no compiled surface serves. Each entry is cross-checked
// in both directions: it must still exist in the SDK's map, and must still
// be served by no surface, so an entry cannot outlive what it describes.
var unservedSensitiveCollections = map[string]string{
	"teleport_token": "no provider surface serves teleport tokens",
}

// providerOpinionSensitive is the ledger of Sensitive attributes the
// provider marks on its own judgment, beyond the controller's declaration
// -- hand flags the derivation deliberately keeps. Keyed by artifact prefix
// then snapshot attribute path. An entry that stops describing a served,
// non-derived Sensitive attribute is reported as stale, and a new hand flag
// does not pass without being recorded here.
var providerOpinionSensitive = map[string]map[string]bool{
	// The controller's metadata carries no hotspotop entry at all, but the
	// operator password is a credential.
	"hotspot_op": {"password": true},
	// The setting collection's declaration covers the mgmt SSH secrets and
	// nothing else; the RADIUS shared secret, SNMP credentials and the
	// guest-access payment-gateway credentials are provider judgment.
	"setting": {
		"radius.secret":                              true,
		"snmp.community":                             true,
		"snmp.password":                              true,
		"guest_access.authorize_loginid":             true,
		"guest_access.authorize_transactionkey":      true,
		"guest_access.facebook_app_secret":           true,
		"guest_access.google_client_secret":          true,
		"guest_access.ippay_terminalid":              true,
		"guest_access.merchantwarrior_apikey":        true,
		"guest_access.merchantwarrior_apipassphrase": true,
		"guest_access.merchantwarrior_merchantuuid":  true,
		"guest_access.password":                      true,
		"guest_access.paypal_password":               true,
		"guest_access.paypal_signature":              true,
		"guest_access.paypal_username":               true,
		"guest_access.quickpay_agreementid":          true,
		"guest_access.quickpay_apikey":               true,
		"guest_access.quickpay_merchantid":           true,
		"guest_access.stripe_api_key":                true,
		"guest_access.wechat_app_secret":             true,
		"guest_access.wechat_secret_key":             true,
	},
	// The WireGuard client preshared key is not in the controller's
	// declaration; the peer public key and the imported configuration file
	// content are hand-defined claim members with no wire of their own.
	"vpn_client": {
		"wireguard.preshared_key":         true,
		"wireguard.peer.public_key":       true,
		"wireguard.configuration.content": true,
	},
	// Certificates, unlike their keys, are not declared by the controller;
	// the provider masks them anyway.
	"vpn_server": {
		"openvpn.ca_crt":            true,
		"openvpn.server_crt":        true,
		"openvpn.shared_client_crt": true,
	},
	// Per-network preshared keys; the controller declares only x_passphrase
	// for wlanconf.
	"wlan": {"private_preshared_keys.password": true},
}

// agreementPolicyMember is the slice of a policy field, grouping member or
// flattened member this check reads: how one observed wire (or nested
// member) is named and disposed.
type agreementPolicyMember struct {
	StructuralName string                  `json:"structural_name"`
	TerraformName  string                  `json:"terraform_name"`
	Disposition    string                  `json:"disposition"`
	ElementMember  string                  `json:"element_member"`
	Fields         []agreementPolicyMember `json:"fields"`
}

// agreementPolicy is the slice of one policy file this check reads.
type agreementPolicy struct {
	Fields    []agreementPolicyMember `json:"fields"`
	Groupings []struct {
		TerraformName string                  `json:"terraform_name"`
		Members       []agreementPolicyMember `json:"members"`
	} `json:"groupings"`
	Flattenings []struct {
		Members []agreementPolicyMember `json:"members"`
	} `json:"flattenings"`
	Claims []struct {
		StructuralNames []string `json:"structural_names"`
	} `json:"claims"`
}

// sensitivePolicyWire is one observed wire's place in the served schema:
// its leaf wire name, the snapshot's dotted attribute path, and whether the
// policy actually serves it there.
type sensitivePolicyWire struct {
	leaf   string
	path   string
	served bool
}

// policyWires resolves every structural name a policy exposes to its
// snapshot path, mirroring the compiler's emission rules: top-level fields
// and flattened members are served when managed or computed, grouping
// members and nested object members when not omitted (and only under a
// served parent). claimed collects the leaf wire names claims consume,
// which have no single path to resolve to.
func policyWires(policy agreementPolicy) (wires []sensitivePolicyWire, claimed map[string]bool) {
	claimed = map[string]bool{}
	for _, claim := range policy.Claims {
		for _, name := range claim.StructuralNames {
			leaf := name
			if dot := strings.LastIndex(name, "."); dot >= 0 {
				leaf = name[dot+1:]
			}
			claimed[leaf] = true
		}
	}
	var nested func(members []agreementPolicyMember, base string, parentServed bool)
	nested = func(members []agreementPolicyMember, base string, parentServed bool) {
		for _, member := range members {
			served := parentServed && member.Disposition != "omitted"
			path := base + "." + member.TerraformName
			if member.StructuralName != "" {
				wires = append(wires, sensitivePolicyWire{member.StructuralName, path, served})
			}
			nested(member.Fields, path, served)
		}
	}
	for _, field := range policy.Fields {
		served := field.Disposition == "managed" || field.Disposition == "computed"
		if field.StructuralName != "" {
			wires = append(wires, sensitivePolicyWire{field.StructuralName, field.TerraformName, served})
		}
		nested(field.Fields, field.TerraformName, served)
	}
	for _, grouping := range policy.Groupings {
		for _, member := range grouping.Members {
			served := member.Disposition != "omitted"
			path := grouping.TerraformName + "." + member.TerraformName
			if member.ElementMember != "" {
				// A collapsed array serves its element's values under the
				// member's own path; the element decisions below it name no
				// path of their own.
				wires = append(wires, sensitivePolicyWire{member.ElementMember, path, served})
				if member.StructuralName != "" {
					wires = append(wires, sensitivePolicyWire{member.StructuralName, path, served})
				}
				continue
			}
			if member.StructuralName != "" {
				wires = append(wires, sensitivePolicyWire{member.StructuralName, path, served})
			}
			nested(member.Fields, path, served)
		}
	}
	for _, flattening := range policy.Flattenings {
		for _, member := range flattening.Members {
			served := member.Disposition == "managed" || member.Disposition == "computed"
			if member.StructuralName != "" {
				wires = append(wires, sensitivePolicyWire{member.StructuralName, member.TerraformName, served})
			}
		}
	}
	return wires, claimed
}

// declaredSensitivityFindings compares one surface's declared wires against
// what it serves: every served occurrence of a declared leaf must be
// Sensitive in the snapshot. A declared leaf consumed by a claim has no
// path this check can follow, so it is refused rather than silently
// passed. checked counts the served occurrences actually verified, so the
// caller can prove the walk saw something.
func declaredSensitivityFindings(
	surface string,
	declared []string,
	wires []sensitivePolicyWire,
	claimed map[string]bool,
	attributes map[string]schemaSnapshotFact,
) (findings []string, checked int) {
	for _, leaf := range declared {
		if claimed[leaf] {
			findings = append(findings, fmt.Sprintf(
				"%s: declared-sensitive wire %q is consumed by a claim; this check cannot "+
					"follow a claim onto the members it maps into and refuses rather than passing",
				surface, leaf))
		}
		for _, wire := range wires {
			if wire.leaf != leaf || !wire.served {
				continue
			}
			checked++
			fact, present := attributes[wire.path]
			if !present {
				findings = append(findings, fmt.Sprintf(
					"%s: declared-sensitive wire %q maps to attribute %q, which the served "+
						"schema snapshot does not carry", surface, leaf, wire.path))
				continue
			}
			if !fact.Sensitive {
				findings = append(findings, fmt.Sprintf(
					"%s: wire %q is declared sensitive by the controller, but attribute %q is "+
						"not Sensitive in the served schema", surface, leaf, wire.path))
			}
		}
	}
	return findings, checked
}

// undeclaredSensitivityFindings is the other direction: every Sensitive
// attribute the surface serves must be derived from the declaration, a
// write-only twin of a derived attribute, or recorded provider opinion --
// and every opinion entry must still describe such an attribute.
func undeclaredSensitivityFindings(
	surface string,
	declared []string,
	wires []sensitivePolicyWire,
	opinions map[string]bool,
	attributes map[string]schemaSnapshotFact,
) []string {
	declaredSet := map[string]bool{}
	for _, leaf := range declared {
		declaredSet[leaf] = true
	}
	derived := map[string]bool{}
	for _, wire := range wires {
		if wire.served && declaredSet[wire.leaf] {
			derived[wire.path] = true
		}
	}
	var findings []string
	used := map[string]bool{}
	paths := make([]string, 0, len(attributes))
	for path := range attributes {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		if !attributes[path].Sensitive || derived[path] {
			continue
		}
		if strings.HasSuffix(path, "_wo") && derived[strings.TrimSuffix(path, "_wo")] {
			continue
		}
		if opinions[path] {
			used[path] = true
			continue
		}
		findings = append(findings, fmt.Sprintf(
			"%s: attribute %q is Sensitive with neither a controller declaration behind it "+
				"nor an entry in providerOpinionSensitive; record the opinion or derive the flag",
			surface, path))
	}
	stale := make([]string, 0, len(opinions))
	for path := range opinions {
		if !used[path] {
			stale = append(stale, path)
		}
	}
	sort.Strings(stale)
	for _, path := range stale {
		findings = append(findings, fmt.Sprintf(
			"%s: providerOpinionSensitive names %q, which is not a served, non-derived "+
				"Sensitive attribute; the entry no longer describes anything -- remove it",
			surface, path))
	}
	return findings
}

// sensitiveAgreementSurface is one compiled surface with everything this
// check needs about it: its committed mapping report's identity and
// collection, and its policy.
type sensitiveAgreementSurface struct {
	prefix     string
	kind       string
	resource   string
	collection string
	policy     agreementPolicy
}

// loadSensitiveAgreementSurfaces reads every committed surface mapping
// report and its policy file. Per-grouping section reports (managed_section)
// describe slices of a surface already covered by its own report, so they
// are skipped.
func loadSensitiveAgreementSurfaces(t *testing.T) []sensitiveAgreementSurface {
	t.Helper()
	reports, err := filepath.Glob(filepath.Join("..", "provider-codegen", "generated", "*.mapping.json"))
	if err != nil || len(reports) == 0 {
		t.Fatalf("no committed mapping reports found (%v); with nothing to walk, every "+
			"assertion below would pass vacuously", err)
	}
	sort.Strings(reports)
	var surfaces []sensitiveAgreementSurface
	for _, report := range reports {
		raw, err := os.ReadFile(report)
		if err != nil {
			t.Fatalf("the mapping report is one of this check's oracles and it is unreadable: %v", err)
		}
		var header struct {
			SurfaceKind string `json:"surface_kind"`
			Resource    string `json:"resource"`
			Collection  string `json:"collection"`
		}
		if err := json.Unmarshal(raw, &header); err != nil {
			t.Fatalf("parse %s: %v", report, err)
		}
		if header.SurfaceKind == "managed_section" {
			continue
		}
		prefix := strings.TrimSuffix(filepath.Base(report), ".mapping.json")
		policyPath := filepath.Join("..", "provider-codegen", "policy", prefix+".json")
		policyRaw, err := os.ReadFile(policyPath)
		if err != nil {
			t.Fatalf("%s has a mapping report but no policy at %s: %v", prefix, policyPath, err)
		}
		var policy agreementPolicy
		if err := json.Unmarshal(policyRaw, &policy); err != nil {
			t.Fatalf("parse %s: %v", policyPath, err)
		}
		surfaces = append(surfaces, sensitiveAgreementSurface{
			prefix:     prefix,
			kind:       header.SurfaceKind,
			resource:   header.Resource,
			collection: header.Collection,
			policy:     policy,
		})
	}
	return surfaces
}

// snapshotSection routes a surface kind to the snapshot section it is
// served under. List resources never appear here: their mapping reports are
// empty by design and not written at all.
func snapshotSection(snapshot providerSchemaSnapshot, kind string) (map[string]schemaSnapshotSurface, bool) {
	switch kind {
	case "managed_resource":
		return snapshot.Resources, true
	case "data_source":
		return snapshot.DataSources, true
	case "action":
		return snapshot.Actions, true
	}
	return nil, false
}

// TestServedSensitivityAgreesWithTheControllersDeclaration walks every
// compiled surface and asserts both directions, plus the collection
// coverage that makes the walk itself checkable: every collection the SDK
// declares secrets for is either served by some surface or explicitly
// acknowledged as unserved.
func TestServedSensitivityAgreesWithTheControllersDeclaration(t *testing.T) {
	snapshot := loadCommittedSchemaSnapshot(t)
	surfaces := loadSensitiveAgreementSurfaces(t)

	servedCollections := map[string]bool{}
	for _, surface := range surfaces {
		if surface.collection != "" {
			servedCollections[surface.collection] = true
		}
	}
	if len(ui.SensitiveFieldsByCollection) == 0 {
		t.Fatal("the SDK's SensitiveFieldsByCollection is empty; the declaration this " +
			"whole check is built on has moved out from under it")
	}
	declaredCollections := make([]string, 0, len(ui.SensitiveFieldsByCollection))
	for collection := range ui.SensitiveFieldsByCollection {
		declaredCollections = append(declaredCollections, collection)
	}
	sort.Strings(declaredCollections)
	for _, collection := range declaredCollections {
		if _, acknowledged := unservedSensitiveCollections[collection]; acknowledged {
			if servedCollections[collection] {
				t.Errorf("unservedSensitiveCollections says %q is unserved, but a mapping "+
					"report records it; the entry no longer describes anything -- remove it",
					collection)
			}
			continue
		}
		if !servedCollections[collection] {
			t.Errorf("the controller declares secrets in collection %q, which no mapping "+
				"report records; either the collection derivation broke or a new secret-"+
				"bearing collection appeared -- serve it or acknowledge it in "+
				"unservedSensitiveCollections", collection)
		}
	}
	for collection := range unservedSensitiveCollections {
		if _, declared := ui.SensitiveFieldsByCollection[collection]; !declared {
			t.Errorf("unservedSensitiveCollections names %q, which the SDK's map does not "+
				"carry; the entry no longer describes anything -- remove it", collection)
		}
	}

	totalChecked := 0
	for _, surface := range surfaces {
		section, routed := snapshotSection(snapshot, surface.kind)
		if !routed {
			t.Errorf("%s has surface kind %q, which this check cannot route to a snapshot "+
				"section; teach snapshotSection about it", surface.prefix, surface.kind)
			continue
		}
		served, ok := section[surface.resource]
		if !ok {
			t.Errorf("%s has a mapping report and a policy but no %s surface %q in the "+
				"schema snapshot; the check cannot see what it serves",
				surface.prefix, surface.kind, surface.resource)
			continue
		}
		declared := ui.SensitiveFieldsByCollection[surface.collection]
		wires, claimed := policyWires(surface.policy)
		findings, checked := declaredSensitivityFindings(
			surface.prefix, declared, wires, claimed, served.Attributes)
		totalChecked += checked
		for _, finding := range append(findings, undeclaredSensitivityFindings(
			surface.prefix, declared, wires, providerOpinionSensitive[surface.prefix],
			served.Attributes)...) {
			t.Error(finding)
		}
	}
	for prefix := range providerOpinionSensitive {
		var known bool
		for _, surface := range surfaces {
			if surface.prefix == prefix {
				known = true
			}
		}
		if !known {
			t.Errorf("providerOpinionSensitive names surface %q, which has no mapping "+
				"report; the ledger entry describes nothing -- remove it", prefix)
		}
	}

	if totalChecked == 0 {
		t.Fatal("no served occurrence of any declared wire was verified; the policy walk " +
			"or the collection recording is broken, and everything above passed vacuously")
	}
	t.Logf("verified %d served occurrences of declared-sensitive wires across %d surfaces",
		totalChecked, len(surfaces))
}

// TestDeclaredSensitivityFindingsDetectEveryDisagreement is the first
// direction's positive control: the comparison has to be shown going red on
// an unmarked served secret, a vanished attribute and a claimed wire, and
// staying quiet where policy omitted the wire or the schema agrees.
func TestDeclaredSensitivityFindingsDetectEveryDisagreement(t *testing.T) {
	wires := []sensitivePolicyWire{
		{leaf: "x_secret", path: "server.secret", served: true},
		{leaf: "x_token", path: "token", served: true},
		{leaf: "x_gone", path: "gone", served: true},
		{leaf: "x_omitted", path: "omitted", served: false},
	}
	attributes := map[string]schemaSnapshotFact{
		"server.secret": {Kind: "attribute", Sensitive: true},
		"token":         {Kind: "attribute"},
	}

	findings, checked := declaredSensitivityFindings("unifi_probe",
		[]string{"x_secret", "x_token", "x_gone", "x_omitted", "x_claimed"},
		wires, map[string]bool{"x_claimed": true}, attributes)

	if checked != 3 {
		t.Errorf("checked = %d, want 3: the served occurrences of x_secret, x_token and x_gone", checked)
	}
	if len(findings) != 3 {
		t.Fatalf("findings = %v, want exactly 3: an unmarked attribute, a vanished "+
			"attribute and a claimed wire; the agreeing and unexposed wires must stay silent",
			findings)
	}
	for _, want := range []string{`"x_token"`, `"x_gone"`, `"x_claimed"`} {
		var named bool
		for _, finding := range findings {
			if strings.Contains(finding, want) && strings.Contains(finding, "unifi_probe") {
				named = true
			}
		}
		if !named {
			t.Errorf("no finding names both unifi_probe and %s", want)
		}
	}
}

// TestUndeclaredSensitivityFindingsDetectEveryDisagreement is the second
// direction's positive control: an unaccounted Sensitive attribute and a
// stale opinion entry must be reported, while derived attributes, their
// write-only twins and live opinions stay silent.
func TestUndeclaredSensitivityFindingsDetectEveryDisagreement(t *testing.T) {
	wires := []sensitivePolicyWire{
		{leaf: "x_secret", path: "secret", served: true},
	}
	attributes := map[string]schemaSnapshotFact{
		"secret":     {Kind: "attribute", Sensitive: true},
		"secret_wo":  {Kind: "attribute", Sensitive: true},
		"opinion":    {Kind: "attribute", Sensitive: true},
		"unrecorded": {Kind: "attribute", Sensitive: true},
		"plain":      {Kind: "attribute"},
	}

	findings := undeclaredSensitivityFindings("unifi_probe",
		[]string{"x_secret"}, wires,
		map[string]bool{"opinion": true, "stale": true}, attributes)

	if len(findings) != 2 {
		t.Fatalf("findings = %v, want exactly 2: the unrecorded Sensitive attribute and "+
			"the stale opinion entry; the derived attribute, its _wo twin and the live "+
			"opinion must stay silent", findings)
	}
	for _, want := range []string{`"unrecorded"`, `"stale"`} {
		var named bool
		for _, finding := range findings {
			if strings.Contains(finding, want) {
				named = true
			}
		}
		if !named {
			t.Errorf("no finding names %s", want)
		}
	}
}

// TestSensitiveAgreementSeesTheRadiusSharedSecret pins the real-data path
// end to end: the controller has always declared radiusprofile x_secret,
// the policy serves it in both server lists, and the snapshot marks both.
// If the policy walk, the collection recording or the SDK map moved, this
// names exactly where the sight line broke.
func TestSensitiveAgreementSeesTheRadiusSharedSecret(t *testing.T) {
	for _, surface := range loadSensitiveAgreementSurfaces(t) {
		if surface.prefix != "radius_profile" {
			continue
		}
		if surface.collection != "radiusprofile" {
			t.Fatalf("radius_profile's mapping report records collection %q, want %q",
				surface.collection, "radiusprofile")
		}
		wires, _ := policyWires(surface.policy)
		paths := map[string]bool{}
		for _, wire := range wires {
			if wire.leaf == "x_secret" && wire.served {
				paths[wire.path] = true
			}
		}
		for _, want := range []string{"acct_server.secret", "auth_server.secret"} {
			if !paths[want] {
				t.Errorf("the policy walk did not resolve x_secret to %q (got %v)", want, paths)
			}
		}
		return
	}
	t.Fatal("no radius_profile surface loaded; the positive control has nothing to stand on")
}
