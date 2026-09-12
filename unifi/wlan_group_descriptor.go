package unifi

// The unifi_wlan_group descriptor is emitted whole into
// wlan_group_descriptor_gen.go, and it is the smallest in the tree. The SDK
// struct carries one practitioner-facing field -- name -- and the four attr_*
// controller internals stay unmapped, so the derived mask can never offer them
// back on a write.
