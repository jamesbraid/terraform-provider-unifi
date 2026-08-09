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
	flags := flag.NewFlagSet("catalog-admission", flag.ContinueOnError)
	flags.SetOutput(stderr)
	policyPath := flags.String("policy", "", "campaign policy")
	inventoryPath := flags.String("inventory", "", "catalog evidence inventory")
	buildSchemaPath := flags.String("build-schema", "", "exact build and schema receipt")
	unitPath := flags.String("unit", "", "unit and HTTP-boundary differential receipt")
	controllerPath := flags.String("controller", "", "controller differential receipt")
	pragmaticPath := flags.String("pragmatic", "", "pragmatic reference resolution")
	outputPath := flags.String("output", "", "catalog admission receipt")
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

	var input catalogparity.AdmissionInput
	var err error
	if _, err := decodeStrictFile(*policyPath, &input.Policy); err != nil {
		fmt.Fprintf(stderr, "campaign policy: %v\n", err)
		return 1
	}
	input.InventorySHA256, err = decodeStrictFile(*inventoryPath, &input.Inventory)
	if err != nil {
		fmt.Fprintf(stderr, "inventory: %v\n", err)
		return 1
	}
	input.BuildSchemaSHA256, err = decodeStrictFile(*buildSchemaPath, &input.BuildSchema)
	if err != nil {
		fmt.Fprintf(stderr, "build/schema receipt: %v\n", err)
		return 1
	}
	input.UnitSHA256, err = decodeStrictFile(*unitPath, &input.Unit)
	if err != nil {
		fmt.Fprintf(stderr, "unit receipt: %v\n", err)
		return 1
	}
	input.ControllerSHA256, err = decodeStrictFile(*controllerPath, &input.Controller)
	if err != nil {
		fmt.Fprintf(stderr, "controller receipt: %v\n", err)
		return 1
	}
	input.PragmaticSHA256, err = decodeStrictFile(*pragmaticPath, &input.Pragmatic)
	if err != nil {
		fmt.Fprintf(stderr, "pragmatic resolution: %v\n", err)
		return 1
	}

	receipt, err := catalogparity.BuildAdmission(input)
	if err != nil {
		fmt.Fprintf(stderr, "admission: %v\n", err)
		return 1
	}
	data, err := json.Marshal(receipt)
	if err != nil {
		fmt.Fprintf(stderr, "encode admission: %v\n", err)
		return 1
	}
	if err := writeAtomic(*outputPath, append(data, '\n')); err != nil {
		fmt.Fprintf(stderr, "write admission: %v\n", err)
		return 1
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

func writeAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".catalog-admission-*")
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
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryPath, path)
}
