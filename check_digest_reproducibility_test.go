package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/ubiquiti-community/terraform-provider-unifi/internal/catalogparity"
)

// TestEveryFrozenDigestIsStillReproducible re-derives the digests that committed
// artifacts pin for one another.
//
// SIXTEEN COMMITTED FILES CARRY A DIGEST OF ANOTHER COMMITTED FILE, and until
// this test nothing on the push path re-derived any of them. The comparison that
// would is real -- managementcontract/catalog.go:75 measures the ledger against
// what the contract claims -- but it runs in an event:manual pipeline, so a
// divergence waits for a campaign to be noticed.
//
// IT CHECKS TWO THINGS AND THEY FAIL FOR DIFFERENT REASONS.
//
// Byte identity: sha256 of the artifact equals the value pinned for it. This
// catches the artifact being regenerated while the files pinning it are not.
//
// Round-trip identity: re-marshalling the artifact through the Go type that
// consumes it reproduces those same bytes. This catches the type drifting from
// the artifact -- a renamed json tag, a changed Go type, or FIELD ORDER.
//
// FIELD ORDER IS WHY THIS EXISTS NOW. check_artifact_decoding_test.go records
// that committed artifacts are guarded by their digests, which is true of a new
// field or a changed tag. It is not true of declaration order. Measured, by
// swapping two fields in catalogparity.Ledger -- same names, same tags, same
// types:
//
//	recomputed ledger digest  21753a3392fa6e56...
//	frozen in five wave files 26f2b1013133dcb9...
//	go test ./... -count=1    0 failing packages
//
// json.Marshal emits fields in declaration order, and an embedded struct's
// fields land where the embed is declared. So factoring a shared envelope out of
// these receipts reorders their JSON unless the envelope fields were already
// contiguous -- and 17 types in the receipt layer have them interleaved. This
// check goes in BEFORE that work rather than discovering it afterwards.
func TestEveryFrozenDigestIsStillReproducible(t *testing.T) {
	cases := []struct {
		field     string
		artifact  string
		frozen    string
		pinnedBy  []string
		roundTrip func(raw []byte) (string, error)
	}{
		{
			field:    "ledger_sha256",
			artifact: "provider-codegen/generated/catalog-parity-ledger.json",
			frozen:   "26f2b1013133dcb93485dd63c85085786825293c2c1faaaa5a0a92b287c22a22",
			pinnedBy: []string{
				"build/wave0/catalog-parity.json", "build/wave1/read-surfaces.json",
				"build/wave2/fleet-foundations.json", "build/wave3/fleet-dependent.json",
				"build/wave4/remaining-managed.json",
			},
			roundTrip: func(raw []byte) (string, error) {
				var ledger catalogparity.Ledger
				if err := json.Unmarshal(raw, &ledger); err != nil {
					return "", err
				}
				return marshalDigest(ledger)
			},
		},
		{
			field:    "inventory_sha256",
			artifact: "build/release-ready/catalog-evidence-inventory.json",
			frozen:   "8387b220b82c1e1aaba18e3141c96c181b7ddc1a138bb869b725043484681f31",
			pinnedBy: []string{"provider-codegen/policy/catalog-pragmatic-references.json"},
			roundTrip: func(raw []byte) (string, error) {
				var inventory catalogparity.EvidenceInventory
				if err := json.Unmarshal(raw, &inventory); err != nil {
					return "", err
				}
				return marshalDigest(inventory)
			},
		},
		{
			field:    "fleet_summary_sha256",
			artifact: "build/restricted/catalog-fleet-gap-summary.json",
			frozen:   "12f576f11f670ad4df4fce32fa6b0913faa285a427c08f26d719de7f478ba922",
			pinnedBy: []string{
				"provider-codegen/policy/catalog-pragmatic-references.json",
				"build/restricted/catalog-pragmatic-resolution.json",
			},
			// No round-trip: no consuming type has been shown byte-faithful for
			// this artifact, and asserting one without measuring it is exactly the
			// failure mode this test exists to prevent.
		},
		{
			field:    "baseline_sha256",
			artifact: "build/m0/provider-schema-digests.json",
			frozen:   "1f5aff6f889192dd671e1dfd30fd0cf0e6c1b1e0addfe8664d7f17b516d77946",
			pinnedBy: []string{
				"build/wave0/catalog-parity.json",
				"build/release-ready/catalog-evidence-inventory.json",
				"provider-codegen/generated/catalog-parity-ledger.json",
				"provider-codegen/generated/catalog-surface-contracts.json",
				"provider-codegen/generated/catalog-migration-manifest.json",
			},
			// No round-trip: catalogparity.Baseline is DERIVED from this file and
			// records this very digest as its own SourceSHA256, so re-marshalling
			// the type cannot reproduce the source bytes. Byte identity is the
			// whole available check, and claiming more would be the failure mode
			// this test exists to prevent.
		},
	}

	checked := 0
	for _, c := range cases {
		t.Run(c.field, func(t *testing.T) {
			raw, err := os.ReadFile(c.artifact)
			if err != nil {
				// Fatal, not Skip. A missing artifact is exactly the state in which
				// a digest check silently stops checking anything.
				t.Fatalf("cannot read %s, so %s is unverified: %v", c.artifact, c.field, err)
			}

			sum := sha256.Sum256(raw)
			if got := hex.EncodeToString(sum[:]); got != c.frozen {
				t.Errorf("%s no longer hashes to the %s pinned for it.\n"+
					"    measured: %s\n    frozen:   %s\n"+
					"    pinned by %d file(s):\n        %s\n\n"+
					"    Regenerating the artifact without moving every pin above leaves\n"+
					"    evidence that disagrees with the tree it describes.",
					c.artifact, c.field, got, c.frozen, len(c.pinnedBy),
					strings.Join(c.pinnedBy, "\n        "))
				return
			}

			if c.roundTrip != nil {
				got, err := c.roundTrip(raw)
				if err != nil {
					t.Fatalf("%s does not round-trip through its Go type: %v", c.artifact, err)
				}
				if got != c.frozen {
					t.Errorf("%s no longer re-derives %s through its Go type.\n"+
						"    re-marshalled: %s\n    frozen:        %s\n\n"+
						"    The file is unchanged, so the TYPE moved: a renamed json tag, a\n"+
						"    changed Go type, or a reordered field. json.Marshal emits in\n"+
						"    declaration order, and an embedded struct's fields land where the\n"+
						"    embed is declared -- so this is what an envelope refactor trips.",
						c.artifact, c.field, got, c.frozen)
					return
				}
			}
			checked++
		})
	}

	// Without this the check passes by verifying nothing, which is the state it
	// reaches the moment the table empties or every artifact is renamed.
	if checked == 0 {
		t.Fatal("no frozen digest was verified, so this check proves nothing")
	}
	t.Logf("%d frozen digest(s) re-derived from their committed artifacts", checked)
}

// TestKnownStaleDigestsAreStillStale reports the ledger in both directions.
//
// build/restricted/catalog-pragmatic-resolution.json pins inventory_sha256
// 436f2cd4..., while the inventory hashes to 8387b220... The committed
// resolution describes an inventory that no longer exists.
//
// IT IS NOT A LIVE GATE FAILURE, measured: catalog-controller-differential.yml
// writes the resolution to /tmp and reads it back from /tmp, so the committed
// copy is a record rather than an input and validatePragmaticAdmission never
// sees it. It is recorded because a committed file carrying a digest that no
// longer matches reads as evidence while being none -- and because the day it
// starts matching is a fact worth surfacing rather than silently absorbing.
func TestKnownStaleDigestsAreStillStale(t *testing.T) {
	const (
		carrier = "build/restricted/catalog-pragmatic-resolution.json"
		stale   = "436f2cd4980a9055"
		current = "8387b220b82c1e1a"
	)
	raw, err := os.ReadFile(carrier)
	if err != nil {
		t.Fatalf("cannot read %s: %v", carrier, err)
	}
	var doc struct {
		InventorySHA256 string `json:"inventory_sha256"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("%s does not decode: %v", carrier, err)
	}
	switch {
	case strings.HasPrefix(doc.InventorySHA256, stale):
		t.Logf("%s still pins the stale inventory digest %s..., as recorded", carrier, stale)
	case strings.HasPrefix(doc.InventorySHA256, current):
		t.Errorf("%s now pins the CURRENT inventory digest.\n"+
			"    This ledger entry has served its purpose: delete the entry and this\n"+
			"    test with it. A check that outlives its subject is the thing it was\n"+
			"    written to prevent.", carrier)
	default:
		t.Errorf("%s pins inventory_sha256 %q, which is neither the recorded stale\n"+
			"    value nor the current inventory. Something moved and nothing else\n"+
			"    reported it.", carrier, doc.InventorySHA256)
	}
}

func marshalDigest(v any) (string, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return "", fmt.Errorf("re-marshal: %w", err)
	}
	data = append(data, '\n')
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}
