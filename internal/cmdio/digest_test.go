package cmdio

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

// TestFileDigestMatchesTheBytesOnDisk pins that the digest is of the file, not
// of anything derived from it. Every caller records it as the identity of an
// artifact, so a digest of normalised or re-encoded content would identify
// something that was never written.
func TestFileDigestMatchesTheBytesOnDisk(t *testing.T) {
	body := []byte("{\"gate\":\"g\"}\n")
	path := filepath.Join(t.TempDir(), "artifact.json")
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := FileDigest(path)
	if err != nil {
		t.Fatalf("FileDigest() error = %v", err)
	}
	sum := sha256.Sum256(body)
	if want := hex.EncodeToString(sum[:]); got != want {
		t.Errorf("digest = %s, want %s", got, want)
	}
	if len(got) != 64 {
		t.Errorf("digest is %d characters, want 64", len(got))
	}
}

// TestFileDigestChangesWithOneByte is the control: without it the test above
// passes for a function that returns a constant.
func TestFileDigestChangesWithOneByte(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a")
	b := filepath.Join(dir, "b")
	if err := os.WriteFile(a, []byte("one"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(b, []byte("onf"), 0o600); err != nil {
		t.Fatal(err)
	}
	da, err := FileDigest(a)
	if err != nil {
		t.Fatal(err)
	}
	db, err := FileDigest(b)
	if err != nil {
		t.Fatal(err)
	}
	if da == db {
		t.Error("two files differing by one byte produced the same digest")
	}
}

// TestFileDigestOfAMissingFileIsAnError, because a digest of an absent artifact
// must not read as a digest of an empty one.
func TestFileDigestOfAMissingFileIsAnError(t *testing.T) {
	if _, err := FileDigest(filepath.Join(t.TempDir(), "absent")); err == nil {
		t.Fatal("a missing file produced a digest")
	}
}
