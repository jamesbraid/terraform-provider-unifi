package unifi

// The global_ap section is a plain mirror, so cmd/descriptor-emitter emits its
// whole descriptor into setting_global_ap_descriptor_gen.go.
//
// The pinned go-unifi SDK's settings.GlobalAp defines ten fields of its own;
// seven are modelled. The 6 GHz trio (6e_channel_size, 6e_tx_power,
// 6e_tx_power_mode) is deliberately NOT: each wire name begins with a digit,
// which no Terraform attribute name may, so exposing them would mean inventing
// a rename the controller's definition doesn't carry -- omitted rather than
// renamed, recorded in provider-codegen/policy/setting.json's omitted list.
//
// Field constraints here ARE compiler-derived: the channel-size OneOfs and the
// tx-power-mode OneOfs come straight from
// settings.FieldConstraints["SettingGlobalAp"]. The two hand-transcribed
// validators in policy/setting.json are the kinds the compiler does not
// derive: ap_exclusions' per-element MAC pattern (a list) and the tx powers'
// Between (a bounded range).
