package unifi

// The device_supervision section is a plain mirror, so cmd/descriptor-emitter
// emits its whole descriptor into setting_device_supervision_descriptor_gen.go.
// All four of settings.DeviceSupervision's own fields are modelled.
//
// Each of the three seconds fields carries a controller-published pattern in
// settings.FieldConstraints["SettingDeviceSupervision"]; checked one at a
// time against a literal "0", every one rejects it (their ranges start at 60,
// 60 and 300), so all three Int64PtrFields carry OmitZero -- pinned by this
// section's own OmitZeroProblems test. The compiler derives no validator from
// a bounded range, so each field's Between is hand-transcribed in
// provider-codegen/policy/setting.json from the same table's Min/Max.
