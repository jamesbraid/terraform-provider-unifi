package schemaparity

import (
	"encoding/json"
	"testing"

	"github.com/ubiquiti-community/terraform-provider-unifi/internal/catalogparity"
)

func promotableRun() SchemaRun {
	return SchemaRun{
		SourceCommit: "cafe", ReleasedCommit: "beef",
		Platform: "linux/amd64", GoVersion: "1.25.8",
		ReleasedSourceRebuildSHA256: "rel-rebuild", ReleasedAuthority: "published_archive",
		ReleasedAuthoritySHA256: "rel-archive", CandidateSHA256: "cand",
		Terraform:          catalogparity.SchemaCLIReceipt{Version: "1.15.8", BinarySHA256: "tf"},
		Tofu:               catalogparity.SchemaCLIReceipt{Version: "1.12.1", BinarySHA256: "tofu"},
		SharedSchemaSHA256: "shared", InventorySHA256: "inv",
		SharedProjectionEqual: true, FullProjectionEqual: false,
		ReleaseToCandidateWithinCLI: true,
	}
}

// TestResultIsDerivedFromBlockers pins the pair that could disagree in shell.
//
// result and promotion_blockers were rendered from separate variables there, so
// a receipt saying "pass" beside a non-empty blocker list was expressible and
// nothing rejected it. Deriving one from the other makes that unrepresentable
// rather than merely unlikely.
func TestResultIsDerivedFromBlockers(t *testing.T) {
	if got := BuildSchemaReceipt(promotableRun()).Result; got != "pass" {
		t.Errorf("no blockers gave result %q, want pass", got)
	}
	run := promotableRun()
	run.PromotionBlockers = []string{"go_version"}
	receipt := BuildSchemaReceipt(run)
	if receipt.Result != "blocked" {
		t.Errorf("one blocker gave result %q, want blocked", receipt.Result)
	}
	if len(receipt.PromotionBlockers) != 1 {
		t.Errorf("blockers = %v, want the one that was passed in", receipt.PromotionBlockers)
	}
}

// TestAbsentBlockersRenderAsAnEmptyArray. Consumers range over the field, and a
// nil slice marshals to null, which decodes back as "no blockers" by accident
// rather than by measurement -- the same soft-failure shape as jq printing null
// for a file that was not there.
func TestAbsentBlockersRenderAsAnEmptyArray(t *testing.T) {
	raw, err := json.Marshal(BuildSchemaReceipt(promotableRun()))
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	blockers, ok := decoded["promotion_blockers"]
	if !ok {
		t.Fatal("promotion_blockers is absent from the receipt entirely")
	}
	if blockers == nil {
		t.Fatal("promotion_blockers marshalled to null; a consumer ranging over it reads no blockers")
	}
	if list, ok := blockers.([]any); !ok || len(list) != 0 {
		t.Fatalf("promotion_blockers = %#v, want an empty array", blockers)
	}
}

// TestTheTwoProjectionClaimsAreOpposite guards a pair that is easy to collapse.
//
// The shell wrote shared_cli_projection_equal: true and
// full_cli_projection_equal: false as LITERALS, so the receipt asserted both
// regardless of what the run found. They are opposite claims -- the CLIs must
// agree on the shared surface and must differ on the full projection -- and a
// port that carried the literals would look correct and record nothing.
func TestTheTwoProjectionClaimsAreOpposite(t *testing.T) {
	run := promotableRun()
	run.SharedProjectionEqual, run.FullProjectionEqual = false, true
	evidence := BuildSchemaReceipt(run).SchemaEvidence
	if evidence.SharedCLIProjectionEqual != false || evidence.FullCLIProjectionEqual != true {
		t.Fatalf("the receipt did not carry what the run measured: shared=%v full=%v",
			evidence.SharedCLIProjectionEqual, evidence.FullCLIProjectionEqual)
	}
}

// TestTerraformOnlyCategoriesAreNotEmpty. The inverted control depends on this
// list: if it were empty the two projections could legitimately match and the
// assertion that they must differ would become unsatisfiable.
func TestTerraformOnlyCategoriesAreNotEmpty(t *testing.T) {
	got := BuildSchemaReceipt(promotableRun()).SchemaEvidence.TerraformOnlyCategories
	if len(got) != 2 {
		t.Fatalf("terraform-only categories = %v, want the two keys tofu omits", got)
	}
}

// TestAZeroRunProducesAnObviouslyBadReceipt is the property the shell lacked.
//
// An unset variable there rendered as an empty JSON string and every consumer
// read it as a value. Here a zero SchemaRun yields empty commits and empty
// digests -- still wrong, but wrong in a way a reader and a decoder can both
// see, rather than a plausible receipt describing nothing.
func TestAZeroRunProducesAnObviouslyBadReceipt(t *testing.T) {
	receipt := BuildSchemaReceipt(SchemaRun{})
	if receipt.SourceCommit != "" || receipt.ProviderBinaries.CandidateSHA256 != "" {
		t.Fatal("a zero run invented values")
	}
	if receipt.Gate != "catalog-build-schema" || receipt.FormatVersion != 1 {
		t.Fatal("the gate identity is not fixed by the constructor")
	}
	if receipt.CleanBuilds.Released != 0 || receipt.CleanBuilds.Candidate != 0 {
		t.Fatalf("a zero run claims %+v clean builds. It performed none", receipt.CleanBuilds)
	}
}

// TestCleanBuildCountsComeFromTheRun replaces an assertion that enshrined the
// defect it was meant to catch.
//
// The first version of this test required clean_builds to be {2, 2}, because
// the constructor wrote that pair as a literal. Both were written in the same
// sitting, by someone who had just finished explaining why the shell's literal
// projection claims were wrong -- so the test agreed with the code and neither
// was measuring anything. The determinism assertion is only worth something if
// a side was built more than once, and a receipt that always says 2 cannot
// report the run where somebody made it 1.
func TestCleanBuildCountsComeFromTheRun(t *testing.T) {
	receipt := BuildSchemaReceipt(SchemaRun{
		CleanBuilds: catalogparity.BuildCounts{Released: 2, Candidate: 1},
	})
	if receipt.CleanBuilds.Released != 2 || receipt.CleanBuilds.Candidate != 1 {
		t.Fatalf("clean builds = %+v, want the pair the run reported", receipt.CleanBuilds)
	}
}
