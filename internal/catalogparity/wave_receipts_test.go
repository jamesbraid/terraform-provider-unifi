package catalogparity

import (
	"encoding/json"
	"reflect"
	"testing"
)

type staticWaveReceipt struct {
	FormatVersion           int               `json:"format_version"`
	Wave                    int               `json:"wave"`
	Result                  string            `json:"result"`
	Promotion               string            `json:"promotion"`
	RuntimeChanged          bool              `json:"runtime_changed"`
	ControllerExecution     string            `json:"controller_execution"`
	SurfaceCount            int               `json:"surface_count"`
	SurfaceCounts           map[string]int    `json:"surface_counts"`
	StatusCounts            map[string]int    `json:"status_counts"`
	BlockerCounts           map[string]int    `json:"blocker_counts"`
	ShadowArtifacts         map[string]string `json:"shadow_artifacts,omitempty"`
	SchemaSHA256            string            `json:"schema_sha256"`
	LedgerSHA256            string            `json:"ledger_sha256"`
	SurfaceContractsSHA256  string            `json:"surface_contracts_sha256"`
	MigrationManifestSHA256 string            `json:"migration_manifest_sha256"`
}

func TestWave1ReadSurfaceReceipt(t *testing.T) {
	receipt := readStaticWaveReceipt(t, "../../build/wave1/read-surfaces.json")
	if receipt.FormatVersion != 1 || receipt.Wave != 1 || receipt.Result != "static_pass" || receipt.Promotion != "blocked_evidence" {
		t.Fatalf("unexpected Wave 1 identity: %+v", receipt)
	}
	if receipt.RuntimeChanged || receipt.ControllerExecution != "not_run" {
		t.Fatalf("Wave 1 execution claims = runtime:%v controller:%q", receipt.RuntimeChanged, receipt.ControllerExecution)
	}
	if receipt.SurfaceCount != 38 || !reflect.DeepEqual(receipt.SurfaceCounts, map[string]int{"data_source": 13, "list_resource": 25}) {
		t.Fatalf("Wave 1 surface counts = %d %v", receipt.SurfaceCount, receipt.SurfaceCounts)
	}
	// Compared against the LEDGER rather than against a constant. A receipt's
	// status_counts is a claim about the ledger it pins by digest, and a
	// constant expectation cannot tell a correct claim from a stale one -- it
	// only says the receipt still says what it used to say. This wave's counts
	// went stale the moment its first surface migrated, and the constant here
	// went on passing.
	requireStatusCountsMatchLedger(t, 1, receipt.StatusCounts)
	if !reflect.DeepEqual(receipt.BlockerCounts, map[string]int{
		"adapter_differential":        38,
		"locked_controller_lifecycle": 38,
		"pagination_filter":           25,
	}) {
		t.Fatalf("Wave 1 blocker counts = %v", receipt.BlockerCounts)
	}
	requireDigestMatches(t, receipt.SchemaSHA256, "../../provider-contracts/schema/terraform-1.15.8.json")
	requireDigestMatches(t, receipt.LedgerSHA256, "../../provider-codegen/generated/catalog-parity-ledger.json")
	requireDigestMatches(t, receipt.SurfaceContractsSHA256, "../../provider-codegen/generated/catalog-surface-contracts.json")
	requireDigestMatches(t, receipt.MigrationManifestSHA256, "../../provider-codegen/generated/catalog-migration-manifest.json")

	ledger := parseTestLedger(t)
	corpus := parseTestCorpus(t)
	seen := 0
	for _, contract := range corpus.Contracts {
		if contract.Wave != 1 {
			continue
		}
		seen++
		// policy_complete is where a wave-1 surface starts, and the in-flight
		// states are where it goes as it is migrated. Naming the states rather
		// than the surfaces is what stops this receipt from needing an edit once
		// per landing -- and an in-flight state is not a loophole, because the
		// ledger refuses one that does not declare why the surface rests there.
		//
		// There is no per-surface exception left. Two were merged away here:
		// unifi_dns_record's list surface, held at shadow_only, and
		// unifi_ap_group's data source, the first wave-1 surface to move at all.
		// Both are in flight, and the states below accept them without naming
		// them -- which is the point. An exception list grows once per landing;
		// a set of admissible states does not.
		if err := ledger.Require(contract.SurfaceKey,
			PolicyComplete, GeneratedShadow, AdapterParity); err != nil {
			t.Fatal(err)
		}
		if contract.Kind == ListResource && !containsString(contract.EvidenceGates, "pagination_filter") {
			t.Fatalf("list contract %s lacks pagination/filter gate", contract.Name)
		}
	}
	if seen != 38 {
		t.Fatalf("Wave 1 corpus surfaces = %d, want 38", seen)
	}
}

func TestWave2FleetFoundationReceipt(t *testing.T) {
	receipt := readStaticWaveReceipt(t, "../../build/wave2/fleet-foundations.json")
	if receipt.FormatVersion != 1 || receipt.Wave != 2 || receipt.Result != "static_pass" || receipt.Promotion != "blocked_evidence" {
		t.Fatalf("unexpected Wave 2 identity: %+v", receipt)
	}
	if receipt.RuntimeChanged || receipt.ControllerExecution != "not_run" {
		t.Fatalf("Wave 2 execution claims = runtime:%v controller:%q", receipt.RuntimeChanged, receipt.ControllerExecution)
	}
	if receipt.SurfaceCount != 8 || !reflect.DeepEqual(receipt.SurfaceCounts, map[string]int{"managed_resource": 8}) {
		t.Fatalf("Wave 2 surface counts = %d %v", receipt.SurfaceCount, receipt.SurfaceCounts)
	}
	requireStatusCountsMatchLedger(t, 2, receipt.StatusCounts)
	if !reflect.DeepEqual(receipt.BlockerCounts, map[string]int{
		"adapter_differential":        7,
		"locked_controller_lifecycle": 7,
	}) {
		t.Fatalf("Wave 2 blocker counts = %v", receipt.BlockerCounts)
	}
	requireStaticWaveDigests(t, receipt)

	ledger := parseTestLedger(t)
	corpus := parseTestCorpus(t)
	seen := 0
	for _, contract := range corpus.Contracts {
		if contract.Wave != 2 {
			continue
		}
		seen++
		want := PolicyComplete
		switch contract.Name {
		case "unifi_dns_record":
			want = Admitted
		// Nine of its twelve SDK fields are controller bookkeeping the released
		// schema never exposed, so the policy omits them by name.
		case "unifi_firewall_zone":
			want = GeneratedShadow
		// The smallest surface in the estate: three SDK fields, two of them
		// renamed.
		case "unifi_site":
			want = GeneratedShadow
		// Three renames, and the second surface whose released descriptions
		// are plain rather than Markdown.
		case "unifi_firewall_group":
			want = GeneratedShadow
		// Two renames, one of them a secret: the SDK stores the password as
		// x_password, and the policy marks it sensitive so the compiler's
		// secret guard stays satisfied.
		case "unifi_dynamic_dns":
			want = GeneratedShadow
		// Two renames the name cannot survive: the SDK carries networkgroup
		// beside wan_networkgroup, and vlan beside wan_vlan and
		// wan_vlan_enabled. policy-scaffold bound both by name, wrongly, and
		// internal/renamecheck read the right ones out of the conversion code.
		case "unifi_wan":
			want = GeneratedShadow
		}
		if err := ledger.Require(contract.SurfaceKey, want); err != nil {
			t.Fatal(err)
		}
		if !containsString(contract.EvidenceGates, "migration") || !containsString(contract.EvidenceGates, "recovery") {
			t.Fatalf("managed contract %s lacks migration/recovery gates", contract.Name)
		}
	}
	if seen != 8 {
		t.Fatalf("Wave 2 corpus surfaces = %d, want 8", seen)
	}
}

func TestWave3FleetDependentReceipt(t *testing.T) {
	receipt := readStaticWaveReceipt(t, "../../build/wave3/fleet-dependent.json")
	if receipt.FormatVersion != 1 || receipt.Wave != 3 || receipt.Result != "static_pass" || receipt.Promotion != "blocked_evidence" {
		t.Fatalf("unexpected Wave 3 identity: %+v", receipt)
	}
	if receipt.RuntimeChanged || receipt.ControllerExecution != "not_run" {
		t.Fatalf("Wave 3 execution claims = runtime:%v controller:%q", receipt.RuntimeChanged, receipt.ControllerExecution)
	}
	if receipt.SurfaceCount != 9 || !reflect.DeepEqual(receipt.SurfaceCounts, map[string]int{"managed_resource": 9}) {
		t.Fatalf("Wave 3 surface counts = %d %v", receipt.SurfaceCount, receipt.SurfaceCounts)
	}
	requireStatusCountsMatchLedger(t, 3, receipt.StatusCounts)
	if !reflect.DeepEqual(receipt.BlockerCounts, map[string]int{
		"adapter_differential":        9,
		"locked_controller_lifecycle": 9,
		"nested_schema_adapter":       1,
	}) {
		t.Fatalf("Wave 3 blocker counts = %v", receipt.BlockerCounts)
	}
	requireStaticWaveDigests(t, receipt)
	portForwardShadow := receipt.ShadowArtifacts["unifi_port_forward"]
	requireDigestMatches(t, portForwardShadow, "../../build/m0/port-forward-shadow.json")

	ledger := parseTestLedger(t)
	corpus := parseTestCorpus(t)
	seen := 0
	for _, contract := range corpus.Contracts {
		if contract.Wave != 3 {
			continue
		}
		seen++
		want := PolicyComplete
		switch contract.Name {
		case "unifi_port_forward":
			want = ShadowOnly
			if contract.BaselineSchemaSHA256 != "dec99a303604aa0a4d86ed8c6082ab616d404b6627f5996ca4b4d9fd479f71b6" {
				t.Fatalf("port-forward schema digest = %q", contract.BaselineSchemaSHA256)
			}
		// The first surface compiled from a catalog and a policy. It rests at
		// generated_shadow until a campaign run can compare the generated
		// resource against the hand-written one it replaces.
		case "unifi_firewall_policy":
			want = GeneratedShadow
		// The widest surface in the estate, and the one that needed blocks and
		// an invented member to be expressible at all.
		case "unifi_wlan":
			want = GeneratedShadow
		// The surface that moved a plan modifier into a leaf package so
		// generated code could name it, and the first whose released
		// descriptions are plain rather than Markdown.
		case "unifi_ap_group":
			want = GeneratedShadow
		// The first surface generated with no omissions at all: every SDK
		// field it carries is served.
		case "unifi_wireguard_peer":
			want = GeneratedShadow
		// The third surface whose released descriptions are plain, and the
		// second to serve a GoDuration string over an SDK int64.
		case "unifi_port_profile":
			want = GeneratedShadow
		}
		if err := ledger.Require(contract.SurfaceKey, want); err != nil {
			t.Fatal(err)
		}
	}
	if seen != 9 {
		t.Fatalf("Wave 3 corpus surfaces = %d, want 9", seen)
	}
}

func TestWave4RemainingManagedReceipt(t *testing.T) {
	receipt := readStaticWaveReceipt(t, "../../build/wave4/remaining-managed.json")
	if receipt.FormatVersion != 1 || receipt.Wave != 4 || receipt.Result != "static_pass" || receipt.Promotion != "blocked_evidence" {
		t.Fatalf("unexpected Wave 4 identity: %+v", receipt)
	}
	if receipt.RuntimeChanged || receipt.ControllerExecution != "not_run" {
		t.Fatalf("Wave 4 execution claims = runtime:%v controller:%q", receipt.RuntimeChanged, receipt.ControllerExecution)
	}
	if receipt.SurfaceCount != 11 || !reflect.DeepEqual(receipt.SurfaceCounts, map[string]int{"managed_resource": 11}) {
		t.Fatalf("Wave 4 surface counts = %d %v", receipt.SurfaceCount, receipt.SurfaceCounts)
	}
	requireStatusCountsMatchLedger(t, 4, receipt.StatusCounts)
	if !reflect.DeepEqual(receipt.BlockerCounts, map[string]int{
		"adapter_differential":        11,
		"locked_controller_lifecycle": 11,
	}) {
		t.Fatalf("Wave 4 blocker counts = %v", receipt.BlockerCounts)
	}
	requireStaticWaveDigests(t, receipt)

	ledger := parseTestLedger(t)
	corpus := parseTestCorpus(t)
	seen := 0
	for _, contract := range corpus.Contracts {
		if contract.Wave != 4 {
			continue
		}
		seen++
		want := PolicyComplete
		switch contract.Name {
		// The first surface whose schema needed a capability no earlier one
		// did: its SDK carries a Settings struct that the released schema
		// presents as three top-level durations.
		case "unifi_power_supervisor":
			want = GeneratedShadow
		// Fronts the SDK's ClientGroup rather than a struct of its own name,
		// and carries the estate's first negative default.
		case "unifi_client_qos_rate":
			want = GeneratedShadow
		// Renames network_id from networkconf_id, which no name match would
		// have found, and serves the deprecated unifi_account alias off the
		// same schema.
		case "unifi_radius_user":
			want = GeneratedShadow
		// Has no policy of its own: deprecatedAccountResource embeds
		// radiusUserResource and calls its Schema, so it moved state in the
		// same edit that migrated unifi_radius_user.
		case "unifi_account":
			want = GeneratedShadow
		// The estate's only surface with a provider-owned nested attribute:
		// peers is configuration rendered into an opaque FRR config string,
		// which the controller stores and never gives back.
		case "unifi_bgp":
			want = GeneratedShadow
		// The estate's second collision trap: the SDK carries both type and
		// static-route_type, and the released type attribute is the second.
		case "unifi_static_route":
			want = GeneratedShadow
		// The second surface whose released type differs from the SDK's:
		// interim_update_interval is int64 seconds in go-unifi and a
		// GoDuration string in the schema, so it keeps schema version 1.
		case "unifi_radius_profile":
			want = GeneratedShadow
		// Flat, and every one of its ten renames is a spelling the SDK chose
		// and the schema did not -- networkconf_id, firewallgroup_ids, ipsec.
		case "unifi_firewall_rule":
			want = GeneratedShadow
		// Serves 23 attributes over unifi.Network, the widest SDK struct in
		// the estate, and omits 260 of its 281 fields. Its write-only
		// pre_shared_key_wo is grafted, as wlan's passphrase_wo is.
		case "unifi_site_to_site_vpn":
			want = GeneratedShadow
		// Fifteen top-level attributes and nine declared groupings over the
		// same unifi.Network, omitting 219 of its 263 fields. The first
		// surface whose grouped members consume an array<object>, and the
		// first to need cmd/nested-type-dedup: dhcp.options and
		// dhcpv6.options are both called `options`, and the generator names a
		// nested object type from the attribute name rather than its path.
		case "unifi_wan":
			want = GeneratedShadow
		}
		if err := ledger.Require(contract.SurfaceKey, want); err != nil {
			t.Fatal(err)
		}
	}
	if seen != 11 {
		t.Fatalf("Wave 4 corpus surfaces = %d, want 11", seen)
	}
}

// The checkpoint asserts that no Wave 1-4 surface sits in limbo: each is either
// settled or declared to be in flight. Migration requires the in-flight states,
// so treating them as limbo would contradict the design; accepting them
// silently would give up the property the checkpoint exists for. Declaring them
// keeps both.
func TestWave4CheckpointAccountsForEveryCatalogState(t *testing.T) {
	ledger := parseTestLedger(t)
	counts := make(map[AdmissionState]int)
	for _, entry := range ledger.Entries {
		counts[entry.State]++
		if expectedWave(entry.SurfaceKey) > 4 {
			continue
		}
		switch entry.State {
		case PolicyComplete, ShadowOnly, Admitted:
		default:
			if !stateIsInFlight(entry.State) {
				t.Fatalf("Wave 1-4 surface %s/%s remains %q, which is neither settled nor a migration waypoint",
					entry.Kind, entry.Name, entry.State)
			}
			if entry.Migration == "" {
				t.Fatalf("Wave 1-4 surface %s/%s is %q with no declaration of why it rests there",
					entry.Kind, entry.Name, entry.State)
			}
		}
	}
	want := map[AdmissionState]int{
		PolicyComplete:  11,
		GeneratedShadow: 54,
		ShadowOnly:      1,
		Admitted:        1,
	}
	if !reflect.DeepEqual(counts, want) {
		t.Fatalf("checkpoint state counts = %v, want %v", counts, want)
	}
	// The estate's one action. It was legacy_authoritative -- the only surface
	// in that state, and the last surface whose schema was hand-written with no
	// generated counterpart at all. It is now in flight like any other, which is
	// what James's override means by 67: not sixty-six and a special case.
	if err := ledger.Require(SurfaceKey{Kind: Action, Name: "unifi_port"},
		GeneratedShadow, AdapterParity, Admitted); err != nil {
		t.Fatal(err)
	}
}

func readStaticWaveReceipt(t *testing.T, path string) staticWaveReceipt {
	t.Helper()
	var receipt staticWaveReceipt
	decodeStrict(t, readFile(t, path), &receipt)
	return receipt
}

func requireStaticWaveDigests(t *testing.T, receipt staticWaveReceipt) {
	t.Helper()
	requireDigestMatches(t, receipt.SchemaSHA256, "../../provider-contracts/schema/terraform-1.15.8.json")
	requireDigestMatches(t, receipt.LedgerSHA256, "../../provider-codegen/generated/catalog-parity-ledger.json")
	requireDigestMatches(t, receipt.SurfaceContractsSHA256, "../../provider-codegen/generated/catalog-surface-contracts.json")
	requireDigestMatches(t, receipt.MigrationManifestSHA256, "../../provider-codegen/generated/catalog-migration-manifest.json")
}

func parseTestLedger(t *testing.T) Ledger {
	t.Helper()
	ledger, err := ParseLedger(readFile(t, "../../provider-codegen/generated/catalog-parity-ledger.json"))
	if err != nil {
		t.Fatal(err)
	}
	return ledger
}

func parseTestCorpus(t *testing.T) SurfaceContractCorpus {
	t.Helper()
	var corpus SurfaceContractCorpus
	if err := json.Unmarshal(readFile(t, "../../provider-codegen/generated/catalog-surface-contracts.json"), &corpus); err != nil {
		t.Fatal(err)
	}
	return corpus
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

// requireStatusCountsMatchLedger checks a wave receipt's status_counts against
// the ledger the receipt pins, rather than against a number written down beside
// it.
//
// The two are not the same check. A constant expectation answers "does this
// file still say what it said when someone wrote the test", which stays true
// through exactly the change that makes the receipt wrong: a surface moves
// state, the ledger digest is re-cut in the same commit, and the distribution
// the receipt claims quietly stops describing it. Wave 1 sat like that -- 37
// policy_complete against a ledger holding 24 generated_shadow -- and every
// gate stayed green, because nothing compared the claim to its subject.
func requireStatusCountsMatchLedger(t *testing.T, wave int, claimed map[string]int) {
	t.Helper()
	ledger := parseTestLedger(t)
	corpus := parseTestCorpus(t)

	inWave := map[SurfaceKey]bool{}
	for _, contract := range corpus.Contracts {
		if contract.Wave == wave {
			inWave[contract.SurfaceKey] = true
		}
	}
	if len(inWave) == 0 {
		t.Fatalf("wave %d has no surfaces in the corpus, so this check would pass on nothing", wave)
	}

	actual := map[string]int{}
	for _, entry := range ledger.Entries {
		if inWave[entry.SurfaceKey] {
			actual[string(entry.State)]++
		}
	}
	if !reflect.DeepEqual(claimed, actual) {
		t.Fatalf("wave %d receipt claims status counts %v, ledger holds %v\n\n"+
			"    The receipt pins that ledger by digest, so the two describe the same\n"+
			"    surfaces and must agree. Re-cut the receipt rather than adjusting this.",
			wave, claimed, actual)
	}
}
