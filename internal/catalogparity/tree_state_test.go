package catalogparity

import (
	"bytes"
	"encoding/json"
	"testing"
)

// TestReceiptsAcceptTheTreeStateTheShellWrites pins a seam that no other test
// crosses.
//
// .woodpecker/scripts/tree-state.sh measures whether the working tree matches
// the commit a receipt names, and the generators embed its answer as
// "tree_state". Every consumer of those receipts -- catalog-admission,
// catalog-management-contract, catalog-migration-recovery,
// catalog-hardware-disposition, catalog-pragmatic-evidence -- decodes with
// DisallowUnknownFields, so a key the shell writes and the struct does not
// declare is not ignored, it is a hard decode failure.
//
// Both halves were independently correct: the shell recorded something true,
// and strict decoding refuses to silently drop a field it does not understand.
// The composition failed, and nothing saw it, because the unit suite never
// feeds shell output to a Go consumer -- that only happens in the controller
// pipeline, which is down.
//
// Adding a field to any of these receipts without adding it here is the same
// break again, which is what this test exists to make loud.
func TestReceiptsAcceptTheTreeStateTheShellWrites(t *testing.T) {
	// The exact shape evidence_tree_json renders, both ways it can render.
	const clean = `{"status":"clean","commit":"1a2b3c","dirty_paths":[]}`
	const dirty = `{"status":"dirty","commit":"1a2b3c","dirty_paths":[" M a.go","?? b.json"]}`

	for name, state := range map[string]string{"clean": clean, "dirty": dirty} {
		t.Run(name, func(t *testing.T) {
			for receiptName, receipt := range map[string]any{
				"BuildSchemaReceipt":            &BuildSchemaReceipt{},
				"UnitDifferentialReceipt":       &UnitDifferentialReceipt{},
				"ControllerDifferentialReceipt": &ControllerDifferentialReceipt{},
			} {
				document := `{"tree_state":` + state + `}`
				decoder := json.NewDecoder(bytes.NewReader([]byte(document)))
				decoder.DisallowUnknownFields()
				if err := decoder.Decode(receipt); err != nil {
					t.Errorf("%s rejects tree_state: %v", receiptName, err)
				}
			}
		})
	}
}

// TestTreeStateSurvivesTheRoundTrip guards the other direction: a consumer that
// re-emits a receipt must not drop the tree state, or the claim is lost one hop
// downstream rather than at the producer.
func TestTreeStateSurvivesTheRoundTrip(t *testing.T) {
	const document = `{"tree_state":{"status":"dirty","commit":"deadbeef","dirty_paths":[" M x.go"]}}`

	var receipt BuildSchemaReceipt
	decoder := json.NewDecoder(bytes.NewReader([]byte(document)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&receipt); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if receipt.TreeState == nil {
		t.Fatal("tree_state decoded to nil; the receipt cannot report the tree it describes")
	}
	if receipt.TreeState.Status != "dirty" || receipt.TreeState.Commit != "deadbeef" {
		t.Fatalf("tree_state = %#v", receipt.TreeState)
	}
	if len(receipt.TreeState.DirtyPaths) != 1 || receipt.TreeState.DirtyPaths[0] != " M x.go" {
		t.Fatalf("dirty_paths = %#v", receipt.TreeState.DirtyPaths)
	}

	encoded, err := json.Marshal(receipt)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if !bytes.Contains(encoded, []byte(`"tree_state"`)) {
		t.Fatalf("re-encoding dropped tree_state: %s", encoded)
	}

	// And a receipt with no tree state must not grow an empty one, which would
	// read as "clean" to anyone scanning for the key.
	var absent BuildSchemaReceipt
	encoded, err = json.Marshal(absent)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if bytes.Contains(encoded, []byte(`"tree_state"`)) {
		t.Fatalf("a receipt with no tree state emitted one: %s", encoded)
	}
}
