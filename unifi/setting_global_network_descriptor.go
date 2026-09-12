package unifi

// The global_network section is a plain mirror, so cmd/descriptor-emitter emits
// its whole descriptor into setting_global_network_descriptor_gen.go.
//
// settings.GlobalNetwork is hand-maintained in the SDK rather than generated
// from the locked field spec (the controller exposes it at
// /api/s/<site>/{get,set}/setting/global_network on newer releases, ahead of
// the spec capture) -- see its own doc comment. It is still one of the settings
// GetSettingKey recognises, so it derives from the controller the same way
// every generated section does.
