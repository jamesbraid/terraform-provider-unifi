package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/cmdio"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/ubiquiti-community/terraform-provider-unifi/internal/catalogparity"
)

var manifestedOutputNames = []string{
	"catalog-parity-ledger.json",
	"catalog-migration-manifest.json",
	"catalog-migration-report.json",
	"catalog-surface-contracts.json",
}

func main() {
	os.Exit(run(os.Args[1:], os.Stderr))
}

func run(args []string, stderr io.Writer) int {
	flags := flag.NewFlagSet("catalog-parity", flag.ContinueOnError)
	flags.SetOutput(stderr)
	baselinePath := flags.String("baseline", "", "path to the released schema digest manifest")
	schemaPath := flags.String("schema", "", "path to the locked canonical provider schema")
	statusPath := flags.String("status", "", "path to the admission status overlay")
	migrationPath := flags.String("migration", "", "path to the migration policy")
	wavesPath := flags.String("waves", "", "path to the catalog wave policy")
	outputDir := flags.String("output-dir", "", "directory for canonical catalog artifacts")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if *baselinePath == "" || *schemaPath == "" || *statusPath == "" || *migrationPath == "" || *wavesPath == "" || *outputDir == "" {
		fmt.Fprintln(stderr, "baseline, schema, status, migration, waves, and output-dir are required")
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(stderr, "positional arguments are not supported")
		return 2
	}

	baselineData, err := readInput("baseline", *baselinePath)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	schemaData, err := readInput("schema", *schemaPath)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	statusData, err := readInput("status", *statusPath)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	migrationData, err := readInput("migration", *migrationPath)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	wavesData, err := readInput("waves", *wavesPath)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}

	baseline, err := catalogparity.ParseBaseline(baselineData)
	if err != nil {
		fmt.Fprintf(stderr, "baseline: %v\n", err)
		return 1
	}
	versions, err := catalogparity.ParseSchemaVersions(schemaData, baseline)
	if err != nil {
		fmt.Fprintf(stderr, "schema: %v\n", err)
		return 1
	}
	overlay, err := catalogparity.ParseStatusOverlay(statusData)
	if err != nil {
		fmt.Fprintf(stderr, "status: %v\n", err)
		return 1
	}
	ledger, err := catalogparity.BuildLedger(baseline, overlay)
	if err != nil {
		fmt.Fprintf(stderr, "status: %v\n", err)
		return 1
	}
	policy, err := catalogparity.ParseMigrationPolicy(migrationData)
	if err != nil {
		fmt.Fprintf(stderr, "migration: %v\n", err)
		return 1
	}
	manifest, err := catalogparity.ExpandMigration(baseline, versions, policy)
	if err != nil {
		fmt.Fprintf(stderr, "migration: %v\n", err)
		return 1
	}
	report, err := catalogparity.BuildMigrationReport(manifest)
	if err != nil {
		fmt.Fprintf(stderr, "migration report: %v\n", err)
		return 1
	}
	wavePolicy, err := catalogparity.ParseWavePolicy(wavesData)
	if err != nil {
		fmt.Fprintf(stderr, "waves: %v\n", err)
		return 1
	}
	corpus, err := catalogparity.BuildSurfaceContracts(baseline, versions, wavePolicy)
	if err != nil {
		fmt.Fprintf(stderr, "waves: %v\n", err)
		return 1
	}

	artifacts := []struct {
		name  string
		value any
	}{
		{name: manifestedOutputNames[0], value: ledger},
		{name: manifestedOutputNames[1], value: manifest},
		{name: manifestedOutputNames[2], value: report},
		{name: manifestedOutputNames[3], value: corpus},
	}
	encoded := make(map[string][]byte, len(artifacts))
	for _, artifact := range artifacts {
		data, err := marshalCanonical(artifact.value)
		if err != nil {
			fmt.Fprintf(stderr, "encode %s: %v\n", artifact.name, err)
			return 1
		}
		encoded[artifact.name] = data
	}

	if err := os.MkdirAll(*outputDir, 0o755); err != nil {
		fmt.Fprintf(stderr, "create output directory: %v\n", err)
		return 1
	}
	if err := rejectUnexpectedCatalogOutputs(*outputDir); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	for _, name := range manifestedOutputNames {
		if err := cmdio.WriteAtomic(filepath.Join(*outputDir, name), encoded[name], cmdio.NoParentDir(), cmdio.Mode(0o644)); err != nil {
			fmt.Fprintf(stderr, "write %s: %v\n", name, err)
			return 1
		}
	}
	return 0
}

func readInput(name, path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", name, err)
	}
	return data, nil
}

func rejectUnexpectedCatalogOutputs(outputDir string) error {
	entries, err := os.ReadDir(outputDir)
	if err != nil {
		return fmt.Errorf("read output directory: %w", err)
	}
	allowed := make(map[string]struct{}, len(manifestedOutputNames))
	for _, name := range manifestedOutputNames {
		allowed[name] = struct{}{}
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), "catalog-") || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		if _, ok := allowed[entry.Name()]; !ok {
			return fmt.Errorf("unexpected catalog output %q", entry.Name())
		}
	}
	return nil
}

func marshalCanonical(value any) ([]byte, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}
