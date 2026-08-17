package catalogparity

import (
	"strings"
	"testing"
)

// TestACompletedCampaignIsAcceptable is the case the whole release path could
// not express.
//
// Six sites required the literal "blocked_evidence", so the moment the evidence
// gaps closed and the producer emitted "pass", admission, hardware disposition
// and migration recovery would all have rejected it. The success state was
// unreachable through six separate doors, and fixing one moved the wall.
func TestACompletedCampaignIsAcceptable(t *testing.T) {
	finished := ControllerDifferentialReceipt{Result: "pass"}
	finished.Plan.EvidenceGapCount = 0
	if err := RequireControllerResultAgreesWithGaps(finished); err != nil {
		t.Fatalf("a completed campaign was rejected: %v.\nThis is the run the campaign is "+
			"working towards", err)
	}
}

func TestTheResultMustFollowFromTheGapCount(t *testing.T) {
	for _, c := range []struct {
		name   string
		result string
		gaps   int
		accept bool
		says   string
	}{
		{"gaps outstanding, blocked", "blocked_evidence", 6, true, ""},
		{"no gaps, pass", "pass", 0, true, ""},
		// The two disagreements are the point. A run claiming to be blocked
		// with nothing blocking it, and a run claiming to pass with six
		// surfaces carrying no acceptance signal at all.
		{"no gaps, still claims blocked", "blocked_evidence", 0, false, `must report "pass"`},
		{"gaps outstanding, claims pass", "pass", 6, false, `must report "blocked_evidence"`},
		{"a result from nowhere", "fine", 6, false, `is "fine"`},
		{"the empty result", "", 0, false, "must report"},
	} {
		t.Run(c.name, func(t *testing.T) {
			receipt := ControllerDifferentialReceipt{Result: c.result}
			receipt.Plan.EvidenceGapCount = c.gaps
			err := RequireControllerResultAgreesWithGaps(receipt)
			if c.accept {
				if err != nil {
					t.Fatalf("rejected: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("accepted a result that does not follow from the gap count")
			}
			if !strings.Contains(err.Error(), c.says) {
				t.Fatalf("the refusal %q does not say %q, so a reader cannot tell which side is "+
					"wrong -- the count or the verdict", err, c.says)
			}
		})
	}
}

// TestTheRefusalNamesBothNumbers. A gate that says only "result is blocked" makes
// the reader go and find the gap count themselves, and the whole reason this
// rule exists is that the two were being compared to a constant instead of to
// each other.
func TestTheRefusalNamesBothNumbers(t *testing.T) {
	receipt := ControllerDifferentialReceipt{Result: "pass"}
	receipt.Plan.EvidenceGapCount = 6
	err := RequireControllerResultAgreesWithGaps(receipt)
	if err == nil {
		t.Fatal("accepted")
	}
	for _, want := range []string{`"pass"`, "6 evidence gap", "blocked_evidence"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("the refusal %q does not mention %s", err, want)
		}
	}
}

func TestControllerResultForGapsIsTotal(t *testing.T) {
	if got := ControllerResultForGaps(0); got != "pass" {
		t.Fatalf("no gaps -> %q", got)
	}
	for _, gaps := range []int{1, 6, 67} {
		if got := ControllerResultForGaps(gaps); got != "blocked_evidence" {
			t.Fatalf("%d gap(s) -> %q", gaps, got)
		}
	}
	// A negative count is not a state any producer reaches, and the rule must
	// still be defined rather than fall through to "pass" -- which is the
	// direction that would let a corrupt receipt read as a complete campaign.
	if got := ControllerResultForGaps(-1); got == "pass" {
		t.Fatal("a negative gap count reported as a completed campaign")
	}
}
