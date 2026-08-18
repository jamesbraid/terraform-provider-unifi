// Package receiptcheck holds the assertions that every receipt gate repeats.
//
// MEASURED BEFORE IT WAS WRITTEN. Across catalogparity, releasequalification and
// managementcontract there are 576 assertions that name a receipt field, and
// they take only 65 distinct shapes; ten shapes account for 75% of them. The
// duplication is not in the field names -- it is in the rules, which is why
// twenty-three receipt types each looked unique.
//
// THE ONE CONSTRAINT THAT DECIDES WHETHER THIS IS SAFE. A shape alone is not a
// rule. `!validHex(FIELD, NUM)` covers validHex(x, 40), a commit SHA, and
// validHex(x, 64), a sha256 -- different rules under one shape. `FIELD != STR`
// spans twenty-six distinct expected literals. Consolidating to the shape and
// dropping the operand would turn twenty-six expected values into one and let a
// 40-character commit satisfy a digest slot, while every test stayed green.
//
// SO EVERY PRIMITIVE HERE TAKES ITS OPERAND AS A REQUIRED PARAMETER. There is no
// Hex() without a length and no Equals() without a want. The constraint is
// structural rather than a convention someone has to remember, because a
// convention would survive exactly until the next rule was added.
//
// NO REFLECTION, DELIBERATELY. A rule set keyed on json tag names would let a
// misspelled field check nothing at all and still pass -- a check that cannot
// fail, which is the defect class this apparatus exists to catch. Callers pass
// the value directly, so the compiler proves the field exists.
package receiptcheck

import (
	"encoding/hex"
	"fmt"
	"strings"
)

// A Rule is one assertion that has already been bound to both the value it
// examines and the operand it compares against. A nil error means it held.
type Rule struct {
	problem string
}

func ok() Rule                         { return Rule{} }
func bad(format string, a ...any) Rule { return Rule{problem: fmt.Sprintf(format, a...)} }

// Equals is `field == want`, the commonest rule in the corpus (88 sites).
func Equals(field, got, want string) Rule {
	if got == want {
		return ok()
	}
	return bad("%s is %q, want %q", field, got, want)
}

// NotEmpty is the rule that a field says anything at all.
func NotEmpty(field, got string) Rule {
	if got != "" {
		return ok()
	}
	return bad("%s is empty, so the receipt names no value for it", field)
}

// Hex requires exactly length hex characters. THE LENGTH IS THE RULE, not a
// detail of it: 40 is a commit SHA and 64 is a sha256, and a checker that
// accepted either would accept a commit where a digest belongs.
func Hex(field, got string, length int) Rule {
	if len(got) == length && isHex(got) {
		return ok()
	}
	return bad("%s is %q, want %d hex characters", field, got, length)
}

// Differ requires two fields to hold different values. Equal digests measured by
// different methods over different bytes mean one was copied, not measured.
func Differ(fieldA, gotA, fieldB, gotB, why string) Rule {
	if gotA != gotB {
		return ok()
	}
	return bad("%s and %s are identical, %s", fieldA, fieldB, why)
}

// IsFalse requires a boolean field to be false, and says why it matters.
func IsFalse(field string, got bool, why string) Rule {
	if !got {
		return ok()
	}
	return bad("%s is true; %s", field, why)
}

// Count requires a collection to hold exactly want entries.
func Count(field string, got, want int) Rule {
	if got == want {
		return ok()
	}
	return bad("%s holds %d entries, want %d", field, got, want)
}

// Custom carries a rule that has no primitive, so the residue stays visible as
// residue rather than being bent into a primitive that nearly fits.
func Custom(holds bool, format string, a ...any) Rule {
	if holds {
		return ok()
	}
	return bad(format, a...)
}

// Failure lists what a gate wanted and did not get.
//
// IT IS A TYPE RATHER THAN A STRING because callers inspect it: the catalog
// gates' own test asserts err.(*GateFailure) and counts Mismatch, so collapsing
// this to fmt.Errorf would have removed a capability while every message stayed
// byte-identical. That is the quietest kind of behaviour change a consolidation
// can make, and it is only visible if you read the consumer.
type Failure struct {
	Gate     string
	Mismatch []string
}

func (f *Failure) Error() string {
	return fmt.Sprintf("%s: %d assertion(s) failed:\n    %s",
		f.Gate, len(f.Mismatch), strings.Join(f.Mismatch, "\n    "))
}

// Run reports EVERY failing rule rather than the first.
//
// A receipt wrong in four ways, reported one at a time, costs a pipeline per
// fix -- and on the manual-only workflows where these gates run, a pipeline is
// not cheap.
func Run(gate string, rules ...Rule) error {
	if len(rules) == 0 {
		// A gate with no rules passes everything. That is a check that cannot
		// fail, so it is an error in its own right.
		return &Failure{Gate: gate, Mismatch: []string{
			"no rules were supplied, so this gate asserts nothing"}}
	}
	var failed []string
	for _, r := range rules {
		if r.problem != "" {
			failed = append(failed, r.problem)
		}
	}
	if len(failed) == 0 {
		// A literal nil, never a typed nil pointer: returning (*Failure)(nil)
		// as an error gives a non-nil interface and every gate would read as
		// failing.
		return nil
	}
	return &Failure{Gate: gate, Mismatch: failed}
}

func isHex(s string) bool {
	_, err := hex.DecodeString(s)
	return err == nil
}
