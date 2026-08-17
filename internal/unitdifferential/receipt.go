package unitdifferential

import "github.com/ubiquiti-community/terraform-provider-unifi/internal/catalogparity"

// Run is everything one catalog-unit-differential run observed. Flat, no
// defaults, nothing derived here.
type Run struct {
	SourceCommit    string
	ReleasedCommit  string
	Platform        string
	GoVersion       string
	InventorySHA256 string

	Released  catalogparity.UnitSuiteReceipt
	Candidate catalogparity.UnitSuiteReceipt

	TreeState         *catalogparity.TreeState
	PromotionBlockers []string

	// DiagnosticToolchainAllowed is CATALOG_ALLOW_DIAGNOSTIC_TOOLCHAIN.
	//
	// It changes what a blocked run is CALLED and never what it found. With it
	// set, an environment that does not match the baseline yields
	// diagnostic_pass and the run continues; without it the caller stops. The
	// blockers themselves are recorded either way, so the receipt says what was
	// wrong regardless of whether anyone was allowed to proceed.
	DiagnosticToolchainAllowed bool
}

// BuildUnitDifferentialReceipt assembles the receipt from what the run found.
//
// RESULT IS DERIVED, and it has four outcomes rather than two. A suite that
// failed is a failure whatever the toolchain was -- the environment cannot
// excuse a broken test. A clean pair of suites in an environment that does not
// match the baseline is diagnostic_pass, which says the tests are fine and the
// run is not promotable. In the shell these came from separate variables, and
// nothing stopped a receipt saying pass beside a non-empty blocker list.
//
// "blocked" IS NEW AND IS A DELIBERATE DIFFERENCE. Faced with blockers and no
// diagnostic permission the shell printed them and exited, writing NO RECEIPT
// AT ALL -- so the one run a reader most wants to inspect afterwards is the one
// that left nothing behind, and a missing file is indistinguishable from a step
// that never ran. The caller still stops; it now stops with a receipt saying
// which check moved.
//
// Nothing can be smuggled through by the new value. BuildAdmission requires
// result == "pass" AND an empty blocker list, so "blocked" and
// "diagnostic_pass" are both rejected, and so is a "pass" that arrived beside
// blockers.
func BuildUnitDifferentialReceipt(run Run) catalogparity.UnitDifferentialReceipt {
	blockers := run.PromotionBlockers
	if blockers == nil {
		// Consumers range over this, and null decodes to a nil slice that reads
		// as "no blockers" by accident rather than by measurement.
		blockers = []string{}
	}

	result := "pass"
	switch {
	case run.Released.Result != "pass" || run.Candidate.Result != "pass":
		result = "fail"
	case len(blockers) > 0 && run.DiagnosticToolchainAllowed:
		result = "diagnostic_pass"
	case len(blockers) > 0:
		result = "blocked"
	}

	return catalogparity.UnitDifferentialReceipt{
		FormatVersion:     1,
		Gate:              "catalog-unit-http-differential",
		TreeState:         run.TreeState,
		Result:            result,
		PromotionBlockers: blockers,
		// The suites run with the module proxy off and no controller. Recorded
		// so a reader can tell this differential apart from the controller one,
		// which is the same shape and does reach a network.
		Network:         "none",
		SourceCommit:    run.SourceCommit,
		ReleasedCommit:  run.ReleasedCommit,
		Platform:        run.Platform,
		GoVersion:       run.GoVersion,
		InventorySHA256: run.InventorySHA256,
		Released:        run.Released,
		Candidate:       run.Candidate,
	}
}
