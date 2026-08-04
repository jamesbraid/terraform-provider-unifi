package providercontracts

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ubiquiti-community/terraform-provider-unifi/internal/managementcontract"
)

type schemaToolchain struct {
	Version               string `json:"version"`
	BinarySHA256          string `json:"binary_sha256"`
	CanonicalSchema       string `json:"canonical_schema"`
	CanonicalSchemaSHA256 string `json:"canonical_schema_sha256"`
}

type dnsContract struct {
	FormatVersion int    `json:"format_version"`
	ContractID    string `json:"contract_id"`
	Mode          string `json:"mode"`
	Provider      struct {
		Address      string `json:"address"`
		Version      string `json:"version"`
		SourceCommit string `json:"source_commit"`
		Binary       struct {
			Platform string `json:"platform"`
			SHA256   string `json:"sha256"`
		} `json:"binary"`
		Schema struct {
			Toolchains map[string]schemaToolchain `json:"toolchains"`
		} `json:"schema"`
	} `json:"provider"`
	Catalog struct {
		SHA256 string `json:"sha256"`
	} `json:"catalog"`
	Operation struct {
		ArtifactSHA256 string `json:"artifact_sha256"`
		Digest         string `json:"digest"`
	} `json:"operation"`
	Lifecycle struct {
		Receipt       string `json:"receipt"`
		ReceiptSHA256 string `json:"receipt_sha256"`
		Result        string `json:"result"`
	} `json:"lifecycle"`
	Provenance struct {
		PolicySHA256           string `json:"policy_sha256"`
		ProviderCodeSpecSHA256 string `json:"provider_code_spec_sha256"`
		MappingSHA256          string `json:"mapping_sha256"`
	} `json:"provenance"`
	Trust struct {
		Kind   string `json:"kind"`
		Signed bool   `json:"signed"`
	} `json:"trust"`
}

type lifecycleReceipt struct {
	Result               string `json:"result"`
	SourceCommit         string `json:"source_commit"`
	ProviderBinarySHA256 string `json:"provider_binary_sha256"`
}

type baseline struct {
	Provider struct {
		Address  string `json:"address"`
		Version  string `json:"version"`
		Platform string `json:"platform"`
	} `json:"provider"`
	Clients struct {
		Terraform struct {
			Version               string `json:"version"`
			BinarySHA256          string `json:"binary_sha256"`
			CanonicalSchemaSHA256 string `json:"canonical_schema_sha256"`
		} `json:"terraform"`
		OpenTofu struct {
			Version               string `json:"version"`
			BinarySHA256          string `json:"binary_sha256"`
			CanonicalSchemaSHA256 string `json:"canonical_schema_sha256"`
		} `json:"opentofu"`
	} `json:"clients"`
}

func readJSON(t *testing.T, path string, target any) {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(content, target); err != nil {
		t.Fatal(err)
	}
}

func fileSHA256(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(content)
	return hex.EncodeToString(digest[:])
}

func requireEqual(t *testing.T, label, expected, actual string) {
	t.Helper()
	if actual != expected {
		t.Fatalf("%s mismatch: expected=%q actual=%q", label, expected, actual)
	}
}

func TestDNSContractEvidenceIsInternallyBound(t *testing.T) {
	var contract dnsContract
	readJSON(t, "unifi_dns_record.v1.json", &contract)
	if contract.FormatVersion != 1 || contract.ContractID == "" {
		t.Fatal("contract identity is incomplete")
	}
	requireEqual(t, "contract mode", "provider_projection_required", contract.Mode)

	checksum, err := os.ReadFile("unifi_dns_record.v1.sha256")
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Fields(string(checksum))
	if len(parts) != 2 {
		t.Fatalf("invalid sidecar checksum: %q", checksum)
	}
	requireEqual(t, "sidecar checksum filename", "unifi_dns_record.v1.json", parts[1])
	requireEqual(t, "sidecar checksum", parts[0], fileSHA256(t, parts[1]))

	var build baseline
	readJSON(t, filepath.Join("..", "build", "m0", "provider-baseline.json"), &build)
	requireEqual(t, "provider address", build.Provider.Address, contract.Provider.Address)
	requireEqual(t, "provider version", build.Provider.Version, contract.Provider.Version)
	requireEqual(t, "provider platform", build.Provider.Platform, contract.Provider.Binary.Platform)

	terraform := contract.Provider.Schema.Toolchains["terraform"]
	requireEqual(t, "Terraform version", build.Clients.Terraform.Version, terraform.Version)
	requireEqual(t, "Terraform binary", build.Clients.Terraform.BinarySHA256, terraform.BinarySHA256)
	requireEqual(t, "Terraform schema", build.Clients.Terraform.CanonicalSchemaSHA256, terraform.CanonicalSchemaSHA256)
	requireEqual(t, "Terraform schema file", terraform.CanonicalSchemaSHA256, fileSHA256(t, terraform.CanonicalSchema))

	tofu := contract.Provider.Schema.Toolchains["tofu"]
	requireEqual(t, "OpenTofu version", build.Clients.OpenTofu.Version, tofu.Version)
	requireEqual(t, "OpenTofu binary", build.Clients.OpenTofu.BinarySHA256, tofu.BinarySHA256)
	requireEqual(t, "OpenTofu schema", build.Clients.OpenTofu.CanonicalSchemaSHA256, tofu.CanonicalSchemaSHA256)
	requireEqual(t, "OpenTofu schema file", tofu.CanonicalSchemaSHA256, fileSHA256(t, tofu.CanonicalSchema))

	requireEqual(t, "catalog", contract.Catalog.SHA256, fileSHA256(t, filepath.Join("..", "provider-codegen", "catalog", "go-unifi-v1.102.0-dns-record.catalog.json")))
	requireEqual(t, "policy", contract.Provenance.PolicySHA256, fileSHA256(t, filepath.Join("..", "provider-codegen", "policy", "dns_record.json")))
	requireEqual(t, "Provider Code Specification", contract.Provenance.ProviderCodeSpecSHA256, fileSHA256(t, filepath.Join("..", "provider-codegen", "generated", "dns_record.provider-code-spec.json")))
	requireEqual(t, "mapping", contract.Provenance.MappingSHA256, fileSHA256(t, filepath.Join("..", "provider-codegen", "generated", "dns_record.mapping.json")))

	receiptPath := filepath.Join("evidence", filepath.Base(contract.Lifecycle.Receipt))
	requireEqual(t, "lifecycle receipt", contract.Lifecycle.ReceiptSHA256, fileSHA256(t, receiptPath))
	var receipt lifecycleReceipt
	readJSON(t, receiptPath, &receipt)
	requireEqual(t, "lifecycle result", "pass", receipt.Result)
	requireEqual(t, "contract lifecycle result", receipt.Result, contract.Lifecycle.Result)
	requireEqual(t, "provider binary", receipt.ProviderBinarySHA256, contract.Provider.Binary.SHA256)

	boundContract := managementcontract.Contract{FormatVersion: contract.FormatVersion}
	boundContract.Provider.SourceCommit = contract.Provider.SourceCommit
	boundContract.Provider.Binary.SHA256 = contract.Provider.Binary.SHA256
	boundContract.Provider.Schema.Toolchains = map[string]managementcontract.SchemaToolchain{
		"terraform": {
			Version:               terraform.Version,
			BinarySHA256:          terraform.BinarySHA256,
			CanonicalSchemaSHA256: terraform.CanonicalSchemaSHA256,
		},
		"tofu": {
			Version:               tofu.Version,
			BinarySHA256:          tofu.BinarySHA256,
			CanonicalSchemaSHA256: tofu.CanonicalSchemaSHA256,
		},
	}
	boundContract.Catalog.SHA256 = contract.Catalog.SHA256
	boundContract.Operation.ArtifactSHA256 = contract.Operation.ArtifactSHA256
	boundContract.Operation.Digest = contract.Operation.Digest
	boundContract.Lifecycle.ReceiptSHA256 = contract.Lifecycle.ReceiptSHA256
	boundContract.Lifecycle.Result = contract.Lifecycle.Result
	boundContract.Provenance.MappingSHA256 = contract.Provenance.MappingSHA256
	boundEvidence := managementcontract.Evidence{
		ProviderBinarySHA256:          receipt.ProviderBinarySHA256,
		CatalogSHA256:                 fileSHA256(t, filepath.Join("..", "provider-codegen", "catalog", "go-unifi-v1.102.0-dns-record.catalog.json")),
		OperationArtifactSHA256:       fileSHA256(t, filepath.Join("..", "provider-codegen", "catalog", "go-unifi-62add0c-dns-record.operation.json")),
		OperationDigest:               "199c002d9a1229aa7e43a94b12da6f33c7ce612ecb471eebcd6896d985b161f8",
		LifecycleReceiptSHA256:        fileSHA256(t, receiptPath),
		LifecycleProviderBinarySHA256: receipt.ProviderBinarySHA256,
		MappingSHA256:                 fileSHA256(t, filepath.Join("..", "provider-codegen", "generated", "dns_record.mapping.json")),
		SourceCommit:                  receipt.SourceCommit,
		SchemaToolchains:              boundContract.Provider.Schema.Toolchains,
	}
	if err := managementcontract.Verify(boundContract, boundEvidence); err != nil {
		t.Fatalf("management contract evidence is not bound: %v", err)
	}

	requireEqual(t, "development trust", "operator-pinned-checksum", contract.Trust.Kind)
	if contract.Trust.Signed {
		t.Fatal("development sidecar unexpectedly claims a signature")
	}
}
