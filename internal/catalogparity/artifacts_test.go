package catalogparity

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

type wave0Receipt struct {
	FormatVersion           int            `json:"format_version"`
	Wave                    int            `json:"wave"`
	Result                  string         `json:"result"`
	RuntimeChanged          bool           `json:"runtime_changed"`
	SurfaceCounts           map[string]int `json:"surface_counts"`
	UnmanifestedSurfaces    int            `json:"unmanifested_surfaces"`
	BaselineSHA256          string         `json:"baseline_sha256"`
	LedgerSHA256            string         `json:"ledger_sha256"`
	MigrationManifestSHA256 string         `json:"migration_manifest_sha256"`
	MigrationReportSHA256   string         `json:"migration_report_sha256"`
	CompilerOutputs         struct {
		ProviderCodeSpecSHA256 string `json:"provider_code_spec_sha256"`
		ImpactSHA256           string `json:"impact_sha256"`
		MappingSHA256          string `json:"mapping_sha256"`
		GeneratedGoSHA256      string `json:"generated_go_sha256"`
	} `json:"compiler_outputs"`
	MigrationStrategyCounts map[string]int `json:"migration_strategy_counts"`
	MigrationRiskCounts     map[string]int `json:"migration_risk_counts"`
	DestructiveMigrations   int            `json:"destructive_migrations"`
}

func TestWave0ReceiptBindsCatalogArtifacts(t *testing.T) {
	receipt := readWave0Receipt(t)
	if receipt.FormatVersion != 1 || receipt.Wave != 0 || receipt.Result != "pass" {
		t.Fatalf("unexpected Wave 0 identity: %+v", receipt)
	}
	if receipt.RuntimeChanged {
		t.Fatal("Wave 0 receipt claims a runtime change")
	}
	requireDigestMatches(t, receipt.BaselineSHA256, "../../build/m0/provider-schema-digests.json")
	requireDigestMatches(t, receipt.LedgerSHA256, "../../provider-codegen/generated/catalog-parity-ledger.json")
	requireDigestMatches(t, receipt.MigrationManifestSHA256, "../../provider-codegen/generated/catalog-migration-manifest.json")
	requireDigestMatches(t, receipt.MigrationReportSHA256, "../../provider-codegen/generated/catalog-migration-report.json")
	requireDigestMatches(t, receipt.CompilerOutputs.ProviderCodeSpecSHA256, "../../provider-codegen/generated/dns_record.provider-code-spec.json")
	requireDigestMatches(t, receipt.CompilerOutputs.ImpactSHA256, "../../provider-codegen/generated/dns_record.impact.json")
	requireDigestMatches(t, receipt.CompilerOutputs.MappingSHA256, "../../provider-codegen/generated/dns_record.mapping.json")
	requireDigestMatches(t, receipt.CompilerOutputs.GeneratedGoSHA256, "../../internal/generated/resource_dns_record/dns_record_resource_gen.go")
}

func TestWave0ReceiptAccountsForCompleteCatalogAndMigration(t *testing.T) {
	receipt := readWave0Receipt(t)
	wantCounts := map[string]int{
		string(ManagedResource): 28,
		string(DataSource):      13,
		string(ListResource):    25,
		string(Action):          1,
	}
	if !mapsEqual(receipt.SurfaceCounts, wantCounts) {
		t.Fatalf("surface counts = %v, want %v", receipt.SurfaceCounts, wantCounts)
	}
	if receipt.UnmanifestedSurfaces != 0 {
		t.Fatalf("unmanifested surfaces = %d, want 0", receipt.UnmanifestedSurfaces)
	}
	if receipt.MigrationStrategyCounts["identity"] != 67 || len(receipt.MigrationStrategyCounts) != 1 {
		t.Fatalf("migration strategy counts = %v, want identity:67", receipt.MigrationStrategyCounts)
	}
	if receipt.MigrationRiskCounts["none"] != 67 || len(receipt.MigrationRiskCounts) != 1 {
		t.Fatalf("migration risk counts = %v, want none:67", receipt.MigrationRiskCounts)
	}
	if receipt.DestructiveMigrations != 0 {
		t.Fatalf("destructive migrations = %d, want 0", receipt.DestructiveMigrations)
	}

	ledgerData := readFile(t, "../../provider-codegen/generated/catalog-parity-ledger.json")
	ledger, err := ParseLedger(ledgerData)
	if err != nil {
		t.Fatal(err)
	}
	var manifest MigrationManifest
	decodeStrict(t, readFile(t, "../../provider-codegen/generated/catalog-migration-manifest.json"), &manifest)
	var report MigrationReport
	decodeStrict(t, readFile(t, "../../provider-codegen/generated/catalog-migration-report.json"), &report)
	if len(ledger.Entries) != 67 || len(manifest.Entries) != 67 || report.SurfaceCount != 67 {
		t.Fatalf("artifact surface counts = ledger:%d manifest:%d report:%d", len(ledger.Entries), len(manifest.Entries), report.SurfaceCount)
	}
	for _, entry := range manifest.Entries {
		if entry.Strategy != IdentityTransform || entry.DestructiveRisk != RiskNone {
			t.Fatalf("surface %s/%s migration is %q risk %q", entry.Kind, entry.Name, entry.Strategy, entry.DestructiveRisk)
		}
	}
}

func TestWave0PublicArtifactsContainNoPrivateLocators(t *testing.T) {
	paths := []string{
		"../../provider-codegen/generated/catalog-parity-ledger.json",
		"../../provider-codegen/generated/catalog-migration-manifest.json",
		"../../provider-codegen/generated/catalog-migration-report.json",
		"../../build/wave0/catalog-parity.json",
	}
	absolutePath := regexp.MustCompile(`(?i)(/users/|/home/|[a-z]:\\)`)
	for _, path := range paths {
		data := readFile(t, path)
		lower := strings.ToLower(string(data))
		if absolutePath.Match(data) || strings.Contains(lower, "file://") || strings.Contains(lower, ".internal") || strings.Contains(lower, ".local") {
			t.Fatalf("public artifact %s contains a private locator", filepath.Base(path))
		}
		for _, token := range bytes.FieldsFunc(data, func(r rune) bool {
			return r == '"' || r == '\'' || r == ',' || r == ':' || r == '[' || r == ']' || r == '{' || r == '}' || r == ' ' || r == '\n'
		}) {
			ip := net.ParseIP(string(token))
			if ip != nil && (ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast()) {
				t.Fatalf("public artifact %s contains a non-public IP address", filepath.Base(path))
			}
		}
	}
}

func readWave0Receipt(t *testing.T) wave0Receipt {
	t.Helper()
	var receipt wave0Receipt
	decodeStrict(t, readFile(t, "../../build/wave0/catalog-parity.json"), &receipt)
	return receipt
}

func requireDigestMatches(t *testing.T, want, path string) {
	t.Helper()
	data := readFile(t, path)
	got := sha256.Sum256(data)
	if hex.EncodeToString(got[:]) != want {
		t.Fatalf("digest mismatch for %s", path)
	}
}

func readFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func decodeStrict(t *testing.T, data []byte, target any) {
	t.Helper()
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		t.Fatal(err)
	}
}

func mapsEqual(left, right map[string]int) bool {
	if len(left) != len(right) {
		return false
	}
	for key, value := range left {
		if right[key] != value {
			return false
		}
	}
	return true
}
