package unifi

// The usw section is a plain mirror, so cmd/descriptor-emitter emits its whole
// descriptor into setting_usw_descriptor_gen.go.
//
// The pinned go-unifi SDK's settings.Usw defines exactly one field of its own,
// dhcp_snoop; everything else on the document is BaseSetting. This is the
// site's legacy "usw" settings key, distinct from the newer global_switch
// document (which carries its own dhcp_snoop among thirteen other fields) --
// the two are separate controller documents and the provider models each from
// its own definition.
