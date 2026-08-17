// Command receipt-gate asserts a receipt, replacing the `jq -e` one-liners that
// used to make these assertions inside .woodpecker/*.yml.
//
// The move is not about shell. An assertion in a `commands:` entry cannot be
// handed an input that should fail it -- its receipt is a /tmp path that exists
// only mid-pipeline, and it says nothing on any run that passes. Thirteen of
// this repository's fourteen workflow gates are in that position, which is how
// four separate defects in them survived: a gate asserting evidence modes the
// producer had deleted, one comparing a constant to itself, and two on receipts
// nothing could decode.
//
// The same assertions live in internal/catalogparity now, where every clause is
// watched failing in a test. This command is the other caller.
//
// Usage:
//
//	go run ./cmd/receipt-gate -gate catalog-build-schema -receipt /tmp/catalog-build-schema.json
//
// Nothing is printed on success. A refusal names every clause that failed, not
// just the first: an operator who fixes one and re-runs a controller campaign
// to find the next has paid an hour for information the receipt already had.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/ubiquiti-community/terraform-provider-unifi/internal/catalogparity"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/releasequalification"
)

func main() {
	gate := flag.String("gate", "", "which receipt to assert")
	receipt := flag.String("receipt", "", "path to the receipt")
	flag.Parse()

	if err := run(*gate, *receipt); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(gate, path string) error {
	if gate == "" {
		return fmt.Errorf("-gate is required; one of %s", strings.Join(gateNames(), ", "))
	}
	if path == "" {
		return fmt.Errorf("-receipt is required: the path to the %s receipt", gate)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("%s: %w", gate, err)
	}

	switch gate {
	case "catalog-build-schema":
		var parsed catalogparity.BuildSchemaReceipt
		if err := decode(gate, path, body, &parsed); err != nil {
			return err
		}
		return catalogparity.CheckBuildSchemaReceipt(parsed)
	case "catalog-unit-differential":
		var parsed catalogparity.UnitDifferentialReceipt
		if err := decode(gate, path, body, &parsed); err != nil {
			return err
		}
		return catalogparity.CheckUnitDifferentialReceipt(parsed)
	case "catalog-controller-differential":
		var parsed catalogparity.ControllerDifferentialReceipt
		if err := decode(gate, path, body, &parsed); err != nil {
			return err
		}
		return catalogparity.CheckControllerDifferentialReceipt(parsed)
	case "catalog-dependency-publishability":
		var parsed releasequalification.DependencyPublishabilityReceipt
		if err := decode(gate, path, body, &parsed); err != nil {
			return err
		}
		return releasequalification.CheckDependencyPublishabilityReceipt(parsed)
	default:
		return fmt.Errorf("unknown gate %q; one of %s", gate, strings.Join(gateNames(), ", "))
	}
}

// decode is strict, and the strictness is a gate in its own right rather than
// tidiness. A producer that adds a field its consumer does not know about is
// how a receipt comes to be written by one half of the pipeline and rejected by
// the other -- twice already, both times found by someone running the consumer
// by hand because no workflow did.
func decode(gate, path string, body []byte, into any) error {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(into); err != nil {
		return fmt.Errorf("%s: %s does not fit the type this gate reads:\n    %w\n\n"+
			"    The receipt was written and cannot be understood here, which is a\n"+
			"    disagreement between producer and consumer rather than a bad run.",
			gate, path, err)
	}
	return nil
}

func gateNames() []string {
	names := []string{
		"catalog-build-schema",
		"catalog-dependency-publishability",
		"catalog-unit-differential",
		"catalog-controller-differential",
	}
	sort.Strings(names)
	return names
}
