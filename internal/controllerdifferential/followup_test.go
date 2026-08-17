package controllerdifferential

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ubiquiti-community/terraform-provider-unifi/internal/catalogparity"
)

func campaign(t *testing.T) (CampaignPolicy, CampaignCounts) {
	t.Helper()
	path := filepath.Join(repositoryRoot, "provider-codegen", "policy", "catalog-campaign.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var policy CampaignPolicy
	var counts CampaignCounts
	if err := json.Unmarshal(raw, &policy); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	if err := json.Unmarshal(raw, &counts); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	return policy, counts
}

// TestTheFrozenReceiptIsAFullRun is the acceptance test: the receipt the shell
// produced on pipeline 232, judged by the Go, against the committed campaign
// policy. The shell's answer for that receipt was "full".
func TestTheFrozenReceiptIsAFullRun(t *testing.T) {
	policy, counts := campaign(t)
	got, err := Followup(frozenReceipt(t), policy, counts)
	if err != nil {
		t.Fatalf("the frozen receipt was rejected: %v", err)
	}
	if got != "full" {
		t.Fatalf("classified as %q, want full", got)
	}
}

// TestEachWayToFailTheFollowupFiresOnItsOwn. The shell was ONE jq -e expression
// of fourteen conjuncts that printed nothing and exited 1, so an operator saw a
// failed step and had to re-derive which conjunct was false.
func TestEachWayToFailTheFollowupFiresOnItsOwn(t *testing.T) {
	policy, counts := campaign(t)
	for _, c := range []struct {
		name    string
		wants   string
		corrupt func(r *catalogparity.ControllerDifferentialReceipt)
	}{
		{"the candidate failed a test", "candidate suite reports failures",
			func(r *catalogparity.ControllerDifferentialReceipt) { r.Candidate.Failed = []string{"TestAccOne"} }},
		{"the candidate is missing a test", "candidate suite reports missing tests",
			func(r *catalogparity.ControllerDifferentialReceipt) { r.Candidate.Missing = []string{"TestAccOne"} }},
		{"the candidate suite did not pass", "candidate suite result is",
			func(r *catalogparity.ControllerDifferentialReceipt) { r.Candidate.Result = "fail" }},
		{"the released suite is missing more than the plan allows", "released suite is missing",
			func(r *catalogparity.ControllerDifferentialReceipt) {
				r.Released.Missing = append(append([]string(nil), r.Released.Missing...), "TestAccExtra")
			}},
		{"the released suite failed something unaccepted", "not a limitation",
			func(r *catalogparity.ControllerDifferentialReceipt) { r.Released.Failed = []string{"TestAccOne"} }},
		{"the released suite reports an unexpected failure", "unexpected failures",
			func(r *catalogparity.ControllerDifferentialReceipt) {
				r.Released.UnexpectedFailures = []string{"TestAccOne"}
			}},
		{"the plan covers a different number of surfaces", "the campaign declares",
			func(r *catalogparity.ControllerDifferentialReceipt) { r.Plan.SurfaceCount = 66 }},
		{"the plan reports a different gap count", "evidence gap",
			func(r *catalogparity.ControllerDifferentialReceipt) { r.Plan.EvidenceGapCount = 5 }},
		{"a full run covers fewer tests than the campaign", "full run covers",
			func(r *catalogparity.ControllerDifferentialReceipt) {
				r.Plan.TestNames = r.Plan.TestNames[:len(r.Plan.TestNames)-1]
			}},
		{"a disposition was dropped", "allowed_skips in the plan",
			func(r *catalogparity.ControllerDifferentialReceipt) { r.Plan.AllowedSkips = nil }},
		// The wording moved when this check was delegated to
		// catalogparity.RequireControllerResultAgreesWithGaps, the single home
		// of the gap-count-to-result rule. Asserting "must report" rather than
		// the old "; want" keeps this about the message NAMING the required
		// result, not about one phrasing of it.
		{"the result disagrees with the gap count", "must report",
			func(r *catalogparity.ControllerDifferentialReceipt) { r.Result = "pass" }},
	} {
		t.Run(c.name, func(t *testing.T) {
			receipt := frozenReceipt(t)
			c.corrupt(&receipt)
			_, err := Followup(receipt, policy, counts)
			if err == nil {
				t.Fatalf("accepted; expected a refusal mentioning %q", c.wants)
			}
			if !strings.Contains(err.Error(), c.wants) {
				t.Fatalf("refused with %q, which does not mention %q. A gate that will not say "+
					"which of its conjuncts is false makes the operator re-derive it", err, c.wants)
			}
		})
	}
}

// TestTheResultMustAGREEWithTheGapCountRatherThanBeBlocked is the deliberate
// divergence, and the reason for it.
//
// The shell required result == "blocked_evidence" outright. That is true today
// -- the campaign has six declared gaps -- and it means the gate FAILS ON
// SUCCESS: close the gaps, set the policy count to zero, and the run produces
// "pass" while the check still demands "blocked_evidence". A check that breaks
// at the moment the thing it guards starts working is one somebody deletes
// rather than reads.
func TestTheResultMustAGREEWithTheGapCountRatherThanBeBlocked(t *testing.T) {
	policy, counts := campaign(t)

	// Today's state: gaps outstanding, so blocked_evidence is required.
	blocked := frozenReceipt(t)
	if blocked.Plan.EvidenceGapCount == 0 {
		t.Fatal("the frozen run has no evidence gaps, so this test no longer contrasts anything")
	}
	blocked.Result = "pass"
	if _, err := Followup(blocked, policy, counts); err == nil {
		t.Fatal("a run claiming pass with outstanding evidence gaps was accepted")
	}

	// The campaign finished: no gaps anywhere, and the result must be pass.
	finished := frozenReceipt(t)
	finished.Result = "pass"
	finished.Plan.EvidenceGapCount = 0
	for i := range finished.Plan.Surfaces {
		finished.Plan.Surfaces[i].MissingSignals = []string{}
	}
	counts.EvidenceGapCount = 0
	if _, err := Followup(finished, policy, counts); err != nil {
		t.Fatalf("a COMPLETED campaign was rejected: %v.\nThe shell required blocked_evidence "+
			"outright, so this is exactly the run it would have failed", err)
	}
}

// TestADiagnosticRunMustBeAProperNarrowing. Zero tests is a run that did
// nothing; the whole catalog is a full run wearing a diagnostic label, and
// calling either complete would let a narrowing that narrowed nothing stand in
// for the campaign.
func TestADiagnosticRunMustBeAProperNarrowing(t *testing.T) {
	policy, counts := campaign(t)
	base := frozenReceipt(t)
	base.Plan.DiagnosticSelection = true
	base.Plan.CatalogTestCount = counts.TestNameCount

	narrowed := base
	narrowed.Plan.TestNames = base.Plan.TestNames[:1]
	got, err := Followup(narrowed, policy, counts)
	if err != nil {
		t.Fatalf("a properly narrowed diagnostic run was rejected: %v", err)
	}
	if got != "diagnostic_complete" {
		t.Fatalf("classified as %q, want diagnostic_complete", got)
	}

	empty := base
	empty.Plan.TestNames = []string{}
	if _, err := Followup(empty, policy, counts); err == nil {
		t.Fatal("a diagnostic run that selected no tests was accepted as complete")
	}

	whole := base
	if _, err := Followup(whole, policy, counts); err == nil {
		t.Fatal("a diagnostic run covering the whole catalog was accepted as complete, so a " +
			"narrowing that narrowed nothing could stand in for the campaign")
	}

	stale := base
	stale.Plan.TestNames = base.Plan.TestNames[:1]
	stale.Plan.CatalogTestCount = counts.TestNameCount - 1
	if _, err := Followup(stale, policy, counts); err == nil {
		t.Fatal("a diagnostic run narrowing a catalog that is not the campaign's was accepted")
	}
}

// TestTheDispositionComparisonIsASet is the second deliberate divergence: the
// shell compared jq arrays, so reordering the policy file would have failed the
// gate with a message about dispositions.
func TestTheDispositionComparisonIsASet(t *testing.T) {
	policy, counts := campaign(t)
	receipt := frozenReceipt(t)
	reversed := append([]string(nil), receipt.Plan.AllowedSkips...)
	for i, j := 0, len(reversed)-1; i < j; i, j = i+1, j-1 {
		reversed[i], reversed[j] = reversed[j], reversed[i]
	}
	if len(reversed) < 2 {
		t.Skip("the campaign declares fewer than two allowed skips, so order cannot differ")
	}
	receipt.Plan.AllowedSkips = reversed
	if _, err := Followup(receipt, policy, counts); err != nil {
		t.Fatalf("reordering the allowed-skip list was rejected: %v.\nThe comparison is about "+
			"which tests are dispositioned, not about the order somebody wrote them in", err)
	}
}

const followupScript = "../../.woodpecker/scripts/catalog-controller-followup.sh"

// TestFollowupAgreesWithTheShell runs the deployed script, not a copy, on the
// two classifications a live pipeline produces.
//
// DELETE THIS TEST IN THE COMMIT THAT DELETES THE SCRIPT. It does not skip when
// the script is missing.
//
// It compares the two shapes the shell can express -- the word it prints, and
// whether it exited zero. It cannot compare messages, because on a rejection
// the shell prints NOTHING AT ALL: it is one jq -e expression of fourteen
// conjuncts, so a failed run tells an operator only that one of them was false.
func TestFollowupAgreesWithTheShell(t *testing.T) {
	if _, err := os.Stat(followupScript); err != nil {
		t.Fatalf("%s is gone, so this comparison measures nothing: %v.\nIf it was deleted "+
			"deliberately, delete this test in the same commit.", followupScript, err)
	}
	policy, counts := campaign(t)

	full := frozenReceipt(t)
	diagnostic := frozenReceipt(t)
	diagnostic.Plan.DiagnosticSelection = true
	diagnostic.Plan.CatalogTestCount = counts.TestNameCount
	diagnostic.Plan.TestNames = diagnostic.Plan.TestNames[:1]

	for _, c := range []struct {
		name    string
		receipt catalogparity.ControllerDifferentialReceipt
	}{
		{"a full campaign run", full},
		{"a narrowed diagnostic run", diagnostic},
	} {
		t.Run(c.name, func(t *testing.T) {
			byShell := runFollowupScript(t, c.receipt)
			byGo, err := Followup(c.receipt, policy, counts)
			if err != nil {
				byGo = ""
			}
			if byShell != byGo {
				t.Fatalf("shell says %q, Go says %q (err %v)", byShell, byGo, err)
			}
		})
	}
}

// TestTheShellFollowupFailsOnACompletedCampaign records the divergence by
// MEASURING it rather than reasoning about it.
//
// A campaign with its evidence gaps closed produces result "pass". The shell
// requires "blocked_evidence" outright, so it rejects exactly the run the
// campaign is working towards -- and rejects it silently, with exit 1 and no
// output. Run here so the claim in Followup's comment is a measurement.
func TestTheShellFollowupFailsOnACompletedCampaign(t *testing.T) {
	if _, err := os.Stat(followupScript); err != nil {
		t.Fatalf("%s is gone; delete this test in the same commit: %v", followupScript, err)
	}
	policy, counts := campaign(t)

	finished := frozenReceipt(t)
	finished.Result = "pass"
	finished.Plan.EvidenceGapCount = 0
	for i := range finished.Plan.Surfaces {
		finished.Plan.Surfaces[i].MissingSignals = []string{}
	}
	counts.EvidenceGapCount = 0

	if verdict := runFollowupScriptWithCounts(t, finished, counts); verdict != "" {
		t.Fatalf("the shell accepted a completed campaign with %q; this test exists because it "+
			"does not, and the divergence in Followup is written on that basis", verdict)
	}
	if _, err := Followup(finished, policy, counts); err != nil {
		t.Fatalf("the Go rejected a completed campaign too: %v", err)
	}
}

func runFollowupScript(t *testing.T, receipt catalogparity.ControllerDifferentialReceipt) string {
	t.Helper()
	_, counts := campaign(t)
	return runFollowupScriptWithCounts(t, receipt, counts)
}

// runFollowupScriptWithCounts returns the word the script printed, or "" when
// it refused.
func runFollowupScriptWithCounts(t *testing.T, receipt catalogparity.ControllerDifferentialReceipt, counts CampaignCounts) string {
	t.Helper()
	work := t.TempDir()

	receiptPath := filepath.Join(work, "receipt.json")
	encoded, err := json.Marshal(receipt)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(receiptPath, encoded, 0o600); err != nil {
		t.Fatal(err)
	}

	// The policy the script reads must carry the counts this case is about, so
	// a completed-campaign case is judged against a completed campaign's policy
	// rather than against today's.
	rawPolicy, err := os.ReadFile(filepath.Join(repositoryRoot, "provider-codegen", "policy", "catalog-campaign.json"))
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err := json.Unmarshal(rawPolicy, &document); err != nil {
		t.Fatal(err)
	}
	document["surface_count"] = counts.SurfaceCount
	document["evidence_gap_count"] = counts.EvidenceGapCount
	document["test_name_count"] = counts.TestNameCount
	policyPath := filepath.Join(work, "campaign.json")
	adjusted, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(policyPath, adjusted, 0o600); err != nil {
		t.Fatal(err)
	}

	command := exec.Command("bash", followupScript, receiptPath)
	command.Env = append(os.Environ(), "CATALOG_CAMPAIGN_POLICY="+policyPath)
	out, err := command.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// TestMissingFEWERThanDeclaredIsAlsoARefusal is the direction nothing covered,
// found by evidence's adversarial pass over this branch.
//
// The sibling comparison three lines up treats released_allowed_failures as a
// SUBSET -- failing fewer than licensed is fine -- so the exactness here read as
// an accident until somebody asked. It is not: allowed_failures is a licence
// and allowed_missing is a CLAIM ABOUT THE RELEASED TREE. A test that was
// declared absent and turns out to run means the plan describes something other
// than the tree it is about, and everything downstream reads that plan.
//
// Without this test, changing the comparison to a subset would break nothing.
func TestMissingFEWERThanDeclaredIsAlsoARefusal(t *testing.T) {
	policy, counts := campaign(t)
	receipt := frozenReceipt(t)
	if len(receipt.Released.Missing) < 2 {
		t.Fatalf("the frozen released suite is missing %d test(s); this test needs at least two "+
			"to remove one and still have a set", len(receipt.Released.Missing))
	}

	// One of the declared-missing tests turns out to run and pass. Nothing about
	// the campaign got worse; the plan stopped describing the tree.
	recovered := receipt.Released.Missing[0]
	receipt.Released.Missing = receipt.Released.Missing[1:]
	receipt.Released.Passed = append(append([]string(nil), receipt.Released.Passed...), recovered)

	_, err := Followup(receipt, policy, counts)
	if err == nil {
		t.Fatalf("a released suite missing FEWER tests than the plan declares was accepted. %q "+
			"ran on a tree the plan says does not contain it, so the plan is describing "+
			"something else", recovered)
	}
	if !strings.Contains(err.Error(), "missing") {
		t.Fatalf("refused with %q, which does not name the disagreement", err)
	}
}
