package schemaparity

import (
	"fmt"
	"sort"
	"strings"
)

// The shell gate this replaces carries SIXTEEN assertions, not the six `cmp`
// calls a reader counts first. Only the parity pair loses byte-identity; every
// other one keeps it and is stated here as its own named function, because a
// port that carried the headline and quietly dropped the controls would look
// like a success.
//
// Enumerated from catalog-build-schema.sh before writing any of this:
//
//	 52        the v0.101.2 tag resolves to the baseline manifest's commit  PROVENANCE
//	 72        the same tree built twice yields one binary                  DETERMINISM
//	 85-86     an injected released binary matches its declared sha256      PROVENANCE
//	146-147    built released vs built candidate, both CLIs                 PARITY, ledger-aware
//	148-151    frozen baseline vs built candidate, both CLIs                PARITY, ledger-aware
//	152        committed digests vs candidate digests                       DIGEST
//	160        terraform vs tofu agree on the shared surface                CROSS-CLI
//	162        terraform vs tofu MUST differ on the full projection         INVERTED CONTROL
//	166-168    action count 1, list count 25, tofu has neither key          SHAPE
//
// Relaxing 72 would admit a non-deterministic build. Relaxing 160 would let the
// two CLIs disagree. Relaxing 162 would delete a deliberate negative control --
// it fails when the two projections are the SAME, which is the one direction a
// careless port turns into a pass.

// Finding is one failed assertion, named by the check that produced it.
type Finding struct {
	Check   string
	Detail  string
	Differs []Difference
}

func (f Finding) String() string {
	if len(f.Differs) == 0 {
		return fmt.Sprintf("%s: %s", f.Check, f.Detail)
	}
	lines := make([]string, 0, len(f.Differs)+1)
	lines = append(lines, fmt.Sprintf("%s: %s", f.Check, f.Detail))
	for _, d := range f.Differs {
		lines = append(lines, "    "+d.String())
	}
	return strings.Join(lines, "\n")
}

// Projections is one run's canonicalised output, keyed by CLI name.
type Projections struct {
	Released  map[string]any
	Candidate map[string]any
}

// CheckParity is the only assertion that loses byte-identity.
//
// It reports a finding when a difference is not licensed by the ledger, AND
// when a ledger entry matches no difference. The second half is what makes it
// set equality: without it, a ledger accumulates entries for changes that were
// reverted or never made, and every one of them reads as though it still
// describes the tree.
func CheckParity(cli string, released, candidate any, ledger Ledger) []Finding {
	diffs := Diff(released, candidate)
	_, undeclared, unmatched := Classify(diffs, ledger)

	var findings []Finding
	if len(undeclared) > 0 {
		findings = append(findings, Finding{
			Check: "schema-parity/" + cli,
			Detail: fmt.Sprintf("%d schema difference(s) are not declared in the schema-change ledger. "+
				"Declare each one -- surface, attribute, field, exact old and new values, and why it "+
				"cannot break an existing configuration -- or revert it. Do not edit the frozen baseline",
				len(undeclared)),
			Differs: undeclared,
		})
	}
	if len(unmatched) > 0 {
		names := make([]string, 0, len(unmatched))
		for _, e := range unmatched {
			names = append(names, fmt.Sprintf("%s.%s %s (%s -> %s)", e.Surface, e.Attribute, e.Field, e.Old, e.New))
		}
		sort.Strings(names)
		findings = append(findings, Finding{
			Check: "schema-parity/" + cli,
			Detail: fmt.Sprintf("%d ledger entr(y/ies) match no observed difference, so they license "+
				"nothing: %s. Either the change was reverted and the entry outlived it, or it names a "+
				"transition nobody produced", len(unmatched), strings.Join(names, "; ")),
		})
	}
	return findings
}

// CheckDeterminism keeps byte-identity. Two builds of one tree must agree.
//
// Stated separately from parity because they are unrelated claims that happen
// to share a comparison operator, and the whole risk in this port is treating
// "it was a cmp" as if it were one assertion.
func CheckDeterminism(label, firstDigest, secondDigest string) []Finding {
	if firstDigest == secondDigest {
		return nil
	}
	return []Finding{{
		Check: "build-determinism/" + label,
		Detail: fmt.Sprintf("the same tree built twice produced different binaries (%s then %s). "+
			"Every downstream comparison assumes a build is a function of its source", firstDigest, secondDigest),
	}}
}

// CheckProvenance keeps byte-identity: the released side must be the tag it
// claims to be, or the whole comparison is against something unknown.
func CheckProvenance(what, want, got string) []Finding {
	if want == got {
		return nil
	}
	return []Finding{{
		Check: "released-provenance/" + what,
		Detail: fmt.Sprintf("expected %s, measured %s. The released side of every comparison below "+
			"would otherwise be an unidentified tree", want, got),
	}}
}

// CheckCrossCLI keeps byte-identity. Terraform and OpenTofu must agree on the
// surface they share, or the provider serves two different contracts.
func CheckCrossCLI(build string, terraformShared, tofuShared any) []Finding {
	diffs := Diff(terraformShared, tofuShared)
	if len(diffs) == 0 {
		return nil
	}
	return []Finding{{
		Check: "cross-cli/" + build,
		Detail: fmt.Sprintf("terraform and tofu disagree on %d leaf/leaves of the shared surface. "+
			"The same provider is serving two contracts", len(diffs)),
		Differs: diffs,
	}}
}

// CheckProjectionsDiffer is the INVERTED control, and the one a careless port
// turns into a pass.
//
// It fails when the two projections are the SAME. Terraform reports action and
// list-resource schemas; OpenTofu does not. If they ever match on the full
// projection, either the fixture stopped exercising the difference or one CLI
// silently stopped reporting what it used to -- both of which make every
// cross-CLI comparison above vacuous rather than wrong.
func CheckProjectionsDiffer(terraformFull, tofuFull any) []Finding {
	if len(Diff(terraformFull, tofuFull)) > 0 {
		return nil
	}
	return []Finding{{
		Check: "cross-cli/inverted-control",
		Detail: "terraform and tofu returned the SAME full projection. They must differ: terraform " +
			"reports action_schemas and list_resource_schemas and tofu does not. Equality here means " +
			"the comparison is no longer exercising anything, not that everything agrees",
	}}
}

// CheckShape keeps byte-identity on the counts that make the inverted control
// mean something: which keys each CLI reports, and how many entries they hold.
func CheckShape(terraformFull, tofuFull map[string]any, wantActions, wantLists int) []Finding {
	var findings []Finding
	count := func(doc map[string]any, key string) int {
		v, ok := doc[key].(map[string]any)
		if !ok {
			return -1
		}
		return len(v)
	}
	if got := count(terraformFull, "action_schemas"); got != wantActions {
		findings = append(findings, Finding{Check: "projection-shape/action_schemas",
			Detail: fmt.Sprintf("terraform reports %d, want %d", got, wantActions)})
	}
	if got := count(terraformFull, "list_resource_schemas"); got != wantLists {
		findings = append(findings, Finding{Check: "projection-shape/list_resource_schemas",
			Detail: fmt.Sprintf("terraform reports %d, want %d", got, wantLists)})
	}
	for _, key := range []string{"action_schemas", "list_resource_schemas"} {
		if _, present := tofuFull[key]; present {
			findings = append(findings, Finding{Check: "projection-shape/tofu",
				Detail: fmt.Sprintf("tofu reports %q, which it is not expected to. The inverted control "+
					"relies on tofu omitting both keys", key)})
		}
	}
	return findings
}
