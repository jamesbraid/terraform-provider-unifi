package catalogparity

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// DefaultSDKDownloadRoot is where the Go module cache keeps a module's
// downloaded archives.
func DefaultSDKDownloadRoot(moduleCache, modulePath string) string {
	return filepath.Join(moduleCache, "cache", "download", filepath.FromSlash(modulePath), "@v")
}

// VerifySDKArchives requires the module cache to hold exactly the two SDK
// archives the policy names.
//
// The inventory is a comparison BETWEEN two SDK versions, and it reads them out
// of the module cache. Without this the cache is an unexamined input: a
// partially populated one, or one holding a different build of the same
// version, produces a complete inventory describing a comparison nobody
// specified. The archives are named in the policy precisely so this can be
// checked, and checking it is the only thing that makes naming them worth
// anything.
//
// BOTH ARCHIVES ARE REPORTED, not just the first. A run whose cache is missing
// both should not be fixed twice.
func VerifySDKArchives(downloadRoot string, sdk SDKComparison) error {
	var problems []string
	for _, want := range []struct {
		side    string
		version string
		digest  string
	}{
		{"released", sdk.ReleasedVersion, sdk.ReleasedArchiveSHA256},
		{"candidate", sdk.CandidateVersion, sdk.CandidateArchiveSHA256},
	} {
		if want.version == "" || want.digest == "" {
			problems = append(problems, fmt.Sprintf("the policy names no %s SDK version or digest", want.side))
			continue
		}
		path := filepath.Join(downloadRoot, want.version+".zip")
		raw, err := os.ReadFile(filepath.Clean(path))
		if err != nil {
			problems = append(problems, fmt.Sprintf("%s SDK archive %s is not in the module cache: %v",
				want.side, want.version, err))
			continue
		}
		sum := sha256.Sum256(raw)
		if got := hex.EncodeToString(sum[:]); got != want.digest {
			problems = append(problems, fmt.Sprintf("%s SDK archive %s is %s, the policy declares %s",
				want.side, want.version, got, want.digest))
		}
	}
	if len(problems) == 0 {
		return nil
	}
	sort.Strings(problems)
	return fmt.Errorf("the module cache does not hold the SDK archives this comparison is about: %v", problems)
}
