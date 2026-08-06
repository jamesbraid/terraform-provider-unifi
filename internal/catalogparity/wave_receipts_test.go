package catalogparity

import (
	"encoding/json"
	"reflect"
	"testing"
)

type staticWaveReceipt struct {
	FormatVersion           int            `json:"format_version"`
	Wave                    int            `json:"wave"`
	Result                  string         `json:"result"`
	Promotion               string         `json:"promotion"`
	RuntimeChanged          bool           `json:"runtime_changed"`
	ControllerExecution     string         `json:"controller_execution"`
	SurfaceCount            int            `json:"surface_count"`
	SurfaceCounts           map[string]int `json:"surface_counts"`
	StatusCounts            map[string]int `json:"status_counts"`
	BlockerCounts           map[string]int `json:"blocker_counts"`
	SchemaSHA256            string         `json:"schema_sha256"`
	LedgerSHA256            string         `json:"ledger_sha256"`
	SurfaceContractsSHA256  string         `json:"surface_contracts_sha256"`
	MigrationManifestSHA256 string         `json:"migration_manifest_sha256"`
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
	if !reflect.DeepEqual(receipt.StatusCounts, map[string]int{"policy_complete": 37, "shadow_only": 1}) {
		t.Fatalf("Wave 1 status counts = %v", receipt.StatusCounts)
	}
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
		want := PolicyComplete
		if contract.Kind == ListResource && contract.Name == "unifi_dns_record" {
			want = ShadowOnly
		}
		if err := ledger.Require(contract.SurfaceKey, want); err != nil {
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

func readStaticWaveReceipt(t *testing.T, path string) staticWaveReceipt {
	t.Helper()
	var receipt staticWaveReceipt
	decodeStrict(t, readFile(t, path), &receipt)
	return receipt
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
