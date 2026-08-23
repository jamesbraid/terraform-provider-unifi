//go:build acceptance

package unifi

// THE ONLY PLACE THAT NEEDS A CONTROLLER, AND THEREFORE THE ONLY PLACE THAT
// IMPORTS THE THING THAT CAN START ONE.
//
// internal/controllertest reaches testcontainers, and through it docker,
// compose, moby, containerd, OpenTelemetry and sigstore. One import edge, in
// this one function, put 119 third-party modules into the dependency graph of
// every `go test ./unifi` -- measured: 178 modules with the edge, 59 without.
//
// TestMain already refused to start a controller unless TF_ACC was set, so the
// START was gated at run time. The IMPORT was not gated at all, which is the
// difference a build tag makes: a package you never call is still a package you
// compile, download and keep in go.sum.
//
// The six other references were plain string constants and moving them recovers
// NOTHING on its own -- measured at 178 either way. This edge holds all of it.

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
