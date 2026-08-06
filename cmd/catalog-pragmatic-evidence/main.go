package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/ubiquiti-community/terraform-provider-unifi/internal/catalogparity"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stderr))
}

func run(args []string, stderr io.Writer) int {
	flags := flag.NewFlagSet("catalog-pragmatic-evidence", flag.ContinueOnError)
	flags.SetOutput(stderr)
	inventoryPath := flags.String("inventory", "", "catalog evidence inventory")
	fleetSummaryPath := flags.String("fleet-summary", "", "restricted value-free fleet summary")
	referencesPath := flags.String("references", "", "pragmatic reference policy")
	controllerReceiptPath := flags.String("controller-receipt", "", "passing controller differential receipt")
	outputPath := flags.String("output", "", "reference resolution output")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if *inventoryPath == "" || *fleetSummaryPath == "" || *referencesPath == "" || *outputPath == "" {
		fmt.Fprintln(stderr, "inventory, fleet-summary, references, and output are required")
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(stderr, "positional arguments are not supported")
		return 2
	}

	var inventory catalogparity.EvidenceInventory
	inventoryDigest, err := decodeStrictFile(*inventoryPath, &inventory)
	if err != nil {
		fmt.Fprintf(stderr, "inventory: %v\n", err)
		return 1
	}
	var fleetSummary catalogparity.FleetReferenceSummary
	fleetSummaryDigest, err := decodeStrictFile(*fleetSummaryPath, &fleetSummary)
	if err != nil {
		fmt.Fprintf(stderr, "fleet summary: %v\n", err)
		return 1
	}
	var references catalogparity.PragmaticReferenceSet
	if _, err := decodeStrictFile(*referencesPath, &references); err != nil {
		fmt.Fprintf(stderr, "references: %v\n", err)
		return 1
	}

	resolution, err := catalogparity.ResolvePragmaticReferences(
		inventory,
		inventoryDigest,
		fleetSummary,
		fleetSummaryDigest,
		references,
	)
	if err != nil {
		fmt.Fprintf(stderr, "resolve references: %v\n", err)
		return 1
	}
	if *controllerReceiptPath != "" {
		controllerReceiptSHA256, err := validateControllerReceipt(*controllerReceiptPath)
		if err != nil {
			fmt.Fprintf(stderr, "controller receipt: %v\n", err)
			return 1
		}
		resolution.ControllerReceiptSHA256 = controllerReceiptSHA256
	}
	data, err := json.Marshal(resolution)
	if err != nil {
		fmt.Fprintf(stderr, "encode resolution: %v\n", err)
		return 1
	}
	if err := writeAtomic(*outputPath, append(data, '\n')); err != nil {
		fmt.Fprintf(stderr, "write resolution: %v\n", err)
		return 1
	}
	return 0
}

type controllerReceipt struct {
	FormatVersion int    `json:"format_version"`
	Gate          string `json:"gate"`
	Result        string `json:"result"`
	Plan          struct {
		EvidenceGapCount int `json:"evidence_gap_count"`
	} `json:"plan"`
	Released struct {
		Result string `json:"result"`
	} `json:"released"`
	Candidate struct {
		Result string `json:"result"`
	} `json:"candidate"`
}

func validateControllerReceipt(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	var receipt controllerReceipt
	if err := json.Unmarshal(data, &receipt); err != nil {
		return "", err
	}
	if receipt.FormatVersion != 1 || receipt.Gate != "catalog controller differential" {
		return "", fmt.Errorf("identity is invalid")
	}
	if receipt.Result != "blocked_evidence" || receipt.Plan.EvidenceGapCount != 10 {
		return "", fmt.Errorf("catalog result is %q with %d gaps", receipt.Result, receipt.Plan.EvidenceGapCount)
	}
	if receipt.Released.Result != "pass" || receipt.Candidate.Result != "pass" {
		return "", fmt.Errorf("released and candidate results must pass")
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
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

func writeAtomic(path string, data []byte) error {
	temporary, err := os.CreateTemp(filepath.Dir(path), ".catalog-pragmatic-evidence-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if _, err := temporary.Write(data); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryPath, path)
}
