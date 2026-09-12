package unifi

// The magic_site_to_site_vpn section is a plain mirror, so
// cmd/descriptor-emitter emits its whole descriptor into
// setting_magic_site_to_site_vpn_descriptor_gen.go.
//
// The dispatch brief for this section assumed settings.MagicSiteToSiteVpn
// carries a controller-generated secret field. It does not: the pinned go-unifi
// SDK's magic_site_to_site_vpn.generated.go declares exactly one field, enabled
// (bool), and nothing else -- confirmed by reading the generated struct
// directly, not by inference. There is therefore no generated-value
// preservation to model here beyond what every other section already gets for
// free: resourcekit's masked write only ever sends the fields the plan names,
// so a future SDK regeneration that adds a real field to this struct would
// surface as a new, unmapped member (caught by go generate's own
// unaccounted-field check) rather than being silently overwritten.
