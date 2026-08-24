//go:build acceptance

package unifi

// The only file that may import internal/controllertest: that one edge pulls
// testcontainers and its whole dependency tree into every `go test ./unifi`,
// which is why it sits behind the acceptance build tag.

import (
	"context"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-log/tflogtest"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/controllertest"
)

func runAcceptanceTests(m *testing.M) int {
	// The provider's own Compose lifecycle does not want the Testcontainers
	// reaper. The herder child does, and gets it back: see herderChildEnv.
	if err := os.Setenv("TESTCONTAINERS_RYUK_DISABLED", "true"); err != nil {
		panic(err)
	}

	ctx, cancel := context.WithCancel(
		tflogtest.RootLogger(context.Background(), os.Stdout),
	)
	defer cancel()

	logger := NewLogger(ctx)

	controller, err := controllertest.Start(ctx, logger, "../docker-compose.yaml")
	// Stop unconditionally: Start returns a usable handle even when it fails
	// partway, and whatever it did bring up still has to come down.
	defer func() {
		if stopErr := controller.Stop(logger); stopErr != nil {
			panic(stopErr)
		}
	}()
	if err != nil {
		panic(err)
	}
	return m.Run()
}
