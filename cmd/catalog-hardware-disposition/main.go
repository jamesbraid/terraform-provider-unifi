package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/cmdio"
	"io"
	"os"

	"github.com/ubiquiti-community/terraform-provider-unifi/internal/catalogparity"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/releasequalification"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stderr))
}

func run(args []string, stderr io.Writer) int {
	flags := flag.NewFlagSet("catalog-hardware-disposition", flag.ContinueOnError)
	flags.SetOutput(stderr)
	controllerPath := flags.String("controller", "", "catalog controller differential receipt")
	outputPath := flags.String("output", "", "scoped port-action hardware disposition")
	treeStateRaw := flags.String("tree-state", "",
		"tree state JSON from evidence_tree_json, measured in THIS step")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if *controllerPath == "" || *outputPath == "" {
		fmt.Fprintln(stderr, "controller and output are required")
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

	var controller catalogparity.ControllerDifferentialReceipt
	controllerSHA256, err := cmdio.DecodeStrictFile(*controllerPath, &controller)
	if err != nil {
		fmt.Fprintf(stderr, "controller: %v\n", err)
		return 1
	}
	receipt, err := releasequalification.BuildHardwareDispositionReceipt(controller, controllerSHA256)
	if err != nil {
		fmt.Fprintf(stderr, "hardware disposition: %v\n", err)
		return 1
	}
	receipt.TreeState = treeState
	data, err := json.Marshal(receipt)
	if err != nil {
		fmt.Fprintf(stderr, "encode hardware disposition: %v\n", err)
		return 1
	}
	if err := cmdio.WriteAtomic(*outputPath, append(data, '\n')); err != nil {
		fmt.Fprintf(stderr, "write hardware disposition: %v\n", err)
		return 1
	}
	return 0
}
