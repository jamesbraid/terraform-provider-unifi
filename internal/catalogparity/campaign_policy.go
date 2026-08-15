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
	FormatVersion    int      `json:"format_version"`
	Gate             string   `json:"gate"`
	SurfaceCount     int      `json:"surface_count"`
	EvidenceGapCount int      `json:"evidence_gap_count"`
	TestNameCount    int      `json:"test_name_count"`
	AllowedSkips     []string `json:"allowed_skips"`

	// SharedScenarioExceptions names surfaces whose scenario is grafted onto
	// the released tree even though their runtime differs.
	//
	// This replaces shared_scenario_owner_count, which was a COUNT of a fact
	// the inventory already carries: the plan builder derives the owner list
	// from runtime status, so the constant was a second copy that could only go
	// stale. Admission now derives the whole set and compares sets rather than
	// lengths, which is strictly stronger -- a plan with the right number of
	// wrong owners used to pass.
	//
	// The exceptions cannot be derived, which is exactly why they are declared.
	// The port action's runtime differs, yet its scenario is shared, and that
	// sharing is what makes its hardware disposition satisfiable. No rule reads
	// that off the tree; a human asserted it, so a human writes it down with a
	// reason. A stale exception fails: admission rejects one naming a surface
	// whose runtime is identical, because that exception is doing nothing.
	SharedScenarioExceptions []SharedScenarioException `json:"shared_scenario_exceptions"`

	ReleasedAllowedFailures []string `json:"released_allowed_failures"`
	ReleasedAllowedMissing  []string `json:"released_allowed_missing"`

	// RuntimeChangeSet declares which surfaces are expected to differ from the
	// released provider at runtime. Migration recovery compares the generated
	// inventory against this list, so a runtime change nobody declared still
	// fails the gate. Sorted by kind then name to match the comparison order.
	RuntimeChangeSet []SurfaceKey `json:"runtime_change_set"`
}

// SharedScenarioException declares one surface whose scenario is shared with
// the released suite despite a differing runtime. The reason is required for
// the same cause as everywhere else on this project: a bare flag is something
// set to make a gate pass, and the next reader cannot tell a considered
// exception from a forgotten one.
type SharedScenarioException struct {
	SurfaceKey
	Reason string `json:"reason"`
}

// Validate reports whether the policy document is self-consistent. It cannot
// tell whether the values describe the current catalog; the plan builder and
// admission compare a real campaign against them for that.
func (policy CampaignPolicy) Validate() error {
	if policy.FormatVersion != 1 || policy.Gate != "catalog controller differential" {
		return fmt.Errorf("campaign policy identity is invalid")
	}
	if policy.SurfaceCount <= 0 || policy.EvidenceGapCount < 0 || policy.TestNameCount <= 0 {
		return fmt.Errorf("campaign policy counts are incomplete")
	}
	seenExceptions := make(map[SurfaceKey]struct{}, len(policy.SharedScenarioExceptions))
	for _, exception := range policy.SharedScenarioExceptions {
		if !validSurfaceKind(exception.Kind) || exception.Name == "" {
			return fmt.Errorf("campaign policy shared scenario exception has an invalid surface")
		}
		if exception.Reason == "" {
			return fmt.Errorf(
				"campaign policy shared scenario exception %s/%s has no reason",
				exception.Kind, exception.Name)
		}
		if _, duplicate := seenExceptions[exception.SurfaceKey]; duplicate {
			return fmt.Errorf(
				"campaign policy shared scenario exceptions repeat %s/%s",
				exception.Kind, exception.Name)
		}
		seenExceptions[exception.SurfaceKey] = struct{}{}
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
	seen := make(map[SurfaceKey]struct{}, len(policy.RuntimeChangeSet))
	for _, key := range policy.RuntimeChangeSet {
		if !validSurfaceKind(key.Kind) || key.Name == "" {
			return fmt.Errorf("campaign policy runtime change set has an invalid surface")
		}
		if _, duplicate := seen[key]; duplicate {
			return fmt.Errorf("campaign policy runtime change set repeats %s/%s", key.Kind, key.Name)
		}
		seen[key] = struct{}{}
	}
	if !sortedSurfaceKeys(policy.RuntimeChangeSet) {
		return fmt.Errorf("campaign policy runtime change set is not sorted by kind then name")
	}
	return nil
}

// sortedSurfaceKeys reports whether keys are in the kind-then-name order that
// migration recovery compares against, so a hand-edited policy cannot fail the
// gate an hour into a campaign purely because an entry was appended.
func sortedSurfaceKeys(keys []SurfaceKey) bool {
	for index := 1; index < len(keys); index++ {
		previous, current := keys[index-1], keys[index]
		if previous.Kind != current.Kind {
			if previous.Kind > current.Kind {
				return false
			}
			continue
		}
		if previous.Name > current.Name {
			return false
		}
	}
	return true
}
