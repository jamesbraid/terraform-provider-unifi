package schemaparity

import "testing"

// Each assertion gets a case that fails when it is removed, and a control that
// passes. They are separate tests rather than one table because the risk this
// port carries is a check being dropped while its neighbours still pass -- a
// single table asserting "some finding was produced" would not notice.

func TestParityAcceptsDeclaredAndRefusesUndeclared(t *testing.T) {
	released := mustParse(t, releasedFixture)
	candidate := mustParse(t, candidateFixture)

	if f := CheckParity("terraform", released, candidate, realLedger()); len(f) != 0 {
		t.Fatalf("the six declared differences were refused: %v", f)
	}
	short := realLedger()
	short.Entries = short.Entries[:5]
	f := CheckParity("terraform", released, candidate, short)
	if len(f) != 1 || len(f[0].Differs) != 1 {
		t.Fatalf("dropping one entry gave %v, want exactly one finding naming one difference", f)
	}
	if f[0].Differs[0].Attribute != "network_id" {
		t.Fatalf("undeclared difference is %v, want network_id", f[0].Differs[0])
	}
}

func TestParityRefusesAStaleDeclaration(t *testing.T) {
	ledger := realLedger()
	ledger.Entries = append(ledger.Entries, Entry{
		Surface: "unifi_wlan", Attribute: "name", Field: "required", Old: "true", New: "",
		WhyNotBreaking: "never made", Evidence: []Evidence{{Claim: "c", Kind: Inferred}},
	})
	f := CheckParity("terraform", mustParse(t, releasedFixture), mustParse(t, candidateFixture), ledger)
	if len(f) != 1 || !contains(f[0].Detail, "license nothing") {
		t.Fatalf("findings = %v, want one naming the entry that matches nothing", f)
	}
}

func TestDeterminismKeepsByteIdentity(t *testing.T) {
	if f := CheckDeterminism("candidate", "aaa", "aaa"); len(f) != 0 {
		t.Fatalf("identical builds produced %v", f)
	}
	f := CheckDeterminism("candidate", "aaa", "bbb")
	if len(f) != 1 || !contains(f[0].Detail, "built twice") {
		t.Fatalf("differing builds gave %v, want a determinism finding", f)
	}
}

func TestProvenanceKeepsByteIdentity(t *testing.T) {
	if f := CheckProvenance("released_commit", "abc", "abc"); len(f) != 0 {
		t.Fatalf("matching provenance produced %v", f)
	}
	if f := CheckProvenance("released_commit", "abc", "def"); len(f) != 1 {
		t.Fatalf("mismatched provenance gave %v, want one finding", f)
	}
}

func TestCrossCLIKeepsByteIdentity(t *testing.T) {
	shared := mustParse(t, releasedFixture)
	if f := CheckCrossCLI("candidate", shared, shared); len(f) != 0 {
		t.Fatalf("agreeing CLIs produced %v", f)
	}
	f := CheckCrossCLI("candidate", shared, mustParse(t, candidateFixture))
	if len(f) != 1 || len(f[0].Differs) != 6 {
		t.Fatalf("disagreeing CLIs gave %v, want one finding naming all six leaves", f)
	}
}

// TestInvertedControlFailsOnEquality is the case a careless port turns into a
// pass. The assertion fires when the two projections MATCH.
func TestInvertedControlFailsOnEquality(t *testing.T) {
	same := mustParse(t, releasedFixture)
	f := CheckProjectionsDiffer(same, same)
	if len(f) != 1 || !contains(f[0].Detail, "SAME full projection") {
		t.Fatalf("identical projections gave %v, want the inverted control to fire", f)
	}
	if f := CheckProjectionsDiffer(same, mustParse(t, candidateFixture)); len(f) != 0 {
		t.Fatalf("differing projections produced %v, want silence", f)
	}
}

func TestShapeCountsAndTofuOmissions(t *testing.T) {
	tf := map[string]any{
		"action_schemas":        map[string]any{"a": 1},
		"list_resource_schemas": map[string]any{"a": 1, "b": 2},
	}
	tofu := map[string]any{}
	if f := CheckShape(tf, tofu, 1, 2); len(f) != 0 {
		t.Fatalf("correct shape produced %v", f)
	}
	if f := CheckShape(tf, tofu, 1, 25); len(f) != 1 {
		t.Fatalf("wrong list count gave %v, want one finding", f)
	}
	if f := CheckShape(tf, map[string]any{"action_schemas": map[string]any{}}, 1, 2); len(f) != 1 ||
		!contains(f[0].Detail, "inverted control") {
		t.Fatalf("tofu reporting an unexpected key gave %v", f)
	}
}
