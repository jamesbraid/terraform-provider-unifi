package resourcekit

import "strings"

// sdkPayloadMarker is the literal separator go-unifi's client
// (jamesbraid/go-unifi's unifi.go, doRequest) appends before the raw request
// body it just sent, on every non-2xx response:
// "%w (%s) for %s %s\npayload: %s". The SDK redacts that payload by matching
// wire names against its own fixed substring list (private_key, passphrase,
// pre_shared_key, password, secret, psk) before printing it -- a guess this
// provider's schema is never consulted for. A wire name outside that list
// (eleven of guest_access's eighteen Sensitive fields, and snmp's own
// community, both measured against a real 400) prints its value verbatim.
const sdkPayloadMarker = "\npayload: "

// diagErrorText renders err for a diagnostic with any SDK request-payload
// tail dropped. This is the one place every kit-built resource's write,
// read, list and delete errors become diagnostic text (resource.go, list.go,
// spec_section.go all route their err.Error() through here), so fixing the
// payload leak here fixes every section built on this package at once,
// present and future, without a per-field substring list that the next wire
// name can miss again. Keeping the payload text was only ever a debugging
// convenience; dropping it is the safe default until a caller has a reason
// (and a schema-driven redaction, not another guess) to keep it.
func diagErrorText(err error) string {
	msg := err.Error()
	if i := strings.Index(msg, sdkPayloadMarker); i >= 0 {
		msg = msg[:i]
	}
	return msg
}
