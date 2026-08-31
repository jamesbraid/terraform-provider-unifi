package resourcekit

import "strings"

// sdkPayloadMarker is the literal separator go-unifi's client
// (jamesbraid/go-unifi's unifi.go, doRequest) appends before the raw request
// body it just sent, on every non-2xx response:
// "%w (%s) for %s %s\npayload: %s". The SDK redacts that payload by matching
// wire names against its own fixed substring list (private_key, passphrase,
// pre_shared_key, password, secret, psk) before printing it -- a guess this
// provider's schema is never consulted for. The controller's own
// sensitive_metadata.json declares 64 sensitive fields; those six substrings
// match 13 of them. wireguard_client_preshared_key misses pre_shared_key by a
// single underscore.
const sdkPayloadMarker = "\npayload: "

// DiagErrorText renders err for a Terraform diagnostic with any SDK
// request-payload tail dropped. Every path in this provider that turns an SDK
// error into diagnostic text goes through here, so a wire name the SDK's
// substring list misses cannot reach the practitioner's terminal or CI log.
//
// Dropping the payload outright, rather than redacting it against a list,
// is deliberate: any list is a prediction about field names, and the failure
// mode when the prediction is wrong is a secret printed in full with no
// error raised. The payload was only ever a debugging convenience.
func DiagErrorText(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	if i := strings.Index(msg, sdkPayloadMarker); i >= 0 {
		msg = msg[:i]
	}
	return msg
}
