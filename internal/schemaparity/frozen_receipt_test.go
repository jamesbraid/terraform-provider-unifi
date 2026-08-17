package schemaparity

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/ubiquiti-community/terraform-provider-unifi/internal/catalogparity"
)

// TestTheConstructorReproducesTheFrozenShellReceipt feeds BuildSchemaReceipt
// the values the shell measured on pipeline 232 and requires it to emit that
// pipeline's receipt.
//
// WHAT THIS DOES AND DOES NOT PROVE. The measured values are read back out of
// the frozen receipt, so for the pass-through fields it is close to a tautology
// -- what it checks there is that assembly neither drops one nor invents one.
// The teeth are in the fields the constructor DECIDES rather than copies:
// result, gate, format_version, build_network, clean_builds,
// terraform_only_categories and the three projection booleans. Every one of
// those was a literal in the jq template, so every one of them is a place a
// port can look right and record nothing.
//
// The half this cannot reach is whether the ORCHESTRATION measures the same
// things the shell measured -- same binaries, same digests, same CLI versions.
// That needs both implementations running on one tree and is the CI-shaped job
// this test does not replace.
//
// IT IS ALSO BLIND WHEREVER THE FROZEN RUN'S VALUE EQUALS THE LITERAL A PORT
// WOULD WRITE, and that is not hypothetical -- it was found by mutation, not by
// reading. Restoring clean_builds to a hardcoded {2, 2} leaves this test green,
// because the frozen run really did build each side twice. Same for result:
// the frozen run was promotable, so a constructor that always wrote "pass"
// would satisfy this comparison. Those two fields are covered by
// TestCleanBuildCountsComeFromTheRun and TestResultIsDerivedFromBlockers, which
// pass values the frozen receipt does not contain. A test built from one real
// example can only fail where that example disagrees with the mistake.
func TestTheConstructorReproducesTheFrozenShellReceipt(t *testing.T) {
	frozen := readFrozenBuildSchema(t)

	rebuilt := BuildSchemaReceipt(SchemaRun{
		SourceCommit:   frozen.SourceCommit,
		ReleasedCommit: frozen.ReleasedCommit,
		Platform:       frozen.Platform,
		GoVersion:      frozen.GoVersion,

		ReleasedSourceRebuildSHA256: frozen.ProviderBinaries.ReleasedSourceRebuildSHA256,
		ReleasedAuthority:           frozen.ProviderBinaries.ReleasedAuthority,
		ReleasedAuthoritySHA256:     frozen.ProviderBinaries.ReleasedAuthoritySHA256,
		CandidateSHA256:             frozen.ProviderBinaries.CandidateSHA256,

		Terraform: frozen.SchemaEvidence.Terraform,
		Tofu:      frozen.SchemaEvidence.Tofu,

		SharedSchemaSHA256: frozen.SchemaEvidence.SharedSchemaSHA256,
		InventorySHA256:    frozen.InventorySHA256,

		ReleaseToCandidateWithinCLI: frozen.SchemaEvidence.ReleaseToCandidateWithinCLI,
		SharedProjectionEqual:       frozen.SchemaEvidence.SharedCLIProjectionEqual,
		FullProjectionEqual:         frozen.SchemaEvidence.FullCLIProjectionEqual,

		CleanBuilds: frozen.CleanBuilds,
		TreeState:   frozen.TreeState,
		// The frozen run was promotable, so this is the empty case -- the one
		// where result must come out "pass" because nothing blocked it, rather
		// than because "pass" was written down.
		PromotionBlockers: frozen.PromotionBlockers,
	})

	want, err := catalogparity.MarshalReceipt(frozen)
	if err != nil {
		t.Fatalf("render the frozen receipt: %v", err)
	}
	got, err := catalogparity.MarshalReceipt(rebuilt)
	if err != nil {
		t.Fatalf("render the rebuilt receipt: %v", err)
	}
	if !bytes.Equal(want, got) {
		t.Fatalf("the constructor does not reproduce the shell's receipt.\nfrozen:\n%s\nrebuilt:\n%s", want, got)
	}
}

// TestTheFrozenRunWasPromotable states the precondition the test above depends
// on, rather than leaving it implied.
//
// If the frozen receipt ever carried a blocker, the comparison above would
// still pass -- result would be derived as "blocked" on both sides -- while
// silently no longer exercising the empty-blocker path, which is the one every
// green pipeline takes.
func TestTheFrozenRunWasPromotable(t *testing.T) {
	frozen := readFrozenBuildSchema(t)
	if len(frozen.PromotionBlockers) != 0 {
		t.Fatalf("the frozen run reports blockers %v, so it no longer exercises the pass path",
			frozen.PromotionBlockers)
	}
	if frozen.Result != "pass" {
		t.Fatalf("the frozen run reports result %q, want pass", frozen.Result)
	}
}

func readFrozenBuildSchema(t *testing.T) catalogparity.BuildSchemaReceipt {
	t.Helper()
	path := filepath.Join("..", "..", "build", "migration-baseline", "catalog-build-schema.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var receipt catalogparity.BuildSchemaReceipt
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&receipt); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
	return receipt
}
