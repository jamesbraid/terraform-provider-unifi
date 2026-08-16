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

func testCampaignPolicy(t *testing.T) catalogparity.CampaignPolicy {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "provider-codegen", "policy", "catalog-campaign.json"))
	if err != nil {
		t.Fatal(err)
	}
	var policy catalogparity.CampaignPolicy
	if err := json.Unmarshal(data, &policy); err != nil {
		t.Fatal(err)
	}
	if err := policy.Validate(); err != nil {
		t.Fatal(err)
	}
	return policy
}

// controllerReceiptJSON builds a controller receipt whose plan dispositions come
// from the policy instead of from a literal.
//
// Every fixture below used to carry its own copy of released_allowed_missing,
// so widening that declaration from three names to thirteen failed five tests
// here that had nothing to do with the change: the declaration had a second
// home, and the second home went stale the first time the first one was right.
// Deriving them means these fixtures track the policy by construction.
func controllerReceiptJSON(t *testing.T, policy catalogparity.CampaignPolicy, released map[string]any) []byte {
	t.Helper()
	data, err := json.Marshal(map[string]any{
		"format_version": 1,
		"gate":           "catalog controller differential",
		"result":         "blocked_evidence",
		"plan": map[string]any{
			"evidence_gap_count":        policy.EvidenceGapCount,
			"released_allowed_failures": policy.ReleasedAllowedFailures,
			"released_allowed_missing":  policy.ReleasedAllowedMissing,
		},
		"released": released,
		"candidate": map[string]any{
			"result":              "pass",
			"failed":              []string{},
			"accepted_failures":   []string{},
			"unexpected_failures": []string{},
			"missing":             []string{},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return append(data, '\n')
}

func TestRunWritesBoundReferenceResolution(t *testing.T) {
	root := filepath.Join("..", "..")
	output := filepath.Join(t.TempDir(), "resolution.json")
	controllerReceipt := filepath.Join(t.TempDir(), "controller.json")
	policy := testCampaignPolicy(t)
	controllerData := controllerReceiptJSON(t, policy, map[string]any{
		"result":              "pass",
		"failed":              []string{},
		"accepted_failures":   []string{},
		"unexpected_failures": []string{},
		"missing":             []string{},
	})
	if err := os.WriteFile(controllerReceipt, controllerData, 0o600); err != nil {
		t.Fatal(err)
	}
	args := []string{
		"-policy", filepath.Join(root, "provider-codegen", "policy", "catalog-campaign.json"),
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
	// Four resolved, two remaining. Three references were withdrawn from the
	// policy because their sources' only acceptance scenario is `added`: the
	// released provider never ran it, so there was no before-and-after to
	// borrow and the resolution they used to produce was not evidence. Two of
	// those surfaces no longer need one -- firewall_policy and site_to_site_vpn
	// now carry their own acceptance files, which the multi-file scenario owner
	// change made visible.
	if resolution.Result != "blocked_evidence" || resolution.ResolvedSignalCount != 4 || resolution.RemainingSignalCount != 2 {
		t.Fatalf("resolution = %+v", resolution)
	}
	sum := sha256.Sum256(controllerData)
	if resolution.ControllerReceiptSHA256 != hex.EncodeToString(sum[:]) {
		t.Fatalf("controller receipt SHA-256 = %q", resolution.ControllerReceiptSHA256)
	}
}

func TestValidateControllerReceiptAcceptsExactReleasedLimitation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "controller.json")
	policy := testCampaignPolicy(t)
	data := controllerReceiptJSON(t, policy, map[string]any{
		"result":              "accepted_limitation",
		"failed":              policy.ReleasedAllowedFailures,
		"accepted_failures":   policy.ReleasedAllowedFailures,
		"unexpected_failures": []string{},
		"missing":             policy.ReleasedAllowedMissing,
	})
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := validateControllerReceipt(path, testCampaignPolicy(t)); err != nil {
		t.Fatalf("validateControllerReceipt() error = %v", err)
	}
}

func TestValidateControllerReceiptAcceptsAllowedReleasedFailureThatPasses(t *testing.T) {
	path := filepath.Join(t.TempDir(), "controller.json")
	policy := testCampaignPolicy(t)
	data := controllerReceiptJSON(t, policy, map[string]any{
		"result":              "accepted_limitation",
		"failed":              []string{},
		"accepted_failures":   []string{},
		"unexpected_failures": []string{},
		"missing":             policy.ReleasedAllowedMissing,
	})
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := validateControllerReceipt(path, testCampaignPolicy(t)); err != nil {
		t.Fatalf("validateControllerReceipt() error = %v", err)
	}
}

func TestValidateControllerReceiptRejectsBroaderReleasedLimitation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "controller.json")
	policy := testCampaignPolicy(t)
	data := controllerReceiptJSON(t, policy, map[string]any{
		"result":              "accepted_limitation",
		"failed":              append(append([]string{}, policy.ReleasedAllowedFailures...), "TestAccUnexpected"),
		"accepted_failures":   append(append([]string{}, policy.ReleasedAllowedFailures...), "TestAccUnexpected"),
		"unexpected_failures": []string{},
		"missing":             policy.ReleasedAllowedMissing,
	})
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := validateControllerReceipt(path, testCampaignPolicy(t)); err == nil {
		t.Fatal("validateControllerReceipt() accepted a broader released limitation")
	}
}

func TestValidateControllerReceiptRejectsBroaderReleasedMissing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "controller.json")
	policy := testCampaignPolicy(t)
	data := controllerReceiptJSON(t, policy, map[string]any{
		"result":              "accepted_limitation",
		"failed":              policy.ReleasedAllowedFailures,
		"accepted_failures":   policy.ReleasedAllowedFailures,
		"unexpected_failures": []string{},
		"missing":             append(append([]string{}, policy.ReleasedAllowedMissing...), "TestAccUnexpected"),
	})
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := validateControllerReceipt(path, testCampaignPolicy(t)); err == nil {
		t.Fatal("validateControllerReceipt() accepted a broader released missing set")
	}
}

func TestRunRejectsIncompleteArguments(t *testing.T) {
	if code := run(nil, os.Stderr); code != 2 {
		t.Fatalf("run(nil) = %d, want 2", code)
	}
}
