package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"

	"github.com/ubiquiti-community/terraform-provider-unifi/internal/cmdio"
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
	policyPath := flags.String("policy", "", "path to the provider policy")
	artifactPrefix := flags.String("artifact-prefix", "", "prefix for generated artifact names")
	outputDir := flags.String("output-dir", "", "directory for generated compiler artifacts")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if *bootstrapPath == "" || *policyPath == "" || *artifactPrefix == "" || *outputDir == "" {
		fmt.Fprintln(stderr, "bootstrap, policy, artifact-prefix, and output-dir are required")
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

	result, err := providercompiler.Compile(providercompiler.CompileInput{Bootstrap: bootstrap, Policy: policy})
	var mismatch *providercompiler.DigestMismatchError
	if errors.As(err, &mismatch) && providercompiler.WellFormedDigest(mismatch.Policy) {
		// A well-formed but stale digest means the SDK struct moved under
		// the policy; re-pin in place so the diff is the review signal.
		repinned, repinErr := providercompiler.RepinPolicy(policy, mismatch.Policy, mismatch.Bootstrap)
		if repinErr != nil {
			fmt.Fprintf(stderr, "compile: %v; re-pin refused: %v\n", err, repinErr)
			return 1
		}
		if writeErr := os.WriteFile(*policyPath, repinned, 0o600); writeErr != nil { // #nosec G703 -- policyPath comes from the -policy build-time flag
			fmt.Fprintf(stderr, "write re-pinned policy: %v\n", writeErr)
			return 1
		}
		fmt.Fprintf(stderr, "re-pinned %s: %s -> %s\n", *policyPath, mismatch.Policy[:8], mismatch.Bootstrap[:8])
		result, err = providercompiler.Compile(providercompiler.CompileInput{Bootstrap: bootstrap, Policy: repinned})
	}
	if err != nil {
		fmt.Fprintf(stderr, "compile: %v\n", err)
		return 1
	}
	// Notices are not errors -- the compile already succeeded -- but they
	// are worth a human seeing, so every plain go generate run (and its CI
	// log) carries them without anyone having to know to grep the
	// compiler's source for the fallback that produced them.
	for _, notice := range result.Notices {
		fmt.Fprintln(stderr, notice)
	}
	if err := os.MkdirAll(*outputDir, 0o750); err != nil {
		fmt.Fprintf(stderr, "create output directory: %v\n", err)
		return 1
	}

	artifacts := []struct {
		name string
		data []byte
	}{
		{*artifactPrefix + ".provider-code-spec.json", result.ProviderCodeSpec},
		{*artifactPrefix + ".mapping.json", result.MappingReport},
	}
	for _, artifact := range artifacts {
		// A list resource's mapping report is empty by design (see
		// providercompiler.Compile); writing a zero-byte file for it would
		// be a committed artifact with nothing in it to review.
		if len(artifact.data) == 0 {
			continue
		}
		if err := cmdio.WriteAtomic(filepath.Join(*outputDir, artifact.name), artifact.data, cmdio.NoParentDir(), cmdio.Mode(0o644)); err != nil {
			fmt.Fprintf(stderr, "write %s: %v\n", artifact.name, err)
			return 1
		}
	}
	return 0
}
