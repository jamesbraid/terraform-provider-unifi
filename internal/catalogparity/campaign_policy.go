package catalogparity

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

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
	RuntimeChangeSet []RuntimeChange `json:"runtime_change_set"`
}

// RuntimeChangeReason is why a human expected a surface's runtime to differ
// from the released provider's.
//
// The reason exists because a bare list of sixty-four names is not a
// declaration anyone can review, and the obvious way to satisfy one is to paste
// in whatever the inventory measured. A reason cannot be pasted: it is a claim
// about WHY, and RuntimeChangeReasonsMatchTree refuses one the repository
// contradicts. The human still decides what changed and why; the tree only gets
// to say "that is not what I show".
type RuntimeChangeReason string

const (
	// ReasonConverted: this surface's own schema is now generated. The
	// surface's generated package must exist.
	ReasonConverted RuntimeChangeReason = "converted"

	// ReasonCompanionConversion: the runtime file changed because a companion
	// surface sharing it was converted, not because this surface was.
	// evidencePaths maps a stem's managed and list surfaces to the same
	// unifi/<stem>_resource.go, and a list resource's schema function lives in
	// the managed resource's file, so converting one moves the other's bytes
	// while leaving its schema untouched. unifi_port_forward is exactly this
	// and nothing derived from the inventory can say so.
	ReasonCompanionConversion RuntimeChangeReason = "companion_conversion"

	// ReasonHandEdit: hand-written runtime code was deliberately changed. This
	// is the reason that should attract review, because unlike the other two it
	// is not a mechanical consequence of the conversion campaign.
	ReasonHandEdit RuntimeChangeReason = "hand_edit"
)

// RuntimeChange declares one surface whose runtime is expected to differ from
// the released provider's, and why.
type RuntimeChange struct {
	SurfaceKey
	Reason RuntimeChangeReason `json:"reason"`
}

func validRuntimeChangeReason(reason RuntimeChangeReason) bool {
	return reason == ReasonConverted ||
		reason == ReasonCompanionConversion ||
		reason == ReasonHandEdit
}

// RuntimeChangeKeys projects the declared surfaces for comparison against a
// measured change set. The reasons are for humans and for the tree check; the
// gate still compares exactly the surfaces it compared before.
func (policy CampaignPolicy) RuntimeChangeKeys() []SurfaceKey {
	keys := make([]SurfaceKey, 0, len(policy.RuntimeChangeSet))
	for _, change := range policy.RuntimeChangeSet {
		keys = append(keys, change.SurfaceKey)
	}
	return keys
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
	for _, change := range policy.RuntimeChangeSet {
		if !validSurfaceKind(change.Kind) || change.Name == "" {
			return fmt.Errorf("campaign policy runtime change set has an invalid surface")
		}
		if !validRuntimeChangeReason(change.Reason) {
			return fmt.Errorf(
				"campaign policy runtime change %s/%s has reason %q, which is not one of %s, %s, %s",
				change.Kind, change.Name, change.Reason,
				ReasonConverted, ReasonCompanionConversion, ReasonHandEdit)
		}
		if _, duplicate := seen[change.SurfaceKey]; duplicate {
			return fmt.Errorf("campaign policy runtime change set repeats %s/%s", change.Kind, change.Name)
		}
		seen[change.SurfaceKey] = struct{}{}
	}
	if !sortedSurfaceKeys(policy.RuntimeChangeKeys()) {
		return fmt.Errorf("campaign policy runtime change set is not sorted by kind then name")
	}
	return nil
}

// RuntimeChangeReasonsMatchTree checks each declared reason against the
// generated packages actually present under generatedRoot, and returns one
// error per contradiction.
//
// It is the half of the declaration a human cannot fake by pasting the measured
// set. "converted" is refutable: the surface either has a generated package or
// it does not. The other two reasons both require the surface's OWN package to
// be absent, because a converted surface's honest reason is that it was
// converted.
//
// It does not choose between companion_conversion and hand_edit. Distinguishing
// "the bytes moved because a companion was generated" from "someone changed
// this code on purpose" is a judgement about intent, which is the whole reason
// this list is declared rather than derived. The tree only gets to refuse a
// claim it contradicts.
func (policy CampaignPolicy) RuntimeChangeReasonsMatchTree(generatedRoot string) []error {
	problems := make([]error, 0)
	for _, change := range policy.RuntimeChangeSet {
		own, companion, err := generatedPackages(change.SurfaceKey)
		if err != nil {
			problems = append(problems, err)
			continue
		}
		ownPresent := directoryExists(filepath.Join(generatedRoot, own))
		switch change.Reason {
		case ReasonConverted:
			if !ownPresent {
				problems = append(problems, fmt.Errorf(
					"%s/%s declares reason %q but has no generated package %s",
					change.Kind, change.Name, change.Reason, own))
			}
		case ReasonCompanionConversion:
			if ownPresent {
				problems = append(problems, fmt.Errorf(
					"%s/%s declares reason %q but its own package %s exists, so it was converted",
					change.Kind, change.Name, change.Reason, own))
				continue
			}
			if companion == "" || !directoryExists(filepath.Join(generatedRoot, companion)) {
				problems = append(problems, fmt.Errorf(
					"%s/%s declares reason %q but no companion package %s exists to explain the change",
					change.Kind, change.Name, change.Reason, companion))
			}
		case ReasonHandEdit:
			if ownPresent {
				problems = append(problems, fmt.Errorf(
					"%s/%s declares reason %q but its own package %s exists, so it was converted",
					change.Kind, change.Name, change.Reason, own))
			}
		}
	}
	return problems
}

// generatedPackages names the surface's own generated package and, for the
// managed and list surfaces that share one runtime file, its companion's.
func generatedPackages(key SurfaceKey) (string, string, error) {
	stem := strings.TrimPrefix(key.Name, "unifi_")
	if stem == key.Name || stem == "" {
		return "", "", fmt.Errorf("surface %s/%s has no generated package mapping", key.Kind, key.Name)
	}
	switch key.Kind {
	case ManagedResource:
		return "resource_" + stem, "listresource_" + stem, nil
	case ListResource:
		return "listresource_" + stem, "resource_" + stem, nil
	case DataSource:
		return "datasource_" + stem, "", nil
	case Action:
		return "action_" + stem, "", nil
	default:
		return "", "", fmt.Errorf("surface %s/%s has no generated package mapping", key.Kind, key.Name)
	}
}

func directoryExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
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
