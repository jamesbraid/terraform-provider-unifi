package catalogparity

import "github.com/ubiquiti-community/terraform-provider-unifi/internal/receiptcheck"

// EnvelopeView hands the shared checker this receipt's envelope fields.
//
// A VIEW RATHER THAN AN EMBED, established the expensive way. Embedding a shared
// struct was built, migrated and reverted: measured across all 22 receipt types,
// no envelope field is present on every one, so there is no omitempty choice
// that preserves every receipt's serialisation. TestDroppingAnyReceiptKeyIsVisible
// caught it -- released_commit joined the set of keys whose loss a round trip
// cannot see.
//
// So the receipt keeps its own fields and its own exact wire contract. Only the
// rules are shared.
func (r ControllerDifferentialReceipt) EnvelopeView() receiptcheck.Envelope {
	view := receiptcheck.Envelope{
		FormatVersion:  r.FormatVersion,
		Gate:           r.Gate,
		Result:         r.Result,
		ReleasedCommit: r.ReleasedCommit,
	}
	if r.TreeState != nil {
		view.HasTreeState = true
		view.TreeStatus = r.TreeState.Status
	}
	return view
}
