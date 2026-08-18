package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"testing"

	"github.com/ubiquiti-community/terraform-provider-unifi/internal/catalogparity"
)

// TestEveryFrozenDigestIsStillReproducible re-derives the digests that committed
// artifacts pin, from the types that produce them.
//
// THE GAP IT CLOSES IS NARROWER THAN IT LOOKS AND WORTH STATING EXACTLY.
// check_artifact_decoding_test.go already reads every committed artifact
// strictly, and its comment records that committed files are additionally
// "guarded by their digests" -- add a field or change a json tag and the pinned
// SHA-256 stops matching. That is true. It is not true of FIELD ORDER.
//
// MEASURED, by swapping two fields in catalogparity.Ledger -- same names, same
// tags, same types, declaration order only:
//
//	recomputed ledger digest  21753a3392fa6e56...
//	frozen in five wave files 26f2b1013133dcb9...
//	go test ./...             0 failing packages
//
// The digest moves and nothing on the push path notices, because the comparison
// that would notice runs in an event:manual pipeline. That is #153's shape: a
// check that is load-bearing and structurally invisible to the only trigger that
// runs before a merge.
//
// WHY THIS IS ABOUT TO MATTER. json.Marshal emits fields in declaration order,
// and an embedded struct's fields land where the embed is declared, so factoring
// a shared envelope out of these receipts reorders their JSON unless the envelope
// fields were already contiguous. Across the receipt layer 17 types have them
// interleaved. Consolidating those types is a size change that silently moves
// every digest derived from them -- which is why this check goes in FIRST, before
// any envelope work, rather than being the thing that discovers the breakage
// afterwards.
func TestEveryFrozenDigestIsStillReproducible(t *testing.T) {
	// Each case names a committed artifact, the digest frozen elsewhere for it,
	// and how that digest is derived. Deriving it HERE rather than calling the
	// producer is deliberate: the producers are unexported, and a check that
	// called them would pass even if the production path stopped using them.
	cases := []struct {
		name     string
		artifact string
		frozen   string
		frozenIn string
		derive   func(t *testing.T, raw []byte) string
	}{
		{
			name:     "catalog parity ledger",
			artifact: "provider-codegen/generated/catalog-parity-ledger.json",
			frozen:   "26f2b1013133dcb93485dd63c85085786825293c2c1faaaa5a0a92b287c22a22",
			frozenIn: "build/wave0..wave4/*.json as ledger_sha256",
			derive: func(t *testing.T, raw []byte) string {
				var ledger catalogparity.Ledger
				if err := json.Unmarshal(raw, &ledger); err != nil {
					t.Fatalf("the artifact does not decode into catalogparity.Ledger: %v", err)
				}
				// canonicalFileDigest: marshal, append newline, sha256.
				data, err := json.Marshal(ledger)
				if err != nil {
					t.Fatalf("re-marshalling the ledger failed: %v", err)
				}
				data = append(data, '\n')
				sum := sha256.Sum256(data)
				return hex.EncodeToString(sum[:])
			},
		},
	}

	verified := 0
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			raw, err := os.ReadFile(c.artifact)
			if err != nil {
				// Read failure is a failure, not a skip. A missing artifact is
				// exactly the state where a digest check quietly stops checking.
				t.Fatalf("cannot read %s, so its frozen digest is unverified: %v", c.artifact, err)
			}
			got := c.derive(t, raw)
			if got != c.frozen {
				t.Errorf("%s no longer re-derives the digest pinned for it.\n"+
					"    recomputed: %s\n"+
					"    frozen:     %s\n"+
					"    frozen in:  %s\n\n"+
					"    Nothing else on the push path catches this. If the change was\n"+
					"    deliberate -- a field reordered, a type embedded -- every pinned\n"+
					"    copy above has to move with it, in the same commit. If it was not\n"+
					"    deliberate, the artifact and the type have diverged.",
					c.artifact, got, c.frozen, c.frozenIn)
				return
			}
			verified++
		})
	}

	// Without this the check passes by verifying nothing, which is the state it
	// reaches the moment the cases table is emptied or every artifact moves.
	if verified == 0 {
		t.Fatal("no frozen digest was verified, so this check proves nothing")
	}
	t.Logf("%d frozen digest(s) re-derived from their committed artifacts", verified)
}
