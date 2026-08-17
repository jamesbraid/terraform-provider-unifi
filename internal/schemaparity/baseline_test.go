package schemaparity

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func promotableEnvironment() Environment {
	return Environment{
		Platform: "linux/amd64", GoVersion: "1.25.8",
		TerraformVersion: "1.15.8", TerraformSHA256: "tf-sha",
		TofuVersion: "1.12.1", TofuSHA256: "tofu-sha",
		ReleasedAuthority: "published_archive",
	}
}

func promotableBaseline() BaselineManifest {
	var b BaselineManifest
	b.Provider.Platform = "linux/amd64"
	b.Toolchain.GoVersion = "1.25.8"
	b.Clients.Terraform = ClientPin{Version: "1.15.8", BinarySHA256: "tf-sha"}
	b.Clients.OpenTofu = ClientPin{Version: "1.12.1", BinarySHA256: "tofu-sha"}
	return b
}

// TestEveryPromotionBlockerFiresSeparately mutates one field at a time.
//
// The shell this replaces appended to an array with seven `[[ ]] ||` lines, and
// nothing exercised them: catalog-build-schema_test.sh never constructed a
// mismatched environment, so any one of the seven could have been inverted or
// deleted and the suite would not have moved. Seven silent conditions is the
// shape that made porting this worth the time.
//
// One case per blocker rather than a table asserting "some blocker fired",
// because a single assertion cannot see one condition going quiet while its
// neighbours cover for it.
func TestEveryPromotionBlockerFiresSeparately(t *testing.T) {
	tests := map[string]struct {
		mutate func(*Environment)
		want   string
	}{
		"go version":         {func(e *Environment) { e.GoVersion = "1.24.0" }, "go_version"},
		"platform":           {func(e *Environment) { e.Platform = "darwin/arm64" }, "platform"},
		"terraform version":  {func(e *Environment) { e.TerraformVersion = "1.14.0" }, "terraform_version"},
		"terraform binary":   {func(e *Environment) { e.TerraformSHA256 = "other" }, "terraform_binary"},
		"tofu version":       {func(e *Environment) { e.TofuVersion = "1.11.0" }, "tofu_version"},
		"tofu binary":        {func(e *Environment) { e.TofuSHA256 = "other" }, "tofu_binary"},
		"released authority": {func(e *Environment) { e.ReleasedAuthority = "source_rebuild" }, "released_binary_authority"},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			env := promotableEnvironment()
			test.mutate(&env)
			got := PromotionBlockers(env, promotableBaseline())
			if !reflect.DeepEqual(got, []string{test.want}) {
				t.Fatalf("blockers = %v, want exactly [%s]", got, test.want)
			}
		})
	}
}

// TestAPromotableEnvironmentBlocksNothing is the control. Without it every case
// above would pass on a function that always returned its argument's name.
func TestAPromotableEnvironmentBlocksNothing(t *testing.T) {
	if got := PromotionBlockers(promotableEnvironment(), promotableBaseline()); len(got) != 0 {
		t.Fatalf("a promotable environment produced %v", got)
	}
}

// TestBlockersAreStableAcrossRuns pins the ordering. Several gates compare
// receipts byte for byte, so an unstable order would make two identical runs
// disagree -- a difference with no cause, which is the most expensive kind to
// chase.
func TestBlockersAreStableAcrossRuns(t *testing.T) {
	env := promotableEnvironment()
	env.GoVersion, env.Platform, env.TofuVersion = "x", "y", "z"
	first := PromotionBlockers(env, promotableBaseline())
	for i := 0; i < 8; i++ {
		if got := PromotionBlockers(env, promotableBaseline()); !reflect.DeepEqual(got, first) {
			t.Fatalf("run %d gave %v, first gave %v", i, got, first)
		}
	}
	if len(first) != 3 {
		t.Fatalf("three mutations produced %d blockers: %v", len(first), first)
	}
}

// TestBaselineManifestParsesTheCommittedFile reads the real artifact rather
// than a fixture. A hand-written fixture would share this author's model of the
// JSON, which is how the digests test came to assert one fact of ninety-five.
func TestBaselineManifestParsesTheCommittedFile(t *testing.T) {
	manifest, err := LoadBaselineManifest("../../build/m0/provider-baseline.json")
	if err != nil {
		t.Fatalf("the committed baseline does not parse: %v", err)
	}
	for name, got := range map[string]string{
		"provider.platform":               manifest.Provider.Platform,
		"toolchain.go_version":            manifest.Toolchain.GoVersion,
		"clients.terraform.version":       manifest.Clients.Terraform.Version,
		"clients.terraform.binary_sha256": manifest.Clients.Terraform.BinarySHA256,
		"clients.opentofu.version":        manifest.Clients.OpenTofu.Version,
		"clients.opentofu.binary_sha256":  manifest.Clients.OpenTofu.BinarySHA256,
	} {
		if got == "" {
			t.Errorf("%s decoded empty; the struct and the artifact disagree about its name", name)
		}
	}
}

// TestAMissingBaselineIsAnError. The shell read it with jq, which prints null
// for a missing file and carries on, so every comparison would have succeeded
// against the string "null" and the receipt would have recorded no blockers.
func TestAMissingBaselineIsAnError(t *testing.T) {
	if _, err := LoadBaselineManifest(filepath.Join(t.TempDir(), "absent.json")); err == nil {
		t.Fatal("a missing baseline was accepted; every promotion check would compare against zero values")
	}
	broken := filepath.Join(t.TempDir(), "broken.json")
	if err := os.WriteFile(broken, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadBaselineManifest(broken); err == nil {
		t.Fatal("an unparseable baseline was accepted")
	}
}
