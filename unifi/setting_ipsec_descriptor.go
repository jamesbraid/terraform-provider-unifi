package unifi

// The ipsec section is a plain mirror, so cmd/descriptor-emitter emits its
// whole descriptor into setting_ipsec_descriptor_gen.go.
//
// settings.Ipsec is hand-maintained in the SDK rather than generated from the
// locked field spec (the controller exposes it at
// /api/s/<site>/{get,set}/setting/ipsec on newer releases, ahead of the spec
// capture) -- see its own doc comment. It is still one of the settings
// GetSettingKey recognises, so it derives from the controller the same way
// every generated section does.
