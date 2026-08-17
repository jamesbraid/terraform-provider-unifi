package main

import (
	"archive/tar"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// discardScopedResources removes everything this run names after its pipeline
// number. It is called before anything is created as well as on the way out;
// see the comment at its first call site for why the "before" matters more.
// Failures are ignored by design -- the resources may simply not exist.
func (q *qualification) discardScopedResources() {
	_ = q.dockerQuiet("rm", "--force", q.controller)
	_ = q.dockerQuiet("network", "rm", q.network)
	for _, volume := range q.volumes {
		_ = q.dockerQuiet("volume", "rm", volume)
	}
}

// dockerRun streams the command's output. Nothing here is quiet: the shell sent
// build and apply output straight to the log, and a qualification run that
// fails is diagnosed from that log alone.
func (q *qualification) dockerRun(args ...string) error {
	command := exec.Command("docker", args...)
	command.Stdout = q.stdout
	command.Stderr = q.stderr
	if err := command.Run(); err != nil {
		return fmt.Errorf("docker %s: %w", strings.Join(args, " "), err)
	}
	return nil
}

// dockerQuiet discards output and returns only whether the command succeeded.
// Used for existence probes, where the failure IS the answer.
func (q *qualification) dockerQuiet(args ...string) error {
	return exec.Command("docker", args...).Run()
}

func (q *qualification) dockerOutput(args ...string) (string, error) {
	var stdout, stderr bytes.Buffer
	command := exec.Command("docker", args...)
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		return "", fmt.Errorf("docker %s: %w: %s", strings.Join(args, " "), err, stderr.String())
	}
	return strings.TrimSpace(stdout.String()), nil
}

// dockerCode runs a command whose non-zero exit is meaningful rather than
// fatal, and returns the exit code. An error that is not an ExitError -- docker
// missing, for instance -- is still an error, because treating "could not run"
// as "ran and reported N" is how a broken harness starts looking like a result.
func (q *qualification) dockerCode(args ...string) (int, error) {
	command := exec.Command("docker", args...)
	command.Stdout = q.stdout
	command.Stderr = q.stderr
	err := command.Run()
	if err == nil {
		return 0, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode(), nil
	}
	return 0, fmt.Errorf("docker %s: %w", strings.Join(args, " "), err)
}

func (q *qualification) digestInVolume(path string) (string, error) {
	output, err := q.dockerOutput("run", "--rm", "--entrypoint", "sha256sum",
		"--mount", "type=volume,src="+q.volumes["tools"]+",dst=/tools,readonly",
		goImage, path)
	if err != nil {
		return "", err
	}
	fields := strings.Fields(output)
	if len(fields) == 0 {
		return "", fmt.Errorf("sha256sum produced no output for %s", path)
	}
	return fields[0], nil
}

// populateVolume streams a directory into a named volume as a tar. The tar is
// built here rather than shelled out to, so the result does not depend on which
// tar implementation the host happens to ship.
func (q *qualification) populateVolume(directory, volume string) error {
	var archive bytes.Buffer
	writer := tar.NewWriter(&archive)
	err := filepath.Walk(directory, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(directory, path)
		if err != nil {
			return err
		}
		if relative == "." {
			return nil
		}
		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		header.Name = filepath.ToSlash(relative)
		if err := writer.WriteHeader(header); err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		contents, err := os.Open(path)
		if err != nil {
			return err
		}
		defer contents.Close()
		_, err = io.Copy(writer, contents)
		return err
	})
	if err != nil {
		return err
	}
	if err := writer.Close(); err != nil {
		return err
	}

	command := exec.Command("docker", "run", "--rm", "--interactive",
		"--entrypoint", "/bin/sh",
		"--mount", "type=volume,src="+volume+",dst=/target",
		goImage, "-c", "tar -xf - -C /target")
	command.Stdin = &archive
	command.Stdout = q.stdout
	command.Stderr = q.stderr
	if err := command.Run(); err != nil {
		return fmt.Errorf("populating volume %s: %w", volume, err)
	}
	return nil
}

// loadCommitIntoVolume pipes `git archive <commit>` into a volume. Using git
// archive rather than the working tree is what makes the build describe the
// named commit instead of whatever is checked out.
func (q *qualification) loadCommitIntoVolume(commit, volume string) error {
	archive := exec.Command("git", "archive", commit)
	loader := exec.Command("docker", "run", "--rm", "--interactive",
		"--entrypoint", "/bin/sh",
		"--mount", "type=volume,src="+volume+",dst=/source",
		goImage, "-c", "tar -xf - -C /source")

	pipe, err := archive.StdoutPipe()
	if err != nil {
		return err
	}
	archive.Stderr = q.stderr
	loader.Stdin = pipe
	loader.Stdout = q.stdout
	loader.Stderr = q.stderr

	if err := loader.Start(); err != nil {
		return err
	}
	if err := archive.Run(); err != nil {
		_ = loader.Wait()
		return fmt.Errorf("git archive %s: %w", commit, err)
	}
	if err := loader.Wait(); err != nil {
		return fmt.Errorf("loading %s into %s: %w", commit, volume, err)
	}
	return nil
}

func (q *qualification) startController() error {
	if err := q.dockerRun("run", "--detach", "--name", q.controller,
		"--network", q.network, "--network-alias", "controller",
		"--init", "--platform", platform, controllerImage); err != nil {
		return err
	}
	return q.waitHealthy()
}

func (q *qualification) stopController() error {
	if err := q.dockerRun("rm", "--force", q.controller); err != nil {
		return err
	}
	// A container that still inspects after a forced removal means the next
	// pass would reuse this one rather than get the fresh target the receipt
	// claims -- so this is checked rather than assumed.
	if q.dockerQuiet("inspect", q.controller) == nil {
		return fmt.Errorf("controller %s still resolves after removal", q.controller)
	}
	return nil
}

func (q *qualification) waitHealthy() error {
	for attempt := 0; attempt < healthAttempts; attempt++ {
		status, err := q.dockerOutput("inspect", "--format", "{{.State.Health.Status}}", q.controller)
		if err == nil && status == "healthy" {
			return nil
		}
		running, runningErr := q.dockerOutput("inspect", "--format", "{{.State.Running}}", q.controller)
		if runningErr == nil && running != "true" {
			q.dumpController()
			return fmt.Errorf("controller %s stopped before becoming healthy (last status %q)",
				q.controller, status)
		}
		time.Sleep(healthInterval)
	}
	q.dumpController()
	return fmt.Errorf("controller %s never became healthy within %s",
		q.controller, time.Duration(healthAttempts)*healthInterval)
}

// dumpController writes the container's state and logs to stderr. A health
// timeout with no logs is the shape of failure that costs an hour to diagnose,
// so the diagnosis is emitted at the moment it is cheapest to collect.
func (q *qualification) dumpController() {
	if state, err := q.dockerOutput("inspect", "--format", "{{json .State}}", q.controller); err == nil {
		fmt.Fprintf(q.stderr, "controller state: %s\n", state)
	}
	logs := exec.Command("docker", "logs", q.controller)
	logs.Stdout = q.stderr
	logs.Stderr = q.stderr
	_ = logs.Run()
}

func (q *qualification) cliArgs(cli, stateVolume, fixture, cliConfig string, args ...string) []string {
	stateVolumeName, ok := q.volumes[stateVolume]
	if !ok {
		panic("unknown state volume " + stateVolume)
	}
	return append([]string{
		"run", "--rm", "--platform", platform, "--network", q.network,
		"--tmpfs", "/tmp:rw,exec,nosuid,size=1g",
		"--env", "CHECKPOINT_DISABLE=1", "--env", "TF_IN_AUTOMATION=1",
		"--env", "TF_CLI_CONFIG_FILE=/tools/" + cliConfig,
		"--env", "TF_DATA_DIR=/tmp/tfdata",
		"--env", "UNIFI_USERNAME=admin", "--env", "UNIFI_PASSWORD=admin",
		"--env", "UNIFI_INSECURE=true", "--env", "UNIFI_API=https://controller:8443",
		"--mount", "type=volume,src=" + q.volumes["tools"] + ",dst=/tools,readonly",
		"--mount", "type=volume,src=" + q.volumes["config"] + ",dst=/config,readonly",
		"--mount", "type=volume,src=" + stateVolumeName + ",dst=/state",
		"--workdir", "/config/" + fixture,
		goImage, "/tools/" + cli,
	}, args...)
}

func (q *qualification) runCLI(cli, stateVolume, fixture, cliConfig string, args ...string) error {
	return q.dockerRun(q.cliArgs(cli, stateVolume, fixture, cliConfig, args...)...)
}

// captureCLI returns the command's stdout. Used for `show -json` and for
// `output -raw`, where the output IS the result rather than a log.
func (q *qualification) captureCLI(cli, stateVolume, fixture, cliConfig string, args ...string) ([]byte, error) {
	full := q.cliArgs(cli, stateVolume, fixture, cliConfig, args...)
	var stdout bytes.Buffer
	command := exec.Command("docker", full...)
	command.Stdout = &stdout
	command.Stderr = q.stderr
	if err := command.Run(); err != nil {
		return nil, fmt.Errorf("%s %s: %w", cli, strings.Join(args, " "), err)
	}
	return stdout.Bytes(), nil
}

// expectChanges requires plan -detailed-exitcode to report 2. Exit 0 means the
// fixture change produced no plan at all, which is a silently passing gate, so
// it is named as a failure rather than tolerated.
func (q *qualification) expectChanges(cli, stateVolume, fixture, plan, cliConfig string) error {
	code, err := q.dockerCode(q.cliArgs(cli, stateVolume, fixture, cliConfig,
		"plan", "-input=false", "-detailed-exitcode",
		"-state=/state/state.tfstate", "-out=/state/"+plan,
		"-var", "dns_name="+dnsName)...)
	if err != nil {
		return err
	}
	switch code {
	case 2:
		return nil
	case 0:
		return fmt.Errorf("plan against fixture %q reported no changes; this step exists to prove "+
			"the change is planned, so an empty plan is a failure", fixture)
	default:
		return fmt.Errorf("plan against fixture %q exited %d, want 2", fixture, code)
	}
}

// expectNoChanges requires plan -detailed-exitcode to report 0. Under the shell
// this was an unguarded call relying on set -e, which conflated "changes
// planned" (2) with "the plan errored" (1); both merely stopped the script.
// They are different failures and are reported differently here.
func (q *qualification) expectNoChanges(cli, stateVolume, fixture, cliConfig string) error {
	code, err := q.dockerCode(q.cliArgs(cli, stateVolume, fixture, cliConfig,
		"plan", "-input=false", "-detailed-exitcode",
		"-state=/state/state.tfstate", "-var", "dns_name="+dnsName)...)
	if err != nil {
		return err
	}
	switch code {
	case 0:
		return nil
	case 2:
		return fmt.Errorf("plan against fixture %q under %s reported changes; the state should "+
			"already match the configuration here", fixture, cliConfig)
	default:
		return fmt.Errorf("plan against fixture %q under %s exited %d (the plan itself failed, "+
			"which is not the same as reporting drift)", fixture, cliConfig, code)
	}
}

func gitOutput(args ...string) (string, error) {
	var stdout, stderr bytes.Buffer
	command := exec.Command("git", args...)
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, stderr.String())
	}
	return strings.TrimSpace(stdout.String()), nil
}

func gitQuiet(args ...string) error {
	return exec.Command("git", args...).Run()
}
