package unifi

// The unifi_dpi_app descriptor is emitted whole into
// dpi_app_descriptor_gen.go: a DPI application rule matches DPI apps or whole
// categories and blocks or logs them, and does nothing the artifacts do not
// state.
//
// The three bools are non-omitempty on the wire, so their schema defaults keep
// the mask always naming them; the two qos rate caps carry a controller pattern
// that rejects a literal 0, so their fields omit a zero rather than let one
// reach the wire. The four attr_* controller internals stay unmapped.
