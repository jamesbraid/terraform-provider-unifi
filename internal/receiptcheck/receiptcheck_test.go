package receiptcheck

import (
	"strings"
	"testing"
)

// TestHexCarriesItsLength is the test this package exists for.
//
// The consolidation it enables merges 36 sites that all read `!validHex(FIELD,
// NUM)` into one primitive. Four of those pass 40 -- a commit SHA -- and two
// pass 64, a sha256. If the shared primitive ever stops using its length
// argument, both groups keep working on their own valid inputs and every
// existing mutation still fails, because those mutations use values like "HEAD"
// that are refused at any width.
//
// SO THE MUTATION HAS TO BE THE OPERAND ITSELF: a well-formed value of the WRONG
// width, correct in alphabet and wrong only in the one respect the operand
// governs. Nothing else distinguishes a dropped length from a carried one.
func TestHexCarriesItsLength(t *testing.T) {
	commit := strings.Repeat("b", 40)
	digest := strings.Repeat("a", 64)

	if err := Run("g", Hex("provider_commit", digest, 40)); err == nil {
		t.Error("a 64-character digest was accepted in a 40-character commit slot; " +
			"the length operand is not being used")
	}
	if err := Run("g", Hex("module_zip_sha256", commit, 64)); err == nil {
		t.Error("a 40-character commit was accepted in a 64-character digest slot; " +
			"the length operand is not being used")
	}
	// Controls: without these the test above passes if Hex refuses everything.
	if err := Run("g", Hex("provider_commit", commit, 40)); err != nil {
		t.Errorf("a valid 40-character commit was refused: %v", err)
	}
	if err := Run("g", Hex("module_zip_sha256", digest, 64)); err != nil {
		t.Errorf("a valid 64-character digest was refused: %v", err)
	}
	// Right width, wrong alphabet. The shell checked ${#digest} alone and this
	// is the case that got through it.
	if err := Run("g", Hex("module_zip_sha256", strings.Repeat("z", 64), 64)); err == nil {
		t.Error("64 non-hex characters were accepted; length alone is not the rule")
	}
}

// TestEqualsCarriesItsWant is the same property for the commonest rule of all.
// One shape, `FIELD != STR`, spans 26 distinct expected literals across the
// corpus; a primitive that dropped `want` would accept all of them everywhere.
func TestEqualsCarriesItsWant(t *testing.T) {
	if err := Run("g", Equals("result", "pass", "pass")); err != nil {
		t.Errorf("the wanted value was refused: %v", err)
	}
	if err := Run("g", Equals("result", "blocked_evidence", "pass")); err == nil {
		t.Error("a different literal was accepted; the want operand is not being used")
	}
	// Two rules wanting DIFFERENT literals must not agree with each other.
	if err := Run("g", Equals("result", "pass", "fail")); err == nil {
		t.Error("a rule wanting \"fail\" accepted \"pass\"")
	}
}

// TestRunWithNoRulesIsAnError covers the vacuity case directly. A gate that
// asserts nothing is the defect this apparatus keeps finding, and a shared
// runner is exactly where it would arrive next: delete the rules and the gate
// still returns nil, reading as a pass.
func TestRunWithNoRulesIsAnError(t *testing.T) {
	err := Run("some-gate")
	if err == nil {
		t.Fatal("a gate with no rules returned nil, so it passes everything")
	}
	if !strings.Contains(err.Error(), "asserts nothing") {
		t.Errorf("the error does not say what is wrong: %v", err)
	}
}

// TestRunReportsEveryFailure covers why it returns a list. These gates run in
// event:manual pipelines, so one failure per run costs a campaign per fix.
func TestRunReportsEveryFailure(t *testing.T) {
	err := Run("g",
		Equals("a", "x", "y"),
		Equals("b", "x", "y"),
		Equals("c", "x", "x"),
		NotEmpty("d", ""),
	)
	if err == nil {
		t.Fatal("three broken rules were accepted")
	}
	for _, want := range []string{"a is", "b is", "d is", "3 assertion(s)"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the report omits %q:\n%v", want, err)
		}
	}
	if strings.Contains(err.Error(), "c is") {
		t.Errorf("the report names a rule that held:\n%v", err)
	}
}

func TestRemainingPrimitives(t *testing.T) {
	for _, c := range []struct {
		name  string
		rule  Rule
		holds bool
	}{
		{"NotEmpty accepts a value", NotEmpty("f", "x"), true},
		{"NotEmpty refuses empty", NotEmpty("f", ""), false},
		{"IsFalse accepts false", IsFalse("f", false, "why"), true},
		{"IsFalse refuses true", IsFalse("f", true, "why"), false},
		{"Count accepts the wanted count", Count("f", 3, 3), true},
		{"Count carries its want", Count("f", 3, 4), false},
		{"Differ accepts different values", Differ("a", "1", "b", "2", "why"), true},
		{"Differ refuses equal values", Differ("a", "1", "b", "1", "why"), false},
		{"Custom accepts a holding predicate", Custom(true, "no"), true},
		{"Custom refuses a broken one", Custom(false, "broken %s", "here"), false},
	} {
		t.Run(c.name, func(t *testing.T) {
			err := Run("g", c.rule)
			if c.holds && err != nil {
				t.Errorf("expected it to hold, got: %v", err)
			}
			if !c.holds && err == nil {
				t.Error("expected it to fail, it held")
			}
		})
	}
}
