package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/ubiquiti-community/terraform-provider-unifi/internal/catalogparity"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/cmdio"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/managementcontract"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stderr))
}

func run(args []string, stderr io.Writer) int {
	flags := flag.NewFlagSet("catalog-management-contract", flag.ContinueOnError)
	flags.SetOutput(stderr)
	admissionPath := flags.String("admission", "", "passing catalog admission receipt")
	buildSchemaPath := flags.String("build-schema", "", "exact build and schema receipt")
	policyPath := flags.String("policy", "", "catalog management contract policy")
	outputPath := flags.String("output", "", "catalog management contract")
	treeStateRaw := flags.String("tree-state", "",
		"tree state JSON from evidence_tree_json, measured in THIS step")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if *admissionPath == "" || *buildSchemaPath == "" || *policyPath == "" || *outputPath == "" {
		fmt.Fprintln(stderr, "admission, build-schema, policy, and output are required")
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

	var admission catalogparity.AdmissionReceipt
	admissionSHA256, err := cmdio.DecodeStrictFile(*admissionPath, &admission)
	if err != nil {
		fmt.Fprintf(stderr, "admission: %v\n", err)
		return 1
	}
	var build catalogparity.BuildSchemaReceipt
	buildSHA256, err := cmdio.DecodeStrictFile(*buildSchemaPath, &build)
	if err != nil {
		fmt.Fprintf(stderr, "build/schema: %v\n", err)
		return 1
	}
	var policy managementcontract.CatalogManagementPolicy
	policySHA256, err := cmdio.DecodeStrictFile(*policyPath, &policy)
	if err != nil {
		fmt.Fprintf(stderr, "policy: %v\n", err)
		return 1
	}

	contract, err := managementcontract.BuildCatalogManagementContract(
		admission,
		admissionSHA256,
		build,
		buildSHA256,
		policy,
		policySHA256,
	)
	if err != nil {
		fmt.Fprintf(stderr, "contract: %v\n", err)
		return 1
	}
	contract.TreeState = treeState
	data, err := json.Marshal(contract)
	if err != nil {
		fmt.Fprintf(stderr, "encode contract: %v\n", err)
		return 1
	}
	if err := cmdio.WriteAtomic(*outputPath, append(data, '\n')); err != nil {
		fmt.Fprintf(stderr, "write contract: %v\n", err)
		return 1
	}
	return 0
}
