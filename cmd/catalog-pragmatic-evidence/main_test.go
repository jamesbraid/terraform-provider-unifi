package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/ubiquiti-community/terraform-provider-unifi/internal/catalogparity"
)

func TestRunWritesBoundReferenceResolution(t *testing.T) {
	root := filepath.Join("..", "..")
	output := filepath.Join(t.TempDir(), "resolution.json")
	controllerReceipt := filepath.Join(t.TempDir(), "controller.json")
	controllerData := []byte(`{"format_version":1,"gate":"catalog controller differential","result":"blocked_evidence","plan":{"evidence_gap_count":10,"released_allowed_failures":["TestAccDeviceFramework_basic"],"released_allowed_missing":["TestAccDeviceList_basic"]},"released":{"result":"pass","failed":[],"accepted_failures":[],"unexpected_failures":[],"missing":[]},"candidate":{"result":"pass","failed":[],"accepted_failures":[],"unexpected_failures":[],"missing":[]}}` + "\n")
	if err := os.WriteFile(controllerReceipt, controllerData, 0o600); err != nil {
		t.Fatal(err)
	}
	args := []string{
		"-inventory", filepath.Join(root, "build", "release-ready", "catalog-evidence-inventory.json"),
		"-fleet-summary", filepath.Join(root, "build", "restricted", "catalog-fleet-gap-summary.json"),
		"-references", filepath.Join(root, "provider-codegen", "policy", "catalog-pragmatic-references.json"),
		"-controller-receipt", controllerReceipt,
		"-output", output,
	}
	if code := run(args, os.Stderr); code != 0 {
		t.Fatalf("run() exit = %d", code)
	}
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	var resolution catalogparity.PragmaticResolution
	if err := json.Unmarshal(data, &resolution); err != nil {
		t.Fatal(err)
	}
	if resolution.Result != "blocked_evidence" || resolution.ResolvedSignalCount != 7 || resolution.RemainingSignalCount != 1 {
		t.Fatalf("resolution = %+v", resolution)
	}
	sum := sha256.Sum256(controllerData)
	if resolution.ControllerReceiptSHA256 != hex.EncodeToString(sum[:]) {
		t.Fatalf("controller receipt SHA-256 = %q", resolution.ControllerReceiptSHA256)
	}
}

func TestValidateControllerReceiptAcceptsExactReleasedLimitation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "controller.json")
	data := []byte(`{"format_version":1,"gate":"catalog controller differential","result":"blocked_evidence","plan":{"evidence_gap_count":10,"released_allowed_failures":["TestAccDeviceFramework_basic"],"released_allowed_missing":["TestAccDeviceList_basic"]},"released":{"result":"accepted_limitation","failed":["TestAccDeviceFramework_basic"],"accepted_failures":["TestAccDeviceFramework_basic"],"unexpected_failures":[],"missing":["TestAccDeviceList_basic"]},"candidate":{"result":"pass","failed":[],"accepted_failures":[],"unexpected_failures":[],"missing":[]}}` + "\n")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := validateControllerReceipt(path); err != nil {
		t.Fatalf("validateControllerReceipt() error = %v", err)
	}
}

func TestValidateControllerReceiptAcceptsAllowedReleasedFailureThatPasses(t *testing.T) {
	path := filepath.Join(t.TempDir(), "controller.json")
	data := []byte(`{"format_version":1,"gate":"catalog controller differential","result":"blocked_evidence","plan":{"evidence_gap_count":10,"released_allowed_failures":["TestAccDeviceFramework_basic"],"released_allowed_missing":["TestAccDeviceList_basic"]},"released":{"result":"accepted_limitation","failed":[],"accepted_failures":[],"unexpected_failures":[],"missing":["TestAccDeviceList_basic"]},"candidate":{"result":"pass","failed":[],"accepted_failures":[],"unexpected_failures":[],"missing":[]}}` + "\n")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := validateControllerReceipt(path); err != nil {
		t.Fatalf("validateControllerReceipt() error = %v", err)
	}
}

func TestValidateControllerReceiptRejectsBroaderReleasedLimitation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "controller.json")
	data := []byte(`{"format_version":1,"gate":"catalog controller differential","result":"blocked_evidence","plan":{"evidence_gap_count":10,"released_allowed_failures":["TestAccDeviceFramework_basic"],"released_allowed_missing":["TestAccDeviceList_basic"]},"released":{"result":"accepted_limitation","failed":["TestAccDeviceFramework_basic","TestAccUnexpected"],"accepted_failures":["TestAccDeviceFramework_basic","TestAccUnexpected"],"unexpected_failures":[],"missing":["TestAccDeviceList_basic"]},"candidate":{"result":"pass","failed":[],"accepted_failures":[],"unexpected_failures":[],"missing":[]}}` + "\n")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := validateControllerReceipt(path); err == nil {
		t.Fatal("validateControllerReceipt() accepted a broader released limitation")
	}
}

func TestValidateControllerReceiptRejectsBroaderReleasedMissing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "controller.json")
	data := []byte(`{"format_version":1,"gate":"catalog controller differential","result":"blocked_evidence","plan":{"evidence_gap_count":10,"released_allowed_failures":["TestAccDeviceFramework_basic"],"released_allowed_missing":["TestAccDeviceList_basic"]},"released":{"result":"accepted_limitation","failed":["TestAccDeviceFramework_basic"],"accepted_failures":["TestAccDeviceFramework_basic"],"unexpected_failures":[],"missing":["TestAccDeviceList_basic","TestAccUnexpected"]},"candidate":{"result":"pass","failed":[],"accepted_failures":[],"unexpected_failures":[],"missing":[]}}` + "\n")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := validateControllerReceipt(path); err == nil {
		t.Fatal("validateControllerReceipt() accepted a broader released missing set")
	}
}

func TestRunRejectsIncompleteArguments(t *testing.T) {
	if code := run(nil, os.Stderr); code != 2 {
		t.Fatalf("run(nil) = %d, want 2", code)
	}
}
