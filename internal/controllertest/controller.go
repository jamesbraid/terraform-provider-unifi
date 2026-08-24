// Package controllertest brings up the UniFi controller the acceptance tests
// run against, and the fake devices they drive.
//
// It is internal and test-only: nothing the provider ships imports it. It
// lives in its own package rather than in the provider's test files so the
// fixture can be unit-tested on its own terms, and so it keeps the shape of
// go-unifi's controllertest — both repositories drive the same herder process
// against the same controller images, and the two fixtures should not drift.
package controllertest

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/docker/compose/v2/pkg/api"
	"github.com/testcontainers/testcontainers-go/modules/compose"
	"github.com/ubiquiti-community/go-unifi/unifi"
	"github.com/ubiquiti-community/terraform-provider-unifi/unifi/util"
)

// Logger is the sliver of the provider's logger this fixture needs. Taking an
// interface keeps the dependency pointing one way: the provider's test files
// import this package, so this package must not import the provider.
type Logger interface {
	Printf(format string, v ...any)
}

// Credentials the controller image seeds itself with.
const (
	controllerUser     = "admin"
	controllerPassword = "admin"

	// envRemoveControllerImages restores the old teardown behaviour for an
	// operator that deliberately wants it. Ordinary acceptance runs preserve
	// their digest-pinned controller image so a released/candidate pair can run
	// without contacting a registry between attempts.
	envRemoveControllerImages = "UNIFI_TEST_REMOVE_CONTROLLER_IMAGES"

	// controllerLogTailLines bounds the startup-failure dump: enough to show
	// why the controller stalled, not so much that it floods the run log.
	controllerLogTailLines = 60
)

// Controller is a running controller and the fleet informing it.
type Controller struct {
	// Endpoint is the controller API URL reachable from this process.
	Endpoint string
	// Client is a logged-in API client.
	Client *unifi.ApiClient

	stack  compose.ComposeStack
	herder *herder
}

// Start brings up the controller, waits for its API, then starts the device
// fleet and publishes each device's MAC to the variable its tests read.
//
// The caller must call Stop, whatever happens: the herder has to be signalled
// and drained before Compose comes down, or the teardown removes the network
// out from under the device containers.
func Start(ctx context.Context, logger Logger, composePath string) (*Controller, error) {
	stack, err := compose.NewDockerCompose(composePath)
	if err != nil {
		return nil, fmt.Errorf("read the compose file: %w", err)
	}
	c := &Controller{stack: stack}

	// Readiness is waitForAPI's job, not the image healthcheck's: the image's
	// fixed retry budget can be exhausted by a slow JVM start still
	// configuring its own logging, failing the run with only "container is
	// unhealthy". Waiting on the API the tests actually use is the stronger
	// signal.
	if err := stack.WithOsEnv().
		Up(ctx, compose.WithRecreate(api.RecreateDiverged)); err != nil {
		logControllerStartupFailure(ctx, logger, stack)
		return c, fmt.Errorf("compose up: %w", err)
	}

	container, err := stack.ServiceContainer(ctx, "unifi")
	if err != nil {
		return c, fmt.Errorf("find the controller container: %w", err)
	}
	host, err := container.Host(ctx)
	if err != nil {
		return c, fmt.Errorf("controller host: %w", err)
	}
	port, err := container.MappedPort(ctx, "8443/tcp")
	if err != nil {
		return c, fmt.Errorf("controller API port: %w", err)
	}

	c.Endpoint = fmt.Sprintf("https://%s:%s", host, port.Port())
	logger.Printf("UniFi controller endpoint: %s", c.Endpoint)
	if err := exportProviderEnv(c.Endpoint); err != nil {
		return c, err
	}

	client, err := waitForAPI(ctx, logger, c.Endpoint, controllerUser, controllerPassword)
	if err != nil {
		logControllerStartupFailure(ctx, logger, stack)
		return c, err
	}
	c.Client = client

	// Every device the suite drives comes from the herder, which owns device
	// planning and device-container lifecycle only: the network, the
	// controller, adoption and the assertions stay this fixture's job.
	h, err := StartDevices(ctx, logger, container)
	if err != nil {
		return c, err
	}
	c.herder = h

	for _, member := range herderFleet {
		device := h.devices[member.Env]
		if err := waitForHerderDevice(ctx, logger, client, device.MAC); err != nil {
			return c, err
		}
		if member.Adopt {
			if err := adoptHerderDevice(ctx, logger, client, device.MAC); err != nil {
				return c, err
			}
		}
		if err := os.Setenv(member.Env, device.MAC); err != nil {
			return c, fmt.Errorf("publish %s: %w", member.Env, err)
		}
	}
	if err := MigrateZoneBasedFirewall(ctx, c.Endpoint, "default", controllerUser, controllerPassword); err != nil {
		return c, fmt.Errorf("migrate zone-based firewall: %w", err)
	}
	return c, nil
}

// Stop tears the fixture down in the only order that works: the herder first,
// then Compose.
func (c *Controller) Stop(logger Logger) error {
	if c.herder != nil {
		c.herder.stop(logger)
	}
	if c.stack == nil {
		return nil
	}
	logger.Printf("RUNNING TEAR DOWN")
	// Not the caller's context: teardown has to run even when the context
	// that started everything is already cancelled.
	options := []compose.StackDownOption{compose.RemoveOrphans(true)}
	if removeControllerImages() {
		options = append(options, compose.RemoveImagesLocal)
	}
	return c.stack.Down(context.Background(), options...)
}

func removeControllerImages() bool {
	return os.Getenv(envRemoveControllerImages) == "true"
}

// logControllerStartupFailure reports what the controller was doing when its
// healthcheck never went green. Compose says only that the container is
// unhealthy, and teardown removes it, so without this the run leaves nothing
// to diagnose. Every step is best-effort: this runs on a path that has already
// failed, and losing the original error to a diagnostic would be worse than
// reporting nothing.
func logControllerStartupFailure(ctx context.Context, logger Logger, stack compose.ComposeStack) {
	container, err := stack.ServiceContainer(ctx, "unifi")
	if err != nil {
		logger.Printf("controller startup failed and its container is gone: %v", err)
		return
	}
	if state, err := container.State(ctx); err == nil {
		logger.Printf(
			"controller container state: status=%s running=%t exit=%d oom=%t error=%q",
			state.Status, state.Running, state.ExitCode, state.OOMKilled, state.Error,
		)
		if state.Health != nil {
			logger.Printf("controller healthcheck: status=%s failing streak=%d",
				state.Health.Status, state.Health.FailingStreak)
			for _, probe := range state.Health.Log {
				logger.Printf("controller healthcheck probe: exit=%d output=%s",
					probe.ExitCode, strings.TrimSpace(probe.Output))
			}
		}
	}
	readCloser, err := container.Logs(ctx)
	if err != nil {
		logger.Printf("controller logs unavailable: %v", err)
		return
	}
	defer func() { _ = readCloser.Close() }()
	// The controller is chatty; its last lines are the ones that say why
	// startup stalled.
	scanner := bufio.NewScanner(readCloser)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	tail := make([]string, 0, controllerLogTailLines)
	for scanner.Scan() {
		if len(tail) == controllerLogTailLines {
			tail = tail[1:]
		}
		tail = append(tail, scanner.Text())
	}
	for _, line := range tail {
		logger.Printf("controller log: %s", strings.TrimSpace(line))
	}
}

// exportProviderEnv points the provider under test at this controller.
func exportProviderEnv(endpoint string) error {
	for name, value := range map[string]string{
		"UNIFI_USERNAME": controllerUser,
		"UNIFI_PASSWORD": controllerPassword,
		"UNIFI_INSECURE": "true",
		"UNIFI_API":      endpoint,
		"UNIFI_API_KEY":  "",
	} {
		if err := os.Setenv(name, value); err != nil {
			return fmt.Errorf("set %s: %w", name, err)
		}
	}
	return nil
}

// waitForAPI waits for the controller API to be ready and accepting JSON
// requests. This is the fixture's readiness gate: it proves the API the tests
// drive is initialized, which the container healthcheck alone never did.
func waitForAPI(
	ctx context.Context,
	logger Logger,
	endpoint, user, password string,
) (client *unifi.ApiClient, err error) {
	// Generous on purpose: this is now the only readiness gate, and a cold
	// controller on a busy runner has been seen spending minutes in JVM
	// startup before it serves anything.
	maxRetries := 120
	retryDelay := 3 * time.Second

	logger.Printf(
		"Waiting for UniFi API to be ready (max %d attempts, %v between attempts)...",
		maxRetries,
		retryDelay,
	)

	var loginSuccessful bool
	for i := range maxRetries {
		// Step 1: log in.
		client, err = unifi.New(ctx, &unifi.Config{
			BaseURL:        endpoint,
			Username:       user,
			Password:       password,
			AllowInsecure:  true,
			TimeoutSeconds: util.Ptr(30),
			CloudConnector: false,
		})
		if err != nil {
			if i < maxRetries-1 {
				if (i+1)%10 == 0 {
					logger.Printf(
						"Still waiting for login... (attempt %d/%d): %v", i+1, maxRetries, err,
					)
				}
				time.Sleep(retryDelay)
				continue
			}
			return nil, fmt.Errorf(
				"UniFi API login did not succeed after %d attempts (waited %v): %w",
				maxRetries, time.Duration(maxRetries)*retryDelay, err,
			)
		}

		if !loginSuccessful {
			logger.Printf("✓ Login successful after %d attempts", i+1)
			loginSuccessful = true
		}

		// Step 2: sites are initialized.
		sites, err := client.ListSites(ctx)
		if err != nil || len(sites) == 0 {
			if i < maxRetries-1 {
				if (i+1)%10 == 0 {
					logger.Printf(
						"Sites not ready yet... (attempt %d/%d): %v", i+1, maxRetries, err,
					)
				}
				time.Sleep(retryDelay)
				continue
			}
			if err != nil {
				return nil, fmt.Errorf(
					"UniFi API sites not ready after %d attempts: %w", maxRetries, err,
				)
			}
			return nil, fmt.Errorf("no sites available after %d attempts", maxRetries)
		}

		// Step 3: the devices endpoint answers. An empty list is the expected
		// answer here: the controller starts with no devices of its own, and
		// the herder has not run yet.
		if _, err := client.ListDevice(ctx, "default"); err != nil {
			if i < maxRetries-1 {
				if (i+1)%10 == 0 {
					logger.Printf(
						"Devices endpoint not ready... (attempt %d/%d): %v", i+1, maxRetries, err,
					)
				}
				time.Sleep(retryDelay)
				continue
			}
			return nil, fmt.Errorf(
				"device endpoint not operational after %d attempts: %w", maxRetries, err,
			)
		}

		// Step 4: networks are initialized.
		networks, err := client.ListNetwork(ctx, "default")
		if err != nil || len(networks) == 0 {
			if i < maxRetries-1 {
				if (i+1)%10 == 0 {
					logger.Printf(
						"Networks not ready yet... (attempt %d/%d): %v", i+1, maxRetries, err,
					)
				}
				time.Sleep(retryDelay)
				continue
			}
			if err != nil {
				return nil, fmt.Errorf(
					"UniFi API networks not ready after %d attempts: %w", maxRetries, err,
				)
			}
			return nil, fmt.Errorf("no networks available after %d attempts", maxRetries)
		}

		ensureWAN(ctx, logger, client, networks)

		logger.Printf(
			"✓ UniFi API fully ready (login + %d sites + devices endpoint) after %d attempts",
			len(sites), i+1,
		)
		return client, nil
	}

	return nil, fmt.Errorf("UniFi API did not become ready after %d attempts", maxRetries)
}

// ensureWAN creates the default WAN network when the controller has none.
// Resources under test reference it, and a fresh controller does not always
// bring one of its own.
//
// A failure here is deliberately not fatal, which is why nothing is returned:
// only some tests need a WAN, and the ones that do report its absence far more
// precisely than a setup error could.
func ensureWAN(
	ctx context.Context,
	logger Logger,
	client *unifi.ApiClient,
	networks []unifi.Network,
) {
	for _, n := range networks {
		if n.Purpose == unifi.PurposeWAN && n.WANNetworkGroup != nil &&
			*n.WANNetworkGroup == "WAN" {
			return
		}
	}
	logger.Printf("No default WAN network found, creating \"Internet 1\"...")
	if _, err := client.CreateNetwork(ctx, "default", &unifi.Network{
		Name:            util.Ptr("Internet 1"),
		Purpose:         unifi.PurposeWAN,
		WANNetworkGroup: util.Ptr("WAN"),
		WANType:         util.Ptr("dhcp"),
	}); err != nil {
		logger.Printf("Failed to create default WAN network: %v", err)
		return
	}
	logger.Printf("✓ Created default WAN network \"Internet 1\"")
}
