// Command upgrade-plan-runner starts a controller, runs the upgrade harness
// against it, and stops the controller again.
//
// WHY THIS EXISTS RATHER THAN COMPOSE IN THE SHELL SCRIPT. The script measures
// an upgrade and must not also provision one: a step that provisions and
// measures has two reasons to fail and one exit code to report them with, and
// the failure everyone will be reading is the measurement.
//
// It is also the only way to get the controller's address without duplicating
// knowledge. The compose file publishes the API on an EPHEMERAL host port, on
// purpose, so a leftover container cannot reserve 8443 globally -- which means
// the endpoint is not knowable ahead of time and cannot be written into a
// workflow. controllertest.Start already resolves it and exports UNIFI_API,
// UNIFI_USERNAME, UNIFI_PASSWORD and UNIFI_INSECURE into this process, so the
// script inherits exactly what the acceptance suite uses. A shell
// reimplementation would be a second copy of that resolution, and the two would
// drift the first time the compose file changed.
package main

import (
	"context"
	"errors"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/ubiquiti-community/terraform-provider-unifi/internal/controllertest"
)

func main() {
	os.Exit(run())
}

func run() int {
	logger := log.New(os.Stderr, "upgrade-plan-runner: ", 0)

	root, err := os.Getwd()
	if err != nil {
		logger.Printf("working directory: %v", err)
		return 1
	}
	composePath := filepath.Join(root, "docker-compose.yaml")
	script := filepath.Join(root, ".woodpecker", "scripts", "catalog-upgrade-plan.sh")
	for _, required := range []string{composePath, script} {
		if _, err := os.Stat(required); err != nil {
			logger.Printf("%v", err)
			logger.Printf("run this from the repository root")
			return 1
		}
	}

	// Ctrl-C and CI cancellation must still stop the controller. A cancelled
	// run that leaves one behind makes the NEXT run fail for a reason that has
	// nothing to do with it, which is a recorded hazard in this repository
	// rather than a hypothetical one.
	ctx, stopSignals := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopSignals()

	controller, err := controllertest.Start(ctx, logger, composePath)
	if err != nil {
		logger.Printf("the controller did not start: %v", err)
		logger.Printf("nothing was measured; this is not an upgrade result")
		return 1
	}
	defer func() {
		if err := controller.Stop(logger); err != nil {
			logger.Printf("the controller did not stop cleanly: %v", err)
		}
	}()

	// The script inherits this process's environment, which controllertest.Start
	// has already populated with the controller's address and credentials.
	command := exec.CommandContext(ctx, "bash", script)
	command.Dir = root
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	command.Env = os.Environ()

	err = command.Run()
	if err == nil {
		return 0
	}
	// The harness distinguishes "the candidate regressed" from "the fixture is
	// unusable" by exit code, and that distinction is the whole point of the
	// control plan. Passing it through unchanged keeps the two apart; collapsing
	// them to 1 would hand the reader the coin flip the script exists to avoid.
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return exit.ExitCode()
	}
	logger.Printf("could not run %s: %v", script, err)
	return 1
}
