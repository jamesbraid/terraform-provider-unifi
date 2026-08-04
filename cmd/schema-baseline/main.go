package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/ubiquiti-community/terraform-provider-unifi/internal/schemabaseline"
)

const defaultProviderAddress = "registry.terraform.io/ubiquiti-community/unifi"

func main() {
	if err := run(os.Args[1:], os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string, stderr io.Writer) error {
	flags := flag.NewFlagSet("schema-baseline", flag.ContinueOnError)
	flags.SetOutput(stderr)
	providerAddress := flags.String("provider-address", defaultProviderAddress, "provider address to select")
	inputPath := flags.String("input", "", "raw Terraform or OpenTofu schema JSON")
	canonicalPath := flags.String("canonical-output", "", "canonical provider schema output")
	digestsPath := flags.String("digests-output", "", "schema digest manifest output")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *inputPath == "" {
		return fmt.Errorf("input path is required")
	}
	if *canonicalPath == "" {
		return fmt.Errorf("canonical output path is required")
	}
	if *digestsPath == "" {
		return fmt.Errorf("digests output path is required")
	}

	raw, err := os.ReadFile(*inputPath)
	if err != nil {
		return fmt.Errorf("read input: %w", err)
	}
	canonical, digests, err := schemabaseline.Canonicalize(raw, *providerAddress)
	if err != nil {
		return err
	}
	if err := os.WriteFile(*canonicalPath, canonical, 0o644); err != nil {
		return fmt.Errorf("write canonical output: %w", err)
	}
	if err := os.WriteFile(*digestsPath, digests, 0o644); err != nil {
		return fmt.Errorf("write digests output: %w", err)
	}
	return nil
}
