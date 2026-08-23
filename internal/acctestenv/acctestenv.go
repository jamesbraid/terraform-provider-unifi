// Package acctestenv names the environment variables the acceptance fleet
// publishes to the tests.
//
// IT EXISTS TO HAVE NO DEPENDENCIES. These two names were declared in
// internal/controllertest, which is correct as far as ownership goes -- the
// herder is what sets them -- but importing that package to read a string
// constant pulled testcontainers, and through it docker, compose, moby,
// containerd, OpenTelemetry and sigstore, into the graph of every ordinary
// `go test ./unifi`. Measured: 178 modules with that edge, 59 without.
//
// Three test files wanted nothing but these two strings. Now they can have
// them, and the one file that genuinely needs a controller is the only one
// that reaches for the package that can start one.
//
// THE VALUES ARE DUPLICATED IN controllertest ON PURPOSE, and the reason is
// not laziness. internal/controllertest is GRAFTED onto the released tree by
// the controller differential -- copyTree, in controllerdifferential/prepare.go
// -- and has to compile there. A released tree predating this package does not
// contain it, so a harness that imported it would break the graft. It did,
// which is how the constraint was found rather than assumed.
//
// So the harness stays self-contained and this package restates the two
// strings. TestTheseNamesMatchTheHarness keeps them from drifting, and it lives
// in THIS package's tests rather than the harness's: the graft copies test
// files too, so a guard on that side would break the released tree the same
// way the import did.
package acctestenv

// The MACs the fleet publishes to the tests. These stay UNIFI_ACC_*: they are
// this provider's own test inputs, not part of the shared herder contract.
const (
	EnvAccDeviceMAC = "UNIFI_ACC_DEVICE_MAC"
	EnvAccAPMAC     = "UNIFI_ACC_AP_MAC"
)
