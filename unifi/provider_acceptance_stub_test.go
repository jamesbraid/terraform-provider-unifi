//go:build !acceptance

package unifi

import (
	"fmt"
	"os"
	"testing"
)

// WITHOUT THE TAG THIS REFUSES RATHER THAN RUNNING, and the refusal is the
// point of it.
//
// The acceptance harness lives behind a build tag so its dependencies stay out
// of the ordinary test build. A stub that quietly returned m.Run() would let
// `TF_ACC=1 go test ./unifi` run every acceptance test against a controller
// that was never started -- passing or failing for reasons that have nothing to
// do with the code, with nothing on screen to say why.
//
// So the only way to ask for acceptance tests and not get them is to be told.
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
