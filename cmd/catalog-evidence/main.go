package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/ubiquiti-community/terraform-provider-unifi/internal/catalogparity"
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
	releasedRoot := flags.String("released-root", "", "extracted released provider source root")
	candidateRoot := flags.String("candidate-root", "", "candidate provider source root")
	outputPath := flags.String("output", "", "evidence inventory output")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if *baselinePath == "" || *contractsPath == "" || *policyPath == "" || *releasedRoot == "" || *candidateRoot == "" || *outputPath == "" {
		fmt.Fprintln(stderr, "baseline, contracts, policy, released-root, candidate-root, and output are required")
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
