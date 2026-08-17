package releasequalification

import (
	"fmt"
	"sort"
	"strings"
)

// VerifyMigrationRecoveryReceipt checks a migration/recovery receipt for the
// properties the campaign gate used to assert as a jq expression in
// catalog-controller-differential.yml.
//
// WHAT IT DELIBERATELY DOES NOT ASSERT: the mode distribution. The old gate
// pinned it as a literal -- {"dns_bidirectional_state":1,
// "dns_list_controller":1,"source_identity":65} -- and that is the defect this
// replaces, in two separate ways.
//
// The first is that two of those modes were deleted, so the gate could not
// pass. The second is worse and would have survived correcting the names: the
// distribution MOVES for legitimate reasons, and has moved three times already.
// TestEvidenceModesCoverTheCommittedInventory records the current census with a
// paragraph of justification per move -- one of them caused by a single
// acceptance-test edit that made two surfaces stop earning
// differential_scenario. A literal in YAML cannot carry that reasoning, cannot
// be reviewed against it, and goes stale the next time the evidence model is
// right to change. Pinning the census belongs in that test, against the
// committed tree, where a diff is a thing a human reads and agrees with.
//
// What is left here is what a campaign run can genuinely get wrong, and every
// check below can fail.
func VerifyMigrationRecoveryReceipt(receipt MigrationRecoveryReceipt) error {
	var problems []string
	note := func(format string, args ...any) {
		problems = append(problems, fmt.Sprintf(format, args...))
	}

	if receipt.Result != "pass" {
		note("result is %q, want \"pass\"", receipt.Result)
	}
	if receipt.SurfaceCount != receipt.RecoveryCount {
		note("surface_count %d and recovery_count %d disagree, so some surface was counted "+
			"without being recovered", receipt.SurfaceCount, receipt.RecoveryCount)
	}
	if len(receipt.Surfaces) != receipt.SurfaceCount {
		note("surface_count is %d but the receipt lists %d surface(s)",
			receipt.SurfaceCount, len(receipt.Surfaces))
	}

	// THE CHECK THIS FILE EXISTS FOR. A mode name that is not declared in Go can
	// only mean the receipt and the enum have drifted apart -- either a mode was
	// deleted and something still emits it, or a new one was added and this list
	// was not told. Asking AllEvidenceModes rather than restating its contents
	// is what makes the question survive the next deletion.
	for _, mode := range sortedKeys(receipt.EvidenceModes) {
		if !IsEvidenceMode(mode) {
			note("evidence_modes names %q, which is not a declared evidence mode (declared: %s)",
				mode, strings.Join(AllEvidenceModes, ", "))
		}
	}

	if summed := sumCounts(receipt.EvidenceModes); summed != receipt.SurfaceCount {
		note("evidence_modes accounts for %d surface(s) but surface_count is %d, so %d surface(s) "+
			"carry no mode", summed, receipt.SurfaceCount, receipt.SurfaceCount-summed)
	}
	if summed := sumCounts(receipt.StrategyCounts); summed != receipt.SurfaceCount {
		note("strategy_counts accounts for %d surface(s) but surface_count is %d",
			summed, receipt.SurfaceCount)
	}

	// Recount from the surfaces themselves. The producer writes the summary maps
	// and the surface list in one pass, so agreement is expected -- which is
	// exactly why disagreement is worth reporting: it means the file was edited
	// after it was written, or two producers wrote it.
	recountedModes := map[string]int{}
	recountedStrategies := map[string]int{}
	for _, surface := range receipt.Surfaces {
		name := string(surface.Kind) + "/" + surface.Name
		if surface.State != "migration_recovery_pass" {
			note("%s is in state %q, want \"migration_recovery_pass\"", name, surface.State)
		}
		if !validHex(surface.ReceiptSHA256, 64) {
			note("%s has receipt_sha256 %q, which is not 64 hex characters",
				name, surface.ReceiptSHA256)
		}
		if !IsEvidenceMode(surface.EvidenceMode) {
			note("%s carries evidence_mode %q, which is not a declared evidence mode",
				name, surface.EvidenceMode)
		}
		recountedModes[surface.EvidenceMode]++
		recountedStrategies[surface.Strategy]++
	}
	for _, mismatch := range compareCounts("evidence_modes", receipt.EvidenceModes, recountedModes) {
		note("%s", mismatch)
	}
	for _, mismatch := range compareCounts("strategy_counts", receipt.StrategyCounts, recountedStrategies) {
		note("%s", mismatch)
	}

	if len(problems) > 0 {
		return fmt.Errorf("migration/recovery receipt is not acceptable:\n  %s",
			strings.Join(problems, "\n  "))
	}
	return nil
}

func sumCounts(counts map[string]int) int {
	total := 0
	for _, count := range counts {
		total += count
	}
	return total
}

func sortedKeys(counts map[string]int) []string {
	keys := make([]string, 0, len(counts))
	for key := range counts {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func compareCounts(label string, declared, recounted map[string]int) []string {
	seen := map[string]bool{}
	for key := range declared {
		seen[key] = true
	}
	for key := range recounted {
		seen[key] = true
	}
	names := make([]string, 0, len(seen))
	for key := range seen {
		names = append(names, key)
	}
	sort.Strings(names)

	var mismatches []string
	for _, name := range names {
		if declared[name] != recounted[name] {
			mismatches = append(mismatches, fmt.Sprintf(
				"%s says %q is %d but the surface list contains %d",
				label, name, declared[name], recounted[name]))
		}
	}
	return mismatches
}
