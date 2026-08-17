package catalogparity

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The SDK archives are the inventory's real input: it is a comparison BETWEEN
// two versions of the SDK, read out of the module cache. The policy names their
// digests, and naming them is only worth something if something checks them --
// otherwise a partially populated cache, or one holding a different build of the
// same version, yields a complete inventory describing a comparison nobody
// specified.
//
// The shell checked this and nothing exercised the check. These do.

func sdkFixture(t *testing.T, released, candidate string) (string, SDKComparison) {
	t.Helper()
	root := t.TempDir()
	digest := func(version, body string) string {
		if body == "" {
			return strings.Repeat("a", 64)
		}
		if err := os.WriteFile(filepath.Join(root, version+".zip"), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256([]byte(body))
		return hex.EncodeToString(sum[:])
	}
	return root, SDKComparison{
		ModulePath:             "example.com/sdk",
		ReleasedVersion:        "v1.101.0",
		ReleasedArchiveSHA256:  digest("v1.101.0", released),
		CandidateVersion:       "v1.102.0",
		CandidateArchiveSHA256: digest("v1.102.0", candidate),
	}
}

func TestVerifySDKArchivesAcceptsACacheThatHoldsBoth(t *testing.T) {
	root, sdk := sdkFixture(t, "released bytes", "candidate bytes")
	if err := VerifySDKArchives(root, sdk); err != nil {
		t.Fatalf("a cache holding exactly the declared archives was refused: %v", err)
	}
}

func TestVerifySDKArchivesReportsBothSidesAtOnce(t *testing.T) {
	root, sdk := sdkFixture(t, "released bytes", "candidate bytes")
	// Both wrong, both missing from the report would be two round trips for one
	// broken cache.
	sdk.ReleasedArchiveSHA256 = strings.Repeat("b", 64)
	sdk.CandidateArchiveSHA256 = strings.Repeat("c", 64)
	err := VerifySDKArchives(root, sdk)
	if err == nil {
		t.Fatal("two wrong digests were accepted")
	}
	for _, version := range []string{"v1.101.0", "v1.102.0"} {
		if !strings.Contains(err.Error(), version) {
			t.Fatalf("the refusal %q does not name %s, so fixing this cache takes two runs",
				err, version)
		}
	}
}

func TestVerifySDKArchivesTellsMissingApartFromWrong(t *testing.T) {
	t.Run("missing", func(t *testing.T) {
		root, sdk := sdkFixture(t, "", "candidate bytes")
		err := VerifySDKArchives(root, sdk)
		if err == nil || !strings.Contains(err.Error(), "not in the module cache") {
			t.Fatalf("an absent archive was accepted or misdescribed: %v", err)
		}
	})
	t.Run("wrong", func(t *testing.T) {
		root, sdk := sdkFixture(t, "released bytes", "candidate bytes")
		sdk.ReleasedArchiveSHA256 = strings.Repeat("d", 64)
		err := VerifySDKArchives(root, sdk)
		if err == nil || !strings.Contains(err.Error(), "the policy declares") {
			t.Fatalf("a wrong digest was accepted or misdescribed: %v", err)
		}
		if strings.Contains(err.Error(), "not in the module cache") {
			t.Fatal("a present-but-wrong archive was reported as missing, which sends the " +
				"reader to re-download a file that is already there")
		}
	})
}

func TestVerifySDKArchivesRefusesAPolicyThatDeclaresNothing(t *testing.T) {
	root, _ := sdkFixture(t, "released bytes", "candidate bytes")
	err := VerifySDKArchives(root, SDKComparison{ModulePath: "example.com/sdk"})
	if err == nil {
		t.Fatal("a policy naming no versions or digests was accepted, so the check passed by " +
			"having nothing to check")
	}
}

func TestDefaultSDKDownloadRootMatchesTheModuleCacheLayout(t *testing.T) {
	got := DefaultSDKDownloadRoot(filepath.Join("home", "go", "pkg", "mod"), "github.com/jamesbraid/go-unifi")
	want := filepath.Join("home", "go", "pkg", "mod", "cache", "download",
		"github.com", "jamesbraid", "go-unifi", "@v")
	if got != want {
		t.Fatalf("download root = %s, want %s", got, want)
	}
}
