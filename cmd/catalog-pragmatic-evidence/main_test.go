package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ubiquiti-community/terraform-provider-unifi/internal/catalogparity"
)

// PROVEN TO FAIL, recorded here rather than only in the commit that proved it.
// Four mutations of the declared-missing set prove this can fail: a declared
// name that does run on the released tree, a candidate-only test left
// undeclared, shared_scenario_exceptions emptied, and an inventory with its
// added scenarios flattened. (cf32d48d)
//
// A TAUTOLOGY TO KNOW ABOUT WHEN READING THE RESULT: the fixture is generated
// from the same policy it is checked against, so three of validateControllerReceipt's
// checks agree by construction across this file. That is the price of removing
// the duplicated declaration, and it means a resolver bug would not surface here.

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
		// What evidence_tree_json renders on a clean tree. The pipeline measures
		// this in the same step that runs the binary; a test supplies it.
		"-tree-state", `{"status":"clean","commit":"0000000000000000000000000000000000000000","dirty_paths":[]}`,
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

// TestRefusesWithoutATreeState makes the refusal a check rather than an
// intention.
//
// A missing -tree-state must fail, never default. A default -- "unknown", or a
// zero value -- would put a tree_state in every receipt that no measurement
// produced, and any gate reading it would be comparing a constant against
// itself. That is the shape where one producer writes a literal and the
// consumers dutifully check it, and it passes review because the branch is
// reachable in a test while the input can never vary in production.
//
// Deleting the ParseTreeState call, or giving it a fallback, fails this.
func TestRefusesWithoutATreeState(t *testing.T) {
	var stderr bytes.Buffer
	code := run([]string{"-policy", "/dev/null", "-inventory", "/dev/null", "-fleet-summary", "/dev/null", "-references", "/dev/null", "-output", "/dev/null"}, &stderr)
	if code == 0 {
		t.Fatalf("catalog-pragmatic-evidence ran without a tree state; exit = 0")
	}
	if !strings.Contains(stderr.String(), "tree state is required") {
		t.Fatalf("stderr = %q, want it to say the tree state is required", stderr.String())
	}
}
