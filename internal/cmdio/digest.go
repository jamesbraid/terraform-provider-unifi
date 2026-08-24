package cmdio

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
)

// FileDigest returns the sha256 of a file's bytes, hex encoded. It reads the
// whole file into memory, so a caller streaming a large file (e.g. verifying
// an HTTP download as it writes it) should compute its own digest over that
// stream instead of calling this afterwards.
func FileDigest(path string) (string, error) {
	raw, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}
