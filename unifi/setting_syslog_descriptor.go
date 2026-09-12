package unifi

// The syslog section is a plain mirror, so cmd/descriptor-emitter emits its
// whole descriptor into setting_syslog_descriptor_gen.go. Its specials are the
// #303 omit-not-zero guard on port/netconsole_port, which the emitter derives
// from the SDK's own constraint table, and the controller key --
// settings.Rsyslogd's own GetSettingKey answer is "rsyslogd", not "syslog".
//
// syslog also carries a plan-time rule, separate from this descriptor: the
// controller rejects enabled=true with no ip (api.err.Invalid), enforced by
// settingResource's own ValidateConfig -- see setting_syslog_validate.go.
