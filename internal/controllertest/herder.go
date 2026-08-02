package controllertest

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net"
	"os"
	"os/exec"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/ubiquiti-community/go-unifi/unifi"
)

// unifi-emu-herder is a foreground process that starts fake UniFi devices as
// Docker containers on a network the caller already owns. The ownership split
// is the whole point: this harness owns the Compose network, the controller,
// the device request, adoption and the assertions; the herder owns device
// planning and device-container lifecycle and nothing else. Its supported
// integration surface is the versioned NDJSON protocol on its stdout — stderr
// is diagnostics and aggregated device logs, and is never parsed here.
// The two herder coordinates share go-unifi's names on purpose. Both
// repositories drive the same process against the same controller images, so a
// runner configured for one is configured for both, and there is one thing to
// bump when the herder moves rather than two.
const (
	// envHerderBin points at the unifi-emu-herder binary. The acceptance
	// suite cannot run without it: the controller starts with no devices of
	// its own, so every device under test comes from here.
	envHerderBin = "UNIFI_TEST_HERDER_BIN"
	// envHerderSyntheticImage overrides the public synthetic image. A
	// development herder build has no compiled default and needs it; a
	// release build carries a version-matched one.
	envHerderSyntheticImage = "UNIFI_TEST_HERDER_SYNTHETIC_IMAGE"
)

// The MACs the fleet publishes to the tests. These stay UNIFI_ACC_*: they are
// this provider's own test inputs, not part of the shared herder contract.
const (
	EnvAccDeviceMAC = "UNIFI_ACC_DEVICE_MAC"
	EnvAccAPMAC     = "UNIFI_ACC_AP_MAC"
)

// herderFleetMember is one device every acceptance run starts.
type herderFleetMember struct {
	Model string
	// Env is the variable this device's MAC reaches the tests through.
	Env string
	// Adopt asks the harness to adopt this device before any test runs.
	// Adoption is the caller's job, never the herder's, and whether it
	// happens up front is a per-device decision: a device that is the
	// subject of an adoption test must be left pending.
	Adopt bool
}

// herderFleet is the fixed fleet the acceptance suite runs against. It is not
// configurable: the tests name the devices they need through the constants
// above, so a fleet that varied with the environment would only mean tests
// that pass on one runner and skip on another.
var herderFleet = []herderFleetMember{
	{
		// A switch, left pending on purpose. unifi_device's allow_adoption
		// is exactly what TestAccDeviceFramework_basic drives, so adopting
		// it here would decide the result before the test starts.
		Model: "USM8P",
		Env:   EnvAccDeviceMAC,
		Adopt: false,
	},
	{
		// An access point, adopted up front. The controller rejects AP
		// group membership for any device it has not adopted, so for
		// TestAccAPGroupFramework_withDevices an adopted AP is a
		// precondition rather than the subject.
		Model: "U7PRO",
		Env:   EnvAccAPMAC,
		Adopt: true,
	},
}

// envRyukDisabled is the Testcontainers reaper switch runAcceptanceTests sets
// for its own Compose lifecycle. The herder reads effective Testcontainers
// configuration and refuses to create anything while the reaper is disabled,
// because crash cleanup is the only thing that removes device containers when
// the herder is killed. Inherited process state must not downgrade that, so
// the variable is removed from the child's environment rather than the check
// being worked around.
const envRyukDisabled = "TESTCONTAINERS_RYUK_DISABLED"

// envDockerSocketOverride tells Testcontainers which socket path to give the
// reaper container, as opposed to the one this process dials.
//
// Re-enabling the reaper for the child makes that distinction matter. Colima
// puts its socket under the user's home directory on the host and mounts the
// daemon's own socket at inVMDockerSocket, so the reaper is created with a
// bind mount of a path the daemon cannot see, fails to start, and takes the
// whole session down with it. Testcontainers reports that as a plain creation
// failure of the device container, which says nothing about the socket, so the
// override is derived here rather than left as an unexplained failure. An
// override the caller set already wins.
const (
	envDockerSocketOverride = "TESTCONTAINERS_DOCKER_SOCKET_OVERRIDE"
	colimaSocketMarker      = "/.colima/"
	inVMDockerSocket        = "/var/run/docker.sock"
)

// controllerInformPort is the port devices inform the controller on. The
// Compose controller reports it as inform_port and publishes it.
const controllerInformPort = 8080

const (
	// herderStartupTimeout and herderChildStopTimeout are passed to the child
	// so it and this harness work to the same clock.
	herderStartupTimeout   = 5 * time.Minute
	herderChildStopTimeout = 30 * time.Second
	// herderSlack keeps every wait here strictly longer than the child's own
	// deadline for the same phase. A stuck run is then reported by the herder
	// as the failure it is, with a code and a phase, rather than by this
	// harness as an unexplained timeout.
	herderSlack = 30 * time.Second
	// herderStopTimeout covers SIGTERM, the terminal event and process exit.
	herderStopTimeout = herderChildStopTimeout + herderSlack
	// herderInformTimeout is how long a started device gets to reach the
	// controller before the run is called off.
	herderInformTimeout = 2 * time.Minute
	// herderAdoptTimeout bounds a precondition adoption reaching connected.
	herderAdoptTimeout = 3 * time.Minute
)

// herderDevice is one entry of the ready event. Only the ready event carries
// addresses, and devices batched into one synthetic container share an IP by
// design, so identity is the MAC and never the address.
type herderDevice struct {
	Index  int    `json:"index"`
	Model  string `json:"model"`
	MAC    string `json:"mac"`
	Serial string `json:"serial"`
	Name   string `json:"name"`
	IP     string `json:"ip"`
}

// herderEvent is the part of a protocol-1 stdout event this harness models.
// The protocol may add fields within a version, so unknown ones are ignored.
type herderEvent struct {
	Protocol int            `json:"protocol"`
	Event    string         `json:"event"`
	RunID    string         `json:"run_id"`
	Phase    string         `json:"phase"`
	Code     string         `json:"code"`
	Message  string         `json:"message"`
	Reason   string         `json:"reason"`
	Devices  []herderDevice `json:"devices"`
}

// herderRequest is the strict JSON document the herder reads on stdin. It
// rejects unknown fields and carries no address: the herder allocates every
// identity it is not given, and Docker assigns the address.
type herderRequest struct {
	Version int                     `json:"version"`
	Devices []herderRequestedDevice `json:"devices"`
}

type herderRequestedDevice struct {
	Model string `json:"model"`
}

// herder is one running unifi-emu-herder child.
type herder struct {
	cmd    *exec.Cmd
	events chan herderEvent
	// devices maps each fleet member's variable to the device it resolved to.
	devices map[string]herderDevice

	stopOnce sync.Once
}

// StartDevices starts the herder against the Compose controller and
// blocks until its ready event names every device in the fleet. A missing
// herder binary is fatal, not a skip: the controller starts with no devices,
// so a suite without one would go green having tested nothing.
func StartDevices(
	ctx context.Context,
	logger Logger,
	controller *testcontainers.DockerContainer,
) (*herder, error) {
	bin := os.Getenv(envHerderBin)
	if bin == "" {
		return nil, fmt.Errorf(
			"%s is not set: the controller starts no devices of its own, so the "+
				"acceptance suite has nothing to drive", envHerderBin,
		)
	}

	network, informURL, err := controllerInformEndpoint(ctx, controller)
	if err != nil {
		return nil, err
	}

	requested := make([]herderRequestedDevice, len(herderFleet))
	for i, member := range herderFleet {
		requested[i] = herderRequestedDevice{Model: member.Model}
	}
	request, err := json.Marshal(herderRequest{Version: 1, Devices: requested})
	if err != nil {
		return nil, fmt.Errorf("encode the device request: %w", err)
	}

	args := []string{
		"--network", network,
		"--inform-url", informURL,
		"--devices", "-",
		"--startup-timeout", herderStartupTimeout.String(),
		"--stop-timeout", herderChildStopTimeout.String(),
	}
	if image := os.Getenv(envHerderSyntheticImage); image != "" {
		args = append(args, "--synthetic-image", image)
	}

	// The child is not tied to ctx: it has to outlive a cancelled setup
	// context so its own SIGTERM cleanup still removes the containers.
	//
	// Running an operator-named binary is this fixture's whole purpose, so
	// the variable command is the contract rather than a weakness: bin comes
	// from the environment of whoever started the test run, who could run the
	// same binary directly. Every argument is built here from a fixed fleet
	// and the controller's own inspected network, never from test data.
	// #nosec G204,G702 -- test fixture; the command is the caller's own herder
	cmd := exec.Command(bin, args...)
	cmd.Env = herderChildEnv(os.Environ())
	cmd.Stdin = bytes.NewReader(request)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("open the herder control stream: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("open the herder diagnostics: %w", err)
	}

	models := make([]string, len(herderFleet))
	for i, member := range herderFleet {
		models[i] = member.Model
	}
	logger.Printf(
		"Starting %s on network %s informing %s (fleet %s)",
		bin, network, informURL, strings.Join(models, ", "),
	)
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start the herder: %w", err)
	}

	h := &herder{
		cmd: cmd,
		// Protocol 1 emits at most three events: started, ready and one
		// terminal event. The buffer holds every one of them, so the
		// decoder never blocks on a consumer that is between waits.
		events: make(chan herderEvent, 8),
	}
	go func() {
		defer close(h.events)
		if err := decodeHerderEvents(stdout, h.events); err != nil {
			logger.Printf("herder control stream: %v", err)
		}
	}()
	// Diagnostics and aggregated device logs, forwarded verbatim. Nothing
	// here is parsed: only protocol-1 stdout events are stable.
	go forwardHerderDiagnostics(logger, stderr)

	ready, err := h.waitForReady(ctx)
	if err != nil {
		h.stop(logger)
		return nil, err
	}
	devices, err := matchHerderFleet(ready, herderFleet)
	if err != nil {
		h.stop(logger)
		return nil, err
	}
	h.devices = devices
	for _, member := range herderFleet {
		d := devices[member.Env]
		logger.Printf(
			"✓ herder ready: %s %s (serial %s, name %s) at %s -> %s",
			d.Model, d.MAC, d.Serial, d.Name, d.IP, member.Env,
		)
	}
	return h, nil
}

// waitForReady consumes events until the ready event, and turns a terminal
// event that arrives first into the failure it reports.
func (h *herder) waitForReady(ctx context.Context) ([]herderDevice, error) {
	ctx, cancel := context.WithTimeout(ctx, herderStartupTimeout+herderSlack)
	defer cancel()

	for {
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("herder did not report ready: %w", ctx.Err())
		case ev, open := <-h.events:
			if !open {
				return nil, errors.New(
					"the herder control stream ended before the ready event",
				)
			}
			switch ev.Event {
			case "ready":
				return ev.Devices, nil
			case "failed":
				return nil, errors.New(herderFailureMessage(ev))
			case "stopped":
				return nil, errors.New("the herder stopped before reporting ready")
			}
		}
	}
}

// stop signals the herder, waits for its terminal event and then for the
// process. Compose teardown must not start until this returns: removing the
// network first would pull it out from under the device containers.
func (h *herder) stop(logger Logger) {
	h.stopOnce.Do(func() {
		if h.cmd.Process == nil {
			return
		}
		logger.Printf("Stopping the herder")
		if err := h.cmd.Process.Signal(syscall.SIGTERM); err != nil {
			logger.Printf("signal the herder: %v", err)
		}

		deadline := time.After(herderStopTimeout)
		for draining := true; draining; {
			select {
			case ev, open := <-h.events:
				if !open {
					draining = false
					break
				}
				switch ev.Event {
				case "stopped":
					logger.Printf("✓ herder stopped (%s)", ev.Reason)
				case "failed":
					logger.Printf("herder %s", herderFailureMessage(ev))
				}
			case <-deadline:
				logger.Printf("herder did not report a terminal event in %s", herderStopTimeout)
				draining = false
			}
		}

		if err := h.cmd.Wait(); err != nil {
			logger.Printf("herder exited: %v", err)
			return
		}
		logger.Printf("✓ herder exited cleanly; every device container is gone")
	})
}

// controllerInformEndpoint reads the Compose network the controller is on and
// the canonical IPv4 inform URL to point devices at.
func controllerInformEndpoint(
	ctx context.Context,
	controller *testcontainers.DockerContainer,
) (network, informURL string, err error) {
	inspect, err := controller.Inspect(ctx)
	if err != nil {
		return "", "", fmt.Errorf("inspect the controller container: %w", err)
	}
	addresses := make(map[string]string, len(inspect.NetworkSettings.Networks))
	for name, endpoint := range inspect.NetworkSettings.Networks {
		addresses[name] = endpoint.IPAddress.String()
	}
	return informEndpoint(addresses)
}

// informEndpoint turns the controller's network attachments into the network
// name to start devices on and the inform URL to point them at.
//
// The URL must be exactly http://<canonical-IPv4-literal>:<port>/inform: a
// device container resolves nothing, and the controller rejects an inform
// whose host is not an address it recognizes. It also has to be the address
// the controller advertises for inform after adoption, or devices adopt and
// then stall. A controller on exactly one network has exactly one address to
// advertise, so that is the one requirement checked here; more than one
// attachment makes the advertised address a guess and is refused.
func informEndpoint(addresses map[string]string) (network, informURL string, err error) {
	names := slices.Sorted(maps.Keys(addresses))
	if len(names) != 1 {
		return "", "", fmt.Errorf(
			"the controller container is attached to %d networks (%s), so the address it "+
				"advertises for inform is ambiguous; it must be on exactly one",
			len(names), strings.Join(names, ", "),
		)
	}

	name := names[0]
	ip := net.ParseIP(addresses[name])
	if ip == nil || ip.To4() == nil {
		return "", "", fmt.Errorf(
			"the controller address %q on network %s is not an IPv4 literal",
			addresses[name], name,
		)
	}
	return name, fmt.Sprintf("http://%s:%d/inform", ip.To4(), controllerInformPort), nil
}

// decodeHerderEvents reads the NDJSON control stream until it ends.
func decodeHerderEvents(r io.Reader, out chan<- herderEvent) error {
	decoder := json.NewDecoder(r)
	for {
		var ev herderEvent
		if err := decoder.Decode(&ev); err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return fmt.Errorf("decode a control event: %w", err)
		}
		out <- ev
	}
}

// forwardHerderDiagnostics copies the herder's stderr to the test log.
func forwardHerderDiagnostics(logger Logger, r io.Reader) {
	scanner := bufio.NewScanner(r)
	// Aggregated device logs can carry a long line; the default 64KiB
	// token limit would end the scan on one.
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		logger.Printf("herder: %s", scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		logger.Printf("herder diagnostics ended: %v", err)
	}
}

// matchHerderFleet pairs the ready event's devices with the fleet that was
// requested, keyed by the variable each member publishes its MAC through.
//
// The pairing is by request index, which is what the protocol guarantees:
// devices carry the index of the entry they were resolved from. Matching on
// model instead would be ambiguous the moment the fleet wants two of one
// model, and matching on order alone would go unnoticed if it ever shifted.
func matchHerderFleet(
	devices []herderDevice,
	fleet []herderFleetMember,
) (map[string]herderDevice, error) {
	if len(devices) != len(fleet) {
		return nil, fmt.Errorf(
			"the ready event names %d devices, want the %d that were requested",
			len(devices), len(fleet),
		)
	}
	out := make(map[string]herderDevice, len(fleet))
	for _, d := range devices {
		if d.Index < 0 || d.Index >= len(fleet) {
			return nil, fmt.Errorf("ready device index %d is outside the fleet", d.Index)
		}
		member := fleet[d.Index]
		if d.Model != member.Model {
			return nil, fmt.Errorf(
				"ready device %d is model %q, want %q", d.Index, d.Model, member.Model,
			)
		}
		if d.MAC == "" {
			return nil, fmt.Errorf("ready device %d carries no MAC", d.Index)
		}
		if _, dup := out[member.Env]; dup {
			return nil, fmt.Errorf("two ready devices claim index %d", d.Index)
		}
		out[member.Env] = d
	}
	return out, nil
}

// herderFailureMessage renders a failed event. The code is the stable part;
// the message is the herder's own public summary.
func herderFailureMessage(ev herderEvent) string {
	return fmt.Sprintf("failed in phase %s: %s: %s", ev.Phase, ev.Code, ev.Message)
}

// herderChildEnv is the environment the herder child runs in: this process's,
// minus the reaper switch, plus the socket the reaper needs on a runtime whose
// Docker socket does not live where the daemon sees it.
func herderChildEnv(environ []string) []string {
	out := make([]string, 0, len(environ)+1)
	var dockerHost string
	overridden := false
	for _, entry := range environ {
		switch {
		case strings.HasPrefix(entry, envRyukDisabled+"="):
			continue
		case strings.HasPrefix(entry, "DOCKER_HOST="):
			dockerHost = strings.TrimPrefix(entry, "DOCKER_HOST=")
		case strings.HasPrefix(entry, envDockerSocketOverride+"="):
			overridden = true
		}
		out = append(out, entry)
	}
	if !overridden && strings.Contains(dockerHost, colimaSocketMarker) {
		out = append(out, envDockerSocketOverride+"="+inVMDockerSocket)
	}
	return out
}

// waitForHerderDevice blocks until the controller has seen the started device.
// The ready event only says the container is healthy; this is what proves the
// inform URL it was given is one the controller actually answers on, and it
// runs before any test so a mis-derived address fails here instead of inside
// an acceptance test.
func waitForHerderDevice(
	ctx context.Context,
	logger Logger,
	client *unifi.ApiClient,
	mac string,
) error {
	ctx, cancel := context.WithTimeout(ctx, herderInformTimeout)
	defer cancel()

	logger.Printf("Waiting for the controller to see %s...", mac)
	for {
		device, err := client.GetDeviceByMAC(ctx, "default", mac)
		if err == nil && device != nil {
			logger.Printf("✓ controller sees %s (state %d)", mac, device.State)
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("the controller never saw device %s: %w", mac, ctx.Err())
		case <-time.After(3 * time.Second):
		}
	}
}

// adoptHerderDevice adopts a device and waits for it to reach connected.
//
// This runs only for fleet members marked Adopt: a device that some test
// adopts itself must be left pending, or the test measures nothing. Adoption
// belongs to the caller either way — the herder starts devices and stops
// there.
func adoptHerderDevice(
	ctx context.Context,
	logger Logger,
	client *unifi.ApiClient,
	mac string,
) error {
	ctx, cancel := context.WithTimeout(ctx, herderAdoptTimeout)
	defer cancel()

	logger.Printf("Adopting %s as a test precondition...", mac)
	if err := client.AdoptDevice(ctx, "default", mac); err != nil {
		return fmt.Errorf("adopt device %s: %w", mac, err)
	}
	for {
		device, err := client.GetDeviceByMAC(ctx, "default", mac)
		if err == nil && device != nil && device.State == unifi.DeviceStateConnected {
			logger.Printf("✓ %s adopted and connected", mac)
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("device %s never reached connected: %w", mac, ctx.Err())
		case <-time.After(3 * time.Second):
		}
	}
}
