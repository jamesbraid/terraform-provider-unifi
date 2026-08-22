package unifi

import (
	"encoding/json"

	"github.com/ubiquiti-community/go-unifi/unifi"
)

// networkMaskFor is the shared half, used by unifi_network and unifi_wan.
//
// Both are Network-backed and both need the same rule: a mask may name only
// what this object's purpose actually encodes. Keeping one implementation means
// the two cannot diverge on the question that decides whether go-unifi accepts
// the write.
func networkMaskFor(managed []string, network *unifi.Network) []string {
	raw, err := json.Marshal(network)
	if err != nil {
		// An object that cannot encode has a bigger problem than its mask, and
		// the write will report it.
		return nil
	}
	var encoded map[string]json.RawMessage
	if err := json.Unmarshal(raw, &encoded); err != nil {
		return nil
	}
	mask := make([]string, 0, len(managed))
	for _, name := range managed {
		if _, carried := encoded[name]; carried {
			mask = append(mask, name)
		}
	}
	return mask
}
