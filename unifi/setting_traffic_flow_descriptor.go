package unifi

// The traffic_flow section is a plain mirror, so cmd/descriptor-emitter emits
// its whole descriptor into setting_traffic_flow_descriptor_gen.go.
//
// All four of settings.TrafficFlow's members are plain, non-omitempty bools --
// the same force-emitted shape ips's
// content_filtering_blocking_page_enabled/honeypot_enabled/memory_optimized/
// restrict_torrents already have, so none of them carries an Elide (see
// resourcekit.BoolField's own comment: a false is a value, not an absence).
