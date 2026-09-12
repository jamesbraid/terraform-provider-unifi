package unifi

// The teleport section is a plain mirror, so cmd/descriptor-emitter emits its
// whole descriptor into setting_teleport_descriptor_gen.go.
//
// enabled and subnet_cidr are only weakly coupled: settings.Teleport's own wire
// pattern for subnet_cidr already tolerates an empty string regardless of
// enabled (`^(...)\/(...)$|^$`, confirmed by running the derived pattern
// against ""), so there is no "enabled requires subnet_cidr" or "subnet_cidr
// requires enabled" wire-level rule to enforce, and no schema-level
// AlsoRequires is added on top of it.
