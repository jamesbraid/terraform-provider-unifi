package unifi

// The unifi_dpi_group descriptor is emitted whole into
// dpi_group_descriptor_gen.go: a DPI group gathers DPI application rules under
// one name so the controller can apply them together.
//
// enabled is non-omitempty on the wire and takes a schema default; dpiapp_ids
// is an optional id list; the four attr_* controller internals stay unmapped.
