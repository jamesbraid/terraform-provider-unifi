package unifi

// The netflow section is a plain mirror, so cmd/descriptor-emitter emits its
// whole descriptor into setting_netflow_descriptor_gen.go. All eleven of
// settings.Netflow's own fields are modelled; none is omitted.
//
// Netflow's bootstrap fields carry no captured FieldConstraints (the lookup key
// is "SettingNetflow", not the bare "Netflow" the bootstrap walks a document
// under -- see setting_global_switch_descriptor.go's own comment for the
// mechanism), so sampling_mode's OneOf, port/sampling_rate's Between and
// version's OneOf are hand-transcribed in provider-codegen/policy/setting.json
// rather than compiler-derived.
//
// Five of netflow's six Int64PtrFields carry a controller-published pattern;
// checked one at a time against a literal "0":
//   - engine_id: `^$|[1-9][0-9]*` -- rejects "0" (the digit class starts at
//     1-9), needs OmitZero.
//   - export_frequency: no FieldConstraints entry captured at all -- nothing
//     to check, left without OmitZero (absence of a captured pattern is not
//     evidence 0 is safe, just evidence nobody has measured it either way).
//   - port: `HasBounds` 1024-65535 -- 0 is out of range, needs OmitZero.
//   - refresh_rate: no FieldConstraints entry captured -- same as
//     export_frequency.
//   - sampling_rate: `HasBounds` 2-16383 -- 0 is out of range, needs
//     OmitZero.
//   - version: `Int64Values` {5, 9, 10} -- 0 is not a member, needs
//     OmitZero.
//
// server's own pattern (`.{0,252}[^\.]$`) requires at least one non-dot
// character -- confirmed by running the anchored pattern against "" -- so
// unlike teleport.subnet_cidr (whose pattern has its own `|^$` escape hatch) an
// empty server rejects at plan time, and the field wants NullZero rather than
// KeepZero.
