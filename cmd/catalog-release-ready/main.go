package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/cmdio"
	"io"
	"os"
	"path/filepath"

	"github.com/ubiquiti-community/terraform-provider-unifi/internal/releasequalification"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stderr))
}

func run(args []string, stderr io.Writer) int {
	flags := flag.NewFlagSet("catalog-release-ready", flag.ContinueOnError)
	flags.SetOutput(stderr)
	ledgerPath := flags.String("ledger", "", "input catalog parity ledger")
	managementPath := flags.String("management-contract", "", "provider catalog management contract")
	migrationPath := flags.String("migration-recovery", "", "catalog migration and recovery receipt")
	contractParityPath := flags.String("contract-parity", "", "downstream catalog contract parity receipt")
	fleetSoakPath := flags.String("fleet-soak", "", "private fleet soak receipt")
	hardwarePath := flags.String("hardware-disposition", "", "scoped port-action hardware disposition")
	dependencyPath := flags.String("dependency-publishability", "", "canonical dependency publishability receipt")
	confidentialityPath := flags.String("confidentiality", "", "public-export confidentiality receipt")
	receiptOutput := flags.String("receipt-output", "", "full-catalog release-ready receipt")
	ledgerOutput := flags.String("ledger-output", "", "release-ready catalog ledger")
	contractOutput := flags.String("contract-output", "", "release promotion contract")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if *ledgerPath == "" || *managementPath == "" || *migrationPath == "" ||
		*contractParityPath == "" || *fleetSoakPath == "" || *hardwarePath == "" ||
		*dependencyPath == "" || *confidentialityPath == "" || *receiptOutput == "" ||
		*ledgerOutput == "" || *contractOutput == "" {
		fmt.Fprintln(stderr, "ledger, management-contract, migration-recovery, contract-parity, fleet-soak, hardware-disposition, dependency-publishability, confidentiality, receipt-output, ledger-output, and contract-output are required")
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(stderr, "positional arguments are not supported")
		return 2
	}
	if duplicateOutput(*receiptOutput, *ledgerOutput, *contractOutput) {
		fmt.Fprintln(stderr, "receipt-output, ledger-output, and contract-output must be distinct")
		return 2
	}

	var input releasequalification.ReleaseReadyInput
	var err error
	input.LedgerSHA256, err = decodeStrictFile(*ledgerPath, &input.Ledger)
	if err != nil {
		fmt.Fprintf(stderr, "ledger: %v\n", err)
		return 1
	}
	input.ManagementSHA256, err = decodeStrictFile(*managementPath, &input.Management)
	if err != nil {
		fmt.Fprintf(stderr, "management contract: %v\n", err)
		return 1
	}
	input.MigrationSHA256, err = decodeStrictFile(*migrationPath, &input.Migration)
	if err != nil {
		fmt.Fprintf(stderr, "migration/recovery: %v\n", err)
		return 1
	}
	input.ContractParitySHA256, err = decodeStrictFile(*contractParityPath, &input.ContractParity)
	if err != nil {
		fmt.Fprintf(stderr, "contract parity: %v\n", err)
		return 1
	}
	input.FleetSoakSHA256, err = decodeStrictFile(*fleetSoakPath, &input.FleetSoak)
	if err != nil {
		fmt.Fprintf(stderr, "fleet soak: %v\n", err)
		return 1
	}
	input.HardwareSHA256, err = decodeStrictFile(*hardwarePath, &input.Hardware)
	if err != nil {
		fmt.Fprintf(stderr, "hardware disposition: %v\n", err)
		return 1
	}
	input.DependencySHA256, err = decodeStrictFile(*dependencyPath, &input.Dependency)
	if err != nil {
		fmt.Fprintf(stderr, "dependency publishability: %v\n", err)
		return 1
	}
	input.ConfidentialitySHA256, err = decodeStrictFile(*confidentialityPath, &input.Confidentiality)
	if err != nil {
		fmt.Fprintf(stderr, "confidentiality: %v\n", err)
		return 1
	}

	artifacts, err := releasequalification.BuildReleaseReadyArtifacts(input)
	if err != nil {
		fmt.Fprintf(stderr, "release-ready: %v\n", err)
		return 1
	}
	outputs := []struct {
		label string
		path  string
		value any
	}{
		{"release-ready receipt", *receiptOutput, artifacts.Receipt},
		{"release-ready ledger", *ledgerOutput, artifacts.Ledger},
		{"promotion contract", *contractOutput, artifacts.Contract},
	}
	for _, output := range outputs {
		data, err := json.Marshal(output.value)
		if err != nil {
			fmt.Fprintf(stderr, "encode %s: %v\n", output.label, err)
			return 1
		}
		if err := cmdio.WriteAtomic(output.path, append(data, '\n')); err != nil {
			fmt.Fprintf(stderr, "write %s: %v\n", output.label, err)
			return 1
		}
	}
	return 0
}

func decodeStrictFile(path string, value any) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return "", err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return "", fmt.Errorf("multiple JSON values")
		}
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func duplicateOutput(paths ...string) bool {
	seen := map[string]struct{}{}
	for _, path := range paths {
		absolute, err := filepath.Abs(path)
		if err != nil {
			return true
		}
		if _, duplicate := seen[absolute]; duplicate {
			return true
		}
		seen[absolute] = struct{}{}
	}
	return false
}
