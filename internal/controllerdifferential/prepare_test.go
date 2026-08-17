package controllerdifferential

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestPreparingTheReleasedTreeVetsEveryLayer is the check the shell could not
// reach.
//
// Preparation needs git and a Go toolchain and NOTHING ELSE -- no controller,
// no docker, no Linux -- which is exactly why the script was split at this
// point. Keeping preparation and execution together meant the tree could not be
// built anywhere the controllers cannot run, so these three vets were
// unreachable from any self-test and were verified by hand. Two of them were
// wrong in the gap between the reproduction and the thing.
//
// It runs the real graft with the real scenario owners the frozen plan lends,
// so a scenario file that stops compiling against the released provider fails
// here rather than an hour into a controller run.
func TestPreparingTheReleasedTreeVetsEveryLayer(t *testing.T) {
	tag, _ := releasedTag(t)
	owners := frozenReceipt(t).Plan.SharedScenarioOwners
	if len(owners) == 0 {
		t.Fatal("the frozen plan lends no scenario owners, so the third layer is unexercised")
	}

	result, err := PrepareReleasedTree(repositoryRoot, repositoryRoot, tag,
		filepath.Join(t.TempDir(), "released"), owners)
	if err != nil {
		t.Fatalf("preparing the released tree failed: %v", err)
	}
	if len(result.Layers) != 3 {
		t.Fatalf("vetted %d layer(s), want 3: the tag alone, the harness, the scenario owners",
			len(result.Layers))
	}
	if len(result.GraftedOwners) != len(owners) {
		t.Fatalf("grafted %d of %d scenario owners", len(result.GraftedOwners), len(owners))
	}
	// The harness must be the CANDIDATE's on both sides, so the two providers
	// are measured by one fixture.
	harness := filepath.Join(result.Root, "internal", "controllertest")
	if _, err := os.Stat(harness); err != nil {
		t.Fatalf("the candidate's harness is not in the released tree: %v", err)
	}
}

// TestEachLayerNamesADifferentThingToLookAt. When the released tree does not
// compile, which of its three owners broke it is the whole question, and the
// three answers have nothing in common: the tag failing alone is not the graft.
func TestEachLayerNamesADifferentThingToLookAt(t *testing.T) {
	tag, _ := releasedTag(t)
	result, err := PrepareReleasedTree(repositoryRoot, repositoryRoot, tag,
		filepath.Join(t.TempDir(), "released"), frozenReceipt(t).Plan.SharedScenarioOwners)
	if err != nil {
		t.Fatalf("preparing the released tree failed: %v", err)
	}
	seen := map[string]bool{}
	for _, layer := range result.Layers {
		if layer.Label == "" || layer.Advice == "" {
			t.Fatalf("a layer has no label or no advice: %+v", layer)
		}
		if seen[layer.Advice] {
			t.Fatalf("two layers give the same advice, so the report cannot say which owner "+
				"broke the tree: %q", layer.Advice)
		}
		seen[layer.Advice] = true
	}
}

// TestAScenarioOwnerThatDoesNotCompileIsRefused is the negative control. Without
// it, every assertion above is satisfied by a function that never vets at all.
//
// The remedy matters as much as the refusal: a scenario owner that cannot
// compile against the released provider must be withdrawn or split, NOT skipped,
// because a skipped owner produces a receipt that looks complete for a
// comparison that never happened.
func TestAScenarioOwnerThatDoesNotCompileIsRefused(t *testing.T) {
	tag, _ := releasedTag(t)
	work := t.TempDir()

	// Only what PrepareReleasedTree READS from the candidate is staged: the
	// harness, the compose file and the scenario owners. Copying the whole
	// repository would drag .git along and measure the filesystem rather than
	// the check.
	broken := filepath.Join(work, "candidate")
	owners := frozenReceipt(t).Plan.SharedScenarioOwners
	if err := copyTree(filepath.Join(repositoryRoot, "internal", "controllertest"),
		filepath.Join(broken, "internal", "controllertest")); err != nil {
		t.Fatal(err)
	}
	if err := copyFile(filepath.Join(repositoryRoot, "docker-compose.yaml"),
		filepath.Join(broken, "docker-compose.yaml")); err != nil {
		t.Fatal(err)
	}
	for _, owner := range owners {
		if err := copyFile(filepath.Join(repositoryRoot, owner), filepath.Join(broken, owner)); err != nil {
			t.Fatal(err)
		}
	}
	// git reads the tag from the real repository; the graft comes from the
	// staged copy, which is what lets one owner be broken without touching the
	// tree this test is running in.
	victim := filepath.Join(broken, owners[0])
	raw, err := os.ReadFile(victim)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(victim, append(raw, []byte("\nfunc thisDoesNotCompile() { undefined() }\n")...), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err = PrepareReleasedTree(repositoryRoot, broken, tag, filepath.Join(work, "released"), owners)
	if err == nil {
		t.Fatal("a scenario owner that does not compile was grafted onto the released tree and " +
			"accepted. The comparison would have run for an hour against a tree that cannot build")
	}
	if !strings.Contains(err.Error(), "scenario owner") {
		t.Fatalf("the refusal %q does not name the layer, so an operator cannot tell which of "+
			"the three owners broke the tree", err)
	}
}

func releasedTag(t *testing.T) (string, string) {
	t.Helper()
	receipt := frozenReceipt(t)
	if len(receipt.ReleasedCommit) != 40 {
		t.Fatalf("the frozen receipt names %q as the released commit", receipt.ReleasedCommit)
	}
	// The tag is read from the baseline manifest rather than written here, for
	// the same reason every other gate derives it: a literal would be another
	// home for the released version.
	raw, err := os.ReadFile(filepath.Join(repositoryRoot, "build", "m0", "provider-baseline.json"))
	if err != nil {
		t.Fatalf("read the baseline manifest: %v", err)
	}
	version := betweenQuotes(string(raw), `"version": "`)
	if version == "" {
		t.Fatal("the baseline manifest declares no provider version")
	}
	return "v" + version, receipt.ReleasedCommit
}

func betweenQuotes(haystack, prefix string) string {
	start := strings.Index(haystack, prefix)
	if start < 0 {
		return ""
	}
	rest := haystack[start+len(prefix):]
	end := strings.Index(rest, `"`)
	if end < 0 {
		return ""
	}
	return rest[:end]
}
