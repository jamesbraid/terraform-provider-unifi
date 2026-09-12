package unifi

// The lcm section is a plain mirror, so cmd/descriptor-emitter emits its whole
// descriptor into setting_lcm_descriptor_gen.go. Its only special is the #288
// omit-not-zero guard on brightness/idle_timeout, which the emitter derives
// from the SDK's own constraint table.
