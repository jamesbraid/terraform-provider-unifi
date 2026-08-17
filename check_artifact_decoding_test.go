package main

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"

	"github.com/ubiquiti-community/terraform-provider-unifi/internal/catalogparity"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/managementcontract"
)

// TestCommittedArtifactsDecodeStrictly reads every committed artifact the way
// its real consumer reads it: with DisallowUnknownFields, into the type that
// consumer decodes into.
//
// WHAT THIS DOES AND DOES NOT CLOSE, measured rather than assumed, because the
// first version of this comment claimed more than the test delivers.
//
// IT DOES NOT REPRODUCE THE 61732c45 BREAK. That break was the shell writing
// tree_state into a receipt produced INTO /tmp during a pipeline run. Those
// receipts are never committed, so no test can read them and this one cannot
// either. `go build`, `go vet`, `go test ./...` and `go generate` were all green
// across it and they would still be green: the class of runtime-produced
// receipts remains unguarded, and closing it needs a fixture of each receipt
// shape or a schema the producer and consumer share.
//
// FOR COMMITTED ARTIFACTS, MOST ARE ALREADY GUARDED -- BY THEIR DIGESTS, NOT BY
// DECODING. Adding a field to a committed file changes its bytes and trips the
// pinned SHA-256; changing a json tag on the consuming type changes the
// re-marshalled bytes and trips it too, because validateAdmissionInventory
// re-marshals what it parsed. Measured: an unknown field in the inventory fails
// 7 packages, all of them on "inventory SHA-256 does not match measured
// inventory" and none of them on decoding.
//
// SO WHAT THIS ADDS IS TWO ARTIFACTS AND NOT SIX. Measured by adding an unknown
// field to each and counting what else goes red, against the branch's known-red
// baseline:
//
//	catalog-management-contract.json      NOTHING else fails. This test alone.
//	catalog-pragmatic-resolution.json     NOTHING else fails. This test alone.
//	catalog-fleet-gap-summary.json        3 other failures -- already guarded
//	catalog-evidence-inventory.json       7 other failures -- already guarded
//	catalog-campaign.json                 already guarded
//	catalog-pragmatic-references.json     already guarded
//
// The four already-guarded entries stay because the guarding is incidental --
// digests pin bytes, and a file that stops decoding while its bytes are
// unchanged is a real state this names directly.
//
// THE PATTERN THE SEAM BELONGS TO, worth naming because this is its third
// instance: A PROPERTY ENFORCED ON ONE SIDE OF A LANGUAGE BOUNDARY AND ASSUMED
// ON THE OTHER. allowed_skips was bound to the campaign policy only in shell, so
// the Go admission gate let a receipt grant itself skips. The plan's test names
// are deduplicated only by `jq | unique`, so the Go gate counts a padded list as
// full coverage. And strictness is declared in Go and unknown to the shell that
// writes the files.
//
// DECODING INTO THE TYPE THE REAL CONSUMER USES IS THE WHOLE POINT. A guard that
// validated these against a type nobody reads would be the same defect wearing a
// green tick -- it would pass while the consumer still rejected the file. Every
// entry below names the consumer it borrows its type from.
func TestCommittedArtifactsDecodeStrictly(t *testing.T) {
	cases := []struct {
		path     string
		consumer string
		target   func() any
	}{{
		path:     "build/release-ready/catalog-evidence-inventory.json",
		consumer: "cmd/catalog-admission, cmd/catalog-pragmatic-evidence, cmd/catalog-migration-recovery",
		target:   func() any { return new(catalogparity.EvidenceInventory) },
	}, {
		path:     "provider-codegen/policy/catalog-campaign.json",
		consumer: "cmd/catalog-admission, cmd/catalog-pragmatic-evidence, cmd/catalog-migration-recovery",
		target:   func() any { return new(catalogparity.CampaignPolicy) },
	}, {
		path:     "build/restricted/catalog-fleet-gap-summary.json",
		consumer: "cmd/catalog-pragmatic-evidence",
		target:   func() any { return new(catalogparity.FleetReferenceSummary) },
	}, {
		path:     "provider-codegen/policy/catalog-pragmatic-references.json",
		consumer: "cmd/catalog-pragmatic-evidence",
		target:   func() any { return new(catalogparity.PragmaticReferenceSet) },
	}, {
		path:     "provider-codegen/policy/catalog-management-contract.json",
		consumer: "cmd/catalog-management-contract",
		target:   func() any { return new(managementcontract.CatalogManagementPolicy) },
	}, {
		// Committed, stale and read by nothing since both former consumers began
		// re-resolving from inputs. Included anyway: a file that no longer
		// decodes is how you find out it was abandoned rather than maintained.
		path:     "build/restricted/catalog-pragmatic-resolution.json",
		consumer: "cmd/catalog-admission (historically; now re-resolved from inputs)",
		target:   func() any { return new(catalogparity.PragmaticResolution) },
	}}

	checked := 0
	for _, test := range cases {
		data, err := os.ReadFile(test.path)
		if err != nil {
			t.Errorf("%s: %v\n"+
				"    This artifact is committed and %s decodes it. If it has moved, move this\n"+
				"    entry with it; if it has been deleted, delete the consumer's flag too.",
				test.path, err, test.consumer)
			continue
		}
		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(test.target()); err != nil {
			t.Errorf("%s does not decode the way %s reads it:\n    %v\n\n"+
				"    That consumer uses DisallowUnknownFields, so this is a runtime failure in\n"+
				"    the pipeline, not a style question. A producer has started writing a field\n"+
				"    the consuming type does not carry -- teach the type about it, or stop\n"+
				"    writing it. This exact break happened at 61732c45 with tree_state, and the\n"+
				"    whole Go suite was green across it.",
				test.path, test.consumer, err)
			continue
		}
		checked++
	}

	// Every assertion above lives inside the loop, so an empty or fully-erroring
	// case list would report success having decoded nothing.
	if checked != len(cases) {
		t.Errorf("decoded %d of %d committed artifacts; the rest are reported above", checked, len(cases))
	}
	t.Logf("%d committed artifact(s) decoded strictly into their consumers' types", checked)
}
