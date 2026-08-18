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
	flags := flag.NewFlagSet("catalog-migration-recovery", flag.ContinueOnError)
	flags.SetOutput(stderr)
	policyPath := flags.String("policy", "", "campaign policy")
	admissionPath := flags.String("admission", "", "passing catalog admission receipt")
	buildSchemaPath := flags.String("build-schema", "", "exact build and schema receipt")
	controllerPath := flags.String("controller", "", "complete controller differential receipt")
	inventoryPath := flags.String("inventory", "", "catalog evidence inventory")
	manifestPath := flags.String("migration-manifest", "", "catalog migration manifest")
	dnsLifecyclePath := flags.String("dns-lifecycle", "", "M3 DNS lifecycle receipt")
	outputPath := flags.String("output", "", "catalog migration and recovery receipt")
	treeStateRaw := flags.String("tree-state", "",
		"tree state JSON from evidence_tree_json, measured in THIS step")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if *policyPath == "" || *admissionPath == "" || *buildSchemaPath == "" || *controllerPath == "" ||
		*inventoryPath == "" || *manifestPath == "" || *dnsLifecyclePath == "" || *outputPath == "" {
		fmt.Fprintln(stderr, "policy, admission, build-schema, controller, inventory, migration-manifest, dns-lifecycle, and output are required")
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

	var input releasequalification.MigrationRecoveryInput
	var err error
	if _, err := cmdio.DecodeStrictFile(*policyPath, &input.Policy); err != nil {
		fmt.Fprintf(stderr, "campaign policy: %v\n", err)
		return 1
	}
	input.AdmissionSHA256, err = cmdio.DecodeStrictFile(*admissionPath, &input.Admission)
	if err != nil {
		fmt.Fprintf(stderr, "admission: %v\n", err)
		return 1
	}
	input.BuildSchemaSHA256, err = cmdio.DecodeStrictFile(*buildSchemaPath, &input.BuildSchema)
	if err != nil {
		fmt.Fprintf(stderr, "build/schema: %v\n", err)
		return 1
	}
	input.ControllerSHA256, err = cmdio.DecodeStrictFile(*controllerPath, &input.Controller)
	if err != nil {
		fmt.Fprintf(stderr, "controller: %v\n", err)
		return 1
	}
	input.InventorySHA256, err = cmdio.DecodeStrictFile(*inventoryPath, &input.Inventory)
	if err != nil {
		fmt.Fprintf(stderr, "inventory: %v\n", err)
		return 1
	}
	input.ManifestSHA256, err = cmdio.DecodeStrictFile(*manifestPath, &input.Manifest)
	if err != nil {
		fmt.Fprintf(stderr, "migration manifest: %v\n", err)
		return 1
	}
	input.DNSLifecycleSHA256, err = cmdio.DecodeStrictFile(*dnsLifecyclePath, &input.DNSLifecycle)
	if err != nil {
		fmt.Fprintf(stderr, "DNS lifecycle: %v\n", err)
		return 1
	}

	receipt, err := releasequalification.BuildMigrationRecoveryReceipt(input)
	if err != nil {
		fmt.Fprintf(stderr, "migration/recovery: %v\n", err)
		return 1
	}
	receipt.TreeState = treeState
	data, err := json.Marshal(receipt)
	if err != nil {
		fmt.Fprintf(stderr, "encode migration/recovery receipt: %v\n", err)
		return 1
	}
	if err := cmdio.WriteAtomic(*outputPath, append(data, '\n')); err != nil {
		fmt.Fprintf(stderr, "write migration/recovery receipt: %v\n", err)
		return 1
	}
	return 0
}
