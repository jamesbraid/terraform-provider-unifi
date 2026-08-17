package unitdifferential

import (
	"sort"

	"github.com/ubiquiti-community/terraform-provider-unifi/internal/schemaparity"
)

// PromotionBlockers names every way this run's environment differs from what
// the baseline manifest says a promotable one must be.
//
// IT CHECKS TWO THINGS WHERE THE SCHEMA GATE CHECKS SEVEN, and the difference
// is deliberate rather than an omission. This gate runs `go test`; it never
// invokes terraform or tofu. Pinning their versions here would block a run over
// tools it did not use, which is a check whose failure tells the reader nothing
// about what this gate measured -- and the remedy would have nothing to do with
// the tests that were run.
//
// The manifest type is shared with the schema gate rather than redeclared. Two
// declarations of the expectations would be two answers to "what must a
// promotable build have been produced with", and each receipt would be silent
// about which one it used.
func PromotionBlockers(platform, goVersion string, baseline schemaparity.BaselineManifest) []string {
	var blockers []string
	if goVersion != baseline.Toolchain.GoVersion {
		blockers = append(blockers, "go_version")
	}
	if platform != baseline.Provider.Platform {
		blockers = append(blockers, "platform")
	}
	// Sorted so two identical runs produce identical bytes; several gates here
	// compare receipts byte for byte.
	sort.Strings(blockers)
	return blockers
}
