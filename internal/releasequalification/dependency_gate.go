package releasequalification

import (
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/receiptcheck"
)

// CheckDependencyPublishabilityReceipt replaces the `jq -e` gate that appears
// IDENTICALLY in two workflows:
//
//	jq -e '.result == "pass" and .module_path == "github.com/ubiquiti-community/go-unifi"
//	       and .replace_present == false and .resolution_runner == "remote_ci"
//	       and .network_boundary == "remote_ci_only"' /tmp/catalog-dependency-publishability.json
//
// THE YAML CALLS IT A BACKSTOP AND IS RIGHT: every field it names is a constant
// the script writes only after its own expect() calls passed. Ported faithfully
// and no further, it would be a check that cannot fail, in Go instead of in
// shell. The five clauses are kept anyway -- they cost nothing and they catch
// the script's abort regressing, which is what a backstop is for -- but they are
// marked, so a reader tallying what this pipeline proves does not count them.
//
// WHAT MAKES THE PORT WORTH DOING IS THE SECOND HALF. The receipt already
// carries facts nothing has ever read: two digests, a commit, and a tree state.
// Those are measurements rather than restatements, and a receipt can be wrong
// about them while every constant above is correct. This is the same shape as
// the unit-differential gate, which could not tell a passing suite from a suite
// that ran no tests while all four of its clauses held.
//
// THE RULES ARE NOW VALUES rather than a function body, over the shared
// primitives in internal/receiptcheck. Each primitive takes its operand as a
// required parameter, so there is no Hex() without a length -- which is the one
// property that makes consolidation safe here. `!validHex(FIELD, NUM)` covers
// both the 40-character commit below and the 64-character digests above it, and
// a shared primitive that dropped the length would let one satisfy the other's
// slot with every test still green.
func CheckDependencyPublishabilityReceipt(receipt DependencyPublishabilityReceipt) error {
	rules := []receiptcheck.Rule{
		// The backstop half. Constants the producer writes; kept against its
		// abort regressing, not counted as verification.
		receiptcheck.Equals("result", receipt.Result, "pass"),
		receiptcheck.Equals("gate", receipt.Gate, "go-unifi-dependency-publishability"),
		receiptcheck.Equals("module_path", receipt.ModulePath, canonicalGoUnifiModule),
		receiptcheck.Equals("resolution_runner", receipt.ResolutionRunner, "remote_ci"),
		receiptcheck.Equals("network_boundary", receipt.NetworkBoundary, "remote_ci_only"),
		receiptcheck.IsFalse("replace_present", receipt.ReplacePresent,
			"a redirected module is not publishable"),

		// The half that carries information. Every one of these can be wrong on
		// a receipt whose constants are all correct.
		receiptcheck.Hex("module_zip_sha256", receipt.ModuleZipSHA256, 64),
		receiptcheck.Hex("module_dir_sha256", receipt.ModuleDirSHA256, 64),

		// The archive and the extracted tree are hashed by different methods
		// over different bytes, so equal digests mean one was copied from the
		// other rather than measured.
		//
		// THE EMPTY GUARD IS LOAD-BEARING AND IS KEPT DELIBERATELY. Two absent
		// digests are equal, and reporting them as "identical" would replace the
		// accurate complaint from the two rules above with a misleading one. A
		// bare Differ() would have changed that, which is the kind of behaviour
		// change a consolidation is most likely to make by accident.
		receiptcheck.Custom(
			receipt.ModuleZipSHA256 == "" || receipt.ModuleZipSHA256 != receipt.ModuleDirSHA256,
			"module_zip_sha256 and module_dir_sha256 are identical, "+
				"which they cannot be if both were measured"),

		receiptcheck.Hex("provider_commit", receipt.ProviderCommit, 40),
		receiptcheck.Custom(receipt.ModuleVersion != "",
			"module_version is empty, so the receipt names no version"),

		// The tree state exists on this receipt precisely because it was
		// missing: the producer wrote a `tree` key this type could not decode,
		// so catalog-release-ready refused the receipt outright. Requiring it
		// now is what stops that returning as an absence nobody notices.
		receiptcheck.Custom(receipt.TreeState != nil,
			"tree_state is absent, so the receipt does not say which tree it describes"),
	}

	// Reached only when the tree state is present, exactly as before: the
	// original read .Status inside an else-branch, so a nil receipt produced one
	// complaint rather than a panic.
	if receipt.TreeState != nil {
		rules = append(rules, receiptcheck.Custom(receipt.TreeState.Status == "clean",
			"tree_state.status is %q; release evidence must describe a committed tree",
			receipt.TreeState.Status))
	}

	return receiptcheck.Run("catalog-dependency-publishability", rules...)
}
