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
)

// dns_bidirectional_state and dns_list_controller used to be modes here, one
// per dns_record surface, and they have been deleted rather than kept.
//
// Their evidence was the M3 lifecycle receipt, and validateDNSLifecycle already
// requires all thirteen of its checks before ANY receipt is built. Inside a
// valid migration/recovery receipt those checks are therefore true for every
// surface, so a mode predicated on them could not fail. That is the same
// argument this file makes against a schema-equivalence mode, and it does not
// get weaker when applied to modes already written.
//
// What justifies dns_record is what justifies the other converted surfaces:
// its acceptance tests are byte-identical to the released provider's and passed
// on both. That is differential_scenario, and dns_record qualifies for it
// directly once scenarios are compared per test rather than per file. The M3
// receipt still gates the whole artifact; it simply is not a per-surface
// distinction.
//
// It also settled a question that had been parked as "should the weaker
// dns_list_controller assertion generalise to the four other surfaces that
// satisfy it". The answer was not to generalise it but that it should not
// exist: all five have byte-identical acceptance tests and qualify for the
// strong mode. The weak one was compensating for file-level granularity.
//
// The lifecycle provenance is deliberately NOT re-added as a per-surface field.
// The fact it would record is "the M3 receipt is about dns_record", which the
// M3 receipt itself does not say -- it carries no surface key. Writing it here
// by name would give a missing fact a second home in a different artifact,
// which is the failure class this project keeps paying for. The fix belongs
// with the M3 receipt's producer.

// evidenceView is every measured fact the mode rules read, indexed once per
// receipt rather than re-scanned per surface.
type evidenceView struct {
	inventory map[catalogparity.SurfaceKey]catalogparity.SurfaceEvidenceInventory
	admission map[catalogparity.SurfaceKey]catalogparity.SurfaceAdmission
	planned   map[catalogparity.SurfaceKey][]string
	released  map[string]struct{}
	candidate map[string]struct{}
	// declaredMissing holds the tests the campaign policy states the released
	// provider cannot run. Membership excuses a test from the RELEASED half of
	// differential_scenario and from nothing else.
	declaredMissing map[string]struct{}
}

func newEvidenceView(input MigrationRecoveryInput) evidenceView {
	view := evidenceView{
		inventory: make(map[catalogparity.SurfaceKey]catalogparity.SurfaceEvidenceInventory, len(input.Inventory.Surfaces)),
		admission: make(map[catalogparity.SurfaceKey]catalogparity.SurfaceAdmission, len(input.Admission.Surfaces)),
		planned:   make(map[catalogparity.SurfaceKey][]string, len(input.Controller.Plan.Surfaces)),
		released:  make(map[string]struct{}, len(input.Controller.Released.Passed)),
		candidate: make(map[string]struct{}, len(input.Controller.Candidate.Passed)),
		declaredMissing: make(
			map[string]struct{}, len(input.Controller.Plan.ReleasedAllowedMissing),
		),
	}
	for _, name := range input.Controller.Plan.ReleasedAllowedMissing {
		view.declaredMissing[name] = struct{}{}
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
	// refines names a mode whose preconditions this one wholly contains. It is
	// how two modes can both hold without the label being arbitrary: a mode
	// that requires everything another requires AND MORE is the more specific
	// true statement, so choosing it is a fact about the rules rather than
	// about the order they happen to be evaluated in.
	//
	// This is NOT a tie-breaker for overlapping rules. If two modes hold and
	// neither contains the other, the label really would be arbitrary and the
	// gate fails instead.
	//
	// NOTE TO WHOEVER ADDS THE NEXT MODE. Assignment by name has now been found
	// three times on this gate: originally, when every surface was stamped
	// source_identity; again when differential_scenario and dns_list_controller
	// both held for list dns_record and the label would have fallen out of
	// evaluation order; and a third time INSIDE THE FIX FOR THE SECOND, when
	// comparing scenarios per test made dns_record satisfy two modes at once.
	// The defect is not a mistake anyone made once. Adding a mode without
	// checking what else can hold for the same surface is how it comes back.
	refines string
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
	}

	held := make([]string, 0, 1)
	refines := map[string]string{}
	refused := make([]string, 0, len(verdicts))
	for _, ruling := range verdicts {
		if ruling.holds {
			held = append(held, ruling.mode)
			refines[ruling.mode] = ruling.refines
			continue
		}
		refused = append(refused, ruling.mode+": "+ruling.because)
	}
	if len(held) == 0 {
		return "", fmt.Errorf(
			"surface %s/%s has no evidence mode; every mode refused it (%s)",
			key.Kind, key.Name, strings.Join(refused, "; "),
		)
	}
	if len(held) == 1 {
		return held[0], nil
	}
	// More than one holds. That is only acceptable when one of them is a strict
	// refinement of all the others -- it required everything they required and
	// more -- in which case it is the most specific true statement and there is
	// nothing arbitrary about recording it. Anything else is genuinely
	// ambiguous and fails.
	mostSpecific := make([]string, 0, 1)
	for _, candidate := range held {
		covers := true
		for _, other := range held {
			if other != candidate && !modeRefines(refines, candidate, other) {
				covers = false
				break
			}
		}
		if covers {
			mostSpecific = append(mostSpecific, candidate)
		}
	}
	if len(mostSpecific) == 1 {
		return mostSpecific[0], nil
	}
	sort.Strings(held)
	return "", fmt.Errorf(
		"surface %s/%s matches %d evidence modes (%s) and none refines the rest; "+
			"the rules overlap and the label would be arbitrary",
		key.Kind, key.Name, len(held), strings.Join(held, ", "),
	)
}

// modeRefines reports whether mode reaches target by following refinements. The
// chain is walked rather than compared once, so a mode refining a mode that
// refines a third is still resolvable, and a cycle terminates instead of
// hanging.
func modeRefines(refines map[string]string, mode, target string) bool {
	seen := map[string]struct{}{}
	for current := refines[mode]; current != ""; current = refines[current] {
		if current == target {
			return true
		}
		if _, loop := seen[current]; loop {
			return false
		}
		seen[current] = struct{}{}
	}
	return false
}

// sourceIdentity holds when the surface's runtime file is byte-identical to the
// released provider's, and holding means the released result carries over: this
// surface needs no new evidence.
//
// THAT IS ONLY TRUE WHILE THE RUNTIME FILE CONTAINS THE THING THAT DETERMINES
// THE SCHEMA, AND FOR 38 OF 41 SURFACES IT NO LONGER DOES.
//
// evidencePaths returns one path per surface -- unifi/<base>.go -- and nothing
// from internal/generated. Since the conversion, 38 of the 41 files in unifi/
// that assign resp.Schema assign it from a generated package. For dns_record the
// digested wrapper holds 4 schema-related lines; the generated package holds 120.
//
// Reproduced rather than reasoned about. A practitioner-visible change was made
// in internal/generated/resource_dns_record -- an attribute stopped being
// Required -- and the file evidencePaths actually hashes was measured before and
// after:
//
//	before  3ffe7e13167de315
//	after   3ffe7e13167de315
//
// Identical. So the surface is stamped source_identity and inherits evidence from
// a released provider that served a different schema.
//
// THE EXPOSURE IS ZERO TODAY AND IT EXPIRES AT THE NEXT RELEASE. Only three
// surfaces currently reach this verdict -- unifi_account as managed resource and
// data source, and unifi_setting -- and none of them has a generated package, so
// nothing is inheriting anything it should not. That is an accident of timing,
// not a safeguard: the conversion edited all 64 other wrappers, so their runtime
// is CHANGED and differentialScenario takes them instead.
//
// Once this release is the baseline those wrappers stop changing. From then on
// any schema-only edit -- regenerate a policy, the generated package moves, the
// wrapper does not -- lands here and reports that no new evidence is needed.
// That is the most common change this project makes.
//
// d74ccb13 did NOT close this. It changed how differentialScenario judges a
// converted surface; it did not change whether this verdict is consulted. All
// three modes are evaluated unconditionally above, and differentialScenario
// explicitly defers to this one when the runtime is unchanged.
//
// The fix is to cover each surface's generated package. It is a change to the
// inventory's SHAPE -- more than one runtime path per surface -- in an artifact
// strict consumers read, so it goes consumer-first, then producer, then re-pin.
// Doing it in one step is the break recorded in task 122, with our name on it.
func (v evidenceView) sourceIdentity(surface catalogparity.SurfaceEvidenceInventory) verdict {
	if surface.Runtime.Status != catalogparity.FileIdentical {
		return verdict{mode: EvidenceSourceIdentity, because: fmt.Sprintf(
			"runtime %s is %s against the released provider", surface.Runtime.Path, surface.Runtime.Status)}
	}
	return verdict{mode: EvidenceSourceIdentity, holds: true}
}

func (v evidenceView) differentialScenario(
	key catalogparity.SurfaceKey,
	surface catalogparity.SurfaceEvidenceInventory,
) verdict {
	const mode = EvidenceDifferentialScenario
	if surface.Runtime.Status != catalogparity.FileChanged {
		return verdict{mode: mode, because: "runtime is unchanged, so source_identity covers it"}
	}
	planned := v.planned[key]
	if len(planned) == 0 {
		return verdict{mode: mode, because: "the campaign plans no acceptance test for it"}
	}
	// A declared test is excused below, so a surface whose every planned test is
	// declared would reach the loops with nothing left to check and fall out
	// holding the mode on no comparison at all.
	//
	// This is not hypothetical. Measured on this tree, fifteen surfaces plan
	// exactly one acceptance test and that test is already declared missing --
	// firewall_policy, firewall_zone, site_to_site_vpn, device, power_supervisor
	// and ten more. Excusing the only test hands them the strongest mode on the
	// strength of nothing, which is the check-that-cannot-fail this file already
	// refuses elsewhere: "A check that cannot fail is decoration."
	//
	// The excuse is for a surface that keeps a scenario speaking for both
	// providers, not for a surface that has none.
	surviving := 0
	for _, name := range planned {
		if _, declared := v.declaredMissing[name]; !declared {
			surviving++
		}
	}
	if surviving == 0 {
		return verdict{mode: mode, because: "every acceptance test the campaign plans for it is declared " +
			"missing from the released provider, so no scenario compares the two"}
	}
	// Identity is required PER ACCEPTANCE TEST, not per file.
	//
	// The scenario owner holds acceptance tests, which drive the provider
	// through HCL, beside unit tests, which reach into provider internals.
	// Converting a surface changes the unit tests by necessity and need not
	// touch a single acceptance test -- but the file digest moves, and under a
	// file-level rule every scenario in it lost its standing. Measured on this
	// tree, eight of the nine acceptance tests that survive from the released
	// provider are byte-identical to it, and all eight sit in files marked
	// changed. They were being disqualified by their file-mates.
	//
	// A byte-identical FILE still settles it immediately, and that is not a
	// fallback: if every byte of the file matches then so does every function
	// in it, which makes file identity a strictly stronger premise than the
	// per-test check it stands in for.
	if surface.Tests.Status != catalogparity.FileIdentical {
		for _, name := range planned {
			if _, declared := v.declaredMissing[name]; declared {
				continue
			}
			scenario, measured := surface.Scenario(name)
			switch {
			case !measured:
				return verdict{mode: mode, because: fmt.Sprintf(
					"the inventory carries no per-scenario comparison for %s, so whether the released "+
						"provider ran this exact test is unmeasured", name)}
			case scenario.Status != catalogparity.ScenarioIdentical:
				return verdict{mode: mode, because: fmt.Sprintf(
					"%s is %s against the released tree, so the two providers were not judged by the "+
						"same stimulus", name, scenario.Status)}
			}
		}
	}
	for _, name := range planned {
		_, declared := v.declaredMissing[name]
		// A test the campaign declares the released provider cannot run is
		// excused the released half of this rule and nothing else.
		//
		// The per-test comparison above exists because acceptance tests were
		// being disqualified by their file-mates: converting a surface moves
		// the file digest without touching a single scenario, and a file-level
		// rule struck all of them down together. A test that is new in the
		// candidate does the same thing one level lower. It cannot be
		// byte-identical to a tree that has never held it and cannot have
		// passed there, so under an undeclared rule two new tests veto the
		// twenty-nine beside them that did run on both. That is the same
		// defect: a scenario losing its standing because of a neighbour rather
		// than because of anything measured about itself.
		//
		// The exclusion is DECLARED, never inferred. It reads the campaign
		// policy's released_allowed_missing, which a reviewer can see and which
		// admission already checks the receipt against, so widening it is an
		// edit to a reviewable list rather than a silent consequence of adding
		// a test.
		//
		// It excuses only the released side. The candidate check below still
		// applies to a declared test, so a declared test that fails on the
		// candidate still denies the mode -- the declaration says "released
		// never had this", not "do not judge this".
		if _, passed := v.released[name]; !passed && !declared {
			return verdict{mode: mode, because: name + " did not pass on the released provider"}
		}
		if _, passed := v.candidate[name]; !passed {
			return verdict{mode: mode, because: name + " did not pass on the candidate provider"}
		}
	}
	return verdict{mode: mode, holds: true}
}

func (v evidenceView) pragmaticReference(
	key catalogparity.SurfaceKey,
	surface catalogparity.SurfaceEvidenceInventory,
) verdict {
	const mode = EvidencePragmaticReference
	if surface.Runtime.Status != catalogparity.FileChanged {
		return verdict{mode: mode, because: "runtime is unchanged, so source_identity covers it"}
	}
	if surface.Tests.Status != catalogparity.FileIdentical {
		return verdict{mode: mode, because: "scenario owner " + surface.Tests.Path + " changed"}
	}
	signal := acceptanceSignal(key.Kind)
	if !containsString(surface.MissingSignals, signal) {
		return verdict{mode: mode, because: fmt.Sprintf(
			"it has its own %s signal, so a reference to another surface is not what justifies it", signal)}
	}
	admitted, ok := v.admission[key]
	if !ok {
		return verdict{mode: mode, because: "it has no admission entry"}
	}
	// Catalog admission proves every inventory gap is either pragmatically
	// resolved or carried as a release blocker (validatePragmaticAdmission), so
	// a gap absent from the blockers is a gap the campaign resolved. That is
	// read here rather than re-derived: re-deriving it would be a second
	// implementation of the resolution rules and a second thing to be wrong.
	if containsString(admitted.ReleaseBlockers, signal) {
		return verdict{mode: mode, because: signal + " is an unresolved release blocker"}
	}
	return verdict{mode: mode, holds: true}
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
