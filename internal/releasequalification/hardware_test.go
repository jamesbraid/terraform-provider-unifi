package releasequalification

import (
	"strings"
	"testing"

	"github.com/ubiquiti-community/terraform-provider-unifi/internal/catalogparity"
)

func TestBuildHardwareDispositionReceiptScopesPortClaim(t *testing.T) {
	controller, digest := validHardwareController(t)

	receipt, err := BuildHardwareDispositionReceipt(controller, digest)
	if err != nil {
		t.Fatalf("BuildHardwareDispositionReceipt() error = %v", err)
	}
	if receipt.Result != "pass" || receipt.Mode != "protocol_sufficient" ||
		receipt.ClaimScope != "controller_poe_configuration_persistence" ||
		receipt.PhysicalElectricalClaim || receipt.ResolvedSignal != "hardware_claim" ||
		receipt.ControllerReceiptSHA256 != digest {
		t.Fatalf("receipt = %#v", receipt)
	}
}

func TestBuildHardwareDispositionReceiptAllowsReleasedLimitation(t *testing.T) {
	controller, digest := validHardwareController(t)
	allowReleasedControllerLimitation(t, &controller)

	receipt, err := BuildHardwareDispositionReceipt(controller, digest)
	if err != nil {
		t.Fatalf("BuildHardwareDispositionReceipt() error = %v", err)
	}
	if receipt.Result != "pass" || receipt.ResolvedSignal != "hardware_claim" {
		t.Fatalf("receipt = %#v", receipt)
	}
}

func TestBuildHardwareDispositionReceiptRejectsCandidateLimitation(t *testing.T) {
	controller, digest := validHardwareController(t)
	allowReleasedControllerLimitation(t, &controller)
	controller.Candidate = controller.Released

	_, err := BuildHardwareDispositionReceipt(controller, digest)
	if err == nil || !strings.Contains(err.Error(), "candidate suite") {
		t.Fatalf("error = %v, want candidate suite failure", err)
	}
}

func TestBuildHardwareDispositionReceiptFailsClosed(t *testing.T) {
	tests := map[string]struct {
		mutate func(*catalogparity.ControllerDifferentialReceipt, *string)
		want   string
	}{
		"digest": {
			mutate: func(_ *catalogparity.ControllerDifferentialReceipt, digest *string) { *digest = "bad" },
			want:   "SHA-256",
		},
		"missing scenario": {
			mutate: func(receipt *catalogparity.ControllerDifferentialReceipt, _ *string) {
				receipt.Plan.Surfaces[len(receipt.Plan.Surfaces)-1].TestNames = []string{"TestAccOther"}
			},
			want: "scenario",
		},
		"extra gap": {
			mutate: func(receipt *catalogparity.ControllerDifferentialReceipt, _ *string) {
				receipt.Plan.Surfaces[len(receipt.Plan.Surfaces)-1].MissingSignals = []string{"hardware_claim", "unknown"}
			},
			want: "hardware gap",
		},
		"candidate did not pass": {
			mutate: func(receipt *catalogparity.ControllerDifferentialReceipt, _ *string) {
				receipt.Candidate.Passed = receipt.Candidate.Passed[:len(receipt.Candidate.Passed)-1]
			},
			want: "candidate suite",
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			controller, digest := validHardwareController(t)
			test.mutate(&controller, &digest)
			_, err := BuildHardwareDispositionReceipt(controller, digest)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want %q", err, test.want)
			}
		})
	}
}

func validHardwareController(t *testing.T) (catalogparity.ControllerDifferentialReceipt, string) {
	t.Helper()
	input := validMigrationInput(t)
	controller := input.Controller
	const scenario = "TestAccPortAction_persistsPoeOverride"
	port := &controller.Plan.Surfaces[len(controller.Plan.Surfaces)-1]
	if port.SurfaceKey != (catalogparity.SurfaceKey{Kind: catalogparity.Action, Name: "unifi_port"}) {
		t.Fatalf("last surface = %#v", port.SurfaceKey)
	}
	oldScenario := port.TestNames[0]
	port.TestNames = []string{scenario}
	controller.Plan.TestNames[len(controller.Plan.TestNames)-1] = scenario
	for _, suite := range []*catalogparity.ControllerSuiteReceipt{&controller.Released, &controller.Candidate} {
		for index := range suite.Passed {
			if suite.Passed[index] == oldScenario {
				suite.Passed[index] = scenario
			}
		}
	}
	port.MissingSignals = []string{"hardware_claim"}
	return controller, strings.Repeat("a", 64)
}
