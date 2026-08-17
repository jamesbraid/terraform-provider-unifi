package schemaparity

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// BaselineManifest is build/m0/provider-baseline.json.
//
// It is the EXPECTATION side of a comparison against the live environment, not
// a receipt: the toolchain and CLI versions a promotable build must have been
// produced with. Regenerating it from the tree would make both sides of that
// comparison share a source, which is why nothing derives it.
type BaselineManifest struct {
	Provider struct {
		Platform string `json:"platform"`
	} `json:"provider"`
	Toolchain struct {
		GoVersion string `json:"go_version"`
	} `json:"toolchain"`
	Clients struct {
		Terraform ClientPin `json:"terraform"`
		OpenTofu  ClientPin `json:"opentofu"`
	} `json:"clients"`
}

// ClientPin is one CLI's expected identity.
type ClientPin struct {
	Version      string `json:"version"`
	BinarySHA256 string `json:"binary_sha256"`
}

// LoadBaselineManifest reads the promotion expectations.
func LoadBaselineManifest(path string) (BaselineManifest, error) {
	raw, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return BaselineManifest{}, fmt.Errorf("read %s: %w", path, err)
	}
	var manifest BaselineManifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return BaselineManifest{}, fmt.Errorf("parse %s: %w", path, err)
	}
	return manifest, nil
}

// Environment is what the run actually measured about itself.
type Environment struct {
	Platform          string
	GoVersion         string
	TerraformVersion  string
	TerraformSHA256   string
	TofuVersion       string
	TofuSHA256        string
	ReleasedAuthority string
}

// PromotionBlockers names every way the environment differs from the baseline.
//
// It returns NAMES rather than a boolean because the receipt records them and a
// reader needs to know which one moved. The shell it replaces appended to an
// array and rendered it with jq; the failure mode that made this worth porting
// is not the array, it is that `evidence_gap_count` downstream was a string
// that could be empty -- `test "" -le 6` is a runtime error, and an int is not.
//
// Sorted so the receipt is stable: an unstable order would make two identical
// runs produce different bytes, and byte-comparison of receipts is how several
// gates in this repository work.
func PromotionBlockers(env Environment, baseline BaselineManifest) []string {
	var blockers []string
	add := func(name string, got, want string) {
		if got != want {
			blockers = append(blockers, name)
		}
	}
	add("go_version", env.GoVersion, baseline.Toolchain.GoVersion)
	add("platform", env.Platform, baseline.Provider.Platform)
	add("terraform_version", env.TerraformVersion, baseline.Clients.Terraform.Version)
	add("terraform_binary", env.TerraformSHA256, baseline.Clients.Terraform.BinarySHA256)
	add("tofu_version", env.TofuVersion, baseline.Clients.OpenTofu.Version)
	add("tofu_binary", env.TofuSHA256, baseline.Clients.OpenTofu.BinarySHA256)
	// The released side must come from the published archive, not a rebuild.
	// A source rebuild is allowed to PRODUCE the comparison and not to promote
	// it: rebuilding proves the tag still compiles to the same schema, and the
	// archive is what users actually ran.
	add("released_binary_authority", env.ReleasedAuthority, "published_archive")
	sort.Strings(blockers)
	return blockers
}
