// Package managementcontract verifies that a provider management sidecar is
// bound to the evidence a consumer measured before controller access.
package managementcontract

import (
	"encoding/hex"
	"fmt"
)

type Contract struct {
	FormatVersion int        `json:"format_version"`
	Provider      Provider   `json:"provider"`
	Catalog       Catalog    `json:"catalog"`
	Operation     Operation  `json:"operation"`
	Lifecycle     Lifecycle  `json:"lifecycle"`
	Provenance    Provenance `json:"provenance"`
}

type Provider struct {
	SourceCommit string `json:"source_commit"`
	Binary       Binary `json:"binary"`
	Schema       Schema `json:"schema"`
}

type Binary struct {
	SHA256 string `json:"sha256"`
}

type Schema struct {
	Toolchains map[string]SchemaToolchain `json:"toolchains"`
}

type SchemaToolchain struct {
	Version               string `json:"version"`
	BinarySHA256          string `json:"binary_sha256"`
	CanonicalSchemaSHA256 string `json:"canonical_schema_sha256"`
}

type Catalog struct {
	SHA256 string `json:"sha256"`
}

type Operation struct {
	ArtifactSHA256 string `json:"artifact_sha256"`
	Digest         string `json:"digest"`
}

type Lifecycle struct {
	ReceiptSHA256 string `json:"receipt_sha256"`
	Result        string `json:"result"`
}

type Provenance struct {
	MappingSHA256 string `json:"mapping_sha256"`
}

type Evidence struct {
	ProviderBinarySHA256          string
	CatalogSHA256                 string
	OperationArtifactSHA256       string
	OperationDigest               string
	LifecycleReceiptSHA256        string
	LifecycleProviderBinarySHA256 string
	MappingSHA256                 string
	SourceCommit                  string
	SchemaToolchains              map[string]SchemaToolchain
}

func Verify(contract Contract, evidence Evidence) error {
	if contract.FormatVersion != 1 {
		return fmt.Errorf("unsupported management contract format version %d", contract.FormatVersion)
	}
	if err := requireDigest("provider binary", contract.Provider.Binary.SHA256); err != nil {
		return err
	}
	if err := requireEqual("provider binary", contract.Provider.Binary.SHA256, evidence.ProviderBinarySHA256); err != nil {
		return err
	}
	if err := requireDigest("catalog", contract.Catalog.SHA256); err != nil {
		return err
	}
	if err := requireEqual("catalog", contract.Catalog.SHA256, evidence.CatalogSHA256); err != nil {
		return err
	}
	if err := requireDigest("operation artifact", contract.Operation.ArtifactSHA256); err != nil {
		return err
	}
	if err := requireEqual("operation artifact", contract.Operation.ArtifactSHA256, evidence.OperationArtifactSHA256); err != nil {
		return err
	}
	if err := requireDigest("operation digest", contract.Operation.Digest); err != nil {
		return err
	}
	if err := requireEqual("operation digest", contract.Operation.Digest, evidence.OperationDigest); err != nil {
		return err
	}
	if contract.Lifecycle.Result != "pass" {
		return fmt.Errorf("lifecycle result is %q, want pass", contract.Lifecycle.Result)
	}
	if err := requireDigest("lifecycle receipt", contract.Lifecycle.ReceiptSHA256); err != nil {
		return err
	}
	if err := requireEqual("lifecycle receipt", contract.Lifecycle.ReceiptSHA256, evidence.LifecycleReceiptSHA256); err != nil {
		return err
	}
	if err := requireEqual("lifecycle provider binary", contract.Provider.Binary.SHA256, evidence.LifecycleProviderBinarySHA256); err != nil {
		return err
	}
	if err := requireDigest("mapping corpus", contract.Provenance.MappingSHA256); err != nil {
		return err
	}
	if err := requireEqual("mapping corpus", contract.Provenance.MappingSHA256, evidence.MappingSHA256); err != nil {
		return err
	}
	if !validHex(contract.Provider.SourceCommit, 40) {
		return fmt.Errorf("source commit is incomplete")
	}
	if err := requireEqual("source commit", contract.Provider.SourceCommit, evidence.SourceCommit); err != nil {
		return err
	}

	for _, name := range []string{"terraform", "tofu"} {
		want, ok := contract.Provider.Schema.Toolchains[name]
		if !ok || want.Version == "" {
			return fmt.Errorf("%s schema toolchain is incomplete", name)
		}
		if err := requireDigest(name+" schema toolchain binary", want.BinarySHA256); err != nil {
			return err
		}
		if err := requireDigest(name+" canonical schema", want.CanonicalSchemaSHA256); err != nil {
			return err
		}
		got, ok := evidence.SchemaToolchains[name]
		if !ok {
			return fmt.Errorf("%s schema toolchain evidence is missing", name)
		}
		if err := requireEqual(name+" schema toolchain version", want.Version, got.Version); err != nil {
			return err
		}
		if err := requireEqual(name+" schema toolchain binary", want.BinarySHA256, got.BinarySHA256); err != nil {
			return err
		}
		if err := requireEqual(name+" canonical schema", want.CanonicalSchemaSHA256, got.CanonicalSchemaSHA256); err != nil {
			return err
		}
	}
	return nil
}

func requireDigest(label, value string) error {
	if !validHex(value, 64) {
		return fmt.Errorf("%s SHA-256 is incomplete", label)
	}
	return nil
}

func validHex(value string, length int) bool {
	if len(value) != length {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func requireEqual(label, want, got string) error {
	if want != got {
		return fmt.Errorf("%s mismatch: expected %q, measured %q", label, want, got)
	}
	return nil
}
