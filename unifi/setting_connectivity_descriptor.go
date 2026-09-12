package unifi

// The connectivity section is a plain mirror, so cmd/descriptor-emitter emits
// its whole descriptor into setting_connectivity_descriptor_gen.go.
//
// The pinned go-unifi SDK's settings.Connectivity defines seven fields of its
// own; five are modelled. x_mesh_essid and x_mesh_psk are deliberately NOT:
// both are x_-prefixed secret candidates, and every secret this resource
// carries (radius.secret, mgmt.ssh_password, snmp's pair, guest_access's
// eighteen) had its read-echo behaviour pinned by a live probe before its
// AfterReceive shape was chosen -- radius-shaped for a verbatim echo,
// mgmt-shaped for a hash. This dispatch's probe (2026-09-01) wrote only
// connectivity.enabled and measured neither field's echo, so both are omitted
// rather than guessed, the same call global_switch's acl_device_isolation and
// radio_ai's default/useXY made.
