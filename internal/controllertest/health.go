package controllertest

import (
	"context"
	"fmt"
	"time"

	"github.com/moby/moby/api/types/container"
	"github.com/testcontainers/testcontainers-go/modules/compose"
)

// healthVerdict reads one container state: ready, keep waiting, or give up.
// An image with no healthcheck is ready as soon as it runs; the API probe
// that follows is the real gate for those.
func healthVerdict(state *container.State) (bool, error) {
	if state.Status == "exited" || state.Status == "dead" {
		return false, fmt.Errorf("controller container %s (exit %d)", state.Status, state.ExitCode)
	}
	if state.Health == nil {
		return true, nil
	}
	switch state.Health.Status {
	case "healthy":
		return true, nil
	case "unhealthy":
		return false, fmt.Errorf("controller healthcheck unhealthy after %d failing probes", state.Health.FailingStreak)
	default:
		return false, nil
	}
}

// waitForHealthy blocks until the controller's own healthcheck passes. The
// -sim image's check already proves login, the v2 API and the demo fleet, so
// nothing logs in before this returns: the controller rate-limits login
// globally, and a probe that logs in during JVM start can lock the suite out
// of the controller it is waiting for.
func waitForHealthy(ctx context.Context, logger Logger, stack compose.ComposeStack, timeout time.Duration) error {
	container, err := stack.ServiceContainer(ctx, "unifi")
	if err != nil {
		return fmt.Errorf("find the controller container: %w", err)
	}
	deadline := time.Now().Add(timeout)
	for attempt := 1; ; attempt++ {
		state, err := container.State(ctx)
		if err != nil {
			return fmt.Errorf("inspect the controller container: %w", err)
		}
		ready, err := healthVerdict(state)
		if err != nil {
			return err
		}
		if ready {
			logger.Printf("✓ Controller healthcheck passed after %d checks", attempt)
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("controller healthcheck not green within %v", timeout)
		}
		if attempt%20 == 0 {
			logger.Printf("Still waiting for the controller healthcheck (%d checks)...", attempt)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(3 * time.Second):
		}
	}
}
