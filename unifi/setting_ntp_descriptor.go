package unifi

// The ntp section is a plain mirror, so cmd/descriptor-emitter emits its whole
// descriptor into setting_ntp_descriptor_gen.go. Its only special is what it
// does NOT do: an unset NTP server slot round-trips as a known "", never
// null.
