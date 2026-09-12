package unifi

// The igmp_snooping section is a plain mirror, so cmd/descriptor-emitter emits
// its whole descriptor into setting_igmp_snooping_descriptor_gen.go. Two of
// settings.IgmpSnooping's fifteen fields are modelled.
//
// The other thirteen (querier mode, switches, flood options -- advanced UI-only
// settings) this schema never modelled. UpdateSettingFields' field mask is what
// keeps them: naming only "enabled" and "network_ids" on the wire leaves every
// other stored field untouched server-side, which
// TestIgmpSnoopingSpecMasksOnlyEnabled pins.
//
// Controller fact, not a provider behaviour (measured on 10.4.57): enabled only
// sticks when network_ids names at least one network. A plan that sets
// enabled=true with an empty network_ids reads back false. This descriptor
// sends exactly what the plan configures either way; the controller's own
// conditional acceptance is exercised by TestAccSettingResource_igmpSnooping,
// not asserted here.
