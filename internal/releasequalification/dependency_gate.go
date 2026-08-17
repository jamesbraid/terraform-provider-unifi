package releasequalification

import (
	"fmt"
	"strings"
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
func CheckDependencyPublishabilityReceipt(receipt DependencyPublishabilityReceipt) error {
	var mismatch []string
	want := func(what, want, got string) {
		if want != got {
			mismatch = append(mismatch, fmt.Sprintf("%s is %q, want %q", what, got, want))
		}
	}

	// The backstop half. Constants the producer writes; kept against its abort
	// regressing, not counted as verification.
	want("result", "pass", receipt.Result)
	want("gate", "go-unifi-dependency-publishability", receipt.Gate)
	want("module_path", canonicalGoUnifiModule, receipt.ModulePath)
	want("resolution_runner", "remote_ci", receipt.ResolutionRunner)
	want("network_boundary", "remote_ci_only", receipt.NetworkBoundary)
	if receipt.ReplacePresent {
		mismatch = append(mismatch, "replace_present is true; a redirected module is not publishable")
	}

	// The half that carries information. Every one of these can be wrong on a
	// receipt whose constants are all correct.
	if !validHex(receipt.ModuleZipSHA256, 64) {
		mismatch = append(mismatch, fmt.Sprintf(
			"module_zip_sha256 is %q, want 64 hex characters", receipt.ModuleZipSHA256))
	}
	if !validHex(receipt.ModuleDirSHA256, 64) {
		mismatch = append(mismatch, fmt.Sprintf(
			"module_dir_sha256 is %q, want 64 hex characters", receipt.ModuleDirSHA256))
	}
	// The archive and the extracted tree are hashed by different methods over
	// different bytes, so equal digests mean one was copied from the other
	// rather than measured.
	if receipt.ModuleZipSHA256 != "" && receipt.ModuleZipSHA256 == receipt.ModuleDirSHA256 {
		mismatch = append(mismatch, "module_zip_sha256 and module_dir_sha256 are identical, "+
			"which they cannot be if both were measured")
	}
	if !validHex(receipt.ProviderCommit, 40) {
		mismatch = append(mismatch, fmt.Sprintf(
			"provider_commit is %q, want a 40-character SHA", receipt.ProviderCommit))
	}
	if receipt.ModuleVersion == "" {
		mismatch = append(mismatch, "module_version is empty, so the receipt names no version")
	}

	// The tree state exists on this receipt precisely because it was missing:
	// the producer wrote a `tree` key this type could not decode, so
	// catalog-release-ready refused the receipt outright. Requiring it now is
	// what stops that returning as an absence nobody notices.
	if receipt.TreeState == nil {
		mismatch = append(mismatch, "tree_state is absent, so the receipt does not say which tree it describes")
	} else if receipt.TreeState.Status != "clean" {
		mismatch = append(mismatch, fmt.Sprintf(
			"tree_state.status is %q; release evidence must describe a committed tree",
			receipt.TreeState.Status))
	}

	if len(mismatch) == 0 {
		return nil
	}
	return fmt.Errorf("catalog-dependency-publishability: %d assertion(s) failed:\n    %s",
		len(mismatch), strings.Join(mismatch, "\n    "))
}
