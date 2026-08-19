package unifi

import (
	"encoding/base64"
	"encoding/hex"
	"testing"

	"golang.org/x/crypto/curve25519"
)

// TestWireguardPublicKeyMatchesRFC7748 checks the derivation against an
// EXTERNAL known answer rather than against itself.
//
// RFC 7748 section 6.1 publishes Alice's X25519 keypair. WireGuard keys are the
// same primitive in base64 rather than hex, so the vector transfers directly.
// Without this the derivation could be self-consistent and wrong -- a different
// base point, or a missing clamp, produces stable nonsense.
func TestWireguardPublicKeyMatchesRFC7748(t *testing.T) {
	privateHex := "77076d0a7318a57d3c16c17251b26645df4c2f87ebc0992ab177fba51db92c2a"
	publicHex := "8520f0098930a754748b7ddcb43ef75a0dbf3a0d26381af4eba4a98eaa9b4e6a"

	privateRaw, err := hex.DecodeString(privateHex)
	if err != nil {
		t.Fatal(err)
	}
	publicRaw, err := hex.DecodeString(publicHex)
	if err != nil {
		t.Fatal(err)
	}

	got, err := wireguardPublicKey(base64.StdEncoding.EncodeToString(privateRaw))
	if err != nil {
		t.Fatalf("deriving: %v", err)
	}
	if want := base64.StdEncoding.EncodeToString(publicRaw); got != want {
		t.Errorf("derived %q, RFC 7748 says %q", got, want)
	}
}

// TestWireguardPublicKeyAgreesOnASharedSecret is the property that makes the
// derived key USABLE rather than merely well-formed: two peers holding each
// other's derived public keys must reach the same shared secret, which is the
// whole of what WireGuard does with them. A derivation that produced plausible
// bytes but not the matching point would pass a format check and fail here.
func TestWireguardPublicKeyAgreesOnASharedSecret(t *testing.T) {
	alicePrivate := "WPiBa/Ak1W+8Sp8L5yvbyhHeRO2o5kJvihq2VtJ+kFg="
	bobPrivate := "iCwGiLNjBcJ8vFVKWmNjOWyMQFVQFVQFVQFVQFVQFUE="

	alicePublic, err := wireguardPublicKey(alicePrivate)
	if err != nil {
		t.Fatalf("alice: %v", err)
	}
	bobPublic, err := wireguardPublicKey(bobPrivate)
	if err != nil {
		t.Fatalf("bob: %v", err)
	}
	if alicePublic == bobPublic {
		t.Fatal("two different private keys derived the same public key; the derivation " +
			"is ignoring its input and every other assertion here is vacuous")
	}

	shared := func(privB64, pubB64 string) []byte {
		priv, err := base64.StdEncoding.DecodeString(privB64)
		if err != nil {
			t.Fatal(err)
		}
		pub, err := base64.StdEncoding.DecodeString(pubB64)
		if err != nil {
			t.Fatal(err)
		}
		out, err := curve25519.X25519(priv, pub)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	if a, b := shared(alicePrivate, bobPublic), shared(bobPrivate, alicePublic); string(a) != string(b) {
		t.Error("the two peers did not agree on a shared secret, so at least one derived " +
			"public key is not the point matching its private key")
	}
}

// TestWireguardPublicKeyRefusesMalformedInput keeps the failure visible. The
// caller uses this on a value the controller supplied, and a silent empty
// string there would restore exactly the null-forever behaviour it replaces.
func TestWireguardPublicKeyRefusesMalformedInput(t *testing.T) {
	for _, testCase := range []struct{ name, key, wantIn string }{
		{"not base64", "!!!!not base64!!!!", "not base64"},
		{"wrong length", base64.StdEncoding.EncodeToString([]byte("short")), "want 32"},
		{"empty", "", "want 32"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			got, err := wireguardPublicKey(testCase.key)
			if err == nil {
				t.Fatalf("accepted %q and returned %q", testCase.key, got)
			}
			if got != "" {
				t.Errorf("returned %q alongside an error", got)
			}
		})
	}
}
