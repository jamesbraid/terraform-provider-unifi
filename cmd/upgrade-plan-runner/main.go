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
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/ubiquiti-community/terraform-provider-unifi/internal/catalogparity"
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
	if _, err := os.Stat(composePath); err != nil {
		logger.Printf("%v", err)
		logger.Printf("run this from the repository root")
		return 1
	}
	settings, err := readSettings(root)
	if err != nil {
		logger.Printf("%v", err)
		return 1
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

	// THE FIREWALL ZONE COLLECTION IS NOT WRITABLE UNTIL THIS RUNS, and its
	// absence does not announce itself: go-unifi reports
	// "not found: type=*unifi.FirewallZone", which reads as a missing zone
	// rather than a disabled controller feature. Every acceptance run performs
	// this migration, so a fixture with a zone in it needs the same treatment or
	// it fails for a reason that has nothing to do with the upgrade.
	if err := controllertest.MigrateZoneBasedFirewall(
		ctx,
		os.Getenv("UNIFI_API"),
		site(),
		os.Getenv("UNIFI_USERNAME"),
		os.Getenv("UNIFI_PASSWORD"),
	); err != nil {
		logger.Printf("the zone-based firewall migration failed: %v", err)
		logger.Printf("a fixture containing a firewall zone would fail for that reason and not for an upgrade one")
		return 1
	}

	// controllertest.Start has already populated this process's environment
	// with the controller's address and credentials, so the measurement below
	// sees exactly what the acceptance suite sees. That is why the measuring
	// lives in this process rather than in a child: a second resolution of the
	// endpoint would be a second copy of knowledge that drifts the first time
	// the compose file changes.
	code, err := measure(ctx, logger, root, settings)
	if err != nil {
		logger.Printf("%v", err)
	}
	return code
}

// settings are the run's inputs, read from the environment because that is what
// the workflow supplies and what the script read.
type settings struct {
	cli              string
	releasedRef      string
	releasedBinary   string
	fixtureDirectory string
	output           string
	treeState        *catalogparity.TreeState
}

func readSettings(root string) (settings, error) {
	s := settings{
		cli:              os.Getenv("TERRAFORM_BIN"),
		releasedRef:      envOr("UPGRADE_RELEASED_REF", "v0.101.2"),
		releasedBinary:   os.Getenv("UPGRADE_RELEASED_BINARY"),
		fixtureDirectory: envOr("UPGRADE_FIXTURE", filepath.Join(root, ".woodpecker", "fixtures", "upgrade")),
		output:           envOr("UPGRADE_OUTPUT", filepath.Join(root, "build", "release-ready", "catalog-upgrade-plan.json")),
	}
	if s.cli == "" {
		return s, errors.New("TERRAFORM_BIN is required (this repository sets it to an OpenTofu binary)")
	}
	// MEASURED HERE, AS THE FIRST THING THIS PROCESS DOES, rather than handed
	// in through a flag.
	//
	// The rule elsewhere is that Go is handed the answer and never measures --
	// but that rule exists because the measurer was shell and the consumer was
	// Go, and it is about WHEN rather than about which language. The point is
	// that the tree is measured before anything is built, planned or started,
	// so the receipt cannot claim a commit that does not contain what was
	// measured. This binary IS the caller: nothing runs between it and the
	// workflow, so a flag would only move the same measurement one process
	// outwards and add a way for a stale value to arrive.
	//
	// catalogparity.MeasureTreeState is the same function cmd/tree-state wraps,
	// so there is one implementation and one set of three outcomes: clean,
	// dirty-and-refused, dirty-and-recorded.
	treeState, err := catalogparity.MeasureTreeState(root, "the upgrade-plan receipt",
		os.Getenv("EVIDENCE_ALLOW_DIRTY_TREE") != "")
	if err != nil {
		return s, err
	}
	s.treeState = treeState
	return s, nil
}

func envOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

// site returns the controller site the fixtures are applied to. UNIFI_SITE is
// what the provider reads, so the migration must target the same one or it
// enables the feature somewhere the fixture never looks.
func site() string {
	if value := os.Getenv("UNIFI_SITE"); value != "" {
		return value
	}
	return "default"
}
