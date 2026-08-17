package releasequalification

import (
	"strings"
	"testing"

	"github.com/ubiquiti-community/terraform-provider-unifi/internal/catalogparity"
)

func passingDependencyReceipt() DependencyPublishabilityReceipt {
	return DependencyPublishabilityReceipt{
		FormatVersion:    1,
		Gate:             "go-unifi-dependency-publishability",
		Result:           "pass",
		ProviderCommit:   strings.Repeat("a", 40),
		ModulePath:       canonicalGoUnifiModule,
		ModuleVersion:    "v1.103.0",
		ModuleCommit:     strings.Repeat("b", 40),
		ModuleZipSHA256:  strings.Repeat("c", 64),
		ModuleDirSHA256:  strings.Repeat("d", 64),
		ReplacePresent:   false,
		ResolutionRunner: "remote_ci",
		NetworkBoundary:  "remote_ci_only",
		TreeState:        &catalogparity.TreeState{Status: "clean", Commit: strings.Repeat("e", 40)},
	}
}

// TestCheckDependencyPublishabilityReceipt watches every clause fail, one at a
// time, from a receipt that passes.
//
// The gate this replaces ran as a `jq -e` line in two workflows against a /tmp
// path that exists only mid-pipeline, so no clause in it had ever been observed
// failing. That is the whole reason for moving it: the assertion did not change,
// the ability to hand it a receipt that should fail did.
func TestCheckDependencyPublishabilityReceipt(t *testing.T) {
	if err := CheckDependencyPublishabilityReceipt(passingDependencyReceipt()); err != nil {
		t.Fatalf("a passing receipt was rejected: %v", err)
	}

	for _, c := range []struct {
		name    string
		mutate  func(*DependencyPublishabilityReceipt)
		mention string
	}{
		// The backstop half. These are constants the producer writes, so they
		// only fail if the producer's own abort regressed -- which is what a
		// backstop is for, and why they are covered but not counted.
		{"result", func(r *DependencyPublishabilityReceipt) { r.Result = "fail" }, "result"},
		{"gate name", func(r *DependencyPublishabilityReceipt) { r.Gate = "something-else" }, "gate"},
		{"module path", func(r *DependencyPublishabilityReceipt) { r.ModulePath = "github.com/other/mod" }, "module_path"},
		{"resolution runner", func(r *DependencyPublishabilityReceipt) { r.ResolutionRunner = "laptop" }, "resolution_runner"},
		{"network boundary", func(r *DependencyPublishabilityReceipt) { r.NetworkBoundary = "anywhere" }, "network_boundary"},
		{"a replace directive", func(r *DependencyPublishabilityReceipt) { r.ReplacePresent = true }, "replace_present"},

		// The half that carries information: each of these is wrong on a
		// receipt whose every constant above is correct.
		{"zip digest absent", func(r *DependencyPublishabilityReceipt) { r.ModuleZipSHA256 = "" }, "module_zip_sha256"},
		{"dir digest absent", func(r *DependencyPublishabilityReceipt) { r.ModuleDirSHA256 = "" }, "module_dir_sha256"},

		// LENGTH ALONE IS NOT ENOUGH. A placeholder of the right size is what a
		// receipt written by a stub looks like, and the shell checked only
		// ${#digest} -- so this case passed there.
		{"zip digest right length, not hex", func(r *DependencyPublishabilityReceipt) {
			r.ModuleZipSHA256 = strings.Repeat("z", 64)
		}, "module_zip_sha256"},

		{"digests identical", func(r *DependencyPublishabilityReceipt) {
			r.ModuleDirSHA256 = r.ModuleZipSHA256
		}, "identical"},
		{"provider commit malformed", func(r *DependencyPublishabilityReceipt) { r.ProviderCommit = "HEAD" }, "provider_commit"},
		{"module version absent", func(r *DependencyPublishabilityReceipt) { r.ModuleVersion = "" }, "module_version"},

		// The field whose absence made this receipt undecodable in the first
		// place. Requiring it stops that returning as a silence.
		{"tree state absent", func(r *DependencyPublishabilityReceipt) { r.TreeState = nil }, "tree_state is absent"},
		{"tree state dirty", func(r *DependencyPublishabilityReceipt) {
			r.TreeState = &catalogparity.TreeState{Status: "dirty"}
		}, "committed tree"},
	} {
		t.Run(c.name, func(t *testing.T) {
			receipt := passingDependencyReceipt()
			c.mutate(&receipt)
			err := CheckDependencyPublishabilityReceipt(receipt)
			if err == nil {
				t.Fatalf("the gate accepted a receipt it should have refused; expected it to mention %q", c.mention)
			}
			if !strings.Contains(err.Error(), c.mention) {
				t.Errorf("refused, but not for the reason under test.\n want mention of: %s\n got: %v", c.mention, err)
			}
		})
	}
}

// TestDependencyGateReportsEveryFailure covers the reason it returns a list. A
// receipt wrong in four ways reported one at a time costs a pipeline per fix.
func TestDependencyGateReportsEveryFailure(t *testing.T) {
	receipt := passingDependencyReceipt()
	receipt.Result = "fail"
	receipt.NetworkBoundary = "anywhere"
	receipt.ModuleZipSHA256 = ""
	receipt.TreeState = nil

	err := CheckDependencyPublishabilityReceipt(receipt)
	if err == nil {
		t.Fatal("a receipt wrong four ways was accepted")
	}
	for _, want := range []string{"result", "network_boundary", "module_zip_sha256", "tree_state"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the report omits %s:\n%v", want, err)
		}
	}
}
