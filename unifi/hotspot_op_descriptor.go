package unifi

// The unifi_hotspot_op descriptor is emitted whole into
// hotspot_op_descriptor_gen.go: a hotspot operator is a guest-portal login
// account.
//
// name and the password are required; note is optional. The controller calls
// the password x_password on the wire, which only the wire-name check would
// catch if this used the Terraform spelling. The four attr_* controller
// internals stay unmapped.
