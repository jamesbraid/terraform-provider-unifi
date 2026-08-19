package receiptcheck

// FormatVersion is the receipt format every producer writes and every consumer
// expects.
//
// IT IS A CONSTANT BECAUSE IT WAS FIFTY LITERALS. `.FormatVersion != 1` appears
// 43 times in non-test source and 50 including tests, across 21 files, and NOT
// ONE of them compares against a named value. They agree, and nothing makes them
// agree -- when the format moves to 2 someone has to find all fifty, and the one
// they miss fails open rather than closed.
//
// A consistency you cannot enforce is a coincidence you are relying on.
const FormatVersion = 1

// Envelope is the part of a receipt that every receipt has some of.
//
// IT IS A VIEW, NOT A WIRE TYPE, and that distinction was established the
// expensive way. The first design embedded a shared struct in each receipt.
// Measured across all 22 receipt types, NO envelope field is present on every
// one: released_commit is always-emitted on some and absent from others, so
// there is no omitempty choice that preserves both populations. Embedding it
// therefore cannot happen without changing what those receipts serialise, and a
// test in catalogparity caught exactly that -- released_commit joining the set
// of keys whose loss a round trip cannot see.
//
// So the receipts keep their own fields and their own exact wire contracts, and
// hand a view of them here. The types do not merge. The RULES do.
//
// IT HOLDS ONLY PRIMITIVES so that this package stays free of the domain
// packages that import it. TreeState is reduced to a status and a presence flag
// rather than referenced, which is what keeps catalogparity able to import
// receiptcheck rather than the other way round.
type Envelope struct {
	FormatVersion   int
	Gate            string
	Result          string
	TreeStatus      string
	HasTreeState    bool
	ProviderAddress string
	SourceCommit    string
	ReleasedCommit  string
}

// EnvelopeExpectation says what a particular receipt owes.
//
// PER-RECEIPT ON PURPOSE. The receipts genuinely differ in which envelope fields
// they carry -- ReleaseReadyReceipt has no tree_state at all -- so requiring one
// uniformly would fail a receipt nothing has ever produced with that field. The
// rules are uniform in FORM, one definition and one runner, and honest about
// which receipt owes which field.
type EnvelopeExpectation struct {
	Gate            string
	Result          string
	ProviderAddress string
	RequireTree     bool
	RequireSource   bool
	RequireReleased bool
}

// EnvelopeRules returns the rules for one receipt's envelope.
//
// This is the single definition that replaces 138 scattered comparisons across
// seven fields. A receipt that adopts it cannot quietly disagree with the others
// about what a valid envelope is, because there is no longer anywhere to
// disagree.
func EnvelopeRules(envelope Envelope, expect EnvelopeExpectation) []Rule {
	rules := []Rule{
		Custom(envelope.FormatVersion == FormatVersion,
			"format_version is %d, want %d", envelope.FormatVersion, FormatVersion),
		Equals("gate", envelope.Gate, expect.Gate),
	}
	if expect.Result != "" {
		rules = append(rules, Equals("result", envelope.Result, expect.Result))
	}
	if expect.ProviderAddress != "" {
		rules = append(rules, Equals("provider_address", envelope.ProviderAddress, expect.ProviderAddress))
	}
	if expect.RequireTree {
		rules = append(rules,
			Custom(envelope.HasTreeState,
				"tree_state is absent, so the receipt does not say which tree it describes"),
			Custom(!envelope.HasTreeState || envelope.TreeStatus == "clean",
				"tree_state.status is %q; evidence must describe a committed tree", envelope.TreeStatus))
	}
	if expect.RequireSource {
		rules = append(rules, Hex("source_commit", envelope.SourceCommit, 40))
	}
	if expect.RequireReleased {
		rules = append(rules, Hex("released_commit", envelope.ReleasedCommit, 40))
	}
	return rules
}
