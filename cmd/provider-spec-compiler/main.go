package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/ubiquiti-community/terraform-provider-unifi/internal/providercompiler"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stderr))
}

func run(args []string, stderr io.Writer) int {
	flags := flag.NewFlagSet("provider-spec-compiler", flag.ContinueOnError)
	flags.SetOutput(stderr)
	bootstrapPath := flags.String("bootstrap", "", "path to the structural bootstrap projection")
	policyPath := flags.String("policy", "", "path to the provider policy")
	baselinePath := flags.String("baseline", "", "path to the M0 schema digest manifest")
	outputDir := flags.String("output-dir", "", "directory for generated compiler artifacts")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if *bootstrapPath == "" || *policyPath == "" || *baselinePath == "" || *outputDir == "" {
		fmt.Fprintln(stderr, "bootstrap, policy, baseline, and output-dir are required")
		return 2
	}

	bootstrap, err := os.ReadFile(*bootstrapPath)
	if err != nil {
		fmt.Fprintf(stderr, "read bootstrap: %v\n", err)
		return 1
	}
	policy, err := os.ReadFile(*policyPath)
	if err != nil {
		fmt.Fprintf(stderr, "read policy: %v\n", err)
		return 1
	}
	baseline, err := os.ReadFile(*baselinePath)
	if err != nil {
		fmt.Fprintf(stderr, "read baseline: %v\n", err)
		return 1
	}

	result, err := providercompiler.Compile(providercompiler.CompileInput{
		Bootstrap:       bootstrap,
		Policy:          policy,
		BaselineDigests: baseline,
	})
	if err != nil {
		fmt.Fprintf(stderr, "compile: %v\n", err)
		return 1
	}
	if err := os.MkdirAll(*outputDir, 0o755); err != nil {
		fmt.Fprintf(stderr, "create output directory: %v\n", err)
		return 1
	}

	artifacts := []struct {
		name string
		data []byte
	}{
		{"dns_record.provider-code-spec.json", result.ProviderCodeSpec},
		{"dns_record.impact.json", result.ImpactReport},
		{"dns_record.mapping.json", result.MappingReport},
	}
	for _, artifact := range artifacts {
		if err := writeAtomic(filepath.Join(*outputDir, artifact.name), artifact.data); err != nil {
			fmt.Fprintf(stderr, "write %s: %v\n", artifact.name, err)
			return 1
		}
	}
	return 0
}

func writeAtomic(path string, data []byte) error {
	directory := filepath.Dir(path)
	temporary, err := os.CreateTemp(directory, ".provider-spec-compiler-*")
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
	if err := temporary.Chmod(0o644); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return err
	}

	dir, err := os.Open(directory)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
