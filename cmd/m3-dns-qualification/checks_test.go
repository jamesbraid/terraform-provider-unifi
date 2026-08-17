package main

import (
	"bytes"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/ubiquiti-community/terraform-provider-unifi/internal/releasequalification"
)

func stateWith(resources string) []byte {
	return []byte(`{"values":{"root_module":{"resources":[` + resources + `]}}}`)
}

const recordResource = `{"address":"unifi_dns_record.test","schema_version":1,"values":` +
	`{"id":"abc123","ttl":"5m0s","name":"m0-dns.example.invalid","enabled":false,"record_type":"A"}}`

// TestNormalizeRecordStateDropsIDAndSortsKeys pins the two transformations the
// comparison depends on: the run-specific id must not take part, and key order
// must not depend on how the CLI happened to serialise the state.
func TestNormalizeRecordStateDropsIDAndSortsKeys(t *testing.T) {
	normalized, err := normalizeRecordState(stateWith(recordResource))
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}

	if bytes.Contains(normalized, []byte("abc123")) {
		t.Errorf("normalized state still carries the controller-assigned id:\n%s", normalized)
	}
	want := `{
  "enabled": false,
  "name": "m0-dns.example.invalid",
  "record_type": "A",
  "ttl": "5m0s"
}
`
	if string(normalized) != want {
		t.Errorf("normalized state =\n%s\nwant\n%s", normalized, want)
	}
}

// TestNormalizeRecordStateRefusesAnAbsentRecord is the regression this port
// exists to add, and it is the one behavioural difference from the shell.
//
// jq printed NOTHING when the select matched nothing, so the shell wrote an
// empty file. All four passes would write empty files, all three cmp calls
// would compare equal, and bidirectional_adapter_state_round_trip would be
// recorded true. sha256 of an empty file is still 64 hex characters, so the
// downstream validHex check accepted the digest too. A run that lost the
// resource everywhere was indistinguishable from a run where all four agreed.
func TestNormalizeRecordStateRefusesAnAbsentRecord(t *testing.T) {
	_, err := normalizeRecordState(stateWith(`{"address":"unifi_network.other","values":{"id":"x"}}`))
	if err == nil {
		t.Fatal("normalizing a state without the record succeeded; an empty normalization makes " +
			"the round-trip comparison pass vacuously, which is the defect this check exists for")
	}
	if !strings.Contains(err.Error(), "vacuously") {
		t.Errorf("error should say why an empty result is unacceptable, got: %v", err)
	}
}

func TestNormalizeRecordStateRefusesDuplicates(t *testing.T) {
	_, err := normalizeRecordState(stateWith(recordResource + "," + recordResource))
	if err == nil {
		t.Fatal("two resources at the same address should not normalize to one")
	}
}

// TestNormalizeRecordStateRefusesAnIDOnlyRecord closes the same hole one level
// in: a record whose only attribute is the id normalizes to {} , and empty
// objects compare equal just as reliably as empty files.
func TestNormalizeRecordStateRefusesAnIDOnlyRecord(t *testing.T) {
	_, err := normalizeRecordState(stateWith(`{"address":"unifi_dns_record.test","values":{"id":"abc"}}`))
	if err == nil {
		t.Fatal("a record with no attributes besides id should not be comparable")
	}
}

// TestNormalizeRecordStateKeepsNumericLiterals guards the digest: routing
// numbers through float64 would rewrite 300 as 300 today and something else
// the first time a large or fractional value appears, silently changing
// normalized_state_sha256 without any state having changed.
func TestNormalizeRecordStateKeepsNumericLiterals(t *testing.T) {
	normalized, err := normalizeRecordState(stateWith(
		`{"address":"unifi_dns_record.test","values":{"id":"x","ttl":300,"weight":10000000000000001}}`))
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	for _, literal := range []string{"300", "10000000000000001"} {
		if !bytes.Contains(normalized, []byte(literal)) {
			t.Errorf("normalized state lost the literal %s:\n%s", literal, normalized)
		}
	}
}

func TestAssertReplacementPlan(t *testing.T) {
	for _, testCase := range []struct {
		name    string
		actions string
		wantErr bool
	}{
		{"create and delete", `["create","delete"]`, false},
		{"delete then create", `["delete","create"]`, false},
		{"update in place", `["update"]`, true},
		{"create only", `["create"]`, true},
		{"no-op", `["no-op"]`, true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			plan := []byte(`{"resource_changes":[{"address":"unifi_dns_record.test",` +
				`"change":{"actions":` + testCase.actions + `}}]}`)
			err := assertReplacementPlan(plan)
			if testCase.wantErr && err == nil {
				t.Errorf("actions %s accepted as a replacement", testCase.actions)
			}
			if !testCase.wantErr && err != nil {
				t.Errorf("actions %s rejected: %v", testCase.actions, err)
			}
		})
	}
}

func TestAssertReplacementPlanRequiresTheRecord(t *testing.T) {
	plan := []byte(`{"resource_changes":[{"address":"unifi_network.other",` +
		`"change":{"actions":["create","delete"]}}]}`)
	if err := assertReplacementPlan(plan); err == nil {
		t.Fatal("a plan that replaces some other resource is not a replacement of the record")
	}
}

// TestAssertUpgradeStateSeparatesIntegerFromString is the whole point of the
// state-upgrade gate: v0 stored ttl as the integer 300 and v1 stores the string
// "5m0s". A check that compared them after conversion to text would accept 300
// where "300" was meant, and would not notice the upgrade failing to run.
func TestAssertUpgradeStateSeparatesIntegerFromString(t *testing.T) {
	v0 := stateWith(`{"address":"unifi_dns_record.test","schema_version":0,"values":{"ttl":300}}`)
	v1 := stateWith(`{"address":"unifi_dns_record.test","schema_version":1,"values":{"ttl":"5m0s"}}`)

	if err := assertUpgradeState(v0, 0, "300"); err != nil {
		t.Errorf("v0 state rejected: %v", err)
	}
	if err := assertUpgradeState(v1, 1, `"5m0s"`); err != nil {
		t.Errorf("v1 state rejected: %v", err)
	}

	stringified := stateWith(`{"address":"unifi_dns_record.test","schema_version":0,"values":{"ttl":"300"}}`)
	if err := assertUpgradeState(stringified, 0, "300"); err == nil {
		t.Error(`ttl "300" accepted where the integer 300 was required`)
	}
	if err := assertUpgradeState(v0, 1, "300"); err == nil {
		t.Error("schema_version 0 accepted where 1 was required, so a skipped upgrade would pass")
	}
	if err := assertUpgradeState(stateWith(""), 0, "300"); err == nil {
		t.Error("a state with no resources should not satisfy the upgrade assertion")
	}
}

// TestAssertUpgradeStateRequiresAnExplicitSchemaVersion keeps an absent field
// from reading as zero -- which is exactly the value the v0 assertion wants.
func TestAssertUpgradeStateRequiresAnExplicitSchemaVersion(t *testing.T) {
	missing := stateWith(`{"address":"unifi_dns_record.test","values":{"ttl":300}}`)
	if err := assertUpgradeState(missing, 0, "300"); err == nil {
		t.Fatal("a missing schema_version satisfied the schema_version == 0 assertion")
	}
}

// TestInstallPrebuiltCandidate replaces m3-evidence-lib_test.sh, which
// .woodpecker/catalog-controller-differential.yml invoked directly.
func TestInstallPrebuiltCandidate(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "candidate")
	if err := os.WriteFile(source, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	destination := filepath.Join(root, "tools", "provider", "terraform-provider-unifi_v0.101.2")
	if err := installPrebuiltCandidate(source, destination); err != nil {
		t.Fatalf("install: %v", err)
	}
	info, err := os.Stat(destination)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o111 == 0 {
		t.Errorf("installed candidate is not executable (mode %v)", info.Mode().Perm())
	}
	installed, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	if string(installed) != "#!/bin/sh\nexit 0\n" {
		t.Errorf("installed contents = %q", installed)
	}
}

func TestInstallPrebuiltCandidateRefusesBadSources(t *testing.T) {
	root := t.TempDir()

	if err := installPrebuiltCandidate(filepath.Join(root, "missing"),
		filepath.Join(root, "out", "provider")); err == nil {
		t.Error("a missing candidate binary was accepted")
	}

	notExecutable := filepath.Join(root, "plain")
	if err := os.WriteFile(notExecutable, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := installPrebuiltCandidate(notExecutable, filepath.Join(root, "out", "provider")); err == nil {
		t.Error("a non-executable candidate binary was accepted")
	}
}

// TestVerifyChecksCompleteRefusesAnUnrunCheck is the guard against the shape
// the shell had: thirteen literal `true`s in a jq object, true because the
// script reached its last line rather than because the work happened. Here a
// check that never ran blocks the receipt and is named.
func TestVerifyChecksCompleteRefusesAnUnrunCheck(t *testing.T) {
	q := &qualification{}
	err := q.verifyChecksComplete()
	if err == nil {
		t.Fatal("a receipt was allowed with every lifecycle check unset")
	}
	for _, name := range []string{"create", "import", "bidirectional_adapter_state_round_trip"} {
		if !strings.Contains(err.Error(), name) {
			t.Errorf("error does not name the unrun check %q: %v", name, err)
		}
	}

	q.checks = releasequalification.DNSLifecycleChecks{
		FreshTargetPerCLIAndAdapter: true, Create: true, Update: true,
		OmittedOptionalFields: true, ConfiguredOptionalFields: true,
		ReplacementPlan: true, RestartRefresh: true, Import: true,
		V0IntegerTTLStateUpgrade: true, NoOpPlan: true, Delete: true,
		Cleanup: true, BidirectionalAdapterStateRoundTrip: true,
	}
	if err := q.verifyChecksComplete(); err != nil {
		t.Fatalf("a fully populated check set was rejected: %v", err)
	}

	// One at a time: every field must be load-bearing, or the guard has a hole
	// exactly where a future edit will put the missing step.
	full := q.checks
	for _, clear := range []func(*releasequalification.DNSLifecycleChecks){
		func(c *releasequalification.DNSLifecycleChecks) { c.FreshTargetPerCLIAndAdapter = false },
		func(c *releasequalification.DNSLifecycleChecks) { c.Create = false },
		func(c *releasequalification.DNSLifecycleChecks) { c.Update = false },
		func(c *releasequalification.DNSLifecycleChecks) { c.OmittedOptionalFields = false },
		func(c *releasequalification.DNSLifecycleChecks) { c.ConfiguredOptionalFields = false },
		func(c *releasequalification.DNSLifecycleChecks) { c.ReplacementPlan = false },
		func(c *releasequalification.DNSLifecycleChecks) { c.RestartRefresh = false },
		func(c *releasequalification.DNSLifecycleChecks) { c.Import = false },
		func(c *releasequalification.DNSLifecycleChecks) { c.V0IntegerTTLStateUpgrade = false },
		func(c *releasequalification.DNSLifecycleChecks) { c.NoOpPlan = false },
		func(c *releasequalification.DNSLifecycleChecks) { c.Delete = false },
		func(c *releasequalification.DNSLifecycleChecks) { c.Cleanup = false },
		func(c *releasequalification.DNSLifecycleChecks) { c.BidirectionalAdapterStateRoundTrip = false },
	} {
		q.checks = full
		clear(&q.checks)
		if err := q.verifyChecksComplete(); err == nil {
			t.Errorf("clearing one check still produced a receipt: %+v", q.checks)
		}
	}
}

// TestRenderCanonicalSortsKeys pins the property that makes the artifact
// independent of Go struct layout.
//
// encoding/json emits struct fields in declaration order, so without the
// generic round-trip a field moved for readability would rewrite a release
// artifact that other gates compare byte for byte. Sorting is also what lets
// this receipt be compared against a shell-produced one through a single
// canonical writer rather than by matching jq's insertion order.
func TestRenderCanonicalSortsKeys(t *testing.T) {
	rendered, err := renderCanonical(releasequalification.DNSLifecycleReceipt{
		FormatVersion:   1,
		Gate:            gateName,
		Result:          "pass",
		SourceCommit:    "abc",
		Platform:        platform,
		ProviderVersion: providerVersion,
	})
	if err != nil {
		t.Fatalf("render: %v", err)
	}

	var keys []string
	for _, line := range strings.Split(string(rendered), "\n") {
		if !strings.HasPrefix(line, `  "`) {
			continue
		}
		keys = append(keys, strings.SplitN(strings.TrimPrefix(line, `  "`), `"`, 2)[0])
	}
	if len(keys) == 0 {
		t.Fatal("no top-level keys parsed; the helper is broken, not the receipt")
	}
	if !sort.StringsAreSorted(keys) {
		t.Errorf("top-level keys are not sorted, so the artifact depends on struct order:\n%v", keys)
	}
	// The declaration order starts format_version, gate, ...; sorted order must
	// not. Without this the test would pass for a struct that happened to be
	// declared alphabetically and prove nothing about the sorting.
	if keys[0] == "format_version" {
		t.Errorf("first key is %q, which is also the first declared field -- "+
			"the round-trip is not reordering anything", keys[0])
	}
	if !strings.HasSuffix(string(rendered), "\n") {
		t.Error("rendered receipt does not end with a newline")
	}
}

// TestRunRequiresTreeState pins the flag that dissolves this tool's entry in
// tree-state-coverage_test.sh. Giving it a default would restore the exemption
// without anyone editing the list.
func TestRunRequiresTreeState(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := run([]string{"-run-id", "1", "-output", filepath.Join(t.TempDir(), "receipt.json")},
		&stdout, &stderr)
	if err == nil {
		t.Fatal("the qualification ran without a tree state")
	}
	if !strings.Contains(err.Error(), "tree state is required") {
		t.Errorf("error = %v, want the ParseTreeState refusal", err)
	}
}

func TestRunRequiresRunID(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := run([]string{
		"-tree-state", `{"status":"clean","commit":"0123456789012345678901234567890123456789","dirty_paths":[]}`,
		"-output", filepath.Join(t.TempDir(), "receipt.json"),
	}, &stdout, &stderr)
	if err == nil {
		t.Fatal("the qualification ran without a run id, so its Docker resources would be unscoped")
	}
	if !strings.Contains(err.Error(), "-run-id is required") {
		t.Errorf("error = %v, want the run-id refusal", err)
	}
}
