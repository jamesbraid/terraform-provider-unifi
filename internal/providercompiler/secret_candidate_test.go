package providercompiler

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// The secret-candidate check refuses a bootstrap field whose name says it
// carries a credential unless the policy has disposed of it safely -- omitted,
// or exposed and marked sensitive.
//
// THE FAILURE THESE TESTS EXIST FOR IS THE LOOKUP, NOT THE RULE. A policy
// disposes of a field in four places, and the original lookup read one:
//
//	fields        structural_name              4045
//	groupings     members[].structural_name     294
//	flattenings   members[].structural_name       4
//	claims        structural_nameS (plural)      63
//
// Measured across the 67 policies: 4406 dispositions, 361 of them invisible to
// a fields-only search. vpn_server and vpn_client disposition EVERY x_ field
// through a grouping, so a fields-only guard calls eleven already-declared,
// already-masked secrets undeclared -- and the remedy it asks for is to omit
// them, which would delete ten live sensitive attributes from the schema.
// TestCompileAcceptsASecretCandidateDispositionedByAGrouping is that regression.
func secretCandidateInput(t *testing.T) CompileInput {
	t.Helper()
	read := func(path string) []byte {
		t.Helper()
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	return CompileInput{
		Bootstrap:       read("../../provider-codegen/bootstrap/go-unifi-v1.103.0-vpn-server.json"),
		Policy:          read("../../provider-codegen/policy/vpn_server.json"),
		BaselineDigests: read("../../build/m0/provider-schema-digests.json"),
		Ledger:          read("../../provider-codegen/generated/catalog-parity-ledger.json"),
	}
}

// TestCompileAcceptsASecretCandidateDispositionedByAGrouping compiles the real
// vpn_server, whose fourteen x_ fields are every one of them declared as
// grouping members. A fields-only lookup fails this with "x_auth_key lacks a
// safe provider disposition" while the policy is correct.
func TestCompileAcceptsASecretCandidateDispositionedByAGrouping(t *testing.T) {
	if _, err := Compile(secretCandidateInput(t)); err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
}

// TestCompileRefusesASecretCandidateWithNoDisposition removes one grouping
// member and nothing else, so the field remains a secret candidate on the
// bootstrap with no disposition anywhere.
//
// The policy is edited IN MEMORY. Mutating the file on disk would leave the
// tree dirty if the test failed part-way, and this suite runs beside generators
// that refuse to run against a dirty tree.
//
// It asserts the message names the field AND all four places searched, because
// "lacks a safe disposition" alone sends an author to policy.fields, which is
// exactly where the field is not.
func TestCompileRefusesASecretCandidateWithNoDisposition(t *testing.T) {
	input := secretCandidateInput(t)

	var document map[string]any
	if err := json.Unmarshal(input.Policy, &document); err != nil {
		t.Fatal(err)
	}
	groupings, _ := document["groupings"].([]any)
	removed := 0
	for _, raw := range groupings {
		group, _ := raw.(map[string]any)
		members, _ := group["members"].([]any)
		kept := members[:0]
		for _, entry := range members {
			member, _ := entry.(map[string]any)
			if member["structural_name"] == "x_auth_key" {
				removed++
				continue
			}
			kept = append(kept, entry)
		}
		group["members"] = kept
	}
	if removed != 1 {
		t.Fatalf("removed %d members named x_auth_key, want exactly 1 -- the fixture no longer describes the policy", removed)
	}
	edited, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	input.Policy = edited

	_, err = Compile(input)
	if err == nil {
		t.Fatal("Compile() accepted a secret candidate with no disposition in any of the four places")
	}
	for _, want := range []string{"x_auth_key", "fields", "groupings", "flattenings", "claims"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("Compile() error = %v, want it to mention %q", err, want)
		}
	}
}
