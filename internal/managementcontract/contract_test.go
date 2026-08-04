package managementcontract

import (
	"strings"
	"testing"
)

const (
	testProviderBinary  = "1111111111111111111111111111111111111111111111111111111111111111"
	testCatalog         = "2222222222222222222222222222222222222222222222222222222222222222"
	testOperation       = "3333333333333333333333333333333333333333333333333333333333333333"
	testOperationModel  = "4444444444444444444444444444444444444444444444444444444444444444"
	testLifecycle       = "5555555555555555555555555555555555555555555555555555555555555555"
	testMapping         = "6666666666666666666666666666666666666666666666666666666666666666"
	testTerraform       = "7777777777777777777777777777777777777777777777777777777777777777"
	testTerraformSchema = "8888888888888888888888888888888888888888888888888888888888888888"
	testTofu            = "9999999999999999999999999999999999999999999999999999999999999999"
	testTofuSchema      = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	testSourceCommit    = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

func TestVerifyRejectsUnboundManagementEvidence(t *testing.T) {
	contract := testContract()
	evidence := testEvidence()

	tests := map[string]struct {
		mutate func(*Evidence)
		want   string
	}{
		"provider binary": {
			mutate: func(e *Evidence) { e.ProviderBinarySHA256 = strings.Repeat("c", 64) },
			want:   "provider binary",
		},
		"catalog": {
			mutate: func(e *Evidence) { e.CatalogSHA256 = strings.Repeat("c", 64) },
			want:   "catalog",
		},
		"operation artifact": {
			mutate: func(e *Evidence) { e.OperationArtifactSHA256 = strings.Repeat("c", 64) },
			want:   "operation artifact",
		},
		"operation model": {
			mutate: func(e *Evidence) { e.OperationDigest = strings.Repeat("c", 64) },
			want:   "operation digest",
		},
		"lifecycle receipt": {
			mutate: func(e *Evidence) { e.LifecycleReceiptSHA256 = strings.Repeat("c", 64) },
			want:   "lifecycle receipt",
		},
		"lifecycle provider": {
			mutate: func(e *Evidence) { e.LifecycleProviderBinarySHA256 = strings.Repeat("c", 64) },
			want:   "lifecycle provider binary",
		},
		"mapping corpus": {
			mutate: func(e *Evidence) { e.MappingSHA256 = strings.Repeat("c", 64) },
			want:   "mapping corpus",
		},
		"terraform toolchain": {
			mutate: func(e *Evidence) {
				toolchain := e.SchemaToolchains["terraform"]
				toolchain.BinarySHA256 = strings.Repeat("c", 64)
				e.SchemaToolchains["terraform"] = toolchain
			},
			want: "terraform schema toolchain binary",
		},
		"tofu schema": {
			mutate: func(e *Evidence) {
				toolchain := e.SchemaToolchains["tofu"]
				toolchain.CanonicalSchemaSHA256 = strings.Repeat("c", 64)
				e.SchemaToolchains["tofu"] = toolchain
			},
			want: "tofu canonical schema",
		},
		"source commit": {
			mutate: func(e *Evidence) { e.SourceCommit = strings.Repeat("c", 40) },
			want:   "source commit",
		},
	}

	if err := Verify(contract, evidence); err != nil {
		t.Fatalf("Verify(valid) error = %v", err)
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			changed := testEvidence()
			test.mutate(&changed)
			err := Verify(contract, changed)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Verify() error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestVerifyRejectsIncompleteSidecar(t *testing.T) {
	contract := testContract()
	contract.Operation.ArtifactSHA256 = ""
	if err := Verify(contract, testEvidence()); err == nil || !strings.Contains(err.Error(), "operation artifact") {
		t.Fatalf("Verify() error = %v, want operation artifact", err)
	}
}

func testContract() Contract {
	return Contract{
		FormatVersion: 1,
		Provider: Provider{
			SourceCommit: testSourceCommit,
			Binary:       Binary{SHA256: testProviderBinary},
			Schema: Schema{Toolchains: map[string]SchemaToolchain{
				"terraform": {Version: "1.15.8", BinarySHA256: testTerraform, CanonicalSchemaSHA256: testTerraformSchema},
				"tofu":      {Version: "1.12.1", BinarySHA256: testTofu, CanonicalSchemaSHA256: testTofuSchema},
			}},
		},
		Catalog:    Catalog{SHA256: testCatalog},
		Operation:  Operation{ArtifactSHA256: testOperation, Digest: testOperationModel},
		Lifecycle:  Lifecycle{ReceiptSHA256: testLifecycle, Result: "pass"},
		Provenance: Provenance{MappingSHA256: testMapping},
	}
}

func testEvidence() Evidence {
	return Evidence{
		ProviderBinarySHA256:          testProviderBinary,
		CatalogSHA256:                 testCatalog,
		OperationArtifactSHA256:       testOperation,
		OperationDigest:               testOperationModel,
		LifecycleReceiptSHA256:        testLifecycle,
		LifecycleProviderBinarySHA256: testProviderBinary,
		MappingSHA256:                 testMapping,
		SourceCommit:                  testSourceCommit,
		SchemaToolchains: map[string]SchemaToolchain{
			"terraform": {Version: "1.15.8", BinarySHA256: testTerraform, CanonicalSchemaSHA256: testTerraformSchema},
			"tofu":      {Version: "1.12.1", BinarySHA256: testTofu, CanonicalSchemaSHA256: testTofuSchema},
		},
	}
}
