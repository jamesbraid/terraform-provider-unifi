// Package acctestenv names the environment variables the acceptance fleet
// publishes to the tests. It exists to have no dependencies: importing
// internal/controllertest for these two strings would pull testcontainers,
// and through it docker, compose and moby, into every ordinary `go test`.
//
// Duplicated in controllertest on purpose: that package is grafted onto
// released trees for comparison, so importing it here would break older ones.
package acctestenv

// The MACs the fleet publishes to the tests. These stay UNIFI_ACC_*: they
// are this provider's own test inputs, not part of the shared herder contract.
const (
	EnvAccDeviceMAC = "UNIFI_ACC_DEVICE_MAC"
	EnvAccAPMAC     = "UNIFI_ACC_AP_MAC"
)
