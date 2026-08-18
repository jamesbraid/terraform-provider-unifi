// Command catalog-controller-differential runs the acceptance catalog against a
// live controller, once with the released provider and once with the candidate,
// and records what each did.
//
// It replaces .woodpecker/scripts/catalog-controller-differential.sh. Every
// judgement -- which tests the plan selects, which scenario files the released
// tree may borrow, what a suite's outcome means, what the run's result is --
// lives in internal/controllerdifferential and runs under go test without a
// controller. What is here is orchestration: read two files, unpack a tag, look
// at the machine, run two suites, write a receipt.
//
// THE WORK IS SPLIT WHERE THE REQUIREMENTS SPLIT. Everything up to and
// including -prepare-only needs git and a Go toolchain; everything after needs
// Linux, x86_64, docker and two binaries. Keeping them together meant the
// released tree could not be built anywhere the controllers cannot run, so its
// three compile checks were unreachable from any test and were verified by
// hand.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/cmdio"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/ubiquiti-community/terraform-provider-unifi/internal/catalogparity"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/controllerdifferential"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/releasedtree"
)

type options struct {
	repository   string
	inventory    string
	policy       string
	waves        string
	testNames    string
	output       string
	treeStateRaw string
	releasedRef  string
	planOnly     bool
	prepareOnly  bool

	controllerImage string
	syntheticImage  string
	ryukImage       string
	herderBin       string
	terraformBin    string

	printDiagnostics bool
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "catalog controller differential: %v\n", err)
		os.Exit(1)
	}
}

// run takes its arguments rather than reading the package-level flag set, so a
// test can exercise the refusals without os.Args and without a second test in
// the same binary redefining a flag.
func run(args []string) error {
	var o options
	flags := flag.NewFlagSet("catalog-controller-differential", flag.ContinueOnError)
	flags.StringVar(&o.repository, "repo", ".", "repository holding the candidate tree")
	flags.StringVar(&o.inventory, "inventory", "build/release-ready/catalog-evidence-inventory.json",
		"evidence inventory, relative to -repo")
	flags.StringVar(&o.policy, "campaign-policy", "provider-codegen/policy/catalog-campaign.json",
		"campaign policy, relative to -repo")
	flags.StringVar(&o.waves, "waves", "1,2,3,4", "waves to select")
	flags.StringVar(&o.testNames, "test-names", "",
		"diagnostic: run only these tests, comma or semicolon separated. Every name must already "+
			"be in the plan")
	flags.StringVar(&o.output, "output", "", "write the receipt here; required")
	flags.StringVar(&o.treeStateRaw, "tree-state", "",
		"JSON from the tree-state measurement; required, no default")
	flags.StringVar(&o.releasedRef, "released-ref", "v0.101.2", "the released side's tag")
	flags.BoolVar(&o.planOnly, "plan-only", false, "write the plan as the receipt and stop")
	flags.BoolVar(&o.prepareOnly, "prepare-only", false,
		"prepare and vet the released tree and stop; needs no controller")
	flags.StringVar(&o.controllerImage, "controller-image", "", "controller image, already pulled")
	flags.StringVar(&o.syntheticImage, "synthetic-image", "", "synthetic fleet image, already pulled")
	flags.StringVar(&o.ryukImage, "ryuk-image", "", "testcontainers ryuk image, already pulled")
	flags.StringVar(&o.herderBin, "herder", "", "the fleet herder binary")
	flags.StringVar(&o.terraformBin, "terraform", "", "the CLI the acceptance harness drives")
	flags.BoolVar(&o.printDiagnostics, "print-failure-diagnostics", false,
		"print the sanitised log lines for each failed test")
	// -followup classifies a receipt this command already wrote, and prints one
	// word. It replaces catalog-controller-followup.sh, whose single caller ran
	// it TWICE -- once grepping for diagnostic_complete and again for full --
	// because a script that prints a word was the only way a workflow could ask
	// it a question. One invocation answers both: the word on stdout, and a
	// non-zero exit with every failed condition named on stderr.
	followup := flags.String("followup", "",
		"classify an existing receipt as full or diagnostic_complete and exit")
	if err := flags.Parse(args); err != nil {
		return err
	}

	if *followup != "" {
		return classifyReceipt(o, *followup)
	}
	if o.output == "" {
		return errors.New("-output is required")
	}
	// Parsed before anything is read or built. This receipt attests a
	// differential against a live controller; generated from a dirty tree it
	// attests a comparison of code that is in no commit.
	treeState, err := catalogparity.ParseTreeState(o.treeStateRaw)
	if err != nil {
		return err
	}
	repo, err := filepath.Abs(o.repository)
	if err != nil {
		return err
	}

	plan, err := buildPlan(o, repo)
	if err != nil {
		return err
	}
	if o.planOnly {
		return writeJSON(o.output, plan)
	}

	work, err := os.MkdirTemp("", "catalog-controller-differential-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(work)

	prepared, err := controllerdifferential.PrepareReleasedTree(repo, repo, o.releasedRef,
		filepath.Join(work, "released"), plan.SharedScenarioOwners)
	if err != nil {
		return err
	}
	if o.prepareOnly {
		fmt.Fprintf(os.Stderr, "prepared the released tree and vetted %d layer(s), grafting %d scenario owner(s)\n",
			len(prepared.Layers), len(prepared.GraftedOwners))
		return nil
	}

	environment, problems, err := measureMachine(o)
	if err != nil {
		return err
	}
	if len(problems) > 0 {
		for _, problem := range problems {
			fmt.Fprintf(os.Stderr, "  %s\n", problem)
		}
		return fmt.Errorf("%d unmet requirement(s); this gate starts controllers and does not "+
			"provision the machine it runs on", len(problems))
	}

	suites := map[string]catalogparity.ControllerSuiteReceipt{}
	for _, side := range []struct{ label, root string }{
		{"candidate", repo},
		{"released", prepared.Root},
	} {
		suite, err := runSuite(o, work, side.label, side.root, plan)
		if err != nil {
			return err
		}
		suites[side.label] = suite
	}

	releasedCommit, err := releasedtree.ResolveCommit(repo, o.releasedRef)
	if err != nil {
		return err
	}
	candidateCommit, err := cmdio.GitOutput(repo, "rev-parse", "HEAD")
	if err != nil {
		return err
	}
	planDigest, err := digestOf(plan)
	if err != nil {
		return err
	}

	receipt := controllerdifferential.BuildReceipt(controllerdifferential.Run{
		Plan:            plan,
		PlanSHA256:      planDigest,
		Released:        suites["released"],
		Candidate:       suites["candidate"],
		ReleasedCommit:  releasedCommit,
		CandidateCommit: candidateCommit,
		Environment:     environment,
		TreeState:       treeState,
	})
	if err := writeJSON(o.output, receipt); err != nil {
		return err
	}

	// The receipt is written before the verdict is reported. An hour of
	// controllers that ends with no artifact is an hour nobody can read.
	if receipt.Result == "fail" {
		return fmt.Errorf("the differential failed: released %q, candidate %q; the receipt is at %s",
			receipt.Released.Result, receipt.Candidate.Result, o.output)
	}
	fmt.Fprintf(os.Stderr, "catalog controller differential: %s. released %q, candidate %q, "+
		"%d evidence gap(s) across %d surface(s)\n",
		receipt.Result, receipt.Released.Result, receipt.Candidate.Result,
		plan.EvidenceGapCount, plan.SurfaceCount)
	return nil
}

func buildPlan(o options, repo string) (catalogparity.ControllerPlanReceipt, error) {
	var plan catalogparity.ControllerPlanReceipt
	var inventory controllerdifferential.Inventory
	if err := readJSON(filepath.Join(repo, o.inventory), &inventory); err != nil {
		return plan, err
	}
	var policy controllerdifferential.CampaignPolicy
	if err := readJSON(filepath.Join(repo, o.policy), &policy); err != nil {
		return plan, err
	}

	waves, err := parseWaves(o.waves)
	if err != nil {
		return plan, err
	}
	plan, err = controllerdifferential.BuildPlan(inventory, policy, waves)
	if err != nil {
		return plan, err
	}
	if o.testNames == "" {
		return plan, nil
	}
	return controllerdifferential.Narrow(plan, splitNames(o.testNames))
}

// parseWaves refuses a list it cannot read rather than selecting nothing. An
// unparsable wave silently dropped is a run that covers less than it says.
func parseWaves(raw string) ([]int, error) {
	var waves []int
	for _, field := range strings.Split(raw, ",") {
		field = strings.TrimSpace(field)
		if field == "" {
			continue
		}
		wave, err := strconv.Atoi(field)
		if err != nil {
			return nil, fmt.Errorf("wave %q is not a number", field)
		}
		waves = append(waves, wave)
	}
	if len(waves) == 0 {
		return nil, errors.New("no waves selected, so the plan would run nothing")
	}
	return waves, nil
}

// splitNames accepts both separators because the shell accepted both: a
// semicolon survives a YAML scalar that a comma-separated list does not.
func splitNames(raw string) []string {
	var names []string
	for _, field := range strings.FieldsFunc(raw, func(r rune) bool { return r == ',' || r == ';' }) {
		if trimmed := strings.TrimSpace(field); trimmed != "" {
			names = append(names, trimmed)
		}
	}
	return names
}

// measureMachine looks at the machine and reports what it found, so
// CheckPreconditions can judge it. Only this side can look; only the other side
// can be tested.
func measureMachine(o options) (controllerdifferential.Environment, []string, error) {
	var environment controllerdifferential.Environment

	osName, err := uname("-s")
	if err != nil {
		return environment, nil, err
	}
	architecture, err := uname("-m")
	if err != nil {
		return environment, nil, err
	}

	requirements := controllerdifferential.Requirements{OS: osName, Architecture: architecture}
	for _, executable := range []struct{ label, variable, value string }{
		{"herder", "CATALOG_HERDER_BIN", o.herderBin},
		{"terraform CLI", "TERRAFORM_BIN", o.terraformBin},
	} {
		requirements.Executables = append(requirements.Executables, controllerdifferential.NamedPath{
			Label: executable.label, Variable: executable.variable, Value: executable.value,
			Present: isExecutable(executable.value),
		})
	}
	for _, image := range []struct{ label, variable, value string }{
		{"controller", "CATALOG_CONTROLLER_IMAGE", o.controllerImage},
		{"synthetic", "CATALOG_SYNTHETIC_IMAGE", o.syntheticImage},
		{"ryuk", "CATALOG_RYUK_IMAGE", o.ryukImage},
	} {
		requirements.Images = append(requirements.Images, controllerdifferential.NamedPath{
			Label: image.label, Variable: image.variable, Value: image.value,
			Present: imageID(image.value) != "",
		})
	}

	problems := controllerdifferential.CheckPreconditions(requirements)
	if len(problems) > 0 {
		return environment, problems, nil
	}

	environment = controllerdifferential.Environment{
		ControllerImage:   o.controllerImage,
		ControllerImageID: imageID(o.controllerImage),
		SyntheticImage:    o.syntheticImage,
		SyntheticImageID:  imageID(o.syntheticImage),
		RyukImage:         o.ryukImage,
		RyukImageID:       imageID(o.ryukImage),
	}
	if environment.HerderSHA256, err = fileDigest(o.herderBin); err != nil {
		return environment, nil, err
	}
	if environment.TerraformBinarySHA256, err = fileDigest(o.terraformBin); err != nil {
		return environment, nil, err
	}
	return environment, nil, nil
}

func runSuite(o options, work, label, root string, plan catalogparity.ControllerPlanReceipt) (catalogparity.ControllerSuiteReceipt, error) {
	command := exec.Command("go", "test", "-json", "-count=1", "-timeout", "90m", "./unifi",
		"-run", controllerdifferential.TestRegex(plan))
	command.Dir = root
	command.Env = append(os.Environ(),
		"TF_ACC=1",
		"TF_ACC_TERRAFORM_PATH="+o.terraformBin,
		"UNIFI_TEST_HERDER_BIN="+o.herderBin,
		"UNIFI_TEST_HERDER_SYNTHETIC_IMAGE="+o.syntheticImage,
		"UNIFI_TEST_CONTROLLER_IMAGE="+o.controllerImage,
		// never, and the receipt records it: this gate refuses an image that is
		// not already present, so a run can never be measuring whatever the
		// registry holds now.
		"UNIFI_TEST_CONTROLLER_PULL_POLICY=never",
		"TESTCONTAINERS_RYUK_CONTAINER_IMAGE="+o.ryukImage,
		"GOPROXY=off", "GOSUMDB=off", "GOVCS=*:off", "GIT_TERMINAL_PROMPT=0",
		"GOTOOLCHAIN=local", "GOCACHE="+filepath.Join(work, label+"-go-cache"))

	// Stderr joins stdout on purpose: a controller that never came up says so
	// there, and Summarise keeps those lines when a suite produced no test
	// outcome at all.
	raw, runErr := command.CombinedOutput()
	exitCode := 0
	var exit *exec.ExitError
	if errors.As(runErr, &exit) {
		exitCode = exit.ExitCode()
	} else if runErr != nil {
		return catalogparity.ControllerSuiteReceipt{}, fmt.Errorf("%s suite: %w", label, runErr)
	}
	if err := os.WriteFile(filepath.Join(work, label+".jsonl"), raw, 0o600); err != nil {
		return catalogparity.ControllerSuiteReceipt{}, err
	}

	suite := controllerdifferential.SummariseSuite(raw, plan, label, exitCode)
	if o.printDiagnostics {
		for _, failed := range suite.Failed {
			fmt.Printf("sanitized controller diagnostic: %s/%s\n", label, failed)
			// SANITISED, and the word in that heading was a lie until now. The
			// port printed the name and none of the output, so nothing redacted
			// anything -- see SanitiseDiagnostics. A controller log carries the
			// controller's URL, the runner's workspace path, the fleet's MAC
			// addresses and the container network's addresses, and a CI log is
			// the least private place a failure goes.
			for _, line := range controllerdifferential.SanitiseDiagnostics(raw, failed) {
				fmt.Print(line)
			}
		}
	}
	return suite, nil
}

func uname(flag string) (string, error) {
	out, err := exec.Command("uname", flag).Output()
	if err != nil {
		return "", fmt.Errorf("uname %s: %w", flag, err)
	}
	return strings.TrimSpace(string(out)), nil
}

func isExecutable(path string) bool {
	if path == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && !info.IsDir() && info.Mode()&0o111 != 0
}

// imageID returns the image's ID, or "" when it is not present locally. This
// gate does not pull.
func imageID(reference string) string {
	if reference == "" {
		return ""
	}
	out, err := exec.Command("docker", "image", "inspect", "--format", "{{.Id}}", reference).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func fileDigest(path string) (string, error) {
	raw, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

func digestOf(value any) (string, error) {
	encoded, err := catalogparity.MarshalReceipt(value)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), nil
}

func readJSON(path string, into any) error {
	raw, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return err
	}
	if err := json.Unmarshal(raw, into); err != nil {
		return fmt.Errorf("parse %s: %w", path, err)
	}
	return nil
}

func writeJSON(path string, value any) error {
	encoded, err := catalogparity.MarshalReceipt(value)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	return os.WriteFile(path, encoded, 0o600)
}

// classifyReceipt answers what a completed run was, and says why not when it
// cannot.
//
// The shell printed nothing on refusal -- one jq -e of fourteen conjuncts with
// its output discarded -- so an operator saw a failed step and re-derived which
// conjunct was false. Followup reports every failed condition, each naming its
// own subject.
func classifyReceipt(o options, path string) error {
	repo, err := filepath.Abs(o.repository)
	if err != nil {
		return err
	}
	var receipt catalogparity.ControllerDifferentialReceipt
	if err := readJSON(path, &receipt); err != nil {
		return err
	}
	var policy controllerdifferential.CampaignPolicy
	if err := readJSON(filepath.Join(repo, o.policy), &policy); err != nil {
		return err
	}
	var counts controllerdifferential.CampaignCounts
	if err := readJSON(filepath.Join(repo, o.policy), &counts); err != nil {
		return err
	}

	classification, err := controllerdifferential.Followup(receipt, policy, counts)
	if err != nil {
		return err
	}
	fmt.Println(classification)
	return nil
}
