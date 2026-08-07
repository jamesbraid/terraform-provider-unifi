package releasequalification

import (
	"fmt"
	"reflect"

	"github.com/ubiquiti-community/terraform-provider-unifi/internal/catalogparity"
)

const portPersistenceScenario = "TestAccPortAction_persistsPoeOverride"

func BuildHardwareDispositionReceipt(
	controller catalogparity.ControllerDifferentialReceipt,
	controllerSHA256 string,
) (HardwareDispositionReceipt, error) {
	if !validHex(controllerSHA256, 64) {
		return HardwareDispositionReceipt{}, fmt.Errorf("controller receipt SHA-256 is invalid")
	}
	if controller.FormatVersion != 1 || controller.Gate != "catalog controller differential" ||
		controller.Result != "blocked_evidence" || controller.Plan.SurfaceCount != 67 ||
		len(controller.Plan.Surfaces) != 67 {
		return HardwareDispositionReceipt{}, fmt.Errorf("controller receipt is not a complete catalog differential")
	}
	want := catalogparity.SurfaceKey{Kind: catalogparity.Action, Name: "unifi_port"}
	var port *catalogparity.ControllerPlanSurface
	for index := range controller.Plan.Surfaces {
		surface := &controller.Plan.Surfaces[index]
		if surface.SurfaceKey != want {
			continue
		}
		if port != nil {
			return HardwareDispositionReceipt{}, fmt.Errorf("controller receipt has duplicate port action")
		}
		port = surface
	}
	if port == nil || !containsString(port.TestNames, portPersistenceScenario) {
		return HardwareDispositionReceipt{}, fmt.Errorf("port action persistence scenario is missing")
	}
	if !reflect.DeepEqual(port.MissingSignals, []string{"hardware_claim"}) {
		return HardwareDispositionReceipt{}, fmt.Errorf("port action hardware gap is not the single scoped claim")
	}
	if !controllerSuiteComplete(
		controller.Released,
		controller.Plan,
		controller.Plan.ReleasedAllowedFailures,
	) || !containsString(controller.Released.Passed, portPersistenceScenario) {
		return HardwareDispositionReceipt{}, fmt.Errorf("released suite did not pass the port action persistence scenario")
	}
	if !controllerSuiteComplete(controller.Candidate, controller.Plan, nil) ||
		!containsString(controller.Candidate.Passed, portPersistenceScenario) {
		return HardwareDispositionReceipt{}, fmt.Errorf("candidate suite did not pass the port action persistence scenario")
	}
	return HardwareDispositionReceipt{
		FormatVersion: 1, Gate: "unifi-port-hardware-disposition", Result: "pass",
		SurfaceKey: want, Mode: "protocol_sufficient",
		ClaimScope:              "controller_poe_configuration_persistence",
		PhysicalElectricalClaim: false, ControllerReceiptSHA256: controllerSHA256,
		ResolvedSignal: "hardware_claim",
	}, nil
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
