package releasequalification

import (
	"strings"
	"testing"
)

// The release path used to require the controller differential's result to be
// the literal "blocked_evidence" at six sites, so the success state -- a
// campaign with no evidence gaps left, which the producer reports as "pass" --
// was unreachable through every one of them. Task 160.
//
// THESE TESTS EXIST BECAUSE THE FIX WAS WIRED AND UNEXERCISED. Deleting the
// agreement check from either site here left this package green: every fixture
// supplies a receipt where the result and the gap count already agree, so
// removing the check changed nothing anyone could see. A rule nothing exercises
// is a rule the next refactor removes silently.

// TestMigrationRecoveryAcceptsACompletedCampaign is the real one. Nothing in
// validateMigrationController is about evidence gaps -- it checks identity,
// commit lineage and surface count -- so a completed campaign has no reason to
// be rejected there, and before task 160 it was rejected anyway.
func TestMigrationRecoveryAcceptsACompletedCampaign(t *testing.T) {
	input := validMigrationInput(t)
	if input.Controller.Plan.EvidenceGapCount == 0 {
		t.Fatal("the fixture already has no evidence gaps, so this test contrasts nothing")
	}

	// The campaign finishes: every surface has its signals, so the plan reports
	// no gaps and the producer reports pass.
	input.Controller.Plan.EvidenceGapCount = 0
	for index := range input.Controller.Plan.Surfaces {
		input.Controller.Plan.Surfaces[index].MissingSignals = []string{}
	}
	input.Controller.Result = "pass"

	if err := validateMigrationController(input); err != nil {
		t.Fatalf("a completed campaign was rejected by migration recovery: %v.\n"+
			"This is the state the campaign is working towards, and the release path is the "+
			"thing that must accept it", err)
	}
}

// TestMigrationRecoveryStillRejectsAResultThatDoesNotFollow is the control.
// Without it the test above is satisfied by removing the check entirely.
func TestMigrationRecoveryStillRejectsAResultThatDoesNotFollow(t *testing.T) {
	for _, c := range []struct {
		name   string
		result string
		gaps   int
	}{
		{"claims pass with gaps outstanding", "pass", 1},
		{"claims blocked with no gaps", "blocked_evidence", 0},
	} {
		t.Run(c.name, func(t *testing.T) {
			input := validMigrationInput(t)
			input.Controller.Result = c.result
			input.Controller.Plan.EvidenceGapCount = c.gaps
			err := validateMigrationController(input)
			if err == nil {
				t.Fatal("accepted a result that does not follow from the gap count")
			}
			if !strings.Contains(err.Error(), "evidence gap") {
				t.Fatalf("refused with %q, which does not name the disagreement", err)
			}
		})
	}
}

// TestHardwareDispositionIsScopedToARunThatHasTheGap records a distinction the
// task 160 sweep turned up rather than assumed.
//
// BuildHardwareDispositionReceipt is NOT one of the doors blocking a completed
// campaign. It requires the port action's missing signals to be exactly
// ["hardware_claim"] -- the gap IS its subject -- so it is only defined for a
// run that has at least one, and "blocked_evidence" was implied by a check the
// function already made rather than being an independent literal. Replacing it
// with the agreement rule is equivalent there and strictly more permissive if
// the gate were ever handed a zero-gap receipt.
//
// The test that matters is that the two failures stay DISTINGUISHABLE: a
// receipt with no gaps must be refused for having no hardware gap, not for its
// result, or the message sends the reader to the wrong place.
func TestHardwareDispositionIsScopedToARunThatHasTheGap(t *testing.T) {
	controller, digest := validHardwareController(t)
	if _, err := BuildHardwareDispositionReceipt(controller, digest); err != nil {
		t.Fatalf("the valid fixture was rejected: %v", err)
	}

	finished := controller
	finished.Result = "pass"
	finished.Plan.EvidenceGapCount = 0
	for index := range finished.Plan.Surfaces {
		finished.Plan.Surfaces[index].MissingSignals = []string{}
	}
	_, err := BuildHardwareDispositionReceipt(finished, digest)
	if err == nil {
		t.Fatal("a receipt with no hardware gap produced a hardware disposition; the disposition " +
			"is about that gap, so there is nothing to disposition")
	}
	if !strings.Contains(err.Error(), "hardware gap") {
		t.Fatalf("refused with %q. A run with no gaps must be refused for having no HARDWARE "+
			"gap, not for its result -- the result now agrees with the count, and blaming it "+
			"would send the reader to the wrong file", err)
	}
}

// TestHardwareDispositionRejectsAResultThatDoesNotFollow. The agreement check
// at that site was LIVE and untested -- probed by handing it a receipt claiming
// "pass" beside one outstanding gap, which it refuses. Deleting the call left
// the package green until this test existed, which is the same shape as the
// rule it enforces: present, correct, and exercised by nothing.
func TestHardwareDispositionRejectsAResultThatDoesNotFollow(t *testing.T) {
	controller, digest := validHardwareController(t)
	if controller.Plan.EvidenceGapCount == 0 {
		t.Fatal("the fixture has no gaps, so a claim of \"pass\" would agree with it and this " +
			"test would assert nothing")
	}
	controller.Result = "pass"

	_, err := BuildHardwareDispositionReceipt(controller, digest)
	if err == nil {
		t.Fatal("a receipt claiming pass beside an outstanding evidence gap was accepted")
	}
	if !strings.Contains(err.Error(), "evidence gap") {
		t.Fatalf("refused with %q, which does not name the disagreement", err)
	}
}
