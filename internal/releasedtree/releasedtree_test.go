package releasedtree

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// These run against this repository rather than a fixture. A fixture would show
// the functions working on a repository built to suit them, which is not the
// claim. The claim is that the released tag every catalog gate is measured
// against resolves and unpacks HERE.

const repository = "../.."

// released reads the tag and commit out of the policy that declares them, for
// the same reason the command does: a literal in this file would be a third
// place for the released version to live, free to disagree with the other two.
func released(t *testing.T) (tag, commit string) {
	t.Helper()
	path := filepath.Join(repository, "provider-codegen", "policy", "catalog-evidence.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var policy struct {
		ReleasedProvider struct {
			Version string `json:"version"`
			Commit  string `json:"commit"`
		} `json:"released_provider"`
	}
	if err := json.Unmarshal(raw, &policy); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	if policy.ReleasedProvider.Version == "" {
		t.Fatalf("%s declares no released provider version", path)
	}
	if len(policy.ReleasedProvider.Commit) != 40 {
		t.Fatalf("%s declares %q as the released commit, which is not a sha",
			path, policy.ReleasedProvider.Commit)
	}
	return "v" + policy.ReleasedProvider.Version, policy.ReleasedProvider.Commit
}

func TestRequireCommitAcceptsTheDeclaredReleasedCommit(t *testing.T) {
	tag, declared := released(t)
	commit, err := RequireCommit(repository, tag, declared)
	if err != nil {
		t.Fatalf("%s does not resolve to the commit the policy declares: %v.\n"+
			"Every catalog comparison is measured against that tree, so this is not a test "+
			"about tags -- it is whether the released side is identified at all", tag, err)
	}
	if commit != declared {
		t.Fatalf("RequireCommit returned %s, want %s", commit, declared)
	}
}

func TestRequireCommitRefusesWhatItCannotIdentify(t *testing.T) {
	tag, declared := released(t)
	for _, c := range []struct {
		name     string
		tag      string
		declared string
		wants    string
	}{
		{"a commit that is not the tag's", tag, strings.Repeat("0", 40), "is declared as the released commit"},
		{"no declared commit at all", tag, "", "no released commit is declared"},
		{"a tag that does not exist", "v0.0.0-not-a-tag", declared, "resolve"},
	} {
		t.Run(c.name, func(t *testing.T) {
			_, err := RequireCommit(repository, c.tag, c.declared)
			if err == nil {
				t.Fatal("accepted")
			}
			if !strings.Contains(err.Error(), c.wants) {
				t.Fatalf("refused with %q, which does not mention %q", err, c.wants)
			}
		})
	}
}

func TestExtractProducesTheTagsTree(t *testing.T) {
	tag, _ := released(t)
	destination := t.TempDir()
	if err := Extract(repository, tag, destination); err != nil {
		t.Fatalf("extract %s: %v", tag, err)
	}
	// go.mod, not a file this migration touches: the released tree must not
	// depend on anything the candidate is currently changing.
	if _, err := os.Stat(filepath.Join(destination, "go.mod")); err != nil {
		t.Fatalf("the extracted tree has no go.mod: %v", err)
	}
	// A .git directory here would make the released root a second working tree,
	// and anything walking it would find the candidate's files under a name
	// that says released.
	if _, err := os.Stat(filepath.Join(destination, ".git")); err == nil {
		t.Fatal("the extracted tree carries a .git directory, so it is not just the tag's content")
	}
}

func TestExtractRefusesADestinationItCannotUse(t *testing.T) {
	tag, _ := released(t)
	t.Run("one that does not exist", func(t *testing.T) {
		if err := Extract(repository, tag, filepath.Join(t.TempDir(), "absent")); err == nil {
			t.Fatal("accepted a destination that does not exist, so the extraction would " +
				"have gone nowhere and reported success")
		}
	})
	t.Run("one that is a file", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "a-file")
		if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		err := Extract(repository, tag, path)
		if err == nil || !strings.Contains(err.Error(), "not a directory") {
			t.Fatalf("accepted a file as the destination, or refused for another reason: %v", err)
		}
	})
}
