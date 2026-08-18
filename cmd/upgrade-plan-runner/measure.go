package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/ubiquiti-community/terraform-provider-unifi/internal/catalogparity"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/cmdio"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/releasedtree"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/upgradeplan"
)

// measure runs the three plans against the controller this process started.
//
// It replaces the shell-out to catalog-upgrade-plan.sh. The judgement -- what
// the three exit codes mean against the fixture's declared polarity -- is
// internal/upgradeplan and runs under go test without a controller. What is
// here is the sequence: build two providers, point two CLI configs at them,
// apply with the old one, plan with each, destroy.
//
// THE CONTROLLER MUST ALREADY BE RUNNING and this function does not start one.
// That separation is why this file exists rather than the script: a step that
// provisions and measures has two reasons to fail and one exit code to report
// them with, and the failure everyone reads is the measurement.
func measure(ctx context.Context, logger *log.Logger, root string, settings settings) (int, error) {
	fixture, err := upgradeplan.LoadFixture(settings.fixtureDirectory)
	if err != nil {
		return 1, err
	}
	if missing := unsetControllerVariables(); len(missing) > 0 {
		return 1, fmt.Errorf("the controller environment is incomplete: %s unset. This measures a "+
			"running controller and does not start one", strings.Join(missing, ", "))
	}
	if !isExecutable(settings.cli) {
		return 1, fmt.Errorf("TERRAFORM_BIN=%s is not executable", settings.cli)
	}

	// WHAT THE BINARY SAYS IT IS, not the variable that held it. The variable
	// is called TERRAFORM_BIN and on these machines it holds OpenTofu; a
	// receipt reporting "terraform" because of a variable's name is a receipt
	// that lies about the tool that produced it.
	identity, err := cliIdentity(settings.cli)
	if err != nil {
		return 1, err
	}

	releasedCommit, err := releasedtree.ResolveCommit(root, settings.releasedRef)
	if err != nil {
		return 1, err
	}

	work, err := os.MkdirTemp("", "catalog-upgrade-")
	if err != nil {
		return 1, err
	}
	defer os.RemoveAll(work)

	oldPlugins := filepath.Join(work, "plugins", "old")
	newPlugins := filepath.Join(work, "plugins", "new")
	provenance, err := stageReleasedProvider(ctx, root, work, oldPlugins, settings, releasedCommit)
	if err != nil {
		return 1, err
	}
	if err := buildProvider(ctx, root, newPlugins, "candidate"); err != nil {
		return 1, err
	}

	oldSHA256, err := cmdio.FileDigest(filepath.Join(oldPlugins, upgradeplan.ProviderBinaryName))
	if err != nil {
		return 1, err
	}
	newSHA256, err := cmdio.FileDigest(filepath.Join(newPlugins, upgradeplan.ProviderBinaryName))
	if err != nil {
		return 1, err
	}
	if err := upgradeplan.TwoProvidersAreDistinct(oldSHA256, newSHA256); err != nil {
		return 1, err
	}

	oldConfig := filepath.Join(work, "old.tfrc")
	newConfig := filepath.Join(work, "new.tfrc")
	if err := upgradeplan.WriteCLIConfig(oldPlugins, oldConfig); err != nil {
		return 1, err
	}
	if err := upgradeplan.WriteCLIConfig(newPlugins, newConfig); err != nil {
		return 1, err
	}

	configDirectory := filepath.Join(work, "config")
	if _, err := upgradeplan.StageFixture(fixture, configDirectory); err != nil {
		return 1, err
	}

	applyLog, applyCode := runCLI(ctx, settings.cli, oldConfig, configDirectory,
		"apply", "-auto-approve", "-input=false")
	if applyCode != 0 {
		indent(logger, applyLog)
		return 1, fmt.Errorf("the released provider could not apply the fixture (exit %d); no "+
			"state was written, so neither plan below would mean anything", applyCode)
	}
	if err := upgradeplan.StateWasWritten(configDirectory); err != nil {
		return 1, err
	}

	// EXIT CODES ARE READ FROM THE PROCESS, NEVER THROUGH A PIPE. With
	// -detailed-exitcode the exit status IS the result -- 0 no changes, 1
	// error, 2 changes -- so piping to tee or head reports tee's status and a
	// harness reads 0 from a plan that never ran.
	controlLog, controlCode := runCLI(ctx, settings.cli, oldConfig, configDirectory,
		"plan", "-detailed-exitcode", "-input=false")
	subjectLog, subjectCode := runCLI(ctx, settings.cli, newConfig, configDirectory,
		"plan", "-detailed-exitcode", "-input=false")

	// Best effort, and reported rather than hidden: a fixture left behind
	// poisons the next run, and silence about it is how that becomes somebody
	// else's mystery.
	_, destroyCode := runCLI(ctx, settings.cli, oldConfig, configDirectory,
		"destroy", "-auto-approve", "-input=false")

	candidateCommit, err := cmdio.GitOutput(root, "rev-parse", "HEAD")
	if err != nil {
		return 1, err
	}
	receipt := upgradeplan.BuildReceipt(upgradeplan.Run{
		CLI:                     identity,
		ReleasedRef:             settings.releasedRef,
		ReleasedCommit:          releasedCommit,
		ReleasedProvenance:      provenance,
		CandidateCommit:         candidateCommit,
		ReleasedProviderSHA256:  oldSHA256,
		CandidateProviderSHA256: newSHA256,
		Fixture:                 fixture,
		ControlExit:             controlCode,
		SubjectExit:             subjectCode,
		DestroyExit:             destroyCode,
		TreeState:               settings.treeState,
	})

	encoded, err := catalogparity.MarshalReceipt(receipt)
	if err != nil {
		return 1, err
	}
	if err := os.MkdirAll(filepath.Dir(settings.output), 0o750); err != nil {
		return 1, err
	}
	if err := os.WriteFile(settings.output, encoded, 0o600); err != nil {
		return 1, err
	}

	logger.Printf("%s", receipt.Verdict)
	logger.Printf("baseline=%s (%s) provenance=%s", settings.releasedRef, releasedCommit, provenance)
	logger.Printf("cli=%s control=%d (expected %d) subject=%d receipt=%s",
		identity, controlCode, fixture.Expected, subjectCode, settings.output)
	if destroyCode != 0 {
		logger.Printf("the fixture was NOT destroyed (exit %d); the controller holds leftover objects",
			destroyCode)
	}

	// The two reds print DIFFERENT logs, because they send the reader to
	// different places: a void result is about the control, a failure is about
	// the subject.
	verdict := upgradeplan.Decide(settings.releasedRef, fixture.Expected, controlCode, subjectCode)
	switch verdict.Result {
	case "fail":
		indent(logger, subjectLog)
	case "void":
		indent(logger, controlLog)
	}
	return verdict.ExitCode, nil
}

// stageReleasedProvider prefers the PUBLISHED BINARY over a rebuild of its
// source.
//
// An operator upgrades from the artifact that was released, not from a fresh
// compile of the tag it was cut at, and those are not guaranteed to be the same
// program: different toolchain, different module resolution. Building from the
// archived tree stays as the fallback, because it is the only thing that works
// on a machine with no release artifact to hand, and a harness that can only
// run in CI is one nobody develops against.
func stageReleasedProvider(ctx context.Context, root, work, plugins string, settings settings, releasedCommit string) (string, error) {
	if err := os.MkdirAll(plugins, 0o750); err != nil {
		return "", err
	}
	destination := filepath.Join(plugins, upgradeplan.ProviderBinaryName)

	if settings.releasedBinary != "" {
		if !isExecutable(settings.releasedBinary) {
			return "", fmt.Errorf("the released binary %s is not executable", settings.releasedBinary)
		}
		raw, err := os.ReadFile(filepath.Clean(settings.releasedBinary))
		if err != nil {
			return "", err
		}
		// Copied under the required name rather than linked under its published
		// one: release artifacts carry a version suffix, and dev_overrides
		// requires terraform-provider-<TYPE> inside the directory.
		if err := os.WriteFile(destination, raw, 0o700); err != nil {
			return "", err
		}
		return "published-binary", nil
	}

	// The released TREE is only extracted when it is going to be built. With a
	// published binary in hand the archive is work nobody consumes, and a step
	// that can fail without affecting the result is a step that can fail the
	// run for no reason.
	source := filepath.Join(work, "released")
	if err := os.MkdirAll(source, 0o750); err != nil {
		return "", err
	}
	if err := releasedtree.Extract(root, releasedCommit, source); err != nil {
		return "", err
	}
	if err := buildProvider(ctx, source, plugins, "released ("+settings.releasedRef+")"); err != nil {
		return "", err
	}
	return "source-build", nil
}

func buildProvider(ctx context.Context, source, plugins, label string) error {
	if err := os.MkdirAll(plugins, 0o750); err != nil {
		return err
	}
	destination := filepath.Join(plugins, upgradeplan.ProviderBinaryName)
	command := exec.CommandContext(ctx, "go", "build", "-o", destination, ".")
	command.Dir = source
	if out, err := command.CombinedOutput(); err != nil {
		return fmt.Errorf("the %s provider did not build; nothing downstream of this can be "+
			"measured: %w\n%s", label, err, out)
	}
	if !isExecutable(destination) {
		return fmt.Errorf("the %s provider built without producing a binary at %s", label, destination)
	}
	return nil
}

// runCLI returns the combined log and the process's own exit code.
func runCLI(ctx context.Context, cli, cliConfig, configDirectory string, args ...string) (string, int) {
	command := exec.CommandContext(ctx, cli, append([]string{"-chdir=" + configDirectory}, args...)...)
	command.Env = append(os.Environ(),
		"TF_CLI_CONFIG_FILE="+cliConfig,
		"TF_IN_AUTOMATION=1")
	out, err := command.CombinedOutput()
	if err == nil {
		return string(out), 0
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return string(out), exit.ExitCode()
	}
	return string(out) + "\n" + err.Error(), 1
}

// unsetControllerVariables names EVERY missing variable rather than the first.
// An operator fixing them one per run is the same waste as a gate that reports
// one disagreeing count.
func unsetControllerVariables() []string {
	var missing []string
	for _, name := range []string{"UNIFI_API", "UNIFI_USERNAME", "UNIFI_PASSWORD"} {
		if os.Getenv(name) == "" {
			missing = append(missing, name)
		}
	}
	return missing
}

func cliIdentity(cli string) (string, error) {
	out, err := exec.Command(cli, "version").Output()
	if err != nil {
		return "", fmt.Errorf("%s version: %w", cli, err)
	}
	first := strings.SplitN(strings.TrimSpace(string(out)), "\n", 2)[0]
	if first == "" {
		return "", fmt.Errorf("%s reported no version", cli)
	}
	return first, nil
}

func isExecutable(path string) bool {
	if path == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && !info.IsDir() && info.Mode()&0o111 != 0
}

func indent(logger *log.Logger, text string) {
	for _, line := range strings.Split(strings.TrimRight(text, "\n"), "\n") {
		logger.Printf("    %s", line)
	}
}
