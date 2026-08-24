package unifi

import (
	"encoding/base64"
	"fmt"

	"golang.org/x/crypto/curve25519"
)

// wireguardPublicKey derives a WireGuard public key from its private key --
// the controller never returns one, and X25519 gives exactly one per key.
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
