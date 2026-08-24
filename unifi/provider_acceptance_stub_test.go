//go:build !acceptance

package unifi

import (
	"fmt"
	"os"
	"testing"
)

// runAcceptanceTests refuses rather than running: a stub that quietly
// returned m.Run() would run every acceptance test against a controller that
// was never started.
func runAcceptanceTests(_ *testing.M) int {
	fmt.Fprintln(os.Stderr,
		"TF_ACC is set but this binary was built without the acceptance harness.\n"+
			"The harness starts the controller and is behind a build tag, because it\n"+
			"pulls testcontainers and 119 other modules into the graph.\n\n"+
			"    go test -tags acceptance ./unifi/...\n\n"+
			"Or set UNIFI_SKIP_CONTAINER with the UNIFI_* variables already pointing\n"+
			"at a controller you started yourself.")
	return 1
}
