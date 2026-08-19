package unifi

import (
	"encoding/base64"
	"fmt"

	"golang.org/x/crypto/curve25519"
)

// wireguardPublicKey derives a WireGuard public key from its private key.
//
// THE CONTROLLER NEVER RETURNS ONE. Measured on 10.4.57: a wireguard-server
// created with a private key reports x_wireguard_private_key back on every
// read, at full length, and wireguard_public_key not at all -- absent on
// create, absent after update, absent forever. The provider read it with a
// pointer-to-null helper, so the attribute was null from the first apply and
// stayed null, and nothing errored because Computed plus UseStateForUnknown
// makes a null plan agree with a null read. Consistently empty is still empty.
//
// DERIVING IT IS NOT THE PROVIDER INVENTING A VALUE. A WireGuard public key is
// the X25519 scalar multiplication of the private key with the curve's base
// point -- one private key has exactly one public key, and the controller
// cannot hold a different one without WireGuard failing to work. So this is
// computing a value the controller already implies rather than guessing one it
// has not confirmed.
//
// It works whoever generated the key. The private key is Optional+Computed, so
// the practitioner may leave it to the controller, and the controller returns
// it either way -- which is what makes the public key always derivable.
func wireguardPublicKey(privateKey string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(privateKey)
	if err != nil {
		return "", fmt.Errorf("wireguard private key is not base64: %w", err)
	}
	if len(raw) != curve25519.ScalarSize {
		return "", fmt.Errorf(
			"wireguard private key is %d bytes, want %d", len(raw), curve25519.ScalarSize)
	}
	public, err := curve25519.X25519(raw, curve25519.Basepoint)
	if err != nil {
		return "", fmt.Errorf("deriving the wireguard public key: %w", err)
	}
	return base64.StdEncoding.EncodeToString(public), nil
}
