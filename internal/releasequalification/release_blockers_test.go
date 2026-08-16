package releasequalification

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/ubiquiti-community/terraform-provider-unifi/internal/catalogparity"
)

// TestAcceptedReleaseBlockersMatchTheCampaignPolicy is the freshness check on
// acceptedReleaseBlockers.
//
// That variable is a release decision written out by hand, for the reason given
// where it is declared: it is the only thing in this gate that pins WHICH gaps
// are acceptable, and deriving it from an input would make it compare that input
// against itself. The cost of writing a decision down is that it goes stale, and
// a stale constant in a gate nobody runs goes stale silently.
//
// So it is checked against the file that actually records the decision,
// provider-codegen/policy/catalog-campaign.json, read here rather than restated.
// The gate cannot read that file itself -- the policy is not one of its eight
// inputs, and making it one is a change to the gate's contract rather than a
// repair -- so the binding lives in a test, which is where this package already
// binds its other declared constants to their sources.
//
// THIS TEST IS EXPECTED TO FAIL ON THE TREE THAT INTRODUCED IT, AND THAT IS THE
// POINT. The gate expects one blocker; the policy declares two. Deciding whether
// the second one is a thing this project ships with is not a decision a test can
// make, so the test states it and stops. A loud stale constant beats a silent
// one.
func TestAcceptedReleaseBlockersMatchTheCampaignPolicy(t *testing.T) {
	const policyPath = "../../provider-codegen/policy/catalog-campaign.json"

	data, err := os.ReadFile(policyPath)
	if err != nil {
		t.Fatal(err)
	}
	var policy catalogparity.CampaignPolicy
	if err := json.Unmarshal(data, &policy); err != nil {
		t.Fatal(err)
	}
	// Without this the comparison would pass against a policy that declares
	// nothing, which is indistinguishable from a policy this test could not
	// read -- and "no accepted gaps" is exactly the shape a truncated or
	// renamed key produces.
	if len(policy.AcceptedEvidenceGaps) == 0 {
		t.Fatalf("%s declares no accepted_evidence_gaps; either the key moved or the file was "+
			"truncated, and this test would otherwise agree with an empty set", policyPath)
	}

	declared := map[catalogparity.EvidenceGap]string{}
	for _, gap := range policy.AcceptedEvidenceGaps {
		declared[gap.Gap()] = gap.Reason
	}
	pinned := map[catalogparity.EvidenceGap]struct{}{}
	for _, gap := range acceptedReleaseBlockers {
		pinned[gap] = struct{}{}
	}

	var missing, extra []string
	for gap, reason := range declared {
		if _, ok := pinned[gap]; !ok {
			missing = append(missing, fmt.Sprintf("%s/%s %s\n            reason given in the policy: %s",
				gap.Kind, gap.Name, gap.Signal, reason))
		}
	}
	for gap := range pinned {
		if _, ok := declared[gap]; !ok {
			extra = append(extra, fmt.Sprintf("%s/%s %s", gap.Kind, gap.Name, gap.Signal))
		}
	}
	sort.Strings(missing)
	sort.Strings(extra)

	if len(missing) > 0 {
		t.Errorf("the campaign policy accepts %d evidence gap(s) that this gate does not, so "+
			"validateReleaseManagement would REFUSE the committed catalog:\n\n        %s\n\n"+
			"    THIS IS A DECISION, NOT A TYPO. Adding one to acceptedReleaseBlockers (release.go)\n"+
			"    asserts that the release ships with that gap unresolved. If that is right, add it\n"+
			"    there and move ResolvedReleaseBlockers at release.go:261 to match the new count.\n"+
			"    If it is not right, the gap has to be closed or the policy has to stop accepting it.\n"+
			"    Either way the gate and the policy currently disagree about what we are willing to\n"+
			"    ship, and the gate is the one that would say no.",
			len(missing), strings.Join(missing, "\n        "))
	}
	if len(extra) > 0 {
		t.Errorf("this gate accepts %d evidence gap(s) the campaign policy no longer declares:\n    %s\n\n"+
			"    A gap that has been closed must stop being accepted here, or the gate keeps a\n"+
			"    permission nobody asked for.",
			len(extra), strings.Join(extra, "\n    "))
	}

	t.Logf("gate pins %d accepted release blocker(s); the policy declares %d",
		len(pinned), len(declared))
}
