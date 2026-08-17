// Command catalog-migration-verify checks a migration/recovery receipt against
// the properties the campaign requires of it.
//
// It replaces a jq expression in catalog-controller-differential.yml that
// asserted the evidence-mode distribution as a literal. Two of the modes it
// named had been deleted from the Go enum three lines below the comment
// explaining why, so the gate could not pass -- and nothing could have caught
// that, because the assertion was YAML and the enum was Go with no compiler
// between them. Moving the assertion into Go is what closes that gap:
// releasequalification.AllEvidenceModes now fails to compile if a mode is
// deleted without its readers being corrected.
//
// This command writes no receipt, so it takes no -tree-state. Tree state
// records which tree an artifact describes; a check that produces no artifact
// has nothing to stamp.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/ubiquiti-community/terraform-provider-unifi/internal/releasequalification"
)

func main() {
	if err := run(os.Args[1:], os.Stderr); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
}

func run(argv []string, stderr io.Writer) error {
	flags := flag.NewFlagSet("catalog-migration-verify", flag.ContinueOnError)
	flags.SetOutput(stderr)
	receiptPath := flags.String("receipt", "", "migration/recovery receipt to verify")
	if err := flags.Parse(argv); err != nil {
		return err
	}
	if *receiptPath == "" {
		return fmt.Errorf("-receipt is required")
	}

	data, err := os.ReadFile(*receiptPath)
	if err != nil {
		return err
	}
	var receipt releasequalification.MigrationRecoveryReceipt
	decoder := json.NewDecoder(bytes.NewReader(data))
	// Strict, like every other reader of these artifacts. A receipt carrying a
	// field this build does not know about is a receipt from a different
	// producer, and verifying it would describe something other than what ran.
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&receipt); err != nil {
		return fmt.Errorf("%s: %w", *receiptPath, err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return fmt.Errorf("%s carries multiple JSON values", *receiptPath)
		}
		return fmt.Errorf("%s: %w", *receiptPath, err)
	}

	return releasequalification.VerifyMigrationRecoveryReceipt(receipt)
}
