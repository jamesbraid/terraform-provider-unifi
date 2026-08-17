package controllerdifferential

import (
	"bytes"
	"testing"

	"github.com/ubiquiti-community/terraform-provider-unifi/internal/catalogparity"
)

// TestBuildReceiptReproducesTheFrozenReceipt feeds the constructor what pipeline
// 232 measured and requires it to emit that pipeline's receipt -- all 1271 lines
// of it, plan and both suites included.
func TestBuildReceiptReproducesTheFrozenReceipt(t *testing.T) {
	frozen := frozenReceipt(t)
	rebuilt := BuildReceipt(runFrom(frozen))

	want, err := catalogparity.MarshalReceipt(frozen)
	if err != nil {
		t.Fatal(err)
	}
	got, err := catalogparity.MarshalReceipt(rebuilt)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(want, got) {
		t.Fatalf("the constructor does not reproduce the shell's receipt.\nfrozen:\n%s\nrebuilt:\n%s", want, got)
	}
}

// TestTheThreeResultsAreThreeDifferENTSTATEMENTS. blocked_evidence is the one
// that matters: the differential SUCCEEDED and the release is still blocked.
// Collapsing it into either neighbour loses the difference between "the
// comparison went wrong" and "the comparison went right and the catalog is
// incomplete", which are acted on by different people.
func TestTheThreeResultsAreThreeDifferentStatements(t *testing.T) {
	pass := catalogparity.ControllerSuiteReceipt{Result: "pass"}
	limited := catalogparity.ControllerSuiteReceipt{Result: "accepted_limitation"}
	failed := catalogparity.ControllerSuiteReceipt{Result: "fail"}

	for _, c := range []struct {
		name      string
		released  catalogparity.ControllerSuiteReceipt
		candidate catalogparity.ControllerSuiteReceipt
		gaps      int
		want      string
	}{
		{"both clean, no gaps", pass, pass, 0, "pass"},
		{"both clean, gaps outstanding", pass, pass, 6, "blocked_evidence"},
		{"released within its allowance, gaps outstanding", limited, pass, 6, "blocked_evidence"},
		{"released within its allowance, no gaps", limited, pass, 0, "pass"},
		{"the candidate fell short", pass, failed, 0, "fail"},
		{"the released side fell short", failed, pass, 0, "fail"},
		{"both fell short", failed, failed, 0, "fail"},
		// A failing suite outranks a clean catalog: the gap count says nothing
		// about whether the comparison worked.
		{"the candidate fell short with gaps outstanding", pass, failed, 6, "fail"},
	} {
		t.Run(c.name, func(t *testing.T) {
			got := BuildReceipt(Run{
				Plan:      catalogparity.ControllerPlanReceipt{EvidenceGapCount: c.gaps},
				Released:  c.released,
				Candidate: c.candidate,
			}).Result
			if got != c.want {
				t.Fatalf("result = %q, want %q", got, c.want)
			}
		})
	}
}

// TestTheFrozenRunIsBlockedEvidence states what the reproduction above
// exercises. If the frozen run were a plain pass, a constructor that wrote
// "pass" unconditionally would satisfy it.
func TestTheFrozenRunIsBlockedEvidence(t *testing.T) {
	frozen := frozenReceipt(t)
	if frozen.Result != "blocked_evidence" {
		t.Fatalf("the frozen run is %q; the reproduction no longer exercises the branch the "+
			"campaign is actually in", frozen.Result)
	}
	if frozen.Released.Result != "accepted_limitation" {
		t.Fatalf("the frozen released suite is %q, so the acceptable-shortfall branch is "+
			"unexercised", frozen.Released.Result)
	}
}

// TestTheEnvironmentIsRecordedByIDNotOnlyByTag. A tag is a name somebody can
// move: two runs of "the controller image" are the same run only if the IDs
// agree, and the tag alone cannot say so.
func TestTheEnvironmentIsRecordedByIDNotOnlyByTag(t *testing.T) {
	frozen := frozenReceipt(t)
	for _, c := range []struct{ what, tag, id string }{
		{"controller", frozen.Target.Image, frozen.Target.ImageID},
		{"synthetic fleet", frozen.Fleet.Image, frozen.Fleet.ImageID},
		{"ryuk", frozen.Testcontainers.RyukImage, frozen.Testcontainers.RyukImageID},
	} {
		if c.tag == "" || c.id == "" {
			t.Fatalf("the %s image is recorded as tag %q id %q; a receipt that names only the "+
				"tag cannot say which image ran", c.what, c.tag, c.id)
		}
		if c.tag == c.id {
			t.Fatalf("the %s image's tag and id are the same value, so one of them is not "+
				"being measured", c.what)
		}
	}
	if frozen.Target.PullPolicy != "never" {
		t.Fatalf("pull policy is %q; this gate refuses an image that is not already present, "+
			"and recording the policy is what makes that refusal auditable afterwards",
			frozen.Target.PullPolicy)
	}
}

func runFrom(receipt catalogparity.ControllerDifferentialReceipt) Run {
	return Run{
		Plan:            receipt.Plan,
		PlanSHA256:      receipt.PlanSHA256,
		Released:        receipt.Released,
		Candidate:       receipt.Candidate,
		ReleasedCommit:  receipt.ReleasedCommit,
		CandidateCommit: receipt.CandidateCommit,
		TreeState:       receipt.TreeState,
		Environment: Environment{
			ControllerImage:       receipt.Target.Image,
			ControllerImageID:     receipt.Target.ImageID,
			SyntheticImage:        receipt.Fleet.Image,
			SyntheticImageID:      receipt.Fleet.ImageID,
			RyukImage:             receipt.Testcontainers.RyukImage,
			RyukImageID:           receipt.Testcontainers.RyukImageID,
			HerderSHA256:          receipt.Fleet.HerderSHA256,
			TerraformBinarySHA256: receipt.TerraformBinarySHA256,
		},
	}
}
