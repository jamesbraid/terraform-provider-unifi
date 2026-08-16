package catalogparity

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestBuildSurfaceContractsAccountsForEveryReleasedSurface(t *testing.T) {
	baseline := releasedBaseline(t)
	versions := releasedSchemaVersions(t, baseline)
	policy := releasedWavePolicy(t)

	corpus, err := BuildSurfaceContracts(baseline, versions, policy)
	if err != nil {
		t.Fatal(err)
	}
	if len(corpus.Contracts) != 67 {
		t.Fatalf("contracts = %d, want 67", len(corpus.Contracts))
	}
	wantWaves := map[int]int{1: 38, 2: 8, 3: 9, 4: 11, 5: 1}
	if got := corpus.WaveCounts(); !reflect.DeepEqual(got, wantWaves) {
		t.Fatalf("WaveCounts() = %v, want %v", got, wantWaves)
	}
	for _, contract := range corpus.Contracts {
		if contract.BaselineSchemaSHA256 == "" || len(contract.RequiredDimensions) == 0 || len(contract.EvidenceGates) == 0 {
			t.Fatalf("incomplete contract for %s/%s: %+v", contract.Kind, contract.Name, contract)
		}
		if contract.Kind != Action && contract.SchemaVersion == nil {
			t.Fatalf("schema version missing for %s/%s", contract.Kind, contract.Name)
		}
		if contract.Wave != expectedWave(contract.SurfaceKey) {
			t.Fatalf("wave for %s/%s = %d, want %d", contract.Kind, contract.Name, contract.Wave, expectedWave(contract.SurfaceKey))
		}
	}
}

func TestBuildSurfaceContractsSetsKindSpecificEvidenceGates(t *testing.T) {
	baseline := releasedBaseline(t)
	corpus, err := BuildSurfaceContracts(baseline, releasedSchemaVersions(t, baseline), releasedWavePolicy(t))
	if err != nil {
		t.Fatal(err)
	}

	assertGate := func(key SurfaceKey, gate string) {
		t.Helper()
		contract, ok := corpus.Contract(key)
		if !ok {
			t.Fatalf("missing contract for %s/%s", key.Kind, key.Name)
		}
		for _, candidate := range contract.EvidenceGates {
			if candidate == gate {
				return
			}
		}
		t.Fatalf("contract %s/%s gates = %v, want %q", key.Kind, key.Name, contract.EvidenceGates, gate)
	}
	assertGate(SurfaceKey{Kind: ListResource, Name: "unifi_firewall_zone"}, "pagination_filter")
	assertGate(SurfaceKey{Kind: ManagedResource, Name: "unifi_network"}, "recovery")
	assertGate(SurfaceKey{Kind: Action, Name: "unifi_port"}, "hardware_claim")
}

func TestBuildSurfaceContractsRejectsIncompleteOrIncorrectPolicy(t *testing.T) {
	baseline := releasedBaseline(t)
	versions := releasedSchemaVersions(t, baseline)
	valid := releasedWavePolicy(t)

	tests := map[string]struct {
		mutate func(*WavePolicy)
		want   string
	}{
		"missing": {
			mutate: func(policy *WavePolicy) { policy.Waves[0].DataSources = policy.Waves[0].DataSources[1:] },
			want:   "missing",
		},
		"duplicate": {
			mutate: func(policy *WavePolicy) {
				policy.Waves[0].DataSources = append(policy.Waves[0].DataSources, policy.Waves[0].DataSources[0])
			},
			want: "duplicate",
		},
		"unknown": {
			mutate: func(policy *WavePolicy) { policy.Waves[0].DataSources[0] = "unifi_missing" },
			want:   "unknown",
		},
		"wrong wave": {
			mutate: func(policy *WavePolicy) { policy.Waves[1].ManagedResources[0] = "unifi_account" },
			want:   "assigned to wave",
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			policy := cloneWavePolicy(t, valid)
			test.mutate(&policy)
			if _, err := BuildSurfaceContracts(baseline, versions, policy); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("BuildSurfaceContracts() error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestParseWavePolicyIsStrict(t *testing.T) {
	data, err := os.ReadFile("../../provider-codegen/policy/catalog.json")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ParseWavePolicy(data); err != nil {
		t.Fatal(err)
	}
	for _, malformed := range [][]byte{
		append(append([]byte(nil), data...), []byte("{}")...),
		[]byte(`{"format_version":1,"provider_address":"registry.terraform.io/ubiquiti-community/unifi","waves":[],"extra":true}`),
	} {
		if _, err := ParseWavePolicy(malformed); err == nil {
			t.Fatal("ParseWavePolicy() accepted malformed input")
		}
	}
}

func releasedSchemaVersions(t *testing.T, baseline Baseline) SchemaVersions {
	t.Helper()
	data, err := os.ReadFile("../../provider-contracts/schema/terraform-1.15.8.json")
	if err != nil {
		t.Fatal(err)
	}
	versions, err := ParseSchemaVersions(data, baseline)
	if err != nil {
		t.Fatal(err)
	}
	return versions
}

func releasedWavePolicy(t *testing.T) WavePolicy {
	t.Helper()
	data, err := os.ReadFile("../../provider-codegen/policy/catalog.json")
	if err != nil {
		t.Fatal(err)
	}
	policy, err := ParseWavePolicy(data)
	if err != nil {
		t.Fatal(err)
	}
	return policy
}

func cloneWavePolicy(t *testing.T, policy WavePolicy) WavePolicy {
	t.Helper()
	data, err := json.Marshal(policy)
	if err != nil {
		t.Fatal(err)
	}
	var clone WavePolicy
	if err := json.Unmarshal(data, &clone); err != nil {
		t.Fatal(err)
	}
	return clone
}
