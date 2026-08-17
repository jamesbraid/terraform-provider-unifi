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

// Digests is build/m0/provider-schema-digests.json.
//
// It carries two claims at different grains: one hash over the whole canonical
// schema, and a map of one hash per surface keyed <container>.<surface>.
type Digests struct {
	FormatVersion   int               `json:"format_version"`
	ProviderAddress string            `json:"provider_address"`
	CanonicalSHA256 string            `json:"canonical_schema_sha256"`
	SchemaSHA256    map[string]string `json:"schema_sha256"`
}

// CheckDigests is the same claim as CheckParity, one abstraction up.
//
// catalog-build-schema.sh compared this file against the freshly built
// candidate with cmp, and it was classified as "digests" and left byte-exact
// when the four content comparisons became ledger-aware. Counting its consumers
// settles what it is: roughly eighty references pass it as -baseline --
// catalog-evidence, catalog-parity, the provider-spec-compiler's generate
// lines, and four test packages. Every one reads it as THE RELEASED BASELINE.
// The single cmp against the candidate is the lone misreading, exactly as one
// line of one script misread provider-contracts/schema. Second instance of the
// same defect, and the same fix: the file stays frozen, the comparison learns
// the ledger.
//
// TWO GRAINS, HANDLED DIFFERENTLY, because only one of them can be scoped.
//
// The per-surface map can: a surface may differ when the ledger declares a
// change for it, and a surface with no entry may not. It is deliberately scoped
// to resource_schemas. A ledger entry names surface, attribute and field with
// no container, so nothing distinguishes unifi_network the resource from
// unifi_network the data source -- and letting an entry written about a
// resource license a change to a data source would be a declaration approving
// something its author never saw. Same limitation as the container-level paths
// in coordinates(), and the same refusal to paper over it.
//
// The whole-schema hash cannot be scoped, so it is asserted in the INVERSE
// direction. With declared changes it MUST differ, because a declared change
// that leaves the whole-schema hash untouched never reached the schema. With no
// declared changes it must match. A gate that merely tolerated a differing hash
// whenever the ledger was non-empty would stop asserting anything the moment
// one entry existed.
func CheckDigests(released, candidate Digests, ledger Ledger) []Finding {
	var findings []Finding

	declared := map[string]bool{}
	for _, e := range ledger.Entries {
		declared[e.Surface] = true
	}

	var undeclared, unmoved []string
	for _, key := range sortedKeys(released.SchemaSHA256, candidate.SchemaSHA256) {
		before, hadBefore := released.SchemaSHA256[key]
		after, hadAfter := candidate.SchemaSHA256[key]
		container, surface, _ := strings.Cut(key, ".")
		licensed := container == "resource_schemas" && declared[surface]

		switch {
		case !hadBefore || !hadAfter:
			undeclared = append(undeclared, fmt.Sprintf("%s (present on one side only)", key))
		case before != after && !licensed:
			undeclared = append(undeclared, key)
		case before == after && licensed:
			unmoved = append(unmoved, key)
		}
	}

	if len(undeclared) > 0 {
		findings = append(findings, Finding{
			Check: "schema-digests/per-surface",
			Detail: fmt.Sprintf("%d surface digest(s) moved without a ledger entry: %s. The committed "+
				"digests are the released baseline that roughly eighty consumers read as -baseline; "+
				"declare the change or revert it, and do not regenerate the baseline",
				len(undeclared), strings.Join(undeclared, ", ")),
		})
	}
	if len(unmoved) > 0 {
		findings = append(findings, Finding{
			Check: "schema-digests/per-surface",
			Detail: fmt.Sprintf("%d surface(s) are declared in the ledger but their digest did not "+
				"move: %s. Either the change was reverted and the entry outlived it, or it never "+
				"reached the schema", len(unmoved), strings.Join(unmoved, ", ")),
		})
	}

	switch {
	case len(ledger.Entries) > 0 && released.CanonicalSHA256 == candidate.CanonicalSHA256:
		findings = append(findings, Finding{
			Check: "schema-digests/whole-schema",
			Detail: fmt.Sprintf("the ledger declares %d change(s) and the whole-schema hash did not "+
				"move (%s). A declared change that leaves this hash untouched never reached the schema",
				len(ledger.Entries), released.CanonicalSHA256),
		})
	case len(ledger.Entries) == 0 && released.CanonicalSHA256 != candidate.CanonicalSHA256:
		findings = append(findings, Finding{
			Check: "schema-digests/whole-schema",
			Detail: fmt.Sprintf("the whole-schema hash moved (%s -> %s) with nothing declared",
				released.CanonicalSHA256, candidate.CanonicalSHA256),
		})
	}
	return findings
}

func sortedKeys(a, b map[string]string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(a)+len(b))
	for _, m := range []map[string]string{a, b} {
		for k := range m {
			if _, ok := seen[k]; !ok {
				seen[k] = struct{}{}
				out = append(out, k)
			}
		}
	}
	sort.Strings(out)
	return out
}
