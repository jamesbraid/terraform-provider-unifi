// Command catalog-unit-differential runs the provider's unit suite against the
// released tag and against the candidate tree, and records what each did.
//
// It replaces .woodpecker/scripts/catalog-unit-differential.sh, whose judgement
// lived in a twenty-eight-line jq program that nothing could call. All of that
// is now internal/unitdifferential, which runs under go test without a suite, a
// tag, or a toolchain.
//
// EVERYTHING HERE IS ORCHESTRATION: resolve the tag, unpack it, run the suite
// twice, digest the logs, write the file. No judgement.
package main

import (
	"errors"
	"flag"
	"fmt"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/cmdio"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/ubiquiti-community/terraform-provider-unifi/internal/catalogparity"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/releasedtree"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/schemaparity"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/unitdifferential"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "catalog-unit-differential: %v\n", err)
		os.Exit(1)
	}
}

// run takes its arguments rather than reading the package-level flag set, so a
// test can exercise the refusals without os.Args and without a second test in
// the same binary redefining a flag.
func run(args []string) error {
	flags := flag.NewFlagSet("catalog-unit-differential", flag.ContinueOnError)
	repository := flags.String("repo", ".", "repository holding the candidate tree")
	baselinePath := flags.String("baseline-manifest", "build/m0/provider-baseline.json",
		"promotion expectations, relative to -repo")
	inventoryPath := flags.String("inventory", "build/release-ready/catalog-evidence-inventory.json",
		"evidence inventory whose digest this run records, relative to -repo")
	releasedRef := flags.String("released-ref", "", "released tag (default: the baseline manifest's)")
	output := flags.String("output", "", "write the receipt here; required")
	logRoot := flags.String("logs", "", "keep the raw test logs here (default: a temp dir, removed on exit)")
	treeStateRaw := flags.String("tree-state", "",
		"JSON from evidence_tree_json describing the working tree; required, no default")
	allowDiagnostic := flags.Bool("allow-diagnostic-toolchain", false,
		"record diagnostic_pass instead of stopping when the environment does not match the baseline")
	if err := flags.Parse(args); err != nil {
		return err
	}

	if *output == "" {
		return errors.New("-output is required")
	}
	// Parsed first, in both modes, so a call site that forgot the flag fails
	// here rather than producing a receipt nobody can check.
	treeState, err := catalogparity.ParseTreeState(*treeStateRaw)
	if err != nil {
		return err
	}
	repo, err := filepath.Abs(*repository)
	if err != nil {
		return err
	}

	baseline, err := schemaparity.LoadBaselineManifest(filepath.Join(repo, *baselinePath))
	if err != nil {
		return err
	}
	// Derived from the manifest rather than hardcoded. The shell wrote v0.101.2
	// as a default beside a manifest that also records the commit, so the two
	// could name different releases with nothing comparing them.
	tag := *releasedRef
	if tag == "" {
		tag = "v" + strings.TrimPrefix(baseline.Provider.Version, "v")
	}
	if tag == "v" {
		return errors.New("no released tag: pass -released-ref, or give the baseline manifest a version")
	}
	releasedCommit, err := releasedtree.RequireCommit(repo, tag, baseline.Provider.ReleasedCommit)
	if err != nil {
		return err
	}
	candidateCommit, err := cmdio.GitOutput(repo, "rev-parse", "HEAD")
	if err != nil {
		return err
	}

	work := *logRoot
	if work == "" {
		work, err = os.MkdirTemp("", "catalog-unit-differential-")
		if err != nil {
			return err
		}
		defer os.RemoveAll(work)
	}
	releasedRoot, err := releasedtree.ExtractTemp(repo, tag, work)
	if err != nil {
		return err
	}

	suites := map[string]catalogparity.UnitSuiteReceipt{}
	for _, side := range []struct{ label, root string }{
		{"released", releasedRoot},
		{"candidate", repo},
	} {
		receipt, err := runSuite(work, side.label, side.root)
		if err != nil {
			return err
		}
		suites[side.label] = receipt
	}

	platform, err := goEnvPair()
	if err != nil {
		return err
	}
	goVersion, err := goEnv("GOVERSION")
	if err != nil {
		return err
	}
	inventorySHA256, err := fileDigest(filepath.Join(repo, *inventoryPath))
	if err != nil {
		return err
	}

	receipt := unitdifferential.BuildUnitDifferentialReceipt(unitdifferential.Run{
		SourceCommit:               candidateCommit,
		ReleasedCommit:             releasedCommit,
		Platform:                   platform,
		GoVersion:                  goVersion,
		InventorySHA256:            inventorySHA256,
		Released:                   suites["released"],
		Candidate:                  suites["candidate"],
		TreeState:                  treeState,
		PromotionBlockers:          unitdifferential.PromotionBlockers(platform, goVersion, baseline),
		DiagnosticToolchainAllowed: *allowDiagnostic,
	})

	encoded, err := catalogparity.MarshalReceipt(receipt)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(*output), 0o750); err != nil {
		return err
	}
	if err := os.WriteFile(*output, encoded, 0o600); err != nil {
		return err
	}

	// THE RECEIPT IS WRITTEN BEFORE THE EXIT CODE IS DECIDED. The shell exited
	// without writing one when the environment did not match, so the run a
	// reader most wants to inspect afterwards is the one that left nothing
	// behind -- and a missing file cannot be told apart from a step that never
	// ran.
	if receipt.Result != "pass" && receipt.Result != "diagnostic_pass" {
		return fmt.Errorf("result is %q with blockers %v; the receipt is at %s",
			receipt.Result, receipt.PromotionBlockers, *output)
	}
	fmt.Fprintf(os.Stderr, "catalog-unit-differential: %s. released %d/%d, candidate %d/%d (packages/tests passing)\n",
		receipt.Result, receipt.Released.PackagePassCount, receipt.Released.PassedTestCount,
		receipt.Candidate.PackagePassCount, receipt.Candidate.PassedTestCount)
	return nil
}

// runSuite runs one side's tests and reduces the log.
//
// Stderr joins stdout in the same log ON PURPOSE. A build error or a panic
// outside a test arrives as text rather than as a test event, and keeping it in
// the log is what lets Summarise report it as an unparsed line instead of
// letting the run read as a clean pass over the packages that did build.
func runSuite(work, label, root string) (catalogparity.UnitSuiteReceipt, error) {
	command := exec.Command("go", "test", "-json", "-count=1", "./...")
	command.Dir = root
	command.Env = append(environmentWithout(os.Environ(), "TF_ACC"),
		"GOPROXY=off", "GOSUMDB=off", "GOVCS=*:off", "GIT_TERMINAL_PROMPT=0",
		"GOTOOLCHAIN=local", "GOCACHE="+filepath.Join(work, label+"-go-cache"))
	raw, runErr := command.CombinedOutput()

	exitCode := 0
	var exit *exec.ExitError
	if errors.As(runErr, &exit) {
		exitCode = exit.ExitCode()
	} else if runErr != nil {
		return catalogparity.UnitSuiteReceipt{}, fmt.Errorf("%s suite: %w", label, runErr)
	}

	logPath := filepath.Join(work, label+".jsonl")
	if err := os.WriteFile(logPath, raw, 0o600); err != nil {
		return catalogparity.UnitSuiteReceipt{}, err
	}
	summary := unitdifferential.Summarise(raw, exitCode)
	events, err := unitdifferential.RenderEvents(summary.Events)
	if err != nil {
		return catalogparity.UnitSuiteReceipt{}, err
	}
	receipt := summary.Receipt
	receipt.RawLogSHA256 = unitdifferential.Digest(raw)
	receipt.NormalizedSummarySHA256 = unitdifferential.Digest(events)
	return receipt, nil
}

// environmentWithout drops a variable rather than setting it empty. TF_ACC is
// read for PRESENCE by the acceptance harness, so TF_ACC= would still turn the
// acceptance suite on -- and this gate promises a run with no controller.
func environmentWithout(environment []string, name string) []string {
	kept := make([]string, 0, len(environment))
	for _, entry := range environment {
		if strings.HasPrefix(entry, name+"=") {
			continue
		}
		kept = append(kept, entry)
	}
	return kept
}

func goEnv(name string) (string, error) {
	out, err := exec.Command("go", "env", name).Output()
	if err != nil {
		return "", fmt.Errorf("go env %s: %w", name, err)
	}
	value := strings.TrimSpace(string(out))
	if value == "" {
		return "", fmt.Errorf("go env %s is empty", name)
	}
	return value, nil
}

func goEnvPair() (string, error) {
	goos, err := goEnv("GOOS")
	if err != nil {
		return "", err
	}
	goarch, err := goEnv("GOARCH")
	if err != nil {
		return "", err
	}
	return goos + "/" + goarch, nil
}

func fileDigest(path string) (string, error) {
	raw, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return "", err
	}
	return unitdifferential.Digest(raw), nil
}
