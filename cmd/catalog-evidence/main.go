package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/ubiquiti-community/terraform-provider-unifi/internal/catalogparity"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/releasedtree"
)

type evidencePolicy struct {
	FormatVersion    int                            `json:"format_version"`
	ProviderAddress  string                         `json:"provider_address"`
	ReleasedProvider catalogparity.ReleasedProvider `json:"released_provider"`
	SDK              catalogparity.SDKComparison    `json:"sdk"`
}

func main() {
	os.Exit(run(os.Args[1:], os.Stderr))
}

func run(args []string, stderr io.Writer) int {
	flags := flag.NewFlagSet("catalog-evidence", flag.ContinueOnError)
	flags.SetOutput(stderr)
	baselinePath := flags.String("baseline", "", "released schema digest manifest")
	contractsPath := flags.String("contracts", "", "catalog surface contract corpus")
	policyPath := flags.String("policy", "", "catalog evidence lineage policy")
	releasedRoot := flags.String("released-root", "",
		"extracted released provider source root; when empty the released tag is extracted here")
	candidateRoot := flags.String("candidate-root", "", "candidate provider source root")
	outputPath := flags.String("output", "", "evidence inventory output")
	// The three flags below carry what .woodpecker/scripts/catalog-evidence-inventory.sh
	// did around this binary. That script was a wrapper: its only work was
	// checking the released tag, unpacking it, and verifying the two SDK
	// archives in the module cache, and then it ran this. Preconditions that
	// live outside the tool they protect are preconditions a second caller
	// silently does without.
	repository := flags.String("repo", ".", "repository the released tag is read from")
	skipSDKArchives := flags.Bool("skip-sdk-archive-check", false,
		"do not verify the SDK archives in the module cache; for a run that has no module cache")
	sdkDownloadRoot := flags.String("sdk-download-root", "",
		"module cache download directory holding the SDK archives (default: derived from GOMODCACHE)")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if *baselinePath == "" || *contractsPath == "" || *policyPath == "" || *candidateRoot == "" || *outputPath == "" {
		fmt.Fprintln(stderr, "baseline, contracts, policy, candidate-root, and output are required")
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(stderr, "positional arguments are not supported")
		return 2
	}

	baselineData, err := os.ReadFile(*baselinePath)
	if err != nil {
		fmt.Fprintf(stderr, "read baseline: %v\n", err)
		return 1
	}
	baseline, err := catalogparity.ParseBaseline(baselineData)
	if err != nil {
		fmt.Fprintf(stderr, "baseline: %v\n", err)
		return 1
	}
	var contracts catalogparity.SurfaceContractCorpus
	if err := decodeStrictFile(*contractsPath, &contracts); err != nil {
		fmt.Fprintf(stderr, "contracts: %v\n", err)
		return 1
	}
	var policy evidencePolicy
	if err := decodeStrictFile(*policyPath, &policy); err != nil {
		fmt.Fprintf(stderr, "policy: %v\n", err)
		return 1
	}
	if policy.FormatVersion != 1 || policy.ProviderAddress != catalogparity.CanonicalProviderAddress {
		fmt.Fprintln(stderr, "policy identity is invalid")
		return 1
	}

	// THE TAG IS DERIVED FROM THE POLICY, not written down a second time. The
	// shell hardcoded v0.101.2 while reading the version out of this same file,
	// so the literal and the policy could disagree and nothing compared them.
	releasedTag := "v" + policy.ReleasedProvider.Version
	if _, err := releasedtree.RequireCommit(*repository, releasedTag, policy.ReleasedProvider.Commit); err != nil {
		fmt.Fprintf(stderr, "released provenance: %v\n", err)
		return 1
	}

	if !*skipSDKArchives {
		root := *sdkDownloadRoot
		if root == "" {
			cache, err := goModuleCache()
			if err != nil {
				fmt.Fprintf(stderr, "sdk archives: %v\n", err)
				return 1
			}
			root = catalogparity.DefaultSDKDownloadRoot(cache, policy.SDK.ModulePath)
		}
		if err := catalogparity.VerifySDKArchives(root, policy.SDK); err != nil {
			fmt.Fprintf(stderr, "sdk archives: %v\n", err)
			return 1
		}
	}

	if *releasedRoot == "" {
		work, err := os.MkdirTemp("", "catalog-evidence-")
		if err != nil {
			fmt.Fprintf(stderr, "released tree: %v\n", err)
			return 1
		}
		defer os.RemoveAll(work)
		extracted, err := releasedtree.ExtractTemp(*repository, releasedTag, work)
		if err != nil {
			fmt.Fprintf(stderr, "released tree: %v\n", err)
			return 1
		}
		releasedRoot = &extracted
	}

	inventory, err := catalogparity.BuildEvidenceInventory(catalogparity.EvidenceInventoryInput{
		Baseline:         baseline,
		Contracts:        contracts,
		ReleasedRoot:     *releasedRoot,
		CandidateRoot:    *candidateRoot,
		ReleasedProvider: policy.ReleasedProvider,
		SDK:              policy.SDK,
	})
	if err != nil {
		fmt.Fprintf(stderr, "inventory: %v\n", err)
		return 1
	}
	data, err := json.Marshal(inventory)
	if err != nil {
		fmt.Fprintf(stderr, "encode inventory: %v\n", err)
		return 1
	}
	if err := writeAtomic(*outputPath, append(data, '\n')); err != nil {
		fmt.Fprintf(stderr, "write inventory: %v\n", err)
		return 1
	}
	return 0
}

func decodeStrictFile(path string, value any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return fmt.Errorf("multiple JSON values")
		}
		return err
	}
	return nil
}

func writeAtomic(path string, data []byte) error {
	directory := filepath.Dir(path)
	temporary, err := os.CreateTemp(directory, ".catalog-evidence-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if _, err := temporary.Write(data); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Chmod(0o644); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryPath, path)
}

// goModuleCache asks the toolchain rather than guessing at $HOME/go/pkg/mod.
// A run with GOMODCACHE set elsewhere -- which every CI step here does -- would
// otherwise have its archives checked in a directory it never populated, and
// report them missing.
func goModuleCache() (string, error) {
	out, err := exec.Command("go", "env", "GOMODCACHE").Output()
	if err != nil {
		return "", fmt.Errorf("go env GOMODCACHE: %w", err)
	}
	cache := strings.TrimSpace(string(out))
	if cache == "" {
		return "", fmt.Errorf("go env GOMODCACHE is empty")
	}
	return cache, nil
}
