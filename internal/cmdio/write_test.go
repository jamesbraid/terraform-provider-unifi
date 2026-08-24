package cmdio

import (
	"os"
	"path/filepath"
	"testing"
)

// TestBareCallIsTheMajorityBehaviour pins what a call with no options does:
// what any future caller gets without thinking about it.
func TestBareCallIsTheMajorityBehaviour(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "made", "up", "receipt.json")

	if err := WriteAtomic(path, []byte(`{"ok":true}`)); err != nil {
		t.Fatalf("WriteAtomic() error = %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("the file was not written: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Errorf("mode = %o, want 0600 -- the bare call must not widen permissions", got)
	}
	body, err := os.ReadFile(path) //nolint:gosec // the path is this test's own TempDir
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != `{"ok":true}` {
		t.Errorf("content = %q, want the bytes passed in", body)
	}
}

// TestModeRecordsTheThreeCallersThatWidenIt covers the 0o644 variant.
func TestModeRecordsTheThreeCallersThatWidenIt(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "receipt.json")
	if err := WriteAtomic(path, []byte("x"), Mode(0o644)); err != nil {
		t.Fatalf("WriteAtomic() error = %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o644 {
		t.Errorf("mode = %o, want 0644", got)
	}
}

// TestNoParentDirIsARealDifference proves the option is not decoration: with
// it, a missing directory must fail rather than be created.
func TestNoParentDirIsARealDifference(t *testing.T) {
	root := t.TempDir()
	missing := filepath.Join(root, "absent", "receipt.json")

	if err := WriteAtomic(missing, []byte("x"), NoParentDir()); err == nil {
		t.Error("WriteAtomic() created a file under a directory that does not exist; " +
			"NoParentDir must preserve the original failure")
	}
	// The control: without the option, the same path succeeds. Without this,
	// the test above passes if WriteAtomic is broken for every path.
	if err := WriteAtomic(missing, []byte("x")); err != nil {
		t.Errorf("the bare call should have created the parent directory: %v", err)
	}
}

// TestSkipSyncStillWritesCorrectly is the only thing exercising this option,
// since it has no caller (see SkipSync). It can't observe whether Sync() was
// skipped -- that's not visible on a normal filesystem -- only that the
// option doesn't otherwise change the result.
func TestSkipSyncStillWritesCorrectly(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "receipt.json")
	if err := WriteAtomic(path, []byte("payload"), SkipSync()); err != nil {
		t.Fatalf("WriteAtomic() error = %v", err)
	}
	body, err := os.ReadFile(path) //nolint:gosec // the path is this test's own TempDir
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "payload" {
		t.Errorf("content = %q, want %q", body, "payload")
	}
}

// TestNoTemporaryFileSurvives covers the reason the write is atomic at all: a
// reader must never see a partial file, and a failed write must not leave
// litter beside the real artifact.
func TestNoTemporaryFileSurvives(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "receipt.json")
	if err := WriteAtomic(path, []byte("x")); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		names := []string{}
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("%d entries left in the directory, want only the artifact: %v", len(entries), names)
	}
}

// TestTemporaryNamesTheArtifact covers the attribution property: a crash
// between create and rename leaves a file, and that file must say what was
// being written.
func TestTemporaryNamesTheArtifact(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "example-artifact.json")
	// Exercised through the real call: write, then confirm the artifact landed
	// and no differently-named litter remains.
	if err := WriteAtomic(path, []byte("x")); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(root)
	if len(entries) != 1 || entries[0].Name() != "example-artifact.json" {
		t.Errorf("directory holds %d entries, want just the artifact", len(entries))
	}
	// And the derivation itself, so a change to it fails here rather than in a
	// post-crash investigation.
	if want := ".example-artifact.json-"; derivePrefix(path) != want {
		t.Errorf("derived prefix = %q, want %q", derivePrefix(path), want)
	}
}
