package catalogparity

import "fmt"

// CampaignPolicy is the single source of truth for the shape of the catalog
// controller differential campaign and for the released suite's accepted
// limitations.
//
// The plan builder, the followup gate, the pragmatic evidence binder, and
// catalog admission all read the same committed policy document. Before that,
// each of them carried its own copy of these values in its own language, and a
// copy that had drifted was only observable by burning a full campaign run.
type CampaignPolicy struct {
	FormatVersion            int      `json:"format_version"`
	Gate                     string   `json:"gate"`
	SurfaceCount             int      `json:"surface_count"`
	EvidenceGapCount         int      `json:"evidence_gap_count"`
	TestNameCount            int      `json:"test_name_count"`
	SharedScenarioOwnerCount int      `json:"shared_scenario_owner_count"`
	AllowedSkips             []string `json:"allowed_skips"`
	ReleasedAllowedFailures  []string `json:"released_allowed_failures"`
	ReleasedAllowedMissing   []string `json:"released_allowed_missing"`
}

// Validate reports whether the policy document is self-consistent. It cannot
// tell whether the values describe the current catalog; the plan builder and
// admission compare a real campaign against them for that.
func (policy CampaignPolicy) Validate() error {
	if policy.FormatVersion != 1 || policy.Gate != "catalog controller differential" {
		return fmt.Errorf("campaign policy identity is invalid")
	}
	if policy.SurfaceCount <= 0 || policy.EvidenceGapCount < 0 ||
		policy.TestNameCount <= 0 || policy.SharedScenarioOwnerCount <= 0 {
		return fmt.Errorf("campaign policy counts are incomplete")
	}
	for _, names := range [][]string{
		policy.AllowedSkips,
		policy.ReleasedAllowedFailures,
		policy.ReleasedAllowedMissing,
	} {
		if len(names) != len(uniqueStrings(names)) {
			return fmt.Errorf("campaign policy repeats a test name")
		}
	}
	// A test cannot both be expected to run and fail and be expected not to
	// exist on the released side, and a skipped test is not a missing one.
	if stringsOverlap(policy.ReleasedAllowedFailures, policy.ReleasedAllowedMissing) ||
		stringsOverlap(policy.AllowedSkips, policy.ReleasedAllowedMissing) ||
		stringsOverlap(policy.AllowedSkips, policy.ReleasedAllowedFailures) {
		return fmt.Errorf("campaign policy dispositions overlap")
	}
	return nil
}
