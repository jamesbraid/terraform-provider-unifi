package releasequalification

import (
	"fmt"
	"sort"
	"strings"

	"github.com/ubiquiti-community/terraform-provider-unifi/internal/catalogparity"
)

// An evidence mode names WHY one surface's identity migration is believed safe.
// It is recorded in the migration/recovery receipt and folded into that
// surface's receipt digest, so it is a published claim rather than a comment.
//
// Every mode here is selected from MEASURED state. Before this file the mode
// was assigned by name: every surface got source_identity unless it was one of
// the two dns_record surfaces, and nothing consulted the inventory. Sixty-two
// surfaces were therefore stamped "recovered because the source is identical"
// while their source had changed -- that change being the entire point of the
// conversion work.
//
// WHAT source_identity WAS ACTUALLY BUYING, which is not obvious and which the
// replacement has to reproduce: the controller differential builds the released
// tree from a git archive and then copies the CANDIDATE's test file over it,
// but only for surfaces whose runtime is byte-identical
// (.woodpecker/scripts/catalog-controller-differential.sh:131-139). Its own
// comment states the rule nothing consumed: "A changed runtime path retains its
// released test owner and needs separate migration evidence." Byte-identity was
// what let one scenario speak for both providers. A converted surface needs
// that same property established some other way.
//
// WHAT IS DELIBERATELY NOT ASSERTED HERE: schema equivalence. It is tempting,
// and it is the first thing a reader will look for. It is already required of
// the whole estate upstream in validateMigrationBuild -- the dual-CLI
// installed-binary projection comparison, which unifi/baseline_projection_test.go
// calls "the real gate". Per-surface schema equality is a CONSEQUENCE of that
// receipt, not an independent test, so a mode asserting it would evaluate
// identically for all sixty-seven surfaces and discriminate nothing. A check
// that cannot fail is decoration.
const (
	// EvidenceSourceIdentity: the runtime source is byte-identical to the
	// released provider's, so nothing about the surface can have moved.
	EvidenceSourceIdentity = "source_identity"

	// EvidenceDifferentialScenario: the runtime source changed, but the
	// scenario source did NOT, and every acceptance test that scenario plans
	// passed against a live controller on BOTH the released and the candidate
	// provider. An unchanged referee reached the same verdict about both
	// implementations. This is the converted analogue of byte-identity.
	EvidenceDifferentialScenario = "differential_scenario"

	// EvidencePragmaticReference: the runtime source changed and the scenario
	// source did not, but the surface has no acceptance signal of its own and
	// the campaign resolved that gap through a declared reference to another
	// surface -- an alias, a sibling read, a fleet shape, or a zero-use
	// endpoint. Catalog admission proves every such gap is either resolved or
	// carried as a release blocker, so an unresolved gap cannot reach here.
	EvidencePragmaticReference = "pragmatic_reference"

	// EvidenceDNSBidirectionalState and EvidenceDNSListController are the two
	// dns_record surfaces, which carry a dedicated M3 lifecycle receipt.
	//
	// KNOWN LIMITATION, recorded rather than hidden: DNSLifecycleReceipt
	// carries no surface key, so the surface these modes apply to is still
	// matched by NAME. What has changed is that each now has a precondition
	// that can fail -- the name alone no longer grants the mode. Giving the M3
	// receipt a surface key would remove the last of it and belongs with that
	// receipt's producer.
	EvidenceDNSBidirectionalState = "dns_bidirectional_state"
	EvidenceDNSListController     = "dns_list_controller"
)

// evidenceView is every measured fact the mode rules read, indexed once per
// receipt rather than re-scanned per surface.
type evidenceView struct {
	inventory map[catalogparity.SurfaceKey]catalogparity.SurfaceEvidenceInventory
	admission map[catalogparity.SurfaceKey]catalogparity.SurfaceAdmission
	planned   map[catalogparity.SurfaceKey][]string
	released  map[string]struct{}
	candidate map[string]struct{}
	dns       DNSLifecycleChecks
}

func newEvidenceView(input MigrationRecoveryInput) evidenceView {
	view := evidenceView{
		inventory: make(map[catalogparity.SurfaceKey]catalogparity.SurfaceEvidenceInventory, len(input.Inventory.Surfaces)),
		admission: make(map[catalogparity.SurfaceKey]catalogparity.SurfaceAdmission, len(input.Admission.Surfaces)),
		planned:   make(map[catalogparity.SurfaceKey][]string, len(input.Controller.Plan.Surfaces)),
		released:  make(map[string]struct{}, len(input.Controller.Released.Passed)),
		candidate: make(map[string]struct{}, len(input.Controller.Candidate.Passed)),
		dns:       input.DNSLifecycle.Lifecycle,
	}
	for _, surface := range input.Inventory.Surfaces {
		view.inventory[surface.SurfaceKey] = surface
	}
	for _, surface := range input.Admission.Surfaces {
		view.admission[surface.SurfaceKey] = surface
	}
	for _, surface := range input.Controller.Plan.Surfaces {
		view.planned[surface.SurfaceKey] = surface.TestNames
	}
	for _, name := range input.Controller.Released.Passed {
		view.released[name] = struct{}{}
	}
	for _, name := range input.Controller.Candidate.Passed {
		view.candidate[name] = struct{}{}
	}
	return view
}

// verdict is one mode's ruling on one surface. A mode that does not hold must
// say why: "no mode applies" with no reason sends the reader to guess, and the
// reason is the whole diagnostic value of the gate failing here rather than an
// hour into a campaign.
type verdict struct {
	mode    string
	holds   bool
	because string
}

// evidenceMode returns the single mode whose preconditions the surface meets.
//
// Exactly one, deliberately. Zero means nothing justifies calling this surface
// recovered and the release must stop. More than one means the rules overlap
// and the recorded label would be an artefact of evaluation order, which is the
// by-name defect wearing different clothes.
func (v evidenceView) evidenceMode(entry catalogparity.MigrationEntry) (string, error) {
	key := entry.SurfaceKey
	surface, ok := v.inventory[key]
	if !ok {
		return "", fmt.Errorf("surface %s/%s has no inventory entry", key.Kind, key.Name)
	}

	verdicts := []verdict{
		v.sourceIdentity(surface),
		v.differentialScenario(key, surface),
		v.pragmaticReference(key, surface),
		v.dnsBidirectionalState(key),
		v.dnsListController(key, surface),
	}

	held := make([]string, 0, 1)
	refused := make([]string, 0, len(verdicts))
	for _, ruling := range verdicts {
		if ruling.holds {
			held = append(held, ruling.mode)
			continue
		}
		refused = append(refused, ruling.mode+": "+ruling.because)
	}
	switch len(held) {
	case 1:
		return held[0], nil
	case 0:
		return "", fmt.Errorf(
			"surface %s/%s has no evidence mode; every mode refused it (%s)",
			key.Kind, key.Name, strings.Join(refused, "; "),
		)
	default:
		sort.Strings(held)
		return "", fmt.Errorf(
			"surface %s/%s matches %d evidence modes (%s); the rules overlap and the label would be arbitrary",
			key.Kind, key.Name, len(held), strings.Join(held, ", "),
		)
	}
}

func (v evidenceView) sourceIdentity(surface catalogparity.SurfaceEvidenceInventory) verdict {
	if surface.Runtime.Status != catalogparity.FileIdentical {
		return verdict{EvidenceSourceIdentity, false, fmt.Sprintf(
			"runtime %s is %s against the released provider", surface.Runtime.Path, surface.Runtime.Status)}
	}
	return verdict{EvidenceSourceIdentity, true, ""}
}

func (v evidenceView) differentialScenario(
	key catalogparity.SurfaceKey,
	surface catalogparity.SurfaceEvidenceInventory,
) verdict {
	const mode = EvidenceDifferentialScenario
	if surface.Runtime.Status != catalogparity.FileChanged {
		return verdict{mode, false, "runtime is unchanged, so source_identity covers it"}
	}
	if surface.Tests.Status != catalogparity.FileIdentical {
		return verdict{mode, false, fmt.Sprintf(
			"scenario %s changed, so the released provider was not exercised by this scenario",
			surface.ScenarioOwner)}
	}
	planned := v.planned[key]
	if len(planned) == 0 {
		return verdict{mode, false, "the campaign plans no acceptance test for it"}
	}
	for _, name := range planned {
		if _, passed := v.released[name]; !passed {
			return verdict{mode, false, name + " did not pass on the released provider"}
		}
		if _, passed := v.candidate[name]; !passed {
			return verdict{mode, false, name + " did not pass on the candidate provider"}
		}
	}
	return verdict{mode, true, ""}
}

func (v evidenceView) pragmaticReference(
	key catalogparity.SurfaceKey,
	surface catalogparity.SurfaceEvidenceInventory,
) verdict {
	const mode = EvidencePragmaticReference
	if surface.Runtime.Status != catalogparity.FileChanged {
		return verdict{mode, false, "runtime is unchanged, so source_identity covers it"}
	}
	if surface.Tests.Status != catalogparity.FileIdentical {
		return verdict{mode, false, "scenario " + surface.ScenarioOwner + " changed"}
	}
	signal := acceptanceSignal(key.Kind)
	if !containsString(surface.MissingSignals, signal) {
		return verdict{mode, false, fmt.Sprintf(
			"it has its own %s signal, so a reference to another surface is not what justifies it", signal)}
	}
	admitted, ok := v.admission[key]
	if !ok {
		return verdict{mode, false, "it has no admission entry"}
	}
	// Catalog admission proves every inventory gap is either pragmatically
	// resolved or carried as a release blocker (validatePragmaticAdmission), so
	// a gap absent from the blockers is a gap the campaign resolved. That is
	// read here rather than re-derived: re-deriving it would be a second
	// implementation of the resolution rules and a second thing to be wrong.
	if containsString(admitted.ReleaseBlockers, signal) {
		return verdict{mode, false, signal + " is an unresolved release blocker"}
	}
	return verdict{mode, true, ""}
}

func (v evidenceView) dnsBidirectionalState(key catalogparity.SurfaceKey) verdict {
	const mode = EvidenceDNSBidirectionalState
	if key.Kind != catalogparity.ManagedResource || key.Name != "unifi_dns_record" {
		return verdict{mode, false, "the M3 lifecycle receipt does not cover this surface"}
	}
	if !v.dns.BidirectionalAdapterStateRoundTrip || !v.dns.V0IntegerTTLStateUpgrade || !v.dns.Import {
		return verdict{mode, false, "the M3 lifecycle receipt does not carry a bidirectional state round trip"}
	}
	return verdict{mode, true, ""}
}

// dnsListController is the weakest mode here and the one to read carefully.
//
// It asserts NAME-level agreement rather than source-level: the same acceptance
// test names passed on both providers, but from two different scenario sources,
// because this surface's scenario changed. That is genuinely less than
// differential_scenario, which requires one unchanged scenario to have judged
// both.
//
// IT IS ALSO STILL BOUND BY NAME, AND THAT IS A DEFECT I AM FLAGGING RATHER
// THAN HIDING. Nothing in this assertion is specific to dns_record. Measured
// against the committed tree, five surfaces satisfy it -- managed ap_group,
// managed device, managed dns_record, managed wan and list dns_record -- and
// restricting it to one of them by name is the same by-name assignment this
// file exists to remove, in a smaller box. Generalising it is a decision about
// how much evidence a release requires, not a refactor, so it is written down
// here and referred upward rather than taken.
//
// The Tests.Status guard is what keeps it from overlapping
// differential_scenario. Without it both modes hold whenever a scenario is
// unchanged, and the recorded label becomes an artefact of evaluation order.
func (v evidenceView) dnsListController(
	key catalogparity.SurfaceKey,
	surface catalogparity.SurfaceEvidenceInventory,
) verdict {
	const mode = EvidenceDNSListController
	if key.Kind != catalogparity.ListResource || key.Name != "unifi_dns_record" {
		return verdict{mode, false, "the M3 lifecycle receipt does not cover this surface"}
	}
	if surface.Tests.Status != catalogparity.FileChanged {
		return verdict{mode, false,
			"its scenario is unchanged, so differential_scenario is the stronger claim available"}
	}
	planned := v.planned[key]
	if len(planned) == 0 {
		return verdict{mode, false, "the campaign plans no acceptance test for it"}
	}
	for _, name := range planned {
		if _, passed := v.released[name]; !passed {
			return verdict{mode, false, name + " did not pass on the released provider"}
		}
		if _, passed := v.candidate[name]; !passed {
			return verdict{mode, false, name + " did not pass on the candidate provider"}
		}
	}
	return verdict{mode, true, ""}
}

// acceptanceSignal is the name missingTestSignals uses for the acceptance gap
// of each surface kind. Kept in one place so a kind added there and forgotten
// here cannot silently make pragmatic_reference unreachable.
func acceptanceSignal(kind catalogparity.SurfaceKind) string {
	switch kind {
	case catalogparity.ListResource:
		return "list_acceptance"
	case catalogparity.Action:
		return "action_acceptance"
	default:
		return "acceptance"
	}
}
