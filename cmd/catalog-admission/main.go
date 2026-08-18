package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/cmdio"
	"io"
	"os"

	"github.com/ubiquiti-community/terraform-provider-unifi/internal/catalogparity"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stderr))
}

func run(args []string, stderr io.Writer) int {
	flags := flag.NewFlagSet("catalog-admission", flag.ContinueOnError)
	flags.SetOutput(stderr)
	policyPath := flags.String("policy", "", "campaign policy")
	inventoryPath := flags.String("inventory", "", "catalog evidence inventory")
	buildSchemaPath := flags.String("build-schema", "", "exact build and schema receipt")
	unitPath := flags.String("unit", "", "unit and HTTP-boundary differential receipt")
	controllerPath := flags.String("controller", "", "controller differential receipt")
	pragmaticPath := flags.String("pragmatic", "", "pragmatic reference resolution")
	outputPath := flags.String("output", "", "catalog admission receipt")
	treeStateRaw := flags.String("tree-state", "",
		"tree state JSON from evidence_tree_json, measured in THIS step")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if *policyPath == "" || *inventoryPath == "" || *buildSchemaPath == "" || *unitPath == "" ||
		*controllerPath == "" || *pragmaticPath == "" || *outputPath == "" {
		fmt.Fprintln(stderr, "policy, inventory, build-schema, unit, controller, pragmatic, and output are required")
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(stderr, "positional arguments are not supported")
		return 2
	}
	// After the argument check -- which touches nothing -- and before any file is
	// opened, because a run that cannot say which tree it describes should cost
	// nothing to refuse. The shell gate this replaced ordered its own guard the
	// same way, for the same reason. There is no default; see ParseTreeState.
	treeState, treeStateErr := catalogparity.ParseTreeState(*treeStateRaw)
	if treeStateErr != nil {
		fmt.Fprintf(stderr, "%v\n", treeStateErr)
		return 2
	}

	var input catalogparity.AdmissionInput
	var err error
	if _, err := cmdio.DecodeStrictFile(*policyPath, &input.Policy); err != nil {
		fmt.Fprintf(stderr, "campaign policy: %v\n", err)
		return 1
	}
	input.InventorySHA256, err = cmdio.DecodeStrictFile(*inventoryPath, &input.Inventory)
	if err != nil {
		fmt.Fprintf(stderr, "inventory: %v\n", err)
		return 1
	}
	input.BuildSchemaSHA256, err = cmdio.DecodeStrictFile(*buildSchemaPath, &input.BuildSchema)
	if err != nil {
		fmt.Fprintf(stderr, "build/schema receipt: %v\n", err)
		return 1
	}
	input.UnitSHA256, err = cmdio.DecodeStrictFile(*unitPath, &input.Unit)
	if err != nil {
		fmt.Fprintf(stderr, "unit receipt: %v\n", err)
		return 1
	}
	input.ControllerSHA256, err = cmdio.DecodeStrictFile(*controllerPath, &input.Controller)
	if err != nil {
		fmt.Fprintf(stderr, "controller receipt: %v\n", err)
		return 1
	}
	input.PragmaticSHA256, err = cmdio.DecodeStrictFile(*pragmaticPath, &input.Pragmatic)
	if err != nil {
		fmt.Fprintf(stderr, "pragmatic resolution: %v\n", err)
		return 1
	}

	receipt, err := catalogparity.BuildAdmission(input)
	if err != nil {
		fmt.Fprintf(stderr, "admission: %v\n", err)
		return 1
	}
	receipt.TreeState = treeState
	data, err := json.Marshal(receipt)
	if err != nil {
		fmt.Fprintf(stderr, "encode admission: %v\n", err)
		return 1
	}
	if err := cmdio.WriteAtomic(*outputPath, append(data, '\n')); err != nil {
		fmt.Fprintf(stderr, "write admission: %v\n", err)
		return 1
	}
	return 0
}
