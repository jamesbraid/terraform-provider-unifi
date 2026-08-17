package upgradeplan

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestBuildReceiptDerivesItsVerdictFromTheRun(t *testing.T) {
	base := Run{
		ReleasedRef: "v0.101.2",
		Fixture:     Fixture{Files: 3, Expected: 0},
		ControlExit: 0,
		SubjectExit: 0,
	}
	if got := BuildReceipt(base); got.Result != "pass" || got.ExpectedControlExit != 0 {
		t.Fatalf("a clean run produced %+v", got)
	}

	regressed := base
	regressed.SubjectExit = 2
	got := BuildReceipt(regressed)
	if got.Result != "fail" {
		t.Fatalf("a candidate that wants changes produced %q", got.Result)
	}
	if !strings.Contains(got.Verdict, "regression") {
		t.Fatalf("verdict %q does not say what happened", got.Verdict)
	}
	// The receipt records the three numbers the verdict came from, so a reader
	// can check the derivation rather than trust it.
	if got.ControlExit != 0 || got.SubjectExit != 2 || got.ExpectedControlExit != 0 {
		t.Fatalf("the receipt does not carry the numbers its verdict came from: %+v", got)
	}
}

// TestTheReceiptUsesTheSameTreeStateKeyAsEveryOtherOne. The shell called it
// "tree", which is the spelling that made the dependency publishability receipt
// undecodable by its own consumer.
func TestTheReceiptUsesTheSameTreeStateKeyAsEveryOtherOne(t *testing.T) {
	encoded, err := json.Marshal(BuildReceipt(Run{Fixture: Fixture{Files: 1}}))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), `"tree":`) {
		t.Fatalf("the receipt still uses the odd key: %s", encoded)
	}
	// Absent rather than empty when nothing measured one, matching the other
	// receipts: an empty object reads as "clean" to anyone scanning for the key.
	if strings.Contains(string(encoded), "tree_state") {
		t.Fatalf("an unmeasured tree state was emitted anyway: %s", encoded)
	}
}

// TestAComparisonOfAProgramWithItselfIsRefused. dev_overrides silently uses
// whatever binary sits in the directory it names, so a stale one is
// indistinguishable from a fresh one -- and a green run where both directories
// hold the same bytes means only that a program agrees with itself.
func TestAComparisonOfAProgramWithItselfIsRefused(t *testing.T) {
	same := strings.Repeat("a", 64)
	if err := TwoProvidersAreDistinct(same, same); err == nil {
		t.Fatal("two byte-identical providers were accepted as a comparison")
	}
	if err := TwoProvidersAreDistinct("", same); err == nil {
		t.Fatal("an undigested provider was accepted, so nothing established that the run " +
			"compared two different programs")
	}
	if err := TwoProvidersAreDistinct(same, strings.Repeat("b", 64)); err != nil {
		t.Fatalf("two distinct providers were refused: %v", err)
	}
}
