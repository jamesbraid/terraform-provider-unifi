package main

import (
	"flag"
	"fmt"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/cmdio"
	"io"
	"os"
	"path/filepath"
	"regexp"

	"github.com/ubiquiti-community/terraform-provider-unifi/internal/providercompiler"
)

var artifactPrefixPattern = regexp.MustCompile(`^[a-z0-9_]+$`)

func main() {
	os.Exit(run(os.Args[1:], os.Stderr))
}

func run(args []string, stderr io.Writer) int {
	flags := flag.NewFlagSet("provider-spec-compiler", flag.ContinueOnError)
	flags.SetOutput(stderr)
	bootstrapPath := flags.String("bootstrap", "", "path to the structural bootstrap projection")
	catalogPath := flags.String("catalog", "", "path to the admitted observed catalog")
	policyPath := flags.String("policy", "", "path to the provider policy")
	baselinePath := flags.String("baseline", "", "path to the M0 schema digest manifest")
	ledgerPath := flags.String("ledger", "", "path to the complete catalog admission ledger")
	artifactPrefix := flags.String("artifact-prefix", "", "prefix for generated artifact names")
	outputDir := flags.String("output-dir", "", "directory for generated compiler artifacts")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if (*bootstrapPath == "") == (*catalogPath == "") || *policyPath == "" || *baselinePath == "" || *ledgerPath == "" || *artifactPrefix == "" || *outputDir == "" {
		fmt.Fprintln(stderr, "exactly one of bootstrap or catalog plus policy, baseline, ledger, artifact-prefix, and output-dir are required")
		return 2
	}
	if !artifactPrefixPattern.MatchString(*artifactPrefix) {
		fmt.Fprintln(stderr, "artifact-prefix must match [a-z0-9_]+")
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(stderr, "positional arguments are not supported")
		return 2
	}

	var bootstrap, catalog []byte
	var err error
	if *bootstrapPath != "" {
		bootstrap, err = os.ReadFile(*bootstrapPath)
		if err != nil {
			fmt.Fprintf(stderr, "read bootstrap: %v\n", err)
			return 1
		}
	} else {
		catalog, err = os.ReadFile(*catalogPath)
		if err != nil {
			fmt.Fprintf(stderr, "read catalog: %v\n", err)
			return 1
		}
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
	ledger, err := os.ReadFile(*ledgerPath)
	if err != nil {
		fmt.Fprintf(stderr, "read ledger: %v\n", err)
		return 1
	}

	result, err := providercompiler.Compile(providercompiler.CompileInput{
		Bootstrap:       bootstrap,
		Catalog:         catalog,
		Policy:          policy,
		BaselineDigests: baseline,
		Ledger:          ledger,
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
		{*artifactPrefix + ".provider-code-spec.json", result.ProviderCodeSpec},
		{*artifactPrefix + ".impact.json", result.ImpactReport},
		{*artifactPrefix + ".mapping.json", result.MappingReport},
	}
	for _, artifact := range artifacts {
		if err := cmdio.WriteAtomic(filepath.Join(*outputDir, artifact.name), artifact.data, cmdio.NoParentDir(), cmdio.Mode(0o644)); err != nil {
			fmt.Fprintf(stderr, "write %s: %v\n", artifact.name, err)
			return 1
		}
	}
	return 0
}
