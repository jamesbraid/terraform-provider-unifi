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

	var controller catalogparity.ControllerDifferentialReceipt
	controllerSHA256, err := decodeStrictFile(*controllerPath, &controller)
	if err != nil {
		fmt.Fprintf(stderr, "controller: %v\n", err)
		return 1
	}
	receipt, err := releasequalification.BuildHardwareDispositionReceipt(controller, controllerSHA256)
	if err != nil {
		fmt.Fprintf(stderr, "hardware disposition: %v\n", err)
		return 1
	}
	data, err := json.Marshal(receipt)
	if err != nil {
		fmt.Fprintf(stderr, "encode hardware disposition: %v\n", err)
		return 1
	}
	if err := writeAtomic(*outputPath, append(data, '\n')); err != nil {
		fmt.Fprintf(stderr, "write hardware disposition: %v\n", err)
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
	temporary, err := os.CreateTemp(filepath.Dir(path), ".catalog-hardware-disposition-*")
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
