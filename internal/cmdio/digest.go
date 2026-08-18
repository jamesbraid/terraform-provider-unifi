package cmdio

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
)

// FileDigest returns the sha256 of a file's bytes, hex encoded.
//
// SIX COPIES, FIVE OF THEM THIS EXACT COMPUTATION. Three wrote it inline, one
// exported it from internal/dependencypin for a single caller, and one routed it
// through unitdifferential.Digest -- which is itself sha256 plus hex. A
// shape-hash reported four distinct behaviours; reading them showed one.
//
// THE SIXTH IS NOT A COPY AND STAYS WHERE IT IS. cmd/m3-dns-qualification digests
// through copyAndDigest, which streams: it is the same helper that verifies an
// HTTP download while writing it to disk, so the local file case reuses the
// streaming path and both produce the same value by construction. Sharing this
// function there would give that command two digest implementations to keep in
// agreement, which is the opposite of the point. It also matters for a file too
// large to hold in memory, which this function does not handle.
func FileDigest(path string) (string, error) {
	raw, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}
